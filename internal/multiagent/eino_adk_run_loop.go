package multiagent

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/einomcp"
	"cyberstrike-ai/internal/einoobserve"
	"cyberstrike-ai/internal/security"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// normalizeStreamingDelta 将可能是“累计片段”的 chunk 归一化为“纯增量”。
// 一些模型/桥接层在流式过程中会重复发送已输出前缀，前端若直接 buffer+=chunk 会出现重复文本。
//
// 注意：与 internal/openai.normalizeStreamingDelta 保持一致。
func normalizeStreamingDelta(current, incoming string) (next, delta string) {
	if incoming == "" {
		return current, ""
	}
	if current == "" {
		return incoming, incoming
	}
	if strings.HasPrefix(incoming, current) && len(incoming) > len(current) {
		return incoming, incoming[len(current):]
	}
	if incoming == current && utf8.RuneCountInString(current) > 1 {
		return current, ""
	}
	return current + incoming, incoming
}

func isInterruptContinue(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	return errors.Is(context.Cause(ctx), ErrInterruptContinue)
}

func isEinoStreamCanceled(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, adk.ErrStreamCanceled) {
		return true
	}
	var streamCanceled *adk.StreamCanceledError
	return errors.As(err, &streamCanceled)
}

func isEinoCancelError(err error) bool {
	if err == nil {
		return false
	}
	var cancelErr *adk.CancelError
	return errors.As(err, &cancelErr)
}

// isEinoVoluntaryCancelErr reports cancel signals produced by Agent Cancel /
// TurnLoop preempt (CancelError, ErrStreamCanceled, context.Canceled).
func isEinoVoluntaryCancelErr(err error) bool {
	if err == nil {
		return false
	}
	return isEinoCancelError(err) || isEinoStreamCanceled(err) || errors.Is(err, context.Canceled)
}

// isEinoTurnLoopPreemptErr is true when a cancel/stream-cancel leaked from the
// current agent turn while the host task context is still alive. TurnLoop
// interrupt-continue does not cancel the parent context; treating that leak as
// fatal would abort the whole run instead of starting the queued next turn.
func isEinoTurnLoopPreemptErr(ctx context.Context, err error) bool {
	if err == nil || !isEinoVoluntaryCancelErr(err) {
		return false
	}
	if ctx != nil && ctx.Err() != nil {
		return false
	}
	return true
}

func isEinoIterationLimitError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	if msg == "" {
		return false
	}
	return strings.Contains(msg, "max iteration") ||
		strings.Contains(msg, "maximum iteration") ||
		strings.Contains(msg, "maximum iterations") ||
		strings.Contains(msg, "iteration limit") ||
		strings.Contains(msg, "达到最大迭代")
}

// einoADKRunLoopArgs 将 Eino adk.Runner 事件循环从 RunDeepAgent / RunEinoSingleChatModelAgent 中抽出复用。
type einoADKRunLoopArgs struct {
	OrchMode             string
	OrchestratorName     string
	ConversationID       string
	Progress             func(eventType, message string, data interface{})
	Logger               *zap.Logger
	SnapshotMCPIDs       func() []string
	StreamsMainAssistant func(agent string) bool
	EinoRoleTag          func(agent string) string
	CheckpointDir        string
	// RunRetryMaxAttempts / RunRetryMaxBackoffSec：429、5xx、网络抖动时的指数退避续跑（0=默认 4 次 / 30s 上限）。
	RunRetryMaxAttempts   int
	RunRetryMaxBackoffSec int

	McpIDsMu *sync.Mutex
	McpIDs   *[]string

	// FilesystemMonitorAgent / FilesystemMonitorRecord 非 nil 时，将 Eino ADK filesystem 中间件工具（ls/read_file/write_file/edit_file/glob/grep）
	// 在完成时写入 MCP 监控；execute 仍由 eino_execute_monitor 记录，此处跳过。
	FilesystemMonitorAgent  *agent.Agent
	FilesystemMonitorRecord einomcp.ExecutionRecorder
	MCPExecutionBinder      *MCPExecutionBinder

	// ToolInvokeNotify 与 einomcp.ToolsFromDefinitions 共享：run loop 在迭代前 Set，execute/MCP 桥 Fire 时立即推送 tool_result（ADK 晚到经 toolResultEmitter 去重）。
	ToolInvokeNotify *einomcp.ToolInvokeNotifyHolder

	DA adk.Agent

	// EmptyResponseMessage 当未捕获到助手正文时的占位（多代理与单代理文案不同）。
	EmptyResponseMessage string

	// ModelFacingTrace 可选：由各 ChatModelAgent Handlers 链末尾中间件写入「即将送入模型」的消息快照；
	// 非空时优先用于 LastAgentTraceInput 序列化，使续跑与 summarization/reduction 后的上下文一致。
	ModelFacingTrace *modelFacingTraceHolder

	// EinoCallbacks 可选：为 ADK Runner 注入 eino [callbacks] 全链路观测（见 internal/einoobserve）。
	EinoCallbacks *config.MultiAgentEinoCallbacksConfig

	// MaxTotalTokens / ToolMaxBytes / ModelName 用于 context overflow 时的激进压缩续跑。
	MaxTotalTokens   int
	ToolMaxBytes     int
	ModelName        string
	MiddlewareConfig *config.MultiAgentEinoMiddlewareConfig

	// TurnLoopInterruptTimeout 仅供测试/特殊运行时覆盖；0 使用 EinoTurnLoopRuntime 默认值。
	TurnLoopInterruptTimeout time.Duration
}

