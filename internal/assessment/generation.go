package assessment

import (
	"context"
	"fmt"

	"cyberstrike-ai/internal/agent"
)

// Repair only malformed structured responses, with no tools available during
// repair. Every attempt consumes the same persisted budget as ordinary turns.
func (e *Engine) repairJSON(ctx context.Context, id string, msgs []agent.ChatMessage, reply *ModelReply, validate func(string) error) ([]agent.ChatMessage, error) {
	for attempt := 0; ; attempt++ {
		message := reply.Message
		// A rejected tool request was never executed. Do not send an orphan tool
		// call back to providers when asking for a corrected JSON-only response.
		message.ToolCalls = nil
		msgs = append(msgs, message)
		err := validate(reply.Message.Content)
		if len(reply.Message.ToolCalls) > 0 {
			err = fmt.Errorf("a JSON result is required; tool calls are not allowed here")
		}
		if err == nil {
			return msgs, nil
		}
		if attempt >= 2 {
			return msgs, err
		}
		validationError := clip(err.Error(), 1000)
		if err = e.Store.Event(id, "", "format_repair", validationError, nil); err != nil {
			return msgs, err
		}
		msgs = append(msgs, agent.ChatMessage{Role: "user", Content: "Correct the structured response using the existing observations only. Return one JSON object matching the requested schema. Do not run tools or invent evidence. Validation error: " + validationError})
		reply, err = e.call(ctx, id, msgs, nil)
		if err != nil {
			return msgs, err
		}
	}
}
