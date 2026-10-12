package handler

import (
	"net/http"

	"cyberstrike-ai/internal/learning"
	"cyberstrike-ai/internal/security"
	"github.com/gin-gonic/gin"
)

func (h *AgentHandler) initializeLearning() {
	if h.config == nil || !h.config.Learning.Enabled || h.db == nil {
		return
	}
	cfg := h.config.OpenAI // Capture startup model settings, independent of task contexts.
	svc := learning.New(h.db, h.config.Learning, learning.NewModelExtractor(cfg), cfg.APIKey, h.logger)
	if err := svc.Start(); err != nil {
		h.logger.Warn("启动经验学习失败，未启用任务采集")
		return
	}
	h.learning = svc
	h.tasks.SetRunPersistenceHooks(func(task *AgentTask) {
		if err := h.db.BeginLearningRun(task.RunID, task.ConversationID); err != nil {
			h.logger.Warn("登记经验学习运行失败")
		}
	}, func(runID, status string) {
		if err := h.db.FinishLearningRun(runID, status); err != nil {
			h.logger.Warn("登记经验学习终态失败")
		}
	})
}

// ListConversationLearning exposes only the selected source conversation. Drafts
// have no activation endpoint and are never installed into the skills directory.
func (h *AgentHandler) ListConversationLearning(c *gin.Context) {
	id := c.Param("id")
	session, ok := security.CurrentSession(c)
	if !ok || !session.Permissions["chat:read"] || h.db == nil || !h.db.UserCanAccessResource(session.UserID, session.ScopeFor("chat:read"), "conversation", id) {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权访问该资源"})
		return
	}
	pid, err := h.db.GetConversationProjectID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "对话不存在"})
		return
	}
	if pid != "" && (!session.Permissions["project:read"] || !h.db.UserCanAccessResource(session.UserID, session.ScopeFor("project:read"), "project", pid)) {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权访问项目经验"})
		return
	}
	jobs, err := h.db.ListLearningJobs(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取经验草稿失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"enabled": h.learning != nil, "mode": "draft_only", "jobs": jobs})
}
