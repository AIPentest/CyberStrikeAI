// Package reasoning maps user/config intent to CloudWeGo Eino OpenAI ChatModel fields
// (ReasoningEffort, ExtraFields such as thinking / reasoning_effort / output_config).
package reasoning

import (
	"strings"

	"cyberstrike-ai/internal/config"

	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"
)

// ClientIntent is optional per-request override from ChatRequest.reasoning.
type ClientIntent struct {
	Mode   string
	Effort string
}

type wireProfile int

const (
	wireNone wireProfile = iota
	wireClaude
	wireDeepseek
	wireOpenAI
	wireOutputConfig
)

// ApplyPlanExecutePlannerModelConfig configures the plan_execute planner/replanner
// ChatModel. Those Eino agents call WithToolChoice(Forced); several gateways reject
// thinking / reasoning fields on the same request (tool_choice required/object).
// Executor should keep the normal ApplyToEinoChatModelConfig path.
func ApplyPlanExecutePlannerModelConfig(cfg *einoopenai.ChatModelConfig, oa *config.OpenAIConfig) {
	if cfg == nil || oa == nil {
		return
	}
	mergeExtraRequestFields(cfg, oa.Reasoning.ExtraRequestFields)
	clearReasoningFromChatModelConfig(cfg)
	applyOpenAIReasoningDisabled(cfg, oa)
	if resolveWireProfile(oa, &oa.Reasoning) == wireDeepseek {
		// DeepSeek enables thinking by default, so omission would not actually
		// disable it for the planner's forced tool-choice requests.
		applyThinkingDisabled(cfg)
	}
}

func clearReasoningFromChatModelConfig(cfg *einoopenai.ChatModelConfig) {
	if cfg == nil {
		return
	}
	cfg.ReasoningEffort = ""
	if cfg.ExtraFields != nil {
		for _, key := range []string{"thinking", "reasoning_effort", "output_config", "reasoning"} {
			delete(cfg.ExtraFields, key)
		}
		if len(cfg.ExtraFields) == 0 {
			cfg.ExtraFields = nil
		}
	}
}

func mergeExtraRequestFields(cfg *einoopenai.ChatModelConfig, fields map[string]interface{}) {
	if cfg == nil || len(fields) == 0 {
		return
	}
	if cfg.ExtraFields == nil {
		cfg.ExtraFields = make(map[string]any, len(fields))
	}
	for k, v := range fields {
		cfg.ExtraFields[k] = v
	}
}

// ApplyToEinoChatModelConfig merges reasoning-related options into cfg.
// Precondition: cfg already has APIKey, BaseURL, Model, HTTPClient set.
func ApplyToEinoChatModelConfig(cfg *einoopenai.ChatModelConfig, oa *config.OpenAIConfig, client *ClientIntent) {
	if cfg == nil || oa == nil {
		return
	}
	sr := &oa.Reasoning
	allowClient := sr.AllowClientReasoningEffective()
	mode := effectiveModeForModel(oa, client)

	// Admin-defined root fields are independent of the selected reasoning wire
	// profile. Merge them first so mode=off can remove only reasoning controls
	// while preserving unrelated gateway options.
	mergeExtraRequestFields(cfg, sr.ExtraRequestFields)
	if mode == "off" {
		clearReasoningFromChatModelConfig(cfg)
		applyOpenAIReasoningDisabled(cfg, oa)
		// Strict OpenAI endpoints reject unknown `thinking` fields, whereas the
		// DeepSeek API enables thinking by default and requires an explicit
		// thinking.type=disabled switch. The configured profile is authoritative;
		// auto-detection only happens inside resolveWireProfile for profile=auto.
		if resolveWireProfile(oa, sr) == wireDeepseek {
			applyThinkingDisabled(cfg)
		}
		return
	}

	// Claude (Anthropic): merge admin extras first; optional extended thinking maps to top-level `thinking`.
	// DeepSeek/OpenAI-style fields are not sent.
	if strings.EqualFold(strings.TrimSpace(oa.Provider), "claude") ||
		strings.EqualFold(strings.TrimSpace(oa.Provider), "anthropic") {
		applyClaudeExtendedThinking(cfg, mode, effectiveEffort(sr, client, allowClient), oa.Model)
		return
	}

	effort := effectiveEffort(sr, client, allowClient)
	prof := resolveWireProfile(oa, sr)

	switch prof {
	case wireClaude, wireNone:
		return
	case wireDeepseek:
		applyDeepseek(cfg, mode, effort)
	case wireOutputConfig:
		applyOutputConfigEffort(cfg, mode, effort)
	default: // wireOpenAI
		applyOpenAICompat(cfg, mode, effort)
	}
}

