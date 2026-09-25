package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"cyberstrike-ai/internal/termout"
	"cyberstrike-ai/internal/toolguard"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Version     string                `yaml:"version,omitempty" json:"version,omitempty"` // 前端显示的版本号，如 v1.3.3
	Server      ServerConfig          `yaml:"server"`
	Log         LogConfig             `yaml:"log"`
	MCP         MCPConfig             `yaml:"mcp"`
	AI          AIConfig              `yaml:"ai,omitempty" json:"ai,omitempty"`
	OpenAI      OpenAIConfig          `yaml:"openai,omitempty" json:"openai,omitempty"`
	FOFA        FofaConfig            `yaml:"fofa,omitempty" json:"fofa,omitempty"`
	ZoomEye     SpaceSearchConfig     `yaml:"zoomeye,omitempty" json:"zoomeye,omitempty"`
	Quake       SpaceSearchConfig     `yaml:"quake,omitempty" json:"quake,omitempty"`
	Shodan      SpaceSearchConfig     `yaml:"shodan,omitempty" json:"shodan,omitempty"`
	Agent       AgentConfig           `yaml:"agent"`
	Hitl        HitlConfig            `yaml:"hitl,omitempty" json:"hitl,omitempty"`
	ToolGuard   *toolguard.Config     `yaml:"tool_guard,omitempty" json:"tool_guard,omitempty"`
	Security    SecurityConfig        `yaml:"security"`
	Database    DatabaseConfig        `yaml:"database"`
	Auth        AuthConfig            `yaml:"auth"`
	Audit       AuditConfig           `yaml:"audit,omitempty" json:"audit,omitempty"`
	Monitor     MonitorConfig         `yaml:"monitor,omitempty" json:"monitor,omitempty"`
	Storage     StorageConfig         `yaml:"storage,omitempty" json:"storage,omitempty"`
	ExternalMCP ExternalMCPConfig     `yaml:"external_mcp,omitempty"`
	Knowledge   KnowledgeConfig       `yaml:"knowledge,omitempty"`
	C2          C2Config              `yaml:"c2,omitempty" json:"c2,omitempty"`                 // 内置 C2 总开关；未配置时默认启用
	Robots      RobotsConfig          `yaml:"robots,omitempty" json:"robots,omitempty"`         // 企业微信/钉钉/飞书等机器人配置
	RolesDir    string                `yaml:"roles_dir,omitempty" json:"roles_dir,omitempty"`   // 角色配置文件目录（新方式）
	Roles       map[string]RoleConfig `yaml:"roles,omitempty" json:"roles,omitempty"`           // 向后兼容：支持在主配置文件中定义角色
	SkillsDir   string                `yaml:"skills_dir,omitempty" json:"skills_dir,omitempty"` // Skills配置文件目录
	AgentsDir   string                `yaml:"agents_dir,omitempty" json:"agents_dir,omitempty"` // 多代理子 Agent Markdown 定义目录（*.md，YAML front matter）
	MultiAgent  MultiAgentConfig      `yaml:"multi_agent,omitempty" json:"multi_agent,omitempty"`
	Project     ProjectConfig         `yaml:"project,omitempty" json:"project,omitempty"`
	Vision      VisionConfig          `yaml:"vision,omitempty" json:"vision,omitempty"`
}

type EnsureLocalConfigResult struct {
	Created     bool
	ExamplePath string
}

const (
	DefaultMaxCompletionTokens                        = 16384
	DefaultSummarizationUserIntentLedgerMaxRunes      = 96000
	DefaultSummarizationUserIntentLedgerEntryMaxRunes = 16000
	DefaultLatestUserMessageMaxRunes                  = 48000
	DefaultLatestUserMessageHeadRunes                 = 24000
	DefaultLatestUserMessageTailRunes                 = 24000
	DefaultSummarizationOutputReserveTokens           = 40960
)

// ProjectConfig 项目黑板（跨对话共享事实）配置。
type ProjectConfig struct {
	Enabled                 bool   `yaml:"enabled" json:"enabled"`
	DefaultProjectID        string `yaml:"default_project_id,omitempty" json:"default_project_id,omitempty"` // 机器人/批量等无显式项目时绑定的默认项目
	FactIndexMaxRunes       int    `yaml:"fact_index_max_runes,omitempty" json:"fact_index_max_runes,omitempty"`
	FactIndexPathMaxRunes   int    `yaml:"fact_index_path_max_runes,omitempty" json:"fact_index_path_max_runes,omitempty"`
	FactSummaryMaxRunes     int    `yaml:"fact_summary_max_runes,omitempty" json:"fact_summary_max_runes,omitempty"`
	DefaultInjectDeprecated bool   `yaml:"default_inject_deprecated,omitempty" json:"default_inject_deprecated,omitempty"`
}

// FactIndexMaxRunesEffective 自动注入黑板索引的最大 rune 数。
func (c ProjectConfig) FactIndexMaxRunesEffective() int {
	if c.FactIndexMaxRunes <= 0 {
		return 3500
	}
	return c.FactIndexMaxRunes
}

// FactIndexPathMaxRunesEffective 攻击路径速览段的最大 rune 数（从 fact_index_max_runes 预算中预留）。
func (c ProjectConfig) FactIndexPathMaxRunesEffective() int {
	if c.FactIndexPathMaxRunes <= 0 {
		return 1000
	}
	return c.FactIndexPathMaxRunes
}

// FactSummaryMaxRunesEffective upsert 时 summary 最大 rune 数（索引一行，宜含验证要点）。
func (c ProjectConfig) FactSummaryMaxRunesEffective() int {
	if c.FactSummaryMaxRunes <= 0 {
		return 200
	}
	return c.FactSummaryMaxRunes
}

// MultiAgentConfig 基于 CloudWeGo Eino adk/prebuilt 的多代理编排（deep | plan_execute | supervisor）。
type MultiAgentConfig struct {
	Enabled               bool   `yaml:"enabled" json:"enabled"`
	RobotDefaultAgentMode string `yaml:"robot_default_agent_mode,omitempty" json:"robot_default_agent_mode,omitempty"` // eino_single | deep | plan_execute | supervisor
	BatchUseMultiAgent    bool   `yaml:"batch_use_multi_agent" json:"batch_use_multi_agent"`                           // 为 true 时批量任务队列中每子任务走 Eino 多代理
	// Orchestration 已弃用：保留仅兼容旧版 config.yaml；编排由聊天/WebShell 请求体 orchestration 决定，未传时按 deep。
	Orchestration string `yaml:"orchestration,omitempty" json:"orchestration,omitempty"`
	// MaxIteration 已废弃：统一使用 agent.max_iterations（YAML 中保留字段仅为兼容旧配置，运行时不读取）。
	MaxIteration int `yaml:"max_iteration,omitempty" json:"max_iteration,omitempty"`
	// PlanExecuteLoopMaxIterations plan_execute 模式下 execute↔replan 外层循环上限；0 表示用 Eino 默认 10。
	PlanExecuteLoopMaxIterations int `yaml:"plan_execute_loop_max_iterations,omitempty" json:"plan_execute_loop_max_iterations,omitempty"`
	// SubAgentMaxIterations 已废弃：子代理与主代理均使用 agent.max_iterations（Markdown max_iterations>0 可覆盖）。
	SubAgentMaxIterations   int    `yaml:"sub_agent_max_iterations,omitempty" json:"sub_agent_max_iterations,omitempty"`
	WithoutGeneralSubAgent  bool   `yaml:"without_general_sub_agent" json:"without_general_sub_agent"`
	WithoutWriteTodos       bool   `yaml:"without_write_todos" json:"without_write_todos"`
	OrchestratorInstruction string `yaml:"orchestrator_instruction" json:"orchestrator_instruction"`
	// OrchestratorInstructionPlanExecute plan_execute 主代理（规划侧）系统提示；非空且 agents/orchestrator-plan-execute.md 正文为空或未存在时生效。不与 Deep 的 orchestrator_instruction 混用。
	OrchestratorInstructionPlanExecute string `yaml:"orchestrator_instruction_plan_execute,omitempty" json:"orchestrator_instruction_plan_execute,omitempty"`
	// OrchestratorInstructionSupervisor supervisor 主代理系统提示（transfer/exit 说明仍由运行追加）；非空且 agents/orchestrator-supervisor.md 正文为空或未存在时生效。
	OrchestratorInstructionSupervisor string                `yaml:"orchestrator_instruction_supervisor,omitempty" json:"orchestrator_instruction_supervisor,omitempty"`
	SubAgents                         []MultiAgentSubConfig `yaml:"sub_agents" json:"sub_agents"`
	// SubAgentUserContextMaxRunes caps user-context supplement for sub-agent task descriptions.
	// 0 (default) preserves all user turns verbatim; >0 caps total runes; negative disables injection.
	SubAgentUserContextMaxRunes int `yaml:"sub_agent_user_context_max_runes,omitempty" json:"sub_agent_user_context_max_runes,omitempty"`
	// EinoSkills configures CloudWeGo Eino ADK skill middleware + optional local filesystem/execute on DeepAgent.
	EinoSkills MultiAgentEinoSkillsConfig `yaml:"eino_skills,omitempty" json:"eino_skills,omitempty"`
	// EinoMiddleware wires optional ADK middleware (patchtoolcalls, toolsearch, plantask, reduction) and Deep extras.
	EinoMiddleware MultiAgentEinoMiddlewareConfig `yaml:"eino_middleware,omitempty" json:"eino_middleware,omitempty"`
	// EinoCallbacks attaches CloudWeGo eino callbacks.InitCallbacks on ADK Runner context (structured logs + optional SSE trace).
	EinoCallbacks MultiAgentEinoCallbacksConfig `yaml:"eino_callbacks,omitempty" json:"eino_callbacks,omitempty"`
}

// SubAgentUserContextMaxRunesEffective returns max runes for sub-agent task supplement; 0 = unlimited; negative = disabled.
func (c MultiAgentConfig) SubAgentUserContextMaxRunesEffective() int {
	return c.SubAgentUserContextMaxRunes
}

// MultiAgentEinoCallbacksConfig enables Eino unified callbacks on each ADK agent run (deep / plan_execute / supervisor / eino_single).
// Modes: log_only (zap + optional OTel; no SSE to browser), sse (adds client SSE eino_trace_* when sse_trace_to_client), full (sse rules + stream callback copies closed).
type MultiAgentEinoCallbacksConfig struct {
	Enabled bool   `yaml:"enabled" json:"enabled"`
	Mode    string `yaml:"mode,omitempty" json:"mode,omitempty"` // log_only | sse | full; empty with enabled=true defaults to log_only
	// SseTraceToClient when true emits eino_trace_* SSE for UI (use only for admin/debug; nil/false recommended in production).
	SseTraceToClient *bool `yaml:"sse_trace_to_client,omitempty" json:"sse_trace_to_client,omitempty"`
	// Otel configures OpenTelemetry trace export (independent of mode; exporter none disables export even if enabled).
	Otel MultiAgentEinoCallbacksOtelConfig `yaml:"otel,omitempty" json:"otel,omitempty"`
	// MaxInputSummaryRunes / MaxOutputSummaryRunes cap text placed in SSE payloads and debug logs (not full payloads).
	MaxInputSummaryRunes  int `yaml:"max_input_summary_runes,omitempty" json:"max_input_summary_runes,omitempty"`
	MaxOutputSummaryRunes int `yaml:"max_output_summary_runes,omitempty" json:"max_output_summary_runes,omitempty"`
	// ZapVerbose when true logs input/output summaries at zap.Debug on start/end; false uses Info with short fields only.
	ZapVerbose bool `yaml:"zap_verbose,omitempty" json:"zap_verbose,omitempty"`
}

// MultiAgentEinoCallbacksOtelConfig OpenTelemetry for Eino callback spans (W3C trace in collector / stdout).
type MultiAgentEinoCallbacksOtelConfig struct {
	Enabled      bool    `yaml:"enabled" json:"enabled"`
	ServiceName  string  `yaml:"service_name,omitempty" json:"service_name,omitempty"`
	Exporter     string  `yaml:"exporter,omitempty" json:"exporter,omitempty"`           // none | stdout | otlphttp
	OTLPEndpoint string  `yaml:"otlp_endpoint,omitempty" json:"otlp_endpoint,omitempty"` // host:port, e.g. localhost:4318 (path /v1/traces)
	SampleRatio  float64 `yaml:"sample_ratio,omitempty" json:"sample_ratio,omitempty"`   // 0–1, default 1.0
}

// EinoCallbacksModeEffective returns off | log_only | sse | full.
func (c MultiAgentEinoCallbacksConfig) EinoCallbacksModeEffective() string {
	if !c.Enabled {
		return "off"
	}
	m := strings.TrimSpace(strings.ToLower(c.Mode))
	switch m {
	case "log_only":
		return "log_only"
	case "sse":
		return "sse"
	case "full":
		return "full"
	case "":
		return "log_only"
	default:
		return "log_only"
	}
}

// SseTraceToClientEffective is false unless explicitly set true (best practice: do not expose framework traces to end users by default).
func (c MultiAgentEinoCallbacksConfig) SseTraceToClientEffective() bool {
	if c.SseTraceToClient == nil {
		return false
	}
	return *c.SseTraceToClient
}

// ShouldEmitEinoTraceSSE is true when client-visible trace events should be sent over progress/SSE.
func (c MultiAgentEinoCallbacksConfig) ShouldEmitEinoTraceSSE(mode string) bool {
	if !c.SseTraceToClientEffective() {
		return false
	}
	return mode == "sse" || mode == "full"
}

// OtelExporterEffective returns none | stdout | otlphttp.
func (c MultiAgentEinoCallbacksOtelConfig) OtelExporterEffective() string {
	e := strings.TrimSpace(strings.ToLower(c.Exporter))
	switch e {
	case "none", "stdout", "otlphttp":
		return e
	case "":
		if c.Enabled {
			return "stdout"
		}
		return "none"
	default:
		return "none"
	}
}

// OtelTracingActive is true when spans should be started (enabled + non-none exporter).
func (c MultiAgentEinoCallbacksConfig) OtelTracingActive() bool {
	if !c.Otel.Enabled {
		return false
	}
	return c.Otel.OtelExporterEffective() != "none"
}

func (c MultiAgentEinoCallbacksOtelConfig) ServiceNameEffective() string {
	s := strings.TrimSpace(c.ServiceName)
	if s != "" {
		return s
	}
	return "cyberstrike-ai"
}

func (c MultiAgentEinoCallbacksOtelConfig) SampleRatioEffective() float64 {
	r := c.SampleRatio
	if r <= 0 {
		return 1.0
	}
	if r > 1 {
		return 1.0
	}
	return r
}

