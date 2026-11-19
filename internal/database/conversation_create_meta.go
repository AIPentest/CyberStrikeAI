package database

// ConversationCreateMeta describes how a conversation was created (for audit hooks).
type ConversationCreateMeta struct {
	Source                      string
	WebShellConnectionID        string
	ProjectID                   string
	RoleName                    string
	AgentMode                   string
	ClientIP                    string
	SessionHint                 string
	HumanApprovalTimeoutSeconds *int
}

// ConversationCreateHook is invoked after a conversation row is inserted.
type ConversationCreateHook func(conv *Conversation, meta ConversationCreateMeta)

var conversationCreateHook ConversationCreateHook

// SetConversationCreateHook registers a global hook (e.g. platform audit).
func SetConversationCreateHook(h ConversationCreateHook) {
	conversationCreateHook = h
}

func notifyConversationCreated(conv *Conversation, meta ConversationCreateMeta) {
	if conversationCreateHook == nil || conv == nil {
		return
	}
	if meta.Source == "" {
		meta.Source = "unknown"
	}
	conversationCreateHook(conv, meta)
}