// AgenticOpenAIExtraFields returns reasoning-related request fields for
// agenticopenai.ChatConfig. The agentic chat backend currently exposes provider
// extensions through ExtraFields instead of typed ReasoningEffort fields.
func AgenticOpenAIExtraFields(oa *config.OpenAIConfig, client *ClientIntent) map[string]any {
	if oa == nil {
		return nil
	}
	sr := &oa.Reasoning
	allowClient := sr.AllowClientReasoningEffective()
	mode := effectiveModeForModel(oa, client)
	fields := cloneExtraRequestFields(sr.ExtraRequestFields)
	if mode == "off" {
		clearReasoningExtraFields(fields)
		if resolveWireProfile(oa, sr) == wireDeepseek {
			if fields == nil {
				fields = make(map[string]any)
			}
			fields["thinking"] = map[string]any{"type": "disabled"}
		}
		return openAIReasoningDisabledFields(fields, oa)
	}
	if strings.EqualFold(strings.TrimSpace(oa.Provider), "claude") ||
		strings.EqualFold(strings.TrimSpace(oa.Provider), "anthropic") {
		return fields
	}
	effort := effectiveEffort(sr, client, allowClient)
	switch resolveWireProfile(oa, sr) {
	case wireDeepseek:
		if mode == "auto" || mode == "on" {
			if fields == nil {
				fields = make(map[string]any)
			}
			fields["thinking"] = map[string]any{"type": "enabled"}
		}
		if effort != "" {
			if fields == nil {
				fields = make(map[string]any)
			}
			fields["reasoning_effort"] = effortStringForAPI(effort)
		}
	case wireOutputConfig:
		e := effort
		if mode == "on" && e == "" {
			e = "high"
		}
		if e != "" {
			if fields == nil {
				fields = make(map[string]any)
			}
			fields["output_config"] = map[string]any{"effort": effortStringForAPI(e)}
		}
	default:
		e := effort
		if mode == "on" && e == "" {
			e = "medium"
		}
		if e != "" {
			if fields == nil {
				fields = make(map[string]any)
			}
			fields["reasoning_effort"] = effortStringForAPI(e)
		}
	}
	return fields
}

// AgenticOpenAIPlannerExtraFields mirrors ApplyPlanExecutePlannerModelConfig for
// agenticopenai.ChatConfig: keep admin extras, strip reasoning controls, and
// explicitly disable DeepSeek thinking where omission would still think.
func AgenticOpenAIPlannerExtraFields(oa *config.OpenAIConfig) map[string]any {
	if oa == nil {
		return nil
	}
	fields := cloneExtraRequestFields(oa.Reasoning.ExtraRequestFields)
	clearReasoningExtraFields(fields)
	fields = openAIReasoningDisabledFields(fields, oa)
	if resolveWireProfile(oa, &oa.Reasoning) == wireDeepseek {
		if fields == nil {
			fields = make(map[string]any)
		}
		fields["thinking"] = map[string]any{"type": "disabled"}
	}
	return fields
}

func cloneExtraRequestFields(fields map[string]interface{}) map[string]any {
	if len(fields) == 0 {
		return nil
	}
	out := make(map[string]any, len(fields))
	for k, v := range fields {
		out[k] = v
	}
	return out
}

func clearReasoningExtraFields(fields map[string]any) {
	for _, key := range []string{"thinking", "reasoning_effort", "output_config", "reasoning"} {
		delete(fields, key)
	}
}

// applyClaudeExtendedThinking sets Anthropic Messages API fields per official guidance:
//   - Adaptive models (4.6+): thinking.type=adaptive; output_config.effort only when user sets effort (API default is high).
//   - Sonnet 3.7: thinking.type=enabled + budget_tokens=10000 (doc example); effort is not mapped — use extra_request_fields for custom budget.
func applyClaudeExtendedThinking(cfg *einoopenai.ChatModelConfig, mode, effort, model string) {
	if cfg == nil || mode == "off" {
		return
	}
	if cfg.ExtraFields == nil {
		cfg.ExtraFields = make(map[string]any)
	}
	m := strings.ToLower(strings.TrimSpace(model))
	sonnet37 := isClaudeSonnet37(m)

	if _, exists := cfg.ExtraFields["thinking"]; !exists {
		cfg.ExtraFields["thinking"] = claudeThinkingForModel(m, sonnet37)
	}

	applyClaudeOutputConfigEffort(cfg, effort, sonnet37)
}

// claudeSonnet37DefaultBudgetTokens matches Anthropic extended-thinking documentation examples (budget_tokens with max_tokens 16000).
const claudeSonnet37DefaultBudgetTokens = 10000

func isClaudeSonnet37(m string) bool {
	return strings.Contains(m, "claude-3-7-sonnet") ||
		strings.Contains(m, "3-7-sonnet") ||
		strings.Contains(m, "sonnet-3.7")
}

func claudeThinkingForModel(m string, sonnet37 bool) map[string]any {
	if sonnet37 {
		return map[string]any{
			"type":          "enabled",
			"budget_tokens": claudeSonnet37DefaultBudgetTokens,
			"display":       "summarized",
		}
	}
	// Opus 4.7+: manual enabled+budget rejected — adaptive only.
	if strings.Contains(m, "opus-4-7") || strings.Contains(m, "opus-4.7") {
		return map[string]any{
			"type":    "adaptive",
			"display": "summarized",
		}
	}
	return map[string]any{
		"type":    "adaptive",
		"display": "summarized",
	}
}

