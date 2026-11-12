package agent

import (
	"encoding/json"
	"strings"
)

const ModelFacingTraceVersionKey = "cyberstrike_model_facing_trace_version"
const SummarizationContentTypeKey = "_eino_summarization_content_type"
const OriginalUserIntentEntriesKey = "_csai_original_user_intent_entries"

// IsModelFacingTraceJSON reports whether a persisted trace was produced from the final
// model-boundary state. Legacy traces have no version marker and require one-time migration.
func IsModelFacingTraceJSON(traceInputJSON string) bool {
	var raw []map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(traceInputJSON)), &raw); err != nil {
		return false
	}
	for _, msg := range raw {
		extra, _ := msg["extra"].(map[string]interface{})
		v, ok := extra[ModelFacingTraceVersionKey]
		if !ok {
			continue
		}
		switch n := v.(type) {
		case float64:
			return n >= 1
		case int:
			return n >= 1
		}
	}
	return false
}

// ParseTraceMessages 解析落库的 last_react_input（OpenAI 风格 messages JSON 数组）。
func ParseTraceMessages(traceInputJSON string) ([]ChatMessage, error) {
	traceInputJSON = strings.TrimSpace(traceInputJSON)
	if traceInputJSON == "" {
		return nil, nil
	}
	var raw []map[string]interface{}
	if err := json.Unmarshal([]byte(traceInputJSON), &raw); err != nil {
		return nil, err
	}
	modelFacing := IsModelFacingTraceJSON(traceInputJSON)
	out := make([]ChatMessage, 0, len(raw))
	for _, msgMap := range raw {
		msg := ChatMessage{}
		role, _ := msgMap["role"].(string)
		if role == "" {
			continue
		}
		msg.Role = role
		msg.ModelFacingTrace = modelFacing
		extra, _ := msgMap["extra"].(map[string]interface{})
		if strings.EqualFold(role, "user") {
			kind, _ := extra[SummarizationContentTypeKey].(string)
			if kind == "summary" || kind == "skills" {
				msg.SummarizationKind = kind
			}
			generatedKind, _ := extra[GeneratedContextTypeKey].(string)
			if IsGeneratedExecutionContext(generatedKind) {
				msg.GeneratedContextKind = generatedKind
			}
		}
		if strings.EqualFold(role, "system") {
			msg.OriginalUserIntentEntries = traceUserIntentEntries(extra[OriginalUserIntentEntriesKey])
		}
		if content, ok := msgMap["content"].(string); ok {
			msg.Content = content
		}
		// Native summaries may carry text only in user_input_multi_content.
		if msg.Content == "" {
			if parts, ok := msgMap["user_input_multi_content"].([]interface{}); ok {
				var texts []string
				for _, rawPart := range parts {
					part, _ := rawPart.(map[string]interface{})
					if part["type"] == "text" {
						if text, ok := part["text"].(string); ok {
							texts = append(texts, text)
						}
					}
				}
				msg.Content = strings.Join(texts, "\n")
			}
		}
		if rc, ok := msgMap["reasoning_content"].(string); ok && strings.TrimSpace(rc) != "" {
			msg.ReasoningContent = rc
		}
		if toolCallsRaw, ok := msgMap["tool_calls"]; ok && toolCallsRaw != nil {
			if toolCallsArray, ok := toolCallsRaw.([]interface{}); ok {
				for _, tcRaw := range toolCallsArray {
					tcMap, ok := tcRaw.(map[string]interface{})
					if !ok {
						continue
					}
					toolCall := ToolCall{}
					if id, ok := tcMap["id"].(string); ok {
						toolCall.ID = id
					}
					if toolType, ok := tcMap["type"].(string); ok {
						toolCall.Type = toolType
					}
					if funcMap, ok := tcMap["function"].(map[string]interface{}); ok {
						toolCall.Function = FunctionCall{}
						if name, ok := funcMap["name"].(string); ok {
							toolCall.Function.Name = name
						}
						if argsRaw, ok := funcMap["arguments"]; ok {
							if argsStr, ok := argsRaw.(string); ok {
								var argsMap map[string]interface{}
								if err := json.Unmarshal([]byte(argsStr), &argsMap); err == nil {
									toolCall.Function.Arguments = argsMap
								}
							} else if argsMap, ok := argsRaw.(map[string]interface{}); ok {
								toolCall.Function.Arguments = argsMap
							}
						}
					}
					if toolCall.ID != "" {
						msg.ToolCalls = append(msg.ToolCalls, toolCall)
					}
				}
			}
		}
		if toolCallID, ok := msgMap["tool_call_id"].(string); ok {
			msg.ToolCallID = toolCallID
		}
		if tn, ok := msgMap["tool_name"].(string); ok && strings.TrimSpace(tn) != "" {
			msg.ToolName = strings.TrimSpace(tn)
		} else if tn, ok := msgMap["name"].(string); ok && strings.TrimSpace(tn) != "" && strings.EqualFold(msg.Role, "tool") {
			msg.ToolName = strings.TrimSpace(tn)
		}
		out = append(out, msg)
	}
	return out, nil
}

