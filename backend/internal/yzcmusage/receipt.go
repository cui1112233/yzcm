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
	"sort"
	"strings"
	"time"
)

const receiptWriteTimeout = 2 * time.Second

var (
	ErrReceiptPersistenceUnavailable = errors.New("neither local recovery nor MySQL can persist the YZCM usage receipt")
	errReceiptDiskUnavailable        = errors.New("usage receipt disk access unavailable")
	errReceiptQuarantineFailed       = errors.New("could not quarantine conflicting usage receipt")
	errReceiptDatabaseUnavailable    = errors.New("usage receipt database is not configured")
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

func receiptReviewDirectory(dir string) string {
	return filepath.Join(dir, ".review", "attribution-conflict")
}

func receiptInReview(dir, name string) bool {
	info, err := os.Lstat(filepath.Join(receiptReviewDirectory(dir), name))
	return err == nil && info.Mode().IsRegular()
}

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

// Only probe MySQL when the local recovery directory fails a write probe.
func (t *Tracker) preflightReceipt(ctx context.Context) error {
	dir, claimErr := t.receiptDirectory(true)
	if claimErr == nil {
		probe, err := os.CreateTemp(dir, ".receipt-probe-*.tmp")
		if err == nil {
			claimErr = probe.Close()
			_ = os.Remove(probe.Name())
		} else {
			claimErr = err
		}
	}
	if claimErr == nil {
		return nil
	}
	dbErr := errReceiptDatabaseUnavailable
	if t.Store.DB != nil {
		probeCtx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		dbErr = t.Store.DB.PingContext(probeCtx)
	}
	if dbErr == nil {
		return nil
	}
	return errors.Join(ErrReceiptPersistenceUnavailable, claimErr, dbErr)
}

func (t *Tracker) saveReceipt(r receipt) error {
	if !r.valid() {
		return errors.New("invalid usage receipt")
	}
	dir, err := t.receiptDirectory(true)
	if err != nil {
		return err
	}
	name := receiptName(r.Request.ID)
	if receiptInReview(dir, name) {
		return ErrReceiptAttributionConflict
	}
	dest := filepath.Join(dir, name)
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
	snapshot := receipt{Request: r, CredentialHash: hash, Status: status, At: at}
	if !snapshot.valid() {
		return errors.New("invalid usage receipt")
	}
	claimErr := t.saveReceipt(snapshot)
	if errors.Is(claimErr, ErrReceiptAttributionConflict) {
		return claimErr
	}
	if claimErr == nil {
		dir, err := t.receiptDirectory(false)
		if err == nil {
			applyCtx, cancel := context.WithTimeout(ctx, receiptWriteTimeout)
			err = t.applyReceipt(applyCtx, dir, receiptName(r.ID))
			cancel()
			if !errors.Is(err, errReceiptDiskUnavailable) {
				return err
			}
		}
		claimErr = err
	}
	return t.recordReceiptWithoutClaim(ctx, snapshot, claimErr)
}

func (t *Tracker) writeReceipt(ctx context.Context, r receipt) error {
	if t.Store.DB == nil {
		return errReceiptDatabaseUnavailable
	}
	return t.Store.recordAt(ctx, r.Request, r.CredentialHash, r.Status, r.At)
}

func (t *Tracker) recordReceiptWithoutClaim(ctx context.Context, r receipt, claimErr error) error {
	readableClaim := false
	if dir, err := t.receiptDirectory(false); err == nil {
		name := receiptName(r.Request.ID)
		if receiptInReview(dir, name) {
			return ErrReceiptAttributionConflict
		}
		if saved, err := readReceipt(filepath.Join(dir, name)); err == nil {
			r, readableClaim = saved, true
		}
	}
	// A readable first claim still wins if only the lock or sync failed.
	// Otherwise the MySQL unique key decides the first stored attribution.
	dbCtx, cancel := context.WithTimeout(ctx, receiptWriteTimeout)
	defer cancel()
	err := t.writeReceipt(dbCtx, r)
	if err == nil || readableClaim || errors.Is(err, ErrReceiptAttributionConflict) {
		return err
	}
	return errors.Join(ErrReceiptPersistenceUnavailable, claimErr, err)
}

func (t *Tracker) applyReceipt(ctx context.Context, dir, name string) error {
	// Stable shard files are never unlinked. The OS releases their locks on
	// process exit. A single lock covers reading the claim, MySQL, and cleanup.
	lockPath := receiptLockPath(dir, name)
	if err := os.MkdirAll(filepath.Dir(lockPath), 0700); err != nil {
		return errors.Join(errReceiptDiskUnavailable, err)
	}
	lock, err := openReceiptLock(ctx, lockPath)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return err // A contending process may still be applying the first claim.
		}
		return errors.Join(errReceiptDiskUnavailable, err)
	}
	defer lock.Close()
	path := filepath.Join(dir, name)
	r, err := readReceipt(path)
	if os.IsNotExist(err) {
		if receiptInReview(dir, name) {
			return ErrReceiptAttributionConflict
		}
		return nil // A concurrent worker already committed and removed this file.
	}
	if err != nil {
		return err
	}
	if receiptInReview(dir, name) {
		return quarantineReceipt(dir, name)
	}
	if err := t.writeReceipt(ctx, r); err != nil {
		if errors.Is(err, ErrReceiptAttributionConflict) {
			return quarantineReceipt(dir, name)
		}
		return err
	}
	return os.Remove(path)
}

