package assessment

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"cyberstrike-ai/internal/agent"
	"github.com/google/uuid"
)

type CriterionResult struct {
	Index        int      `json:"index"`
	Status       string   `json:"status"`
	Reason       string   `json:"reason"`
	ExecutionIDs []string `json:"execution_ids"`
}
type FindingReview struct {
	StepID        string   `json:"step_id"`
	EvidenceIndex int      `json:"evidence_index"`
	Status        string   `json:"status"`
	Reason        string   `json:"reason"`
	ExecutionIDs  []string `json:"execution_ids"`
}
type Review struct {
	Summary  string            `json:"summary"`
	Criteria []CriterionResult `json:"criteria"`
	Findings []FindingReview   `json:"findings"`
}
type purposeKey struct{}
type purpose struct{ Phase, Profile string }

func withPurpose(ctx context.Context, phase, profile string) context.Context {
	return context.WithValue(ctx, purposeKey{}, purpose{phase, profile})
}
func requestPurpose(ctx context.Context) purpose {
	p, _ := ctx.Value(purposeKey{}).(purpose)
	if p.Phase == "" {
		p.Phase = "planning"
	}
	return p
}
func criteria(t *Task) []string {
	if len(t.Criteria) > 0 {
		return t.Criteria
	}
	return []string{t.Goal}
}
func reserveForReview(t *Task) int {
	r := t.TokenBudget / 5
	if r > 64000 {
		r = 64000
	}
	return r
}

