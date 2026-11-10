package audit

// Entry describes one platform audit record (not chat/tool execution bodies).
type Entry struct {
	Level        string
	Category     string
	Action       string
	Result       string // success | failure
	Actor        string
	SessionHint  string
	ResourceType string
	ResourceID   string
	Message      string
	Detail       map[string]interface{}
	ClientIP     string // optional when c is nil (robot, batch, DB hook)
}