// applyClaudeOutputConfigEffort sets top-level output_config.effort only when effort is explicitly configured.
// Omitted effort uses the API default (high); do not inject effort on mode:on alone.
func applyClaudeOutputConfigEffort(cfg *einoopenai.ChatModelConfig, effort string, sonnet37 bool) {
	if cfg == nil || sonnet37 {
		return
	}
	if _, exists := cfg.ExtraFields["output_config"]; exists {
		return
	}
	e := effortStringForAPI(effort)
	if e == "" {
		return
	}
	cfg.ExtraFields["output_config"] = map[string]any{"effort": e}
}

func effectiveMode(sr *config.OpenAIReasoningConfig, client *ClientIntent, allowClient bool) string {
	server := strings.ToLower(strings.TrimSpace(sr.ModeEffective()))
	if server == "" || server == "default" {
		server = "auto"
	}
	if !allowClient || client == nil {
		return server
	}
	cm := strings.ToLower(strings.TrimSpace(client.Mode))
	if cm == "" || cm == "default" {
		return server
	}
	return cm
}

func effectiveEffort(sr *config.OpenAIReasoningConfig, client *ClientIntent, allowClient bool) string {
	se := normalizeEffort(sr.Effort)
	if !allowClient || client == nil {
		return se
	}
	ce := normalizeEffort(client.Effort)
	if ce != "" {
		return ce
	}
	return se
}

func normalizeEffort(s string) string {
	e := strings.ToLower(strings.TrimSpace(s))
	switch e {
	case "low", "medium", "high", "max", "xhigh":
		return e
	default:
		return ""
	}
}

// usesExtraFieldsReasoningEffort 为 Eino 无枚举的最高档 effort，经 ExtraFields 原样下发（max / xhigh 由网关自行识别，不做互转）。
func usesExtraFieldsReasoningEffort(e string) bool {
	return e == "max" || e == "xhigh"
}

func resolveWireProfile(oa *config.OpenAIConfig, sr *config.OpenAIReasoningConfig) wireProfile {
	provider := strings.TrimSpace(oa.Provider)
	if strings.EqualFold(provider, "claude") || strings.EqualFold(provider, "anthropic") {
		return wireClaude
	}
	p := strings.ToLower(strings.TrimSpace(sr.ProfileEffective()))
	switch p {
	case "output_config", "output_config_effort":
		return wireOutputConfig
	case "openai", "openai_compat":
		return wireOpenAI
	case "deepseek", "deepseek_compat":
		return wireDeepseek
	case "auto", "":
		if oa.IsDeepSeekEndpointOrModel() {
			return wireDeepseek
		}
		return wireOpenAI
	default:
		return wireOpenAI
	}
}

func applyThinkingDisabled(cfg *einoopenai.ChatModelConfig) {
	if cfg == nil {
		return
	}
	if cfg.ExtraFields == nil {
		cfg.ExtraFields = make(map[string]any)
	}
	cfg.ExtraFields["thinking"] = map[string]any{"type": "disabled"}
}

func applyDeepseek(cfg *einoopenai.ChatModelConfig, mode, effort string) {
	// auto: enable thinking for DeepSeek line; on: same; auto without effort still opens thinking.
	if mode == "auto" || mode == "on" {
		if cfg.ExtraFields == nil {
			cfg.ExtraFields = make(map[string]any)
		}
		cfg.ExtraFields["thinking"] = map[string]any{"type": "enabled"}
	}
	if effort != "" {
		if cfg.ExtraFields == nil {
			cfg.ExtraFields = make(map[string]any)
		}
		cfg.ExtraFields["reasoning_effort"] = effortStringForAPI(effort)
	}
}

func applyOpenAICompat(cfg *einoopenai.ChatModelConfig, mode, effort string) {
	if mode == "auto" && effort == "" {
		return
	}
	e := effort
	if mode == "on" && e == "" {
		e = "medium"
	}
	if e == "" {
		return
	}
	if usesExtraFieldsReasoningEffort(e) {
		if cfg.ExtraFields == nil {
			cfg.ExtraFields = make(map[string]any)
		}
		cfg.ExtraFields["reasoning_effort"] = effortStringForAPI(e)
		return
	}
	switch e {
	case "low":
		cfg.ReasoningEffort = einoopenai.ReasoningEffortLevelLow
	case "medium":
		cfg.ReasoningEffort = einoopenai.ReasoningEffortLevelMedium
	case "high":
		cfg.ReasoningEffort = einoopenai.ReasoningEffortLevelHigh
	}
}

func applyOutputConfigEffort(cfg *einoopenai.ChatModelConfig, mode, effort string) {
	if mode == "auto" && effort == "" {
		return
	}
	e := effort
	if mode == "on" && e == "" {
		e = "high"
	}
	if e == "" {
		return
	}
	if cfg.ExtraFields == nil {
		cfg.ExtraFields = make(map[string]any)
	}
	cfg.ExtraFields["output_config"] = map[string]any{"effort": effortStringForAPI(e)}
}

func effortStringForAPI(e string) string {
	// 原样透传：OpenAI 官方多为 xhigh，部分兼容网关为 max，由配置/对话 effort 选择。
	return strings.ToLower(strings.TrimSpace(e))
}
