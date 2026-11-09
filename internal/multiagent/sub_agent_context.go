package multiagent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"cyberstrike-ai/internal/agent"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

const userContextSupplementHeader = "\n\n## User history input (verbatim; sub-agent must read)\n"

// taskContextEnrichMiddleware intercepts "task" tool calls on the orchestrator
// and appends the user's original conversation messages to the task description.
// This ensures sub-agents always receive the full user intent (target URLs,
// scope, etc.) even when the orchestrator forgets to include them.
//
// Design: user context is injected into the task description (per-task), NOT
// into the sub-agent's Instruction (system prompt). This keeps sub-agent
// Instructions clean as pure role definitions while attaching context to the
// specific delegation — aligned with Claude Code's agent design philosophy.
type taskContextEnrichMiddleware struct {
	adk.BaseChatModelAgentMiddleware
	supplement string // pre-built user context block
}

// newTaskContextEnrichMiddleware returns a middleware that enriches task
// descriptions with user conversation context. Returns nil if disabled
// (maxRunes < 0) or no user messages exist.
// projectBlackboard 仅传项目黑板索引块（BuildFactIndexBlock）；勿传完整 systemPromptExtra。
func newTaskContextEnrichMiddleware(userMessage string, history []agent.ChatMessage, maxRunes int, projectBlackboard string) adk.ChatModelAgentMiddleware {
	supplement := buildUserContextSupplement(userMessage, history, maxRunes)
	if bb := strings.TrimSpace(projectBlackboard); bb != "" {
		if supplement != "" {
			supplement += "\n\n" + bb
		} else {
			supplement = "\n\n" + bb
		}
	}
	if supplement == "" {
		return nil
	}
	return &taskContextEnrichMiddleware{supplement: supplement}
}

type agenticTaskContextEnrichMiddleware struct {
	*adk.TypedBaseChatModelAgentMiddleware[*schema.AgenticMessage]
	supplement string
}

func newAgenticTaskContextEnrichMiddleware(userMessage string, history []agent.ChatMessage, maxRunes int, projectBlackboard string) adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage] {
	supplement := buildUserContextSupplement(userMessage, history, maxRunes)
	if bb := strings.TrimSpace(projectBlackboard); bb != "" {
		if supplement != "" {
			supplement += "\n\n" + bb
		} else {
			supplement = "\n\n" + bb
		}
	}
	if supplement == "" {
		return nil
	}
	return &agenticTaskContextEnrichMiddleware{
		TypedBaseChatModelAgentMiddleware: &adk.TypedBaseChatModelAgentMiddleware[*schema.AgenticMessage]{},
		supplement:                        supplement,
	}
}

func (m *taskContextEnrichMiddleware) WrapInvokableToolCall(
	ctx context.Context,
	endpoint adk.InvokableToolCallEndpoint,
	tCtx *adk.ToolContext,
) (adk.InvokableToolCallEndpoint, error) {
	return wrapTaskContextEnrichCall(m, ctx, endpoint, tCtx)
}

func (m *agenticTaskContextEnrichMiddleware) WrapInvokableToolCall(
	ctx context.Context,
	endpoint adk.InvokableToolCallEndpoint,
	tCtx *adk.ToolContext,
) (adk.InvokableToolCallEndpoint, error) {
	return wrapTaskContextEnrichCall(m, ctx, endpoint, tCtx)
}

type taskContextEnricher interface {
	enrichTaskDescription(argsJSON string) string
}

func wrapTaskContextEnrichCall(
	m taskContextEnricher,
	ctx context.Context,
	endpoint adk.InvokableToolCallEndpoint,
	tCtx *adk.ToolContext,
) (adk.InvokableToolCallEndpoint, error) {
	if tCtx == nil || !strings.EqualFold(strings.TrimSpace(tCtx.Name), "task") {
		return endpoint, nil
	}
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
		enriched := m.enrichTaskDescription(argumentsInJSON)
		return endpoint(ctx, enriched, opts...)
	}, nil
}

// enrichTaskDescription parses the task JSON arguments, appends user context
// to the "description" field, and re-serializes. Falls back to the original
// JSON if parsing fails or no description field exists.
func (m *taskContextEnrichMiddleware) enrichTaskDescription(argsJSON string) string {
	return enrichTaskDescriptionWithSupplement(argsJSON, m.supplement)
}

func (m *agenticTaskContextEnrichMiddleware) enrichTaskDescription(argsJSON string) string {
	return enrichTaskDescriptionWithSupplement(argsJSON, m.supplement)
}

func enrichTaskDescriptionWithSupplement(argsJSON, supplement string) string {
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &raw); err != nil {
		return argsJSON
	}
	desc, ok := raw["description"].(string)
	if !ok {
		return argsJSON
	}
	raw["description"] = desc + supplement
	enriched, err := json.Marshal(raw)
	if err != nil {
		return argsJSON
	}
	return string(enriched)
}

// buildUserContextSupplement collects user messages from conversation history
// and the current message, returning a formatted block to append to task
// descriptions. Returns "" if disabled or no user messages exist.
func buildUserContextSupplement(userMessage string, history []agent.ChatMessage, maxRunes int) string {
	if maxRunes < 0 {
		return ""
	}

	var userMsgs []string
	for _, h := range history {
		if h.Role == "user" {
			if m := strings.TrimSpace(h.Content); m != "" {
				userMsgs = append(userMsgs, m)
			}
		}
	}
	if um := strings.TrimSpace(userMessage); um != "" {
		if len(userMsgs) == 0 || userMsgs[len(userMsgs)-1] != um {
			userMsgs = append(userMsgs, um)
		}
	}
	if len(userMsgs) == 0 {
		return ""
	}

	lines := make([]string, 0, len(userMsgs))
	for i, msg := range userMsgs {
		lines = append(lines, fmt.Sprintf("[round %d] %s", i+1, msg))
	}
	joined := strings.Join(lines, "\n")
	if maxRunes > 0 && len([]rune(joined)) > maxRunes {
		joined = truncateKeepFirstLast(userMsgs, maxRunes)
	}

	return userContextSupplementHeader + joined
}

// truncateKeepFirstLast keeps the first and last user messages, giving each
// half the rune budget. The first message typically contains target info;
// the last contains the current instruction.
func truncateKeepFirstLast(msgs []string, maxRunes int) string {
	if len(msgs) == 1 {
		return truncateRunes(msgs[0], maxRunes)
	}

	first := msgs[0]
	last := msgs[len(msgs)-1]
	sep := "\n---\n...(middle conversation omitted)...\n---\n"
	sepLen := len([]rune(sep))

	budget := maxRunes - sepLen
	if budget <= 0 {
		return truncateRunes(first+"\n---\n"+last, maxRunes)
	}

	halfBudget := budget / 2
	firstTrunc := truncateRunes(first, halfBudget)
	lastTrunc := truncateRunes(last, budget-len([]rune(firstTrunc)))

	return firstTrunc + sep + lastTrunc
}

func truncateRunes(s string, max int) string {
	rs := []rune(s)
	if len(rs) <= max {
		return s
	}
	if max <= 0 {
		return ""
	}
	return string(rs[:max])
}
