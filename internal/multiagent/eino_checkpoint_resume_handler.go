package multiagent

import (
	"context"

	"github.com/cloudwego/eino/adk"
	"go.uber.org/zap"
)

type einoCheckpointResumeHandlerConfig struct {
	Context        context.Context
	ConversationID string
	OrchMode       string
	Progress       func(eventType, message string, data interface{})
	Logger         *zap.Logger
	Store          *fileCheckPointStore
	CheckPointID   string
	Resume         func(checkPointID string) (*adk.AsyncIterator[*adk.AgentEvent], error)
}

type einoCheckpointResumeHandler struct {
	cfg einoCheckpointResumeHandlerConfig
}

func newEinoCheckpointResumeHandler(cfg einoCheckpointResumeHandlerConfig) *einoCheckpointResumeHandler {
	if cfg.Context == nil {
		cfg.Context = context.Background()
	}
	return &einoCheckpointResumeHandler{cfg: cfg}
}

func (h *einoCheckpointResumeHandler) TryResume() *adk.AsyncIterator[*adk.AgentEvent] {
	if h == nil || h.cfg.Store == nil || h.cfg.CheckPointID == "" || h.cfg.Resume == nil {
		return nil
	}
	if _, existed, err := h.cfg.Store.Get(h.cfg.Context, h.cfg.CheckPointID); err != nil {
		if h.cfg.Logger != nil {
			h.cfg.Logger.Warn("eino checkpoint preflight get failed", zap.String("checkPointID", h.cfg.CheckPointID), zap.Error(err))
		}
		return nil
	} else if !existed {
		return nil
	}
	h.emitProgress("Checkpoint detected; resuming execution from the interrupted node...")
	if h.cfg.Logger != nil {
		h.cfg.Logger.Info("eino runner: resume from checkpoint", zap.String("checkPointID", h.cfg.CheckPointID))
	}
	iter, err := h.cfg.Resume(h.cfg.CheckPointID)
	if err == nil {
		return iter
	}
	if h.cfg.Logger != nil {
		h.cfg.Logger.Warn("eino runner: resume failed, fallback to fresh run",
			zap.String("checkPointID", h.cfg.CheckPointID),
			zap.Error(err))
	}
	h.emitProgress("Checkpoint resume failed; fell back to a fresh run.")
	return nil
}

func (h *einoCheckpointResumeHandler) emitProgress(message string) {
	if h == nil || h.cfg.Progress == nil {
		return
	}
	h.cfg.Progress("progress", message, map[string]interface{}{
		"conversationId": h.cfg.ConversationID,
		"source":         "eino",
		"orchestration":  h.cfg.OrchMode,
		"checkPointID":   h.cfg.CheckPointID,
	})
}
