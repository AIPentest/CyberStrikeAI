package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/security"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// GetTokenUsageStats returns model token usage aggregates for dashboard views.
func (h *ConversationHandler) GetTokenUsageStats(c *gin.Context) {
	filter := tokenUsageFilterFromQuery(c)
	if session, ok := security.CurrentSession(c); ok {
		filter.Access = database.RBACListAccess{UserID: session.UserID, Scope: session.Scope}
	}
	stats, err := h.db.GetModelTokenUsageStats(filter)
	if err != nil {
		h.logger.Error("failed to get token usage stats", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, stats)
}

// GetConversationTokenUsageStats returns token usage scoped to one conversation.
func (h *ConversationHandler) GetConversationTokenUsageStats(c *gin.Context) {
	filter := tokenUsageFilterFromQuery(c)
	filter.ConversationID = strings.TrimSpace(c.Param("id"))
	if session, ok := security.CurrentSession(c); ok {
		filter.Access = database.RBACListAccess{UserID: session.UserID, Scope: session.Scope}
	}
	stats, err := h.db.GetModelTokenUsageStats(filter)
	if err != nil {
		h.logger.Error("failed to get conversation token usage stats", zap.Error(err), zap.String("conversationId", filter.ConversationID))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, stats)
}

func tokenUsageFilterFromQuery(c *gin.Context) database.ModelTokenUsageFilter {
	days, _ := strconv.Atoi(strings.TrimSpace(c.DefaultQuery("days", "7")))
	if days <= 0 {
		days = 7
	}
	if days > 365 {
		days = 365
	}
	limit, _ := strconv.Atoi(strings.TrimSpace(c.DefaultQuery("limit", "10")))
	if limit <= 0 {
		limit = 10
	}
	if limit > 500 {
		limit = 500
	}
	filter := database.ModelTokenUsageFilter{
		ConversationID: strings.TrimSpace(c.Query("conversation_id")),
		ProjectID:      strings.TrimSpace(c.Query("project_id")),
		Days:           days,
		Limit:          limit,
	}
	if since := parseTokenUsageQueryTime(c.Query("since")); !since.IsZero() {
		filter.Since = since
	} else if days > 0 {
		now := time.Now()
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -(days - 1))
		filter.Since = start
	}
	if until := parseTokenUsageQueryTime(c.Query("until")); !until.IsZero() {
		filter.Until = until
	}
	return filter
}

func parseTokenUsageQueryTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t
		}
	}
	return time.Time{}
}
