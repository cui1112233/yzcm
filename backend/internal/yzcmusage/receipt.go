package yzcmusage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// A receipt contains only attribution metadata. API keys and response content
// remain in memory and must never enter this recovery directory.
type receipt struct {
	Request        Request   `json:"request"`
	CredentialHash string    `json:"credentialHash"`
	Status         int       `json:"httpStatus"`
	At             time.Time `json:"recordedAt"`
}

func (r receipt) valid() bool {
	hash, err := hex.DecodeString(r.CredentialHash)
	return err == nil && len(hash) == 32 && r.Request.validIdentity() && len(r.Request.Feature) <= 64 && !r.At.IsZero() && r.Status >= 100 && r.Status <= 599
}

func receiptName(requestID string) string { return "receipt-" + Fingerprint(requestID) + ".json" }

func receiptLockPath(dir, name string) string {
	shard := sha256.Sum256([]byte(name))[0]
	return filepath.Join(dir, ".locks", fmt.Sprintf("%02x", shard))
}

func readReceipt(path string) (receipt, error) {
	var r receipt
	info, err := os.Lstat(path)
	if err != nil {
		return r, err
	}
	if !info.Mode().IsRegular() {
		return r, errors.New("invalid usage receipt file")
	}
	file, err := os.Open(path)
	if err != nil {
		return r, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, 16385))
	if err != nil {
		return r, err
	}
	if len(raw) > 16384 || json.Unmarshal(raw, &r) != nil || !r.valid() || filepath.Base(path) != receiptName(r.Request.ID) {
		return r, errors.New("invalid usage receipt payload")
	}
	return r, nil
}

func (t *Tracker) receiptDirectory(create bool) (string, error) {
	if strings.TrimSpace(t.ReceiptDir) == "" {
		return "", errors.New("usage receipt directory is not configured")
	}
	dir, err := filepath.Abs(t.ReceiptDir)
	if err != nil {
		return "", err
	}
	if create {
		if err = os.MkdirAll(dir, 0700); err != nil {
			return "", err
		}
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("invalid usage receipt directory")
	}
	return dir, nil
}
func (t *Tracker) saveReceipt(r receipt) error {
	if !r.valid() {
		return errors.New("invalid usage receipt")
	}
	dir, err := t.receiptDirectory(true)
	if err != nil {
		return err
	}
	dest := filepath.Join(dir, receiptName(r.Request.ID))
	if _, err := os.Lstat(dest); err == nil {
		return syncReceiptDirectory(dir)
	} else if !os.IsNotExist(err) {
		return err
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".receipt-*.tmp")
	if err != nil {
		return err
	}
	tmp := file.Name()
	defer os.Remove(tmp)
	if err = file.Chmod(0600); err != nil {
		file.Close()
		return err
	}
	if _, err = file.Write(raw); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	// Hard-link publication is atomic and never replaces an existing receipt.
	// The first attribution therefore survives duplicate deliveries and restarts.
	if err = os.Link(tmp, dest); os.IsExist(err) {
		return syncReceiptDirectory(dir)
	} else if err != nil {
		return err
	}
	return syncReceiptDirectory(dir)
}
func (t *Tracker) recordReceipt(ctx context.Context, r Request, hash string, status int, at time.Time) error {
	if err := t.saveReceipt(receipt{Request: r, CredentialHash: hash, Status: status, At: at}); err != nil {
		return err
	}
	dir, err := t.receiptDirectory(false)
	if err != nil {
		return err
	}
	return t.applyReceipt(ctx, dir, receiptName(r.ID))
}

func (t *Tracker) applyReceipt(ctx context.Context, dir, name string) error {
	// Stable shard files are never unlinked. The OS releases their locks on
	// process exit. A single lock covers reading the claim, MySQL, and cleanup.
	lockPath := receiptLockPath(dir, name)
	if err := os.MkdirAll(filepath.Dir(lockPath), 0700); err != nil {
		return err
	}
	lock, err := openReceiptLock(ctx, lockPath)
	if err != nil {
		return err
	}
	defer lock.Close()
	path := filepath.Join(dir, name)
	r, err := readReceipt(path)
	if os.IsNotExist(err) {
		return nil // A concurrent worker already committed and removed this file.
	}
	if err != nil {
		return err
	}
	if err := t.Store.recordAt(ctx, r.Request, r.CredentialHash, r.Status, r.At); err != nil {
		if errors.Is(err, ErrReceiptAttributionConflict) {
			return fmt.Errorf("%s: %w", name, err)
		}
		return err
	}
	return os.Remove(path)
}

func logReceiptError(err error) {
	if errors.Is(err, ErrReceiptAttributionConflict) {
		log.Printf("YZCM usage: %s (recovery receipt retained)", err)
	} else {
		log.Print("YZCM usage: request receipt pending or could not be persisted")
	}
}
func (t *Tracker) flushReceipts(parent context.Context) {
	dir, err := t.receiptDirectory(false)
	if err != nil {
		return
	}
	directory, err := os.Open(dir)
	if err != nil {
		return
	}
	defer directory.Close()
	// Bound both the number of receipts and database wait on each pass.
	entries, err := directory.ReadDir(100)
	if err != nil && err != io.EOF {
		return
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	for _, entry := range entries {
		if ctx.Err() != nil {
			return
		}
		name := entry.Name()
		if !entry.Type().IsRegular() {
			continue
		}
		if strings.HasPrefix(name, ".receipt-") && strings.HasSuffix(name, ".tmp") {
			if stat, err := entry.Info(); err == nil && time.Since(stat.ModTime()) > time.Hour {
				_ = os.Remove(filepath.Join(dir, name))
			}
			continue
		}
		if !strings.HasPrefix(name, "receipt-") || !strings.HasSuffix(name, ".json") || len(name) != len("receipt-")+64+len(".json") {
			continue
		}
		if err := t.applyReceipt(ctx, dir, name); err != nil && errors.Is(err, ErrReceiptAttributionConflict) {
			logReceiptError(err)
		}
	}
}
