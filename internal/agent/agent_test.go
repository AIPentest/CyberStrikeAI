package agent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/mcp/builtin"

	"go.uber.org/zap"
)

// setupTestAgent 创建测试用的Agent
func setupTestAgent(t *testing.T) *Agent {
	logger := zap.NewNop()
	mcpServer := mcp.NewServer(logger)

	openAICfg := &config.OpenAIConfig{
		APIKey:  "test-key",
		BaseURL: "https://api.test.com/v1",
		Model:   "test-model",
	}

	agentCfg := &config.AgentConfig{
		MaxIterations: 10,
	}

	return NewAgent(openAICfg, agentCfg, mcpServer, nil, logger, 10)
}

func TestAgent_NewAgent_DefaultValues(t *testing.T) {
	logger := zap.NewNop()
	mcpServer := mcp.NewServer(logger)

	openAICfg := &config.OpenAIConfig{
		APIKey:  "test-key",
		BaseURL: "https://api.test.com/v1",
		Model:   "test-model",
	}

	// 测试默认配置
	agent := NewAgent(openAICfg, nil, mcpServer, nil, logger, 0)

	if agent.maxIterations != 30 {
		t.Errorf("default iteration count mismatch: want 30, got %d", agent.maxIterations)
	}
}

func TestAgent_NewAgent_CustomConfig(t *testing.T) {
	logger := zap.NewNop()
	mcpServer := mcp.NewServer(logger)

	openAICfg := &config.OpenAIConfig{
		APIKey:  "test-key",
		BaseURL: "https://api.test.com/v1",
		Model:   "test-model",
	}

	agentCfg := &config.AgentConfig{
		MaxIterations: 20,
	}

	agent := NewAgent(openAICfg, agentCfg, mcpServer, nil, logger, 15)

	if agent.maxIterations != 15 {
		t.Errorf("iteration count mismatch: want 15, got %d", agent.maxIterations)
	}
}

func TestBuildToolFailureMessageAuthorizationDenied(t *testing.T) {
	msg := buildToolFailureMessage(
		"list_project_facts",
		"tool authorization denied: no access to project",
		errors.New("tool authorization denied: no access to project"),
	)
	for _, want := range []string{
		"Tool name: list_project_facts",
		"Error detail: tool authorization denied: no access to project",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message missing %q:\n%s", want, msg)
		}
	}
	for _, notWant := range []string{
		"可能的原因",
		"建议",
		"错误类型",
		"工具 \"list_project_facts\" 不存在或未启用",
		"单次执行超时",
	} {
		if strings.Contains(msg, notWant) {
			t.Fatalf("message should not include generic hint %q:\n%s", notWant, msg)
		}
	}
}

func TestBuildToolFailureMessageCanceled(t *testing.T) {
	msg := buildToolFailureMessage(
		"long_running_tool",
		"The tool call was manually terminated (from the MCP monitoring page). The agent will carry this result into the following steps; the whole task will not be stopped because of it.",
		context.Canceled,
	)

	for _, want := range []string{
		"Tool name: long_running_tool",
		"Error detail: The tool call was manually terminated",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message missing %q:\n%s", want, msg)
		}
	}
}

func TestBuildToolFailureMessageDeadlineExceeded(t *testing.T) {
	msg := buildToolFailureMessage(
		"nmap",
		"The tool execution was automatically terminated after exceeding 15 minutes (adjust agent.tool_timeout_minutes in config.yaml)",
		context.DeadlineExceeded,
	)

	for _, want := range []string{
		"Tool name: nmap",
		"Error detail: The tool execution was automatically terminated after exceeding 15 minutes",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message missing %q:\n%s", want, msg)
		}
	}
}

func TestBuildToolFailureMessageUnknownKeepsGenericFallback(t *testing.T) {
	msg := buildToolFailureMessage("custom_tool", "dial tcp: connection reset by peer", errors.New("dial tcp: connection reset by peer"))

	for _, want := range []string{
		"Tool name: custom_tool",
		"Error detail: dial tcp: connection reset by peer",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message missing %q:\n%s", want, msg)
		}
	}
}

