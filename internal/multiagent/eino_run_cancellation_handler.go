package multiagent

import "context"

type einoRunCancellationHandler struct {
	ctx            context.Context
	conversationID string
	progress       func(eventType, message string, data interface{})
	pending        *einoPendingToolCalls
	takePartial    einoPartialResultFunc
}

type einoRunCancellationHandlerConfig struct {
	Context        context.Context
	ConversationID string
	Progress       func(eventType, message string, data interface{})
	Pending        *einoPendingToolCalls
	TakePartial    einoPartialResultFunc
}

func newEinoRunCancellationHandler(cfg einoRunCancellationHandlerConfig) *einoRunCancellationHandler {
	return &einoRunCancellationHandler{
		ctx:            cfg.Context,
		conversationID: cfg.ConversationID,
		progress:       cfg.Progress,
		pending:        cfg.Pending,
		takePartial:    cfg.TakePartial,
	}
}

func (h *einoRunCancellationHandler) Handle(runErr error) (*RunResult, error) {
	if h == nil {
		return nil, runErr
	}
	if h.pending != nil {
		h.pending.FlushAsFailed(runErr)
	}
	if h.progress != nil {
		if isInterruptContinue(h.ctx) {
			h.progress("progress", "Current output paused; merging user supplement and continuing…", map[string]interface{}{
				"conversationId": h.conversationID,
				"source":         "eino",
				"kind":           "interrupt_continue",
			})
		} else if runErr != nil {
			h.progress("error", runErr.Error(), map[string]interface{}{
				"conversationId": h.conversationID,
				"source":         "eino",
			})
		}
	}
	if h.takePartial == nil {
		return nil, runErr
	}
	return h.takePartial(runErr)
}
