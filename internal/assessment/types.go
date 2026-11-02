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
	ApprovalMode     string    `json:"approval_mode"`
	ID               string    `json:"id"`
	OwnerID          string    `json:"owner_id"`
	ProjectID        string    `json:"project_id"`
	Name             string    `json:"name"`
	Goal             string    `json:"goal"`
	Scope            string    `json:"scope"`
	Material         string    `json:"material"`
	Model            string    `json:"model"`
	Tools            []string  `json:"tools"`
	Concurrency      int       `json:"concurrency"`
	TokenBudget      int       `json:"token_budget"`
	Tokens           int       `json:"tokens"` // actual usage, or conservative charge if usage is absent
	Reserved         int       `json:"reserved_tokens"`
	RemainingSeconds int64     `json:"remaining_seconds"`
	Deadline         int64     `json:"deadline"`
	MaxRounds        int       `json:"max_rounds"`
	Rounds           int       `json:"rounds"`
	Status           string    `json:"status"`
	Reason           string    `json:"reason"`
	Summary          string    `json:"summary"`
	Steps            []*Step   `json:"steps"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type Step struct {
	ID          string              `json:"id"`
	Key         string              `json:"key"`
	Title       string              `json:"title"`
	Instruction string              `json:"instruction"`
	DependsOn   []string            `json:"depends_on"`
	Status      string              `json:"status"`
	Turns       int                 `json:"turns"`
	Result      string              `json:"result"`
	Evidence    []Evidence          `json:"evidence"`
	Messages    []agent.ChatMessage `json:"-"`
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
