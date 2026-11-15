package agentfinalizer

import (
	"strings"

	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/multiagent"
)

const (
	StatusCompleted    = "completed"
	StatusInProgress   = "in_progress"
	StatusBlocked      = "blocked"
	StatusFailed       = "failed"
	StatusCancelled    = "cancelled"
	StatusAwaitingHITL = "awaiting_hitl"

	ReasonVerified        = "verified"
	ReasonPendingTools    = "pending_tool_executions"
	ReasonEmptyResponse   = "empty_response"
	ReasonAwaitingHITL    = "awaiting_hitl"
	ReasonFailed          = "failed"
	ReasonCancelled       = "cancelled"
	ReasonMissingEvidence = "missing_execution_evidence"
)

// Decision is the single contract that may promote an agent run to a final
// user-facing answer. Natural-language assistant text is only a candidate until
// this object says Finalizable.
type Decision struct {
	Status               string   `json:"status"`
	Finalizable          bool     `json:"finalizable"`
	Finalized            bool     `json:"finalized"`
	CompletionReason     string   `json:"completionReason"`
	FinalText            string   `json:"finalText,omitempty"`
	EvidenceVerified     bool     `json:"evidenceVerified"`
	EvidenceRefs         []string `json:"evidenceRefs,omitempty"`
	PendingExecutionIDs  []string `json:"pendingExecutionIds,omitempty"`
	PendingToolRuns      []string `json:"pendingToolRuns,omitempty"`
	MissingChecks        []string `json:"missingChecks,omitempty"`
	AgentMode            string   `json:"agentMode,omitempty"`
	ConversationID       string   `json:"conversationId,omitempty"`
	AssistantMessageID   string   `json:"messageId,omitempty"`
	CandidateResponseLen int      `json:"candidateResponseLen,omitempty"`
}

type Input struct {
	Response                 string
	MCPExecutionIDs          []string
	ConversationID           string
	AssistantMessageID       string
	AgentMode                string
	Status                   string
	CompletionReason         string
	AwaitingHITL             bool
	RequireExecutionEvidence bool
}

func FromRunResult(db *database.DB, result *multiagent.RunResult, in Input) Decision {
	if result != nil {
		if in.Status == "" && result.RuntimeStatus != "" {
			in.Status = result.RuntimeStatus
		}
		if in.CompletionReason == "" && result.RuntimeCompletionReason != "" {
			in.CompletionReason = result.RuntimeCompletionReason
		}
		if strings.TrimSpace(in.Response) == "" {
			in.Response = result.Response
		}
		if len(in.MCPExecutionIDs) == 0 {
			in.MCPExecutionIDs = result.MCPExecutionIDs
		}
	}
	d := Decide(db, in)
	if result != nil {
		result.Finalized = d.Finalized
		result.Status = d.Status
		result.CompletionReason = d.CompletionReason
		result.EvidenceVerified = d.EvidenceVerified
		result.EvidenceRefs = append([]string(nil), d.EvidenceRefs...)
		result.PendingExecutionIDs = append([]string(nil), d.PendingExecutionIDs...)
		result.MissingChecks = append([]string(nil), d.MissingChecks...)
	}
	return d
}

