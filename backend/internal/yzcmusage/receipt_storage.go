package yzcmusage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const receiptDirectoryIDFile = ".receipt-directory-id"
const receiptBindingTimeout = 5 * time.Second

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
	pathDigest      string
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

func receiptPathDigest(dir string) string {
	if runtime.GOOS == "windows" {
		dir = strings.ToLower(dir)
	}
	return Fingerprint(dir)
}

func newReceiptStoreID() (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(random[:]), nil
}

func inspectUnboundReceiptDirectory(b receiptBinding) (receiptBindingRow, error) {
	row := receiptBindingRow{PathDigest: b.pathDigest, State: receiptBindingPending}
	_, dirErr := visibleReceiptDirectory(b.dir)
	parentID, parentErr := readReceiptDirectoryID(b.anchor)
	if dirErr != nil {
		if os.IsNotExist(dirErr) && os.IsNotExist(parentErr) {
			row.Origin = receiptBindingNew
		} else {
			return row, errors.Join(errReceiptDirectoryChanged, dirErr, parentErr)
		}
	} else {
		innerID, innerErr := readReceiptDirectoryID(filepath.Join(b.dir, receiptDirectoryIDFile))
		switch {
		case os.IsNotExist(parentErr) && os.IsNotExist(innerErr):
			row.Origin = receiptBindingUpgrade
		case innerErr == nil && (os.IsNotExist(parentErr) || parentErr == nil && parentID == innerID):
			row.StoreID, row.Origin = innerID, receiptBindingAdopt
		default:
			return row, errors.Join(errReceiptDirectoryChanged, parentErr, innerErr)
		}
	}
	if row.StoreID == "" {
		id, err := newReceiptStoreID()
		row.StoreID = id
		return row, err
	}
	return row, nil
}

func preparePendingReceiptDirectory(ctx context.Context, b receiptBinding, row receiptBindingRow) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	parentID, parentErr := readReceiptDirectoryID(b.anchor)
	inside := filepath.Join(b.dir, receiptDirectoryIDFile)
	innerID, innerErr := readReceiptDirectoryID(inside)
	if !matchingOrMissingReceiptID(parentID, parentErr, row.StoreID) || !matchingOrMissingReceiptID(innerID, innerErr, row.StoreID) {
		return errReceiptDirectoryChanged
	}
	info, err := visibleReceiptDirectory(b.dir)
	if err != nil {
		// Only a fresh pending row may provision a missing directory. An
		// upgrade never replaces a directory that might already hold receipts.
		if !os.IsNotExist(err) || row.Origin != receiptBindingNew || parentErr == nil {
			return errors.Join(errReceiptDirectoryChanged, err)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := createPendingReceiptRoot(b.dir); err != nil {
			return err
		}
		info, err = visibleReceiptDirectory(b.dir)
		if err != nil {
			return err
		}
	}
	if row.Origin == receiptBindingAdopt && innerErr != nil {
		return errReceiptDirectoryChanged
	}
	for _, marker := range []struct {
		path string
		err  error
	}{{inside, innerErr}, {b.anchor, parentErr}} {
		if err := ctx.Err(); err != nil {
			return err
		}
		if marker.err != nil {
			if err := publishReceiptFile(marker.path, ".receipt-binding-*.tmp", []byte(row.StoreID)); err != nil {
				return err
			}
		}
	}
	b.info = info
	return b.verify()
}

