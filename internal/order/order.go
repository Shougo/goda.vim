// Package order orders the handling of the messages of the editor: the
// notifications one after the other, in the order they came, and a request
// once the notifications that came before it are handled. Requests still
// run side by side (a handler may call back into the editor, which may send
// a request meanwhile).
//
// The reader of the messages calls Notification and Request in the order
// they came, and runs what they return in goroutines of their own.
package order

import (
	"context"
	"sync"
)

// Sequencer keeps the order of one stream of messages.
type Sequencer struct {
	mu sync.Mutex
	// tail is closed once the notifications so far are handled.
	tail chan struct{}
}

// New returns a Sequencer.
func New() *Sequencer {
	tail := make(chan struct{})
	close(tail)
	return &Sequencer{tail: tail}
}

// Notification returns f to run after the notifications before it (and
// before those after it). With ctx done (the stream is closing), f does not
// wait.
func (s *Sequencer) Notification(ctx context.Context, f func()) func() {
	s.mu.Lock()
	prev := s.tail
	done := make(chan struct{})
	s.tail = done
	s.mu.Unlock()
	return func() {
		defer close(done)
		wait(ctx, prev)
		f()
	}
}

// Request returns f to run after the notifications before it.
func (s *Sequencer) Request(ctx context.Context, f func()) func() {
	s.mu.Lock()
	prev := s.tail
	s.mu.Unlock()
	return func() {
		wait(ctx, prev)
		f()
	}
}

func wait(ctx context.Context, ch <-chan struct{}) {
	select {
	case <-ch:
	case <-ctx.Done():
	}
}