func (c MultiAgentEinoCallbacksConfig) EinoCallbacksMaxInputSummaryRunes() int {
	if c.MaxInputSummaryRunes > 0 {
		return c.MaxInputSummaryRunes
	}
	return 400
}

func (c MultiAgentEinoCallbacksConfig) EinoCallbacksMaxOutputSummaryRunes() int {
	if c.MaxOutputSummaryRunes > 0 {
		return c.MaxOutputSummaryRunes
	}
	return 400
}

// MultiAgentEinoMiddlewareConfig optional Eino ADK middleware and Deep / supervisor tuning.
type MultiAgentEinoMiddlewareConfig struct {
	// PatchToolCalls inserts placeholder tool results for dangling assistant tool_calls (nil = enabled).
	PatchToolCalls *bool `yaml:"patch_tool_calls,omitempty" json:"patch_tool_calls,omitempty"`
	// ToolSearch enables dynamictool/toolsearch: hide tail tools until model calls tool_search (reduces prompt tools).
	ToolSearchEnable        bool `yaml:"tool_search_enable,omitempty" json:"tool_search_enable,omitempty"`
	ToolSearchMinTools      int  `yaml:"tool_search_min_tools,omitempty" json:"tool_search_min_tools,omitempty"`           // default 20; applies when len(tools) >= this
	ToolSearchAlwaysVisible int  `yaml:"tool_search_always_visible,omitempty" json:"tool_search_always_visible,omitempty"` // default 12; first N tools stay always visible
	// ToolSearchAlwaysVisibleTools keeps specified tool names always visible (never hidden by tool_search).
	ToolSearchAlwaysVisibleTools []string `yaml:"tool_search_always_visible_tools,omitempty" json:"tool_search_always_visible_tools,omitempty"`
	// Plantask adds TaskCreate/Get/Update/List (file-backed under skills dir); requires eino_skills + local backend.
	PlantaskEnable bool `yaml:"plantask_enable,omitempty" json:"plantask_enable,omitempty"`
	// PlantaskRelDir relative to skills_dir for per-conversation task boards (default .eino/plantask).
	PlantaskRelDir string `yaml:"plantask_rel_dir,omitempty" json:"plantask_rel_dir,omitempty"`
	// Reduction truncates/offloads large tool outputs (requires eino local backend for Write).
	ReductionEnable            bool     `yaml:"reduction_enable,omitempty" json:"reduction_enable,omitempty"`
	ReductionRootDir           string   `yaml:"reduction_root_dir,omitempty" json:"reduction_root_dir,omitempty"`                         // 非空：落盘根目录（默认 tmp/reduction）；其下按 projects/{id} 或 conversations/{id} 隔离
	ReductionMaxLengthForTrunc int      `yaml:"reduction_max_length_for_trunc,omitempty" json:"reduction_max_length_for_trunc,omitempty"` // default 12000
	ReductionMaxTokensForClear int      `yaml:"reduction_max_tokens_for_clear,omitempty" json:"reduction_max_tokens_for_clear,omitempty"` // default 50000
	ReductionClearExclude      []string `yaml:"reduction_clear_exclude,omitempty" json:"reduction_clear_exclude,omitempty"`
	ReductionSubAgents         bool     `yaml:"reduction_sub_agents,omitempty" json:"reduction_sub_agents,omitempty"` // also attach to sub-agents
	// SummarizationTriggerRatio controls summarization trigger threshold as max_total_tokens * ratio (default 0.8).
	SummarizationTriggerRatio float64 `yaml:"summarization_trigger_ratio,omitempty" json:"summarization_trigger_ratio,omitempty"`
	// SummarizationOutputReserveTokens reserves completion headroom for the summarization model call (default 40960).
	SummarizationOutputReserveTokens int `yaml:"summarization_output_reserve_tokens,omitempty" json:"summarization_output_reserve_tokens,omitempty"`
	// SummarizationEmitInternalEvents controls middleware internal event emission (default true).
	SummarizationEmitInternalEvents *bool `yaml:"summarization_emit_internal_events,omitempty" json:"summarization_emit_internal_events,omitempty"`
	// SummarizationUserIntentLedgerMaxRunes caps the DB-backed immutable user input ledger injected into model context.
	SummarizationUserIntentLedgerMaxRunes int `yaml:"summarization_user_intent_ledger_max_runes,omitempty" json:"summarization_user_intent_ledger_max_runes,omitempty"`
	// SummarizationUserIntentLedgerEntryMaxRunes caps each user message entry inside the immutable user input ledger.
	SummarizationUserIntentLedgerEntryMaxRunes int `yaml:"summarization_user_intent_ledger_entry_max_runes,omitempty" json:"summarization_user_intent_ledger_entry_max_runes,omitempty"`
	// LatestUserMessageMaxRunes caps the current user turn inserted into model context; full text is persisted as an artifact when capped.
	LatestUserMessageMaxRunes int `yaml:"latest_user_message_max_runes,omitempty" json:"latest_user_message_max_runes,omitempty"`
	// LatestUserMessageHeadRunes keeps the head preview for an oversized current user turn.
	LatestUserMessageHeadRunes int `yaml:"latest_user_message_head_runes,omitempty" json:"latest_user_message_head_runes,omitempty"`
	// LatestUserMessageTailRunes keeps the tail preview for an oversized current user turn.
	LatestUserMessageTailRunes int `yaml:"latest_user_message_tail_runes,omitempty" json:"latest_user_message_tail_runes,omitempty"`
	// SummarizationRetryMaxAttempts 已废弃：summarization 与 Eino 原生 ModelRetry 共用 model_retry_max_retries 及 isEinoTransientRunError。
	SummarizationRetryMaxAttempts int `yaml:"summarization_retry_max_attempts,omitempty" json:"summarization_retry_max_attempts,omitempty"`
	// PlanExecuteUserInputBudgetRatio caps planner/replanner/executor userInput prompt budget ratio (default 0.35).
	PlanExecuteUserInputBudgetRatio float64 `yaml:"plan_execute_user_input_budget_ratio,omitempty" json:"plan_execute_user_input_budget_ratio,omitempty"`
	// PlanExecuteExecutedStepsBudgetRatio caps executed_steps prompt budget ratio (default 0.2).
	PlanExecuteExecutedStepsBudgetRatio float64 `yaml:"plan_execute_executed_steps_budget_ratio,omitempty" json:"plan_execute_executed_steps_budget_ratio,omitempty"`
	// PlanExecuteMaxStepResultRunes caps each executed step result length for prompt view (default 4000).
	PlanExecuteMaxStepResultRunes int `yaml:"plan_execute_max_step_result_runes,omitempty" json:"plan_execute_max_step_result_runes,omitempty"`
	// PlanExecuteKeepLastSteps keeps only the tail steps in prompt view (default 8).
	PlanExecuteKeepLastSteps int `yaml:"plan_execute_keep_last_steps,omitempty" json:"plan_execute_keep_last_steps,omitempty"`
	// CheckpointDir is retained for config compatibility. Chat agent runs do
	// not consume it; cross-turn recovery is centralized in conversations.last_react_*.
	CheckpointDir string `yaml:"checkpoint_dir,omitempty" json:"checkpoint_dir,omitempty"`
	// DeepOutputKey passed to deep.Config OutputKey (session final text); empty = off.
	DeepOutputKey string `yaml:"deep_output_key,omitempty" json:"deep_output_key,omitempty"`
	// DeepModelRetryMaxRetries 已废弃：请用 model_retry_max_retries；保留字段仅为兼容旧配置。
	DeepModelRetryMaxRetries int `yaml:"deep_model_retry_max_retries,omitempty" json:"deep_model_retry_max_retries,omitempty"`
	// ModelRetryMaxRetries configures Eino ADK native ChatModel retry attempts; 0=default 4.
	ModelRetryMaxRetries int `yaml:"model_retry_max_retries,omitempty" json:"model_retry_max_retries,omitempty"`
	// ModelRetryMaxBackoffSec caps native model retry backoff seconds; 0=default 30.
	ModelRetryMaxBackoffSec int `yaml:"model_retry_max_backoff_sec,omitempty" json:"model_retry_max_backoff_sec,omitempty"`
	// ModelFailoverChannels lists ai.channels IDs to try after native model retry is exhausted.
	ModelFailoverChannels []string `yaml:"model_failover_channels,omitempty" json:"model_failover_channels,omitempty"`
	// ModelFailoverMaxRetries caps distinct failover channel attempts; 0=all configured failover channels.
	ModelFailoverMaxRetries int `yaml:"model_failover_max_retries,omitempty" json:"model_failover_max_retries,omitempty"`
	// RunRetryMaxAttempts 已废弃：模型临时错误由 Eino 原生 ModelRetry 处理；仅保留给非模型层 run loop 兜底与 summarization 旧字段。
	RunRetryMaxAttempts int `yaml:"run_retry_max_attempts,omitempty" json:"run_retry_max_attempts,omitempty"`
	// RunRetryMaxBackoffSec 已废弃：请用 model_retry_max_backoff_sec；仅保留给非模型层 run loop 兜底与 summarization 旧字段。
	RunRetryMaxBackoffSec int `yaml:"run_retry_max_backoff_sec,omitempty" json:"run_retry_max_backoff_sec,omitempty"`
	// EmptyResponseContinueMaxAttempts Run 成功但未捕获助手正文时 Handler 层退避续跑次数；0=默认 5。
	EmptyResponseContinueMaxAttempts int `yaml:"empty_response_continue_max_attempts,omitempty" json:"empty_response_continue_max_attempts,omitempty"`
	// TaskToolDescriptionPrefix when non-empty sets deep.Config TaskToolDescriptionGenerator (sub-agent names appended).
	TaskToolDescriptionPrefix string `yaml:"task_tool_description_prefix,omitempty" json:"task_tool_description_prefix,omitempty"`
}

func (c MultiAgentEinoMiddlewareConfig) SummarizationTriggerRatioEffective() float64 {
	v := c.SummarizationTriggerRatio
	if v <= 0 {
		return 0.8
	}
	if v < 0.5 {
		return 0.5
	}
	if v > 0.95 {
		return 0.95
	}
	return v
}

func (c MultiAgentEinoMiddlewareConfig) SummarizationOutputReserveTokensEffective() int {
	if c.SummarizationOutputReserveTokens > 0 {
		return c.SummarizationOutputReserveTokens
	}
	return DefaultSummarizationOutputReserveTokens
}

func (c MultiAgentEinoMiddlewareConfig) SummarizationEmitInternalEventsEffective() bool {
	if c.SummarizationEmitInternalEvents != nil {
		return *c.SummarizationEmitInternalEvents
	}
	return true
}

func (c MultiAgentEinoMiddlewareConfig) SummarizationUserIntentLedgerMaxRunesEffective() int {
	if c.SummarizationUserIntentLedgerMaxRunes > 0 {
		return c.SummarizationUserIntentLedgerMaxRunes
	}
	return DefaultSummarizationUserIntentLedgerMaxRunes
}

func (c MultiAgentEinoMiddlewareConfig) SummarizationUserIntentLedgerEntryMaxRunesEffective() int {
	if c.SummarizationUserIntentLedgerEntryMaxRunes > 0 {
		return c.SummarizationUserIntentLedgerEntryMaxRunes
	}
	return DefaultSummarizationUserIntentLedgerEntryMaxRunes
}

func (c MultiAgentEinoMiddlewareConfig) LatestUserMessageMaxRunesEffective() int {
	if c.LatestUserMessageMaxRunes > 0 {
		return c.LatestUserMessageMaxRunes
	}
	return DefaultLatestUserMessageMaxRunes
}

func (c MultiAgentEinoMiddlewareConfig) LatestUserMessageHeadRunesEffective() int {
	if c.LatestUserMessageHeadRunes > 0 {
		return c.LatestUserMessageHeadRunes
	}
	return DefaultLatestUserMessageHeadRunes
}

func (c MultiAgentEinoMiddlewareConfig) LatestUserMessageTailRunesEffective() int {
	if c.LatestUserMessageTailRunes > 0 {
		return c.LatestUserMessageTailRunes
	}
	return DefaultLatestUserMessageTailRunes
}

func (c MultiAgentEinoMiddlewareConfig) PlanExecuteUserInputBudgetRatioEffective() float64 {
	v := c.PlanExecuteUserInputBudgetRatio
	if v <= 0 {
		return 0.35
	}
	if v < 0.1 {
		return 0.1
	}
	if v > 0.6 {
		return 0.6
	}
	return v
}

func (c MultiAgentEinoMiddlewareConfig) PlanExecuteExecutedStepsBudgetRatioEffective() float64 {
	v := c.PlanExecuteExecutedStepsBudgetRatio
	if v <= 0 {
		return 0.2
	}
	if v < 0.08 {
		return 0.08
	}
	if v > 0.5 {
		return 0.5
	}
	return v
}

func (c MultiAgentEinoMiddlewareConfig) PlanExecuteMaxStepResultRunesEffective() int {
	if c.PlanExecuteMaxStepResultRunes > 0 {
		return c.PlanExecuteMaxStepResultRunes
	}
	return 4000
}

func (c MultiAgentEinoMiddlewareConfig) PlanExecuteKeepLastStepsEffective() int {
	if c.PlanExecuteKeepLastSteps > 0 {
		return c.PlanExecuteKeepLastSteps
	}
	return 8
}

func (c MultiAgentEinoMiddlewareConfig) ReductionMaxLengthForTruncEffective() int {
	if c.ReductionMaxLengthForTrunc > 0 {
		return c.ReductionMaxLengthForTrunc
	}
	return 12000
}

func (c MultiAgentEinoMiddlewareConfig) ReductionMaxTokensForClearEffective() int {
	if c.ReductionMaxTokensForClear > 0 {
		return c.ReductionMaxTokensForClear
	}
	return 50000
}

// MultiAgentEinoSkillsConfig toggles Eino official skill progressive disclosure and host filesystem tools.
type MultiAgentEinoSkillsConfig struct {
	// Disable skips skill middleware (and does not attach local FS tools for Deep).
	Disable bool `yaml:"disable" json:"disable"`
	// FilesystemTools registers read_file/glob/grep/write/edit/execute (eino-ext local backend). Nil/omitted = true.
	FilesystemTools *bool `yaml:"filesystem_tools,omitempty" json:"filesystem_tools,omitempty"`
	// SkillToolName overrides the default Eino tool name "skill".
	SkillToolName string `yaml:"skill_tool_name,omitempty" json:"skill_tool_name,omitempty"`
}

// EinoSkillFilesystemToolsEffective returns whether Deep/sub-agents should attach local filesystem + streaming shell.
func (c MultiAgentEinoSkillsConfig) EinoSkillFilesystemToolsEffective() bool {
	if c.FilesystemTools != nil {
		return *c.FilesystemTools
	}
	return true
}

