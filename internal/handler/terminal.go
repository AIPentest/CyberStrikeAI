package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const (
	terminalMaxCommandLen = 4096
	terminalMaxOutputLen  = 256 * 1024 // 256KB
	terminalTimeout       = 30 * time.Minute
)

// TerminalHandler 处理系统设置中的终端命令执行
type TerminalHandler struct {
	logger *zap.Logger
}

// maskTerminalCommand 对可能包含敏感信息的终端命令做脱敏，避免在日志中直接记录密码等内容
func maskTerminalCommand(cmd string) string {
	trimmed := strings.TrimSpace(cmd)
	lower := strings.ToLower(trimmed)
	if strings.Contains(lower, "sudo") || strings.Contains(lower, "password") {
		return "[masked sensitive terminal command]"
	}
	if len(trimmed) > 256 {
		return trimmed[:256] + "..."
	}
	return trimmed
}

// NewTerminalHandler 创建终端处理器
func NewTerminalHandler(logger *zap.Logger) *TerminalHandler {
	return &TerminalHandler{logger: logger}
}

// RunCommandRequest 执行命令请求
type RunCommandRequest struct {
	Command string `json:"command"`
	Shell   string `json:"shell,omitempty"`
	Cwd     string `json:"cwd,omitempty"`
}

// RunCommandResponse 执行命令响应
type RunCommandResponse struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
	Error    string `json:"error,omitempty"`
}

// RunCommand 执行终端命令（需登录）
func (h *TerminalHandler) RunCommand(c *gin.Context) {
	var req RunCommandRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "请求体无效，需要 command 字段", "invalid request body; the command field is required")
		return
	}

	cmdStr := strings.TrimSpace(req.Command)
	if cmdStr == "" {
		respondError(c, http.StatusBadRequest, "command 不能为空", "command must not be empty")
		return
	}
	if len(cmdStr) > terminalMaxCommandLen {
		respondError(c, http.StatusBadRequest, "命令过长", "command too long")
		return
	}

	shell := req.Shell
	if shell == "" {
		if runtime.GOOS == "windows" {
			shell = "cmd"
		} else {
			shell = "sh"
		}
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), terminalTimeout)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/c", cmdStr)
	} else {
		cmd = exec.CommandContext(ctx, shell, "-c", cmdStr)
		// 无 TTY 时设置 COLUMNS/TERM，使 ping 等工具的 usage 排版与真实终端一致
		cmd.Env = append(os.Environ(), "COLUMNS=256", "LINES=40", "TERM=xterm-256color")
	}

	if req.Cwd != "" {
		absCwd, err := filepath.Abs(req.Cwd)
		if err != nil {
			respondError(c, http.StatusBadRequest, "工作目录无效", "invalid working directory")
			return
		}
		cur, _ := os.Getwd()
		curAbs, _ := filepath.Abs(cur)
		rel, err := filepath.Rel(curAbs, absCwd)
		if err != nil || strings.HasPrefix(rel, "..") || rel == ".." {
			respondError(c, http.StatusBadRequest, "工作目录必须在当前进程目录下", "working directory must be inside the current process directory")
			return
		}
		cmd.Dir = absCwd
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	stdoutBytes := stdout.Bytes()
	stderrBytes := stderr.Bytes()

	// 限制输出长度，防止内存占用过大（复制后截断，避免修改原 buffer）
	truncSuffix := []byte("\n...(输出已截断)\n")
	if len(stdoutBytes) > terminalMaxOutputLen {
		tmp := make([]byte, terminalMaxOutputLen+len(truncSuffix))
		n := copy(tmp, stdoutBytes[:terminalMaxOutputLen])
		copy(tmp[n:], truncSuffix)
		stdoutBytes = tmp
	}
	if len(stderrBytes) > terminalMaxOutputLen {
		tmp := make([]byte, terminalMaxOutputLen+len(truncSuffix))
		n := copy(tmp, stderrBytes[:terminalMaxOutputLen])
		copy(tmp[n:], truncSuffix)
		stderrBytes = tmp
	}

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
		if ctx.Err() == context.DeadlineExceeded {
			so := strings.ReplaceAll(string(stdoutBytes), "\r\n", "\n")
			so = strings.ReplaceAll(so, "\r", "\n")
			se := strings.ReplaceAll(string(stderrBytes), "\r\n", "\n")
			se = strings.ReplaceAll(se, "\r", "\n")
			resp := RunCommandResponse{
				Stdout:   so,
				Stderr:   se,
				ExitCode: -1,
				Error:    "命令执行超时（" + terminalTimeout.String() + "）",
			}
			c.JSON(http.StatusOK, resp)
			return
		}
		h.logger.Debug("终端命令执行异常", zap.String("command", maskTerminalCommand(cmdStr)), zap.Error(err))
	}

	// 统一为 \n，避免前端因 \r 出现错位/对角线排版
	stdoutStr := strings.ReplaceAll(string(stdoutBytes), "\r\n", "\n")
	stdoutStr = strings.ReplaceAll(stdoutStr, "\r", "\n")
	stderrStr := strings.ReplaceAll(string(stderrBytes), "\r\n", "\n")
	stderrStr = strings.ReplaceAll(stderrStr, "\r", "\n")

	resp := RunCommandResponse{
		Stdout:   stdoutStr,
		Stderr:   stderrStr,
		ExitCode: exitCode,
	}
	if err != nil && exitCode != 0 {
		resp.Error = err.Error()
	}
	c.JSON(http.StatusOK, resp)
}

