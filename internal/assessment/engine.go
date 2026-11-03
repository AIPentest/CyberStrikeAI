package assessment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/runlease"
	"cyberstrike-ai/internal/security"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

var ErrBudget = errors.New("assessment budget exhausted")

type ModelReply struct {
	Message agent.ChatMessage
	Tokens  int
}

// Runtime adapters retain CSAI model configuration and MCP authorization/guardrails.
type Runtime interface {
	Context(context.Context, *Task) (context.Context, error)
	Tools(*Task) ([]agent.Tool, error)
	Complete(context.Context, *Task, []agent.ChatMessage, []agent.Tool, int) (*ModelReply, error)
	Execute(context.Context, *Task, string, agent.ToolCall) (*agent.ToolExecutionResult, error)
}
type activeRun struct {
	cancel context.CancelFunc
	done   chan struct{}
}
type Engine struct {
	Store   *Store
	Runtime Runtime
	log     *zap.Logger
	mu      sync.Mutex
	active  map[string]*activeRun
	closed  bool
}

func NewEngine(s *Store, r Runtime, log *zap.Logger) *Engine {
	if log == nil {
		log = zap.NewNop()
	}
	return &Engine{Store: s, Runtime: r, log: log, active: map[string]*activeRun{}}
}
func (e *Engine) Start(id string) error { return e.StartAs(id, "") }

