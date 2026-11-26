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

// A nil binding is reserved for the explicitly configured DB-only mode.
func (t *Tracker) preflightReceipt(ctx context.Context) (*receiptBinding, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if t.DBOnly && strings.TrimSpace(t.ReceiptDir) == "" {
		if err := t.bindReceiptDBOnly(ctx); err != nil {
			return nil, errors.Join(ErrReceiptPersistenceUnavailable, err)
		}
		if err := t.checkReceiptWriter(ctx); err != nil {
			return nil, errors.Join(ErrReceiptPersistenceUnavailable, err)
		}
		return nil, nil
	}
	b, err := t.bindReceiptDirectory(ctx)
	if err != nil {
		return nil, errors.Join(ErrReceiptPersistenceUnavailable, err)
	}
	probe, claimErr := os.CreateTemp(b.dir, ".receipt-probe-*.tmp")
	if claimErr == nil {
		claimErr = probe.Close()
		_ = os.Remove(probe.Name())
	}
	if claimErr == nil {
		if err := b.verifyVisible(); err != nil {
			return nil, errors.Join(ErrReceiptPersistenceUnavailable, err)
		}
		return &b, nil
	}
	// A readable directory can be protected against a concurrent publisher
	// while the ledger takes over. A hidden directory cannot be bypassed.
	lockCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	lock, err := b.openPublicationLock(lockCtx)
	if err != nil {
		return nil, errors.Join(ErrReceiptPersistenceUnavailable, claimErr, err)
	}
	_ = lock.Close()
	if err := t.checkReceiptWriter(ctx); err != nil {
		return nil, errors.Join(ErrReceiptPersistenceUnavailable, claimErr, err)
	}
	if err := b.verifyVisible(); err != nil {
		return nil, errors.Join(ErrReceiptPersistenceUnavailable, err)
	}
	return &b, nil
}

func saveReceiptAt(dir string, r receipt) error {
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
	return publishReceiptFile(dest, ".receipt-*.tmp", raw)
}

func (t *Tracker) recordBoundReceipt(ctx context.Context, b *receiptBinding, r receipt) error {
	if !r.valid() {
		return errors.New("invalid usage receipt")
	}
	if b == nil {
		if !t.DBOnly || strings.TrimSpace(t.ReceiptDir) != "" {
			return ErrReceiptPersistenceUnavailable
		}
		return t.writeReceiptFallback(ctx, r)
	}
	if err := b.verifyVisible(); err != nil {
		return unverifiedReceipt(*b, r, err)
	}
	lockCtx, cancelLock := context.WithTimeout(ctx, receiptWriteTimeout)
	lock, err := b.openPublicationLock(lockCtx)
	cancelLock()
	if err != nil {
		return t.recoverKnownClaim(ctx, *b, r, err)
	}
	if err := b.verifyVisible(); err != nil {
		_ = lock.Close()
		return unverifiedReceipt(*b, r, err)
	}
	claimErr := saveReceiptAt(b.dir, r)
	if errors.Is(claimErr, ErrReceiptAttributionConflict) {
		_ = lock.Close()
		return claimErr
	}
	if claimErr != nil {
		defer lock.Close()
		return t.writeAfterClaimFailure(ctx, *b, r, claimErr)
	}
	_ = lock.Close()
	applyCtx, cancel := context.WithTimeout(ctx, receiptWriteTimeout)
	defer cancel()
	err = t.applyReceipt(applyCtx, *b, receiptName(r.Request.ID))
	if errors.Is(err, errReceiptDirectoryChanged) {
		return unverifiedReceipt(*b, r, err)
	}
	if errors.Is(err, errReceiptDiskUnavailable) {
		return t.recoverKnownClaim(ctx, *b, r, err)
	}
	return err
}

func (t *Tracker) writeReceipt(ctx context.Context, r receipt) error {
	if t.Store.DB == nil {
		return errReceiptDatabaseUnavailable
	}
	return t.Store.recordAt(ctx, r.Request, r.CredentialHash, r.Status, r.At)
}

func (t *Tracker) writeReceiptFallback(ctx context.Context, r receipt) error {
	dbCtx, cancel := context.WithTimeout(ctx, receiptWriteTimeout)
	defer cancel()
	if err := t.writeReceipt(dbCtx, r); err != nil && !errors.Is(err, ErrReceiptAttributionConflict) {
		return errors.Join(ErrReceiptPersistenceUnavailable, err)
	} else {
		return err
	}
}