// Verification uses a separate work item and fresh tool executions. It never
// promotes the discovery agent's claim directly to a confirmed finding.
func ensureVerificationSteps(t *Task) {
	for _, source := range t.Steps {
		if source.Status != "completed" || source.VerifyOf != "" || source.Profile == "verification" {
			continue
		}
		found := false
		for _, v := range source.Evidence {
			if v.Kind == "finding" {
				found = true
			}
		}
		if !found {
			continue
		}
		exists := false
		for _, s := range t.Steps {
			if s.VerifyOf == source.Key {
				exists = true
			}
		}
		if exists || len(t.Steps) >= MaxSteps {
			continue
		}
		t.Steps = append(t.Steps, &Step{ID: uuid.NewString(), Key: "verify-" + source.ID, Title: "Verify: " + source.Title, Profile: "verification", VerifyOf: source.Key, DependsOn: []string{source.Key}, Status: "pending", Evidence: []Evidence{}, Instruction: "Independently check the candidate findings from step " + source.Key + " within the original scope. Read their observations, perform fresh minimally invasive checks and negative controls, and record fact/gap evidence with new execution IDs. Explain reproducibility and counter-evidence. Do not repeat destructive operations or assume the discovery was correct. Do not report the same candidate as a new finding."})
	}
}
func (e *Engine) review(ctx context.Context, id string, forcedPartial bool) error {
	ctx = withPurpose(ctx, "review", "")
	t, err := e.Store.Get(id)
	if err != nil {
		return err
	}
	if err = e.Store.Change(id, "", "review_start", "Independent outcome review", func(t *Task) error {
		if t.Status != "running" && t.Status != "awaiting_approval" {
			return ErrConflict
		}
		t.Phase = "review"
		return nil
	}); err != nil {
		return err
	}
	out := Review{Summary: "Independent review", Criteria: []CriterionResult{}, Findings: []FindingReview{}}
	for i := range criteria(t) {
		out.Criteria = append(out.Criteria, CriterionResult{Index: i, Status: "insufficient", Reason: "Review not completed"})
	}
	keys := []string{}
	for _, s := range t.Steps {
		for i, v := range s.Evidence {
			if v.Kind == "finding" {
				keys = append(keys, fmt.Sprintf("%s/%d", s.ID, i))
				out.Findings = append(out.Findings, FindingReview{StepID: s.ID, EvidenceIndex: i, Status: "needs_retest", Reason: "Review not completed"})
			}
		}
	}
	batches := (len(keys) + 7) / 8
	if batches == 0 {
		batches = 1
	}
	for batch := 0; batch < batches; batch++ {
		view := t.Public()
		selected := map[string]bool{}
		for i := batch * 8; i < len(keys) && i < (batch+1)*8; i++ {
			selected[keys[i]] = true
		}
		for _, s := range view.Steps {
			for i := range s.Evidence {
				if s.Evidence[i].Kind == "finding" && !selected[fmt.Sprintf("%s/%d", s.ID, i)] {
					s.Evidence[i] = Evidence{Kind: "omitted"}
				}
			}
		}
		result, reviewErr := e.reviewBatch(ctx, id, view)
		if reviewErr != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			forcedPartial = true
			out.Summary = "Review incomplete: " + clip(reviewErr.Error(), 1000)
			break
		}
		out.Summary = clip(result.Summary, 8000)
		for _, v := range result.Criteria {
			if batch == 0 || verdictRank(v.Status) > verdictRank(out.Criteria[v.Index].Status) {
				out.Criteria[v.Index] = v
			}
		}
		for _, v := range result.Findings {
			for i, old := range out.Findings {
				if old.StepID == v.StepID && old.EvidenceIndex == v.EvidenceIndex {
					out.Findings[i] = v
					break
				}
			}
		}
		if err = e.Store.Change(id, "", "review_batch", "Saved independent review batch", func(t *Task) error {
			if t.Status != "running" && t.Status != "awaiting_approval" {
				return ErrConflict
			}
			copy := out
			t.Review = &copy
			return nil
		}, out); err != nil {
			return err
		}
	}
	return e.Store.Change(id, "", "review_result", clip(out.Summary, 4000), func(t *Task) error {
		if t.Status != "running" && t.Status != "awaiting_approval" {
			return ErrConflict
		}
		complete := !forcedPartial
		for _, r := range out.Criteria {
			if r.Status != "satisfied" {
				complete = false
			}
		}
		for _, finding := range out.Findings {
			if finding.Status == "needs_retest" {
				complete = false
			}
		}
		for _, s := range t.Steps {
			if s.Status != "completed" && s.Status != "skipped" {
				complete = false
			}
		}
		t.Review = &out
		t.Summary = clip(out.Summary, 8000)
		t.Phase = ""
		t.Status = "partial"
		t.Reason = "acceptance_gaps"
		if forcedPartial {
			t.Reason = "partial_review"
		}
		if complete {
			t.Status = "completed"
			t.Reason = "acceptance_reviewed"
		}
		return nil
	}, out)
}
func validateReview(t *Task, r *Review) error {
	if len(r.Criteria) != len(criteria(t)) {
		return fmt.Errorf("review every acceptance criterion")
	}
	known := map[string]bool{}
	for _, s := range t.Steps {
		for _, id := range s.SuccessfulIDs {
			known[id] = true
		}
	}
	seen := map[int]bool{}
	for i := range r.Criteria {
		v := &r.Criteria[i]
		if v.Index < 0 || v.Index >= len(criteria(t)) || seen[v.Index] {
			return fmt.Errorf("invalid criterion index")
		}
		seen[v.Index] = true
		if !contains([]string{"satisfied", "partial", "not_met", "insufficient"}, v.Status) {
			return fmt.Errorf("invalid criterion verdict")
		}
		if v.Status == "satisfied" && len(v.ExecutionIDs) == 0 {
			return fmt.Errorf("satisfied criterion requires execution evidence")
		}
		for _, id := range v.ExecutionIDs {
			if !known[id] {
				return fmt.Errorf("unknown review execution %q", id)
			}
		}
		v.Reason = clip(v.Reason, 3000)
	}
	expected := map[string]bool{}
	for _, s := range t.Steps {
		for i, v := range s.Evidence {
			if v.Kind == "finding" {
				expected[fmt.Sprintf("%s/%d", s.ID, i)] = true
			}
		}
	}
	if len(r.Findings) != len(expected) {
		return fmt.Errorf("review every candidate finding")
	}
	for i := range r.Findings {
		v := &r.Findings[i]
		key := fmt.Sprintf("%s/%d", v.StepID, v.EvidenceIndex)
		if !expected[key] {
			return fmt.Errorf("invalid or duplicate finding review")
		}
		delete(expected, key)
		if !contains([]string{"confirmed", "rejected", "needs_retest"}, v.Status) {
			return fmt.Errorf("invalid finding verdict")
		}
		source := t.Step(v.StepID)
		if v.Status != "needs_retest" {
			fresh := false
			for _, id := range v.ExecutionIDs {
				if !known[id] {
					return fmt.Errorf("unknown finding verification")
				}
				for _, s := range t.Steps {
					if s.VerifyOf == source.Key && s.Status == "completed" && contains(s.SuccessfulIDs, id) && !contains(source.SuccessfulIDs, id) {
						fresh = true
					}
				}
			}
			if !fresh {
				return fmt.Errorf("finding verdict requires fresh independent verification")
			}
		}
		v.Reason = clip(v.Reason, 3000)
	}
	r.Summary = clip(r.Summary, 8000)
	return nil
}
func findingReviewStatus(t *Task, sid string, index int) string {
	if t.Review != nil {
		for _, r := range t.Review.Findings {
			if r.StepID == sid && r.EvidenceIndex == index {
				return r.Status
			}
		}
	}
	return "needs_retest"
}
func criterionText(lines []string) []string {
	out := []string{}
	for _, line := range lines {
		if s := strings.TrimSpace(line); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func (e *Engine) reviewBatch(ctx context.Context, id string, t *Task) (*Review, error) {
	var err error
	batchSources := map[string]bool{}
	for _, s := range t.Steps {
		for _, v := range s.Evidence {
			if v.Kind == "finding" {
				batchSources[s.Key] = true
			}
		}
	}
	specification, _ := json.Marshal(criteria(t))
	msgs := []agent.ChatMessage{{Role: "system", Content: `You are the independent assessment outcome reviewer. Review each user acceptance criterion against recorded evidence; discovery agents' confidence and successful command execution are not proof of the criterion. You may read assessment_memory, but cannot execute external actions. Never invent execution IDs. Distinguish satisfied, partial, not_met, insufficient. Confirm a candidate finding only with new successful executions from its separate verification step and meaningful positive/negative controls; otherwise use needs_retest or rejected. Return JSON only: {"summary":"outcome and remaining gaps","criteria":[{"index":0,"status":"satisfied|partial|not_met|insufficient","reason":"evidence-based explanation","execution_ids":["id"]}],"findings":[{"step_id":"source step ID","evidence_index":0,"status":"confirmed|rejected|needs_retest","reason":"why","execution_ids":["fresh verification execution id"]}]}. Return exactly one row per criterion and per candidate finding. Keep each reason under 80 characters, the summary under 400 characters, and cite at most two decisive execution IDs per row. Use the user's language.`}, {Role: "user", Content: "Acceptance criteria: " + string(specification) + "\n" + reviewContext(t)}}
	// Include exact source IDs and all verification relationships, independently of
	// the bounded prompt summaries used for exploratory planning.
	rows := []map[string]any{}
	for _, s := range t.Steps {
		for i, v := range s.Evidence {
			if v.Kind == "finding" {
				rows = append(rows, map[string]any{"step_id": s.ID, "index": i, "key": s.Key, "finding": Evidence{Kind: v.Kind, Title: clip(v.Title, 200), Detail: clip(v.Detail, 1200), ExecutionIDs: tailIDs(v.ExecutionIDs, 8)}})
			}
		}
		if s.VerifyOf != "" && batchSources[s.VerifyOf] {
			rows = append(rows, map[string]any{"verification_step": s.ID, "verify_of": s.VerifyOf, "status": s.Status, "executions": tailIDs(s.SuccessfulIDs, 8), "result": clip(s.Result, 1200)})
		}
	}
	b, _ := json.Marshal(rows)
	msgs[1].Content += "\nCandidate / verification index: " + string(b)
	var reply *ModelReply
	for n := 0; ; n++ {
		tools := assessmentTools(false)
		if n >= 3 {
			tools = nil
		}
		reply, err = e.call(ctx, id, msgs, tools)
		if err != nil {
			return nil, err
		}
		if len(reply.Message.ToolCalls) == 0 {
			break
		}
		if n >= 3 {
			return nil, fmt.Errorf("review memory budget exceeded")
		}
		if err = validateToolCalls(msgs, reply.Message.ToolCalls, nil); err != nil {
			return nil, err
		}
		msgs = append(msgs, reply.Message)
		for _, c := range reply.Message.ToolCalls {
			content := "Only task memory reads are allowed"
			if c.Function.Name == memoryTool {
				content, err = e.internalTool(ctx, id, "", c)
				if err != nil {
					content = "Memory error: " + err.Error()
				}
			}
			msgs = append(msgs, agent.ChatMessage{Role: "tool", ToolCallID: c.ID, ToolName: c.Function.Name, Content: content})
		}
	}
	var out Review
	_, err = e.repairJSON(ctx, id, msgs, reply, func(content string) error {
		out = Review{}
		if err := decodeObject(content, &out); err != nil {
			return err
		}
		return validateReview(t, &out)
	})
	if err != nil {
		return nil, err
	}

	return &out, nil
}

func verdictRank(status string) int {
	switch status {
	case "satisfied":
		return 0
	case "partial":
		return 1
	case "insufficient":
		return 2
	default:
		return 3
	}
}
func tailIDs(ids []string, n int) []string {
	if len(ids) > n {
		return ids[len(ids)-n:]
	}
	return ids
}
func reviewContext(t *Task) string {
	rows := []map[string]any{}
	for _, s := range t.Steps {
		rows = append(rows, map[string]any{"id": s.ID, "key": s.Key, "status": s.Status, "result": clip(s.Result, 150), "verify_of": s.VerifyOf, "executions": tailIDs(s.SuccessfulIDs, 4)})
	}
	b, _ := json.Marshal(map[string]any{"goal": clip(t.Goal, 2000), "scope": clip(t.Scope, 2000), "material": clip(t.Material, 2000), "directions": rows})
	return string(b)
}
