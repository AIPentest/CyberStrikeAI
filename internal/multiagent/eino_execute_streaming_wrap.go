package multiagent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"cyberstrike-ai/internal/einomcp"
	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/security"

	"github.com/cloudwego/eino/adk/filesystem"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

// prependPythonUnbufferedEnv 为 /bin/sh -c 注入 PYTHONUNBUFFERED=1。
// eino-ext local 对流式 stdout 使用 bufio 按「行」推送；python3 写管道时默认块缓冲，print 长期留在用户态缓冲，
// 管道里收不到换行，表现为长时间无输出直至超时或退出。若命令里已出现 PYTHONUNBUFFERED 则不再覆盖。
func prependPythonUnbufferedEnv(shellCommand string) string {
	if strings.TrimSpace(shellCommand) == "" {
		return shellCommand
	}
	if strings.Contains(strings.ToUpper(shellCommand), "PYTHONUNBUFFERED") {
		return shellCommand
	}
	return "export PYTHONUNBUFFERED=1\n" + shellCommand
}

// einoExecuteTimeoutUserHint 与写入 ADK 工具消息（模型可见）及 SSE tool_result 尾标一致。
func einoExecuteTimeoutUserHint() string {
	return "Timed out · terminated"
}

// einoExecuteRecvErrIsToolTimeout 判断 Recv 错误是否由 agent.tool_timeout_minutes 触发。
// WithTimeout 到期后 local 侧常报 canceled / exit -1，但 execCtx.Err() 仍为 DeadlineExceeded。
func einoExecuteRecvErrIsToolTimeout(rerr error, tctx context.Context) bool {
	if tctx != nil && errors.Is(tctx.Err(), context.DeadlineExceeded) {
		return true
	}
	return errors.Is(rerr, context.DeadlineExceeded)
}

// einoStreamingShellWrap 包装 Eino filesystem 使用的 StreamingShell（cloudwego eino-ext local.Local）。
// 官方 execute 工具默认走 ExecuteStreaming 且不设 RunInBackendGround；末尾带 & 时子进程仍与管道相连，
// streamStdout 按行读取会在无换行输出时长时间阻塞（与 MCP 工具 exec 的独立实现不同）。
// 对「完全后台」命令自动开启 RunInBackendGround，与 local.runCmdInBackground 行为对齐。
//
// 使用 Pipe 将内层流转发给调用方：在 inner EOF 后、关闭 Pipe 前同步调用 ToolInvokeNotify.Fire，
// run loop 收到 Fire 后立即推送 tool_result（toolResultSent 去重），避免 ADK Tool 事件迟到时 UI 卡在「执行中」。
//
// 若 inner 在校验阶段直接返回 error（未建立 reader），不会进入下方 goroutine，也必须 Fire；
// 否则 pending tool_call 要等整轮 run 结束才被 force-close，与已展示的助手/工具软错误文案不同步。
type einoStreamingShellWrap struct {
	inner         filesystem.StreamingShell
	invokeNotify  *einomcp.ToolInvokeNotifyHolder
	einoAgentName string
	// outputChunk 可选；非 nil 时在收到内层 ExecuteResponse 片段时推送，与 MCP 工具的 tool_result_delta 一致（需有效 toolCallId）。
	outputChunk func(toolName, toolCallID, chunk string)
	// toolTimeoutMinutes 与 agent.tool_timeout_minutes 对齐；>0 时对单次 execute 套用 context 超时（与 MCP 工具经 executeToolViaMCP 行为一致）。0 表示仅依赖上层 ctx（如整任务 10h 上限）。
	toolTimeoutMinutes int
	// toolWaitTimeoutSeconds 与 agent.tool_wait_timeout_seconds 对齐；>0 时本轮等待到期后返回 execution_id，shell 继续后台运行。
	toolWaitTimeoutSeconds int
	// shellNoOutputTimeoutSec：无任何输出时的空闲秒数；0=关闭。
	shellNoOutputTimeoutSec int
	// beginMonitor 在 execute 开始时写入 running 状态；finishMonitor 在流结束后更新为 completed/failed。
	beginMonitor            func(toolCallID, command string) string
	appendPartialMonitor    func(executionID, toolCallID, chunk string)
	registerCancelMonitor   func(executionID string, cancel context.CancelFunc)
	unregisterCancelMonitor func(executionID string)
	finishMonitor           func(executionID, toolCallID, command, stdout string, success bool, invokeErr error)
}