// PatchToolCallsEffective returns whether patchtoolcalls middleware should run (default true).
func (c MultiAgentEinoMiddlewareConfig) PatchToolCallsEffective() bool {
	if c.PatchToolCalls != nil {
		return *c.PatchToolCalls
	}
	return true
}

// MultiAgentSubConfig 子代理（Eino ChatModelAgent）：deep 下由 task 调度；supervisor 下由 transfer 委派；plan_execute 不使用子代理列表。
type MultiAgentSubConfig struct {
	ID            string   `yaml:"id" json:"id"`
	Name          string   `yaml:"name" json:"name"`
	Description   string   `yaml:"description" json:"description"`
	Instruction   string   `yaml:"instruction" json:"instruction"`
	BindRole      string   `yaml:"bind_role,omitempty" json:"bind_role,omitempty"` // 可选：关联主配置 roles 中的角色名；未配 role_tools 时沿用该角色的 tools
	RoleTools     []string `yaml:"role_tools" json:"role_tools"`                   // 与单 Agent 角色工具相同 key；空表示全部工具（bind_role 可补全 tools）
	MaxIterations int      `yaml:"max_iterations" json:"max_iterations"`
	Kind          string   `yaml:"kind,omitempty" json:"kind,omitempty"` // 仅 Markdown：kind=orchestrator 表示 Deep 主代理（与 orchestrator.md 二选一约定）
}

// MultiAgentPublic 返回给前端的精简信息（不含子代理指令全文）。
type MultiAgentPublic struct {
	Enabled                                    bool     `json:"enabled"`
	RobotDefaultAgentMode                      string   `json:"robot_default_agent_mode,omitempty"`
	BatchUseMultiAgent                         bool     `json:"batch_use_multi_agent"`
	SubAgentCount                              int      `json:"sub_agent_count"`
	Orchestration                              string   `json:"orchestration,omitempty"`
	PlanExecuteLoopMaxIterations               int      `json:"plan_execute_loop_max_iterations"`
	SummarizationUserIntentLedgerMaxRunes      int      `json:"summarization_user_intent_ledger_max_runes"`
	SummarizationUserIntentLedgerEntryMaxRunes int      `json:"summarization_user_intent_ledger_entry_max_runes"`
	LatestUserMessageMaxRunes                  int      `json:"latest_user_message_max_runes"`
	LatestUserMessageHeadRunes                 int      `json:"latest_user_message_head_runes"`
	LatestUserMessageTailRunes                 int      `json:"latest_user_message_tail_runes"`
	ModelRetryMaxRetries                       int      `json:"model_retry_max_retries"`
	ModelRetryMaxBackoffSec                    int      `json:"model_retry_max_backoff_sec"`
	ModelFailoverChannels                      []string `json:"model_failover_channels,omitempty"`
	ModelFailoverMaxRetries                    int      `json:"model_failover_max_retries"`
	ToolSearchAlwaysVisibleTools               []string `json:"tool_search_always_visible_tools,omitempty"`
	ToolSearchAlwaysVisibleEffectiveTools      []string `json:"tool_search_always_visible_effective_tools,omitempty"`
}

// NormalizeAgentMode 解析代理模式（eino_single | deep | plan_execute | supervisor）；空值默认 eino_single。
func NormalizeAgentMode(mode string) string {
	s := strings.TrimSpace(strings.ToLower(mode))
	switch s {
	case "", "eino_single":
		return "eino_single"
	case "deep":
		return "deep"
	case "plan_execute", "plan-execute", "planexecute", "pe":
		return "plan_execute"
	case "supervisor", "super", "sv":
		return "supervisor"
	default:
		return "eino_single"
	}
}

// NormalizeRobotAgentMode 解析机器人默认对话模式。
func NormalizeRobotAgentMode(ma MultiAgentConfig) string {
	return NormalizeAgentMode(ma.RobotDefaultAgentMode)
}

// NormalizeMultiAgentOrchestration 返回 deep、plan_execute 或 supervisor。
func NormalizeMultiAgentOrchestration(s string) string {
	v := strings.TrimSpace(strings.ToLower(s))
	switch v {
	case "plan_execute", "plan-execute", "planexecute", "pe":
		return "plan_execute"
	case "supervisor", "super", "sv":
		return "supervisor"
	default:
		return "deep"
	}
}

// MultiAgentAPIUpdate 设置页/API 仅更新多代理标量字段；写入 YAML 时不覆盖 sub_agents 等块。
type MultiAgentAPIUpdate struct {
	Enabled                                    bool      `json:"enabled"`
	RobotDefaultAgentMode                      string    `json:"robot_default_agent_mode,omitempty"`
	BatchUseMultiAgent                         bool      `json:"batch_use_multi_agent"`
	PlanExecuteLoopMaxIterations               *int      `json:"plan_execute_loop_max_iterations,omitempty"`
	SummarizationUserIntentLedgerMaxRunes      *int      `json:"summarization_user_intent_ledger_max_runes,omitempty"`
	SummarizationUserIntentLedgerEntryMaxRunes *int      `json:"summarization_user_intent_ledger_entry_max_runes,omitempty"`
	LatestUserMessageMaxRunes                  *int      `json:"latest_user_message_max_runes,omitempty"`
	LatestUserMessageHeadRunes                 *int      `json:"latest_user_message_head_runes,omitempty"`
	LatestUserMessageTailRunes                 *int      `json:"latest_user_message_tail_runes,omitempty"`
	ModelRetryMaxRetries                       *int      `json:"model_retry_max_retries,omitempty"`
	ModelRetryMaxBackoffSec                    *int      `json:"model_retry_max_backoff_sec,omitempty"`
	ModelFailoverChannels                      *[]string `json:"model_failover_channels,omitempty"`
	ModelFailoverMaxRetries                    *int      `json:"model_failover_max_retries,omitempty"`
	// 指针区分「JSON 未传该字段」与「传空数组要清空」；省略时不应覆盖 YAML 中的常驻工具白名单。
	ToolSearchAlwaysVisibleTools *[]string `json:"tool_search_always_visible_tools,omitempty"`
}

// RobotsConfig 机器人配置（企业微信、钉钉、飞书、微信 iLink、Telegram、Slack、Discord、QQ 等）
type RobotsConfig struct {
	Session  RobotSessionConfig  `yaml:"session,omitempty" json:"session,omitempty"`   // 机器人会话隔离策略
	Wechat   RobotWechatConfig   `yaml:"wechat,omitempty" json:"wechat,omitempty"`     // 微信（iLink 扫码绑定）
	Wecom    RobotWecomConfig    `yaml:"wecom,omitempty" json:"wecom,omitempty"`       // 企业微信
	Dingtalk RobotDingtalkConfig `yaml:"dingtalk,omitempty" json:"dingtalk,omitempty"` // 钉钉
	Lark     RobotLarkConfig     `yaml:"lark,omitempty" json:"lark,omitempty"`         // 飞书
	Telegram RobotTelegramConfig `yaml:"telegram,omitempty" json:"telegram,omitempty"` // Telegram
	Slack    RobotSlackConfig    `yaml:"slack,omitempty" json:"slack,omitempty"`       // Slack
	Discord  RobotDiscordConfig  `yaml:"discord,omitempty" json:"discord,omitempty"`   // Discord
	QQ       RobotQQConfig       `yaml:"qq,omitempty" json:"qq,omitempty"`             // QQ 机器人
}

// RobotWechatConfig 微信 iLink 机器人配置（个人微信 ClawBot / iLink 协议）
type RobotWechatConfig struct {
	Enabled       bool                     `yaml:"enabled" json:"enabled"`
	BotToken      string                   `yaml:"bot_token,omitempty" json:"bot_token,omitempty"`
	ILinkBotID    string                   `yaml:"ilink_bot_id,omitempty" json:"ilink_bot_id,omitempty"`
	ILinkUserID   string                   `yaml:"ilink_user_id,omitempty" json:"ilink_user_id,omitempty"`
	BaseURL       string                   `yaml:"base_url,omitempty" json:"base_url,omitempty"`               // 默认 https://ilinkai.weixin.qq.com
	BotType       string                   `yaml:"bot_type,omitempty" json:"bot_type,omitempty"`               // get_bot_qrcode 参数，默认 3
	BotAgent      string                   `yaml:"bot_agent,omitempty" json:"bot_agent,omitempty"`             // base_info.bot_agent
	GetUpdatesBuf string                   `yaml:"get_updates_buf,omitempty" json:"get_updates_buf,omitempty"` // 长轮询游标（运行时）
	Auth          RobotAuthorizationConfig `yaml:"auth,omitempty" json:"auth,omitempty"`
}

const (
	RobotAuthModeUserBinding    = "user_binding"
	RobotAuthModeServiceAccount = "service_account"
)

// RobotAuthorizationConfig controls how a verified platform sender becomes
// an RBAC principal. service_account is intentionally fail-closed unless an
// explicit non-admin service user and sender allowlist are both configured.
type RobotAuthorizationConfig struct {
	Mode                 string   `yaml:"mode,omitempty" json:"mode,omitempty"`
	ServiceUserID        string   `yaml:"service_user_id,omitempty" json:"service_user_id,omitempty"`
	AllowedExternalUsers []string `yaml:"allowed_external_users,omitempty" json:"allowed_external_users,omitempty"`
}

func (c RobotAuthorizationConfig) EffectiveMode() string {
	mode := strings.ToLower(strings.TrimSpace(c.Mode))
	if mode == "" {
		return RobotAuthModeUserBinding
	}
	return mode
}

func (c RobotAuthorizationConfig) ExternalUserAllowed(externalUserID string) bool {
	externalUserID = strings.TrimSpace(externalUserID)
	if externalUserID == "" {
		return false
	}
	for _, allowed := range c.AllowedExternalUsers {
		if strings.TrimSpace(allowed) == externalUserID {
			return true
		}
	}
	return false
}

// RobotSessionConfig 机器人会话隔离策略
type RobotSessionConfig struct {
	StrictUserIdentity *bool `yaml:"strict_user_identity,omitempty" json:"strict_user_identity,omitempty"` // true 时只允许真实用户标识，不允许会话/群 ID 兜底
}

// StrictUserIdentityEnabled 返回是否启用严格用户身份模式；未配置时默认 true。
func (c RobotSessionConfig) StrictUserIdentityEnabled() bool {
	if c.StrictUserIdentity == nil {
		return true
	}
	return *c.StrictUserIdentity
}

// RobotWecomConfig 企业微信机器人配置
type RobotWecomConfig struct {
	Enabled        bool                     `yaml:"enabled" json:"enabled"`
	Token          string                   `yaml:"token" json:"token"`                       // 回调 URL 校验 Token
	EncodingAESKey string                   `yaml:"encoding_aes_key" json:"encoding_aes_key"` // EncodingAESKey
	CorpID         string                   `yaml:"corp_id" json:"corp_id"`                   // 企业 ID
	Secret         string                   `yaml:"secret" json:"secret"`                     // 应用 Secret
	AgentID        int64                    `yaml:"agent_id" json:"agent_id"`                 // 应用 AgentId
	Auth           RobotAuthorizationConfig `yaml:"auth,omitempty" json:"auth,omitempty"`
}

// ValidateWecomConfig 校验企业微信机器人配置；启用时必须配置 token，否则回调无法防伪造。
func ValidateWecomConfig(w RobotWecomConfig) error {
	if !w.Enabled {
		return nil
	}
	if strings.TrimSpace(w.Token) == "" {
		return fmt.Errorf("robots.wecom.enabled 为 true 时必须配置 robots.wecom.token")
	}
	return nil
}

// RobotDingtalkConfig 钉钉机器人配置
type RobotDingtalkConfig struct {
	Enabled                     bool                     `yaml:"enabled" json:"enabled"`
	ClientID                    string                   `yaml:"client_id" json:"client_id"`                                           // 应用 Key (AppKey)
	ClientSecret                string                   `yaml:"client_secret" json:"client_secret"`                                   // 应用 Secret
	AllowConversationIDFallback bool                     `yaml:"allow_conversation_id_fallback" json:"allow_conversation_id_fallback"` // sender_id 缺失时是否允许回退到会话 ID
	Auth                        RobotAuthorizationConfig `yaml:"auth,omitempty" json:"auth,omitempty"`
}

// RobotLarkConfig 飞书机器人配置
type RobotLarkConfig struct {
	Enabled             bool                     `yaml:"enabled" json:"enabled"`
	AppID               string                   `yaml:"app_id" json:"app_id"`                                 // 应用 App ID
	AppSecret           string                   `yaml:"app_secret" json:"app_secret"`                         // 应用 App Secret
	VerifyToken         string                   `yaml:"verify_token" json:"verify_token"`                     // 事件订阅 Verification Token（可选）
	AllowChatIDFallback bool                     `yaml:"allow_chat_id_fallback" json:"allow_chat_id_fallback"` // 用户 ID 缺失时是否允许回退到 chat_id
	Auth                RobotAuthorizationConfig `yaml:"auth,omitempty" json:"auth,omitempty"`
}

// RobotTelegramConfig Telegram 机器人配置（Bot API 长轮询）
type RobotTelegramConfig struct {
	Enabled            bool                     `yaml:"enabled" json:"enabled"`
	BotToken           string                   `yaml:"bot_token" json:"bot_token"`
	BotUsername        string                   `yaml:"bot_username,omitempty" json:"bot_username,omitempty"` // 可选，用于群聊 @ 识别；留空则启动时 getMe
	AllowGroupMessages bool                     `yaml:"allow_group_messages" json:"allow_group_messages"`     // 群聊中仅响应 @ 机器人
	UpdateOffset       int64                    `yaml:"update_offset,omitempty" json:"update_offset,omitempty"`
	Auth               RobotAuthorizationConfig `yaml:"auth,omitempty" json:"auth,omitempty"`
}

// RobotSlackConfig Slack 机器人配置（Socket Mode，无需公网回调）
type RobotSlackConfig struct {
	Enabled  bool                     `yaml:"enabled" json:"enabled"`
	BotToken string                   `yaml:"bot_token" json:"bot_token"` // xoxb-
	AppToken string                   `yaml:"app_token" json:"app_token"` // xapp-（connections:write）
	Auth     RobotAuthorizationConfig `yaml:"auth,omitempty" json:"auth,omitempty"`
}

// RobotDiscordConfig Discord 机器人配置（Gateway WebSocket）
type RobotDiscordConfig struct {
	Enabled            bool                     `yaml:"enabled" json:"enabled"`
	BotToken           string                   `yaml:"bot_token" json:"bot_token"`
	AllowGuildMessages bool                     `yaml:"allow_guild_messages" json:"allow_guild_messages"` // 服务器频道中仅响应 @ 机器人
	Auth               RobotAuthorizationConfig `yaml:"auth,omitempty" json:"auth,omitempty"`
}

