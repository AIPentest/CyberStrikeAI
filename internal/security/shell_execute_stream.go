package security

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"

	"github.com/cloudwego/eino/adk/filesystem"
	"github.com/cloudwego/eino/schema"
)

// ConfigureShellCmdForAgentExecute Consistent with the exec tool: non-interactive stdin, pager/TERM environment, independent process group.
func ConfigureShellCmdForAgentExecute(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	applyDefaultTerminalEnv(cmd)
	attachNonInteractiveStdin(cmd)
	_ = prepareShellCmdSession(cmd)
}

// TerminateShellCmdTree Best-effort terminate the shell and its child process group (consistent with exec/execute timeout cancellation).
func TerminateShellCmdTree(cmd *exec.Cmd) {
	terminateCmdTree(cmd)
}

// TerminateShellCmdSession Terminate using the process group ID cached at Start (still valid after the shell has exited).
func TerminateShellCmdSession(session *ShellSession) {
	TerminateShellSession(session)
}

// EinoStreamingShell Provide a streaming shell for the Eino ADK execute tool, with behavior aligned to exec:
// Read stdout/stderr concurrently (fixed-length blocks, not line by line) to avoid the official local.ExecuteStreaming draining first stdout
// causing stderr errors (such as a sudo password prompt) to stay invisible for a long time while the UI keeps showing running'.
type EinoStreamingShell struct{}

// NewEinoStreamingShell Create the execute streaming shell implementation.
func NewEinoStreamingShell() *EinoStreamingShell {
	return &EinoStreamingShell{}
}

// ExecuteStreaming implementation filesystem.StreamingShell.
func (s *EinoStreamingShell) ExecuteStreaming(ctx context.Context, input *filesystem.ExecuteRequest) (*schema.StreamReader[*filesystem.ExecuteResponse], error) {
	if input == nil || input.Command == "" {
		return nil, fmt.Errorf("command is required")
	}

	sr, w := schema.Pipe[*filesystem.ExecuteResponse](100)
	if input.RunInBackendGround || IsBackgroundShellCommand(input.Command) {
		go runShellInBackground(ctx, input.Command, w)
		return sr, nil
	}
	go streamShellForeground(ctx, input.Command, w)
	return sr, nil
}

func runShellInBackground(ctx context.Context, command string, w *schema.StreamWriter[*filesystem.ExecuteResponse]) {
	defer w.Close()

	command = strings.TrimSpace(command)
	if IsBackgroundShellCommand(command) {
		command = strings.TrimSpace(strings.TrimSuffix(command, "&"))
	}
	session, err := StartManagedBackground(ctx, "/bin/sh", command, "")
	if err != nil {
		_ = w.Send(nil, err)
		return
	}
	exitCode := 0
	_ = w.Send(&filesystem.ExecuteResponse{
		Output:   fmt.Sprintf("command started in background (process group %d); cleaned up when this task ends\n", session.rootPID),
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

	command = PrepareShellCommandForExecute(command)
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command)
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
	session, err := StartShellSessionContext(ctx, cmd)
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

	readStop := make(chan struct{})
	defer close(readStop)
	chunks := make(chan string, 64)
	var wg sync.WaitGroup
	readFn := func(r io.Reader) {
		defer wg.Done()
		buf := make([]byte, 8192)
		for {
			n, readErr := r.Read(buf)
			if n > 0 {
				select {
				case chunks <- string(buf[:n]):
				case <-readStop:
					return
				}
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
			go func() { _ = session.Wait() }()
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
