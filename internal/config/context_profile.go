package config

import "strings"

const (
	// DefaultMaxTotalTokens 是未显式配置时的通用上下文预算。
	DefaultMaxTotalTokens = 120000
	// DefaultLocalContextWindow 是 local 档位下未提供 context_window 时的保守窗口。
	DefaultLocalContextWindow = 32768
	// LocalContextBudgetPercent 是 local 档位把原生窗口折算成提示词预算的比例。
	LocalContextBudgetPercent = 60
	// LocalContextBudgetCap 防止 256K/1M 窗口把预算放大到超出本地模型有效上下文。
	LocalContextBudgetCap = 131072
	// MinLocalContextWindow 以下不建议运行带工具的 agent。
	MinLocalContextWindow = 16384
	// LocalTokenEstimateSafetyPercent 是 local 档位对 token 估算的保守放大比例。
	// 本地 tokenizer 与 tiktoken 不一致（CJK 尤甚），留出余量避免踩到运行时窗口。
	LocalTokenEstimateSafetyPercent = 110

	ContextWarningLocalWindowUnknown = "local_context_window_unknown"
	ContextWarningLocalWindowSmall   = "local_context_window_small"
	ContextWarningMaxTotalClamped    = "max_total_tokens_clamped"
)

// NormalizeContextProfile 只接受 auto（空）与 local；未知值按 auto 处理，避免配置拼写错误放大预算。
func NormalizeContextProfile(raw string) string {
	if strings.EqualFold(strings.TrimSpace(raw), "local") {
		return "local"
	}
	return ""
}

func (c OpenAIConfig) IsLocalContextProfile() bool {
	return NormalizeContextProfile(c.ContextProfile) == "local"
}

// TokenEstimateSafetyPercent 供 token 计数器使用：local 档位放大估算，auto 保持原值。
func (c OpenAIConfig) TokenEstimateSafetyPercent() int {
	if c.IsLocalContextProfile() {
		return LocalTokenEstimateSafetyPercent
	}
	return 100
}

// OutputLimitField 返回输出上限字段名。本地端点普遍只认旧字段 max_tokens。
func (c OpenAIConfig) OutputLimitField() string {
	if c.IsLocalContextProfile() {
		return "max_tokens"
	}
	return "max_completion_tokens"
}

// EffectiveContextWindow 返回用于预算计算的原生窗口；local 档位缺省时按保守值处理。
func (c OpenAIConfig) EffectiveContextWindow() int {
	if c.ContextWindow > 0 {
		return c.ContextWindow
	}
	if c.IsLocalContextProfile() {
		return DefaultLocalContextWindow
	}
	return 0
}

// EffectiveMaxTotalTokens 把 max_total_tokens 收紧到模型窗口内。
// 通用档位保留输出预留；local 档位再按 Effective Context 经验折到窗口的 60%，并限制在 128K 以内。
func (c OpenAIConfig) EffectiveMaxTotalTokens() int {
	configured := c.MaxTotalTokens
	if configured <= 0 {
		configured = DefaultMaxTotalTokens
	}
	window := c.EffectiveContextWindow()
	if window <= 0 {
		return configured
	}

	reserve := c.MaxCompletionTokensEffective()
	if reserve > window/2 {
		reserve = window / 2
	}
	hard := window - reserve
	if hard < 4096 {
		hard = window * 85 / 100
	}
	if hard < 1024 {
		hard = 1024
	}

	budget := configured
	if budget > hard {
		budget = hard
	}
	if c.IsLocalContextProfile() {
		local := window * LocalContextBudgetPercent / 100
		if local > LocalContextBudgetCap {
			local = LocalContextBudgetCap
		}
		if local < 8192 && window >= 8192 {
			local = 8192
		}
		if local > hard {
			local = hard
		}
		if budget > local {
			budget = local
		}
	}
	if budget < 1024 {
		budget = 1024
	}
	return budget
}

// ContextBudgetWarnings 返回可本地化的告警码；不直接拼自然语言，便于 UI 多语言与日志检索。
func (c OpenAIConfig) ContextBudgetWarnings() []string {
	var out []string
	if c.IsLocalContextProfile() {
		if c.ContextWindow <= 0 {
			out = append(out, ContextWarningLocalWindowUnknown)
		} else if c.ContextWindow < MinLocalContextWindow {
			out = append(out, ContextWarningLocalWindowSmall)
		}
	}
	configured := c.MaxTotalTokens
	if configured <= 0 {
		configured = DefaultMaxTotalTokens
	}
	if c.EffectiveContextWindow() > 0 && configured > c.EffectiveMaxTotalTokens() {
		out = append(out, ContextWarningMaxTotalClamped)
	}
	return out
}

// WithEffectiveContextBudget 返回运行期副本：预算与 local 档位中间件参数都在这里收口。
func (c *Config) WithEffectiveContextBudget() *Config {
	if c == nil {
		return nil
	}
	out := *c
	out.OpenAI.MaxTotalTokens = c.OpenAI.EffectiveMaxTotalTokens()
	out.MultiAgent.EinoMiddleware = EffectiveEinoMiddlewareForChannel(c.MultiAgent.EinoMiddleware, c.OpenAI)
	return &out
}

