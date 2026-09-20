package security

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/mcp"

	"go.uber.org/zap"
)

// setupTestExecutor 创建测试用的执行器
func setupTestExecutor(t *testing.T) (*Executor, *mcp.Server) {
	logger := zap.NewNop()
	mcpServer := mcp.NewServer(logger)

	cfg := &config.SecurityConfig{
		Tools: []config.ToolConfig{},
	}

	executor := NewExecutor(cfg, mcpServer, logger)
	return executor, mcpServer
}

func TestExecutor_ExecuteInternalTool_UnknownTool(t *testing.T) {
	executor, _ := setupTestExecutor(t)

	ctx := context.Background()
	args := map[string]interface{}{
		"test": "value",
	}

	// 测试未知的内部工具类型
	toolResult, err := executor.executeInternalTool(ctx, "unknown_tool", "internal:unknown_tool", args)
	if err != nil {
		t.Fatalf("执行内部工具失败: %v", err)
	}

	if !toolResult.IsError {
		t.Fatal("未知的工具类型应该返回错误")
	}

	if !strings.Contains(toolResult.Content[0].Text, "未知的内部工具类型") {
		t.Errorf("错误消息应该包含'未知的内部工具类型'")
	}
}

func TestExecuteSystemCommand_BackgroundDoesNotBlockOnChildStdout(t *testing.T) {
	executor, _ := setupTestExecutor(t)
	// 子进程先向 stdout 写无换行字符再长时间 sleep；若与 echo $pid 共享管道且未重定向子进程 stdout，
	// ReadString('\n') 会阻塞到子进程退出。后台包装须将子进程标准流与 PID 行分离。
	scope := NewProcessScope()
	t.Cleanup(func() {
		if err := scope.Close(); err != nil {
			t.Error(err)
		}
	})
	ctx, cancel := context.WithTimeout(WithProcessScope(context.Background(), scope), 4*time.Second)
	defer cancel()
	args := map[string]interface{}{
		"command": `(sh -c 'printf x; sleep 120') &`,
		"shell":   "sh",
	}
	res, err := executor.executeSystemCommand(ctx, args)
	if err != nil {
		t.Fatalf("executeSystemCommand: %v", err)
	}
	if res == nil || res.IsError {
		t.Fatalf("expected success, got %+v", res)
	}
	txt := res.Content[0].Text
	if !strings.Contains(txt, "后台命令已启动") {
		t.Fatalf("unexpected body: %q", txt)
	}
}

func TestExecToolSoftWaitExposesPartialOutput(t *testing.T) {
	executor, server := setupTestExecutor(t)
	server.ConfigureToolWaitTimeoutSeconds(1)
	mcp.RegisterExecutionControlTools(server, nil)
	server.RegisterTool(mcp.Tool{Name: "exec", InputSchema: map[string]interface{}{"type": "object"}}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		return executor.ExecuteTool(ctx, "exec", args)
	})

	result, executionID, err := server.CallTool(context.Background(), "exec", map[string]interface{}{
		"command": "for i in 1 2 3 4; do echo partial-$i; sleep 0.3; done; sleep 5",
		"shell":   "sh",
	})
	if err != nil {
		t.Fatalf("CallTool exec: %v", err)
	}
	if executionID == "" || result == nil || !result.IsError {
		t.Fatalf("expected soft wait timeout, id=%q result=%#v", executionID, result)
	}

	status, _, err := server.CallTool(context.Background(), "get_tool_execution", map[string]interface{}{
		"execution_id":             executionID,
		"include_partial_output":   true,
		"partial_output_max_bytes": 4096,
	})
	if err != nil {
		t.Fatalf("get_tool_execution: %v", err)
	}
	body := mcp.ToolResultPlainText(status)
	if !strings.Contains(body, `"status": "running"`) {
		t.Fatalf("expected running execution, got: %s", body)
	}
	if !strings.Contains(body, "partial-") || !strings.Contains(body, "partial_output") {
		t.Fatalf("expected partial output in execution status, got: %s", body)
	}
	server.CancelToolExecution(executionID)
}

