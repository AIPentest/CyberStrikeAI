package mcp

import (
	"context"
	"cyberstrike-ai/internal/runlease"
	"errors"
	"testing"
	"time"
)

func TestExecutionOwnedAfterContextDetached(t *testing.T) {
	scope := runlease.New()
	parent, cancel := context.WithCancel(runlease.WithScope(context.Background(), scope))
	service := NewExecutionService(nil, nil)
	entered := make(chan struct{})
	handle, err := service.Submit(parent, ExecutionRequest{Run: func(ctx context.Context) (*ToolResult, error) { close(entered); <-ctx.Done(); return nil, ctx.Err() }})
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	cancel()
	snapshot, _ := service.Get(handle.ID)
	if snapshot.Execution.Status != ToolExecutionStatusRunning {
		t.Fatal("caller cancellation ended detached worker")
	}
	scope.Cancel()
	deadline, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if err = scope.Wait(deadline); err != nil {
		t.Fatal(err)
	}
	snapshot, _ = service.Get(handle.ID)
	if snapshot.Execution.Status != ToolExecutionStatusCancelled {
		t.Fatalf("unexpected state: %s", snapshot.Execution.Status)
	}
	if _, err = service.Submit(parent, ExecutionRequest{Run: func(context.Context) (*ToolResult, error) { t.Error("closed task executed tool"); return nil, nil }}); !errors.Is(err, runlease.ErrClosed) {
		t.Fatalf("late submit: %v", err)
	}
}
func TestRemoteCancellationRequiresAcknowledgement(t *testing.T) {
	for _, confirm := range []bool{false, true} {
		name := "unconfirmed"
		if confirm {
			name = "confirmed"
		}
		t.Run(name, func(t *testing.T) {
			scope := runlease.New()
			ctx := runlease.WithScope(context.Background(), scope)
			service := NewExecutionService(nil, nil)
			entered := make(chan struct{})
			req := ExecutionRequest{Remote: true, Run: func(ctx context.Context) (*ToolResult, error) { close(entered); <-ctx.Done(); return nil, ctx.Err() }}
			if confirm {
				req.ConfirmCancellation = func(context.Context) error { return nil }
			}
			handle, err := service.Submit(ctx, req)
			if err != nil {
				t.Fatal(err)
			}
			<-entered
			scope.Cancel()
			deadline, stop := context.WithTimeout(context.Background(), time.Second)
			defer stop()
			err = scope.Wait(deadline)
			snapshot, _ := service.Get(handle.ID)
			if confirm {
				if err != nil || snapshot.Execution.Status != ToolExecutionStatusCancelled {
					t.Fatalf("confirmed: %v %+v", err, snapshot.Execution)
				}
			} else {
				if !errors.Is(err, runlease.ErrUnconfirmed) || snapshot.Execution.Status != ToolExecutionStatusOrphaned {
					t.Fatalf("notification treated as confirmation: %v %+v", err, snapshot.Execution)
				}
			}
		})
	}
}
