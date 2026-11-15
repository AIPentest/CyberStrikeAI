// Package deepassessment coordinates durable objectives around the existing Agent runtime.
// It schedules bounded Agent runs; it does not implement a second tool loop.
package deepassessment

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

type Task struct {
	Archived            bool       `json:"archived"`
	ArchivedAt          *time.Time `json:"archived_at,omitempty"`
	PlannedGraphVersion int64      `json:"planned_graph_version"`
	ActiveSeconds       int64      `json:"active_seconds"`
	// Legacy budget fields are decoded only to normalize old saved tasks.

	Deadline         *time.Time                `json:"deadline,omitempty"`
	AutoResume       bool                      `json:"auto_resume"`
	RestartPending   bool                      `json:"restart_pending,omitempty"`
	NextWake         *time.Time                `json:"next_wake,omitempty"`
	RetryCount       int                       `json:"retry_count"`
	TokenBudget      int64                     `json:"token_budget,omitempty"`
	TokensUsed       int64                     `json:"tokens_used"`
	Metrics          map[string]SessionMetrics `json:"metrics,omitempty"`
	ID               string                    `json:"id"`
	OwnerID          string                    `json:"owner_id"`
	ExecutorID       string                    `json:"executor_id"`
	ProjectID        string                    `json:"project_id"`
	Name             string                    `json:"name"`
	Objective        string                    `json:"objective"`
	Scope            string                    `json:"scope"`
	Criteria         string                    `json:"criteria"`
	Role             string                    `json:"role"`
	Model            string                    `json:"model"`
	HITLPolicy       string                    `json:"hitl_policy"`
	Concurrency      int                       `json:"concurrency"`
	MaxRounds        int                       `json:"max_rounds,omitempty"`
	RemainingSeconds int64                     `json:"remaining_seconds,omitempty"`
	Rounds           int                       `json:"rounds"`
	Status           string                    `json:"status"`
	Revision         int64                     `json:"revision"`
	Strategy         string                    `json:"strategy"`
	Summary          string                    `json:"summary"`
	Question         string                    `json:"question"`
	Guidance         []string                  `json:"guidance"`
	Steps            []Step                    `json:"steps"`
	CreatedAt        time.Time                 `json:"created_at"`
	UpdatedAt        time.Time                 `json:"updated_at"`
	RunStartedAt     *time.Time                `json:"run_started_at,omitempty"`
}

type Step struct {
	AssetIDs        []string `json:"asset_ids,omitempty"`
	Priority        int      `json:"priority,omitempty"`
	Sources         []string `json:"source_ids,omitempty"`
	Evidence        []string `json:"evidence,omitempty"`
	LastProgress    string   `json:"last_progress,omitempty"`
	StalledSegments int      `json:"stalled_segments"`
	ID              string   `json:"id"`
	Title           string   `json:"title"`
	Instruction     string   `json:"instruction"`
	DependsOn       []string `json:"depends_on"`
	Status          string   `json:"status"`
	Summary         string   `json:"summary"`
	ConversationID  string   `json:"conversation_id"`
	Conversations   []string `json:"conversations,omitempty"`
	Attempts        int      `json:"attempts"`
}

type Proposal struct {
	Strategy string   `json:"strategy"`
	Summary  string   `json:"summary"`
	Steps    []Step   `json:"steps"`
	Retire   []string `json:"retire"`
	Question string   `json:"question"`
}

type Outcome struct {
	Evidence       []string `json:"evidence,omitempty"`
	Progress       string   `json:"progress,omitempty"`
	Status         string   `json:"status"` // completed or blocked; failed/interrupted are assigned by the host
	Summary        string   `json:"summary"`
	ConversationID string   `json:"conversation_id"`
}

type Verdict struct {
	Complete bool     `json:"complete"`
	Summary  string   `json:"summary"`
	Missing  string   `json:"missing"`
	Evidence []string `json:"evidence"` // completed step IDs, checked by the host
}

