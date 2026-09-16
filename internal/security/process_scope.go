package security

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"time"

	"cyberstrike-ai/internal/processguard"
	"github.com/google/uuid"
)

var ErrProcessScopeClosed = errors.New("task is ending; new processes are not allowed")
var ErrBackgroundNeedsTask = errors.New("background commands require a managed task")

type processScopeKey struct{}

// ProcessScope owns local commands for one task run, including background work.
// Ownership is carried by context values, so MCP's WithoutCancel retains it.
// Start and Seal serialize under the same lock: no process can escape cleanup
// by starting between the final snapshot and task completion.
type ProcessScope struct {
	ID       string
	mu       sync.Mutex
	closed   bool
	sessions map[*ShellSession]struct{}
	guard    processguard.Group
	guardErr error
	closeMu  sync.Mutex
}

func NewProcessScope() *ProcessScope {
	return &ProcessScope{ID: uuid.NewString(), sessions: make(map[*ShellSession]struct{})}
}

func WithProcessScope(ctx context.Context, scope *ProcessScope) context.Context {
	return context.WithValue(ctx, processScopeKey{}, scope)
}

func ProcessScopeFromContext(ctx context.Context) *ProcessScope {
	if ctx == nil {
		return nil
	}
	scope, _ := ctx.Value(processScopeKey{}).(*ProcessScope)
	return scope
}

func (s *ProcessScope) Seal() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
}

func startShellSessionContext(ctx context.Context, cmd *exec.Cmd, start func() error) (*ShellSession, error) {
	scope := ProcessScopeFromContext(ctx)
	if scope != nil {
		scope.mu.Lock()
		defer scope.mu.Unlock()
		if scope.closed {
			return nil, ErrProcessScopeClosed
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := prepareShellCmdSession(cmd); err != nil {
		return nil, err
	}

	var launch *processguard.Launch
	if scope != nil {
		if scope.guard == nil && scope.guardErr == nil {
			scope.guard, scope.guardErr = processguard.New(scope.ID)
		}
		if scope.guardErr != nil {
			return nil, scope.guardErr
		}
		var err error
		launch, err = scope.guard.Prepare(cmd)
		if err != nil {
			return nil, err
		}
		defer launch.Dispose()
	}
	// Bound Go's output-copy goroutines when descendants inherit a pipe.
	if cmd.WaitDelay == 0 {
		cmd.WaitDelay = 2 * time.Second
	}
	if err := start(); err != nil {
		return nil, err
	}
	if launch != nil {
		if err := launch.Commit(); err != nil {
			terminateProcessGroup(cmd.Process.Pid, cmd)
			_ = cmd.Wait()
			if scope != nil {
				_ = scope.guard.Release(cmd.Process.Pid)
			}
			return nil, err
		}
	}
	session := &ShellSession{Cmd: cmd, rootPID: cmd.Process.Pid, scope: scope, done: make(chan struct{})}
	if scope != nil {
		scope.sessions[session] = struct{}{}
	}
	return session, nil
}

// Close seals the scope, asks every process group to exit, then escalates to
// SIGKILL. It waits for command reaping, with one shared deadline, not N timeouts.
// Failed entries remain owned, permitting a later Close to retry cleanup.
func (s *ProcessScope) Close() error {
	if s == nil {
		return nil
	}
	s.closeMu.Lock()
	defer s.closeMu.Unlock()
	s.mu.Lock()
	s.closed = true
	sessions := make([]*ShellSession, 0, len(s.sessions))
	for session := range s.sessions {
		sessions = append(sessions, session)
	}
	s.mu.Unlock()
	if len(sessions) == 0 {
		return s.closeGuard()
	}
	for _, session := range sessions {
		session.signal(false)
	}
	if waitShellSessions(sessions, 3*time.Second) {
		return s.closeGuard()
	}
	for _, session := range sessions {
		session.Terminate()
	}
	guardErr := s.closeGuard()
	if waitShellSessions(sessions, 3*time.Second) {
		return guardErr
	}
	remaining := make([]int, 0, len(sessions))
	for _, session := range sessions {
		if !session.tryComplete() {
			remaining = append(remaining, session.rootPID)
		}
	}
	return fmt.Errorf("task %s: process cleanup timed out (process groups %v)", s.ID, remaining)
}

func waitShellSessions(sessions []*ShellSession, timeout time.Duration) bool {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		complete := true
		for _, session := range sessions {
			if !session.tryComplete() {
				complete = false
			}
		}
		if complete {
			return true
		}
		select {
		case <-deadline.C:
			return false
		case <-ticker.C:
		}
	}

}

// StartManagedBackground returns promptly while retaining task ownership. The
// shell executes the job in the foreground internally, keeping a waitable root
// alive; tool completion must not cancel the job's lifetime.
func StartManagedBackground(ctx context.Context, shell, command, dir string) (*ShellSession, error) {
	if ProcessScopeFromContext(ctx) == nil {
		return nil, ErrBackgroundNeedsTask
	}
	cmd := exec.Command(shell, "-c", PrepareShellCommandForExecute(command))
	cmd.Dir = dir
	ConfigureShellCmdForAgentExecute(cmd)
	// Nil output streams use /dev/null; background output cannot hold tool pipes.
	session, err := StartShellSessionContext(ctx, cmd)
	if err != nil {
		return nil, err
	}
	go func() { _ = session.Wait() }()
	return session, nil
}

func (s *ProcessScope) closeGuard() error {
	if s.guard == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return s.guard.Close(ctx)
}

func (s *ProcessScope) IsolationBackend() string {
	if s == nil {
		return "none"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.guard != nil {
		return s.guard.Name()
	}
	if s.guardErr != nil {
		return "unavailable"
	}
	return "pending"
}