// RobotQQConfig QQ 机器人配置（QQ 开放平台 WebSocket）
type RobotQQConfig struct {
	Enabled      bool                     `yaml:"enabled" json:"enabled"`
	AppID        string                   `yaml:"app_id" json:"app_id"`
	ClientSecret string                   `yaml:"client_secret" json:"client_secret"`
	Sandbox      bool                     `yaml:"sandbox" json:"sandbox"` // 沙箱环境（上线前测试）
	Auth         RobotAuthorizationConfig `yaml:"auth,omitempty" json:"auth,omitempty"`
}

func (c RobotsConfig) AuthorizationFor(platform string) RobotAuthorizationConfig {
	switch strings.ToLower(strings.TrimSpace(platform)) {
	case "wechat":
		return c.Wechat.Auth
	case "wecom":
		return c.Wecom.Auth
	case "dingtalk":
		return c.Dingtalk.Auth
	case "lark":
		return c.Lark.Auth
	case "telegram":
		return c.Telegram.Auth
	case "slack":
		return c.Slack.Auth
	case "discord":
		return c.Discord.Auth
	case "qq":
		return c.QQ.Auth
	default:
		return RobotAuthorizationConfig{}
	}
}

func ValidateRobotAuthorization(c RobotAuthorizationConfig, path string) error {
	switch c.EffectiveMode() {
	case RobotAuthModeUserBinding:
		return nil
	case RobotAuthModeServiceAccount:
		serviceUserID := strings.TrimSpace(c.ServiceUserID)
		if serviceUserID == "" {
			return fmt.Errorf("%s.auth.service_user_id 不能为空", path)
		}
		if len(c.AllowedExternalUsers) == 0 {
			return fmt.Errorf("%s.auth.allowed_external_users 至少配置一个真实发送者", path)
		}
		seen := map[string]bool{}
		for _, userID := range c.AllowedExternalUsers {
			userID = strings.TrimSpace(userID)
			if userID == "" || userID == "*" {
				return fmt.Errorf("%s.auth.allowed_external_users 不允许空值或通配符", path)
			}
			if seen[userID] {
				return fmt.Errorf("%s.auth.allowed_external_users 包含重复用户", path)
			}
			seen[userID] = true
		}
		return nil
	default:
		return fmt.Errorf("%s.auth.mode 仅支持 user_binding 或 service_account", path)
	}
}

func ValidateRobotsAuthorization(c RobotsConfig) error {
	items := []struct {
		path string
		auth RobotAuthorizationConfig
	}{
		{"robots.wechat", c.Wechat.Auth}, {"robots.wecom", c.Wecom.Auth},
		{"robots.dingtalk", c.Dingtalk.Auth}, {"robots.lark", c.Lark.Auth},
		{"robots.telegram", c.Telegram.Auth}, {"robots.slack", c.Slack.Auth},
		{"robots.discord", c.Discord.Auth}, {"robots.qq", c.QQ.Auth},
	}
	for _, item := range items {
		if err := ValidateRobotAuthorization(item.auth, item.path); err != nil {
			return err
		}
	}
	return nil
}

func (c RobotsConfig) ServiceAccountUserIDs() map[string]string {
	out := map[string]string{}
	for _, platform := range []string{"wechat", "wecom", "dingtalk", "lark", "telegram", "slack", "discord", "qq"} {
		auth := c.AuthorizationFor(platform)
		if auth.EffectiveMode() == RobotAuthModeServiceAccount {
			out[platform] = strings.TrimSpace(auth.ServiceUserID)
		}
	}
	return out
}

type ServerConfig struct {
	Host string `yaml:"host" json:"host"`
	Port int    `yaml:"port" json:"port"`
	// CORSAllowedOrigins contains additional, exact origins that may call the API.
	// Same-origin browser requests are always allowed. Wildcards are intentionally unsupported.
	CORSAllowedOrigins []string `yaml:"cors_allowed_origins,omitempty" json:"cors_allowed_origins,omitempty"`
	// TLSEnabled 为 true 时主 Web UI 使用 HTTPS；现代浏览器在同源下会协商 HTTP/2，缓解 HTTP/1.1 每源并发连接数限制。
	TLSEnabled bool `yaml:"tls_enabled,omitempty" json:"tls_enabled,omitempty"`
	// TLSCertPath / TLSKeyPath 非空时从 PEM 文件加载证书（生产环境推荐）。
	TLSCertPath string `yaml:"tls_cert_path,omitempty" json:"tls_cert_path,omitempty"`
	TLSKeyPath  string `yaml:"tls_key_path,omitempty" json:"tls_key_path,omitempty"`
	// TLSAutoSelfSign 为 true 且未配置有效证书路径时，启动时生成内存自签证书（仅本地/测试；浏览器会提示不受信任）。
	TLSAutoSelfSign bool `yaml:"tls_auto_self_sign,omitempty" json:"tls_auto_self_sign,omitempty"`
	// TLSHTTPRedirect 为 false 时禁用 HTTP→HTTPS 跳转；省略或为 true 且已启用 HTTPS 时，明文 HTTP 访问将 308 跳转到 HTTPS（同端口嗅探分流）。
	TLSHTTPRedirect *bool `yaml:"tls_http_redirect,omitempty" json:"tls_http_redirect,omitempty"`
}

type LogConfig struct {
	Level                   string `yaml:"level"`
	Output                  string `yaml:"output"`
	DiagnosticDir           string `yaml:"diagnostic_dir"`
	DiagnosticDisabled      bool   `yaml:"diagnostic_disabled"`
	DiagnosticRetentionDays int    `yaml:"diagnostic_retention_days"`
}

type MCPConfig struct {
	Enabled           bool   `yaml:"enabled"`
	Host              string `yaml:"host"`
	Port              int    `yaml:"port"`
	AuthHeader        string `yaml:"auth_header,omitempty"`         // 可选的全局服务凭证 header；普通调用优先使用用户 Bearer Token
	AuthHeaderValue   string `yaml:"auth_header_value,omitempty"`   // 全局服务凭证，仅 allow_global_access=true 时接受
	AllowGlobalAccess bool   `yaml:"allow_global_access,omitempty"` // 静态服务密钥是否映射为全局服务身份（默认关闭）
}

type OpenAIConfig struct {
	Provider            string `yaml:"provider,omitempty" json:"provider,omitempty"` // API 提供商: "openai"(默认) 或 "claude"，claude 使用 Eino 原生 Anthropic Messages API
	APIKey              string `yaml:"api_key" json:"api_key"`
	BaseURL             string `yaml:"base_url" json:"base_url"`
	Model               string `yaml:"model" json:"model"`
	MaxTotalTokens      int    `yaml:"max_total_tokens,omitempty" json:"max_total_tokens,omitempty"`
	MaxCompletionTokens int    `yaml:"max_completion_tokens,omitempty" json:"max_completion_tokens,omitempty"`
	// Reasoning 控制 Eino ChatModel 的 thinking / reasoning_effort / output_config 等（Eino 单/多代理路径生效）。
	Reasoning OpenAIReasoningConfig `yaml:"reasoning,omitempty" json:"reasoning,omitempty"`
}

// AIConfig stores first-class model channels. Runtime callers resolve a channel
// into OpenAIConfig at the edge instead of moving API credentials through chat requests.
type AIConfig struct {
	DefaultChannel string                     `yaml:"default_channel,omitempty" json:"default_channel,omitempty"`
	Channels       map[string]AIChannelConfig `yaml:"channels,omitempty" json:"channels,omitempty"`
}

type AIChannelConfig struct {
	Name                string                `yaml:"name,omitempty" json:"name,omitempty"`
	Provider            string                `yaml:"provider,omitempty" json:"provider,omitempty"`
	APIKey              string                `yaml:"api_key" json:"api_key"`
	BaseURL             string                `yaml:"base_url" json:"base_url"`
	Model               string                `yaml:"model" json:"model"`
	MaxTotalTokens      int                   `yaml:"max_total_tokens,omitempty" json:"max_total_tokens,omitempty"`
	MaxCompletionTokens int                   `yaml:"max_completion_tokens,omitempty" json:"max_completion_tokens,omitempty"`
	Reasoning           OpenAIReasoningConfig `yaml:"reasoning,omitempty" json:"reasoning,omitempty"`
}

func (c AIChannelConfig) ToOpenAIConfig() OpenAIConfig {
	provider := strings.TrimSpace(c.Provider)
	if provider == "" || provider == "openai_compatible" {
		provider = "openai"
	}
	return OpenAIConfig{
		Provider:            provider,
		APIKey:              c.APIKey,
		BaseURL:             c.BaseURL,
		Model:               c.Model,
		MaxTotalTokens:      c.MaxTotalTokens,
		MaxCompletionTokens: c.MaxCompletionTokens,
		Reasoning:           c.Reasoning,
	}
}

func AIChannelFromOpenAI(id, name string, oa OpenAIConfig) AIChannelConfig {
	if strings.TrimSpace(name) == "" {
		name = id
	}
	return AIChannelConfig{
		Name:                name,
		Provider:            oa.Provider,
		APIKey:              oa.APIKey,
		BaseURL:             oa.BaseURL,
		Model:               oa.Model,
		MaxTotalTokens:      oa.MaxTotalTokens,
		MaxCompletionTokens: oa.MaxCompletionTokens,
		Reasoning:           oa.Reasoning,
	}
}

func NormalizeAIChannelID(s string) string {
	id := strings.ToLower(strings.TrimSpace(s))
	id = strings.ReplaceAll(id, "_", "-")
	var b strings.Builder
	lastDash := false
	for _, r := range id {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "default"
	}
	return out
}

func (c *AIConfig) EnsureDefaultFromOpenAI(openAI OpenAIConfig) {
	if c.Channels == nil {
		c.Channels = make(map[string]AIChannelConfig)
	}
	def := NormalizeAIChannelID(c.DefaultChannel)
	if def == "default" && strings.TrimSpace(c.DefaultChannel) == "" {
		def = "default"
	}
	c.DefaultChannel = def
	if _, ok := c.Channels[def]; !ok {
		c.Channels[def] = AIChannelFromOpenAI(def, "Default", openAI)
	}
}

func (c AIConfig) ResolveChannel(channelID string) (OpenAIConfig, string, bool) {
	id := NormalizeAIChannelID(channelID)
	if strings.TrimSpace(channelID) == "" {
		id = NormalizeAIChannelID(c.DefaultChannel)
	}
	if id == "" {
		id = "default"
	}
	if c.Channels != nil {
		if ch, ok := c.Channels[id]; ok {
			return ch.ToOpenAIConfig(), id, true
		}
	}
	return OpenAIConfig{}, id, false
}

func (c *Config) ResolveAIChannel(channelID string) (OpenAIConfig, string, bool) {
	if c == nil {
		return OpenAIConfig{}, "", false
	}
	if oa, id, ok := c.AI.ResolveChannel(channelID); ok {
		return oa, id, true
	}
	return c.OpenAI, NormalizeAIChannelID(channelID), strings.TrimSpace(c.OpenAI.Model) != "" || strings.TrimSpace(c.OpenAI.BaseURL) != ""
}

func (c *Config) ApplyDefaultAIChannel() {
	if c == nil {
		return
	}
	c.NormalizeAIProviderProfiles()
	c.AI.EnsureDefaultFromOpenAI(c.OpenAI)
	if oa, _, ok := c.AI.ResolveChannel(c.AI.DefaultChannel); ok {
		c.OpenAI = oa
	}
	c.NormalizeAIProviderProfiles()
}

func (c OpenAIConfig) MaxCompletionTokensEffective() int {
	if c.MaxCompletionTokens > 0 {
		return c.MaxCompletionTokens
	}
	return DefaultMaxCompletionTokens
}

// IsDeepSeekEndpointOrModel reports whether the channel targets DeepSeek's
// official-compatible API endpoint. The historical name is kept for compatibility;
// model names alone are not enough to infer DeepSeek wire behavior behind
// OpenAI-compatible gateways.
func (c OpenAIConfig) IsDeepSeekEndpointOrModel() bool {
	baseURL := strings.ToLower(strings.TrimSpace(c.BaseURL))
	return strings.Contains(baseURL, "deepseek")
}

func (c OpenAIConfig) IsDeepSeekOfficialEndpoint() bool {
	host := normalizedURLHost(c.BaseURL)
	return host == "api.deepseek.com"
}

func normalizedURLHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		parsed, err = url.Parse("https://" + strings.TrimLeft(raw, "/"))
		if err != nil {
			return ""
		}
	}
	return strings.ToLower(strings.TrimPrefix(parsed.Hostname(), "www."))
}

func NormalizeOpenAIProviderProfile(oa *OpenAIConfig) {
	if oa == nil {
		return
	}
	if oa.IsDeepSeekOfficialEndpoint() {
		oa.Reasoning.Profile = "deepseek"
	}
}

func (c *Config) NormalizeAIProviderProfiles() {
	if c == nil {
		return
	}
	NormalizeOpenAIProviderProfile(&c.OpenAI)
	if c.AI.Channels != nil {
		for id, ch := range c.AI.Channels {
			oa := ch.ToOpenAIConfig()
			NormalizeOpenAIProviderProfile(&oa)
			ch.Reasoning = oa.Reasoning
			c.AI.Channels[id] = ch
		}
	}
}

// OpenAIReasoningConfig 全局默认与网关 profile（对话页可通过 ChatRequest.reasoning 覆盖，受 AllowClientReasoning 约束）。
type OpenAIReasoningConfig struct {
	// Mode: auto（默认）| on | off | default（与 auto 相同）。
	// off 在 OpenAI/Claude profile 下省略推理字段；DeepSeek profile 下发送 thinking.type=disabled（其默认开启思考）。
	Mode string `yaml:"mode,omitempty" json:"mode,omitempty"`
	// Effort: low | medium | high | max | xhigh；max/xhigh 为不同网关最高档命名，原样下发、不互转。空表示不单独指定强度。
	Effort string `yaml:"effort,omitempty" json:"effort,omitempty"`
	// AllowClientReasoning 为 false 时忽略请求体 reasoning；nil 或未设置等同于 true。
	AllowClientReasoning *bool `yaml:"allow_client_reasoning,omitempty" json:"allow_client_reasoning,omitempty"`
	// Profile: auto | deepseek_compat | openai_compat | output_config_effort
	Profile string `yaml:"profile,omitempty" json:"profile,omitempty"`
	// ExtraRequestFields 合并进 Chat Completions 根 JSON（管理员用；与自动字段同名时后者覆盖）。
	// Mode=off 时会移除其中的推理控制字段，但保留其他扩展字段；DeepSeek profile 随后补充显式关闭开关。
	ExtraRequestFields map[string]interface{} `yaml:"extra_request_fields,omitempty" json:"extra_request_fields,omitempty"`
}

