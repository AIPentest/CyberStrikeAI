package attackchain

import (
	"strings"

	"cyberstrike-ai/internal/agent"
)

// toolEvidence is the host-side ground truth behind a generated chain: the tool
// call IDs the trajectory really contains, and the subset still visible to the
// model after budget fitting.
type toolEvidence struct {
	real  map[string]struct{}
	shown map[string]struct{}
}

// newToolEvidence returns nil when the evidence cannot be enumerated, and then no
// node is judged: the process-detail supplement dumps raw event JSON whose IDs
// are not part of the parsed trace, and the messages-table fallback has none.
func newToolEvidence(msgs []agent.ChatMessage, fittedPrompt string, supplementAppended bool) *toolEvidence {
	if supplementAppended || len(msgs) == 0 {
		return nil
	}
	real := make(map[string]struct{})
	for _, msg := range msgs {
		for _, call := range msg.ToolCalls {
			if id := strings.TrimSpace(call.ID); id != "" {
				real[id] = struct{}{}
			}
		}
		if id := strings.TrimSpace(msg.ToolCallID); id != "" {
			real[id] = struct{}{}
		}
	}
	if len(real) == 0 {
		return nil
	}
	shown := make(map[string]struct{}, len(real))
	for id := range real {
		if strings.Contains(fittedPrompt, id) {
			shown[id] = struct{}{}
		}
	}
	return &toolEvidence{real: real, shown: shown}
}

// keep decides whether a node that claims something ran may stay in the graph. A
// citation absent from the trajectory is invented. A missing citation is only
// invented when every real ID was still on screen for the model to copy verbatim;
// otherwise the ID may simply have been cut by the token budget.
func (e *toolEvidence) keep(citedID string) bool {
	if e == nil {
		return true
	}
	if citedID != "" {
		_, ok := e.real[citedID]
		return ok
	}
	return len(e.shown) != len(e.real)
}
