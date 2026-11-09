package multiagent

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/einomcp"
	"cyberstrike-ai/internal/mcp"

	"github.com/cloudwego/eino/adk"
)

type einoToolResultProgressEmitter struct {
	conversationID   string
	orchestratorName string
	progress         func(eventType, message string, data interface{})
	einoRoleTag      func(agent string) string

	pending          *einoPendingToolCalls
	executeStdoutDup *einoExecuteStdoutSuppressor
	runMessages      *einoRunMessageAccumulator

	filesystemMonitorAgent  *agent.Agent
	filesystemMonitorRecord einomcp.ExecutionRecorder
	mcpExecutionBinder      *MCPExecutionBinder

	sent sync.Map
}

type einoToolResultProgressEmitterConfig struct {
	ConversationID   string
	OrchestratorName string
	Progress         func(eventType, message string, data interface{})
	EinoRoleTag      func(agent string) string

	Pending          *einoPendingToolCalls
	ExecuteStdoutDup *einoExecuteStdoutSuppressor
	RunMessages      *einoRunMessageAccumulator

	FilesystemMonitorAgent  *agent.Agent
	FilesystemMonitorRecord einomcp.ExecutionRecorder
	MCPExecutionBinder      *MCPExecutionBinder
}

func newEinoToolResultProgressEmitter(cfg einoToolResultProgressEmitterConfig) *einoToolResultProgressEmitter {
	if cfg.EinoRoleTag == nil {
		cfg.EinoRoleTag = func(string) string { return "" }
	}
	return &einoToolResultProgressEmitter{
		conversationID:          cfg.ConversationID,
		orchestratorName:        cfg.OrchestratorName,
		progress:                cfg.Progress,
		einoRoleTag:             cfg.EinoRoleTag,
		pending:                 cfg.Pending,
		executeStdoutDup:        cfg.ExecuteStdoutDup,
		runMessages:             cfg.RunMessages,
		filesystemMonitorAgent:  cfg.FilesystemMonitorAgent,
		filesystemMonitorRecord: cfg.FilesystemMonitorRecord,
		mcpExecutionBinder:      cfg.MCPExecutionBinder,
	}
}

func (e *einoToolResultProgressEmitter) Emit(ctx context.Context, toolName, content, toolCallID string, isErr bool, agentName string) bool {
	if e == nil {
		return false
	}
	if strings.HasPrefix(strings.TrimSpace(content), modelOutputRejectedResultPrefix) {
		return false
	}
	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		toolName = "unknown"
	}
	preview := content
	if len(preview) > 200 {
		preview = preview[:200] + "..."
	}
	backgroundRunning := isErr && isMCPBackgroundWaitResult(content)
	displayIsErr := isErr && !backgroundRunning
	data := map[string]interface{}{
		"toolName":       toolName,
		"success":        !displayIsErr,
		"isError":        displayIsErr,
		"result":         content,
		"resultPreview":  preview,
		"agentFacing":    true,
		"conversationId": e.conversationID,
		"einoAgent":      agentName,
		"einoRole":       e.einoRoleTag(agentName),
		"source":         "eino",
	}
	if backgroundRunning {
		data["status"] = "background_running"
		data["modelFacingIsError"] = isErr
		if execID := mcpExecutionIDFromWaitResult(content); execID != "" {
			data["executionId"] = execID
		}
	}
	tid := strings.TrimSpace(toolCallID)
	if tid == "" {
		tid = e.inferToolCallID(agentName)
	}
	if tid != "" {
		if e.pending != nil {
			e.pending.RemoveByID(tid)
		}
		if _, loaded := e.sent.LoadOrStore(tid, struct{}{}); loaded {
			return false
		}
		data["toolCallId"] = tid
		toolCallID = tid
	}
	if e.executeStdoutDup != nil {
		e.executeStdoutDup.Record(toolName, content, displayIsErr)
	}
	if args := e.toolCallArguments(toolCallID, toolName); len(args) > 0 {
		data["argumentsObj"] = args
		data["arguments"] = mustMarshalToolArguments(args)
	}
	if execID := recordEinoADKFilesystemToolMonitor(ctx, e.filesystemMonitorAgent, e.filesystemMonitorRecord, e.mcpExecutionBinder, toolName, toolCallID, e.messages(), content, displayIsErr); execID != "" {
		if stored := e.filesystemMonitorAgent.MCPExecutionResultText(execID); strings.TrimSpace(stored) != "" {
			content = stored
			if len(content) > 200 {
				preview = content[:200] + "..."
			} else {
				preview = content
			}
			data["result"] = content
			data["resultPreview"] = preview
		}
	}
	if e.filesystemMonitorAgent != nil && e.mcpExecutionBinder != nil {
		if execID := e.mcpExecutionBinder.ExecutionID(toolCallID); execID != "" {
			// Use the execution record rather than parsing the rendered result:
			// reduction can rewrite text without changing the safety decision.
			if e.filesystemMonitorAgent.MCPExecutionStatus(execID) == mcp.ToolExecutionStatusBlocked {
				data["blocked"] = true
				data["status"] = mcp.ToolExecutionStatusBlocked
				data["success"] = false
				data["isError"] = true
				data["executionId"] = execID
			}
			e.filesystemMonitorAgent.UpdateMCPExecutionDisplayResult(execID, content)
		}
	}
	if e.progress != nil {
		e.progress("tool_result", fmt.Sprintf("Tool result (%s)", toolName), data)
	}
	return true
}

func (e *einoToolResultProgressEmitter) inferToolCallID(agentName string) string {
	if e.pending == nil {
		return ""
	}
	if inferred, ok := e.pending.PopNextForAgent(agentName); ok {
		return inferred.ToolCallID
	}
	if inferred, ok := e.pending.PopNextForAgent(e.orchestratorName); ok {
		return inferred.ToolCallID
	}
	if inferred, ok := e.pending.PopNextForAgent(""); ok {
		return inferred.ToolCallID
	}
	if inferred, ok := e.pending.PopAny(); ok {
		return inferred.ToolCallID
	}
	return ""
}

func (e *einoToolResultProgressEmitter) messages() []adk.Message {
	if e == nil || e.runMessages == nil {
		return nil
	}
	return e.runMessages.Messages()
}

func (e *einoToolResultProgressEmitter) toolCallArguments(toolCallID, toolName string) map[string]interface{} {
	if e == nil {
		return nil
	}
	if e.mcpExecutionBinder != nil {
		if args := e.mcpExecutionBinder.Arguments(toolCallID); len(args) > 0 {
			return args
		}
	}
	return toolCallArgsFromAccumulated(e.messages(), toolCallID, toolName)
}
