package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cyberstrike-ai/internal/toolguard"

	"go.uber.org/zap"
)

func testToolGuard(t *testing.T, enabled bool) *toolguard.Manager {
	t.Helper()
	guard, err := toolguard.NewManager(toolguard.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if err := guard.Update(toolguard.Config{Enabled: enabled, Rules: []toolguard.Rule{{
		ID: "government", Name: "Government sites protection", Enabled: true,
		Pattern: `(?i)[a-z0-9.-]+\.gov(?:\.[a-z0-9.-]+)?`,
		Message: "Identified {match}; attacking government sites is forbidden. Please check the target authorization.",
	}}}); err != nil {
		t.Fatal(err)
	}
	return guard
}

func assertGuardRefusal(t *testing.T, result *ToolResult, err error) {
	t.Helper()
	message := ToolResultPlainText(result)
	if err != nil {
		t.Fatalf("expected structured refusal, got error: %v", err)
	} else if result == nil || !result.IsError || !result.Blocked {
		t.Fatalf("expected tool error result, got %#v", result)
	}
	for _, text := range []string{toolGuardBlockedPrefix, "attacking government sites is forbidden", "agency.gov.cn", "government"} {
		if !strings.Contains(message, text) {
			t.Errorf("refusal %q missing %q", message, text)
		}
	}
}

func TestServerToolGuardBlocksBeforeHandlerAndUpdatesLive(t *testing.T) {
	storage := newInMemoryMonitorStorage()
	server := NewServerWithStorage(zap.NewNop(), storage)
	guard := testToolGuard(t, true)
	server.SetToolGuard(guard)
	var calls, authorized atomic.Int32
	server.SetToolAuthorizer(func(context.Context, string, map[string]interface{}) error {
		authorized.Add(1)
		return nil
	})
	server.RegisterTool(Tool{Name: "scan"}, func(context.Context, map[string]interface{}) (*ToolResult, error) {
		calls.Add(1)
		return &ToolResult{Content: []Content{{Type: "text", Text: "ok"}}}, nil
	})
	args := map[string]interface{}{"command": "scan https://agency.gov.cn"}
	result, executionID, err := server.CallTool(context.Background(), "scan", args)
	assertGuardRefusal(t, result, err)
	if calls.Load() != 0 || authorized.Load() != 1 {
		t.Fatalf("calls=%d authorized=%d, want 0 and 1", calls.Load(), authorized.Load())
	}
	execution, err := storage.GetToolExecution(executionID)
	if err != nil || execution == nil || execution.Status != ToolExecutionStatusBlocked || !strings.Contains(execution.Error, toolGuardBlockedPrefix) {
		t.Fatalf("expected persisted blocked execution, got %#v, err=%v", execution, err)
	}

	result, _, err = server.CallTool(context.Background(), "scan", map[string]interface{}{"target": "example.org"})
	if err != nil || result.IsError || calls.Load() != 1 {
		t.Fatalf("allowed target did not execute: result=%#v calls=%d err=%v", result, calls.Load(), err)
	}
	cfg := guard.Config()
	cfg.Rules[0].Enabled = false
	if err := guard.Update(cfg); err != nil {
		t.Fatal(err)
	}
	result, _, err = server.CallTool(context.Background(), "scan", args)
	if err != nil || result.IsError || calls.Load() != 2 {
		t.Fatalf("disabled rule did not take effect: result=%#v calls=%d err=%v", result, calls.Load(), err)
	}
}

func TestHTTPToolGuardReturnsMCPErrorAndPersistsRefusal(t *testing.T) {
	storage := newInMemoryMonitorStorage()
	server := NewServerWithStorage(zap.NewNop(), storage)
	server.SetToolGuard(testToolGuard(t, true))
	var calls int
	server.RegisterTool(Tool{Name: "scan"}, func(context.Context, map[string]interface{}) (*ToolResult, error) {
		calls++
		return &ToolResult{Content: []Content{{Type: "text", Text: "ok"}}}, nil
	})
	for _, tc := range []struct {
		target  string
		blocked bool
	}{
		{target: "https://agency.gov.cn", blocked: true},
		{target: "https://example.org", blocked: false},
	} {
		body, err := json.Marshal(map[string]interface{}{
			"jsonrpc": "2.0", "id": 1, "method": "tools/call",
			"params": map[string]interface{}{"name": "scan", "arguments": map[string]interface{}{"target": tc.target}},
		})
		if err != nil {
			t.Fatal(err)
		}
		recorder := httptest.NewRecorder()
		server.HandleHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/mcp", strings.NewReader(string(body))))
		var response Message
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if recorder.Code != http.StatusOK || response.Error != nil {
			t.Fatalf("expected MCP tool result, status=%d body=%s", recorder.Code, recorder.Body)
		}
		var result ToolResult
		if err := json.Unmarshal(response.Result, &result); err != nil {
			t.Fatal(err)
		}
		if tc.blocked {
			assertGuardRefusal(t, &result, nil)
			if calls != 0 {
				t.Fatal("HTTP tool handler ran for a blocked target")
			}
			executions, err := storage.LoadToolExecutions()
			if err != nil || len(executions) != 1 || executions[0].Status != ToolExecutionStatusBlocked || !strings.Contains(executions[0].Error, toolGuardBlockedPrefix) {
				t.Fatalf("expected persisted HTTP refusal, got %#v err=%v", executions, err)
			}
		} else if result.IsError || calls != 1 {
			t.Fatalf("allowed HTTP target did not execute: result=%#v calls=%d", result, calls)
		}
	}
}

func TestExternalToolGuardBlocksBeforeClientAndUpdatesLive(t *testing.T) {
	manager := NewExternalMCPManager(zap.NewNop())
	t.Cleanup(manager.StopAll)
	guard := testToolGuard(t, true)
	manager.SetToolGuard(guard)
	client := newBlockingExternalMCPClient("ok")
	close(client.release)
	manager.mu.Lock()
	manager.clients["lab"] = client
	manager.mu.Unlock()
	args := map[string]interface{}{"target": "https://agency.gov.cn"}
	result, executionID, err := manager.CallTool(context.Background(), "lab::slow_tool", args)
	assertGuardRefusal(t, result, err)
	if client.count.Load() != 0 {
		t.Fatal("external client ran for a blocked target")
	}
	execution, ok := manager.GetExecution(executionID)
	if !ok || execution.Status != ToolExecutionStatusBlocked || !strings.Contains(execution.Error, toolGuardBlockedPrefix) {
		t.Fatalf("expected blocked external execution, got %#v", execution)
	}
	cfg := guard.Config()
	cfg.Enabled = false
	if err := guard.Update(cfg); err != nil {
		t.Fatal(err)
	}
	result, _, err = manager.CallTool(context.Background(), "lab::slow_tool", args)
	if err != nil || result.IsError || client.count.Load() != 1 {
		t.Fatalf("disabled guard did not take effect: result=%#v calls=%d err=%v", result, client.count.Load(), err)
	}
}

func TestExternalToolGuardRechecksQueuedCallsWithoutTrippingCircuit(t *testing.T) {
	manager := NewExternalMCPManager(zap.NewNop())
	t.Cleanup(manager.StopAll)
	manager.toolWaitTimeout = 10 * time.Millisecond
	manager.ConfigureResilience(ExternalMCPResilienceConfig{
		MaxConcurrentPerServer: 1, MaxConcurrentTotal: 4,
		CircuitFailureThreshold: 1, CircuitCooldown: time.Minute,
	})
	guard := testToolGuard(t, false)
	manager.SetToolGuard(guard)
	client := newBlockingExternalMCPClient("ok")
	close(client.release)
	manager.mu.Lock()
	manager.clients["lab"] = client
	manager.mu.Unlock()

	// Occupy the provider slot so the call passes its initial policy check and
	// remains queued until a live rule update is applied.
	release, err := manager.acquireExternalMCPCallSlot(context.Background(), "lab")
	if err != nil {
		t.Fatal(err)
	}
	released := false
	t.Cleanup(func() {
		if !released {
			release()
		}
	})
	_, executionID, err := manager.CallTool(context.Background(), "lab::slow_tool", map[string]interface{}{"target": "agency.gov.cn"})
	if err != nil || executionID == "" {
		t.Fatalf("failed to queue external call: id=%q err=%v", executionID, err)
	}
	deadline := time.After(time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for len(manager.globalSemaphore) != 2 {
		select {
		case <-deadline:
			t.Fatal("execution did not reach the provider slot queue")
		case <-ticker.C:
		}
	}
	cfg := guard.Config()
	cfg.Enabled = true
	if err := guard.Update(cfg); err != nil {
		t.Fatal(err)
	}
	release()
	released = true
	snapshot, err := manager.executionService.Wait(context.Background(), executionID, time.Second)
	if err != nil || snapshot == nil || snapshot.Execution == nil || snapshot.Execution.Status != ToolExecutionStatusBlocked {
		t.Fatalf("expected queued execution to be blocked on policy recheck, got %#v err=%v", snapshot, err)
	}
	assertGuardRefusal(t, snapshot.Execution.Result, nil)
	if client.count.Load() != 0 {
		t.Fatal("queued call bypassed the updated guard")
	}
	manager.mu.RLock()
	runtime := manager.serverRuntimes["lab"]
	failures, openUntil := runtime.consecutiveFailures, runtime.circuitOpenUntil
	manager.mu.RUnlock()
	if failures != 0 || !openUntil.IsZero() {
		t.Fatalf("local policy refusal affected provider circuit: failures=%d openUntil=%v", failures, openUntil)
	}
	result, _, err := manager.CallTool(context.Background(), "lab::slow_tool", map[string]interface{}{"target": "example.org"})
	if err != nil || result.IsError || client.count.Load() != 1 {
		t.Fatalf("allowed call failed after policy refusal: result=%#v calls=%d err=%v", result, client.count.Load(), err)
	}
}