type Event struct {
	ID        int64     `json:"id"`
	StepID    string    `json:"step_id"`
	Kind      string    `json:"kind"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

type Runtime interface {
	Plan(context.Context, *Task) (Proposal, error)
	Run(context.Context, *Task, Step, func(string, string) error) (Outcome, error)
	Review(context.Context, *Task) (Verdict, error)
}

func (t *Task) Validate() error {
	criteriaLimit := 8000
	// Inheriting the objective must not truncate or reject an otherwise valid goal.
	if t.Criteria == t.Objective {
		criteriaLimit = 16000
	}
	for _, f := range []struct {
		value string
		max   int
		name  string
	}{
		{t.Name, 200, "name"}, {t.Objective, 16000, "objective"}, {t.Scope, 16000, "scope"}, {t.Criteria, criteriaLimit, "criteria"},
	} {
		if strings.TrimSpace(f.value) == "" || utf8.RuneCountInString(f.value) > f.max {
			return fmt.Errorf("%s is required (maximum %d characters)", f.name, f.max)
		}
	}
	if t.Concurrency < 1 || t.Concurrency > 4 {
		return fmt.Errorf("invalid concurrency")
	}
	return nil
}

func (t *Task) Step(id string) *Step {
	for i := range t.Steps {
		if t.Steps[i].ID == id {
			return &t.Steps[i]
		}
	}
	return nil
}

func (t *Task) Ready(s Step) bool {
	if s.Status != "pending" {
		return false
	}
	for _, id := range s.DependsOn {
		if p := t.Step(id); p == nil || p.Status != "completed" {
			return false
		}
	}
	return true
}

func Active(status string) bool {
	return status == "running" || status == "pausing" || status == "cancelling"
}

// Queue is independent of planner transcripts. Equal priorities keep insertion order.
func (t *Task) Queue() []Step {
	steps := append([]Step(nil), t.Steps...)
	sort.SliceStable(steps, func(i, j int) bool { return stepPriority(steps[i]) < stepPriority(steps[j]) })
	return steps
}
func stepPriority(s Step) int {
	if s.Priority == 0 {
		return 100
	}
	return s.Priority
}

// ApplyProposal only adds work or retires unstarted branches. It cannot rewrite
// observations, active work, user criteria, or user intent.
func ApplyProposal(t *Task, p Proposal) error {
	if len(p.Steps) > 8 {
		return fmt.Errorf("one planning update may add at most eight steps")
	}
	if len(p.Strategy) > 12000 || len(p.Summary) > 12000 || len(p.Question) > 4000 {
		return fmt.Errorf("plan text is too large")
	}
	seen := map[string]bool{}
	for _, s := range t.Steps {
		seen[strings.Join(strings.Fields(strings.ToLower(s.Instruction)), " ")] = true
	}
	all := append([]Step(nil), t.Steps...)
	for _, s := range p.Steps {
		if s.ID == "" || len(s.ID) > 64 || strings.ContainsAny(s.ID, "/\\ \t\n") || t.Step(s.ID) != nil {
			return fmt.Errorf("invalid or duplicate step ID: %s", s.ID)
		}
		for _, existing := range all {
			if existing.ID == s.ID {
				return fmt.Errorf("duplicate step ID: %s", s.ID)
			}
		}
		if strings.TrimSpace(s.Title) == "" || len(s.Title) > 600 || strings.TrimSpace(s.Instruction) == "" || len(s.Instruction) > 16000 || len(s.DependsOn) > 16 || len(s.Sources) > 16 || len(s.AssetIDs) > 32 || s.Priority < 0 || s.Priority > 1000 {
			return fmt.Errorf("invalid step: %s", s.ID)
		}
		key := strings.Join(strings.Fields(strings.ToLower(s.Instruction)), " ")
		if seen[key] {
			return fmt.Errorf("repeated direction: %s; use a different approach or request an explicit retry", s.ID)
		}
		seen[key] = true
		if s.DependsOn == nil {
			s.DependsOn = []string{}
		}
		s.Status = "pending"
		s.Summary = ""
		s.ConversationID = ""
		s.Conversations = nil
		s.Attempts = 0
		s.Evidence = nil
		s.LastProgress = ""
		s.StalledSegments = 0
		all = append(all, s)
	}
	for _, id := range p.Retire {
		s := t.Step(id)
		if s == nil || s.Status == "running" || s.Status == "completed" || (s.Status == "cleanup_unconfirmed" || s.Status == "uncertain") {
			return fmt.Errorf("cannot retire step %s", id)
		}
	}
	byID := map[string]Step{}
	for _, s := range all {
		byID[s.ID] = s
	}
	visiting, done := map[string]bool{}, map[string]bool{}
	var visit func(string) error
	visit = func(id string) error {
		if visiting[id] {
			return fmt.Errorf("cyclic step dependencies")
		}
		if done[id] {
			return nil
		}
		s, ok := byID[id]
		if !ok {
			return fmt.Errorf("missing dependency: %s", id)
		}
		visiting[id] = true
		for _, d := range s.DependsOn {
			if err := visit(d); err != nil {
				return err
			}
		}
		visiting[id] = false
		done[id] = true
		return nil
	}
	for id := range byID {
		if err := visit(id); err != nil {
			return err
		}
	}
	t.Steps = all
	for _, id := range p.Retire {
		t.Step(id).Status = "retired"
	}
	t.Strategy = p.Strategy
	t.Summary = p.Summary
	t.Question = p.Question
	return nil
}

func AcceptVerdict(t *Task, v Verdict) bool {
	if !v.Complete || strings.TrimSpace(v.Summary) == "" || strings.TrimSpace(v.Missing) != "" || len(v.Evidence) == 0 {
		return false
	}
	for _, s := range t.Steps {
		if s.Status != "completed" && s.Status != "retired" {
			return false
		}
	}
	for _, id := range v.Evidence {
		if s := t.Step(id); s == nil || s.Status != "completed" || s.ConversationID == "" || s.Summary == "" {
			return false
		}
	}
	return true
}

func Clip(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// SessionMetrics is a monotonic snapshot from Pi, including compaction usage.
type SessionMetrics struct {
	ContextTokens       *int64 `json:"context_tokens"`
	ContextWindow       int64  `json:"context_window"`
	ToolDefinitionBytes int64  `json:"tool_definition_bytes"`
	StepID              string `json:"step_id"`
	Tokens              int64  `json:"tokens"`
	Compactions         int    `json:"compactions"`
	ToolCount           int    `json:"tool_count"`
}

// Continuous normalizes persisted pre-continuous tasks without starting them.
func (t *Task) Continuous() {
	t.Deadline = nil
	t.TokenBudget = 0
	t.MaxRounds = 0
	t.RemainingSeconds = 0
	if t.Status == "budget_exhausted" {
		t.Status = "paused"
	}
}