func TestAgentCancelRunningMCPToolsForConversation(t *testing.T) {
	ag := setupTestAgent(t)
	ag.mcpServer.ConfigureToolWaitTimeoutSeconds(1)
	ag.mcpServer.RegisterTool(mcp.Tool{Name: "block", InputSchema: map[string]interface{}{"type": "object"}}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})

	ctx1 := mcp.WithMCPConversationID(context.Background(), "conv-1")
	result1, execID1, err := ag.mcpServer.CallTool(ctx1, "block", nil)
	if err != nil {
		t.Fatalf("CallTool conv-1: %v", err)
	}
	if result1 == nil || !result1.IsError || execID1 == "" {
		t.Fatalf("expected bounded wait for conv-1, result=%#v id=%q", result1, execID1)
	}

	ctx2 := mcp.WithMCPConversationID(context.Background(), "conv-2")
	result2, execID2, err := ag.mcpServer.CallTool(ctx2, "block", nil)
	if err != nil {
		t.Fatalf("CallTool conv-2: %v", err)
	}
	if result2 == nil || !result2.IsError || execID2 == "" {
		t.Fatalf("expected bounded wait for conv-2, result=%#v id=%q", result2, execID2)
	}

	if got := ag.CancelRunningMCPToolsForConversation("conv-1", "session ended"); got != 1 {
		t.Fatalf("cancelled count = %d, want 1", got)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		exec1, _ := ag.mcpServer.GetExecution(execID1)
		exec2, _ := ag.mcpServer.GetExecution(execID2)
		if exec1 != nil && exec1.Status == mcp.ToolExecutionStatusCancelled {
			if exec2 == nil || exec2.Status != mcp.ToolExecutionStatusRunning {
				t.Fatalf("conv-2 execution should remain running, got %#v", exec2)
			}
			if !strings.Contains(exec1.Error, "session ended") && (exec1.Result == nil || !strings.Contains(mcp.ToolResultPlainText(exec1.Result), "session ended")) {
				t.Fatalf("cancel note missing from conv-1 execution: %#v", exec1)
			}
			_ = ag.CancelRunningMCPToolsForConversation("conv-2", "")
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("conv-1 execution did not become cancelled")
}

func TestExecuteMCPToolForConversationInjectsConversationID(t *testing.T) {
	ag := setupTestAgent(t)
	gotArgs := make(chan map[string]interface{}, 1)
	ag.mcpServer.RegisterTool(mcp.Tool{Name: builtin.ToolRecordVulnerability, InputSchema: map[string]interface{}{"type": "object"}}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		gotArgs <- args
		return &mcp.ToolResult{Content: []mcp.Content{{Type: "text", Text: "ok"}}}, nil
	})

	result, err := ag.ExecuteMCPToolForConversation(context.Background(), "conv-record", builtin.ToolRecordVulnerability, map[string]interface{}{})
	if err != nil {
		t.Fatalf("ExecuteMCPToolForConversation: %v", err)
	}
	if result == nil || result.IsError {
		t.Fatalf("expected successful result, got %#v", result)
	}

	select {
	case args := <-gotArgs:
		if got := args["conversation_id"]; got != "conv-record" {
			t.Fatalf("conversation_id = %#v, want conv-record", got)
		}
	case <-time.After(time.Second):
		t.Fatal("tool was not called")
	}
}

func TestExecuteMCPToolForConversationBindsExecutionConversation(t *testing.T) {
	ag := setupTestAgent(t)
	ag.mcpServer.ConfigureToolWaitTimeoutSeconds(1)
	release := make(chan struct{})
	ag.mcpServer.RegisterTool(mcp.Tool{Name: "slow-bind", InputSchema: map[string]interface{}{"type": "object"}}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		select {
		case <-release:
			return &mcp.ToolResult{Content: []mcp.Content{{Type: "text", Text: "done"}}}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})

	result, err := ag.ExecuteMCPToolForConversation(context.Background(), "conv-bound", "slow-bind", nil)
	if err != nil {
		t.Fatalf("ExecuteMCPToolForConversation: %v", err)
	}
	if result == nil || !result.IsError || result.ExecutionID == "" {
		t.Fatalf("expected bounded wait result with execution id, result=%#v", result)
	}

	exec, ok := ag.mcpServer.GetExecution(result.ExecutionID)
	if !ok || exec == nil {
		t.Fatalf("missing execution %q", result.ExecutionID)
	}
	if exec.ConversationID != "conv-bound" {
		t.Fatalf("execution conversation = %q, want conv-bound", exec.ConversationID)
	}
	close(release)
}

func TestExecuteMCPToolForConversationConcurrentRecordIsolation(t *testing.T) {
	ag := setupTestAgent(t)
	seen := make(chan string, 2)
	ag.mcpServer.RegisterTool(mcp.Tool{Name: builtin.ToolRecordVulnerability, InputSchema: map[string]interface{}{"type": "object"}}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		if conv, _ := args["conversation_id"].(string); conv != "" {
			seen <- conv
		}
		return &mcp.ToolResult{Content: []mcp.Content{{Type: "text", Text: "ok"}}}, nil
	})

	var wg sync.WaitGroup
	for _, conv := range []string{"conv-a", "conv-b"} {
		conv := conv
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := ag.ExecuteMCPToolForConversation(context.Background(), conv, builtin.ToolRecordVulnerability, map[string]interface{}{}); err != nil {
				t.Errorf("ExecuteMCPToolForConversation %s: %v", conv, err)
			}
		}()
	}
	wg.Wait()
	close(seen)

	got := map[string]int{}
	for conv := range seen {
		got[conv]++
	}
	if got["conv-a"] != 1 || got["conv-b"] != 1 {
		t.Fatalf("conversation ids = %#v, want one call for conv-a and conv-b", got)
	}
}