// ModeEffective returns auto when empty or default.
func (c OpenAIReasoningConfig) ModeEffective() string {
	m := strings.ToLower(strings.TrimSpace(c.Mode))
	if m == "" || m == "default" {
		return "auto"
	}
	return m
}

// ProfileEffective returns auto when empty.
func (c OpenAIReasoningConfig) ProfileEffective() string {
	p := strings.ToLower(strings.TrimSpace(c.Profile))
	if p == "" {
		return "auto"
	}
	return p
}

// AllowClientReasoningEffective true when client may send ChatRequest.reasoning.
func (c OpenAIReasoningConfig) AllowClientReasoningEffective() bool {
	if c.AllowClientReasoning == nil {
		return true
	}
	return *c.AllowClientReasoning
}

type FofaConfig struct {
	// APIKey 为 FOFA API Key（建议使用只读权限的 Key）
	APIKey  string `yaml:"api_key,omitempty" json:"api_key,omitempty"`
	BaseURL string `yaml:"base_url,omitempty" json:"base_url,omitempty"` // 默认 https://fofa.info/api/v1/search/all
}

type SpaceSearchConfig struct {
	APIKey  string `yaml:"api_key,omitempty" json:"api_key,omitempty"`
	BaseURL string `yaml:"base_url,omitempty" json:"base_url,omitempty"`
}

type ProcessIsolationConfig struct {
	Mode           string `yaml:"mode" json:"mode"`
	CgroupRoot     string `yaml:"cgroup_root" json:"cgroup_root"`
	MaxProcesses   int    `yaml:"max_processes" json:"max_processes"`
	MemoryMaxBytes int64  `yaml:"memory_max_bytes" json:"memory_max_bytes"`
	CPUQuotaMicros int64  `yaml:"cpu_quota_micros" json:"cpu_quota_micros"`
}

type SecurityConfig struct {
	ProcessIsolation ProcessIsolationConfig `yaml:"process_isolation,omitempty" json:"process_isolation"`

	Tools               []ToolConfig `yaml:"tools,omitempty"`                 // 向后兼容：支持在主配置文件中定义工具
	ToolsDir            string       `yaml:"tools_dir,omitempty"`             // 工具配置文件目录（新方式）
	ToolDescriptionMode string       `yaml:"tool_description_mode,omitempty"` // 工具描述模式: "short" | "full"，默认 short
}

type DatabaseConfig struct {
	Path            string `yaml:"path"`                        // 会话数据库路径
	KnowledgeDBPath string `yaml:"knowledge_db_path,omitempty"` // 知识库数据库路径（可选，为空则使用会话数据库）
}

type AgentConfig struct {
	MaxIterations                      int `yaml:"max_iterations" json:"max_iterations"`
	ToolTimeoutMinutes                 int `yaml:"tool_timeout_minutes" json:"tool_timeout_minutes"`                                     // 单次工具执行最大时长（分钟），超时自动终止，防止长时间挂起；0 表示不限制（不推荐）
	ToolWaitTimeoutSeconds             int `yaml:"tool_wait_timeout_seconds" json:"tool_wait_timeout_seconds"`                           // 工具本轮等待秒数；到时返回 execution_id，worker 继续后台执行；0 表示等到完成
	ExternalMCPMaxConcurrentPerServer  int `yaml:"external_mcp_max_concurrent_per_server" json:"external_mcp_max_concurrent_per_server"` // 单个外部 MCP server 同时运行的工具数；0 表示默认 2
	ExternalMCPMaxConcurrentTotal      int `yaml:"external_mcp_max_concurrent_total" json:"external_mcp_max_concurrent_total"`           // 所有外部 MCP 工具全局并发；0 表示默认 16
	ExternalMCPCircuitFailureThreshold int `yaml:"external_mcp_circuit_failure_threshold" json:"external_mcp_circuit_failure_threshold"` // 单个 MCP server 连续失败多少次后打开熔断；0 表示默认 3；负数关闭
	ExternalMCPCircuitCooldownSeconds  int `yaml:"external_mcp_circuit_cooldown_seconds" json:"external_mcp_circuit_cooldown_seconds"`   // 熔断后冷却秒数；0 表示默认 60
	// ShellNoOutputTimeoutSeconds execute/exec 无任何 stdout/stderr 时的空闲终止秒数（通用防挂死，不维护命令黑名单）；0=默认 300（5 分钟）；-1=关闭。
	ShellNoOutputTimeoutSeconds int `yaml:"shell_no_output_timeout_seconds" json:"shell_no_output_timeout_seconds"`
	// WorkspaceRootDir 会话工作目录根路径（curl/wget 下载、read_file/glob/grep 本地分析）；空=tmp/workspace，其下按 projects/{id} 或 conversations/{id} 隔离。
	WorkspaceRootDir string `yaml:"workspace_root_dir,omitempty" json:"workspace_root_dir,omitempty"`
	// SystemPromptPath 单代理系统提示 Markdown/文本文件路径（相对 config.yaml 所在目录，或可写绝对路径）。非空且可读时替换内置单代理提示；留空用内置。
	SystemPromptPath string `yaml:"system_prompt_path,omitempty" json:"system_prompt_path,omitempty"`
}

// HitlConfig 人机协同全局选项；与会话侧栏/API 中的白名单合并为并集后参与判定。
// tool_whitelist 可在侧栏「应用」时合并写入 config.yaml 并立即生效。
// audit_agent_prompt / audit_agent_prompt_review_edit 可在人机协同页编辑并立即生效；空则使用内置默认。
type HitlConfig struct {
	// AuditBackend 审计 Agent 后端：openai（兼容协议聊天模型）或 typesafe（Jev 结构化裁决）。空值视为 openai。
	AuditBackend string `yaml:"audit_backend,omitempty" json:"audit_backend,omitempty"`
	// AuditModel 审计 Agent 专用模型。openai 后端空字段继承主模型；typesafe 后端 api_key 必填，不继承主模型密钥。
	AuditModel OpenAIConfig `yaml:"audit_model,omitempty" json:"audit_model,omitempty"`
	// ToolWhitelist 全局免审批工具名（与白名单内工具不触发 HITL 审批）。
	ToolWhitelist []string `yaml:"tool_whitelist,omitempty" json:"tool_whitelist,omitempty"`
	// AuditAgentPrompt 审批模式（approval）下审计 Agent 系统提示词。
	AuditAgentPrompt string `yaml:"audit_agent_prompt,omitempty" json:"audit_agent_prompt,omitempty"`
	// AuditAgentPromptReviewEdit 审查编辑模式（review_edit）下审计 Agent 系统提示词。
	AuditAgentPromptReviewEdit string `yaml:"audit_agent_prompt_review_edit,omitempty" json:"audit_agent_prompt_review_edit,omitempty"`
	// RetentionDays 已决策审计日志（hitl_interrupts 非 pending）保留天数；省略时默认 90；0 表示不自动清理。
	RetentionDays *int `yaml:"retention_days,omitempty" json:"retention_days,omitempty"`
	// DefaultMode 全局默认人机协同模式（off | approval | review_edit）；新建会话无独立配置时沿用。
	DefaultMode string `yaml:"default_mode,omitempty" json:"default_mode,omitempty"`
	// DefaultReviewer 全局默认审批方（human | audit_agent）；新建会话无独立配置时沿用。
	DefaultReviewer string `yaml:"default_reviewer,omitempty" json:"default_reviewer,omitempty"`
	// DefaultTimeoutSeconds 全局默认审批等待秒数；nil 表示使用前端历史默认 300 秒，0 表示不限时。
	DefaultTimeoutSeconds *int `yaml:"default_timeout_seconds,omitempty" json:"default_timeout_seconds,omitempty"`
}

// EffectiveDefaultMode returns off, approval, or review_edit; omitted or unknown values default to off.
func (h HitlConfig) EffectiveDefaultMode() string {
	switch strings.ToLower(strings.TrimSpace(h.DefaultMode)) {
	case "feedback", "followup":
		return "approval"
	case "approval", "review_edit":
		return strings.ToLower(strings.TrimSpace(h.DefaultMode))
	default:
		return "off"
	}
}

// EffectiveDefaultReviewer returns human or audit_agent; omitted or unknown values default to human.
func (h HitlConfig) EffectiveDefaultReviewer() string {
	switch strings.ToLower(strings.TrimSpace(h.DefaultReviewer)) {
	case "audit_agent", "agent", "ai":
		return "audit_agent"
	default:
		return "human"
	}
}

// EffectiveDefaultTimeoutSeconds returns the default HITL approval timeout; nil defaults to 5 minutes.
func (h HitlConfig) EffectiveDefaultTimeoutSeconds() int {
	if h.DefaultTimeoutSeconds == nil {
		return 300
	}
	if *h.DefaultTimeoutSeconds < 0 {
		return 0
	}
	return *h.DefaultTimeoutSeconds
}

// RetentionDaysEffective returns retention; 0 means keep forever; omitted defaults to 90.
func (h HitlConfig) RetentionDaysEffective() int {
	if h.RetentionDays == nil {
		return 90
	}
	if *h.RetentionDays < 0 {
		return 0
	}
	return *h.RetentionDays
}

const (
	HitlAuditBackendOpenAI   = "openai"
	HitlAuditBackendTypeSafe = "typesafe"
	TypeSafeDefaultBaseURL   = "https://api.typesafe.ai"
	TypeSafeDefaultModel     = "jev-latest"
)

// EffectiveAuditBackend returns openai or typesafe. Omitted or unknown values default to openai.
func (h HitlConfig) EffectiveAuditBackend() string {
	switch strings.ToLower(strings.TrimSpace(h.AuditBackend)) {
	case HitlAuditBackendTypeSafe, "jev", "type-safe", "typesafe-ai":
		return HitlAuditBackendTypeSafe
	default:
		return HitlAuditBackendOpenAI
	}
}

// TypeSafeConfigEffective returns TypeSafe endpoint settings. Empty base_url/model use defaults; API key is never inherited from the main OpenAI channel.
func (h HitlConfig) TypeSafeConfigEffective() (baseURL, apiKey, model string) {
	baseURL = strings.TrimSpace(h.AuditModel.BaseURL)
	if baseURL == "" {
		baseURL = TypeSafeDefaultBaseURL
	}
	apiKey = strings.TrimSpace(h.AuditModel.APIKey)
	model = strings.TrimSpace(h.AuditModel.Model)
	if model == "" {
		model = TypeSafeDefaultModel
	}
	return strings.TrimSuffix(baseURL, "/"), apiKey, model
}

// AuditModelEffective returns the audit-agent model config with empty fields inherited from the main model config.
func (h HitlConfig) AuditModelEffective(main OpenAIConfig) OpenAIConfig {
	out := main
	am := h.AuditModel
	if strings.TrimSpace(am.Provider) != "" {
		out.Provider = strings.TrimSpace(am.Provider)
	}
	if strings.TrimSpace(am.BaseURL) != "" {
		out.BaseURL = strings.TrimSpace(am.BaseURL)
	}
	if strings.TrimSpace(am.APIKey) != "" {
		out.APIKey = strings.TrimSpace(am.APIKey)
	}
	if strings.TrimSpace(am.Model) != "" {
		out.Model = strings.TrimSpace(am.Model)
	}
	if am.MaxTotalTokens > 0 {
		out.MaxTotalTokens = am.MaxTotalTokens
	}
	if am.MaxCompletionTokens > 0 {
		out.MaxCompletionTokens = am.MaxCompletionTokens
	}
	return out
}

const hitlAuditAgentPromptBase = `你是 CyberStrikeAI 人机协同审计 Agent。审查 Agent 即将执行的工具调用是否会对系统造成实质性损害。

你会收到 JSON，包含 hitlMode、toolName、arguments/argumentsObj、userMessage、thinking、reasoningChain、planning 等字段。

规则优先级：
- 必须 reject 的规则
- 必须 approve 的规则
- 默认放行规则

规则匹配要求：
- 如果同时命中 reject 和 approve，必须 reject。
- 每次裁决必须给出一个规则编号，不得输出“命中规则：无”。
- 未命中明确 reject 或 approve 规则，但未发现明确破坏行为时，命中 D1：默认放行。
- 不得仅凭“本地执行”“设置超时”“文件名包含 test”判断脚本无害；必须检查实际 command、代码或请求参数。
- 如果脚本具体内容不可见，应表述为“当前参数未显示明确高危操作”，不能断言“无持久化影响”。

裁决基调（默认放行）：
- 绝大多数渗透测试操作（包括各类攻击 payload）都应放行
- 在裁决之前需要判断出：1、此请求的是干什么（理解请求内容）？2、此请求造成什么后果？
- 仅在「明确会直接破坏目标业务系统正常运行，或造成真实用户数据丢失/不可逆篡改」时 → reject
- 信息不足、参数略宽泛、仅为探测意图时 → approve
- 拿不准时 → approve（宁可放过可恢复的测试，不要误拦合法攻击）

必须 reject 的高危情形（示例，非穷举）：
- 修改/重置任意用户或管理员密码、强制下线真实用户
- 修改服务器或服务配置（系统配置、Web/数据库参数、防火墙、启动项、计划任务）
- 修改/创建/删除用户、角色、权限（账户体系篡改）
- 删除或大规模破坏数据/文件（DROP/TRUNCATE/DELETE 全表、rm 重要文件、清空数据库）
- 停止、禁用、重启业务服务（systemctl stop、kill 关键进程、shutdown、reboot）
- 大规模压测或高并发造成 DoS

以下情形必须 approve（即使看起来“攻击性强”或包含写操作）：
- 各类漏洞探测与利用 payload（SQL 注入含读写型、命令注入、反序列化、文件包含、路径遍历、SSRF、SSTI、XXE 等）
- 文件上传测试，包括上传 webshell、写入 webshell 文件
- 命令执行探测、反弹 Shell、dnslog / 数据外带探测
- 数据库命令执行、写文件、写计划任务、内存马注入等 getshell 利用
- 常规速率的口令爆破（非 DoS 级）
- 信息收集、端口/目录扫描、枚举、抓取
- 读取、查询、探测类操作（含敏感文件）
- 攻陷后无害侦察命令（whoami/id/uname/ls/cat 等）
- 参数宽泛但无明确破坏意图的请求

判定原则：
- “写”本身不是拦截理由——渗透测试大量依赖写操作（注入、上传、爆破、getshell）
- 关键看是否破坏「业务可用性 / 关键数据完整性」
- 只有明确命中上方 reject 清单才 reject`

const hitlAuditAgentPromptApprovalOutput = `
仅输出一行 JSON，不要 markdown 代码块：
{"decision":"approve"|"reject","comment":"实际操作：...；成功后的后果：...；命中规则：..."}`

