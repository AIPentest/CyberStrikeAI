package handler

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"sync"
	"testing"
	"time"

	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/runlease"
	"cyberstrike-ai/internal/security"
)

func TestTaskCleanupWaitsBeforeReleasingConversation(t *testing.T) {
	manager := NewAgentTaskManager()
	task, _ := manager.StartTask("conv", "old", func(error) {})
	entered, release := make(chan struct{}), make(chan struct{})
	manager.SetToolCanceler(func(string) { close(entered); <-release })
	done := make(chan error, 1)
	go func() { done <- manager.FinishTaskRun("conv", task.RunID, "completed") }()
	<-entered
	if status := manager.GetTaskSnapshot("conv").Status; status != "cleaning" {
		t.Errorf("status = %s", status)
	}
	if _, err := manager.StartTask("conv", "new", nil); !errors.Is(err, ErrTaskAlreadyRunning) {
		t.Errorf("new task admitted during cleanup: %v", err)
	}
	ctx := manager.BindProcessScope(context.Background(), "conv", task.RunID)
	if _, err := security.StartShellSessionContext(ctx, exec.Command("unused-command")); !errors.Is(err, security.ErrProcessScopeClosed) {
		t.Errorf("late process admitted: %v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	manager.SetToolCanceler(nil)
	next, err := manager.StartTask("conv", "new", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.FinishTask("conv", "completed")
	if next.RunID == task.RunID {
		t.Fatal("run identity reused")
	}
	_ = manager.FinishTaskRun("conv", task.RunID, "cancelled")
	if manager.GetTaskSnapshot("conv").RunID != next.RunID {
		t.Fatal("old defer removed new task")
	}
	// A delayed worker keeps its original closed scope, even after a new run starts.
	if _, err := security.StartManagedBackground(ctx, "sh", "sleep 300", ""); !errors.Is(err, security.ErrProcessScopeClosed) {
		t.Fatalf("old context borrowed new task: %v", err)
	}
}

func TestTaskFinishAndShutdownReapBackgroundProcesses(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix shell")
	}
	for _, shutdown := range []bool{false, true} {
		name := "finish"
		if shutdown {
			name = "shutdown"
		}
		t.Run(name, func(t *testing.T) {
			manager := NewAgentTaskManager()
			task, _ := manager.StartTask("conv", "job", nil)
			ctx := manager.BindProcessScope(context.Background(), "conv", task.RunID)
			session, err := security.StartManagedBackground(ctx, "sh", "sleep 300", "")
			if err != nil {
				t.Fatal(err)
			}
			if shutdown {
				manager.Shutdown()
			} else if err := manager.FinishTaskRun("conv", task.RunID, "completed"); err != nil {
				t.Fatal(err)
			}
			done := make(chan struct{})
			go func() { _ = session.Wait(); close(done) }()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("task ended before process exited")
			}
			if manager.GetTaskSnapshot("conv") != nil {
				t.Fatal("finished task still active")
			}
			if shutdown {
				if _, err := manager.StartTask("new", "job", nil); err == nil {
					t.Fatal("shutdown admitted a new task")
				}
			}
		})
	}
}

func TestTaskDonePublishedAfterCleanup(t *testing.T) {
	manager := NewAgentTaskManager()
	bus := NewTaskEventBus()
	manager.SetTaskEventBus(bus)
	task, _ := manager.StartTask("conv", "job", nil)
	_, events := bus.Subscribe("conv")
	if err := manager.FinishTaskRun("conv", task.RunID, "completed"); err != nil {
		t.Fatal(err)
	}
	if event, ok := <-events; !ok || len(event) == 0 {
		t.Fatal("subscriber closed without done event")
	}
	if _, ok := <-events; ok {
		t.Fatal("subscriber not closed after completion")
	}
}

func TestTaskCleanupFailureRetainsOwnershipAndRetries(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix shell")
	}
	manager := NewAgentTaskManager()
	task, _ := manager.StartTask("conv", "job", nil)
	ctx := manager.BindProcessScope(context.Background(), "conv", task.RunID)
	// Simulate an executor which has not yet reaped its direct child.
	session, err := security.StartShellSessionContext(ctx, exec.Command("sh", "-c", "sleep 300"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Terminate(); _ = session.Wait(); manager.Shutdown() })
	if err := manager.FinishTaskRun("conv", task.RunID, "completed"); err == nil {
		t.Fatal("unreaped process reported as cleaned up")
	}
	snapshot := manager.GetTaskSnapshot("conv")
	if snapshot == nil || snapshot.Status != "cleanup_failed" || snapshot.CleanupError == "" {
		t.Fatalf("missing actionable cleanup state: %+v", snapshot)
	}
	if len(manager.GetCompletedTasks()) != 0 {
		t.Fatal("cleanup failure recorded as completed")
	}
	if _, err := manager.StartTask("conv", "new", nil); !errors.Is(err, ErrTaskAlreadyRunning) {
		t.Fatal("cleanup failure released conversation")
	}
	_ = session.Wait()
	manager.cleanupStuckCancelling()
	if manager.GetTaskSnapshot("conv") != nil {
		t.Fatal("cleanup retry did not finish reaped task")
	}
}

