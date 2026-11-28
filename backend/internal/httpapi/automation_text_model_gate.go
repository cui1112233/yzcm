package httpapi

import (
	"context"
	"sync"

	"qiantie/backend/internal/batchfactoryv11"
)

// automationTextModelGate lets the first text-model operation in a batch act
// as a probe. Other books wait for that probe instead of all hitting the same
// broken model at once. After the probe succeeds, normal concurrency resumes.
// A definite model_not_found opens the circuit for the whole batch and cancels
// text work that is already waiting or still in flight.
type automationTextModelGate struct {
	mu           sync.Mutex
	probeDone    chan struct{}
	probeStarted bool
	fatal        error
	cancel       context.CancelCauseFunc
}

func newAutomationTextModelGate(cancel context.CancelCauseFunc) *automationTextModelGate {
	return &automationTextModelGate{probeDone: make(chan struct{}), cancel: cancel}
}

func (g *automationTextModelGate) Do(ctx context.Context, fn func() error) error {
	g.mu.Lock()
	if g.fatal != nil {
		err := g.fatal
		g.mu.Unlock()
		return err
	}
	isProbe := !g.probeStarted
	if isProbe {
		g.probeStarted = true
	}
	probeDone := g.probeDone
	g.mu.Unlock()

	if !isProbe {
		select {
		case <-probeDone:
		case <-ctx.Done():
			g.mu.Lock()
			fatal := g.fatal
			g.mu.Unlock()
			if fatal != nil {
				return fatal
			}
			return context.Cause(ctx)
		}
		g.mu.Lock()
		fatal := g.fatal
		g.mu.Unlock()
		if fatal != nil {
			return fatal
		}
	}

	err := fn()
	var cancelCause error
	g.mu.Lock()
	if batchfactoryv11.IsDirectorModelUnavailable(err) && g.fatal == nil {
		g.fatal = err
		cancelCause = err
	}
	if isProbe {
		close(g.probeDone)
	}
	g.mu.Unlock()
	if cancelCause != nil && g.cancel != nil {
		g.cancel(cancelCause)
	}
	return err
}