func runEinoADKAgentLoop(ctx context.Context, args *einoADKRunLoopArgs, baseMsgs []adk.Message) (*RunResult, error) {
	if args == nil || args.DA == nil {
		return nil, fmt.Errorf("eino run loop: args or Agent is nil")
	}
	if args.McpIDs == nil {
		s := []string{}
		args.McpIDs = &s
	}
	if args.McpIDsMu == nil {
		args.McpIDsMu = &sync.Mutex{}
	}

	orchMode := args.OrchMode
	orchestratorName := args.OrchestratorName
	conversationID := args.ConversationID
	progress := args.Progress
	logger := args.Logger
	runID := newEinoRunID()
	progress = withEinoRunIDProgress(runID, progress)
	args.Progress = progress
	if logger != nil {
		logger.Info("eino run session started",
			zap.String("runId", runID),
			zap.String("conversationId", conversationID),
			zap.String("orchestration", orchMode),
			zap.String("orchestratorName", orchestratorName),
		)
	}
	snapshotMCPIDs := args.SnapshotMCPIDs
	if snapshotMCPIDs == nil {
		snapshotMCPIDs = func() []string { return nil }
	}
	streamsMainAssistant := args.StreamsMainAssistant
	if streamsMainAssistant == nil {
		streamsMainAssistant = func(agent string) bool {
			return agent == "" || agent == orchestratorName
		}
	}
	einoRoleTag := args.EinoRoleTag
	if einoRoleTag == nil {
		einoRoleTag = func(agent string) string {
			if streamsMainAssistant(agent) {
				return "orchestrator"
			}
			return "sub"
		}
	}
	// panic recovery：防止 Eino 框架内部 panic 导致整个 goroutine 崩溃、连接无法正常关闭。
	defer func() {
		if r := recover(); r != nil {
			if logger != nil {
				logger.Error("eino runner panic recovered", zap.Any("recover", r), zap.Stack("stack"))
			}
			if progress != nil {
				progress("error", fmt.Sprintf("Internal error: %v", r), map[string]interface{}{
					"conversationId": conversationID,
					"source":         "eino",
				})
			}
		}
	}()

	msgs := append([]adk.Message(nil), baseMsgs...)

	emptyHint := strings.TrimSpace(args.EmptyResponseMessage)
	if emptyHint == "" {
		// Legacy Chinese marker 未捕获到助手文本输出 stays byte-identical here:
		// IsEinoEmptyResponseResult / agentfinalizer / monitor.js
		// isEinoEmptyResponsePlaceholder match both markers (dual-accept).
		emptyHint = "(Eino session completed but no assistant text was captured. Check process details or logs.) " +
			"（Eino session completed, but no assistant text output was captured. Please check the process details or logs.)"
	}

	if args.EinoCallbacks != nil {
		ctx = einoobserve.AttachAgentRunCallbacks(ctx, args.EinoCallbacks, einoobserve.Params{
			Logger:           logger,
			Progress:         progress,
			ConversationID:   conversationID,
			OrchMode:         orchMode,
			OrchestratorName: orchestratorName,
			RunID:            runID,
		})
	}

	drain := newEinoRunEventDrain(einoRunEventDrainConfig{
		Context:                 ctx,
		ConversationID:          conversationID,
		OrchMode:                orchMode,
		OrchestratorName:        orchestratorName,
		Progress:                progress,
		Logger:                  logger,
		BaseMessages:            msgs,
		SnapshotMCPIDs:          snapshotMCPIDs,
		StreamsMainAssistant:    streamsMainAssistant,
		EinoRoleTag:             einoRoleTag,
		MiddlewareConfig:        args.MiddlewareConfig,
		FilesystemMonitorAgent:  args.FilesystemMonitorAgent,
		FilesystemMonitorRecord: args.FilesystemMonitorRecord,
		MCPExecutionBinder:      args.MCPExecutionBinder,
	})
	session := newEinoRunRuntimeSession(einoRunRuntimeSessionConfig{
		Context:        ctx,
		Args:           args,
		Drain:          drain,
		BaseMessages:   msgs,
		EmptyHint:      emptyHint,
		SnapshotMCPIDs: snapshotMCPIDs,
		EinoRoleTag:    einoRoleTag,
	})
	defer session.Close()

	// 仅在退避重试后真正收到数据/完成一步时清零，避免重启后首个无错 ADK 事件误把计数打回 0。
	drain.BindHandlers(session.ConfirmRecovery)

	for {
		// iter.Next 可能长时间阻塞（工具执行、模型推理）；须与 ctx 联动，否则取消/超时无法及时 flush pending。
		ev, ok, iterCtxErr := nextAgentEventWithContext(ctx, session.Iterator())
		if iterCtxErr != nil {
			return session.HandleIteratorContextError(iterCtxErr)
		}
		if !ok {
			// iter 结束并不总是“正常完成”：
			// 当取消/超时发生在 iter.Next() 阻塞期间时，可能直接返回 !ok。
			// 此时必须保留 checkpoint，避免后续恢复时被误判为“无断点”而全量重跑。
			completed, result, err := session.HandleIteratorEnd()
			if result != nil || err != nil {
				return result, err
			}
			if completed {
				break
			}
			continue
		}
		if ev == nil {
			continue
		}
		if ev.Err != nil {
			handled := session.HandleRunError(ev.Err)
			if handled.Result != nil || handled.Err != nil {
				return handled.Result, handled.Err
			}
			if handled.Restarted {
				continue
			}
		}
		drain.ObserveAgent(ev.AgentName)
		if ev.Output == nil || ev.Output.MessageOutput == nil {
			continue
		}
		mv := ev.Output.MessageOutput

		if drain.HandleToolResultStreaming(mv, ev.AgentName) {
			continue
		}

		if handledStream, streamRecvErr := drain.HandleAssistantStream(mv, ev.AgentName); handledStream {
			if streamRecvErr != nil {
				handled := session.HandleStreamError(streamRecvErr, ev.AgentName)
				if handled.Result != nil || handled.Err != nil {
					return handled.Result, handled.Err
				}
				if handled.Restarted {
					continue
				}
			} else {
				session.ConfirmRecovery()
			}
			continue
		}

		msg, gerr := mv.GetMessage()
		if gerr != nil || msg == nil {
			continue
		}
		drain.HandleMaterialized(mv, msg, ev.AgentName)
		session.ConfirmRecovery()
	}

	return session.BuildFinalResult(), nil
}

