package assessment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/openai"
)

func TestOutcomeReviewCannotConfirmWithoutIndependentExecution(t *testing.T) {
	task := &Task{Goal: "goal", Criteria: []string{"criterion"}, Steps: []*Step{{ID: "source", Key: "source", Status: "completed", SuccessfulIDs: []string{"original"}, Evidence: []Evidence{{Kind: "finding"}}}, {ID: "verify", Key: "verify", VerifyOf: "source", Profile: "verification", Status: "completed", SuccessfulIDs: []string{"fresh"}}}}
	review := &Review{Criteria: []CriterionResult{{Index: 0, Status: "satisfied", ExecutionIDs: []string{"original"}}}, Findings: []FindingReview{{StepID: "source", EvidenceIndex: 0, Status: "confirmed", ExecutionIDs: []string{"original"}}}}
	if err := validateReview(task, review); err == nil {
		t.Fatal("discovery self-confirmed")
	}
	review.Findings[0].ExecutionIDs = []string{"fresh"}
	if err := validateReview(task, review); err != nil {
		t.Fatal(err)
	}
	review.Criteria[0].ExecutionIDs = []string{"invented"}
	if err := validateReview(task, review); err == nil {
		t.Fatal("invented acceptance proof accepted")
	}
}
func TestIndependentReviewCanRejectPlannerCompletion(t *testing.T) {
	store, task := fixture(t)
	r := normalRuntime()
	r.review = func(context.Context, *Task, []agent.ChatMessage, []agent.Tool) (*ModelReply, error) {
		return reply(Review{Summary: "Still lacks proof", Criteria: []CriterionResult{{Index: 0, Status: "insufficient", Reason: "No direct evidence for target criterion"}}}), nil
	}
	e := NewEngine(store, autonomousFixture{r}, nil)
	defer e.Close()
	e.Start(task.ID)
	got := waitTask(t, store, task.ID, func(t *Task) bool { return t.Status == "partial" && t.Deadline == 0 })
	if got.Review == nil || got.Reason != "acceptance_gaps" {
		t.Fatal("review did not govern final outcome")
	}
}
func TestCandidateAutomaticallyGetsFreshVerification(t *testing.T) {
	store, task := fixture(t)
	r := normalRuntime()
	r.complete = func(_ context.Context, t *Task, m []agent.ChatMessage, _ []agent.Tool) (*ModelReply, error) {
		if strings.Contains(m[0].Content, "You plan an authorized") {
			if len(t.Steps) == 0 {
				return planFixture("Inspect candidate", map[string]any{"key": "discover", "title": "Discover", "instruction": "Find candidate", "profile": "validation"}), nil
			}
			return reply(map[string]string{"status": "completed", "summary": "Planner wants to finish"}), nil
		}
		var st *Step
		for _, s := range t.Steps {
			if s.Status == "running" {
				st = s
				break
			}
		}
		if st == nil {
			return nil, fmt.Errorf("no worker")
		}
		if m[len(m)-1].Role == "tool" {
			kind := "finding"
			if st.VerifyOf != "" {
				kind = "fact"
			}
			return reply(map[string]any{"status": "completed", "evidence": []Evidence{{Kind: kind, Title: st.Key, ExecutionIDs: []string{"exec-" + st.ID}}}}), nil
		}
		return &ModelReply{Message: agent.ChatMessage{Role: "assistant", ToolCalls: []agent.ToolCall{{ID: "call-" + st.ID, Function: agent.FunctionCall{Name: "inspect", Arguments: map[string]interface{}{}}}}}, Tokens: 100}, nil
	}
	r.execute = func(_ context.Context, _ *Task, sid string, _ agent.ToolCall) (*agent.ToolExecutionResult, error) {
		return &agent.ToolExecutionResult{ExecutionID: "exec-" + sid, Result: "fixture observation"}, nil
	}
	r.review = func(_ context.Context, t *Task, _ []agent.ChatMessage, _ []agent.Tool) (*ModelReply, error) {
		source := t.Steps[0]
		verified := t.Steps[1]
		return reply(Review{Summary: "Independently checked", Criteria: []CriterionResult{{Index: 0, Status: "satisfied", ExecutionIDs: verified.SuccessfulIDs}}, Findings: []FindingReview{{StepID: source.ID, EvidenceIndex: 0, Status: "confirmed", ExecutionIDs: verified.SuccessfulIDs}}}), nil
	}
	e := NewEngine(store, autonomousFixture{r}, nil)
	defer e.Close()
	e.Start(task.ID)
	got := waitTask(t, store, task.ID, func(t *Task) bool { return t.Status == "completed" && t.Deadline == 0 })
	if len(got.Steps) != 2 || r.calls.Load() != 2 || got.Steps[1].VerifyOf != got.Steps[0].Key || Metrics(got).Confirmed != 1 {
		t.Fatal("verification was not independent", Metrics(got))
	}
}
func TestRepeatedIdenticalActionStopsBeforeThirdExecution(t *testing.T) {
	store, task := fixture(t)
	r := normalRuntime()
	var seq atomic.Int32
	base := r.complete
	r.complete = func(ctx context.Context, t *Task, m []agent.ChatMessage, tools []agent.Tool) (*ModelReply, error) {
		if strings.Contains(m[0].Content, "You plan an authorized") {
			return base(ctx, t, m, tools)
		}
		return &ModelReply{Message: agent.ChatMessage{Role: "assistant", ToolCalls: []agent.ToolCall{{ID: fmt.Sprint(seq.Add(1)), Function: agent.FunctionCall{Name: "inspect", Arguments: map[string]interface{}{"target": "same"}}}}}, Tokens: 100}, nil
	}
	e := NewEngine(store, autonomousFixture{r}, nil)
	defer e.Close()
	e.Start(task.ID)
	got := waitTask(t, store, task.ID, func(t *Task) bool { return t.Status == "blocked" && t.Deadline == 0 })
	if r.calls.Load() != 2 || got.Steps[0].FailureKind != "stagnation" || Metrics(got).Suppressed != 1 {
		t.Fatal("stagnation not contained", r.calls.Load(), Metrics(got))
	}
}
func TestModelTransientRetryNeverReplaysExternalTools(t *testing.T) {
	store, task := fixture(t)
	store.Change(task.ID, "", "", "", func(t *Task) error { t.Status = "running"; return nil })
	var n atomic.Int32
	r := &fakeRuntime{complete: func(context.Context, *Task, []agent.ChatMessage, []agent.Tool) (*ModelReply, error) {
		if n.Add(1) == 1 {
			return nil, &openai.APIError{StatusCode: 429, Body: "fixture rate limit"}
		}
		return reply(map[string]string{"status": "blocked"}), nil
	}}
	e := NewEngine(store, r, nil)
	if _, err := e.call(context.Background(), task.ID, []agent.ChatMessage{{Role: "user", Content: "fixture"}}, nil); err != nil {
		t.Fatal(err)
	}
	got, _ := store.Get(task.ID)
	if n.Load() != 2 || r.calls.Load() != 0 || got.ModelRetries != 1 {
		t.Fatal("retry accounting or boundary failed")
	}
	r.complete = func(context.Context, *Task, []agent.ChatMessage, []agent.Tool) (*ModelReply, error) {
		return nil, &openai.APIError{StatusCode: 401, Body: "fixture auth error"}
	}
	if _, err := e.call(context.Background(), task.ID, nil, nil); failureKind(err) != "model_auth" {
		t.Fatal(err)
	}
}
func TestBudgetAdjustmentAndModelFailureResumePreserveProgress(t *testing.T) {
	store, task := fixture(t)
	trace, _ := json.Marshal([]agent.ChatMessage{{Role: "system", Content: "system"}, {Role: "user", Content: "task"}, {Role: "assistant", Content: "saved progress"}})
	store.Change(task.ID, "", "", "", func(t *Task) error {
		t.Status = "failed"
		t.Tokens = 3000
		t.Rounds = 2
		t.Steps = []*Step{{ID: "step", Status: "blocked", FailureKind: "model_transient", Trace: trace, Turns: 4}}
		return nil
	})
	e := NewEngine(store, normalRuntime(), nil)
	if err := e.AdjustBudget(task.ID, BudgetChange{200000, 3600, 20}); err != nil {
		t.Fatal(err)
	}
	if err := e.Retry(task.ID, "step"); err != nil {
		t.Fatal(err)
	}
	got, _ := store.Get(task.ID)
	if got.Tokens != 3000 || got.Steps[0].Turns != 4 || len(got.Steps[0].Trace) == 0 {
		t.Fatal("safe resume lost completed work")
	}
	if err := e.AdjustBudget(task.ID, BudgetChange{1000, 3600, 20}); err == nil {
		t.Fatal("budget below usage allowed")
	}
	store.Change(task.ID, "", "", "", func(t *Task) error { t.Status = "running"; t.TokenBudget = 10000; t.Tokens = 2000; return nil })
	if _, err := e.call(context.Background(), task.ID, []agent.ChatMessage{{Role: "user", Content: strings.Repeat("x", 2000)}}, nil); !errors.Is(err, ErrBudget) {
		t.Fatal("review reserve consumed by exploration", err)
	}
}
func TestSkillLoadingCannotEscapeRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "secret"), []byte("secret fixture"), 0600)
	os.Mkdir(filepath.Join(root, "skill"), 0700)
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(root, "skill", "SKILL.md")); err != nil {
		t.Skip("symlink unavailable")
	}
	rt := &CSAIRuntime{SkillsRoot: root}
	if _, err := rt.skillText("skill"); err == nil {
		t.Fatal("skill escaped root")
	}
	if _, err := rt.skillText("../outside"); err == nil {
		t.Fatal("traversal accepted")
	}
}
func TestMemoryFiltersPreferNewestRelevantFailure(t *testing.T) {
	store, task := fixture(t)
	e := NewEngine(store, normalRuntime(), nil)
	store.Event(task.ID, "s", "tool_result", "old", map[string]any{"tool": "inspect", "asset": "fixture.test", "output": "login failed", "is_error": true, "blocked": false})
	store.Event(task.ID, "s", "tool_result", "new", map[string]any{"tool": "inspect", "asset": "fixture.test", "output": "login failed", "is_error": true, "blocked": false})
	store.Event(task.ID, "s", "tool_result", "unrelated", map[string]any{"tool": "other", "output": "login ok", "is_error": false, "blocked": false})
	content, err := e.internalTool(context.Background(), task.ID, "", agent.ToolCall{Function: agent.FunctionCall{Name: memoryTool, Arguments: map[string]interface{}{"query": "login failed", "tool": "inspect", "asset": "fixture.test", "status": "failed"}}})
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Observations []struct {
			ID int64 `json:"event_id"`
		} `json:"observations"`
	}
	if err = json.Unmarshal([]byte(content), &result); err != nil || len(result.Observations) != 2 || result.Observations[0].ID <= result.Observations[1].ID {
		t.Fatal("ranking/filter failure", content, err)
	}
}

