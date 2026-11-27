package novelfetchworkshop

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"qiantie/backend/internal/mergeworker"
)

func TestRedisBookAdmissionSerializesAcrossQueuesAndReleasesAfterFailure(t *testing.T) {
	url := os.Getenv("QIANTIE_TEST_INTEGRATION_REDIS_URL")
	if url == "" {
		t.Skip("set QIANTIE_TEST_INTEGRATION_REDIS_URL to run against a test Redis instance")
	}
	prefix := fmt.Sprintf("test:novel-fetch:admission:%d", time.Now().UnixNano())
	client, err := mergeworker.NewRedisStore(url, prefix)
	if err != nil {
		t.Fatal("test Redis URL is invalid")
	}
	first := NewRedisQueueWithDo(prefix, RedisDoFunc(client.Do))
	second := NewRedisQueueWithDo(prefix, RedisDoFunc(client.Do))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	firstDone := make(chan error, 1)
	synthetic := errors.New("synthetic callback error")
	go func() {
		firstDone <- first.WithBookAdmission(ctx, "reader", "one", func(ctx context.Context) error {
			close(entered)
			select {
			case <-release:
				return synthetic
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	select {
	case <-entered:
	case err := <-firstDone:
		t.Fatalf("first acquisition failed: %v", err)
	case <-ctx.Done():
		t.Fatal("first acquisition timed out")
	}
	wait, stop := context.WithTimeout(ctx, 120*time.Millisecond)
	var enteredSameBook bool
	err = second.WithBookAdmission(wait, "reader", "one", func(context.Context) error {
		enteredSameBook = true
		return nil
	})
	stop()
	if err == nil || enteredSameBook {
		t.Fatalf("second instance entered the same book during admission: %t, %v", enteredSameBook, err)
	}
	if err := second.WithBookAdmission(ctx, "reader", "other", func(context.Context) error { return nil }); err != nil {
		t.Fatalf("different books should not share a lock: %v", err)
	}
	close(release)
	if err := <-firstDone; !errors.Is(err, synthetic) {
		t.Fatalf("first callback error = %v", err)
	}
	if err := second.WithBookAdmission(ctx, "reader", "one", func(context.Context) error { return nil }); err != nil {
		t.Fatalf("failed callback held the book lock: %v", err)
	}
}
