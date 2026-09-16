package multiagent

import (
	"context"
	"strings"
	"sync"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

type einoTurnHistoryKey struct{}
type einoTurnInstructionKey struct{}

// Owned by one TurnLoop, never shared between conversations. Model state is
// authoritative after compaction; events are a fallback for agents without the
// trace middleware and supply tool results completed after the last snapshot.
type einoTurnHistory struct {
	mu         sync.Mutex
	messages   []*schema.Message
	modelState bool
	pending    map[string]bool
	events     []*schema.Message
}

func (h *einoTurnHistory) begin(messages []*schema.Message) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.messages = cloneSchemaMessages(messages)
	h.modelState = false
	h.events = nil
}

func captureEinoTurnHistory(ctx context.Context, messages []*schema.Message) {
	h, _ := ctx.Value(einoTurnHistoryKey{}).(*einoTurnHistory)
	if h == nil || len(messages) == 0 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	// Remove only the instruction known to be regenerated on Run. Other system
	// content may contain durable context and must not be indiscriminately dropped.
	instruction, _ := ctx.Value(einoTurnInstructionKey{}).(string)
	h.messages = nil
	for _, msg := range cloneSchemaMessages(messages) {
		if msg.Role == schema.System && instruction != "" {
			if msg.Content == instruction {
				continue
			}
			msg.Content = strings.TrimPrefix(msg.Content, instruction+"\n\n")
		}
		h.messages = append(h.messages, msg)
	}
	h.modelState = true
	h.pending = make(map[string]bool)
	for _, msg := range h.messages {
		for _, call := range msg.ToolCalls {
			h.pending[call.ID] = true
		}
		if msg.Role == schema.Tool {
			delete(h.pending, msg.ToolCallID)
		}
	}
	// Snapshots already contain completed results. Release raw event payloads as
	// compaction advances instead of retaining another full transcript for long-running turns.
	for i, msg := range h.events {
		if msg != nil && (msg.Role != schema.Tool || !h.pending[msg.ToolCallID]) {
			h.events[i] = nil
		}
	}
}

func (h *einoTurnHistory) nextInput() []*schema.Message {
	h.mu.Lock()
	defer h.mu.Unlock()
	messages := cloneSchemaMessages(h.messages)
	if !h.modelState {
		messages = append(messages, cloneSchemaMessages(h.events)...)
	} else {
		// Never resurrect events discarded by summarization. Only pending calls in
		// the authoritative state may acquire results from the event stream.
		results := make(map[string]*schema.Message)
		for _, msg := range h.events {
			if msg != nil && msg.Role == schema.Tool {
				results[msg.ToolCallID] = msg
			}
		}
		var merged []*schema.Message
		for i := 0; i < len(messages); i++ {
			msg := messages[i]
			merged = append(merged, msg)
			if msg.Role != schema.Assistant || len(msg.ToolCalls) == 0 {
				continue
			}
			present := make(map[string]bool)
			for i+1 < len(messages) && messages[i+1].Role == schema.Tool {
				i++
				merged = append(merged, messages[i])
				present[messages[i].ToolCallID] = true
			}
			for _, call := range msg.ToolCalls {
				if !present[call.ID] && results[call.ID] != nil {
					merged = append(merged, cloneSchemaMessages([]*schema.Message{results[call.ID]})...)
				}
			}
		}
		messages = merged
	}
	// Cancellation may leave a partial parallel tool batch. Explicit unknown
	// results keep the protocol valid without claiming an unfinished call succeeded.
	_, state, _ := newToolPairReconcilerMiddleware(nil, "turn_loop_continue").BeforeModelRewriteState(
		context.Background(), &adk.ChatModelAgentState{Messages: messages}, nil)
	return state.Messages
}

type einoTurnEventHandler func(context.Context, *adk.TurnContext[EinoTurnLoopItem, *schema.Message], *adk.AsyncIterator[*adk.AgentEvent]) error

func (h *einoTurnHistory) wrapEvents(handler einoTurnEventHandler) einoTurnEventHandler {
	return func(ctx context.Context, tc *adk.TurnContext[EinoTurnLoopItem, *schema.Message], events *adk.AsyncIterator[*adk.AgentEvent]) error {
		iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
		done := make(chan struct{})
		go func() {
			defer close(done)
			defer gen.Close()
			var streams sync.WaitGroup
			defer streams.Wait()
			for {
				ev, ok := events.Next()
				if !ok {
					return
				}
				if ev != nil && ev.Output != nil && ev.Output.MessageOutput != nil {
					mv := ev.Output.MessageOutput
					h.mu.Lock()
					index := len(h.events)
					h.events = append(h.events, nil)
					h.mu.Unlock()
					save := func(msg *schema.Message) {
						if msg == nil {
							return
						}
						h.mu.Lock()
						if !h.modelState || (msg.Role == schema.Tool && h.pending[msg.ToolCallID]) {
							h.events[index] = cloneSchemaMessages([]*schema.Message{msg})[0]
						}
						h.mu.Unlock()
					}
					if mv.IsStreaming && mv.MessageStream != nil {
						copies := mv.MessageStream.Copy(2)
						// Copy the event as well: the framework can retain its original event.
						eventCopy, outputCopy, variantCopy := *ev, *ev.Output, *mv
						variantCopy.MessageStream = copies[0]
						outputCopy.MessageOutput = &variantCopy
						eventCopy.Output = &outputCopy
						ev = &eventCopy
						streams.Add(1)
						go func() {
							defer streams.Done()
							defer copies[1].Close()
							msg, err := (&adk.MessageVariant{IsStreaming: true, MessageStream: copies[1]}).GetMessage()
							if err == nil {
								save(msg)
							} // incomplete streams are not completed history
						}()
					} else {
						save(mv.Message)
					}
				}
				gen.Send(ev)
			}
		}()
		var err error
		if handler != nil {
			err = handler(ctx, tc, iter)
		}
		// Drain even if the UI bridge returned early on voluntary cancellation.
		// The next GenInput must not race asynchronous event/stream consumers.
		for {
			ev, ok := iter.Next()
			if !ok {
				break
			}
			if ev != nil && ev.Output != nil && ev.Output.MessageOutput != nil {
				mv := ev.Output.MessageOutput
				if mv.IsStreaming && mv.MessageStream != nil {
					mv.MessageStream.Close()
				}
			}
			if err == nil && ev != nil && ev.Err != nil && !isEinoVoluntaryCancelErr(ev.Err) {
				err = ev.Err
			}
		}
		<-done
		return err
	}
}