func TestTaskFinishWaitsForCancellationCallbacks(t *testing.T) {
	manager := NewAgentTaskManager()
	task, _ := manager.StartTask("conv", "job", nil)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	manager.SetToolCanceler(func(string) { once.Do(func() { close(entered); <-release }) })
	cancelled := make(chan struct{})
	go func() { _, _ = manager.CancelTask("conv", ErrTaskCancelled); close(cancelled) }()
	<-entered
	finished := make(chan struct{})
	go func() { _ = manager.FinishTaskRun("conv", task.RunID, "cancelled"); close(finished) }()
	select {
	case <-finished:
		t.Error("task finished while old cancellation callbacks could still affect new run")
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	<-cancelled
	<-finished
	manager.Shutdown()
}

func TestTaskWaitsForDetachedMCPWorker(t *testing.T) {
	manager := NewAgentTaskManager()
	defer manager.Shutdown()
	task, _ := manager.StartTask("conv", "job", nil)
	ctx := manager.BindProcessScope(context.Background(), "conv", task.RunID)
	service := mcp.NewExecutionService(nil, nil)
	entered, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	_, err := service.Submit(ctx, mcp.ExecutionRequest{Run: func(ctx context.Context) (*mcp.ToolResult, error) {
		close(entered)
		<-ctx.Done()
		close(cancelled)
		<-release
		return nil, ctx.Err()
	}})
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	done := make(chan error, 1)
	go func() { done <- manager.FinishTaskRun("conv", task.RunID, "completed") }()
	<-cancelled
	if manager.GetTaskSnapshot("conv") == nil {
		t.Error("task released while detached worker still running")
	}
	close(release)
	if err = <-done; err != nil {
		t.Fatal(err)
	}
}

func TestTaskReportsUnconfirmedRemoteCleanup(t *testing.T) {
	manager := NewAgentTaskManager()
	defer manager.Shutdown()
	task, _ := manager.StartTask("conv", "job", nil)
	ctx := manager.BindProcessScope(context.Background(), "conv", task.RunID)
	service := mcp.NewExecutionService(nil, nil)
	entered := make(chan struct{})
	_, err := service.Submit(ctx, mcp.ExecutionRequest{Remote: true, Run: func(ctx context.Context) (*mcp.ToolResult, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}})
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	err = manager.FinishTaskRun("conv", task.RunID, "completed")
	if !errors.Is(err, runlease.ErrUnconfirmed) {
		t.Fatalf("remote cancellation reported as verified: %v", err)
	}
	history := manager.GetCompletedTasks()
	if len(history) != 1 || history[0].Status != "cleanup_unconfirmed" || history[0].CleanupError == "" {
		t.Fatalf("missing retained warning: %+v", history)
	}
}
