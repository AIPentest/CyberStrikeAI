package multiagent

import (
	"context"
	"strings"
	"sync/atomic"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

type einoTurnLoopEventBridge struct {
	conversationID string
	orchestration  string
	progress       func(eventType, message string, data interface{})
	gen            *adk.AsyncGenerator[*adk.AgentEvent]
	forwardedErr   atomic.Bool
}

func newEinoTurnLoopEventBridge(
	conversationID string,
	orchestration string,
	progress func(eventType, message string, data interface{}),
	gen *adk.AsyncGenerator[*adk.AgentEvent],
) *einoTurnLoopEventBridge {
	return &einoTurnLoopEventBridge{
		conversationID: conversationID,
		orchestration:  orchestration,
		progress:       progress,
		gen:            gen,
	}
}

func (b *einoTurnLoopEventBridge) OnAgentEvents(
	_ context.Context,
	tc *adk.TurnContext[EinoTurnLoopItem, *schema.Message],
	events *adk.AsyncIterator[*adk.AgentEvent],
) error {
	for {
		ev, ok := events.Next()
		if !ok {
			return nil
		}
		if ev == nil {
			continue
		}
		if ev.Err != nil && isEinoVoluntaryCancelErr(ev.Err) {
			// TurnLoop owns cancel routing. Returning CancelError /
			// ErrStreamCanceled from OnAgentEvents aborts the whole loop and
			// races with the Preempted signal becoming observable. A preempt
			// must return nil so the queued user supplement can start the next
			// turn; a terminal stop is surfaced through TurnLoopExitState.
			if isEinoTurnLoopPreemptCancel(tc, ev.Err) {
				b.emitPreempted()
			}
			return nil
		}
		if b.gen != nil {
			b.gen.Send(ev)
		}
		if ev.Err != nil {
			b.forwardedErr.Store(true)
			return ev.Err
		}
	}
}

func (b *einoTurnLoopEventBridge) ForwardedError() bool {
	if b == nil {
		return false
	}
	return b.forwardedErr.Load()
}

func (b *einoTurnLoopEventBridge) emitPreempted() {
	if b == nil || b.progress == nil {
		return
	}
	b.progress("progress", "Eino TurnLoop has switched to the next turn with the user supplement at a safe point.", map[string]interface{}{
		"conversationId": b.conversationID,
		"source":         "eino",
		"orchestration":  b.orchestration,
		"kind":           "turn_loop_preempted",
	})
}

func isEinoTurnLoopPreemptCancel(tc *adk.TurnContext[EinoTurnLoopItem, *schema.Message], err error) bool {
	if tc == nil || !isEinoVoluntaryCancelErr(err) {
		return false
	}
	select {
	case <-tc.Preempted:
		return true
	default:
		return false
	}
}

func einoTurnLoopInterruptTimelineSummary(note string) string {
	note = strings.TrimSpace(note)
	if note == "" {
		return "User chose \"interrupt and continue\" without a note; pushed into Eino TurnLoop and waiting for the safe-point resume."
	}
	return "User interrupt note (Eino TurnLoop native resume):\n\n" + note
}
