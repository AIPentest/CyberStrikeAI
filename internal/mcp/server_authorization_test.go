package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"cyberstrike-ai/internal/authctx"

	"go.uber.org/zap"
)

func TestToolAuthorizerIsUniversalAndExecutionKeepsOwner(t *testing.T) {
	server := NewServer(zap.NewNop())
	server.RegisterTool(Tool{Name: "echo", InputSchema: map[string]interface{}{"type": "object"}}, func(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
		return &ToolResult{Content: []Content{{Type: "text", Text: "ok"}}}, nil
	})
	server.SetToolAuthorizer(func(ctx context.Context, toolName string, args map[string]interface{}) error {
		if _, ok := authctx.PrincipalFromContext(ctx); !ok {
			return errors.New("principal required")
		}
		return nil
	})
	_, deniedExecutionID, err := server.CallTool(context.Background(), "echo", nil)
	if err == nil {
		t.Fatal("tool call without principal was allowed")
	}
	if deniedExecutionID == "" {
		t.Fatal("denied tool call should still return an execution id")
	}
	deniedExecution, ok := server.GetExecution(deniedExecutionID)
	if !ok || deniedExecution == nil {
		t.Fatalf("missing denied execution %q", deniedExecutionID)
	}
	if deniedExecution.Status != ToolExecutionStatusFailed || !strings.Contains(deniedExecution.Error, "principal required") {
		t.Fatalf("denied execution = %#v, want failed with authorization error", deniedExecution)
	}
	ctx := authctx.WithPrincipal(context.Background(), authctx.NewPrincipal("u1", "user", "assigned", map[string]bool{"mcp:execute": true}))
	_, executionID, err := server.CallTool(ctx, "echo", nil)
	if err != nil {
		t.Fatal(err)
	}
	execution, ok := server.GetExecution(executionID)
	if !ok || execution.OwnerUserID != "u1" {
		t.Fatalf("execution owner = %#v, want u1", execution)
	}
}