// traceUserIntentEntries accepts only the host's structured ledger. Malformed
// metadata never falls back to parsing arbitrary system/assistant/tool text.
func traceUserIntentEntries(value interface{}) []string {
	entries, ok := value.([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		text, ok := entry.(string)
		if !ok {
			return nil
		}
		if strings.TrimSpace(text) != "" {
			out = append(out, text)
		}
	}
	return out
}

// TraceMessagesForResume drops old instructions while carrying the structured
// human ledger separately. historyToMessages renders it under current budgets.
func TraceMessagesForResume(msgs []ChatMessage) []ChatMessage {
	out := make([]ChatMessage, 0, len(msgs))
	for _, msg := range msgs {
		if strings.EqualFold(msg.Role, "system") {
			if len(msg.OriginalUserIntentEntries) == 0 {
				continue
			}
			msg.Content = ""
			msg.ToolCalls = nil
			msg.ReasoningContent = ""
		}
		out = append(out, msg)
	}
	return out
}

// ExtractLastUserTurnMessages trims at the last human turn, not an internally
// generated user-role summary. Preserve the host ledger across the trim.
func ExtractLastUserTurnMessages(msgs []ChatMessage) []ChatMessage {
	lastUser := 0
	var ledger *ChatMessage
	for i := range msgs {
		m := msgs[i]
		if strings.EqualFold(m.Role, "user") && m.SummarizationKind == "" && !IsGeneratedExecutionContext(m.GeneratedContextKind) {
			lastUser = i
		}
		if strings.EqualFold(m.Role, "system") && len(m.OriginalUserIntentEntries) > 0 {
			ledger = &m
		}
	}
	out := TraceMessagesForResume(msgs[lastUser:])
	if ledger != nil && lastUser > 0 {
		out = append(TraceMessagesForResume([]ChatMessage{*ledger}), out...)
	}
	return out
}

// ExtractLastUserTurnTraceJSON retains the host ledger and source markers in
// attack-chain history without mistaking a generated summary for a human turn.
func ExtractLastUserTurnTraceJSON(traceInputJSON string) string {
	var arr []map[string]interface{}
	if err := json.Unmarshal([]byte(traceInputJSON), &arr); err != nil {
		return traceInputJSON
	}
	lastUser := 0
	var ledger map[string]interface{}
	for i, m := range arr {
		role, _ := m["role"].(string)
		extra, _ := m["extra"].(map[string]interface{})
		kind, _ := extra[SummarizationContentTypeKey].(string)
		generatedKind, _ := extra[GeneratedContextTypeKey].(string)
		if strings.EqualFold(role, "user") && kind != "summary" && kind != "skills" && !IsGeneratedExecutionContext(generatedKind) {
			lastUser = i
		}
		if strings.EqualFold(role, "system") {
			if entries := traceUserIntentEntries(extra[OriginalUserIntentEntriesKey]); len(entries) > 0 {
				ledger = map[string]interface{}{"role": "system", "extra": map[string]interface{}{OriginalUserIntentEntriesKey: entries}}
			}
		}
	}
	if lastUser <= 0 {
		return traceInputJSON
	}
	trimmed := make([]map[string]interface{}, 0, len(arr)-lastUser+1)
	if ledger != nil {
		trimmed = append(trimmed, ledger)
	}
	trimmed = append(trimmed, arr[lastUser:]...)
	b, err := json.Marshal(trimmed)
	if err != nil {
		return traceInputJSON
	}
	return string(b)
}

// MergeAssistantTraceOutput 将 last_react_output 合并进轨迹最后一条 assistant（与 loadHistoryFromAgentTrace 一致）。
func MergeAssistantTraceOutput(msgs []ChatMessage, assistantOut string) []ChatMessage {
	assistantOut = strings.TrimSpace(assistantOut)
	if assistantOut == "" || len(msgs) == 0 {
		return msgs
	}
	out := append([]ChatMessage(nil), msgs...)
	last := &out[len(out)-1]
	if strings.EqualFold(last.Role, "assistant") && len(last.ToolCalls) == 0 {
		last.Content = assistantOut
		return out
	}
	out = append(out, ChatMessage{
		Role:    "assistant",
		Content: assistantOut,
	})
	return out
}

// MessagesToTraceJSON persists allowlisted internal provenance without adding
// unsupported fields to ChatMessage.MarshalJSON's provider request payload.
func MessagesToTraceJSON(msgs []ChatMessage) (string, error) {
	filtered := make([]map[string]interface{}, 0, len(msgs))
	for _, m := range TraceMessagesForResume(msgs) {
		raw, err := json.Marshal(m)
		if err != nil {
			return "", err
		}
		var data map[string]interface{}
		if err := json.Unmarshal(raw, &data); err != nil {
			return "", err
		}
		extra := make(map[string]interface{})
		if m.ModelFacingTrace {
			extra[ModelFacingTraceVersionKey] = 1
		}
		if strings.EqualFold(m.Role, "user") && (m.SummarizationKind == "summary" || m.SummarizationKind == "skills") {
			extra[SummarizationContentTypeKey] = m.SummarizationKind
		}
		if strings.EqualFold(m.Role, "user") && IsGeneratedExecutionContext(m.GeneratedContextKind) {
			extra[GeneratedContextTypeKey] = m.GeneratedContextKind
		}
		if strings.EqualFold(m.Role, "system") && len(m.OriginalUserIntentEntries) > 0 {
			extra[OriginalUserIntentEntriesKey] = m.OriginalUserIntentEntries
		}
		if len(extra) > 0 {
			data["extra"] = extra
		}
		filtered = append(filtered, data)
	}
	b, err := json.Marshal(filtered)
	return string(b), err
}
