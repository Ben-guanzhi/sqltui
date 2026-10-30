package db

import (
	"context"
	"sync"
)

// CancelRegistry tracks the cancel handle of the statement in flight. The
// Run paths are fire-and-forget from tea.Cmd closures (no ctx travels with
// the call), so a backend registers each statement's handle here and
// CancelRunning stops the most recent one.
//
// CancelFunc itself is not comparable, so a seq tags the registration: a
// statement finishing after its successor only clears its own registration.
type CancelRegistry struct {
	mu     sync.Mutex
	cancel context.CancelFunc
	seq    uint64
}

// Track registers cancel and returns the clear func. A new Track overwrites
// the previous registration; the returned clear only unregisters its own.
// Calling clear (or CancelRunning) cancels the context.
func (r *CancelRegistry) Track(cancel context.CancelFunc) (clear func()) {
	r.mu.Lock()
	r.seq++
	seq := r.seq
	r.cancel = cancel
	r.mu.Unlock()
	return func() {
		r.mu.Lock()
		if r.seq == seq {
			r.cancel = nil
		}
		r.mu.Unlock()
		cancel()
	}
}

// CancelRunning stops the most recent registration. False means nothing was
// in flight — the UI uses that to decide between "canceled" and quit.
func (r *CancelRegistry) CancelRunning() bool {
	r.mu.Lock()
	cancel := r.cancel
	r.cancel = nil
	r.mu.Unlock()
	if cancel == nil {
		return false
	}
	cancel()
	return true
}
