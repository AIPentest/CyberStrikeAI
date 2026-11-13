package mcp

import (
	"context"
	"strings"
)

// ToolRunRegistry 在工具开始/结束时登记当前 executionId，供对话页「仅终止当前工具」与监控页共用取消逻辑。
type ToolRunRegistry interface {
	RegisterRunningTool(conversationID, executionID string)
	UnregisterRunningTool(conversationID, executionID string)
}

// AgentExecuteRunRegistry 登记进行中的 Agent filesystem execute，供「中断并继续」终止 amass 等长命令。
type AgentExecuteRunRegistry interface {
	RegisterActiveAgentExecute(conversationID string, cancel context.CancelFunc)
	UnregisterActiveAgentExecute(conversationID string)
	AbortActiveAgentExecute(conversationID, note string) bool
	TakeAgentExecuteAbortNote(conversationID string) string
}

type toolRunRegistryCtxKey struct{}
type agentExecuteRunRegistryCtxKey struct{}
type mcpConversationIDCtxKey struct{}
type mcpExecutionIDCtxKey struct{}
type mcpProjectIDCtxKey struct{}

// WithToolRunRegistry 将登记器注入 ctx（Agent / 原生 Agent 任务 ctx）。
func WithToolRunRegistry(ctx context.Context, reg ToolRunRegistry) context.Context {
	if ctx == nil || reg == nil {
		return ctx
	}
	return context.WithValue(ctx, toolRunRegistryCtxKey{}, reg)
}

// ToolRunRegistryFromContext 取出登记器（无则 nil）。
func ToolRunRegistryFromContext(ctx context.Context) ToolRunRegistry {
	if ctx == nil {
		return nil
	}
	v, _ := ctx.Value(toolRunRegistryCtxKey{}).(ToolRunRegistry)
	return v
}

// WithAgentExecuteRunRegistry 将 Agent execute 取消登记器注入 ctx。
func WithAgentExecuteRunRegistry(ctx context.Context, reg AgentExecuteRunRegistry) context.Context {
	if ctx == nil || reg == nil {
		return ctx
	}
	return context.WithValue(ctx, agentExecuteRunRegistryCtxKey{}, reg)
}

// AgentExecuteRunRegistryFromContext 取出 Agent execute 登记器（无则 nil）。
func AgentExecuteRunRegistryFromContext(ctx context.Context) AgentExecuteRunRegistry {
	if ctx == nil {
		return nil
	}
	v, _ := ctx.Value(agentExecuteRunRegistryCtxKey{}).(AgentExecuteRunRegistry)
	return v
}

// WithMCPConversationID 将对话 ID 注入 ctx，供 CallTool 内与 executionId 关联。
func WithMCPConversationID(ctx context.Context, conversationID string) context.Context {
	if ctx == nil {
		return nil
	}
	id := strings.TrimSpace(conversationID)
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, mcpConversationIDCtxKey{}, id)
}

// MCPConversationIDFromContext 读取对话 ID。
func MCPConversationIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(mcpConversationIDCtxKey{}).(string)
	return v
}

// WithMCPExecutionID 将当前工具 executionId 注入 ctx，供超长输出落盘文件名对齐。
func WithMCPExecutionID(ctx context.Context, executionID string) context.Context {
	if ctx == nil {
		return nil
	}
	id := strings.TrimSpace(executionID)
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, mcpExecutionIDCtxKey{}, id)
}

// MCPExecutionIDFromContext 读取当前工具 executionId。
func MCPExecutionIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(mcpExecutionIDCtxKey{}).(string)
	return v
}

// WithMCPProjectID 将项目 ID 注入 ctx，供 reduction/trunc 落盘路径与项目隔离对齐。
func WithMCPProjectID(ctx context.Context, projectID string) context.Context {
	if ctx == nil {
		return nil
	}
	id := strings.TrimSpace(projectID)
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, mcpProjectIDCtxKey{}, id)
}

// MCPProjectIDFromContext 读取项目 ID。
func MCPProjectIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(mcpProjectIDCtxKey{}).(string)
	return v
}

func notifyToolRunBegin(ctx context.Context, executionID string) {
	reg := ToolRunRegistryFromContext(ctx)
	if reg == nil {
		return
	}
	conv := MCPConversationIDFromContext(ctx)
	if conv == "" || strings.TrimSpace(executionID) == "" {
		return
	}
	reg.RegisterRunningTool(conv, executionID)
}

func notifyToolRunEnd(ctx context.Context, executionID string) {
	reg := ToolRunRegistryFromContext(ctx)
	if reg == nil {
		return
	}
	conv := MCPConversationIDFromContext(ctx)
	if conv == "" || strings.TrimSpace(executionID) == "" {
		return
	}
	reg.UnregisterRunningTool(conv, executionID)
}