func readBoundClaim(b receiptBinding, name string) (receipt, error) {
	if err := b.verify(); err != nil {
		return receipt{}, err
	}
	review, err := os.Lstat(receiptReviewDirectory(b.dir))
	if err == nil && (!review.IsDir() || review.Mode()&os.ModeSymlink != 0) || err != nil && !os.IsNotExist(err) {
		return receipt{}, errReceiptDirectoryChanged
	}
	_, err = readReceipt(filepath.Join(receiptReviewDirectory(b.dir), name))
	if err == nil {
		return receipt{}, ErrReceiptAttributionConflict
	}
	if !os.IsNotExist(err) {
		return receipt{}, errReceiptDirectoryChanged
	}
	r, err := readReceipt(filepath.Join(b.dir, name))
	if checkErr := b.verify(); checkErr != nil {
		return receipt{}, checkErr
	}
	return r, err
}

// This path runs while holding the publication lock. A proved absence may use
// the ledger directly. Any ambiguity is retained for review, never auto-posted.
func (t *Tracker) writeAfterClaimFailure(ctx context.Context, b receiptBinding, r receipt, claimErr error) error {
	saved, err := readBoundClaim(b, receiptName(r.Request.ID))
	switch {
	case err == nil:
		return t.writeKnownReceipt(ctx, saved)
	case errors.Is(err, ErrReceiptAttributionConflict):
		return err
	case !os.IsNotExist(err) || errors.Is(err, errReceiptDirectoryChanged):
		return unverifiedReceipt(b, r, errors.Join(claimErr, err))
	}
	info, err := visibleReceiptDirectory(b.dir)
	if err != nil || !os.SameFile(info, b.info) {
		return unverifiedReceipt(b, r, errors.Join(claimErr, errReceiptDirectoryChanged, err))
	}
	if err := t.writeReceiptFallback(ctx, r); errors.Is(err, ErrReceiptPersistenceUnavailable) {
		return unverifiedReceipt(b, r, errors.Join(claimErr, err))
	} else {
		return err
	}
}

func (t *Tracker) recoverKnownClaim(ctx context.Context, b receiptBinding, r receipt, cause error) error {
	saved, err := readBoundClaim(b, receiptName(r.Request.ID))
	if errors.Is(err, ErrReceiptAttributionConflict) {
		return err
	}
	if err != nil {
		return unverifiedReceipt(b, r, errors.Join(cause, err))
	}
	return t.writeKnownReceipt(ctx, saved)
}

func (t *Tracker) writeKnownReceipt(ctx context.Context, r receipt) error {
	dbCtx, cancel := context.WithTimeout(ctx, receiptWriteTimeout)
	defer cancel()
	return t.writeReceipt(dbCtx, r)
}

func unverifiedReceipt(b receiptBinding, r receipt, cause error) error {
	// The parent binding is separate from the recovery directory. Store only
	// review evidence here. This directory is never scanned for automatic replay.
	if id, err := readReceiptDirectoryID(b.anchor); err == nil && id == b.id {
		review := b.anchor + ".unverified"
		if err := makeReceiptDirectory(review); err != nil {
			return errors.Join(ErrReceiptPersistenceUnavailable, cause, err)
		}
		raw, err := json.Marshal(r)
		if err == nil {
			err = publishReceiptFile(filepath.Join(review, "receipt-"+Fingerprint(string(raw))+".json"), ".receipt-unverified-*.tmp", raw)
		}
		return errors.Join(ErrReceiptPersistenceUnavailable, cause, err)
	}
	return errors.Join(ErrReceiptPersistenceUnavailable, cause)
}