// EffectiveEinoMiddlewareForChannel 在 local 档位下只收紧参数，不放大用户已经设得更小的值。
// 档位是显式 opt-in；希望完全自定义时使用 auto。
func EffectiveEinoMiddlewareForChannel(mw MultiAgentEinoMiddlewareConfig, oa OpenAIConfig) MultiAgentEinoMiddlewareConfig {
	if !oa.IsLocalContextProfile() {
		return mw
	}
	window := oa.EffectiveContextWindow()
	budget := oa.EffectiveMaxTotalTokens()
	out := mw

	triggerRatio := mw.SummarizationTriggerRatioEffective()
	if triggerRatio > 0.85 {
		triggerRatio = 0.85
	}
	out.SummarizationTriggerRatio = triggerRatio

	out.SummarizationOutputReserveTokens = tightenInt(
		mw.SummarizationOutputReserveTokensEffective(),
		localSummaryOutputReserve(window, oa.MaxCompletionTokensEffective()),
	)
	out.ReductionMaxLengthForTrunc = tightenInt(
		mw.ReductionMaxLengthForTruncEffective(),
		localToolTruncBytes(window),
	)
	out.ReductionMaxTokensForClear = tightenInt(
		mw.ReductionMaxTokensForClearEffective(),
		localToolClearTokens(budget),
	)
	out.ReductionEnable = true
	out.ReductionSubAgents = true

	out.SummarizationUserIntentLedgerMaxRunes = tightenInt(
		mw.SummarizationUserIntentLedgerMaxRunesEffective(),
		localLedgerRunes(budget),
	)
	out.SummarizationUserIntentLedgerEntryMaxRunes = tightenInt(
		mw.SummarizationUserIntentLedgerEntryMaxRunesEffective(),
		localLedgerEntryRunes(out.SummarizationUserIntentLedgerMaxRunes),
	)

	out.LatestUserMessageMaxRunes = tightenInt(
		mw.LatestUserMessageMaxRunesEffective(),
		localLatestUserRunes(budget),
	)
	out.LatestUserMessageHeadRunes = tightenInt(
		mw.LatestUserMessageHeadRunesEffective(),
		out.LatestUserMessageMaxRunes/2,
	)
	out.LatestUserMessageTailRunes = tightenInt(
		mw.LatestUserMessageTailRunesEffective(),
		out.LatestUserMessageMaxRunes-out.LatestUserMessageHeadRunes,
	)

	out.ToolSearchEnable = true
	if mw.ToolSearchMinTools <= 0 || mw.ToolSearchMinTools > 8 {
		out.ToolSearchMinTools = 8
	}
	if mw.ToolSearchAlwaysVisible <= 0 || mw.ToolSearchAlwaysVisible > 8 {
		out.ToolSearchAlwaysVisible = 8
	}
	out.ToolSearchAlwaysVisibleTools = trimLocalAlwaysVisibleTools(mw.ToolSearchAlwaysVisibleTools)
	includeBuiltins := false
	out.ToolSearchIncludeBuiltins = &includeBuiltins

	out.PlanExecuteMaxStepResultRunes = tightenInt(
		mw.PlanExecuteMaxStepResultRunesEffective(),
		2000,
	)
	return out
}

func tightenInt(current, ceil int) int {
	if ceil <= 0 {
		return current
	}
	if current <= 0 || current > ceil {
		return ceil
	}
	return current
}

func localSummaryOutputReserve(window, maxCompletion int) int {
	reserve := window / 32
	if reserve < 2048 {
		reserve = 2048
	}
	if reserve > 8192 {
		reserve = 8192
	}
	if maxCompletion > 0 && reserve > maxCompletion {
		reserve = maxCompletion
	}
	return reserve
}

func localToolTruncBytes(window int) int {
	bytes := window / 6
	if bytes < 8192 {
		bytes = 8192
	}
	if bytes > 32768 {
		bytes = 32768
	}
	if window > 0 && bytes > window {
		bytes = window
	}
	return bytes
}

func localToolClearTokens(budget int) int {
	tokens := budget * 35 / 100
	if tokens < 4096 {
		tokens = 4096
	}
	if budget > 0 && tokens > budget {
		tokens = budget
	}
	return tokens
}

func localLedgerRunes(budget int) int {
	runes := budget * 2
	if runes > DefaultSummarizationUserIntentLedgerMaxRunes {
		runes = DefaultSummarizationUserIntentLedgerMaxRunes
	}
	if runes < 4096 {
		runes = 4096
	}
	return runes
}

func localLedgerEntryRunes(ledgerMax int) int {
	runes := ledgerMax / 6
	if runes > DefaultSummarizationUserIntentLedgerEntryMaxRunes {
		runes = DefaultSummarizationUserIntentLedgerEntryMaxRunes
	}
	if runes < 1024 {
		runes = 1024
	}
	return runes
}

func localLatestUserRunes(budget int) int {
	runes := budget * 2
	if runes > DefaultLatestUserMessageMaxRunes {
		runes = DefaultLatestUserMessageMaxRunes
	}
	if runes < 4096 {
		runes = 4096
	}
	return runes
}

// localCoreToolNames 是本地小窗口下值得常驻的工具；其余交给 tool_search 按需解锁。
var localCoreToolNames = map[string]struct{}{
	"read_file":             {},
	"glob":                  {},
	"grep":                  {},
	"write_file":            {},
	"edit_file":             {},
	"execute":               {},
	"exec":                  {},
	"task":                  {},
	"transfer_to_agent":     {},
	"exit":                  {},
	"write_todos":           {},
	"skill":                 {},
	"tool_search":           {},
	"record_vulnerability":  {},
	"get_tool_execution":    {},
	"wait_tool_execution":   {},
	"analyze_image":         {},
	"search_knowledge_base": {},
	"http-framework-test":   {},
}

func trimLocalAlwaysVisibleTools(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(localCoreToolNames))
	seen := make(map[string]struct{}, len(in))
	for _, raw := range in {
		name := strings.TrimSpace(raw)
		key := strings.ToLower(name)
		if _, ok := localCoreToolNames[key]; !ok {
			continue
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, name)
	}
	return out
}