func TestStagnationCannotBeBypassedByRenamingDirection(t *testing.T) {
	call := agent.ToolCall{Function: agent.FunctionCall{Name: "inspect", Arguments: map[string]interface{}{"target": "fixture"}}}
	a := Attempt{Action: actionFingerprint(call), Outcome: "same", Material: digest(""), Cycle: 0}
	old := &Step{ID: "old", FailureKind: "stagnation", Attempts: []Attempt{a, a}}
	fresh := &Step{ID: "renamed"}
	task := &Task{Steps: []*Step{old, fresh}}
	if !stagnantInTask(task, fresh, call) {
		t.Fatal("renaming bypassed negative direction memory")
	}
	fresh.VerifyOf = "old"
	if stagnantInTask(task, fresh, call) {
		t.Fatal("independent verification was suppressed")
	}
	fresh.VerifyOf = ""
	task.Material = "operator supplied a changed prerequisite"
	if stagnantInTask(task, fresh, call) {
		t.Fatal("new human guidance did not permit reconsideration")
	}
}

func TestFindingReviewIsBatchedAndPreservesAllVerdicts(t *testing.T) {
	store, task := fixture(t)
	store.Change(task.ID, "", "", "", func(t *Task) error {
		t.Status = "running"
		t.TokenBudget = 1000000
		s := &Step{ID: "source", Key: "source", Status: "completed", SuccessfulIDs: []string{"source-exec"}}
		for i := 0; i < 21; i++ {
			s.Evidence = append(s.Evidence, Evidence{Kind: "finding", Title: fmt.Sprint(i), ExecutionIDs: []string{"source-exec"}})
		}
		t.Steps = []*Step{s}
		return nil
	})
	var calls atomic.Int32
	r := normalRuntime()
	r.review = func(_ context.Context, _ *Task, m []agent.ChatMessage, _ []agent.Tool) (*ModelReply, error) {
		calls.Add(1)
		part := strings.Split(m[1].Content, "Candidate / verification index: ")
		if len(part) != 2 {
			return nil, fmt.Errorf("missing batch index")
		}
		var rows []map[string]interface{}
		if err := json.Unmarshal([]byte(part[1]), &rows); err != nil {
			return nil, err
		}
		out := Review{Criteria: []CriterionResult{{Index: 0, Status: "partial", Reason: "Pending retest", ExecutionIDs: []string{"source-exec"}}}}
		for _, row := range rows {
			if id, ok := row["step_id"].(string); ok {
				out.Findings = append(out.Findings, FindingReview{StepID: id, EvidenceIndex: int(row["index"].(float64)), Status: "needs_retest", Reason: "Needs fresh proof"})
			}
		}
		if len(out.Findings) > 8 {
			t.Fatal("unbounded review output")
		}
		return reply(out), nil
	}
	e := NewEngine(store, r, nil)
	if err := e.review(context.Background(), task.ID, false); err != nil {
		t.Fatal(err)
	}
	got, _ := store.Get(task.ID)
	if calls.Load() != 3 || got.Review == nil || len(got.Review.Findings) != 21 || got.Status != "partial" {
		t.Fatal("batched review lost work", calls.Load(), got.Status)
	}
}

func TestPartialReviewFeedbackSurvivesResume(t *testing.T) {
	store, task := fixture(t)
	store.Change(task.ID, "", "", "", func(t *Task) error {
		t.Status = "partial"
		t.Review = &Review{Summary: "Missing negative control", Criteria: []CriterionResult{{Index: 0, Status: "insufficient", Reason: "Missing negative control"}}}
		return nil
	})
	r := normalRuntime()
	seen := make(chan string, 1)
	r.complete = func(ctx context.Context, t *Task, m []agent.ChatMessage, _ []agent.Tool) (*ModelReply, error) {
		if strings.Contains(m[0].Content, "You plan an authorized") {
			seen <- m[1].Content
			return reply(map[string]string{"status": "blocked"}), nil
		}
		return nil, fmt.Errorf("unexpected work")
	}
	e := NewEngine(store, r, nil)
	defer e.Close()
	if err := e.Start(task.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case prompt := <-seen:
		if !strings.Contains(prompt, "Missing negative control") {
			t.Fatal("acceptance gaps lost on resume")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no planning turn")
	}
}
