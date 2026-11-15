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

	ContextWarningLocalWindowUnknown   = "local_context_window_unknown"
	ContextWarningLocalWindowSmall     = "local_context_window_small"
	ContextWarningMaxTotalClamped      = "max_total_tokens_clamped"
	ContextWarningMaxCompletionClamped = "max_completion_tokens_clamped"
)

// NormalizeContextProfile returns an explicit profile, or empty for auto.
func NormalizeContextProfile(raw string) string {
	switch profile := strings.ToLower(strings.TrimSpace(raw)); profile {
	case "local", "local_auto", "economy", "custom":
		return profile
	}
	return ""
}

// IsEconomyContextProfile enables input savings for DeepSeek by default. An
// explicit custom/local profile takes precedence, including on older channels.
func (c OpenAIConfig) IsEconomyContextProfile() bool {
	switch NormalizeContextProfile(c.ContextProfile) {
	case "economy":
		return true
	case "custom", "local", "local_auto":
		return false
	}
	profile := c.Reasoning.ProfileEffective()
	// Model-name detection only selects input savings. The gateway's wire
	// protocol and reasoning parameters still follow its provider profile.
	return c.IsDeepSeekOfficialEndpoint() || profile == "deepseek" || profile == "deepseek_compat" ||
		strings.Contains(strings.ToLower(c.Model), "deepseek")
}

func (c OpenAIConfig) IsLocalContextProfile() bool {
	profile := NormalizeContextProfile(c.ContextProfile)
	return profile == "local" || profile == "local_auto"
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
	if window > 0 && budget > window {
		budget = window
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
	if c.EffectiveContextWindow() > 0 && c.MaxCompletionTokens > c.MaxCompletionTokensEffective() {
		out = append(out, ContextWarningMaxCompletionClamped)
	}
	return out
}

// EffectiveRuntimeForChannel applies input-saving or local-model limits to a
// runtime copy. Explicit custom profiles use the configured runtime settings.
func EffectiveRuntimeForChannel(mw AgentTuningConfig, oa OpenAIConfig) AgentTuningConfig {
	if oa.IsEconomyContextProfile() {
		out := mw
		out.ToolSearchEnable = true
		out.ToolSearchMinTools = tightenInt(mw.ToolSearchMinTools, 16)
		out.ToolSearchAlwaysVisible = tightenInt(mw.ToolSearchAlwaysVisible, 6)
		includeBuiltins := false
		out.ToolSearchIncludeBuiltins = &includeBuiltins
		out.ReductionEnable = true
		out.ReductionMaxLengthForTrunc = tightenInt(mw.ReductionMaxLengthForTruncEffective(), 8000)
		return out
	}
	if !oa.IsLocalContextProfile() {
		return mw
	}
	window := oa.EffectiveContextWindow()
	out := mw

	out.ReductionMaxLengthForTrunc = tightenInt(
		mw.ReductionMaxLengthForTruncEffective(),
		localToolTruncBytes(window),
	)
	out.ReductionEnable = true

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
