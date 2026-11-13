package multiagent

type RunResult struct {
	RuntimeStatus           string // immutable runtime outcome; delivery finalization may update Status separately
	RuntimeCompletionReason string
	Response                string
	MCPExecutionIDs         []string
	LastAgentTraceInput     string // 已序列化的消息带（JSON）：原生循环或 Agent 均写入，供续跑/攻击链等恢复上下文
	LastAgentTraceOutput    string // 本轮助手侧对外展示文本（摘要或最终回复）
	Finalized               bool
	Status                  string
	CompletionReason        string
	EvidenceVerified        bool
	EvidenceRefs            []string
	PendingExecutionIDs     []string
	MissingChecks           []string
}
