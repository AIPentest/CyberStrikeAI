package security

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"sort"
	"sync"
	"time"

	"cyberstrike-ai/internal/processguard"
	"github.com/google/uuid"
)

var ErrProcessScopeClosed = errors.New("task is ending; new processes are not allowed")
var ErrBackgroundNeedsTask = errors.New("background commands require a managed task")

// ErrCleanupUnconfirmed means SIGKILL is pending on processes blocked in the kernel.
var ErrCleanupUnconfirmed = errors.New("local cleanup unconfirmed: processes in uninterruptible sleep (D) with SIGKILL pending")

func cleanupDuration(name string) time.Duration {
	if value, err := time.ParseDuration(os.Getenv(name)); err == nil && value > 0 {
		return value
	}
	return 3 * time.Second
}

type processScopeKey struct{}

// ProcessScope owns local commands for one task run, including background work.
// Ownership is carried by context values, so MCP's WithoutCancel retains it.
// Start and Seal serialize under the same lock: no process can escape cleanup
// by starting between the final snapshot and task completion.
type ProcessScope struct {
	ID            string
	mu            sync.Mutex
	closed        bool
	sessions      map[*ShellSession]struct{}
	guard         processguard.Group
	guardErr      error
	closeMu       sync.Mutex
	blockedGroups []int // protected by closeMu
	graceTimeout  time.Duration
	killTimeout   time.Duration
	guardTimeout  time.Duration
}

func NewProcessScope() *ProcessScope {
	return &ProcessScope{
		ID:           uuid.NewString(),
		sessions:     make(map[*ShellSession]struct{}),
		graceTimeout: cleanupDuration("CS_CLEANUP_GRACE"),
		killTimeout:  cleanupDuration("CS_CLEANUP_KILL"),
		guardTimeout: cleanupDuration("CS_CLEANUP_GUARD"),
	}
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
		return s.cleanupResult(s.closeGuard())
	}
	for _, session := range sessions {
		session.signal(false)
	}
	if waitShellSessions(sessions, s.graceTimeout) {
		return s.cleanupResult(s.closeGuard())
	}
	for _, session := range sessions {
		session.Terminate()
	}
	guardErr := s.closeGuard()
	waitShellSessions(sessions, s.killTimeout)

	// Only the watchdog backend is covered by the process group scan. A
	// cgroup or Job Object can contain members outside the original group.
	watchdogTimedOut := errors.Is(guardErr, context.DeadlineExceeded) &&
		s.IsolationBackend() == "process_group_watchdog"
	var remaining []int
	var releaseErr error
	for _, session := range sessions {
		if session.tryComplete() {
			continue
		}
		if watchdogTimedOut {
			deferred, err := session.deferKernelCleanup()
			if deferred {
				s.blockedGroups = append(s.blockedGroups, session.rootPID)
				slog.Warn("Task process cleanup unconfirmed", "runId", s.ID, "pgid", session.rootPID)
				continue
			}
			releaseErr = errors.Join(releaseErr, err)
		}
		remaining = append(remaining, session.rootPID)
	}
	if len(remaining) == 0 {
		if watchdogTimedOut {
			guardErr = s.closeGuard()
		}
		return s.cleanupResult(guardErr)
	}
	sort.Ints(remaining)
	return errors.Join(fmt.Errorf("task %s: process cleanup timed out (process groups %v)", s.ID, remaining), guardErr, releaseErr)
}

func (s *ProcessScope) cleanupResult(guardErr error) error {
	if guardErr != nil || len(s.blockedGroups) == 0 {
		return guardErr
	}
	sort.Ints(s.blockedGroups)
	return fmt.Errorf("task %s: %w (process groups %v)", s.ID, ErrCleanupUnconfirmed, s.blockedGroups)
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
	ctx, cancel := context.WithTimeout(context.Background(), s.guardTimeout)
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
