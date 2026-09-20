package multiagent

import (
	"encoding/json"
	"strings"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/einomcp"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

type einoRunResultBuilderConfig struct {
	OrchMode         string
	EmptyHint        string
	RunMessages      *einoRunMessageAccumulator
	AssistantOutput  *einoAssistantOutputAccumulator
	SnapshotMCPIDs   func() []string
	ModelFacingTrace func() []adk.Message
}

type einoRunResultBuilder struct {
	cfg einoRunResultBuilderConfig
}

func newEinoRunResultBuilder(cfg einoRunResultBuilderConfig) *einoRunResultBuilder {
	return &einoRunResultBuilder{cfg: cfg}
}

func (b *einoRunResultBuilder) BuildPartial(runErr error) (*RunResult, error) {
	if b == nil || b.cfg.RunMessages == nil || !b.cfg.RunMessages.HasNewMessages() {
		return nil, runErr
	}
	return b.build(true), runErr
}

func (b *einoRunResultBuilder) BuildFinal() *RunResult {
	if b == nil {
		return &RunResult{}
	}
	return b.build(false)
}

func (b *einoRunResultBuilder) build(partial bool) *RunResult {
	var runMsgs []adk.Message
	if b.cfg.RunMessages != nil {
		runMsgs = b.cfg.RunMessages.NewMessages()
	}
	var lastAssistant string
	var lastPlanExecuteExecutor string
	if b.cfg.AssistantOutput != nil {
		lastAssistant = b.cfg.AssistantOutput.LastAssistant()
		lastPlanExecuteExecutor = b.cfg.AssistantOutput.LastPlanExecuteExecutor()
	}
	var modelFacing []adk.Message
	if b.cfg.ModelFacingTrace != nil {
		modelFacing = b.cfg.ModelFacingTrace()
	}
	var ids []string
	if b.cfg.SnapshotMCPIDs != nil {
		ids = b.cfg.SnapshotMCPIDs()
	}
	return buildEinoRunResultFromAccumulated(
		b.cfg.OrchMode,
		runMsgs,
		modelFacing,
		lastAssistant,
		lastPlanExecuteExecutor,
		b.cfg.EmptyHint,
		ids,
		partial,
	)
}

func einoPartialRunLastOutputHint() string {
	return "[执行未正常结束（用户停止、超时或异常）。续跑时请基于上文已产生的工具与结果继续，勿重复已完成步骤。]\n" +
		"[Run ended abnormally; continue from the trace above without repeating completed steps.]"
}

func buildEinoRunResultFromAccumulated(
	orchMode string,
	runAccumulatedMsgs []adk.Message,
	persistMsgs []adk.Message,
	lastAssistant string,
	lastPlanExecuteExecutor string,
	emptyHint string,
	mcpIDs []string,
	partial bool,
) *RunResult {
	traceForJSON := persistMsgs
	traceJSON := ""
	if len(traceForJSON) > 0 {
		traceForJSON = markModelFacingTraceForPersistence(traceForJSON)
		if histJSON, err := json.Marshal(traceForJSON); err == nil {
			traceJSON = string(histJSON)
		}
	}
	cleaned := strings.TrimSpace(lastAssistant)
	if orchMode == "plan_execute" {
		if e := strings.TrimSpace(lastPlanExecuteExecutor); e != "" {
			cleaned = e
		} else {
			cleaned = UnwrapPlanExecuteUserText(cleaned)
		}
	}
	// exit.final_result 是正式交付物：即使助手正文已有过渡语（如「交付终审报告：」），
	// 也必须优先采用 exit 内容，避免 supervisor 等模式只展示空壳开场白。
	if exitFinal := strings.TrimSpace(einoExtractExitDeliverableFromMsgs(runAccumulatedMsgs)); exitFinal != "" {
		cleaned = einoMergeAssistantIntroWithExitFinal(cleaned, exitFinal)
	} else if cleaned == "" {
		if fb := strings.TrimSpace(einoExtractFallbackAssistantFromMsgs(runAccumulatedMsgs)); fb != "" {
			cleaned = fb
			if orchMode == "plan_execute" {
				cleaned = UnwrapPlanExecuteUserText(cleaned)
			}
		}
	}
	cleaned = dedupeRepeatedParagraphs(cleaned, 80)
	cleaned = dedupeParagraphsByLineFingerprint(cleaned, 100)
	const maxResponseRunes = 100000
	if rs := []rune(cleaned); len(rs) > maxResponseRunes {
		cleaned = string(rs[:maxResponseRunes]) + "\n\n... (response truncated / 响应已截断)"
	}
	lastOut := cleaned
	resp := cleaned
	if partial && cleaned == "" {
		lastOut = einoPartialRunLastOutputHint()
		resp = emptyHint
	}
	out := &RunResult{
		Response:             resp,
		MCPExecutionIDs:      mcpIDs,
		LastAgentTraceInput:  traceJSON,
		LastAgentTraceOutput: lastOut,
	}
	if !partial && out.Response == "" {
		out.Response = emptyHint
		out.LastAgentTraceOutput = out.Response
	}
	return out
}

func markModelFacingTraceForPersistence(msgs []adk.Message) []adk.Message {
	out := cloneADKMessagesForTrace(msgs)
	if len(out) == 0 || out[0] == nil {
		return out
	}
	if out[0].Extra == nil {
		out[0].Extra = make(map[string]any, 1)
	}
	out[0].Extra[agent.ModelFacingTraceVersionKey] = 1
	return out
}

// einoExtractExitDeliverableFromMsgs 从轨迹中提取当前轮次的 exit 正式交付物
//（工具输出或 assistant 调用 exit 时的 arguments.final_result）。
// 若更靠近末尾出现了非 exit 的工具结果，则认为 exit 不是终态交付。
func einoExtractExitDeliverableFromMsgs(msgs []adk.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m == nil {
			continue
		}
		switch m.Role {
		case schema.Tool:
			if strings.EqualFold(strings.TrimSpace(m.ToolName), adk.ToolInfoExit.Name) {
				content := strings.TrimSpace(m.Content)
				if content != "" && !strings.HasPrefix(content, einomcp.ToolErrorPrefix) {
					return content
				}
				// exit 工具输出为空时，继续向前从 assistant 参数回填。
				continue
			}
			return ""
		case schema.Assistant:
			if s := einoExtractExitFinalFromAssistantToolCalls(m); s != "" {
				return s
			}
			if einoAssistantHasNonExitToolCall(m) {
				return ""
			}
		}
	}
	return ""
}

