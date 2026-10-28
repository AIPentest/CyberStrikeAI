package security

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// FormatCommandFailureResult 与 exec 工具 ToolResult 文案一致（不含 ToolErrorPrefix）。
func FormatCommandFailureResult(exitCode int, output string) string {
	output = strings.TrimSpace(output)
	errMsg := fmt.Sprintf("exit status %d", exitCode)
	if output == "" {
		return fmt.Sprintf("Command execution failed: %s", errMsg)
	}
	if strings.HasPrefix(output, "Command execution failed:") {
		return output
	}
	return fmt.Sprintf("Command execution failed: %s\noutput: %s", errMsg, output)
}

// FormatCommandFailureFromErr 根据 exec/execute 返回的 error 生成统一失败文案（IsError 正文）。
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
	return fmt.Sprintf("Command execution failed: %v\noutput: %s", err, output)
}

// ExecuteFailureStatusLine 流式 execute 结束时追加的单行状态（输出正文已在流中推送过）。
func ExecuteFailureStatusLine(exitCode int) string {
	return fmt.Sprintf("\nCommand execution failed: exit status %d", exitCode)
}

// IsCommandFailureResult 判断工具结果正文是否表示命令非零退出（用于 execute / exec 对齐 isError）。
func IsCommandFailureResult(content string) bool {
	return strings.Contains(content, "Command execution failed:")
}

// IsLegacyShellExitNoise 过滤旧版 shell 流中冗余的 exit code 行。
func IsLegacyShellExitNoise(s string) bool {
	trimmed := strings.TrimSpace(s)
	return strings.HasPrefix(trimmed, "command exited with non-zero code ")
}
