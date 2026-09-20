package multiagent

import (
	"context"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

const (
	einoTurnLoopInterruptPreemptTimeout = 3 * time.Second
	einoTurnLoopIdleStop                = 250 * time.Millisecond
)

// EinoTurnLoopItem is the conversation-level input unit consumed by an Eino
// TurnLoop. The item is gob-friendly so it can be checkpointed by TurnLoop when
// a CheckPointStore is configured.
type EinoTurnLoopItem struct {
	Messages []*schema.Message
	Kind     string
	Note     string
}

// EinoTurnLoopRuntime wraps Eino's native TurnLoop with the semantics this
// project needs: persistent per-conversation runtime, user-supplement preempt,
// and graceful idle shutdown.
type EinoTurnLoopRuntime struct {
	loop             *adk.TurnLoop[EinoTurnLoopItem, *schema.Message]
	interruptTimeout time.Duration
}

type EinoTurnLoopRuntimeConfig struct {
	Agent            adk.Agent
	InitialMessages  []*schema.Message
	Store            adk.CheckPointStore
	CheckpointID     string
	EnableStreaming  bool
	PrepareAgent     func(context.Context, *adk.TurnLoop[EinoTurnLoopItem, *schema.Message], []EinoTurnLoopItem) (adk.Agent, error)
	OnAgentEvents    func(context.Context, *adk.TurnContext[EinoTurnLoopItem, *schema.Message], *adk.AsyncIterator[*adk.AgentEvent]) error
	InterruptTimeout time.Duration
}

func NewEinoTurnLoopRuntime(cfg EinoTurnLoopRuntimeConfig) *EinoTurnLoopRuntime {
	timeout := cfg.InterruptTimeout
	if timeout <= 0 {
		timeout = einoTurnLoopInterruptPreemptTimeout
	}
	enableStreaming := cfg.EnableStreaming
	prepareAgent := cfg.PrepareAgent
	history := &einoTurnHistory{}
	if prepareAgent == nil {
		prepareAgent = func(context.Context, *adk.TurnLoop[EinoTurnLoopItem, *schema.Message], []EinoTurnLoopItem) (adk.Agent, error) {
			return cfg.Agent, nil
		}
	}
	loop := adk.NewTurnLoop[EinoTurnLoopItem, *schema.Message](adk.TurnLoopConfig[EinoTurnLoopItem, *schema.Message]{
		Store:        cfg.Store,
		CheckpointID: cfg.CheckpointID,
		GenInput: func(ctx context.Context, _ *adk.TurnLoop[EinoTurnLoopItem, *schema.Message], items []EinoTurnLoopItem) (*adk.GenInputResult[EinoTurnLoopItem, *schema.Message], error) {
			msgs := append(history.nextInput(), mergeEinoTurnLoopMessages(items)...)
			history = &einoTurnHistory{}
			history.begin(msgs)
			return &adk.GenInputResult[EinoTurnLoopItem, *schema.Message]{
				RunCtx: context.WithValue(ctx, einoTurnHistoryKey{}, history),
				Input: &adk.AgentInput{
					Messages:        msgs,
					EnableStreaming: enableStreaming,
				},
				Consumed: items,
			}, nil
		},
		GenResume: func(ctx context.Context, _ *adk.TurnLoop[EinoTurnLoopItem, *schema.Message], interruptedItems, unhandledItems, newItems []EinoTurnLoopItem) (*adk.GenResumeResult[EinoTurnLoopItem, *schema.Message], error) {
			consumed := make([]EinoTurnLoopItem, 0, len(interruptedItems)+len(newItems))
			consumed = append(consumed, interruptedItems...)
			consumed = append(consumed, newItems...)
			remaining := append([]EinoTurnLoopItem(nil), unhandledItems...)
			return &adk.GenResumeResult[EinoTurnLoopItem, *schema.Message]{
				RunCtx:    context.WithValue(ctx, einoTurnHistoryKey{}, history),
				Consumed:  consumed,
				Remaining: remaining,
			}, nil
		},
		PrepareAgent: prepareAgent,
		OnAgentEvents: func(ctx context.Context, tc *adk.TurnContext[EinoTurnLoopItem, *schema.Message], events *adk.AsyncIterator[*adk.AgentEvent]) error {
			return history.wrapEvents(cfg.OnAgentEvents)(ctx, tc, events)
		},
	})
	if len(cfg.InitialMessages) > 0 {
		loop.Push(EinoTurnLoopItem{Kind: "initial", Messages: cloneSchemaMessages(cfg.InitialMessages)})
	}
	return &EinoTurnLoopRuntime{loop: loop, interruptTimeout: timeout}
}

func (r *EinoTurnLoopRuntime) Run(ctx context.Context) {
	if r == nil || r.loop == nil {
		return
	}
	r.loop.Run(ctx)
}

func (r *EinoTurnLoopRuntime) PushInterruptContinue(note string) bool {
	if r == nil || r.loop == nil {
		return false
	}
	item := EinoTurnLoopItem{
		Kind:     "interrupt_continue",
		Note:     strings.TrimSpace(note),
		Messages: []*schema.Message{schema.UserMessage(formatInterruptContinuePrompt(note))},
	}
	ok, ack := r.loop.Push(item, adk.WithPreemptTimeout[EinoTurnLoopItem, *schema.Message](adk.AnySafePoint, r.interruptTimeout))
	if ack != nil {
		go func() { <-ack }()
	}
	return ok
}

func (r *EinoTurnLoopRuntime) StopImmediate(cause string) {
	if r == nil || r.loop == nil {
		return
	}
	r.loop.Stop(adk.WithImmediate(), adk.WithStopCause(cause))
}

func (r *EinoTurnLoopRuntime) StopWhenIdle() {
	if r == nil || r.loop == nil {
		return
	}
	r.loop.Stop(adk.UntilIdleFor(einoTurnLoopIdleStop))
}

func (r *EinoTurnLoopRuntime) Wait() *adk.TurnLoopExitState[EinoTurnLoopItem, *schema.Message] {
	if r == nil || r.loop == nil {
		return nil
	}
	return r.loop.Wait()
}

func mergeEinoTurnLoopMessages(items []EinoTurnLoopItem) []*schema.Message {
	var msgs []*schema.Message
	for _, item := range items {
		msgs = append(msgs, cloneSchemaMessages(item.Messages)...)
	}
	return msgs
}

func formatInterruptContinuePrompt(note string) string {
	note = strings.TrimSpace(note)
	if note == "" {
		return "用户请求中断当前推理并继续。请基于已经完成的步骤继续，不要重复已完成工具调用。"
	}
	return "用户请求中断当前推理并补充上下文后继续：\n" + note +
		"\n\n请基于已经完成的步骤继续，不要重复已完成工具调用。"
}

func cloneSchemaMessages(in []*schema.Message) []*schema.Message {
	if len(in) == 0 {
		return nil
	}
	out := make([]*schema.Message, 0, len(in))
	for _, msg := range in {
		if msg == nil {
			continue
		}
		cp := *msg
		if len(msg.ToolCalls) > 0 {
			cp.ToolCalls = append([]schema.ToolCall(nil), msg.ToolCalls...)
		}
		if len(msg.MultiContent) > 0 {
			cp.MultiContent = append([]schema.ChatMessagePart(nil), msg.MultiContent...)
		}
		if len(msg.UserInputMultiContent) > 0 {
			cp.UserInputMultiContent = append([]schema.MessageInputPart(nil), msg.UserInputMultiContent...)
		}
		if len(msg.AssistantGenMultiContent) > 0 {
			cp.AssistantGenMultiContent = append([]schema.MessageOutputPart(nil), msg.AssistantGenMultiContent...)
		}
		cp.Extra = cloneAnyMap(msg.Extra)
		out = append(out, &cp)
	}
	return out
}
