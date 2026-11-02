// Package assessment implements durable, conversation-independent assessments.
package assessment

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"cyberstrike-ai/internal/agent"
	"github.com/google/uuid"
)

const MaxSteps = 100
const MaxTurns = 20

// Task is the durable source of truth. No chat conversation or message is created.
type Task struct {
	PreviousReview   *Review           `json:"previous_review,omitempty"`
	Criteria         []string          `json:"criteria"`
	ReviewerModel    string            `json:"reviewer_model,omitempty"`
	WorkerModels     map[string]string `json:"worker_models,omitempty"`
	SkillIDs         []string          `json:"skill_ids,omitempty"`
	Review           *Review           `json:"review,omitempty"`
	Phase            string            `json:"phase,omitempty"`
	FailureKind      string            `json:"failure_kind,omitempty"`
	ModelCalls       int               `json:"model_calls"`
	ModelRetries     int               `json:"model_retries"`
	Usage            map[string]int    `json:"usage"`
	Strategy         string            `json:"strategy,omitempty"`
	ExecutorID       string            `json:"executor_id,omitempty"`
	ApprovalMode     string            `json:"approval_mode"`
	ID               string            `json:"id"`
	OwnerID          string            `json:"owner_id"`
	ProjectID        string            `json:"project_id"`
	Name             string            `json:"name"`
	Goal             string            `json:"goal"`
	Scope            string            `json:"scope"`
	Material         string            `json:"material"`
	Model            string            `json:"model"`
	Tools            []string          `json:"tools"`
	Concurrency      int               `json:"concurrency"`
	TokenBudget      int               `json:"token_budget"`
	Tokens           int               `json:"tokens"` // actual usage, or conservative charge if usage is absent
	Reserved         int               `json:"reserved_tokens"`
	RemainingSeconds int64             `json:"remaining_seconds"`
	Deadline         int64             `json:"deadline"`
	MaxRounds        int               `json:"max_rounds"`
	Rounds           int               `json:"rounds"`
	Status           string            `json:"status"`
	Reason           string            `json:"reason"`
	Summary          string            `json:"summary"`
	Steps            []*Step           `json:"steps"`
	CreatedAt        time.Time         `json:"created_at"`
	UpdatedAt        time.Time         `json:"updated_at"`
}

type Step struct {
	ToolCalls          int                 `json:"tool_calls"`
	RetryCount         int                 `json:"retry_count"`
	Profile            string              `json:"profile,omitempty"`
	VerifyOf           string              `json:"verify_of,omitempty"`
	ToolNames          []string            `json:"tools,omitempty"`
	FailureKind        string              `json:"failure_kind,omitempty"`
	Attempts           []Attempt           `json:"attempts,omitempty"`
	Suppressed         int                 `json:"suppressed_actions"`
	ApprovalID         string              `json:"approval_id,omitempty"`
	ApprovalDigest     string              `json:"approval_digest,omitempty"`
	ApprovalKind       string              `json:"approval_kind,omitempty"`
	ApprovedBy         string              `json:"approved_by,omitempty"`
	ApprovedExecutorID string              `json:"approved_executor_id,omitempty"`
	SeenCallIDs        []string            `json:"seen_call_ids,omitempty"`
	Checkpoint         string              `json:"checkpoint,omitempty"`
	RetrievedIDs       []string            `json:"retrieved_execution_ids,omitempty"`
	RetiredReason      string              `json:"retired_reason,omitempty"`
	ID                 string              `json:"id"`
	Key                string              `json:"key"`
	Title              string              `json:"title"`
	Instruction        string              `json:"instruction"`
	DependsOn          []string            `json:"depends_on"`
	Status             string              `json:"status"`
	Turns              int                 `json:"turns"`
	Result             string              `json:"result"`
	Evidence           []Evidence          `json:"evidence"`
	Messages           []agent.ChatMessage `json:"-"`
	// Trace is private model state stored in the payload, excluded by Public().
	Trace         json.RawMessage `json:"trace,omitempty"`
	Pending       *agent.ToolCall `json:"pending,omitempty"`
	Approved      bool            `json:"approved"`
	ExecutionID   string          `json:"execution_id,omitempty"`
	ExecutionIDs  []string        `json:"execution_ids"`
	SuccessfulIDs []string        `json:"successful_execution_ids"`
}

