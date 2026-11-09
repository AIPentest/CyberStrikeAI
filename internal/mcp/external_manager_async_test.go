package mcp

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
)

type blockingExternalMCPClient struct {
	started chan struct{}
	calls   chan string
	release chan struct{}
	result  *ToolResult
	count   atomic.Int32
}

func newBlockingExternalMCPClient(resultText string) *blockingExternalMCPClient {
	return &blockingExternalMCPClient{
		started: make(chan struct{}),
		calls:   make(chan string, 8),
		release: make(chan struct{}),
		result:  &ToolResult{Content: []Content{{Type: "text", Text: resultText}}},
	}
}

func (c *blockingExternalMCPClient) Initialize(ctx context.Context) error { return nil }
func (c *blockingExternalMCPClient) ListTools(ctx context.Context) ([]Tool, error) {
	return []Tool{{Name: "slow_tool"}}, nil
}
func (c *blockingExternalMCPClient) CallTool(ctx context.Context, name string, args map[string]interface{}) (*ToolResult, error) {
	c.count.Add(1)
	select {
	case c.calls <- name:
	default:
	}
	select {
	case <-c.started:
	default:
		close(c.started)
	}
	select {
	case <-c.release:
		return c.result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (c *blockingExternalMCPClient) Close() error      { return nil }
func (c *blockingExternalMCPClient) IsConnected() bool { return true }
func (c *blockingExternalMCPClient) GetStatus() string { return "connected" }

type failingExternalMCPClient struct{}

func (c *failingExternalMCPClient) Initialize(ctx context.Context) error { return nil }
func (c *failingExternalMCPClient) ListTools(ctx context.Context) ([]Tool, error) {
	return []Tool{{Name: "fail_tool"}}, nil
}
func (c *failingExternalMCPClient) CallTool(ctx context.Context, name string, args map[string]interface{}) (*ToolResult, error) {
	return nil, errors.New("boom")
}
func (c *failingExternalMCPClient) Close() error      { return nil }
func (c *failingExternalMCPClient) IsConnected() bool { return true }
func (c *failingExternalMCPClient) GetStatus() string { return "connected" }

func TestExternalMCPManager_CallToolBoundedWaitThenContinue(t *testing.T) {
	manager := NewExternalMCPManager(zap.NewNop())
	manager.ConfigureToolWaitTimeoutSeconds(1)
	manager.toolWaitTimeout = 10 * time.Millisecond
	client := newBlockingExternalMCPClient("slow result ready")
	manager.mu.Lock()
	manager.clients["lab"] = client
	manager.mu.Unlock()

	callCtx, callCancel := context.WithCancel(context.Background())
	result, executionID, err := manager.CallTool(callCtx, "lab::slow_tool", map[string]interface{}{"target": "example"})
	if err != nil {
		t.Fatalf("CallTool returned error: %v", err)
	}
	if executionID == "" {
		t.Fatal("expected execution id")
	}
	if result == nil || !result.IsError {
		t.Fatalf("expected soft timeout tool result, got %#v", result)
	}
	text := ToolResultPlainText(result)
	if !strings.Contains(text, executionID) || !strings.Contains(text, "wait_tool_execution") {
		t.Fatalf("timeout result should include execution id and wait guidance, got %q", text)
	}

	select {
	case <-client.started:
	default:
		t.Fatal("worker did not start")
	}
	callCancel()
	close(client.release)

	snapshot, err := manager.executionService.Wait(context.Background(), executionID, time.Second)
	if err != nil {
		t.Fatalf("Wait returned error: %v", err)
	}
	if snapshot == nil || snapshot.Execution == nil {
		t.Fatal("expected execution snapshot")
	}
	if snapshot.Execution.Status != ToolExecutionStatusCompleted {
		t.Fatalf("status = %q, want completed", snapshot.Execution.Status)
	}
	if got := ToolResultPlainText(snapshot.Execution.Result); got != "slow result ready" {
		t.Fatalf("result = %q, want slow result ready", got)
	}
}

func TestExecutionControlWaitToolReturnsCompletedResult(t *testing.T) {
	manager := NewExternalMCPManager(zap.NewNop())
	manager.toolWaitTimeout = 10 * time.Millisecond
	client := newBlockingExternalMCPClient("control wait result")
	manager.mu.Lock()
	manager.clients["lab"] = client
	manager.mu.Unlock()

	result, executionID, err := manager.CallTool(context.Background(), "lab::slow_tool", nil)
	if err != nil {
		t.Fatalf("CallTool returned error: %v", err)
	}
	if result == nil || !result.IsError || executionID == "" {
		t.Fatalf("expected soft timeout and execution id, got result=%#v id=%q", result, executionID)
	}

	server := NewServer(zap.NewNop())
	RegisterExecutionControlTools(server, manager)
	close(client.release)

	waitResult, _, err := server.CallTool(context.Background(), "wait_tool_execution", map[string]interface{}{
		"execution_id":    executionID,
		"timeout_seconds": 1,
	})
	if err != nil {
		t.Fatalf("wait_tool_execution returned error: %v", err)
	}
	if waitResult == nil || waitResult.IsError {
		t.Fatalf("expected successful wait result, got %#v", waitResult)
	}
	body := ToolResultPlainText(waitResult)
	if !strings.Contains(body, `"status": "completed"`) || !strings.Contains(body, "control wait result") {
		t.Fatalf("wait result body missing completed status/result: %s", body)
	}
}

func TestExternalMCPManager_PerServerConcurrencyLimitsWorkers(t *testing.T) {
	manager := NewExternalMCPManager(zap.NewNop())
	manager.toolWaitTimeout = 10 * time.Millisecond
	manager.ConfigureResilience(ExternalMCPResilienceConfig{
		MaxConcurrentPerServer:  1,
		MaxConcurrentTotal:      4,
		CircuitFailureThreshold: -1,
		CircuitCooldown:         time.Second,
	})
	client := newBlockingExternalMCPClient("ok")
	manager.mu.Lock()
	manager.clients["lab"] = client
	manager.mu.Unlock()

	done1 := make(chan struct{})
	go func() {
		_, _, _ = manager.CallTool(context.Background(), "lab::slow_tool", nil)
		close(done1)
	}()
	select {
	case <-client.calls:
	case <-time.After(time.Second):
		t.Fatal("first worker did not enter client")
	}

	type callOutcome struct {
		executionID string
		err         error
	}
	done2 := make(chan callOutcome, 1)
	go func() {
		_, executionID, err := manager.CallTool(context.Background(), "lab::slow_tool", nil)
		done2 <- callOutcome{executionID: executionID, err: err}
	}()
	select {
	case <-client.calls:
		t.Fatal("second worker entered client before per-server slot was released")
	case <-time.After(50 * time.Millisecond):
	}
	var second callOutcome
	select {
	case second = <-done2:
	case <-time.After(time.Second):
		t.Fatal("second call did not return after bounded wait")
	}
	if second.err != nil || second.executionID == "" {
		t.Fatalf("second call should return queued execution id after bounded wait, id=%q err=%v", second.executionID, second.err)
	}
	snapshot, err := manager.executionService.Get(second.executionID)
	if err != nil {
		t.Fatalf("Get queued execution: %v", err)
	}
	if snapshot == nil || snapshot.Execution == nil || snapshot.Execution.Status != ToolExecutionStatusQueued {
		t.Fatalf("second execution status = %#v, want queued", snapshot)
	}
	close(client.release)
	select {
	case <-client.calls:
	case <-time.After(time.Second):
		t.Fatal("second worker did not enter client after slot release")
	}
	<-done1
}

func TestExternalMCPManager_CircuitBreakerOpensAfterFailures(t *testing.T) {
	manager := NewExternalMCPManager(zap.NewNop())
	manager.ConfigureResilience(ExternalMCPResilienceConfig{
		MaxConcurrentPerServer:  2,
		MaxConcurrentTotal:      4,
		CircuitFailureThreshold: 1,
		CircuitCooldown:         time.Minute,
	})
	manager.mu.Lock()
	manager.clients["lab"] = &failingExternalMCPClient{}
	manager.mu.Unlock()

	_, _, err := manager.CallTool(context.Background(), "lab::fail_tool", nil)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected first call to fail with client error, got %v", err)
	}
	_, _, err = manager.CallTool(context.Background(), "lab::fail_tool", nil)
	if err == nil || !strings.Contains(err.Error(), "circuit") {
		t.Fatalf("expected circuit breaker rejection, got %v", err)
	}
}
