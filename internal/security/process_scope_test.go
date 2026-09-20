//go:build !windows

package security

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk/filesystem"
)

func readTestPID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(path)
		if err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("process did not write PID to %s", path)
	return 0
}

func requireProcessGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if syscall.Kill(pid, 0) == syscall.ESRCH {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("process %d survived task cleanup", pid)
}

func TestProcessScope_BackgroundSurvivesToolButEndsWithTask(t *testing.T) {
	executor, _ := setupTestExecutor(t)
	scope := NewProcessScope()
	t.Cleanup(func() { _ = scope.Close() })
	taskCtx := WithProcessScope(context.Background(), scope)
	ctx, cancel := context.WithCancel(context.WithoutCancel(taskCtx))
	defer cancel()
	pidFile := filepath.Join(t.TempDir(), "pid")
	result, err := executor.executeSystemCommand(ctx, map[string]interface{}{
		"command": fmt.Sprintf("echo $$ > %q; sleep 300 &", pidFile),
	})
	if err != nil || result.IsError {
		t.Fatalf("background launch: %v, %+v", err, result)
	}
	pid := readTestPID(t, pidFile)
	cancel() // MCP completes and cancels its per-tool context.
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("tool completion killed task background process: %v", err)
	}
	if err := scope.Close(); err != nil {
		t.Fatal(err)
	}
	requireProcessGone(t, pid)
	if _, err := StartManagedBackground(taskCtx, "sh", "sleep 300", ""); !errors.Is(err, ErrProcessScopeClosed) {
		t.Fatalf("closed task accepted a new process: %v", err)
	}
}

func TestProcessScope_EinoBackgroundReturnsPromptlyAndIsOwned(t *testing.T) {
	for _, useFlag := range []bool{false, true} {
		t.Run(fmt.Sprint(useFlag), func(t *testing.T) {
			scope := NewProcessScope()
			t.Cleanup(func() { _ = scope.Close() })
			ctx := WithProcessScope(context.Background(), scope)
			pidFile := filepath.Join(t.TempDir(), "pid")
			command := fmt.Sprintf("echo $$ > %q; sleep 300", pidFile)
			if !useFlag {
				command += " &"
			}
			stream, err := NewEinoStreamingShell().ExecuteStreaming(ctx, &filesystem.ExecuteRequest{Command: command, RunInBackendGround: useFlag})
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			done := make(chan error, 1)
			go func() {
				for {
					_, err := stream.Recv()
					if err != nil {
						done <- err
						return
					}
				}
			}()
			select {
			case err := <-done:
				if !errors.Is(err, io.EOF) {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("background launch waited for job completion")
			}
			pid := readTestPID(t, pidFile)
			if err := scope.Close(); err != nil {
				t.Fatal(err)
			}
			requireProcessGone(t, pid)
		})
	}
}

func TestProcessScope_ForceKillsIgnoringTERMAndGrandchild(t *testing.T) {
	scope := NewProcessScope()
	t.Cleanup(func() { _ = scope.Close() })
	ctx := WithProcessScope(context.Background(), scope)
	pidFile := filepath.Join(t.TempDir(), "child")
	session, err := StartManagedBackground(ctx, "sh", fmt.Sprintf("trap '' TERM; sleep 300 & echo $! > %q; wait", pidFile), "")
	if err != nil {
		t.Fatal(err)
	}
	childPID := readTestPID(t, pidFile)
	if err := scope.Close(); err != nil {
		t.Fatal(err)
	}
	requireProcessGone(t, session.rootPID)
	requireProcessGone(t, childPID)
	if session.Cmd.ProcessState == nil {
		t.Fatal("root process was not reaped")
	}
}

func TestProcessScope_ConcurrentStartAndClose(t *testing.T) {
	scope := NewProcessScope()
	ctx := WithProcessScope(context.Background(), scope)
	t.Cleanup(func() { _ = scope.Close() })
	var wg sync.WaitGroup
	var mu sync.Mutex
	var sessions []*ShellSession
	begin := make(chan struct{})
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-begin
			session, err := StartManagedBackground(ctx, "sh", "sleep 300", "")
			if err != nil {
				if !errors.Is(err, ErrProcessScopeClosed) {
					t.Errorf("start: %v", err)
				}
				return
			}
			mu.Lock()
			sessions = append(sessions, session)
			mu.Unlock()
		}()
	}
	close(begin)
	if err := scope.Close(); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	for _, session := range sessions {
		requireProcessGone(t, session.rootPID)
	}
}

func TestProcessScope_UnmanagedBackgroundRejected(t *testing.T) {
	if _, err := StartManagedBackground(context.Background(), "sh", "sleep 300", ""); !errors.Is(err, ErrBackgroundNeedsTask) {
		t.Fatal(err)
	}
}

func TestProcessScope_ForegroundExitKillsLeftoverChild(t *testing.T) {
	scope := NewProcessScope()
	t.Cleanup(func() { _ = scope.Close() })
	ctx := WithProcessScope(context.Background(), scope)
	pidFile := filepath.Join(t.TempDir(), "child")
	// A shell that exits with a redirected child must not lose that child.
	cmd := exec.CommandContext(ctx, "sh", "-c", fmt.Sprintf("sleep 300 </dev/null >/dev/null 2>&1 & echo $! > %q", pidFile))
	if _, err := combinedOutputCancellable(ctx, cmd); err != nil {
		t.Fatal(err)
	}
	pid := readTestPID(t, pidFile)
	if err := scope.Close(); err != nil {
		t.Fatal(err)
	}
	requireProcessGone(t, pid)
}
