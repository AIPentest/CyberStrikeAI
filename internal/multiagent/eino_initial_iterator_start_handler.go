package multiagent

import "github.com/cloudwego/eino/adk"

type einoAgentEventIteratorStarter func([]adk.Message) *adk.AsyncIterator[*adk.AgentEvent]

type einoInitialIteratorStartHandlerConfig struct {
	ConversationID string
	OrchMode       string
	Progress       func(eventType, message string, data interface{})
	UseTurnLoop    bool
	StartRunner    einoAgentEventIteratorStarter
	StartTurnLoop  einoAgentEventIteratorStarter
}

type einoInitialIteratorStartHandler struct {
	cfg einoInitialIteratorStartHandlerConfig
}

func newEinoInitialIteratorStartHandler(cfg einoInitialIteratorStartHandlerConfig) *einoInitialIteratorStartHandler {
	return &einoInitialIteratorStartHandler{cfg: cfg}
}

func (h *einoInitialIteratorStartHandler) StartIfNeeded(existing *adk.AsyncIterator[*adk.AgentEvent], msgs []adk.Message) *adk.AsyncIterator[*adk.AgentEvent] {
	if existing != nil {
		return existing
	}
	if h == nil {
		return nil
	}
	if h.cfg.UseTurnLoop {
		h.emitTurnLoopTakeover()
		if h.cfg.StartTurnLoop == nil {
			return nil
		}
		return h.cfg.StartTurnLoop(msgs)
	}
	if h.cfg.StartRunner == nil {
		return nil
	}
	return h.cfg.StartRunner(msgs)
}

func (h *einoInitialIteratorStartHandler) emitTurnLoopTakeover() {
	if h == nil || h.cfg.Progress == nil {
		return
	}
	// User-visible progress copy (English); the Chinese legacy text stays matched
	// byte-identically in handler/chat.js diagnostic filters (dual-accept).
	h.cfg.Progress("progress", "Eino TurnLoop resident multi-turn runtime has taken over this conversation turn.", map[string]interface{}{
		"conversationId": h.cfg.ConversationID,
		"source":         "eino",
		"orchestration":  h.cfg.OrchMode,
		"kind":           "turn_loop_takeover",
	})
}