func (t *Tracker) applyReceipt(ctx context.Context, b receiptBinding, name string) error {
	// Stable shard files are never unlinked. The OS releases their locks on
	// process exit. A single lock covers reading the claim, MySQL, and cleanup.
	if err := b.verify(); err != nil {
		return err
	}
	lockPath := receiptLockPath(b.dir, name)
	if err := makeReceiptDirectory(filepath.Dir(lockPath)); err != nil {
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
	if err := b.verify(); err != nil {
		return err
	}
	path := filepath.Join(b.dir, name)
	r, err := readReceipt(path)
	if checkErr := b.verify(); checkErr != nil {
		return checkErr
	}
	if os.IsNotExist(err) {
		if receiptInReview(b.dir, name) {
			return ErrReceiptAttributionConflict
		}
		return nil // A concurrent worker already committed and removed this file.
	}
	if err != nil {
		return errors.Join(errReceiptDirectoryChanged, err)
	}
	if receiptInReview(b.dir, name) {
		return quarantineReceipt(b.dir, name)
	}
	if err := t.writeReceipt(ctx, r); err != nil {
		if errors.Is(err, ErrReceiptAttributionConflict) {
			return quarantineReceipt(b.dir, name)
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
	if err := makeReceiptDirectory(filepath.Dir(review)); err != nil {
		return err
	}
	if err := makeReceiptDirectory(review); err != nil {
		return err
	}
	target := filepath.Join(review, name)
	if previous, err := os.Lstat(target); err == nil && !os.SameFile(sourceInfo, previous) {
		// A publisher may have raced the first move. Keep both pieces of evidence
		// and use a stable name in case a crash interrupted this move as well.
		raw, err := os.ReadFile(source)
		if err != nil {
			return err
		}
		review = filepath.Join(review, "duplicate-"+Fingerprint(string(raw)))
		if err := makeReceiptDirectory(review); err != nil {
			return err
		}
		target = filepath.Join(review, name)
	} else if err != nil && !os.IsNotExist(err) {
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
		log.Printf("YZCM usage: receipt_not_durable receipt=%s after provider response; no verified ledger or recovery claim", name)
	case errors.Is(err, errReceiptQuarantineFailed):
		log.Printf("YZCM usage: receipt_quarantine_failed receipt=%s reason=attribution-conflict", name)
	case errors.Is(err, ErrReceiptAttributionConflict):
		log.Printf("YZCM usage: receipt_attribution_conflict receipt=%s", name)
	default:
		log.Printf("YZCM usage: receipt_pending receipt=%s", name)
	}
}

func (t *Tracker) flushReceipts(parent context.Context) {
	if parent.Err() != nil || !t.receiptScanMu.TryLock() {
		return
	}
	defer t.receiptScanMu.Unlock()
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	b, err := t.bindReceiptDirectory(ctx)
	if err != nil {
		t.resetReceiptScan()
		return
	}
	if ctx.Err() != nil {
		return
	}
	if t.receiptScan == nil || !os.SameFile(t.receiptScanInfo, b.info) {
		t.resetReceiptScan()
		f, err := os.Open(b.dir)
		if err != nil {
			return
		}
		info, err := f.Stat()
		if err != nil || !os.SameFile(info, b.info) {
			_ = f.Close()
			return
		}
		t.receiptScan, t.receiptScanInfo = f, info
	}
	// Retain the directory stream across passes, so even failures early in the
	// listing cannot monopolize recovery. Reopen only after EOF or replacement.
	quarantined, failed := 0, 0
	for i := 0; i < 100 && ctx.Err() == nil; i++ {
		entry := t.receiptScanNext
		t.receiptScanNext = nil
		if entry == nil {
			batch, err := t.receiptScan.ReadDir(1)
			if err != nil || len(batch) == 0 {
				t.resetReceiptScan()
				break
			}
			entry = batch[0]
		}
		name := entry.Name()
		if !entry.Type().IsRegular() {
			continue
		}
		if strings.HasPrefix(name, ".receipt-") && strings.HasSuffix(name, ".tmp") {
			if stat, err := entry.Info(); err == nil && time.Since(stat.ModTime()) > time.Hour {
				_ = os.Remove(filepath.Join(b.dir, name))
			}
			continue
		}
		if !strings.HasPrefix(name, "receipt-") || !strings.HasSuffix(name, ".json") || len(name) != len("receipt-")+64+len(".json") {
			continue
		}
		itemCtx, cancelItem := context.WithTimeout(ctx, receiptWriteTimeout)
		err := t.applyReceipt(itemCtx, b, name)
		cancelItem()
		if ctx.Err() != nil {
			// Give a receipt cut short by the batch deadline a full next attempt.
			t.receiptScanNext = entry
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

func (t *Tracker) resetReceiptScan() {
	if t.receiptScan != nil {
		_ = t.receiptScan.Close()
	}
	t.receiptScan, t.receiptScanInfo, t.receiptScanNext = nil, nil, nil
}

func (t *Tracker) closeReceiptScan() {
	t.receiptScanMu.Lock()
	defer t.receiptScanMu.Unlock()
	t.resetReceiptScan()
}