func (w *einoStreamingShellWrap) ExecuteStreaming(ctx context.Context, input *filesystem.ExecuteRequest) (*schema.StreamReader[*filesystem.ExecuteResponse], error) {
	if w.inner == nil {
		return nil, fmt.Errorf("einoStreamingShellWrap: inner shell is nil")
	}
	if input == nil {
		return w.inner.ExecuteStreaming(ctx, nil)
	}
	req := *input
	userCmd := strings.TrimSpace(req.Command)
	tid := strings.TrimSpace(compose.GetToolCallID(ctx))
	agentTag := strings.TrimSpace(w.einoAgentName)
	if security.IsBackgroundShellCommand(req.Command) && !req.RunInBackendGround {
		req.RunInBackendGround = true
	}
	req.Command = prependPythonUnbufferedEnv(req.Command)
	convID := mcp.MCPConversationIDFromContext(ctx)
	execReg := mcp.EinoExecuteRunRegistryFromContext(ctx)

	var monitorExecID string
	if w.beginMonitor != nil {
		monitorExecID = w.beginMonitor(tid, userCmd)
	}
	if monitorExecID != "" && convID != "" {
		if toolReg := mcp.ToolRunRegistryFromContext(ctx); toolReg != nil {
			toolReg.RegisterRunningTool(convID, monitorExecID)
		}
	}
	toolRunReg := mcp.ToolRunRegistryFromContext(ctx)

	execCtx, execCancel := context.WithCancel(ctx)
	var timeoutCancel context.CancelFunc
	if w.toolTimeoutMinutes > 0 {
		execCtx, timeoutCancel = context.WithTimeout(execCtx, time.Duration(w.toolTimeoutMinutes)*time.Minute)
	}
	if monitorExecID != "" && w.registerCancelMonitor != nil {
		w.registerCancelMonitor(monitorExecID, execCancel)
	}
	if execReg != nil && convID != "" {
		execReg.RegisterActiveEinoExecute(convID, execCancel)
	}

	sr, err := w.inner.ExecuteStreaming(execCtx, &req)
	if err != nil {
		if timeoutCancel != nil {
			timeoutCancel()
		}
		if execCancel != nil {
			execCancel()
		}
		if monitorExecID != "" && w.unregisterCancelMonitor != nil {
			w.unregisterCancelMonitor(monitorExecID)
		}
		if einoExecuteRecvErrIsToolTimeout(err, execCtx) {
			hint := "\n\n" + einoExecuteTimeoutUserHint() + "\n"
			if w.finishMonitor != nil {
				w.finishMonitor(monitorExecID, tid, userCmd, hint, false, context.DeadlineExceeded)
			}
			if w.invokeNotify != nil && tid != "" {
				w.invokeNotify.Fire(tid, "execute", agentTag, false, hint, context.DeadlineExceeded)
			}
			return schema.StreamReaderFromArray([]*filesystem.ExecuteResponse{{Output: hint}}), nil
		}
		if w.finishMonitor != nil {
			w.finishMonitor(monitorExecID, tid, userCmd, "", false, err)
		}
		if w.invokeNotify != nil && tid != "" {
			w.invokeNotify.Fire(tid, "execute", agentTag, false, "", err)
		}
		return nil, err
	}
	if sr == nil {
		if timeoutCancel != nil {
			timeoutCancel()
		}
		if execCancel != nil {
			execCancel()
		}
		return sr, nil
	}

	outR, outW := schema.Pipe[*filesystem.ExecuteResponse](32)

	go func(inner *schema.StreamReader[*filesystem.ExecuteResponse], command string, cancel context.CancelFunc, timeoutCleanup context.CancelFunc, tctx context.Context, conversationID string, reg mcp.EinoExecuteRunRegistry, toolReg mcp.ToolRunRegistry, execID string, toolCallID string, noOutputSec int, waitTimeoutSec int) {
		var innerCloseOnce sync.Once
		closeInner := func() {
			innerCloseOnce.Do(func() { inner.Close() })
		}
		defer closeInner()
		if timeoutCleanup != nil {
			defer timeoutCleanup()
		}
		if cancel != nil {
			defer cancel()
		}
		if reg != nil && conversationID != "" {
			defer reg.UnregisterActiveEinoExecute(conversationID)
		}
		if toolReg != nil && conversationID != "" && execID != "" {
			defer toolReg.UnregisterRunningTool(conversationID, execID)
		}
		if w.unregisterCancelMonitor != nil && execID != "" {
			defer w.unregisterCancelMonitor(execID)
		}

		// ctx 取消时关闭内层流，避免 amass 等长时间无换行输出时 Recv 永久阻塞。
		stopWatch := make(chan struct{})
		go func() {
			select {
			case <-tctx.Done():
				closeInner()
			case <-stopWatch:
			}
		}()
		defer close(stopWatch)

		var sb strings.Builder
		success := true
		var invokeErr error
		exitCode := 0
		hasExitCode := false
		softReturned := false
		var outCloseOnce sync.Once
		closeOut := func() {
			outCloseOnce.Do(func() { outW.Close() })
		}
		defer closeOut()
		sendOut := func(resp *filesystem.ExecuteResponse, err error) bool {
			if softReturned {
				return false
			}
			return outW.Send(resp, err)
		}

		idleWatch := security.NewShellInactivityWatch(noOutputSec)
		if idleWatch != nil {
			defer idleWatch.Stop()
		}
		var waitTimeoutCh <-chan time.Time
		var waitTimer *time.Timer
		if waitTimeoutSec > 0 {
			waitTimer = time.NewTimer(time.Duration(waitTimeoutSec) * time.Second)
			waitTimeoutCh = waitTimer.C
			defer waitTimer.Stop()
		}

		type execRecvMsg struct {
			resp *filesystem.ExecuteResponse
			err  error
		}
		recvCh := make(chan execRecvMsg, 1)
		go func() {
			for {
				resp, rerr := inner.Recv()
				recvCh <- execRecvMsg{resp: resp, err: rerr}
				if rerr != nil {
					return
				}
			}
		}()

		fireInactivityTimeout := func() {
			success = false
			invokeErr = fmt.Errorf("shell inactivity timeout (%ds)", idleWatch.Sec)
			msg := security.ShellNoOutputTimeoutMessage(idleWatch.Sec)
			_ = sendOut(&filesystem.ExecuteResponse{Output: msg}, nil)
			sb.WriteString(msg)
			if w.appendPartialMonitor != nil && execID != "" {
				w.appendPartialMonitor(execID, toolCallID, msg)
			}
			if w.outputChunk != nil && toolCallID != "" {
				w.outputChunk("execute", toolCallID, msg)
			}
			if cancel != nil {
				cancel()
			}
			closeInner()
		}

	recvLoop:
		for {
			var idleCh <-chan struct{}
			if idleWatch != nil {
				idleCh = idleWatch.Expired
			}
			select {
			case <-idleCh:
				fireInactivityTimeout()
				break recvLoop
			case <-waitTimeoutCh:
				if execID != "" && !softReturned {
					msg := einoExecuteSoftWaitTimeoutResult(execID, waitTimeoutSec)
					_ = outW.Send(&filesystem.ExecuteResponse{Output: msg}, nil)
					softReturned = true
					closeOut()
				}
				waitTimeoutCh = nil
			case msg := <-recvCh:
				rerr := msg.err
				resp := msg.resp
				if errors.Is(rerr, io.EOF) {
					break recvLoop
				}
				if rerr != nil {
					success = false
					invokeErr = rerr
					if einoExecuteRecvErrIsToolTimeout(rerr, tctx) {
						invokeErr = context.DeadlineExceeded
						break recvLoop
					}
					if errors.Is(rerr, context.Canceled) || (tctx != nil && errors.Is(tctx.Err(), context.Canceled)) {
						invokeErr = context.Canceled
						break recvLoop
					}
					_ = sendOut(nil, rerr)
					break recvLoop
				}
				if resp != nil {
					if resp.ExitCode != nil {
						hasExitCode = true
						exitCode = *resp.ExitCode
						continue
					}
					var appended string
					if resp.Output != "" {
						if security.IsLegacyShellExitNoise(resp.Output) {
							continue
						}
						if idleWatch != nil {
							idleWatch.Bump()
						}
						sb.WriteString(resp.Output)
						appended = resp.Output
						if w.appendPartialMonitor != nil && execID != "" {
							w.appendPartialMonitor(execID, toolCallID, appended)
						}
					}
					if w.outputChunk != nil && strings.TrimSpace(appended) != "" {
						w.outputChunk("execute", toolCallID, appended)
					}
					if sendOut(resp, nil) {
						success = false
						invokeErr = fmt.Errorf("execute stream closed by consumer")
						break recvLoop
					}
				}
			}
		}

		if success && hasExitCode && exitCode != 0 {
			success = false
			invokeErr = &ExecuteExitError{Code: exitCode}
		}
		// WithTimeout 触发后，子进程常被信号结束，local 侧多报 exit -1 / canceled，错误链里不一定带 DeadlineExceeded。
		// 用执行所用 ctx 归一化，便于 UI 展示「超时」而非含糊的 -1。
		if tctx != nil && errors.Is(tctx.Err(), context.DeadlineExceeded) {
			success = false
			invokeErr = context.DeadlineExceeded
		}
		// 用户「中断并继续」终止 execute：合并说明进工具结果（与 MCP CancelToolExecutionWithNote 一致）。
		partialStreamed := sb.String()
		var abortNote string
		if reg != nil && conversationID != "" && (invokeErr != nil || errors.Is(tctx.Err(), context.Canceled)) {
			if note := reg.TakeEinoExecuteAbortNote(conversationID); note != "" {
				abortNote = note
				merged := mcp.MergePartialToolOutputAndAbortNote(partialStreamed, note)
				sb.Reset()
				sb.WriteString(merged)
				if invokeErr == nil {
					success = false
					invokeErr = context.Canceled
				}
			}
		}
		// ADK 从本 Pipe 拼出 tool 消息正文；仅 Notify 尾标不会进入模型上下文。超时句写入流，与 UI 一致。
		if invokeErr != nil && errors.Is(invokeErr, context.DeadlineExceeded) {
			hint := "\n\n" + einoExecuteTimeoutUserHint() + "\n"
			_ = sendOut(&filesystem.ExecuteResponse{Output: hint}, nil)
			if w.appendPartialMonitor != nil && execID != "" {
				w.appendPartialMonitor(execID, toolCallID, hint)
			}
			if w.outputChunk != nil && tid != "" {
				w.outputChunk("execute", tid, hint)
			}
			sb.WriteString(hint)
		}
		// 中断时循环内已逐行写入 stdout；此处只追加 USER INTERRUPT NOTE，避免整段输出重复。
		if invokeErr != nil && errors.Is(invokeErr, context.Canceled) && abortNote != "" {
			if partialStreamed != "" {
				_ = sendOut(&filesystem.ExecuteResponse{Output: "\n\n" + mcp.AbortNoteBannerForModel + "\n" + abortNote}, nil)
			} else if text := strings.TrimSpace(sb.String()); text != "" {
				_ = sendOut(&filesystem.ExecuteResponse{Output: text + "\n"}, nil)
			}
		}
		rawOutput := sb.String()
		fireBody := rawOutput
		if !success && hasExitCode && exitCode != 0 {
			statusLine := security.ExecuteFailureStatusLine(exitCode)
			if !strings.Contains(rawOutput, "Command execution failed:") {
				_ = sendOut(&filesystem.ExecuteResponse{Output: statusLine}, nil)
				if w.appendPartialMonitor != nil && execID != "" {
					w.appendPartialMonitor(execID, toolCallID, statusLine)
				}
				sb.WriteString(statusLine)
			}
			fireBody = einomcp.ToolErrorPrefix + security.FormatCommandFailureResult(exitCode, rawOutput)
		}
		if w.finishMonitor != nil {
			w.finishMonitor(execID, toolCallID, command, sb.String(), success, invokeErr)
		}
		if w.invokeNotify != nil {
			if !softReturned {
				w.invokeNotify.Fire(toolCallID, "execute", agentTag, success, fireBody, invokeErr)
			}
		}
	}(sr, userCmd, execCancel, timeoutCancel, execCtx, convID, execReg, toolRunReg, monitorExecID, tid, w.shellNoOutputTimeoutSec, w.toolWaitTimeoutSeconds)

	return outR, nil
}

func einoExecuteSoftWaitTimeoutResult(executionID string, waitTimeoutSec int) string {
	waitText := "configured wait timeout"
	if waitTimeoutSec > 0 {
		waitText = fmt.Sprintf("%ds", waitTimeoutSec)
	}
	return fmt.Sprintf(`The tool was submitted for background execution and is still running.

execution_id: %s
status: running
wait_timeout: %s

You can keep reasoning or switch to another tool, call get_tool_execution / wait_tool_execution to read partial_output and keep waiting, or call cancel_tool_execution to cancel.`, executionID, waitText)
}
