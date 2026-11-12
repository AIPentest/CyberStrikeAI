package security

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// FormatCommandFailureResult Consistent with the exec tool's ToolResult text (without ToolErrorPrefix).
func FormatCommandFailureResult(exitCode int, output string) string {
	output = strings.TrimSpace(output)
	errMsg := fmt.Sprintf("exit status %d", exitCode)
	if output == "" {
		return fmt.Sprintf("Command execution failed: %s", errMsg)
	}
	if strings.HasPrefix(output, "Command execution failed:") {
		return output
	}
	return fmt.Sprintf("Command execution failed: %s\n Output: %s", errMsg, output)
}

// FormatCommandFailureFromErr Generate a unified failure message from the error returned by exec/execute (IsError body).
func FormatCommandFailureFromErr(err error, output string) string {
	if err == nil {
		return strings.TrimSpace(output)
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return FormatCommandFailureResult(exitError.ExitCode(), output)
	}
	output = strings.TrimSpace(output)
	if output == "" {
		return fmt.Sprintf("Command execution failed: %v", err)
	}
	if strings.HasPrefix(output, "Command execution failed:") {
		return output
	}
	return fmt.Sprintf("Command execution failed: %v\n Output: %s", err, output)
}

// ExecuteFailureStatusLine A single-line status appended when streaming execute finishes (the output body has already been pushed in the stream).
func ExecuteFailureStatusLine(exitCode int) string {
	return fmt.Sprintf("\n Command execution failed: exit status %d", exitCode)
}

// IsCommandFailureResult Determine whether the tool result body indicates a non-zero command exit (used for execute / exec alignment isError).
func IsCommandFailureResult(content string) bool {
	return strings.Contains(content, "Command execution failed:")
}

// IsLegacyShellExitNoise Filter out redundant exit code lines in the legacy shell stream.
func IsLegacyShellExitNoise(s string) bool {
	trimmed := strings.TrimSpace(s)
	return strings.HasPrefix(trimmed, "command exited with non-zero code ")
}
