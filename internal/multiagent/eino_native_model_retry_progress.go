package multiagent

import (
	"fmt"

	"github.com/cloudwego/eino/adk"
	"go.uber.org/zap"
)

func emitEinoNativeModelRetryProgress(
	conversationID, orchMode string,
	willRetry *adk.WillRetryError,
	progress func(eventType, message string, data interface{}),
	logger *zap.Logger,
	runErr error,
) bool {
	if willRetry == nil {
		return false
	}
	if progress != nil {
		reason := ""
		if willRetry.RejectReason() != nil {
			reason = fmt.Sprint(willRetry.RejectReason())
		}
		progress("eino_model_retry", "Model call hit a transient issue; Eino is retrying natively…", map[string]interface{}{
			"conversationId": conversationID,
			"source":         "eino",
			"orchestration":  orchMode,
			"attempt":        willRetry.RetryAttempt,
			"reason":         reason,
			"error":          willRetry.Error(),
		})
	}
	if logger != nil {
		logger.Warn("eino native model retry event",
			zap.String("orchestration", orchMode),
			zap.Int("attempt", willRetry.RetryAttempt),
			zap.Error(runErr))
	}
	return true
}
