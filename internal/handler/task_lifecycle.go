package handler

import (
	"cyberstrike-ai/internal/runlease"
	"errors"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"net/http"
)

// ShutdownTasks stops local work before shared MCP clients and databases close.
func (h *AgentHandler) ShutdownTasks() {
	if h != nil && h.tasks != nil {
		h.tasks.Shutdown()
		if h.logger != nil {
			for _, task := range h.tasks.GetActiveTasks() {
				h.logger.Warn("服务关闭时任务仍有未完成的清理", zap.String("runId", task.RunID), zap.String("cleanupError", task.CleanupError))
			}
		}
	}
}

// taskFinishingEventSender makes the visible done event follow local cleanup.
func (h *AgentHandler) taskFinishingEventSender(send func(string, string, interface{}), conversationID, runID string, status func() string) func(string, string, interface{}) {
	return func(eventType, message string, data interface{}) {
		if eventType == "done" {
			if err := h.tasks.FinishTaskRun(conversationID, runID, status()); err != nil {
				if h.logger != nil {
					h.logger.Warn(taskCleanupMessage(err), zap.String("runId", runID), zap.Error(err))
				}
				send("error", taskCleanupMessage(err)+": "+err.Error(), map[string]interface{}{"errorType": taskCleanupStatus(err)})
				data = map[string]interface{}{"conversationId": conversationID, "runId": runID, "status": taskCleanupStatus(err), "cleanupError": err.Error()}
			}
		}
		send(eventType, message, data)
	}
}

// taskFinishingJSONResponder applies the same ordering to successful and failed
// non-streaming requests. No response claims completion before cleanup returns.
func (h *AgentHandler) taskFinishingJSONResponder(c *gin.Context, conversationID, runID string, status func() string) func(int, interface{}) {
	return func(code int, payload interface{}) {
		if err := h.tasks.FinishTaskRun(conversationID, runID, status()); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "status": taskCleanupStatus(err), "conversationId": conversationID})
			return
		}
		c.JSON(code, payload)
	}
}

func taskCleanupStatus(err error) string {
	if errors.Is(err, runlease.ErrUnconfirmed) {
		return "cleanup_unconfirmed"
	}
	return "cleanup_failed"
}
func taskCleanupMessage(err error) string {
	if errors.Is(err, runlease.ErrUnconfirmed) {
		return "本地执行已结束，远端 MCP 停止状态待确认"
	}
	return "任务资源清理失败，将自动重试"
}