// streamEvent SSE 事件
type streamEvent struct {
	T string `json:"t"` // "out" | "err" | "exit"
	D string `json:"d,omitempty"`
	C int    `json:"c"` // exit code（不用 omitempty，否则 0 不序列化导致前端显示 [exit undefined]）
}

// RunCommandStream 流式执行命令，输出实时推送到前端（SSE）
func (h *TerminalHandler) RunCommandStream(c *gin.Context) {
	var req RunCommandRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "请求体无效，需要 command 字段", "invalid request body; the command field is required")
		return
	}
	cmdStr := strings.TrimSpace(req.Command)
	if cmdStr == "" {
		respondError(c, http.StatusBadRequest, "command 不能为空", "command must not be empty")
		return
	}
	if len(cmdStr) > terminalMaxCommandLen {
		respondError(c, http.StatusBadRequest, "命令过长", "command too long")
		return
	}
	shell := req.Shell
	if shell == "" {
		if runtime.GOOS == "windows" {
			shell = "cmd"
		} else {
			shell = "sh"
		}
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), terminalTimeout)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/c", cmdStr)
	} else {
		cmd = exec.CommandContext(ctx, shell, "-c", cmdStr)
		cmd.Env = append(os.Environ(), "COLUMNS=256", "LINES=40", "TERM=xterm-256color")
	}
	if req.Cwd != "" {
		absCwd, err := filepath.Abs(req.Cwd)
		if err != nil {
			respondError(c, http.StatusBadRequest, "工作目录无效", "invalid working directory")
			return
		}
		cur, _ := os.Getwd()
		curAbs, _ := filepath.Abs(cur)
		rel, err := filepath.Rel(curAbs, absCwd)
		if err != nil || strings.HasPrefix(rel, "..") || rel == ".." {
			respondError(c, http.StatusBadRequest, "工作目录必须在当前进程目录下", "working directory must be inside the current process directory")
			return
		}
		cmd.Dir = absCwd
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Writer.WriteHeader(http.StatusOK)
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		cancel()
		return
	}

	sendEvent := func(ev streamEvent) {
		body, _ := json.Marshal(ev)
		c.SSEvent("", string(body))
		flusher.Flush()
	}

	_ = runCommandStreamImpl(cmd, sendEvent, ctx)
}