func createPendingReceiptRoot(dir string) error {
	ancestor := filepath.Dir(dir)
	for {
		_, err := os.Stat(ancestor)
		if err == nil {
			break
		}
		if !os.IsNotExist(err) || filepath.Dir(ancestor) == ancestor {
			return err
		}
		ancestor = filepath.Dir(ancestor)
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	for parent := filepath.Dir(dir); ; parent = filepath.Dir(parent) {
		if err := syncReceiptDirectory(parent); err != nil {
			return err
		}
		if parent == ancestor {
			return nil
		}
	}
}

func matchingOrMissingReceiptID(id string, err error, want string) bool {
	return err == nil && id == want || os.IsNotExist(err)
}

// InitializeReceiptStorage verifies the immutable database binding at process
// startup. Failed verification is not cached and may be retried after recovery.
func (t *Tracker) InitializeReceiptStorage(ctx context.Context) error {
	if t.DBOnly && strings.TrimSpace(t.ReceiptDir) == "" {
		return t.bindReceiptDBOnly(ctx)
	}
	_, err := t.bindReceiptDirectory(ctx)
	return err
}

func (t *Tracker) bindReceiptDBOnly(parent context.Context) error {
	if !t.DBOnly || strings.TrimSpace(t.ReceiptDir) != "" {
		return errReceiptDirectoryChanged
	}
	t.receiptBindingMu.Lock()
	bound := t.receiptDBOnlyBound
	t.receiptBindingMu.Unlock()
	if bound {
		return nil
	}
	ctx, cancel := context.WithTimeout(parent, receiptBindingTimeout)
	defer cancel()
	pathDigest := Fingerprint("yzcm-receipt-db-only")
	row, found, err := t.Store.loadReceiptBinding(ctx)
	if err != nil {
		return err
	}
	if !found {
		id, err := newReceiptStoreID()
		if err != nil {
			return err
		}
		row, err = t.Store.reserveReceiptBinding(ctx, receiptBindingRow{PathDigest: pathDigest, StoreID: id, State: receiptBindingActive, Origin: receiptBindingDBOnly})
		if err != nil {
			return err
		}
	}
	if row.PathDigest != pathDigest || row.State != receiptBindingActive || row.Origin != receiptBindingDBOnly {
		return errReceiptDirectoryChanged
	}
	t.receiptBindingMu.Lock()
	t.receiptDBOnlyBound = true
	t.receiptBindingMu.Unlock()
	return nil
}

func (t *Tracker) bindReceiptDirectory(parent context.Context) (receiptBinding, error) {
	var b receiptBinding
	if strings.TrimSpace(t.ReceiptDir) == "" || t.DBOnly {
		return b, errReceiptDirectoryChanged
	}
	dir, err := filepath.Abs(t.ReceiptDir)
	if err != nil {
		return b, err
	}
	b = receiptBinding{dir: dir, anchor: filepath.Join(filepath.Dir(dir), "."+filepath.Base(dir)+"-receipt-id"), pathDigest: receiptPathDigest(dir)}
	t.receiptBindingMu.Lock()
	bound := t.receiptBound
	t.receiptBindingMu.Unlock()
	if bound != nil {
		if bound.pathDigest != b.pathDigest {
			return b, errReceiptDirectoryChanged
		}
		return *bound, bound.verifyVisible()
	}
	ctx, cancel := context.WithTimeout(parent, receiptBindingTimeout)
	defer cancel()
	row, found, err := t.Store.loadReceiptBinding(ctx)
	if err != nil {
		return b, err
	}
	if !found {
		proposed, err := inspectUnboundReceiptDirectory(b)
		if err != nil {
			return b, err
		}
		row, err = t.Store.reserveReceiptBinding(ctx, proposed)
		if err != nil {
			return b, err
		}
	}
	if _, err := hex.DecodeString(row.StoreID); len(row.StoreID) != 32 || err != nil || row.PathDigest != b.pathDigest {
		return b, errReceiptDirectoryChanged
	}
	switch row.Origin {
	case receiptBindingNew, receiptBindingUpgrade, receiptBindingAdopt:
	default:
		return b, errReceiptDirectoryChanged
	}
	b.id = row.StoreID
	switch row.State {
	case receiptBindingPending:
		if err := t.Store.finishReceiptBinding(ctx, row, func(current receiptBindingRow) error {
			return preparePendingReceiptDirectory(ctx, b, current)
		}); err != nil {
			return b, err
		}
	case receiptBindingActive:
	default:
		return b, errReceiptDirectoryChanged
	}
	b.info, err = visibleReceiptDirectory(dir)
	if err != nil {
		return b, errors.Join(errReceiptDirectoryChanged, err)
	}
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
