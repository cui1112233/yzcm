package yzcmusage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const receiptDirectoryIDFile = ".receipt-directory-id"

var errReceiptDirectoryChanged = errors.New("configured usage receipt directory is missing, inaccessible or no longer matches its binding")

// Create only a direct child. A lost recovery root must never be recreated.
func makeReceiptDirectory(path string) error {
	if err := os.Mkdir(path, 0700); err != nil && !os.IsExist(err) {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("invalid usage receipt subdirectory")
	}
	return nil
}

type receiptBinding struct {
	dir, anchor, id string
	info            os.FileInfo
}

func visibleReceiptDirectory(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errReceiptDirectoryChanged
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, errReceiptDirectoryChanged
	}
	if _, err := f.ReadDir(1); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return info, nil
}

func readReceiptDirectoryID(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() != 32 {
		return "", errReceiptDirectoryChanged
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 33))
	if err != nil {
		return "", err
	}
	if _, err := hex.DecodeString(string(raw)); len(raw) != 32 || err != nil {
		return "", errReceiptDirectoryChanged
	}
	return string(raw), nil
}

// Publish a fully synced file without replacing evidence from another process.
func publishReceiptFile(path, pattern string, raw []byte) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err = errors.Join(err, closeErr); err != nil {
		return err
	}
	if err := os.Link(f.Name(), path); err != nil && !os.IsExist(err) {
		return err
	}
	return syncReceiptDirectory(dir)
}

func (t *Tracker) bindReceiptDirectory() (receiptBinding, error) {
	var b receiptBinding
	if strings.TrimSpace(t.ReceiptDir) == "" || t.DBOnly {
		return b, errReceiptDirectoryChanged
	}
	t.receiptBindingMu.Lock()
	bound := t.receiptBound
	t.receiptBindingMu.Unlock()
	if bound != nil {
		return *bound, bound.verifyVisible()
	}
	dir, err := filepath.Abs(t.ReceiptDir)
	if err != nil {
		return b, err
	}
	// Never recreate a missing configured directory. It might contain claims
	// on a volume or mount that has temporarily become invisible.
	info, err := visibleReceiptDirectory(dir)
	if err != nil {
		return b, errors.Join(errReceiptDirectoryChanged, err)
	}
	anchor := filepath.Join(filepath.Dir(dir), "."+filepath.Base(dir)+"-receipt-id")
	inside := filepath.Join(dir, receiptDirectoryIDFile)
	parentID, parentErr := readReceiptDirectoryID(anchor)
	innerID, innerErr := readReceiptDirectoryID(inside)
	if os.IsNotExist(parentErr) {
		if os.IsNotExist(innerErr) {
			var random [16]byte
			if _, err := rand.Read(random[:]); err != nil {
				return b, err
			}
			if err := publishReceiptFile(inside, ".receipt-binding-*.tmp", []byte(hex.EncodeToString(random[:]))); err != nil {
				return b, errors.Join(errReceiptDirectoryChanged, err)
			}
			innerID, innerErr = readReceiptDirectoryID(inside)
		}
		if innerErr != nil {
			return b, errors.Join(errReceiptDirectoryChanged, innerErr)
		}
		if err := publishReceiptFile(anchor, ".receipt-binding-*.tmp", []byte(innerID)); err != nil {
			return b, errors.Join(errReceiptDirectoryChanged, err)
		}
		parentID, parentErr = readReceiptDirectoryID(anchor)
	}
	if parentErr != nil || innerErr != nil || parentID != innerID {
		return b, errors.Join(errReceiptDirectoryChanged, parentErr, innerErr)
	}
	b = receiptBinding{dir: dir, anchor: anchor, id: parentID, info: info}
	if err := b.verify(); err != nil {
		return b, err
	}
	t.receiptBindingMu.Lock()
	defer t.receiptBindingMu.Unlock()
	if t.receiptBound != nil && (t.receiptBound.id != b.id || !os.SameFile(t.receiptBound.info, b.info)) {
		return b, errReceiptDirectoryChanged
	}
	t.receiptBound = &b
	return b, nil
}

func (b receiptBinding) verify() error {
	info, dirErr := os.Lstat(b.dir)
	parentID, parentErr := readReceiptDirectoryID(b.anchor)
	innerID, innerErr := readReceiptDirectoryID(filepath.Join(b.dir, receiptDirectoryIDFile))
	if dirErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || parentErr != nil || innerErr != nil || parentID != b.id || innerID != b.id || !os.SameFile(info, b.info) {
		return errors.Join(errReceiptDirectoryChanged, dirErr, parentErr, innerErr)
	}
	return nil
}

func (b receiptBinding) verifyVisible() error {
	if err := b.verify(); err != nil {
		return err
	}
	info, err := visibleReceiptDirectory(b.dir)
	if err != nil || !os.SameFile(info, b.info) {
		return errors.Join(errReceiptDirectoryChanged, err)
	}
	return nil
}

func (b receiptBinding) openPublicationLock(ctx context.Context) (*os.File, error) {
	return openReceiptLock(ctx, b.anchor+".publication-lock")
}

type receiptProbe struct {
	done chan struct{}
	err  error
}

func (t *Tracker) checkReceiptWriter(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	t.receiptProbeMu.Lock()
	if probe := t.receiptProbe; probe != nil {
		t.receiptProbeMu.Unlock()
		select {
		case <-probe.done:
			return probe.err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if time.Now().Before(t.receiptProbeRetry) {
		err := t.receiptProbeErr
		t.receiptProbeMu.Unlock()
		return err
	}
	probe := &receiptProbe{done: make(chan struct{})}
	t.receiptProbe = probe
	t.receiptProbeMu.Unlock()

	probe.err = t.Store.probeReceiptWrite(ctx)
	t.receiptProbeMu.Lock()
	if probe.err != nil && !errors.Is(probe.err, context.Canceled) {
		t.receiptProbeErr = probe.err
		t.receiptProbeRetry = time.Now().Add(time.Second)
	}
	t.receiptProbe = nil
	close(probe.done)
	t.receiptProbeMu.Unlock()
	return probe.err
}
