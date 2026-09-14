package security

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"sync"

	"github.com/cloudwego/eino/adk/filesystem"
	"github.com/cloudwego/eino/schema"
)

// ConfigureShellCmdForAgentExecute 与 exec 工具一致：非交互 stdin、pager/TERM 环境、独立进程组。
func ConfigureShellCmdForAgentExecute(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	applyDefaultTerminalEnv(cmd)
	attachNonInteractiveStdin(cmd)
	_ = prepareShellCmdSession(cmd)
}

// TerminateShellCmdTree 尽力终止 shell 及其子进程组（与 exec/execute 超时取消一致）。
func TerminateShellCmdTree(cmd *exec.Cmd) {
	terminateCmdTree(cmd)
}

// TerminateShellCmdSession 使用 Start 时缓存的进程组 ID 终止（shell 已退出时仍有效）。
func TerminateShellCmdSession(session *ShellSession) {
	TerminateShellSession(session)
}

// EinoStreamingShell 为 Eino ADK execute 工具提供流式 shell，行为与 exec 对齐：
// 并发读取 stdout/stderr（定长块，非按行），避免官方 local.ExecuteStreaming 先排空 stdout
// 导致 stderr 错误（如 sudo 密码提示）长时间不可见、UI 一直显示「执行中」。
type EinoStreamingShell struct{}

// NewEinoStreamingShell 创建 execute 流式 shell 实现。
func NewEinoStreamingShell() *EinoStreamingShell {
	return &EinoStreamingShell{}
}

// newShellCommand 按平台选择 shell：Windows 使用 cmd.exe，类 Unix 使用 /bin/sh。
// 修复 Windows 平台 execute 工具硬编码 /bin/sh 导致 exec: "/bin/sh": executable file not found。
func newShellCommand(ctx context.Context, command string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.CommandContext(ctx, "cmd", "/c", command)
	}
	return exec.CommandContext(ctx, "/bin/sh", "-c", command)
}

// ExecuteStreaming 实现 filesystem.StreamingShell。
func (s *EinoStreamingShell) ExecuteStreaming(ctx context.Context, input *filesystem.ExecuteRequest) (*schema.StreamReader[*filesystem.ExecuteResponse], error) {
	if input == nil || input.Command == "" {
		return nil, fmt.Errorf("command is required")
	}

	sr, w := schema.Pipe[*filesystem.ExecuteResponse](100)
	if input.RunInBackendGround {
		go runShellInBackground(ctx, input.Command, w)
		return sr, nil
	}
	go streamShellForeground(ctx, input.Command, w)
	return sr, nil
}

func runShellInBackground(ctx context.Context, command string, w *schema.StreamWriter[*filesystem.ExecuteResponse]) {
	defer w.Close()

	command = prepareShellCommandForPlatform(command)
	cmd := newShellCommand(ctx, command)
	applyDefaultTerminalEnv(cmd)
	attachNonInteractiveStdin(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = w.Send(nil, fmt.Errorf("failed to create stdout pipe: %w", err))
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdout.Close()
		_ = w.Send(nil, fmt.Errorf("failed to create stderr pipe: %w", err))
		return
	}
	session, err := StartShellSession(cmd)
	if err != nil {
		_ = stdout.Close()
		_ = stderr.Close()
		_ = w.Send(nil, fmt.Errorf("failed to start command: %w", err))
		return
	}

	done := make(chan struct{})
	go func() {
		drainShellPipes(stdout, stderr)
		_ = session.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		TerminateShellCmdSession(session)
	}

	exitCode := 0
	_ = w.Send(&filesystem.ExecuteResponse{
		Output:   "command started in background\n",
		ExitCode: &exitCode,
	}, nil)
}

func drainShellPipes(stdout, stderr io.Reader) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(io.Discard, stdout)
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(io.Discard, stderr)
	}()
	wg.Wait()
}

func streamShellForeground(ctx context.Context, command string, w *schema.StreamWriter[*filesystem.ExecuteResponse]) {
	defer w.Close()

	command = prepareShellCommandForPlatform(command)
	cmd := newShellCommand(ctx, command)
	applyDefaultTerminalEnv(cmd)
	attachNonInteractiveStdin(cmd)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		_ = w.Send(nil, fmt.Errorf("failed to create stdout pipe: %w", err))
		return
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		_ = stdoutPipe.Close()
		_ = w.Send(nil, fmt.Errorf("failed to create stderr pipe: %w", err))
		return
	}
	session, err := StartShellSession(cmd)
	if err != nil {
		_ = stdoutPipe.Close()
		_ = stderrPipe.Close()
		_ = w.Send(nil, fmt.Errorf("failed to start command: %w", err))
		return
	}

	stopWatch := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			TerminateShellCmdSession(session)
		case <-stopWatch:
		}
	}()
	defer close(stopWatch)

	chunks := make(chan string, 64)
	var wg sync.WaitGroup
	readFn := func(r io.Reader) {
		defer wg.Done()
		buf := make([]byte, 8192)
		for {
			n, readErr := r.Read(buf)
			if n > 0 {
				chunks <- string(buf[:n])
			}
			if readErr != nil {
				return
			}
		}
	}

	wg.Add(2)
	go readFn(stdoutPipe)
	go readFn(stderrPipe)
	go func() {
		wg.Wait()
		close(chunks)
	}()

	hadOutput := false
	for chunk := range chunks {
		if chunk == "" {
			continue
		}
		hadOutput = true
		if w.Send(&filesystem.ExecuteResponse{Output: chunk}, nil) {
			TerminateShellCmdSession(session)
			return
		}
	}

	waitErr := session.Wait()
	if waitErr == nil {
		exitCode := 0
		_ = w.Send(&filesystem.ExecuteResponse{ExitCode: &exitCode}, nil)
		return
	}

	var exitError *exec.ExitError
	if errors.As(waitErr, &exitError) {
		exitCode := exitError.ExitCode()
		resp := &filesystem.ExecuteResponse{ExitCode: &exitCode}
		if !hadOutput {
			resp.Output = FormatCommandFailureResult(exitCode, "")
		}
		_ = w.Send(resp, nil)
		return
	}
	_ = w.Send(nil, fmt.Errorf("command failed: %w", waitErr))
}

// prepareShellCommandForPlatform 按平台预处理命令：
//   - Windows（cmd.exe）：不注入 sh 专用指令，避免 /dev/null、export 等导致命令失败；
//   - 类 Unix（/bin/sh）：保持原有非交互 + 后台 IO 重定向包装，行为不变。
func prepareShellCommandForPlatform(command string) string {
	if runtime.GOOS == "windows" {
		return command
	}
	return PrepareShellCommandForExecute(command)
}
