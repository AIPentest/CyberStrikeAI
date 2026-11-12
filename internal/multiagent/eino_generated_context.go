package multiagent

import (
	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/config"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

func isGeneratedExecutionUserMessage(msg adk.Message) bool {
	if msg == nil || msg.Role != schema.User {
		return false
	}
	kind, _ := msg.Extra[agent.GeneratedContextTypeKey].(string)
	return agent.IsGeneratedExecutionContext(kind)
}

// Templates contain plan/model/tool text. Carry human intent from the original
// messages before flattening/budgeting, while keeping the execution prompt intact.
func withPlanExecuteInputProvenance(msgs, originals []adk.Message, appCfg *config.Config, mwCfg *config.MultiAgentEinoMiddlewareConfig, kind string) []adk.Message {
	out := make([]adk.Message, len(msgs))
	for i, msg := range msgs {
		if msg == nil || msg.Role != schema.User {
			out[i] = msg
			continue
		}
		clone := *msg
		clone.Extra = cloneAnyMap(msg.Extra)
		if clone.Extra == nil {
			clone.Extra = make(map[string]interface{})
		}
		clone.Extra[agent.GeneratedContextTypeKey] = kind
		out[i] = &clone
	}
	maxRunes, entryMax := config.DefaultSummarizationUserIntentLedgerMaxRunes, config.DefaultSummarizationUserIntentLedgerEntryMaxRunes
	if mwCfg != nil {
		maxRunes = mwCfg.SummarizationUserIntentLedgerMaxRunesEffective()
		entryMax = mwCfg.SummarizationUserIntentLedgerEntryMaxRunesEffective()
	}
	maxTotal := 120000
	if appCfg != nil && appCfg.OpenAI.MaxTotalTokens > 0 {
		maxTotal = appCfg.OpenAI.MaxTotalTokens
	}
	maxRunes = minPositiveInt(maxRunes, modelFacingRuneBudget(maxTotal, 0.20))
	return mergeMessageIntoLeadingSystem(out, buildOriginalUserIntentLedgerMessage(originals, maxRunes, minPositiveInt(entryMax, maxRunes)))
}

// Eino's deterministic <all_user_messages> replacement knows only its own
// summary flags. For host templates, give that replacement actual human entries
// from the ledger, keeping model-generated execution data outside user authority.
// Summary model input, snapshots and working trails still contain the template.
func summarizationDefaultFinalizeInput(msgs, originals []adk.Message) []adk.Message {
	hasTemplates := false
	out := make([]adk.Message, 0, len(msgs))
	liveHuman := make(map[string]bool)
	for _, msg := range msgs {
		if isGeneratedExecutionUserMessage(msg) {
			hasTemplates = true
			clone := *msg
			clone.Role = schema.Assistant
			out = append(out, &clone)
		} else {
			out = append(out, msg)
			if msg != nil && msg.Role == schema.User && !isInternalSummarizationUserMessage(msg) {
				liveHuman[normalizeUserIntentLedgerText(adkUserMessageText(msg))] = true
			}
		}
	}
	if !hasTemplates {
		return msgs
	}
	var carried []adk.Message
	for _, entry := range collectOriginalUserIntentEntries(originals) {
		if !liveHuman[normalizeUserIntentLedgerText(entry)] {
			carried = append(carried, schema.UserMessage(entry))
		}
	}
	return append(carried, out...)
}