// modelFacingTraceSnapshot returns only the state that actually reached the model boundary.
// Never fall back to event-stream accumulation here: it can contain pre-reduction tool output
// that the model never received (for example when summarization failed before the first call).
func modelFacingTraceSnapshot(args *einoADKRunLoopArgs) []adk.Message {
	if args != nil && args.ModelFacingTrace != nil {
		if snap := args.ModelFacingTrace.Snapshot(); len(snap) > 0 {
			return snap
		}
	}
	return nil
}

// friendlyEinoExecuteInvokeTail 将 Eino execute 超时/中断/流异常转为简短提示。
// 命令非零退出（ExecuteExitError）已有 exec 对齐的正文，不再追加「执行未正常结束」。
func friendlyEinoExecuteInvokeTail(invokeErr error) string {
	if invokeErr == nil {
		return ""
	}
	var exitErr *ExecuteExitError
	if errors.As(invokeErr, &exitErr) {
		return ""
	}
	if errors.Is(invokeErr, context.DeadlineExceeded) {
		return einoExecuteTimeoutUserHint()
	}
	if errors.Is(invokeErr, context.Canceled) {
		return ""
	}
	if strings.Contains(invokeErr.Error(), "shell inactivity timeout") {
		return ""
	}
	return "[abnormal execution termination] " + invokeErr.Error()
}

// einoToolResultIsError 统一判断 Eino 工具结果是否应标记为错误（与 MCP exec 的 IsError 对齐）。
func einoToolResultIsError(toolName, content string) bool {
	if strings.HasPrefix(content, einomcp.ToolErrorPrefix) {
		return true
	}
	if strings.TrimSpace(toolName) == "execute" && security.IsCommandFailureResult(content) {
		return true
	}
	return false
}

