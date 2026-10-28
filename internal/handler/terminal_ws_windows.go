//go:build windows

package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// RunCommandWS 交互式 PTY 终端依赖 Unix PTY（见 terminal_ws_unix.go）；Windows 暂不支持。
func (h *TerminalHandler) RunCommandWS(c *gin.Context) {
	respondError(c, http.StatusNotImplemented, "Windows 暂不支持交互式 WebSocket 终端；请使用 POST /terminal/run 或 /terminal/run/stream。", "Interactive WebSocket terminal is not supported on Windows; use POST /terminal/run or /terminal/run/stream instead.")
}