const hitlAuditAgentPromptReviewEditOutput = `
仅输出一行 JSON，不要 markdown 代码块：
{"decision":"approve"|"reject","comment":"实际操作：...；成功后的后果：...；命中规则：...","editedArguments":{...}}

editedArguments 规则（仅 approve 且需要改参时填写，否则省略该字段）：
- 提供完整替换后的工具参数对象，键名与 argumentsObj 一致
- 只做最小必要修改以收窄范围、消除风险（如限制 path、去掉危险 flag）
- 禁止扩大攻击面：不得扩大目标范围、提升权限或引入破坏性参数
- 无法安全改参时应 reject，不要勉强 approve`

// DefaultHitlAuditAgentPrompt 内置审批模式审计 Agent 提示词。
func DefaultHitlAuditAgentPrompt() string {
	return hitlAuditAgentPromptBase + hitlAuditAgentPromptApprovalOutput
}

// DefaultHitlAuditAgentPromptReviewEdit 内置审查编辑模式审计 Agent 提示词。
func DefaultHitlAuditAgentPromptReviewEdit() string {
	return hitlAuditAgentPromptBase + hitlAuditAgentPromptReviewEditOutput
}

// EffectiveAuditAgentPrompt 返回审批模式生效的审计 Agent 提示词。
func (c HitlConfig) EffectiveAuditAgentPrompt() string {
	return c.EffectiveAuditAgentPromptForMode("approval")
}

// EffectiveAuditAgentPromptForMode 按 HITL 模式返回生效的审计 Agent 提示词。
func (c HitlConfig) EffectiveAuditAgentPromptForMode(mode string) string {
	if normalizeHitlModeForPrompt(mode) == "review_edit" {
		if s := strings.TrimSpace(c.AuditAgentPromptReviewEdit); s != "" {
			return s
		}
		return DefaultHitlAuditAgentPromptReviewEdit()
	}
	if s := strings.TrimSpace(c.AuditAgentPrompt); s != "" {
		return s
	}
	return DefaultHitlAuditAgentPrompt()
}

// JevOperatorPolicy returns a custom audit-strategy prompt for TypeSafe Jev.
// Built-in default prompts stay encoded as Jev questions and are not copied into state.
func (c HitlConfig) JevOperatorPolicy(mode string) string {
	effective := strings.TrimSpace(c.EffectiveAuditAgentPromptForMode(mode))
	var def string
	if normalizeHitlModeForPrompt(mode) == "review_edit" {
		def = strings.TrimSpace(DefaultHitlAuditAgentPromptReviewEdit())
	} else {
		def = strings.TrimSpace(DefaultHitlAuditAgentPrompt())
	}
	if effective == "" || effective == def {
		return ""
	}
	return effective
}

func normalizeHitlModeForPrompt(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "review_edit":
		return "review_edit"
	default:
		return "approval"
	}
}

type AuthConfig struct {
	SessionDurationHours int `yaml:"session_duration_hours" json:"session_duration_hours"`
}

// MonitorConfig MCP 状态监控（tool_executions）保留策略。
type MonitorConfig struct {
	// RetentionDays 执行记录保留天数；省略时默认 90；0 表示不自动清理。
	RetentionDays *int `yaml:"retention_days,omitempty" json:"retention_days,omitempty"`
}

// RetentionDaysEffective returns retention; 0 means keep forever; omitted defaults to 90.
func (m MonitorConfig) RetentionDaysEffective() int {
	if m.RetentionDays == nil {
		return 90
	}
	if *m.RetentionDays < 0 {
		return 0
	}
	return *m.RetentionDays
}

// 运行空间垃圾清理的类别键。顺序即前端展示顺序，勿依赖 map 迭代顺序。
const (
	StorageCategoryWorkspace            = "workspace"
	StorageCategoryReduction            = "reduction"
	StorageCategoryConversationArtifact = "conversation_artifacts"
	StorageCategoryPlantask             = "plantask"
	StorageCategoryC2Artifacts          = "c2_artifacts"
	StorageCategoryChatUploads          = "chat_uploads"
	StorageCategoryWorkflowCheckpoints  = "workflow_checkpoints"
	StorageCategoryDiagnosticLogs       = "diagnostic_logs"
)

// StorageCategoryOrder 列出全部可清理类别，供 UI 与报表稳定排序。
var StorageCategoryOrder = []string{
	StorageCategoryWorkspace,
	StorageCategoryReduction,
	StorageCategoryConversationArtifact,
	StorageCategoryPlantask,
	StorageCategoryC2Artifacts,
	StorageCategoryChatUploads,
	StorageCategoryWorkflowCheckpoints,
	StorageCategoryDiagnosticLogs,
}

// StorageCategoryDefaults 各类别默认保留天数。取较短值的是纯派生产物
// （reduction/checkpoint），取较长值的是可能仍需人工回看的上传件。
var StorageCategoryDefaults = map[string]int{
	StorageCategoryWorkspace:            30,
	StorageCategoryReduction:            7,
	StorageCategoryConversationArtifact: 30,
	StorageCategoryPlantask:             30,
	StorageCategoryC2Artifacts:          30,
	StorageCategoryChatUploads:          90,
	StorageCategoryWorkflowCheckpoints:  7,
	StorageCategoryDiagnosticLogs:       14,
}

// StorageCategoryConfig 单个清理类别的策略覆盖。
type StorageCategoryConfig struct {
	// Enabled 省略时默认 true；显式 false 表示该类别既不自动清理也不出现在手动清理范围内。
	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	// RetentionDays 省略时使用 StorageCategoryDefaults；0 表示不按保留期清理（孤儿目录仍会回收）。
	RetentionDays *int `yaml:"retention_days,omitempty" json:"retention_days,omitempty"`
}

// StorageConfig 运行空间垃圾清理策略。
// 自动清理默认关闭：与 Argo ttlStrategy / K8s ttlSecondsAfterFinished 的 unset 语义一致，
// 升级后不会在管理员不知情的情况下删除既有数据。
type StorageConfig struct {
	// AutoClean 省略或 false 关闭后台自动清理；显式 true 才启用。
	AutoClean *bool `yaml:"auto_clean,omitempty" json:"auto_clean,omitempty"`
	// IntervalMinutes 后台清理轮询间隔；省略默认 60，最小 5。
	IntervalMinutes *int `yaml:"interval_minutes,omitempty" json:"interval_minutes,omitempty"`
	// OrphanGraceDays 会话/项目已删除但目录残留时的最小保留天数；省略默认 1。
	OrphanGraceDays *int `yaml:"orphan_grace_days,omitempty" json:"orphan_grace_days,omitempty"`
	// ActiveGraceHours 最近有活动的会话一律跳过清理；省略默认 24。
	ActiveGraceHours *int `yaml:"active_grace_hours,omitempty" json:"active_grace_hours,omitempty"`
	// Categories 按类别覆盖策略；未列出的类别使用内置默认值。
	Categories map[string]StorageCategoryConfig `yaml:"categories,omitempty" json:"categories,omitempty"`
}

// AutoCleanEffective returns true only when storage.auto_clean is explicitly true.
func (s StorageConfig) AutoCleanEffective() bool {
	return s.AutoClean != nil && *s.AutoClean
}

// IntervalMinutesEffective returns the background sweep interval; defaults to 60, floor 5.
func (s StorageConfig) IntervalMinutesEffective() int {
	if s.IntervalMinutes == nil {
		return 60
	}
	if *s.IntervalMinutes < 5 {
		return 5
	}
	return *s.IntervalMinutes
}

// OrphanGraceDaysEffective returns the minimum age before an orphaned session dir is reclaimed; defaults to 1.
func (s StorageConfig) OrphanGraceDaysEffective() int {
	if s.OrphanGraceDays == nil {
		return 1
	}
	if *s.OrphanGraceDays < 0 {
		return 0
	}
	return *s.OrphanGraceDays
}

// ActiveGraceHoursEffective returns the recent-activity protection window; defaults to 24, floor 1.
func (s StorageConfig) ActiveGraceHoursEffective() int {
	if s.ActiveGraceHours == nil {
		return 24
	}
	if *s.ActiveGraceHours < 1 {
		return 1
	}
	return *s.ActiveGraceHours
}

// CategoryEnabled reports whether a category participates in cleanup; unknown keys default to false.
func (s StorageConfig) CategoryEnabled(key string) bool {
	if _, known := StorageCategoryDefaults[key]; !known {
		return false
	}
	if c, ok := s.Categories[key]; ok && c.Enabled != nil {
		return *c.Enabled
	}
	return true
}

// CategoryRetentionDays returns the effective retention for a category; unknown keys yield 0 (keep forever).
func (s StorageConfig) CategoryRetentionDays(key string) int {
	def, known := StorageCategoryDefaults[key]
	if !known {
		return 0
	}
	c, ok := s.Categories[key]
	if !ok || c.RetentionDays == nil {
		return def
	}
	if *c.RetentionDays < 0 {
		return 0
	}
	return *c.RetentionDays
}

// AuditConfig platform operation audit log settings (not chat/tool execution bodies).
type AuditConfig struct {
	// Enabled nil or true enables persistence; explicit false disables.
	Enabled        *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	RetentionDays  int   `yaml:"retention_days,omitempty" json:"retention_days,omitempty"`
	MaxDetailBytes int   `yaml:"max_detail_bytes,omitempty" json:"max_detail_bytes,omitempty"`
	// AuthFailureCooldownSeconds: per-IP cooldown for auth login/change_password failure audit rows; -1 disables; 0 uses default 60.
	AuthFailureCooldownSeconds int `yaml:"auth_failure_cooldown_seconds,omitempty" json:"auth_failure_cooldown_seconds,omitempty"`
}

// EnabledEffective returns true unless audit.enabled is explicitly false.
func (a AuditConfig) EnabledEffective() bool {
	if a.Enabled == nil {
		return true
	}
	return *a.Enabled
}

// RetentionDaysEffective returns retention; 0 means keep forever.
func (a AuditConfig) RetentionDaysEffective() int {
	if a.RetentionDays < 0 {
		return 0
	}
	return a.RetentionDays
}

// MaxDetailBytesEffective caps serialized detail JSON size.
func (a AuditConfig) MaxDetailBytesEffective() int {
	if a.MaxDetailBytes <= 0 {
		return 8192
	}
	return a.MaxDetailBytes
}

// AuthFailureCooldownEffective returns seconds between duplicate auth-failure audit rows per IP (default 60; -1 disables).
func (a AuditConfig) AuthFailureCooldownEffective() int {
	if a.AuthFailureCooldownSeconds < 0 {
		return 0
	}
	if a.AuthFailureCooldownSeconds == 0 {
		return 60
	}
	return a.AuthFailureCooldownSeconds
}

// ExternalMCPConfig 外部MCP配置
type ExternalMCPConfig struct {
	Servers map[string]ExternalMCPServerConfig `yaml:"servers,omitempty" json:"servers,omitempty"`
}

