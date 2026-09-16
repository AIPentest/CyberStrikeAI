// Package runlease binds asynchronous tool workers to a task run even when
// their contexts detach from a per-call timeout or an SSE connection.
package runlease

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

var ErrClosed = errors.New("task is ending; new tool executions are not allowed")
var ErrUnconfirmed = errors.New("remote cancellation is unconfirmed")

// Bound detached worker fan-out independently of OS process limits.
const MaxTaskWorkers = 256

type contextKey struct{}
type Scope struct {
	mu          sync.Mutex
	sealed      bool
	workers     map[string]context.CancelFunc
	unconfirmed map[string]string
	changed     chan struct{}
}

func New() *Scope {
	return &Scope{workers: make(map[string]context.CancelFunc), unconfirmed: make(map[string]string), changed: make(chan struct{})}
}
func WithScope(ctx context.Context, s *Scope) context.Context {
	return context.WithValue(ctx, contextKey{}, s)
}
func FromContext(ctx context.Context) *Scope {
	if ctx == nil {
		return nil
	}
	s, _ := ctx.Value(contextKey{}).(*Scope)
	return s
}
func (s *Scope) notify() { close(s.changed); s.changed = make(chan struct{}) }
func (s *Scope) Register(id string, cancel context.CancelFunc) (func(), error) {
	if s == nil {
		return func() {}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sealed {
		return nil, ErrClosed
	}
	if len(s.workers) >= MaxTaskWorkers {
		return nil, fmt.Errorf("task worker limit reached (%d)", MaxTaskWorkers)
	}
	if _, ok := s.workers[id]; ok {
		return nil, fmt.Errorf("duplicate worker %s", id)
	}
	s.workers[id] = cancel
	var once sync.Once
	return func() { once.Do(func() { s.mu.Lock(); delete(s.workers, id); s.notify(); s.mu.Unlock() }) }, nil
}
func (s *Scope) Seal() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.sealed = true
	s.mu.Unlock()
}
func (s *Scope) Cancel() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.sealed = true
	cs := make([]context.CancelFunc, 0, len(s.workers))
	for _, c := range s.workers {
		cs = append(cs, c)
	}
	s.mu.Unlock()
	for _, c := range cs {
		if c != nil {
			c()
		}
	}
}
func (s *Scope) MarkUnconfirmed(id, message string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.unconfirmed[id] = message
	s.notify()
	s.mu.Unlock()
}
func (s *Scope) Wait(ctx context.Context) error {
	if s == nil {
		return nil
	}
	for {
		s.mu.Lock()
		pending := make([]string, 0, len(s.workers))
		for id := range s.workers {
			pending = append(pending, id)
		}
		uncertain := make([]string, 0, len(s.unconfirmed))
		for id, msg := range s.unconfirmed {
			uncertain = append(uncertain, id+": "+msg)
		}
		changed := s.changed
		s.mu.Unlock()
		if len(pending) == 0 {
			if len(uncertain) > 0 {
				sort.Strings(uncertain)
				return fmt.Errorf("%w: %s", ErrUnconfirmed, strings.Join(uncertain, "; "))
			}
			return nil
		}
		select {
		case <-changed:
		case <-ctx.Done():
			sort.Strings(pending)
			return fmt.Errorf("tool workers still running %v: %w", pending, ctx.Err())
		}
	}
}