func TestServerCallToolBoundedWaitForInternalTool(t *testing.T) {
	server := NewServer(zap.NewNop())
	server.toolWaitTimeout = 10 * time.Millisecond
	release := make(chan struct{})
	started := make(chan struct{})
	server.RegisterTool(Tool{Name: "slow", InputSchema: map[string]interface{}{"type": "object"}}, func(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
		close(started)
		select {
		case <-release:
			return &ToolResult{Content: []Content{{Type: "text", Text: "internal done"}}}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})

	callCtx, callCancel := context.WithCancel(context.Background())
	result, executionID, err := server.CallTool(callCtx, "slow", nil)
	if err != nil {
		t.Fatalf("CallTool returned error: %v", err)
	}
	if executionID == "" || result == nil || !result.IsError {
		t.Fatalf("expected soft timeout with execution id, result=%#v id=%q", result, executionID)
	}
	if text := ToolResultPlainText(result); !strings.Contains(text, executionID) || !strings.Contains(text, "wait_tool_execution") {
		t.Fatalf("timeout result missing execution guidance: %q", text)
	}
	select {
	case <-started:
	default:
		t.Fatal("internal worker did not start")
	}
	callCancel()
	close(release)

	snapshot, err := server.executionService.Wait(context.Background(), executionID, time.Second)
	if err != nil {
		t.Fatalf("wait internal execution: %v", err)
	}
	if snapshot == nil || snapshot.Execution == nil || snapshot.Execution.Status != ToolExecutionStatusCompleted {
		t.Fatalf("snapshot = %#v, want completed", snapshot)
	}
	if got := ToolResultPlainText(snapshot.Execution.Result); got != "internal done" {
		t.Fatalf("result = %q, want internal done", got)
	}
}

func TestWaitToolExecutionWaitsForInternalActiveExecution(t *testing.T) {
	server := NewServer(zap.NewNop())
	server.toolWaitTimeout = 10 * time.Millisecond
	release := make(chan struct{})
	server.RegisterTool(Tool{Name: "slow", InputSchema: map[string]interface{}{"type": "object"}}, func(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
		select {
		case <-release:
			return &ToolResult{Content: []Content{{Type: "text", Text: "wait saw completion"}}}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	RegisterExecutionControlTools(server, nil)

	result, executionID, err := server.CallTool(context.Background(), "slow", nil)
	if err != nil {
		t.Fatalf("CallTool returned error: %v", err)
	}
	if result == nil || !result.IsError || executionID == "" {
		t.Fatalf("expected initial bounded wait timeout, result=%#v id=%q", result, executionID)
	}

	done := make(chan *ToolResult, 1)
	errCh := make(chan error, 1)
	go func() {
		waitResult, _, waitErr := server.CallTool(context.Background(), "wait_tool_execution", map[string]interface{}{
			"execution_id":    executionID,
			"timeout_seconds": 1,
		})
		if waitErr != nil {
			errCh <- waitErr
			return
		}
		done <- waitResult
	}()

	select {
	case <-done:
		t.Fatal("wait_tool_execution returned before target execution completed")
	case err := <-errCh:
		t.Fatalf("wait_tool_execution errored before release: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)

	select {
	case err := <-errCh:
		t.Fatalf("wait_tool_execution returned error: %v", err)
	case waitResult := <-done:
		if waitResult == nil || waitResult.IsError {
			t.Fatalf("expected successful wait result, got %#v", waitResult)
		}
		if body := ToolResultPlainText(waitResult); !strings.Contains(body, "wait saw completion") || !strings.Contains(body, `"status": "completed"`) {
			t.Fatalf("wait result missing completed target: %s", body)
		}
	case <-time.After(time.Second):
		t.Fatal("wait_tool_execution did not return after target completion")
	}
}

func TestWaitToolExecutionTimeoutIsObservationNotFailure(t *testing.T) {
	server := NewServer(zap.NewNop())
	server.toolWaitTimeout = 10 * time.Millisecond
	release := make(chan struct{})
	server.RegisterTool(Tool{Name: "slow_observed", InputSchema: map[string]interface{}{"type": "object"}}, func(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
		<-release
		return &ToolResult{Content: []Content{{Type: "text", Text: "done"}}}, nil
	})
	RegisterExecutionControlTools(server, nil)

	result, executionID, err := server.CallTool(context.Background(), "slow_observed", nil)
	if err != nil {
		t.Fatalf("CallTool returned error: %v", err)
	}
	if result == nil || !result.IsError || executionID == "" {
		t.Fatalf("expected initial bounded wait timeout, result=%#v id=%q", result, executionID)
	}

	waitResult, _, err := server.CallTool(context.Background(), "wait_tool_execution", map[string]interface{}{
		"execution_id":    executionID,
		"timeout_seconds": 0.01,
	})
	if err != nil {
		t.Fatalf("wait_tool_execution returned error: %v", err)
	}
	if waitResult == nil {
		t.Fatal("missing wait result")
	}
	if waitResult.IsError {
		t.Fatalf("wait timeout should be a successful observation, got %#v", waitResult)
	}
	body := ToolResultPlainText(waitResult)
	if !strings.Contains(body, `"status": "running"`) || !strings.Contains(body, "This wait reached timeout_seconds") {
		t.Fatalf("wait timeout body missing running status/guidance: %s", body)
	}
	close(release)
}

func TestGetToolExecutionIncludesBoundedPartialOutput(t *testing.T) {
	server := NewServer(zap.NewNop())
	RegisterExecutionControlTools(server, nil)

	executionID := server.BeginToolExecution(context.Background(), "execute", map[string]interface{}{"command": "demo"})
	if executionID == "" {
		t.Fatal("missing execution id")
	}
	server.AppendToolExecutionPartialOutput(executionID, "first\n")
	server.AppendToolExecutionPartialOutput(executionID, strings.Repeat("x", 32))

	result, _, err := server.CallTool(context.Background(), "get_tool_execution", map[string]interface{}{
		"execution_id":             executionID,
		"partial_output_max_bytes": 8,
	})
	if err != nil {
		t.Fatalf("get_tool_execution: %v", err)
	}
	body := ToolResultPlainText(result)
	if !strings.Contains(body, `"partial_output": "xxxxxxxx"`) {
		t.Fatalf("missing bounded partial output: %s", body)
	}
	if !strings.Contains(body, `"partial_output_bytes": 38`) {
		t.Fatalf("missing partial byte count: %s", body)
	}

	result, _, err = server.CallTool(context.Background(), "get_tool_execution", map[string]interface{}{
		"execution_id":           executionID,
		"include_partial_output": false,
	})
	if err != nil {
		t.Fatalf("get_tool_execution without partial: %v", err)
	}
	if body := ToolResultPlainText(result); strings.Contains(body, "partial_output") {
		t.Fatalf("partial output should be omitted: %s", body)
	}
}