func einoAssistantHasNonExitToolCall(msg *schema.Message) bool {
	if msg == nil {
		return false
	}
	for _, tc := range msg.ToolCalls {
		if !strings.EqualFold(strings.TrimSpace(tc.Function.Name), adk.ToolInfoExit.Name) {
			return true
		}
	}
	return false
}

// einoMergeAssistantIntroWithExitFinal 合并助手过渡语与 exit 交付正文。
// exit 内容优先；若助手正文只是开场白且未被 exit 文本包含，则前置保留。
func einoMergeAssistantIntroWithExitFinal(assistant, exitFinal string) string {
	assistant = strings.TrimSpace(assistant)
	exitFinal = strings.TrimSpace(exitFinal)
	if exitFinal == "" {
		return assistant
	}
	if assistant == "" || assistant == exitFinal {
		return exitFinal
	}
	if strings.Contains(exitFinal, assistant) {
		return exitFinal
	}
	if strings.Contains(assistant, exitFinal) {
		return assistant
	}
	return assistant + "\n\n" + exitFinal
}

// einoExtractFallbackAssistantFromMsgs 在「主通道未产出助手正文」时，从 Eino ADK
// 原生消息轨迹中回填用户可见回复。这里保持克制：只采纳倒序最近的可交付终态，
// 避免把工具调用前的过渡语或子任务过程误升为最终回复。
//
// 可交付终态：
// - exit 工具输出；
// - assistant 调用 exit 时 arguments.final_result；
// - 没有后续普通工具结果截断的纯 assistant 正文。
func einoExtractFallbackAssistantFromMsgs(msgs []adk.Message) string {
	if s := einoExtractExitDeliverableFromMsgs(msgs); s != "" {
		return s
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m == nil {
			continue
		}
		switch m.Role {
		case schema.Tool:
			// 最近一条是普通工具结果：说明助手尚未给出最终正文，勿回退到更早过程语。
			return ""
		case schema.Assistant:
			if len(m.ToolCalls) == 0 {
				if content := strings.TrimSpace(m.Content); content != "" {
					return content
				}
			}
		}
	}
	return ""
}

func einoExtractExitFinalFromAssistantToolCalls(msg *schema.Message) string {
	if msg == nil || len(msg.ToolCalls) == 0 {
		return ""
	}
	for i := len(msg.ToolCalls) - 1; i >= 0; i-- {
		tc := msg.ToolCalls[i]
		if !strings.EqualFold(strings.TrimSpace(tc.Function.Name), adk.ToolInfoExit.Name) {
			continue
		}
		if s := einoParseExitFinalResultArguments(tc.Function.Arguments); s != "" {
			return s
		}
	}
	return ""
}

func einoParseExitFinalResultArguments(arguments string) string {
	arguments = strings.TrimSpace(arguments)
	if arguments == "" {
		return ""
	}
	var wrap struct {
		FinalResult json.RawMessage `json:"final_result"`
	}
	if err := json.Unmarshal([]byte(arguments), &wrap); err != nil || len(wrap.FinalResult) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(wrap.FinalResult, &s); err == nil {
		return strings.TrimSpace(s)
	}
	var anyVal interface{}
	if err := json.Unmarshal(wrap.FinalResult, &anyVal); err != nil {
		return ""
	}
	b, err := json.Marshal(anyVal)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
