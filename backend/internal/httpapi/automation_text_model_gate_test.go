package httpapi

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"qiantie/backend/internal/batchfactoryv11"
)

func TestAutomationTextModelGateStopsSiblingCallsAfterModelNotFound(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	gate := newAutomationTextModelGate(cancel)

	started := make(chan struct{})
	release := make(chan struct{})
	modelErr := &batchfactoryv11.DirectorHTTPError{
		StatusCode: http.StatusServiceUnavailable,
		Model:      "missing-model",
		Body:       `{"error":{"code":"model_not_found","message":"missing"}}`,
	}

	var calls atomic.Int32
	var firstErr error
	var secondErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		firstErr = gate.Do(ctx, func() error {
			calls.Add(1)
			close(started)
			<-release
			return modelErr
		})
	}()
	<-started
	go func() {
		defer wg.Done()
		secondErr = gate.Do(ctx, func() error {
			calls.Add(1)
			return nil
		})
	}()

	close(release)
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("text model gate did not release waiting books")
	}

	if calls.Load() != 1 {
		t.Fatalf("provider calls = %d, want 1", calls.Load())
	}
	if firstErr != modelErr || secondErr != modelErr {
		t.Fatalf("errors = (%v, %v), want shared model_not_found", firstErr, secondErr)
	}
	if context.Cause(ctx) != modelErr {
		t.Fatalf("cancel cause = %v, want model_not_found", context.Cause(ctx))
	}
}

func TestAutomationTextModelGateReleasesConcurrencyAfterHealthyProbe(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	gate := newAutomationTextModelGate(cancel)

	var calls atomic.Int32
	if err := gate.Do(ctx, func() error { calls.Add(1); return nil }); err != nil {
		t.Fatalf("probe failed: %v", err)
	}
	if err := gate.Do(ctx, func() error { calls.Add(1); return nil }); err != nil {
		t.Fatalf("follow-up failed: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("provider calls = %d, want 2", calls.Load())
	}
}