// StartAs binds execution to the authenticated operator, never the potentially
// more privileged resource owner when an assigned collaborator resumes a task.
func (e *Engine) StartAs(id, actorID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed || e.active[id] != nil || len(e.active) >= 4 {
		return ErrConflict
	}
	t, err := e.Store.Get(id)
	if err != nil {
		return err
	}
	if actorID == "" {
		actorID = t.OwnerID
	}
	t.ExecutorID = actorID
	ctx, err := e.Runtime.Context(context.Background(), t)
	if err != nil {
		return err
	}
	if _, err = e.Runtime.Tools(t); err != nil {
		return err
	}
	if err = e.Store.Change(id, "", "start", "Assessment started", func(t *Task) error {
		if t.Status != "draft" && t.Status != "paused" && t.Status != "blocked" && t.Status != "partial" && t.Status != "failed" {
			return ErrConflict
		}
		if t.Tokens+t.Reserved >= t.TokenBudget || t.RemainingSeconds <= 0 || t.Rounds >= t.MaxRounds {
			return ErrBudget
		}
		t.Status = "running"
		for _, s := range t.Steps {
			if s.Status == "awaiting_approval" && s.Pending != nil {
				t.ExecutorID = actorID
				requestApproval(t, s, s.Pending)
			} else if s.Status == "pending" {
				clearApproval(s)
			}
		}
		t.ExecutorID = actorID
		t.Reason = ""
		t.FailureKind = ""
		if t.Review != nil {
			t.PreviousReview = t.Review
		}
		t.Review = nil
		t.Deadline = time.Now().Unix() + t.RemainingSeconds
		return nil
	}); err != nil {
		return err
	}
	ctx, cancel := context.WithDeadline(ctx, time.Unix(time.Now().Unix()+t.RemainingSeconds, 0))
	scope := security.NewProcessScope()
	workers := runlease.New()
	ctx = runlease.WithScope(security.WithProcessScope(ctx, scope), workers)
	closeAdmission := func() { scope.Seal(); workers.Cancel(); cancel() }
	stopDeadlineWatch := context.AfterFunc(ctx, func() { scope.Seal(); workers.Cancel() })
	run := &activeRun{cancel: closeAdmission, done: make(chan struct{})}
	e.active[id] = run
	go func() {
		defer func() {
			stopDeadlineWatch()
			closeAdmission()
			e.mu.Lock()
			delete(e.active, id)
			close(run.done)
			e.mu.Unlock()
		}()
		// Isolate and reap local subprocesses even if an MCP tool detaches cancellation.
		err := func() (err error) {
			defer func() {
				if p := recover(); p != nil {
					err = fmt.Errorf("assessment runtime panic: %v", p)
				}
			}()
			return e.run(ctx, id)
		}()
		workers.Cancel()
		scope.Seal()
		cleanupErr := scope.Close()
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		workerErr := workers.Wait(cleanupCtx)
		cleanupCancel()
		cleanupErr = errors.Join(cleanupErr, workerErr)
		if cleanupErr != nil {
			err = fmt.Errorf("process cleanup failed: %w", cleanupErr)
		}
		if errors.Is(err, ErrBudget) && ctx.Err() == nil && cleanupErr == nil {
			if reviewErr := e.review(ctx, id, true); reviewErr != nil {
				e.log.Info("partial assessment review unavailable", zap.Error(reviewErr))
			}
		}
		changeErr := e.Store.Change(id, "", "settled", "Assessment run settled", func(t *Task) error {
			settleTime(t)
			switch t.Status {
			case "pausing":
				t.Status = "paused"
			case "stopping":
				t.Status = "stopped"
			case "running", "awaiting_approval":
				if errors.Is(err, ErrBudget) || errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
					t.Status = "exhausted"
					t.Reason = "budget_exhausted"
				} else if err != nil {
					t.Status = "failed"
					t.Reason = clip(err.Error(), 2000)
					t.FailureKind = failureKind(err)
				} else {
					t.Status = "blocked"
					t.Reason = "no_runnable_work"
				}
			}
			if cleanupErr != nil {
				t.Status = "failed"
				t.Reason = cleanupErr.Error()
			}
			for _, s := range t.Steps {
				if s.Status == "running" {
					interrupt(s, "Execution interrupted; review logs before explicit retry.")
				}
			}
			return nil
		})
		if changeErr != nil {
			e.log.Error("assessment settlement failed", zap.String("id", id), zap.Error(changeErr))
		}
	}()
	return nil
}
func (e *Engine) Control(id, action string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if action != "pause" && action != "stop" {
		return fmt.Errorf("unknown control action")
	}
	err := e.Store.Change(id, "", "control", action, func(t *Task) error {
		if t.Terminal() {
			return ErrConflict
		}
		if action == "pause" {
			if e.active[id] != nil {
				t.Status = "pausing"
			} else {
				t.Status = "paused"
				settleTime(t)
			}
		} else {
			if e.active[id] != nil {
				t.Status = "stopping"
			} else {
				t.Status = "stopped"
				settleTime(t)
			}
		}
		return nil
	})
	if err == nil && e.active[id] != nil {
		e.active[id].cancel()
	}
	return err
}
func (e *Engine) Close() {
	e.mu.Lock()
	e.closed = true
	var runs []*activeRun
	for id, r := range e.active {
		_ = e.Store.Change(id, "", "shutdown", "Server shutting down", func(t *Task) error {
			if t.Status == "running" || t.Status == "awaiting_approval" {
				t.Status = "pausing"
			}
			return nil
		})
		r.cancel()
		runs = append(runs, r)
	}
	e.mu.Unlock()
	for _, r := range runs {
		<-r.done
	}
}
func (e *Engine) Decide(id, step, approvalID, reviewerID string, approved bool) error {
	event := map[string]any{"approval_id": approvalID, "reviewer_id": reviewerID, "approved": approved}
	return e.Store.Change(id, step, "approval", fmt.Sprintf("approved=%t", approved), func(t *Task) error {
		s := t.Step(step)
		if s == nil || s.Status != "awaiting_approval" || s.Pending == nil || approvalID == "" || approvalID != s.ApprovalID || reviewerID == "" || s.ApprovalDigest != callDigest(*s.Pending) {
			return ErrConflict
		}
		if t.Terminal() || t.Status == "pausing" || t.Status == "stopping" {
			return ErrConflict
		}
		event["call_digest"] = s.ApprovalDigest
		event["executor_id"] = s.ApprovedExecutorID
		if approved {
			s.Approved = true
			s.ApprovalKind = "manual"
			s.ApprovedBy = reviewerID
			s.Status = "pending"
		} else {
			s.Status = "blocked"
			s.Result = "Tool execution denied by reviewer"
			s.Pending = nil
			clearApproval(s)
		}
		return nil
	}, event)
}
func (e *Engine) Retry(id, step string) error {
	return e.Store.Change(id, step, "retry", "Explicit retry requested; prior evidence retained in events", func(t *Task) error {
		if !contains([]string{"paused", "blocked", "draft", "partial", "failed", "exhausted"}, t.Status) {
			return ErrConflict
		}
		s := t.Step(step)
		if s == nil || s.Status != "blocked" {
			return ErrConflict
		}
		s.Status = "pending"
		s.RetryCount++
		if (s.FailureKind != "model_transient" && s.FailureKind != "model_auth") || len(s.Trace) == 0 {
			s.Trace = nil
			s.Turns = 0
		}
		if t.Status == "failed" || t.Status == "partial" {
			t.Status = "paused"
		}
		s.FailureKind = ""
		s.Pending = nil
		clearApproval(s)
		s.Result = ""
		return nil
	})
}
func dependenciesReady(t *Task, s *Step) bool {
	for _, key := range s.DependsOn {
		found := false
		for _, d := range t.Steps {
			if d.Key == key && d.Status == "completed" {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
func hasLiveWork(t *Task) bool {
	for _, s := range t.Steps {
		if s.Status == "running" || s.Status == "awaiting_approval" || (s.Status == "pending" && dependenciesReady(t, s)) {
			return true
		}
	}
	return false
}
func planningRevision(t *Task) string {
	var b strings.Builder
	for _, s := range t.Steps {
		if s.Status == "running" && len(s.Evidence) > 0 && s.Checkpoint != "" {
			fmt.Fprintf(&b, "checkpoint:%s:%d:%s;", s.ID, len(s.Evidence), s.Checkpoint)
		}
		if s.Status == "completed" || s.Status == "blocked" || s.Status == "skipped" {
			fmt.Fprintf(&b, "%s:%s:%d;", s.ID, s.Status, s.Turns)
		}
	}
	return b.String()
}

// Refill each free slot as soon as a worker settles; a slow independent branch
// must not delay the next dependent step or planning from a fresh discovery.
func (e *Engine) run(ctx context.Context, id string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	type completion struct {
		id  string
		err error
	}
	done := make(chan completion, 4)
	planDone := make(chan error, 1)
	planning := false
	active := map[string]bool{}
	var wg sync.WaitGroup
	defer func() { cancel(); wg.Wait() }()
	lastPlan := "unplanned"
	collect := func(c completion) error {
		delete(active, c.id)
		if errors.Is(c.err, ErrBudget) || ctx.Err() != nil {
			return c.err
		}
		return nil
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
	drain:
		for {
			select {
			case c := <-done:
				if err := collect(c); err != nil {
					return err
				}
			default:
				break drain
			}
		}
		t, err := e.Store.Get(id)
		if err != nil {
			return err
		}
		if t.Status != "running" && t.Status != "awaiting_approval" {
			return nil
		}
		for _, s := range t.Steps {
			if len(active) >= t.Concurrency {
				break
			}
			if s.Status != "pending" || active[s.ID] || !dependenciesReady(t, s) {
				continue
			}
			active[s.ID] = true
			wg.Add(1)
			go func(sid string) { defer wg.Done(); done <- completion{sid, e.work(ctx, id, sid)} }(s.ID)
		}
		waiting := false
		for _, s := range t.Steps {
			if s.Status == "awaiting_approval" {
				waiting = true
			}
		}
		revision := planningRevision(t)
		// Only external progress wakes planning. Approval polling never spends tokens.
		if !planning && len(active) < t.Concurrency && !waiting && (revision != lastPlan) {
			if t.Rounds >= t.MaxRounds || len(t.Steps) >= MaxSteps {
				if len(active) == 0 {
					return ErrBudget
				}
			} else {
				lastPlan = revision
				planning = true
				wg.Add(1)
				go func() {
					defer wg.Done()
					var planErr error
					defer func() {
						if p := recover(); p != nil {
							planErr = fmt.Errorf("planner panic: %v", p)
						}
						planDone <- planErr
					}()
					planErr = e.plan(ctx, id)
				}()

			}
		}
		if len(active) == 0 && !waiting && !planning {
			return nil
		}
		if waiting && len(active) == 0 && t.Status != "awaiting_approval" {
			if err = e.Store.Change(id, "", "waiting", "Waiting for tool approval", func(t *Task) error {
				if t.Status == "running" {
					t.Status = "awaiting_approval"
				}
				return nil
			}); err != nil {
				return err
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-planDone:
			planning = false
			if err != nil {
				return err
			}
		case c := <-done:
			if err := collect(c); err != nil {
				return err
			}
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// call reserves a conservative UTF-8 byte upper bound plus output allowance before
// making a request. Parallel workers cannot independently spend the same budget.
func (e *Engine) callOnce(ctx context.Context, id string, msgs []agent.ChatMessage, tools []agent.Tool) (*ModelReply, error) {
	input, _ := json.Marshal(struct {
		Messages []agent.ChatMessage
		Tools    []agent.Tool
	}{msgs, tools})
	reserve := len(input) + 512 + 4096
	busy := errors.New("token budget temporarily reserved")
	var err error
	for {
		err = e.Store.Change(id, "", "", "", func(t *Task) error {
			if t.Status != "running" && t.Status != "awaiting_approval" {
				return ErrConflict
			}
			limit := t.TokenBudget
			if p := requestPurpose(ctx); p.Phase != "review" && p.Phase != "settlement" {
				limit -= reserveForReview(t)
			}
			if t.Tokens+reserve > limit {
				return ErrBudget
			}
			if t.Tokens+t.Reserved+reserve > limit {
				return busy
			}
			t.Reserved += reserve
			return nil
		})
		if !errors.Is(err, busy) {
			break
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	if err != nil {
		return nil, err
	}
	t, err := e.Store.Get(id)
	if err != nil {
		return nil, err
	}
	reply, callErr := e.Runtime.Complete(ctx, t, msgs, tools, 4096)
	charge := reserve
	if reply != nil && reply.Tokens > 0 {
		charge = reply.Tokens
	}
	err = e.Store.Change(id, "", "usage", "Model usage charged", func(t *Task) error {
		t.Reserved -= reserve
		t.Tokens += charge
		t.ModelCalls++
		if t.Usage == nil {
			t.Usage = map[string]int{}
		}
		p := requestPurpose(ctx)
		t.Usage[p.Phase+":"+p.Profile] += charge
		return nil
	})
	if err != nil {
		return nil, err
	}
	if callErr != nil {
		return nil, callErr
	}
	if reply == nil {
		return nil, fmt.Errorf("empty model response")
	}
	// A charged final response still contains usable evidence. Budget checks
	// prevent the next request; never discard this already-paid result.
	return reply, nil
}

type planOutput struct {
	Strategy string `json:"strategy"`
	Retire   []struct {
		Key    string `json:"key"`
		Reason string `json:"reason"`
	} `json:"retire"`
	Status  string `json:"status"`
	Summary string `json:"summary"`
	Steps   []struct {
		Key         string   `json:"key"`
		Title       string   `json:"title"`
		Instruction string   `json:"instruction"`
		DependsOn   []string `json:"depends_on"`
		Profile     string   `json:"profile"`
		VerifyOf    string   `json:"verify_of"`
		Tools       []string `json:"tools"`
	} `json:"steps"`
}

func decodeObject(s string, out any) error {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	}
	return json.Unmarshal([]byte(s), out)
}
func (e *Engine) plan(ctx context.Context, id string) error {
	t, err := e.Store.Get(id)
	if err != nil {
		return err
	}
	msgs := []agent.ChatMessage{{Role: "system", Content: `You plan an authorized security assessment. You have only a task-scoped memory reader, not external execution tools. Search and read worker observations when a direction is blocked or detailed evidence is needed. Use only recorded facts; never claim a test ran without tool evidence. Respect scope and exclusions. Do not repeat completed/blocked directions under new keys. Propose small concrete tasks, at most 4 per round. Dependencies reference earlier step keys only. Maintain a persistent strategy describing the remaining roadmap and dependencies. Address gaps from previous_review before proposing completion; reuse completed observations instead of blindly repeating them. Retire obsolete pending or blocked steps with an explicit reason when an alternative route supersedes them. Never retire running, awaiting-approval or completed steps. Do not reissue an interrupted operation; propose a distinct alternative or leave the gap for explicit human retry. Stop with blocked when no useful untried direction remains. completed means the stated goal is supported by recorded execution evidence, not merely that the plan ran. Return ONLY JSON: {"status":"continue|completed|blocked","summary":"progress and remaining gaps","strategy":"remaining roadmap","retire":[{"key":"obsolete-step","reason":"why superseded; remaining coverage gap"}],"steps":[{"key":"unique stable key","title":"short title","instruction":"bounded action and required evidence","depends_on":["earlier-key"],"profile":"general|recon|auth|validation|verification","tools":["subset of selected tools"],"verify_of":"optional finding source key"}]}. Use specialized profiles and narrow tool subsets for each task. Candidate findings get independent verification steps before final review. Use the user's language.`}, {Role: "user", Content: taskContext(t)}}
	var r *ModelReply
	for reads := 0; ; reads++ {
		tools := assessmentTools(false)
		if reads >= 3 {
			tools = nil
		}
		r, err = e.call(ctx, id, msgs, tools)
		if err != nil {
			return err
		}
		if len(r.Message.ToolCalls) == 0 {
			break
		}
		if reads >= 3 {
			return fmt.Errorf("planner memory-read budget exhausted")
		}
		if err = validateToolCalls(msgs, r.Message.ToolCalls, nil); err != nil {
			return err
		}
		msgs = append(msgs, r.Message)
		for _, call := range r.Message.ToolCalls {
			content := "Only assessment_memory is available to the planner"
			if call.Function.Name == memoryTool {
				var readErr error
				content, readErr = e.internalTool(ctx, id, "", call)
				if readErr != nil {
					content = "Memory operation error: " + readErr.Error()
				}
			}
			msgs = append(msgs, agent.ChatMessage{Role: "tool", ToolCallID: call.ID, ToolName: call.Function.Name, Content: content})
		}
	}
	var p planOutput
	_, err = e.repairJSON(ctx, id, msgs, r, func(content string) error {
		p = planOutput{}
		if err := decodeObject(content, &p); err != nil {
			return fmt.Errorf("invalid planner JSON: %w", err)
		}
		if p.Status != "continue" && p.Status != "completed" && p.Status != "blocked" {
			return fmt.Errorf("invalid planner status")
		}
		if len(p.Steps) > 4 || len(p.Retire) > MaxSteps {
			return fmt.Errorf("planner proposed too many changes")
		}
		return validatePlan(t, p)
	})
	if err != nil {
		return err
	}
	needsReview := false
	forcePartial := false
	err = e.Store.Change(id, "", "plan", clip(p.Summary, 4000), func(t *Task) error {
		if t.Status != "running" && t.Status != "awaiting_approval" {
			return ErrConflict
		}
		t.Rounds++
		t.Summary = clip(p.Summary, 8000)
		if strings.TrimSpace(p.Strategy) != "" {
			t.Strategy = clip(p.Strategy, 8000)
		}
		t.Status = "running"
		for _, r := range p.Retire {
			if strings.TrimSpace(r.Reason) == "" {
				return fmt.Errorf("retiring a direction requires a reason")
			}
			found := false
			for _, st := range t.Steps {
				if st.Key == r.Key {
					found = true
					if st.Status != "pending" && st.Status != "blocked" && st.Status != "skipped" {
						continue
					}
					st.Status = "skipped"
					st.RetiredReason = clip(r.Reason, 2000)
				}
			}
			if !found {
				return fmt.Errorf("unknown retired direction %q", r.Key)
			}
		}
		keys := map[string]bool{}
		signatures := map[string]bool{}
		for _, s := range t.Steps {
			keys[s.Key] = true
			signatures[directionSignature(s.Title, s.Instruction)] = true
		}
		added := 0
		if p.Status == "continue" {
			for _, n := range p.Steps {
				key := strings.ToLower(strings.TrimSpace(n.Key))
				if key == "" || len(key) > 120 || n.Title == "" || n.Instruction == "" {
					return fmt.Errorf("invalid plan step")
				}
				if keys[key] || signatures[directionSignature(n.Title, n.Instruction)] {
					continue
				}
				for _, dep := range n.DependsOn {
					if !keys[dep] {
						return fmt.Errorf("unknown or forward dependency %q", dep)
					}
				}
				if len(t.Steps) >= MaxSteps {
					return ErrBudget
				}
				t.Steps = append(t.Steps, &Step{ID: uuid.NewString(), Key: key, Title: clip(n.Title, 200), Instruction: clip(n.Instruction, 8000), DependsOn: n.DependsOn, Profile: n.Profile, VerifyOf: n.VerifyOf, ToolNames: n.Tools, Status: "pending", Evidence: []Evidence{}})
				keys[key] = true
				signatures[directionSignature(n.Title, n.Instruction)] = true
				added++
			}
		}
		if p.Status == "completed" {
			ensureVerificationSteps(t)
			hasEvidence := false
			unresolved := false
			for _, s := range t.Steps {
				if s.Status != "completed" && s.Status != "skipped" {
					unresolved = true
				}
				for _, v := range s.Evidence {
					if v.Kind != "gap" && len(v.ExecutionIDs) > 0 {
						hasEvidence = true
					}
				}
			}
			if hasEvidence && !hasLiveWork(t) {
				forcePartial = unresolved
				needsReview = true
				t.Phase = "review"
			} else {
				if !hasLiveWork(t) {
					t.Status = "blocked"
					t.Reason = "completion_missing_evidence"
				}
			}
		}
		if (p.Status == "blocked" || (p.Status == "continue" && added == 0)) && !hasLiveWork(t) {
			t.Status = "blocked"
			t.Reason = "no_new_direction"
			for _, s := range t.Steps {
				if len(s.Evidence) > 0 {
					needsReview = true
					forcePartial = true
					t.Status = "running"
					break
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if needsReview {
		err := e.review(ctx, id, forcePartial)
		if err != nil && ctx.Err() == nil && !errors.Is(err, ErrBudget) {
			return e.Store.Change(id, "", "review_unavailable", "Independent review unavailable; results preserved", func(t *Task) error {
				if t.Status != "running" && t.Status != "awaiting_approval" {
					return ErrConflict
				}
				t.Status = "partial"
				t.Phase = ""
				t.Reason = "review_unavailable"
				t.FailureKind = failureKind(err)
				return nil
			})
		}
		return err
	}
	return nil
}

func directionSignature(title, instruction string) string {
	return strings.ToLower(strings.Join(strings.Fields(title+" "+instruction), " "))
}
func validatePlan(t *Task, p planOutput) error {
	keys := map[string]bool{}
	for _, s := range t.Steps {
		keys[s.Key] = true
	}
	for _, r := range p.Retire {
		if !keys[r.Key] || strings.TrimSpace(r.Reason) == "" {
			return fmt.Errorf("retire requires an existing key and reason")
		}
	}
	for _, n := range p.Steps {
		if n.Profile != "" && !validProfile(n.Profile) {
			return fmt.Errorf("invalid profile")
		}
		for _, tool := range n.Tools {
			if !contains(t.Tools, tool) {
				return fmt.Errorf("step tools must narrow the task allowlist")
			}
		}
		if n.VerifyOf != "" {
			if !keys[n.VerifyOf] || n.Profile != "verification" {
				return fmt.Errorf("verification needs a known source and verification profile")
			}
		}
		key := strings.ToLower(strings.TrimSpace(n.Key))
		if key == "" || len(key) > 120 || strings.TrimSpace(n.Title) == "" || strings.TrimSpace(n.Instruction) == "" {
			return fmt.Errorf("invalid plan step")
		}
		for _, dep := range n.DependsOn {
			if !keys[dep] {
				return fmt.Errorf("unknown or forward dependency %q", dep)
			}
		}
		keys[key] = true
	}
	return nil
}