func Decide(db *database.DB, in Input) Decision {
	text := strings.TrimSpace(in.Response)
	status := strings.TrimSpace(in.Status)
	if status == "" {
		status = StatusCompleted
	}
	reason := strings.TrimSpace(in.CompletionReason)
	if reason == "" {
		reason = ReasonVerified
	}
	d := Decision{
		Status:               status,
		CompletionReason:     reason,
		FinalText:            text,
		EvidenceVerified:     true,
		EvidenceRefs:         evidenceRefs(in.MCPExecutionIDs),
		AgentMode:            strings.TrimSpace(in.AgentMode),
		ConversationID:       strings.TrimSpace(in.ConversationID),
		AssistantMessageID:   strings.TrimSpace(in.AssistantMessageID),
		CandidateResponseLen: len([]rune(text)),
	}

	if in.AwaitingHITL {
		d.Status = StatusAwaitingHITL
		d.CompletionReason = ReasonAwaitingHITL
		d.EvidenceVerified = false
		d.MissingChecks = append(d.MissingChecks, "workflow is awaiting HITL approval")
		return d
	}
	if isEmptyCandidate(text) {
		d.Status = StatusBlocked
		d.CompletionReason = ReasonEmptyResponse
		d.EvidenceVerified = false
		d.MissingChecks = append(d.MissingChecks, "assistant final text is empty or only an empty-response placeholder")
		return d
	}
	switch status {
	case StatusInProgress, StatusBlocked, StatusFailed, StatusCancelled, StatusAwaitingHITL:
		d.Status = status
		d.EvidenceVerified = false
		if d.CompletionReason == ReasonVerified {
			d.CompletionReason = status
		}
		d.MissingChecks = append(d.MissingChecks, "agent run status is "+status)
		return d
	}

	pending := pendingExecutions(db, in.MCPExecutionIDs)
	if len(pending) > 0 {
		d.Status = StatusInProgress
		d.CompletionReason = ReasonPendingTools
		d.EvidenceVerified = false
		d.PendingExecutionIDs = pending
		d.PendingToolRuns = append([]string(nil), pending...)
		d.MissingChecks = append(d.MissingChecks, "tool execution still queued or running")
		return d
	}

	if in.RequireExecutionEvidence && !hasCompletedEvidence(db, in.MCPExecutionIDs) {
		d.Status = StatusBlocked
		d.CompletionReason = ReasonMissingEvidence
		d.EvidenceVerified = false
		d.MissingChecks = append(d.MissingChecks, "execution evidence is required but no completed tool execution was recorded")
		return d
	}

	d.Finalizable = true
	d.Finalized = true
	d.Status = StatusCompleted
	if d.CompletionReason == "" {
		d.CompletionReason = ReasonVerified
	}
	return d
}

func ResponsePayload(d Decision, extra map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{
		"finalized":           d.Finalized,
		"finalizable":         d.Finalizable,
		"status":              d.Status,
		"completionReason":    d.CompletionReason,
		"evidenceVerified":    d.EvidenceVerified,
		"evidenceRefs":        d.EvidenceRefs,
		"pendingExecutionIds": d.PendingExecutionIDs,
		"pendingToolRuns":     d.PendingToolRuns,
		"missingChecks":       d.MissingChecks,
	}
	if d.ConversationID != "" {
		out["conversationId"] = d.ConversationID
	}
	if d.AssistantMessageID != "" {
		out["messageId"] = d.AssistantMessageID
	}
	if d.AgentMode != "" {
		out["agentMode"] = d.AgentMode
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

func isEmptyCandidate(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return true
	}
	return strings.Contains(s, "no assistant text was captured") ||
		strings.Contains(s, "未捕获到助手文本输出")
}

func evidenceRefs(ids []string) []string {
	out := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, "mcp_execution:"+id)
	}
	return out
}

func pendingExecutions(db *database.DB, ids []string) []string {
	if db == nil || len(ids) == 0 {
		return nil
	}
	out := make([]string, 0)
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		exec, err := db.GetToolExecution(id)
		if err != nil || exec == nil {
			continue
		}
		switch strings.TrimSpace(exec.Status) {
		case mcp.ToolExecutionStatusQueued, mcp.ToolExecutionStatusRunning:
			out = append(out, id)
		}
	}
	return out
}

func hasCompletedEvidence(db *database.DB, ids []string) bool {
	if db == nil || len(ids) == 0 {
		return false
	}
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		exec, err := db.GetToolExecution(id)
		if err != nil || exec == nil {
			continue
		}
		if strings.TrimSpace(exec.Status) == mcp.ToolExecutionStatusCompleted {
			return true
		}
	}
	return false
}