type ObservedAsset struct {
	Host     string `json:"host"`
	IP       string `json:"ip"`
	Domain   string `json:"domain"`
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
	Server   string `json:"server"`
}

type Evidence struct {
	Revision     string         `json:"revision,omitempty"`
	Asset        *ObservedAsset `json:"asset,omitempty"`
	Kind         string         `json:"kind"` // fact | finding | asset | gap
	Title        string         `json:"title"`
	Detail       string         `json:"detail"`
	ExecutionIDs []string       `json:"execution_ids"`
}

type Event struct {
	ID        int64           `json:"id"`
	StepID    string          `json:"step_id"`
	Kind      string          `json:"kind"`
	Message   string          `json:"message"`
	Data      json.RawMessage `json:"data"`
	CreatedAt string          `json:"created_at"`
}

func (t *Task) Validate() error {
	t.Name, t.Goal, t.Scope = strings.TrimSpace(t.Name), strings.TrimSpace(t.Goal), strings.TrimSpace(t.Scope)
	if t.Name == "" || t.Goal == "" || t.Scope == "" {
		return fmt.Errorf("name, goal and scope are required")
	}
	if len(t.Name) > 200 || len(t.Goal) > 12000 || len(t.Scope) > 12000 || len(t.Material) > 24000 {
		return fmt.Errorf("assessment input is too long")
	}
	if t.Concurrency == 0 {
		t.Concurrency = 2
	}
	if t.Concurrency < 1 || t.Concurrency > 4 {
		return fmt.Errorf("concurrency must be 1..4")
	}
	if t.TokenBudget == 0 {
		t.TokenBudget = 200000
	}
	if t.TokenBudget < 4000 || t.TokenBudget > 10000000 {
		return fmt.Errorf("token_budget must be 4000..10000000")
	}
	if t.RemainingSeconds == 0 {
		t.RemainingSeconds = 7200
	}
	if t.RemainingSeconds < 60 || t.RemainingSeconds > 86400 {
		return fmt.Errorf("remaining_seconds must be 60..86400")
	}
	if t.MaxRounds == 0 {
		t.MaxRounds = 12
	}
	if t.MaxRounds < 1 || t.MaxRounds > 50 {
		return fmt.Errorf("max_rounds must be 1..50")
	}
	if len(t.Tools) == 0 || len(t.Tools) > 100 {
		return fmt.Errorf("select 1..100 tools")
	}
	if t.ApprovalMode == "" {
		t.ApprovalMode = "policy"
	}
	if t.ApprovalMode != "policy" && t.ApprovalMode != "manual" {
		return fmt.Errorf("approval_mode must be policy or manual")
	}
	t.Criteria = criterionText(t.Criteria)
	defaultCriterion := len(t.Criteria) == 0
	if len(t.Criteria) == 0 {
		t.Criteria = []string{t.Goal}
	}
	if len(t.Criteria) > 12 {
		return fmt.Errorf("at most 12 acceptance criteria")
	}
	for _, c := range t.Criteria {
		if len(c) > 2000 && !defaultCriterion {
			return fmt.Errorf("acceptance criterion too long")
		}
	}
	if len(t.SkillIDs) > 4 {
		return fmt.Errorf("at most four selected skills")
	}
	for p := range t.WorkerModels {
		if !validProfile(p) {
			return fmt.Errorf("unknown worker profile %q", p)
		}
	}
	t.ID = uuid.NewString()
	t.Status = "draft"
	t.Steps = []*Step{}
	t.CreatedAt = time.Now().UTC()
	t.UpdatedAt = t.CreatedAt
	return nil
}

func (t *Task) Step(id string) *Step {
	for _, s := range t.Steps {
		if s.ID == id {
			return s
		}
	}
	return nil
}
func (t *Task) Public() *Task {
	b, _ := json.Marshal(t)
	var out Task
	_ = json.Unmarshal(b, &out)
	for _, s := range out.Steps {
		s.Trace = nil
		for i := range s.Evidence {
			s.Evidence[i].Revision = EvidenceRevision(s.Evidence[i])
		}
	}
	return &out
}
func (t *Task) Terminal() bool {
	return t.Status == "completed" || t.Status == "stopped" || t.Status == "exhausted" || t.Status == "failed"
}
func clip(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "\n[truncated; see execution log]"
	}
	return s
}