func isMCPBackgroundWaitResult(content string) bool {
	text := strings.ToLower(strings.TrimSpace(content))
	if text == "" {
		return false
	}
	hasExecutionID := strings.Contains(text, "execution_id:") || strings.Contains(text, `"execution_id"`)
	hasRunningStatus := strings.Contains(text, "status: running") || strings.Contains(text, "status: queued") ||
		strings.Contains(text, `"status": "running"`) || strings.Contains(text, `"status":"running"`) ||
		strings.Contains(text, `"status": "queued"`) || strings.Contains(text, `"status":"queued"`)
	hasSoftWaitSignal := strings.Contains(text, "工具已提交到后台执行") ||
		strings.Contains(text, "本次等待已到达") ||
		strings.Contains(text, "wait_timeout:") ||
		strings.Contains(text, "background execution") ||
		strings.Contains(text, "still running") ||
		strings.Contains(text, "仍未完成")
	return hasExecutionID && hasRunningStatus && hasSoftWaitSignal
}

func mcpExecutionIDFromWaitResult(content string) string {
	re := regexp.MustCompile(`(?i)"?execution_id"?\s*[:=]\s*"?([0-9a-f]{8}-[0-9a-f-]{12,})"?`)
	if m := re.FindStringSubmatch(content); len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		lower := strings.ToLower(line)
		if !strings.HasPrefix(lower, "execution_id:") {
			continue
		}
		return strings.Trim(strings.TrimSpace(line[len("execution_id:"):]), `"'`)
	}
	return ""
}

// einoToolResultBody 去掉工具错误前缀，返回展示/持久化正文。
func einoToolResultBody(content string) string {
	if strings.HasPrefix(content, einomcp.ToolErrorPrefix) {
		return strings.TrimPrefix(content, einomcp.ToolErrorPrefix)
	}
	return content
}

// nextAgentEventWithContext 在 ctx 取消时不再无限阻塞于 iter.Next()（工具执行/模型推理期间常见）。
func nextAgentEventWithContext(ctx context.Context, iter *adk.AsyncIterator[*adk.AgentEvent]) (ev *adk.AgentEvent, ok bool, ctxErr error) {
	if iter == nil {
		return nil, false, nil
	}
	type nextRes struct {
		ev *adk.AgentEvent
		ok bool
	}
	ch := make(chan nextRes, 1)
	go func() {
		e, o := iter.Next()
		ch <- nextRes{e, o}
	}()
	select {
	case <-ctx.Done():
		return nil, false, ctx.Err()
	case res := <-ch:
		return res.ev, res.ok, nil
	}
}

// recvSchemaMessageStream 消费 ADK Tool 流式结果；ctx 取消时立即返回，避免 amass 等无输出时永久阻塞。
func recvSchemaMessageStream(ctx context.Context, stream *schema.StreamReader[*schema.Message]) (content, toolCallID, toolName string, recvErr error) {
	msgs, recvErr := recvSchemaToolResultMessages(ctx, stream)
	if len(msgs) == 0 {
		return "", "", "", recvErr
	}
	parts := make([]string, 0, len(msgs))
	for _, msg := range msgs {
		if msg == nil {
			continue
		}
		parts = append(parts, msg.Content)
		if id := strings.TrimSpace(msg.ToolCallID); id != "" {
			toolCallID = id
		}
		if name := strings.TrimSpace(msg.ToolName); name != "" {
			toolName = name
		}
	}
	return strings.Join(parts, ""), toolCallID, toolName, recvErr
}

// recvSchemaToolResultMessages 先收齐 Tool 流，再用 Eino ConcatMessages 合并。
// EventSender 一 call 一条流时走 ConcatMessages；并行结果被摊平进同一条流时按 CallID 分列再合并。
func recvSchemaToolResultMessages(ctx context.Context, stream *schema.StreamReader[*schema.Message]) (msgs []*schema.Message, recvErr error) {
	if stream == nil {
		return nil, nil
	}
	var chunks []*schema.Message
	recvErr = recvEinoSchemaMessageStreamWithContext(ctx, stream, 8, func(chunk *schema.Message) {
		chunks = append(chunks, chunk)
	})
	msgs, concatErr := concatToolResultChunks(chunks)
	if concatErr != nil && recvErr == nil {
		return nil, concatErr
	}
	return msgs, recvErr
}

func buildEinoCheckpointID(orchMode string) string {
	mode := sanitizeEinoPathSegment(strings.TrimSpace(orchMode))
	if mode == "" {
		mode = "default"
	}
	return "runner-" + mode
}

func buildEinoTurnLoopCheckpointID(orchMode string) string {
	mode := sanitizeEinoPathSegment(strings.TrimSpace(orchMode))
	if mode == "" {
		mode = "default"
	}
	return "turn-loop-" + mode
}