// ExternalMCPServerConfig 外部MCP服务器配置（遵循官方 MCP 配置格式，兼容 Claude Desktop / Cursor / VS Code）。
// 所有字符串字段均支持 ${VAR} 和 ${VAR:-default} 环境变量展开语法。
type ExternalMCPServerConfig struct {
	// 传输类型: "stdio" | "sse" | "http"（Streamable HTTP）。
	// stdio 模式可省略，有 command 字段时自动推断。
	Type string `yaml:"type,omitempty" json:"type,omitempty"`

	// stdio 模式配置
	Command string            `yaml:"command,omitempty" json:"command,omitempty"`
	Args    []string          `yaml:"args,omitempty" json:"args,omitempty"`
	Env     map[string]string `yaml:"env,omitempty" json:"env,omitempty"`

	// HTTP/SSE 模式配置
	URL     string            `yaml:"url,omitempty" json:"url,omitempty"`
	Headers map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`

	// 官方标准字段
	Disabled    bool     `yaml:"disabled,omitempty" json:"disabled,omitempty"`       // 禁用服务器（官方字段）
	AutoApprove []string `yaml:"autoApprove,omitempty" json:"autoApprove,omitempty"` // 自动批准的工具列表（官方字段）

	// SDK 高级配置（对应 MCP Go SDK 传输层参数）
	MaxRetries        int `yaml:"max_retries,omitempty" json:"max_retries,omitempty"`               // Streamable HTTP 断线重连次数（默认 5）
	TerminateDuration int `yaml:"terminate_duration,omitempty" json:"terminate_duration,omitempty"` // stdio 进程优雅关闭等待秒数（默认 5）
	KeepAlive         int `yaml:"keep_alive,omitempty" json:"keep_alive,omitempty"`                 // 客户端心跳间隔秒数（0 = 禁用）

	// 通用配置
	Description       string          `yaml:"description,omitempty" json:"description,omitempty"`
	Timeout           int             `yaml:"timeout,omitempty" json:"timeout,omitempty"`                         // 连接超时（秒）
	ExternalMCPEnable bool            `yaml:"external_mcp_enable,omitempty" json:"external_mcp_enable,omitempty"` // 是否启用
	ToolEnabled       map[string]bool `yaml:"tool_enabled,omitempty" json:"tool_enabled,omitempty"`               // 每个工具的启用状态
}

// GetTransportType 返回实际传输类型。优先读 Type，否则根据 Command/URL 自动推断。
func (c ExternalMCPServerConfig) GetTransportType() string {
	if c.Type != "" {
		return c.Type
	}
	if c.Command != "" {
		return "stdio"
	}
	if c.URL != "" {
		return "http"
	}
	return ""
}

type ToolConfig struct {
	Name             string            `yaml:"name"`
	Command          string            `yaml:"command"`
	Args             []string          `yaml:"args,omitempty"`              // 固定参数（可选）
	ShortDescription string            `yaml:"short_description,omitempty"` // 简短描述（用于工具列表，减少token消耗）
	Description      string            `yaml:"description"`                 // 详细描述（用于工具文档）
	Enabled          bool              `yaml:"enabled"`
	Parameters       []ParameterConfig `yaml:"parameters,omitempty"`         // 参数定义（可选）
	ArgMapping       string            `yaml:"arg_mapping,omitempty"`        // 参数映射方式: "auto", "manual", "template"（可选）
	AllowedExitCodes []int             `yaml:"allowed_exit_codes,omitempty"` // 允许的退出码列表（某些工具在成功时也返回非零退出码）
}

// ParameterConfig 参数配置
type ParameterConfig struct {
	Name        string      `yaml:"name"`                // 参数名称
	Type        string      `yaml:"type"`                // 参数类型: string, int, bool, array
	Description string      `yaml:"description"`         // 参数描述
	Required    bool        `yaml:"required,omitempty"`  // 是否必需
	Default     interface{} `yaml:"default,omitempty"`   // 默认值
	ItemType    string      `yaml:"item_type,omitempty"` // 当 type 为 array 时，数组元素类型，如 string, number, object
	Flag        string      `yaml:"flag,omitempty"`      // 命令行标志，如 "-u", "--url", "-p"
	Position    *int        `yaml:"position,omitempty"`  // 位置参数的位置（从0开始）
	Format      string      `yaml:"format,omitempty"`    // 参数格式: "flag", "positional", "combined" (flag=value), "template"
	Template    string      `yaml:"template,omitempty"`  // 模板字符串，如 "{flag} {value}" 或 "{value}"
	Options     []string    `yaml:"options,omitempty"`   // 可选值列表（用于枚举）
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}
	if cfg.ToolGuard != nil {
		if err := validateToolGuardYAML(data); err != nil {
			return nil, fmt.Errorf("调用拦截配置无效: %w", err)
		}
	}
	if _, err := toolguard.Compile(cfg.EffectiveToolGuard()); err != nil {
		return nil, fmt.Errorf("调用拦截配置无效: %w", err)
	}

	if cfg.Auth.SessionDurationHours <= 0 {
		cfg.Auth.SessionDurationHours = 12
	}
	if cfg.Audit.MaxDetailBytes <= 0 {
		cfg.Audit.MaxDetailBytes = 8192
	}
	cfg.NormalizeAIProviderProfiles()
	cfg.ApplyDefaultAIChannel()
	if err := validateOpenAIOutputLimits(cfg.OpenAI); err != nil {
		return nil, err
	}
	// 如果配置了工具目录，从目录加载工具配置
	if cfg.Security.ToolsDir != "" {
		inlineTools := append([]ToolConfig(nil), cfg.Security.Tools...)
		toolsDir := ResolveToolsDir(cfg.Security.ToolsDir, path)
		merged, err := MergeToolsFromDir(toolsDir, inlineTools)
		if err != nil {
			return nil, fmt.Errorf("从工具目录加载工具配置失败: %w", err)
		}
		cfg.Security.Tools = merged
	}

	// 外部 MCP：迁移 + 环境变量展开
	if cfg.ExternalMCP.Servers != nil {
		for name, serverCfg := range cfg.ExternalMCP.Servers {
			// 官方 disabled 字段 → ExternalMCPEnable
			if serverCfg.Disabled {
				serverCfg.ExternalMCPEnable = false
			} else if !serverCfg.ExternalMCPEnable {
				// 默认启用
				serverCfg.ExternalMCPEnable = true
			}

			// 展开所有 ${VAR} / ${VAR:-default} 环境变量引用
			ExpandConfigEnv(&serverCfg)

			cfg.ExternalMCP.Servers[name] = serverCfg
		}
	}

	// 从角色目录加载角色配置
	if cfg.RolesDir != "" {
		configDir := filepath.Dir(path)
		rolesDir := cfg.RolesDir

		// 如果是相对路径，相对于配置文件所在目录
		if !filepath.IsAbs(rolesDir) {
			rolesDir = filepath.Join(configDir, rolesDir)
		}

		roles, err := LoadRolesFromDir(rolesDir)
		if err != nil {
			return nil, fmt.Errorf("从角色目录加载角色配置失败: %w", err)
		}

		cfg.Roles = roles
	} else {
		// 如果未配置 roles_dir，初始化为空 map
		if cfg.Roles == nil {
			cfg.Roles = make(map[string]RoleConfig)
		}
	}

	if err := ValidateWecomConfig(cfg.Robots.Wecom); err != nil {
		return nil, err
	}
	if err := ValidateRobotsAuthorization(cfg.Robots); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func validateOpenAIOutputLimits(openAI OpenAIConfig) error {
	if openAI.MaxCompletionTokens < 0 {
		return fmt.Errorf("openai.max_completion_tokens 必须为正数")
	}
	return nil
}

func EnsureLocalConfig(path string) (EnsureLocalConfigResult, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = "config.yaml"
	}

	if _, err := os.Stat(path); err == nil {
		return EnsureLocalConfigResult{}, nil
	} else if !os.IsNotExist(err) {
		return EnsureLocalConfigResult{}, fmt.Errorf("检查配置文件失败: %w", err)
	}

	examplePath := filepath.Join(filepath.Dir(path), "config.example.yaml")
	if _, err := os.Stat(examplePath); err != nil {
		if os.IsNotExist(err) {
			if alt := "config.example.yaml"; examplePath != alt {
				if _, altErr := os.Stat(alt); altErr == nil {
					examplePath = alt
				} else {
					return EnsureLocalConfigResult{}, fmt.Errorf("配置文件 %s 不存在，且未找到模板 %s", path, examplePath)
				}
			} else {
				return EnsureLocalConfigResult{}, fmt.Errorf("配置文件 %s 不存在，且未找到模板 %s", path, examplePath)
			}
		} else {
			return EnsureLocalConfigResult{}, fmt.Errorf("检查配置模板失败: %w", err)
		}
	}

	data, err := os.ReadFile(examplePath)
	if err != nil {
		return EnsureLocalConfigResult{}, fmt.Errorf("读取配置模板失败: %w", err)
	}

	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return EnsureLocalConfigResult{}, fmt.Errorf("创建配置目录失败: %w", err)
		}
	}
	if err := os.WriteFile(path, data, fs.FileMode(0600)); err != nil {
		return EnsureLocalConfigResult{}, fmt.Errorf("创建配置文件失败: %w", err)
	}

	return EnsureLocalConfigResult{
		Created:     true,
		ExamplePath: examplePath,
	}, nil
}

func PrintBootstrapAdminPassword(password string) {
	termout.PrintBootstrapAdminCredentials(password)
}

// generateRandomToken 生成用于 MCP 鉴权的随机字符串（64 位十六进制）
func generateRandomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// persistMCPAuth 将 MCP 的 auth_header / auth_header_value 写回配置文件
func persistMCPAuth(path string, mcp *MCPConfig) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	inMcpBlock := false
	mcpIndent := -1

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !inMcpBlock {
			if strings.HasPrefix(trimmed, "mcp:") {
				inMcpBlock = true
				mcpIndent = len(line) - len(strings.TrimLeft(line, " "))
			}
			continue
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		leadingSpaces := len(line) - len(strings.TrimLeft(line, " "))
		if leadingSpaces <= mcpIndent {
			inMcpBlock = false
			mcpIndent = -1
			if strings.HasPrefix(trimmed, "mcp:") {
				inMcpBlock = true
				mcpIndent = leadingSpaces
			}
			continue
		}

		prefix := line[:leadingSpaces]
		rest := strings.TrimSpace(line[leadingSpaces:])
		comment := ""
		if idx := strings.Index(line, "#"); idx >= 0 {
			comment = strings.TrimRight(line[idx:], " ")
		}
		withComment := ""
		if comment != "" {
			if !strings.HasPrefix(comment, " ") {
				withComment = " "
			}
			withComment += comment
		}

		if strings.HasPrefix(rest, "auth_header_value:") {
			lines[i] = fmt.Sprintf("%sauth_header_value: %q%s", prefix, mcp.AuthHeaderValue, withComment)
		} else if strings.HasPrefix(rest, "auth_header:") {
			lines[i] = fmt.Sprintf("%sauth_header: %q%s", prefix, mcp.AuthHeader, withComment)
		}
	}

	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644)
}

// EnsureMCPAuth only provisions the privileged static service credential when
// global service access was explicitly enabled.
func EnsureMCPAuth(path string, cfg *Config) error {
	if !cfg.MCP.Enabled || !cfg.MCP.AllowGlobalAccess || strings.TrimSpace(cfg.MCP.AuthHeaderValue) != "" {
		return nil
	}
	token, err := generateRandomToken()
	if err != nil {
		return fmt.Errorf("生成 MCP 鉴权密钥失败: %w", err)
	}
	cfg.MCP.AuthHeaderValue = token
	if strings.TrimSpace(cfg.MCP.AuthHeader) == "" {
		cfg.MCP.AuthHeader = "X-MCP-Token"
	}
	return persistMCPAuth(path, &cfg.MCP)
}

// PrintMCPConfigJSON 向终端输出 MCP 配置的 JSON，可直接复制到 Cursor / Claude Code 的 mcp 配置中使用
func PrintMCPConfigJSON(mcp MCPConfig) {
	if !mcp.Enabled {
		return
	}
	hostForURL := strings.TrimSpace(mcp.Host)
	if hostForURL == "" || hostForURL == "0.0.0.0" {
		hostForURL = "localhost"
	}
	url := fmt.Sprintf("http://%s:%d/mcp", hostForURL, mcp.Port)
	headers := map[string]string{"Authorization": "Bearer <USER_SESSION_TOKEN>"}
	if mcp.AllowGlobalAccess && mcp.AuthHeader != "" {
		delete(headers, "Authorization")
		headers[mcp.AuthHeader] = mcp.AuthHeaderValue
	}
	serverEntry := map[string]interface{}{
		"url": url,
	}
	if len(headers) > 0 {
		serverEntry["headers"] = headers
	}
	// Claude Code 需要 type: "http"
	serverEntry["type"] = "http"
	out := map[string]interface{}{
		"mcpServers": map[string]interface{}{
			"cyberstrike-ai": serverEntry,
		},
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println("[CyberStrikeAI] MCP 配置（可复制到 Cursor / Claude Code 使用）：")
	fmt.Println("  Cursor: 放入 ~/.cursor/mcp.json 的 mcpServers，或项目 .cursor/mcp.json")
	fmt.Println("  Claude Code: 放入 .mcp.json 或 ~/.claude.json 的 mcpServers")
	fmt.Println("----------------------------------------------------------------")
	fmt.Println(string(b))
	fmt.Println("----------------------------------------------------------------")
}

// ResolveToolsDir 将 tools_dir 解析为绝对路径（相对路径相对于 configPath 所在目录）。
func ResolveToolsDir(toolsDir, configPath string) string {
	toolsDir = strings.TrimSpace(toolsDir)
	if toolsDir == "" {
		return ""
	}
	if filepath.IsAbs(toolsDir) {
		return toolsDir
	}
	return filepath.Join(filepath.Dir(configPath), toolsDir)
}

// MergeToolsFromDir 从目录加载工具并与 inline 列表合并：目录中的工具优先，主配置中的工具作为补充。
func MergeToolsFromDir(toolsDir string, inlineTools []ToolConfig) ([]ToolConfig, error) {
	dirTools, err := LoadToolsFromDir(toolsDir)
	if err != nil {
		return nil, err
	}
	existing := make(map[string]bool, len(dirTools))
	for _, tool := range dirTools {
		existing[tool.Name] = true
	}
	merged := append([]ToolConfig(nil), dirTools...)
	for _, tool := range inlineTools {
		if !existing[tool.Name] {
			merged = append(merged, tool)
		}
	}
	return merged, nil
}

// loadInlineSecurityToolsFromYAML 读取 config.yaml 中 security.tools（不含 tools_dir 扫描结果）。
func loadInlineSecurityToolsFromYAML(configPath string) ([]ToolConfig, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}
	var partial struct {
		Security struct {
			Tools []ToolConfig `yaml:"tools"`
		} `yaml:"security"`
	}
	if err := yaml.Unmarshal(data, &partial); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}
	if partial.Security.Tools == nil {
		return []ToolConfig{}, nil
	}
	return partial.Security.Tools, nil
}

// ReloadSecurityToolsFromDir 从 tools_dir 重新加载工具并更新 cfg.Security.Tools（ApplyConfig 热重载用）。
func ReloadSecurityToolsFromDir(cfg *Config, configPath string) error {
	if cfg == nil || strings.TrimSpace(cfg.Security.ToolsDir) == "" {
		return nil
	}
	inlineTools, err := loadInlineSecurityToolsFromYAML(configPath)
	if err != nil {
		return err
	}
	toolsDir := ResolveToolsDir(cfg.Security.ToolsDir, configPath)
	merged, err := MergeToolsFromDir(toolsDir, inlineTools)
	if err != nil {
		return fmt.Errorf("从工具目录加载工具配置失败: %w", err)
	}
	cfg.Security.Tools = merged
	return nil
}

// LoadToolsFromDir 从目录加载所有工具配置文件
func LoadToolsFromDir(dir string) ([]ToolConfig, error) {
	var tools []ToolConfig

	// 检查目录是否存在
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return tools, nil // 目录不存在时返回空列表，不报错
	}

	// 读取目录中的所有 .yaml 和 .yml 文件
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("读取工具目录失败: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}

		filePath := filepath.Join(dir, name)
		tool, err := LoadToolFromFile(filePath)
		if err != nil {
			// 记录错误但继续加载其他文件
			fmt.Printf("警告: 加载工具配置文件 %s 失败: %v\n", filePath, err)
			continue
		}

		tools = append(tools, *tool)
	}

	return tools, nil
}

// LoadToolFromFile 从单个文件加载工具配置
func LoadToolFromFile(path string) (*ToolConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败: %w", err)
	}

	var tool ToolConfig
	if err := yaml.Unmarshal(data, &tool); err != nil {
		return nil, fmt.Errorf("解析工具配置失败: %w", err)
	}

	// 验证必需字段
	if tool.Name == "" {
		return nil, fmt.Errorf("工具名称不能为空")
	}
	if tool.Command == "" {
		return nil, fmt.Errorf("工具命令不能为空")
	}

	return &tool, nil
}

// LoadRolesFromDir 从目录加载所有角色配置文件
func LoadRolesFromDir(dir string) (map[string]RoleConfig, error) {
	roles := make(map[string]RoleConfig)

	// 检查目录是否存在
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return roles, nil // 目录不存在时返回空map，不报错
	}

	// 读取目录中的所有 .yaml 和 .yml 文件
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("读取角色目录失败: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}

		filePath := filepath.Join(dir, name)
		role, err := LoadRoleFromFile(filePath)
		if err != nil {
			// 记录错误但继续加载其他文件
			fmt.Printf("警告: 加载角色配置文件 %s 失败: %v\n", filePath, err)
			continue
		}

		// 使用角色名称作为key
		roleName := role.Name
		if roleName == "" {
			// 如果角色名称为空，使用文件名（去掉扩展名）作为名称
			roleName = strings.TrimSuffix(strings.TrimSuffix(name, ".yaml"), ".yml")
			role.Name = roleName
		}

		roles[roleName] = *role
	}

	return roles, nil
}

// LoadRoleFromFile 从单个文件加载角色配置
func LoadRoleFromFile(path string) (*RoleConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败: %w", err)
	}

	var role RoleConfig
	if err := yaml.Unmarshal(data, &role); err != nil {
		return nil, fmt.Errorf("解析角色配置失败: %w", err)
	}

	// 处理 icon 字段：如果包含 Unicode 转义格式（\U0001F3C6），转换为实际的 Unicode 字符
	// Go 的 yaml 库可能不会自动解析 \U 转义序列，需要手动转换
	if role.Icon != "" {
		icon := role.Icon
		// 去除可能的引号
		icon = strings.Trim(icon, `"`)

		// 检查是否是 Unicode 转义格式 \U0001F3C6（8位十六进制）或 \uXXXX（4位十六进制）
		if len(icon) >= 3 && icon[0] == '\\' {
			if icon[1] == 'U' && len(icon) >= 10 {
				// \U0001F3C6 格式（8位十六进制）
				if codePoint, err := strconv.ParseInt(icon[2:10], 16, 32); err == nil {
					role.Icon = string(rune(codePoint))
				}
			} else if icon[1] == 'u' && len(icon) >= 6 {
				// \uXXXX 格式（4位十六进制）
				if codePoint, err := strconv.ParseInt(icon[2:6], 16, 32); err == nil {
					role.Icon = string(rune(codePoint))
				}
			}
		}
	}

	// 验证必需字段
	if role.Name == "" {
		// 如果名称为空，尝试从文件名获取
		baseName := filepath.Base(path)
		role.Name = strings.TrimSuffix(strings.TrimSuffix(baseName, ".yaml"), ".yml")
	}

	return &role, nil
}

