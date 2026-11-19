package handler

import (
	"errors"
	"fmt"
	"net/http"

	"cyberstrike-ai/internal/runlease"
	"cyberstrike-ai/internal/security"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// ShutdownTasks stops local work before shared MCP clients and databases close.
func (h *AgentHandler) ShutdownTasks() {
	if h != nil && h.tasks != nil {
		h.asyncSubmitMu.Lock()
		h.tasks.Shutdown()
		h.asyncSubmitMu.Unlock()
		h.asyncWorkers.Wait()
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
				event := taskCleanupEvent(err)
				send(event.Type, event.Message, event.Data)
				data = map[string]interface{}{"conversationId": conversationID, "runId": runID, "status": taskCleanupStatus(err), "cleanupError": err.Error()}
			} else {
				payload := map[string]interface{}{}
				if existing, ok := data.(map[string]interface{}); ok {
					for key, value := range existing {
						payload[key] = value
					}
				}
				payload["conversationId"] = conversationID
				payload["runId"] = runID
				for _, task := range h.tasks.GetCompletedTasks() {
					if task.RunID == runID && task.ConversationID == conversationID {
						payload["status"] = task.Status
						break
					}
				}
				data = payload
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

// A cleanup failure that can still resolve on retry takes precedence over
// an unconfirmed outcome. Preserve the details of both for diagnostics.
func taskCleanupResult(processErr, workerErr error) error {
	localUnconfirmed := errors.Is(processErr, security.ErrCleanupUnconfirmed)
	remoteUnconfirmed := errors.Is(workerErr, runlease.ErrUnconfirmed)
	switch {
	case processErr != nil && !localUnconfirmed && remoteUnconfirmed:
		return fmt.Errorf("local cleanup: %w, remote state: %v", processErr, workerErr)
	case localUnconfirmed && workerErr != nil && !remoteUnconfirmed:
		return fmt.Errorf("local cleanup: %v, worker cleanup: %w", processErr, workerErr)
	default:
		return errors.Join(processErr, workerErr)
	}
}

func taskCleanupStatus(err error) string {
	if errors.Is(err, security.ErrCleanupUnconfirmed) || errors.Is(err, runlease.ErrUnconfirmed) {
		return "cleanup_unconfirmed"
	}
	return "cleanup_failed"
}

func taskCleanupMessage(err error) string {
	switch {
	case errors.Is(err, security.ErrCleanupUnconfirmed) && errors.Is(err, runlease.ErrUnconfirmed):
		return "本地残留进程处于不可中断状态，远端 MCP 停止状态待确认"
	case errors.Is(err, security.ErrCleanupUnconfirmed):
		return "本地残留进程处于不可中断状态，待主机解除阻塞后回收"
	case errors.Is(err, runlease.ErrUnconfirmed):
		return "本地执行已结束，远端 MCP 停止状态待确认"
	default:
		return "任务资源清理失败，将自动重试"
	}
}

func taskCleanupEvent(err error) StreamEvent {
	status := taskCleanupStatus(err)
	kind := "error"
	if status == "cleanup_unconfirmed" {
		kind = "warning"
	}
	return StreamEvent{Type: kind, Message: taskCleanupMessage(err) + ": " + err.Error(), Data: map[string]interface{}{"errorType": status}}
}
