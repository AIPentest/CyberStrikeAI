package handler

import (
	"strings"

	"cyberstrike-ai/internal/project"
	"go.uber.org/zap"
)

// agentSessionContextBlock 注入会话工作目录与项目黑板（用于 system prompt 追加块）。
// 用户输入由 message history 承载；压缩后由 summarization 摘要指令保留关键约束。
func (h *AgentHandler) agentSessionContextBlock(conversationID string) string {
	var parts []string
	if ws := h.buildWorkspaceBlock(conversationID); ws != "" {
		parts = append(parts, ws)
	}
	if bb := h.projectBlackboardBlock(conversationID); bb != "" {
		parts = append(parts, bb)
	}
	return strings.Join(parts, "\n\n")
}

func (h *AgentHandler) buildWorkspaceBlock(conversationID string) string {
	if h == nil || h.config == nil {
		return ""
	}
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" {
		return ""
	}
	projectID := h.conversationProjectID(conversationID)
	rel := project.WorkspaceRootDir(h.config.Agent.WorkspaceRootDir, projectID, conversationID)
	abs, err := project.EnsureWorkspace(rel)
	if err != nil {
		if h.logger != nil {
			h.logger.Warn("failed to create session workspace directory",
				zap.String("conversationId", conversationID),
				zap.String("projectId", projectID),
				zap.String("path", rel),
				zap.Error(err))
		}
		return ""
	}
	return project.BuildWorkspaceBlock(abs) + "\n\n" + project.BuildWorkspaceInventoryBlock(abs)
}

// projectBlackboardBlock 根据对话 ID 构建项目事实索引块（用于注入 system prompt）。
func (h *AgentHandler) projectBlackboardBlock(conversationID string) string {
	if h == nil || h.db == nil || h.config == nil {
		return ""
	}
	if !h.config.Project.Enabled {
		return ""
	}
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" {
		return ""
	}
	projectID, err := h.db.GetConversationProjectID(conversationID)
	if err != nil || projectID == "" {
		return ""
	}
	block, err := project.BuildProjectBlackboardBlock(h.db, projectID, h.config.Project, conversationID)
	if err != nil {
		h.logger.Warn("failed to build project blackboard index", zap.String("conversationId", conversationID), zap.Error(err))
		return ""
	}
	return strings.TrimSpace(block)
}

// conversationProjectID 返回对话绑定的项目 ID；未绑定或查询失败时返回空字符串。
func (h *AgentHandler) conversationProjectID(conversationID string) string {
	if h == nil || h.db == nil {
		return ""
	}
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" {
		return ""
	}
	projectID, err := h.db.GetConversationProjectID(conversationID)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(projectID)
}