func TestExecuteSystemCommand_FailureFormat(t *testing.T) {
	executor, _ := setupTestExecutor(t)
	res, err := executor.executeSystemCommand(context.Background(), map[string]interface{}{
		"command": "echo fail-msg >&2; exit 7",
		"shell":   "sh",
	})
	if err != nil {
		t.Fatalf("executeSystemCommand: %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("expected IsError, got %+v", res)
	}
	text := res.Content[0].Text
	if text != FormatCommandFailureResult(7, "fail-msg\n") && text != FormatCommandFailureResult(7, "fail-msg") {
		t.Fatalf("unexpected failure text: %q", text)
	}
	if !strings.Contains(text, "exit status 7") || !strings.Contains(text, "fail-msg") {
		t.Fatalf("unexpected failure text: %q", text)
	}
}

func TestExecuteSystemCommand_OutputIsSourceLimited(t *testing.T) {
	executor, _ := setupTestExecutor(t)
	spillRoot := t.TempDir()
	executor.SetToolOutputMaxBytes(200)
	executor.SetToolOutputSpillRoot(spillRoot)
	ctx := mcp.WithMCPConversationID(context.Background(), "exec-spill")
	res, err := executor.executeSystemCommand(ctx, map[string]interface{}{
		"command": "i=0; while [ $i -lt 2000 ]; do printf 0123456789; i=$((i+1)); done",
		"shell":   "sh",
	})
	if err != nil {
		t.Fatalf("executeSystemCommand: %v", err)
	}
	if res == nil || res.IsError {
		t.Fatalf("expected success, got %+v", res)
	}
	text := res.Content[0].Text
	if !strings.Contains(text, "<persisted-output>") || !strings.Contains(text, "Full output saved to:") {
		t.Fatalf("missing persisted-output notice: %q", text)
	}
	if len(text) > 200 {
		t.Fatalf("output exceeded hard limit: len=%d text=%q", len(text), text)
	}
	if strings.Contains(text, strings.Repeat("0123456789", 20)) {
		t.Fatalf("output kept too much data: len=%d", len(text))
	}
}

func TestExecuteSystemCommand_StreamingOutputIsSourceLimited(t *testing.T) {
	executor, _ := setupTestExecutor(t)
	spillRoot := t.TempDir()
	executor.SetToolOutputMaxBytes(200)
	executor.SetToolOutputSpillRoot(spillRoot)
	var streamed strings.Builder
	ctx := context.WithValue(context.Background(), ToolOutputCallbackCtxKey, ToolOutputCallback(func(chunk string) {
		streamed.WriteString(chunk)
	}))
	ctx = mcp.WithMCPConversationID(ctx, "exec-stream-spill")
	res, err := executor.executeSystemCommand(ctx, map[string]interface{}{
		"command": "i=0; while [ $i -lt 2000 ]; do printf abcdefghij; i=$((i+1)); done",
		"shell":   "sh",
	})
	if err != nil {
		t.Fatalf("executeSystemCommand: %v", err)
	}
	text := res.Content[0].Text
	if !strings.Contains(text, "<persisted-output>") {
		t.Fatalf("missing persisted-output notice: %q", text)
	}
	if len(text) > 200 {
		t.Fatalf("returned output exceeded hard limit: len=%d text=%q", len(text), text)
	}
	// SSE only streams the bounded prefix; final agent-facing body is the spill notice.
	if len(streamed.String()) > 200 {
		t.Fatalf("streamed prefix exceeded hard limit: len=%d", len(streamed.String()))
	}
	if streamed.Len() == 0 {
		t.Fatal("expected some streamed prefix before truncation")
	}
	if strings.Contains(text, strings.Repeat("abcdefghij", 50)) {
		t.Fatalf("returned output kept too much raw data: len=%d", len(text))
	}
}

func TestBuildCommandArgs_NmapSkipsEmptyOptionalFlags(t *testing.T) {
	pos1 := 1
	executor, _ := setupTestExecutor(t)
	toolConfig := &config.ToolConfig{
		Name:    "nmap",
		Command: "nmap",
		Args:    []string{"-sT", "-sV", "-sC"},
		Parameters: []config.ParameterConfig{
			{Name: "target", Type: "string", Required: true, Position: &pos1, Format: "positional"},
			{Name: "ports", Type: "string", Flag: "-p", Format: "flag"},
			{Name: "timing", Type: "string", Template: "-T{value}", Format: "template"},
			{Name: "nse_scripts", Type: "string", Flag: "--script", Format: "flag"},
			{Name: "os_detection", Type: "bool", Flag: "-O", Format: "flag", Default: false},
			{Name: "aggressive", Type: "bool", Flag: "-A", Format: "flag", Default: false},
			{Name: "scan_type", Type: "string", Format: "template", Template: "{value}"},
			{Name: "additional_args", Type: "string", Format: "positional"},
		},
	}

	args := map[string]interface{}{
		"target":          "110.52.223.114",
		"ports":           "21, 22, 80, 443",
		"timing":          "4",
		"nse_scripts":     "",
		"scan_type":       "",
		"os_detection":    false,
		"aggressive":      false,
		"additional_args": "-Pn",
	}

	cmdArgs := executor.buildCommandArgs("nmap", toolConfig, args)
	joined := strings.Join(cmdArgs, " ")

	if strings.Contains(joined, "--script") {
		t.Fatalf("empty nse_scripts must not emit --script, got: %v", cmdArgs)
	}
	if !strings.Contains(joined, "110.52.223.114") {
		t.Fatalf("target missing from args: %v", cmdArgs)
	}
	// target 应出现在 -Pn 之前，避免被误当作 --script 的参数
	pnIdx := indexOf(cmdArgs, "-Pn")
	targetIdx := indexOf(cmdArgs, "110.52.223.114")
	if pnIdx < 0 || targetIdx < 0 || targetIdx >= pnIdx {
		t.Fatalf("expected target before -Pn, got: %v", cmdArgs)
	}
}

func indexOf(slice []string, s string) int {
	for i, v := range slice {
		if v == s {
			return i
		}
	}
	return -1
}

// TestCombinedOutputCancellable_ContextCancelKillsTree 验证 ctx 取消时能在数秒内结束（杀进程组，非挂死）。
func TestCombinedOutputCancellable_ContextCancelKillsTree(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix process group kill")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", "sleep 300")
	ConfigureShellCmdForAgentExecute(cmd)

	done := make(chan error, 1)
	go func() {
		_, err := combinedOutputCancellable(ctx, cmd)
		done <- err
	}()

	time.Sleep(150 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected context cancel error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("combinedOutputCancellable did not return within 5s after context cancel")
	}
}