func quarantineReceipt(dir, name string) error {
	if err := moveReceiptToReview(dir, name); err != nil {
		return errors.Join(ErrReceiptAttributionConflict, errReceiptQuarantineFailed, err)
	}
	return ErrReceiptAttributionConflict
}

// Publish the original evidence atomically before unlinking its active name.
// Keep the receipt lock across both operations so interrupted moves are safe
// even on filesystems where a directory-to-directory rename is non-atomic.
func moveReceiptToReview(dir, name string) error {
	source := filepath.Join(dir, name)
	sourceInfo, err := os.Lstat(source)
	if err != nil {
		return err
	}
	review := receiptReviewDirectory(dir)
	target := filepath.Join(review, name)
	if previous, err := os.Lstat(target); err == nil && !os.SameFile(sourceInfo, previous) {
		// A publisher may have raced the first move. Keep both pieces of evidence
		// and use a stable name in case a crash interrupted this move as well.
		raw, err := os.ReadFile(source)
		if err != nil {
			return err
		}
		review = filepath.Join(review, "duplicate-"+Fingerprint(string(raw)))
		target = filepath.Join(review, name)
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(review, 0700); err != nil {
		return err
	}
	if err := os.Link(source, target); os.IsExist(err) {
		published, statErr := os.Lstat(target)
		if statErr != nil || !os.SameFile(sourceInfo, published) {
			return errors.New("review evidence path is already occupied")
		}
	} else if err != nil {
		return err
	}
	for folder := review; ; folder = filepath.Dir(folder) {
		if err := syncReceiptDirectory(folder); err != nil {
			return err
		}
		if folder == dir {
			break
		}
	}
	if err := os.Remove(source); err != nil {
		return err
	}
	return syncReceiptDirectory(dir)
}

func logReceiptError(requestID string, err error) {
	name := receiptName(requestID)
	switch {
	case errors.Is(err, ErrReceiptPersistenceUnavailable):
		log.Printf("YZCM usage: receipt_not_durable receipt=%s after provider response; local recovery and MySQL failed", name)
	case errors.Is(err, errReceiptQuarantineFailed):
		log.Printf("YZCM usage: receipt_quarantine_failed receipt=%s reason=attribution-conflict", name)
	case errors.Is(err, ErrReceiptAttributionConflict):
		log.Printf("YZCM usage: receipt_attribution_conflict receipt=%s", name)
	default:
		log.Printf("YZCM usage: receipt_pending receipt=%s", name)
	}
}

func (t *Tracker) flushReceipts(parent context.Context) {
	t.receiptScanMu.Lock()
	defer t.receiptScanMu.Unlock()
	dir, err := t.receiptDirectory(false)
	if err != nil {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) == 0 {
		return
	}
	// Continue past the last inspected name on the next pass even if moving
	// a conflict failed. Work and MySQL wait remain bounded per pass.
	start := sort.Search(len(entries), func(i int) bool { return entries[i].Name() > t.receiptCursor })
	if start == len(entries) {
		start = 0
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	quarantined, failed := 0, 0
	for _, entry := range entries[start:min(start+100, len(entries))] {
		if ctx.Err() != nil {
			break
		}
		name := entry.Name()
		previous := t.receiptCursor
		t.receiptCursor = name
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
		itemCtx, cancelItem := context.WithTimeout(ctx, receiptWriteTimeout)
		err := t.applyReceipt(itemCtx, dir, name)
		cancelItem()
		if ctx.Err() != nil {
			// Give a receipt cut short by the batch deadline a full next attempt.
			t.receiptCursor = previous
		}
		if errors.Is(err, errReceiptQuarantineFailed) {
			failed++
		} else if errors.Is(err, ErrReceiptAttributionConflict) {
			quarantined++
		}
	}
	if quarantined+failed > 0 {
		log.Printf("YZCM usage: receipt_review reason=attribution-conflict quarantined=%d quarantine_failed=%d", quarantined, failed)
	}
}