func Default() *Config {
	strictRobotIdentity := true
	return &Config{
		Server: ServerConfig{
			Host: "0.0.0.0",
			Port: 8080,
		},
		Log: LogConfig{
			Level:  "info",
			Output: "stdout",
		},
		MCP: MCPConfig{
			Enabled: false,
			Host:    "127.0.0.1",
			Port:    8081,
		},
		AI: AIConfig{
			DefaultChannel: "default",
			Channels: map[string]AIChannelConfig{
				"default": {
					Name:                "Default",
					Provider:            "openai_compatible",
					BaseURL:             "https://api.openai.com/v1",
					Model:               "gpt-4",
					MaxTotalTokens:      120000,
					MaxCompletionTokens: DefaultMaxCompletionTokens,
				},
			},
		},
		OpenAI: OpenAIConfig{},
		Agent: AgentConfig{
			MaxIterations:                      30,  // 默认最大迭代次数
			ToolTimeoutMinutes:                 10,  // 单次工具执行默认最多 10 分钟，避免异常长时间占用
			ToolWaitTimeoutSeconds:             60,  // 外部 MCP 工具单轮最多等待 60 秒，超时后返回 execution_id 可继续等待
			ExternalMCPMaxConcurrentPerServer:  2,   // 单个外部 MCP server 默认最多 2 个工具同时执行
			ExternalMCPMaxConcurrentTotal:      16,  // 外部 MCP 工具全局默认最多 16 个同时执行
			ExternalMCPCircuitFailureThreshold: 3,   // 单个 server 连续 3 次失败后临时熔断
			ExternalMCPCircuitCooldownSeconds:  60,  // 熔断默认冷却 60 秒
			ShellNoOutputTimeoutSeconds:        300, // execute/exec 无新输出空闲终止（秒）；-1 关闭
		},
		Security: SecurityConfig{
			Tools:    []ToolConfig{}, // 工具配置应该从 config.yaml 或 tools/ 目录加载
			ToolsDir: "tools",        // 默认工具目录
		},
		Database: DatabaseConfig{
			Path:            "data/conversations.db",
			KnowledgeDBPath: "data/knowledge.db", // 默认知识库数据库路径
		},
		Auth: AuthConfig{
			SessionDurationHours: 12,
		},
		Audit: func() AuditConfig {
			on := true
			return AuditConfig{
				RetentionDays:  90,
				MaxDetailBytes: 8192,
				Enabled:        &on,
			}
		}(),
		Monitor: func() MonitorConfig {
			days := 90
			return MonitorConfig{RetentionDays: &days}
		}(),
		Robots: RobotsConfig{
			Session: RobotSessionConfig{
				StrictUserIdentity: &strictRobotIdentity,
			},
		},
		Knowledge: KnowledgeConfig{
			Enabled:  true,
			BasePath: "knowledge_base",
			Embedding: EmbeddingConfig{
				Provider: "openai",
				Model:    "text-embedding-3-small",
				BaseURL:  "https://api.openai.com/v1",
			},
			Retrieval: RetrievalConfig{
				TopK:                5,
				SimilarityThreshold: 0.65,
				MultiQuery:          MultiQueryConfig{MaxQueries: 4},
				Rerank:              RerankConfig{},
				PostRetrieve: PostRetrieveConfig{
					PrefetchTopK: 20,
				},
			},
			Indexing: IndexingConfig{
				ChunkStrategy:         "markdown_then_recursive",
				RequestTimeoutSeconds: 120,
				ChunkSize:             768, // 增加到 768，更好的上下文保持
				ChunkOverlap:          50,
				MaxChunksPerItem:      20, // 限制单个知识项最多 20 个块，避免消耗过多配额
				BatchSize:             64,
				PreferSourceFile:      false,
				MaxRPM:                100, // 默认 100 RPM，避免 429 错误
				RateLimitDelayMs:      600, // 600ms 间隔，对应 100 RPM
				MaxRetries:            3,
				RetryDelayMs:          1000,
				SubIndexes:            nil,
			},
		},
	}
}

// C2Config 内置 C2 模块开关（与知识库 enabled 语义一致：关闭后不初始化监听器、不注册 C2 MCP 工具）。
type C2Config struct {
	// Enabled 为 nil 表示未写配置，按 true 处理（兼容旧 config.yaml）
	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
}

// EnabledEffective 返回是否启用 C2；未显式配置时默认启用。
func (c C2Config) EnabledEffective() bool {
	if c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

// C2Public 返回给前端的 C2 状态（仅标量）。
type C2Public struct {
	Enabled bool `json:"enabled"`
}

// Public 将内部配置转为 API 响应。
func (c C2Config) Public() C2Public {
	return C2Public{Enabled: c.EnabledEffective()}
}

// C2APIUpdate 设置页/API 更新 C2 开关。
type C2APIUpdate struct {
	Enabled bool `json:"enabled"`
}

// KnowledgeConfig 知识库配置
type KnowledgeConfig struct {
	Enabled   bool            `yaml:"enabled" json:"enabled"`     // 是否启用知识检索
	BasePath  string          `yaml:"base_path" json:"base_path"` // 知识库路径
	Embedding EmbeddingConfig `yaml:"embedding" json:"embedding"`
	Retrieval RetrievalConfig `yaml:"retrieval" json:"retrieval"`
	Indexing  IndexingConfig  `yaml:"indexing,omitempty" json:"indexing,omitempty"` // 索引构建配置
}

// IndexingConfig 索引构建配置（用于控制知识库索引构建时的行为）
type IndexingConfig struct {
	// ChunkStrategy: "markdown_then_recursive"（默认，Eino Markdown 标题切分后再递归切）或 "recursive"（仅递归切分）
	ChunkStrategy string `yaml:"chunk_strategy,omitempty" json:"chunk_strategy,omitempty"`
	// RequestTimeoutSeconds 嵌入 HTTP 客户端超时（秒），0 表示使用默认 120
	RequestTimeoutSeconds int `yaml:"request_timeout_seconds,omitempty" json:"request_timeout_seconds,omitempty"`
	// 分块配置
	ChunkSize        int `yaml:"chunk_size,omitempty" json:"chunk_size,omitempty"`                   // 每个块的最大 token 数（估算），默认 512
	ChunkOverlap     int `yaml:"chunk_overlap,omitempty" json:"chunk_overlap,omitempty"`             // 块之间的重叠 token 数，默认 50
	MaxChunksPerItem int `yaml:"max_chunks_per_item,omitempty" json:"max_chunks_per_item,omitempty"` // 单个知识项的最大块数量，0 表示不限制

	// PreferSourceFile 为 true 时优先用 Eino FileLoader 从 file_path 读原文再索引（与库内 content 不一致时以磁盘为准）
	PreferSourceFile bool `yaml:"prefer_source_file,omitempty" json:"prefer_source_file,omitempty"`

	// 速率限制配置（用于避免 API 速率限制）
	RateLimitDelayMs int `yaml:"rate_limit_delay_ms,omitempty" json:"rate_limit_delay_ms,omitempty"` // 请求间隔时间（毫秒），0 表示不使用固定延迟
	MaxRPM           int `yaml:"max_rpm,omitempty" json:"max_rpm,omitempty"`                         // 每分钟最大请求数，0 表示不限制

	// 重试配置（用于处理临时错误）
	MaxRetries   int `yaml:"max_retries,omitempty" json:"max_retries,omitempty"`       // 最大重试次数，默认 3
	RetryDelayMs int `yaml:"retry_delay_ms,omitempty" json:"retry_delay_ms,omitempty"` // 重试间隔（毫秒），默认 1000

	// BatchSize 嵌入批大小（SQLite 索引写入），0 表示默认 64
	BatchSize int `yaml:"batch_size,omitempty" json:"batch_size,omitempty"`
	// SubIndexes 传入 Eino indexer.WithSubIndexes（逻辑分区标记，随 Document 元数据传递）
	SubIndexes []string `yaml:"sub_indexes,omitempty" json:"sub_indexes,omitempty"`
}

// EmbeddingConfig 嵌入配置
type EmbeddingConfig struct {
	Provider string `yaml:"provider" json:"provider"` // 嵌入模型提供商
	Model    string `yaml:"model" json:"model"`       // 模型名称
	BaseURL  string `yaml:"base_url" json:"base_url"` // API Base URL
	APIKey   string `yaml:"api_key" json:"api_key"`   // API Key（从OpenAI配置继承）
}

// PostRetrieveConfig 检索后处理：固定对正文做规范化去重（最佳实践）、上下文预算截断；PrefetchTopK 用于多取候选再收敛到 top_k。
type PostRetrieveConfig struct {
	// PrefetchTopK 向量检索阶段每条 MultiQuery 变体最多保留的候选数；0 表示使用内置默认 max(top_k*4, 20)。
	PrefetchTopK int `yaml:"prefetch_top_k,omitempty" json:"prefetch_top_k,omitempty"`
	// MaxContextChars 返回文档内容总 Unicode 字符数上限（整段 chunk，不截断半段）；0 表示不限制。
	MaxContextChars int `yaml:"max_context_chars,omitempty" json:"max_context_chars,omitempty"`
	// MaxContextTokens 返回文档内容总 token 上限（tiktoken，按嵌入模型名映射，失败则 cl100k_base）；0 表示不限制。
	MaxContextTokens int `yaml:"max_context_tokens,omitempty" json:"max_context_tokens,omitempty"`
}

// MultiQueryConfig Eino MultiQuery 查询改写（始终启用，无关闭开关）。
type MultiQueryConfig struct {
	// MaxQueries LLM 生成的检索变体上限（含原问语义覆盖）；0 表示默认 4。
	MaxQueries int `yaml:"max_queries,omitempty" json:"max_queries,omitempty"`
}

func (c MultiQueryConfig) MaxQueriesEffective() int {
	if c.MaxQueries <= 0 {
		return 4
	}
	if c.MaxQueries > 8 {
		return 8
	}
	return c.MaxQueries
}

// RerankConfig 检索精排（始终启用）；支持 dashscope 与 Cohere 兼容 HTTP API。
type RerankConfig struct {
	// Provider: dashscope | cohere；空则按 base_url 自动推断。
	Provider string `yaml:"provider,omitempty" json:"provider,omitempty"`
	Model    string `yaml:"model,omitempty" json:"model,omitempty"`
	BaseURL  string `yaml:"base_url,omitempty" json:"base_url,omitempty"`
	APIKey   string `yaml:"api_key,omitempty" json:"api_key,omitempty"`
}

func (c RerankConfig) ProviderEffective(baseURL string) string {
	p := strings.TrimSpace(strings.ToLower(c.Provider))
	if p != "" {
		return p
	}
	u := strings.ToLower(baseURL)
	if strings.Contains(u, "dashscope") {
		return "dashscope"
	}
	return "cohere"
}

func (c RerankConfig) ModelEffective(provider string) string {
	if m := strings.TrimSpace(c.Model); m != "" {
		return m
	}
	if provider == "dashscope" {
		return "gte-rerank"
	}
	return "rerank-multilingual-v3.0"
}

// RetrievalConfig 检索配置
type RetrievalConfig struct {
	TopK                int     `yaml:"top_k" json:"top_k"`                               // 检索Top-K
	SimilarityThreshold float64 `yaml:"similarity_threshold" json:"similarity_threshold"` // 余弦相似度阈值
	// SubIndexFilter 非空时仅保留 sub_indexes 含该标签（逗号分隔之一）的行；sub_indexes 为空的旧行仍返回。
	SubIndexFilter string           `yaml:"sub_index_filter,omitempty" json:"sub_index_filter,omitempty"`
	MultiQuery     MultiQueryConfig `yaml:"multi_query" json:"multi_query"`
	Rerank         RerankConfig     `yaml:"rerank" json:"rerank"`
	// PostRetrieve 检索后处理（去重、预算截断）；精排在 MultiQuery 融合后执行。
	PostRetrieve PostRetrieveConfig `yaml:"post_retrieve,omitempty" json:"post_retrieve,omitempty"`
}

// RolesConfig 角色配置（已废弃，使用 map[string]RoleConfig 替代）
// 保留此类型以兼容旧代码，但建议直接使用 map[string]RoleConfig
type RolesConfig struct {
	Roles map[string]RoleConfig `yaml:"roles,omitempty" json:"roles,omitempty"`
}

// RoleConfig 单个角色配置
type RoleConfig struct {
	Name            string   `yaml:"name" json:"name"`                                             // 角色名称
	Description     string   `yaml:"description" json:"description"`                               // 角色描述
	UserPrompt      string   `yaml:"user_prompt" json:"user_prompt"`                               // 用户提示词(追加到用户消息前)
	Icon            string   `yaml:"icon,omitempty" json:"icon,omitempty"`                         // 角色图标（可选）
	Tools           []string `yaml:"tools,omitempty" json:"tools,omitempty"`                       // 关联的工具列表（toolKey格式，如 "toolName" 或 "mcpName::toolName"）
	MCPs            []string `yaml:"mcps,omitempty" json:"mcps,omitempty"`                         // 向后兼容：关联的MCP服务器列表（已废弃，使用tools替代）
	WorkflowID      string   `yaml:"workflow_id,omitempty" json:"workflow_id,omitempty"`           // 可选：绑定工作流 ID
	WorkflowVersion string   `yaml:"workflow_version,omitempty" json:"workflow_version,omitempty"` // latest 或具体版本号；空等同 latest
	WorkflowPolicy  string   `yaml:"workflow_policy,omitempty" json:"workflow_policy,omitempty"`   // auto | off；空且 workflow_id 非空时按 auto
	Enabled         bool     `yaml:"enabled" json:"enabled"`                                       // 是否启用
}
