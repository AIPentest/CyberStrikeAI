package assessment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"cyberstrike-ai/internal/agent"
	"github.com/google/uuid"
)

// Approval binds a host-generated request to exact arguments and the run actor.
// Model call IDs alone are neither unique nor a trustworthy authorization token.
func callDigest(call agent.ToolCall) string {
	b, err := json.Marshal(call)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func clearApproval(s *Step) {
	s.Approved = false
	s.ApprovalID = ""
	s.ApprovalDigest = ""
	s.ApprovalKind = ""
	s.ApprovedBy = ""
	s.ApprovedExecutorID = ""
}
func requestApproval(t *Task, s *Step, call *agent.ToolCall) {
	clearApproval(s)
	s.ApprovalID = uuid.NewString()
	s.ApprovalDigest = callDigest(*call)
	s.ApprovedExecutorID = t.ExecutorID
	s.Pending = call
	s.Status = "awaiting_approval"
}
func (e *Engine) approvedCall(ctx context.Context, t *Task, s *Step, call agent.ToolCall) (bool, error) {
	if !s.Approved || s.Pending == nil || s.ApprovalDigest == "" || s.ApprovalDigest != callDigest(call) || s.ApprovedExecutorID != t.ExecutorID {
		return false, nil
	}
	manual := true
	if p, ok := e.Runtime.(interface{ RequiresApproval(*Task, string) bool }); ok {
		manual = p.RequiresApproval(t, call.Function.Name)
	}
	if s.ApprovalKind == "policy" {
		return !manual, nil
	}
	if s.ApprovalKind != "manual" || s.ApprovedBy == "" {
		return false, nil
	}
	if verifier, ok := e.Runtime.(interface {
		VerifyApproval(context.Context, *Task, *Step) (bool, error)
	}); ok {
		return verifier.VerifyApproval(ctx, t, s)
	}
	return true, nil
}
func (r *CSAIRuntime) VerifyApproval(ctx context.Context, t *Task, s *Step) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	a, err := r.DB.ResolveRBACAccess(s.ApprovedBy)
	if err != nil {
		return false, err
	}
	if a == nil || !a.User.Enabled {
		return false, nil
	}
	for _, permission := range []string{"assessment:write", "hitl:write"} {
		scope := a.PermissionScopes[permission]
		if scope == "" {
			scope = a.Scope
		}
		if !a.Permissions[permission] || !r.DB.UserCanAccessResource(a.User.ID, scope, "assessment", t.ID) {
			return false, nil
		}
	}
	return true, nil
}
func validateToolCalls(messages []agent.ChatMessage, calls []agent.ToolCall, used []string) error {
	if len(calls) > 8 {
		return fmt.Errorf("at most 8 tool calls are permitted per model turn")
	}
	seen := map[string]bool{}
	for _, id := range used {
		seen[id] = true
	}
	for _, m := range messages {
		for _, c := range m.ToolCalls {
			seen[c.ID] = true
		}
	}
	for _, c := range calls {
		if c.ID == "" || len(c.ID) > 200 || seen[c.ID] {
			return fmt.Errorf("tool call IDs must be present, bounded and unique")
		}
		seen[c.ID] = true
		if len(c.Function.Name) > 200 || c.Function.Name == "" {
			return fmt.Errorf("invalid tool name")
		}
		b, err := json.Marshal(c.Function.Arguments)
		if err != nil || len(b) > 64<<10 {
			return fmt.Errorf("tool arguments exceed 64 KiB")
		}
	}
	return nil
}

// EvidenceRevision identifies the exact observation shown to a reviewer.
func EvidenceRevision(v Evidence) string {
	v.Revision = ""
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
