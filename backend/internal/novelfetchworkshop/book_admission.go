package novelfetchworkshop

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// ProcessBookAdmissionQueue serializes the integration decision and its
// document update. The queue's per-book reservation still enforces one active job.
type ProcessBookAdmissionQueue interface {
	WithBookAdmission(ctx context.Context, owner, bookID string, admit func(context.Context) error) error
}

func (q *MemoryQueue) WithBookAdmission(ctx context.Context, _, _ string, admit func(context.Context) error) error {
	q.admissionMu.Lock()
	defer q.admissionMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	return admit(ctx)
}

func (q *RedisQueue) WithBookAdmission(ctx context.Context, owner, bookID string, admit func(context.Context) error) error {
	sum := sha256.Sum256([]byte(owner + "\x00" + bookID))
	key := q.Prefix + ":integration-admission:" + hex.EncodeToString(sum[:])
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	token := hex.EncodeToString(nonce[:])
	waitCtx, stopWaiting := context.WithTimeout(ctx, 3*time.Second)
	defer stopWaiting()
	const acquire = `if redis.call('SET',KEYS[1],ARGV[1],'NX','EX',60) then return 1 end return 0`
	for {
		result, err := q.command(waitCtx, "EVAL", acquire, "1", key, token)
		if err != nil {
			return err
		}
		if fmt.Sprint(result) == "1" {
			break
		}
		timer := time.NewTimer(30 * time.Millisecond)
		select {
		case <-waitCtx.Done():
			timer.Stop()
			return errors.New("book admission is busy")
		case <-timer.C:
		}
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		const release = `if redis.call('GET',KEYS[1])==ARGV[1] then return redis.call('DEL',KEYS[1]) end return 0`
		_, _ = q.command(releaseCtx, "EVAL", release, "1", key, token)
	}()
	// Leave time for the admission to unwind before another caller can take the lock.
	admitCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	return admit(admitCtx)
}
