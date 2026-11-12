package audit

import (
	"strings"

	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/security"

	"github.com/gin-gonic/gin"
)

// RegisterConversationCreateHook records platform audit rows for every new conversation.
func RegisterConversationCreateHook(s *Service) {
	if s == nil {
		return
	}
	database.SetConversationCreateHook(func(conv *database.Conversation, meta database.ConversationCreateMeta) {
		detail := map[string]interface{}{
			"title":  conv.Title,
			"source": meta.Source,
		}
		if meta.WebShellConnectionID != "" {
			detail["webshell_connection_id"] = meta.WebShellConnectionID
		}
		s.Record(nil, Entry{
			Category:     "conversation",
			Action:       "create",
			Result:       "success",
			Message:      "Create conversation",
			ResourceType: "conversation",
			ResourceID:   conv.ID,
			Detail:       detail,
			ClientIP:     meta.ClientIP,
			SessionHint:  meta.SessionHint,
		})
	})
}

// ConversationCreateMeta builds audit metadata for conversation creation.
func ConversationCreateMeta(source string) database.ConversationCreateMeta {
	return database.ConversationCreateMeta{Source: strings.TrimSpace(source)}
}

// ConversationCreateMetaFromGin includes client IP and session hint when available.
func ConversationCreateMetaFromGin(c *gin.Context, source string) database.ConversationCreateMeta {
	m := ConversationCreateMeta(source)
	if c == nil {
		return m
	}
	m.ClientIP = c.ClientIP()
	if token := c.GetString(security.ContextAuthTokenKey); token != "" {
		m.SessionHint = sessionHint(token)
	}
	return m
}
