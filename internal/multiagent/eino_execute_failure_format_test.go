package multiagent

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"cyberstrike-ai/internal/einomcp"
	"cyberstrike-ai/internal/security"

	"github.com/cloudwego/eino/adk/filesystem"
	"github.com/cloudwego/eino/schema"
)

type mockStreamingShellExitFail struct {
	output string
	code   int
}

func (m *mockStreamingShellExitFail) ExecuteStreaming(ctx context.Context, input *filesystem.ExecuteRequest) (*schema.StreamReader[*filesystem.ExecuteResponse], error) {
	outR, outW := schema.Pipe[*filesystem.ExecuteResponse](4)
	go func() {
		defer outW.Close()
		if m.output != "" {
			_ = outW.Send(&filesystem.ExecuteResponse{Output: m.output}, nil)
		}
		code := m.code
		_ = outW.Send(&filesystem.ExecuteResponse{ExitCode: &code}, nil)
	}()
	return outR, nil
}

func TestEinoStreamingShellWrap_CommandFailureFormat(t *testing.T) {
	inner := &mockStreamingShellExitFail{
		output: "sudo: a password is required\n",
		code:   1,
	}
	notify := einomcp.NewToolInvokeNotifyHolder()
	var firedBody string
	var firedSuccess bool
	var firedErr error
	notify.Set(func(toolCallID, toolName, einoAgent string, success bool, content string, invokeErr error) {
		firedBody = content
		firedSuccess = success
		firedErr = invokeErr
	})
	wrap := &einoStreamingShellWrap{inner: inner, invokeNotify: notify}
	sr, err := wrap.ExecuteStreaming(context.Background(), &filesystem.ExecuteRequest{Command: "sudo whoami"})
	if err != nil {
		t.Fatalf("ExecuteStreaming: %v", err)
	}
	defer sr.Close()

	var stream strings.Builder
	for {
		resp, rerr := sr.Recv()
		if errors.Is(rerr, io.EOF) {
			break
		}
		if rerr != nil {
			t.Fatalf("recv: %v", rerr)
		}
		if resp != nil {
			stream.WriteString(resp.Output)
		}
	}

	if firedSuccess {
		t.Fatal("expected success=false")
	}
	var exitErr *ExecuteExitError
	if !errors.As(firedErr, &exitErr) || exitErr.Code != 1 {
		t.Fatalf("expected ExecuteExitError code 1, got %v", firedErr)
	}
	if !strings.HasPrefix(firedBody, einomcp.ToolErrorPrefix) {
		t.Fatalf("missing tool error prefix: %q", firedBody)
	}
	body := strings.TrimPrefix(firedBody, einomcp.ToolErrorPrefix)
	if body != security.FormatCommandFailureResult(1, "sudo: a password is required\n") {
		t.Fatalf("fire body = %q", body)
	}
	if !strings.Contains(stream.String(), "sudo:") {
		t.Fatalf("stream missing sudo output: %q", stream.String())
	}
	if strings.Contains(stream.String(), "command exited with non-zero") {
		t.Fatalf("stream has legacy noise: %q", stream.String())
	}
	if strings.Contains(stream.String(), "abnormal execution termination") {
		t.Fatalf("stream has abnormal tail: %q", stream.String())
	}
	if !security.IsCommandFailureResult(stream.String()) {
		t.Fatalf("stream missing failure status line: %q", stream.String())
	}
	if tail := friendlyEinoExecuteInvokeTail(firedErr); tail != "" {
		t.Fatalf("unexpected invoke tail: %q", tail)
	}
	if !einoToolResultIsError("execute", firedBody) {
		t.Fatal("expected isError for execute failure")
	}
}

func TestFriendlyEinoExecuteInvokeTail(t *testing.T) {
	if friendlyEinoExecuteInvokeTail(&ExecuteExitError{Code: 1}) != "" {
		t.Fatal("exit error should not get abnormal tail")
	}
	if !strings.Contains(friendlyEinoExecuteInvokeTail(context.DeadlineExceeded), "Timed out") {
		t.Fatal("deadline should get timeout hint")
	}
	if friendlyEinoExecuteInvokeTail(errors.New("broken pipe")) == "" {
		t.Fatal("unexpected error should get tail")
	}
}

func TestMCPBackgroundWaitResultIsDisplayRunning(t *testing.T) {
	body := `工具已提交到后台执行，但本次等待已到达上限。

execution_id: 3eaaa391-050b-4be1-a870-48a855923cb7
tool: exec
status: running
wait_timeout: 10s
elapsed: 10s

你可以继续推理、改用其他工具，或调用 wait_tool_execution 继续等待该 execution_id；也可以调用 cancel_tool_execution 取消。`
	modelFacing := einomcp.ToolErrorPrefix + body
	if !einoToolResultIsError("exec", modelFacing) {
		t.Fatal("soft wait timeout must remain model-facing tool error")
	}
	if !isMCPBackgroundWaitResult(einoToolResultBody(modelFacing)) {
		t.Fatal("soft wait timeout should display as background running")
	}
	if got := mcpExecutionIDFromWaitResult(einoToolResultBody(modelFacing)); got != "3eaaa391-050b-4be1-a870-48a855923cb7" {
		t.Fatalf("execution id = %q", got)
	}
	if isMCPBackgroundWaitResult("execution_id: abc\nstatus: failed\nerror: boom") {
		t.Fatal("real failures must not display as background running")
	}
	jsonBody := `{
  "execution_id": "e98baefc-72eb-4a7e-9091-9be179a75d71",
  "tool": "exec",
  "status": "running"
}

本次等待已到达 timeout_seconds，上述 execution 仍未完成。可继续等待、取消，或采用其他步骤。`
	if !isMCPBackgroundWaitResult(jsonBody) {
		t.Fatal("json wait_tool_execution timeout should display as background running")
	}
	if got := mcpExecutionIDFromWaitResult(jsonBody); got != "e98baefc-72eb-4a7e-9091-9be179a75d71" {
		t.Fatalf("json execution id = %q", got)
	}
}
