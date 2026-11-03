package assessment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cyberstrike-ai/internal/agent"
)

type autonomousFixture struct{ *fakeRuntime }

func (autonomousFixture) RequiresApproval(*Task, string) bool { return false }
func stepState(t *Task, key string) *Step {
	for _, s := range t.Steps {
		if s.Key == key {
			return s
		}
	}
	return nil
}
func planFixture(strategy string, steps ...map[string]any) *ModelReply {
	return reply(map[string]any{"status": "continue", "strategy": strategy, "steps": steps})
}
func TestLongRunRefillsSlotBeforeSlowSiblingCompletes(t *testing.T) {
	store, task := fixture(t)
	slowStarted := make(chan struct{})
	fastFollowup := make(chan struct{})
	releaseSlow := make(chan struct{})
	var maxActive, active atomic.Int32
	r := normalRuntime()
	r.complete = func(ctx context.Context, t *Task, m []agent.ChatMessage, tools []agent.Tool) (*ModelReply, error) {
		if strings.Contains(m[0].Content, "You plan an authorized") {
			if len(t.Steps) == 0 {
				return planFixture("fast -> followup, independent slow branch", map[string]any{"key": "fast", "title": "fast", "instruction": "fast"}, map[string]any{"key": "slow", "title": "slow", "instruction": "slow"}, map[string]any{"key": "next", "title": "next", "instruction": "next", "depends_on": []string{"fast"}}), nil
			}
			return reply(map[string]any{"status": "completed", "summary": "finished"}), nil
		}
		var key string
		for _, k := range []string{"fast", "slow", "next"} {
			if strings.Contains(m[1].Content, "YOUR TASK: "+k+"\n") {
				key = k
			}
		}
		if m[len(m)-1].Role == "tool" {
			return reply(map[string]any{"status": "completed", "evidence": []Evidence{{Kind: "fact", Title: key, ExecutionIDs: []string{"exec-" + key}}}}), nil
		}
		return &ModelReply{Message: agent.ChatMessage{Role: "assistant", ToolCalls: []agent.ToolCall{{ID: "call-" + key, Function: agent.FunctionCall{Name: "inspect", Arguments: map[string]interface{}{"key": key}}}}}, Tokens: 100}, nil
	}
	r.execute = func(ctx context.Context, _ *Task, _ string, c agent.ToolCall) (*agent.ToolExecutionResult, error) {
		n := active.Add(1)
		defer active.Add(-1)
		for {
			old := maxActive.Load()
			if n <= old || maxActive.CompareAndSwap(old, n) {
				break
			}
		}
		key := c.Function.Arguments["key"].(string)
		switch key {
		case "slow":
			close(slowStarted)
			select {
			case <-releaseSlow:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		case "fast":
			select {
			case <-slowStarted:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		case "next":
			close(fastFollowup)
		}
		return &agent.ToolExecutionResult{ExecutionID: "exec-" + key, Result: key}, nil
	}
	e := NewEngine(store, autonomousFixture{r}, nil)
	defer e.Close()
	if err := e.Start(task.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-fastFollowup:
	case <-time.After(5 * time.Second):
		t.Fatal("dependent work waited for unrelated slow sibling")
	}
	close(releaseSlow)
	got := waitTask(t, store, task.ID, func(t *Task) bool { return t.Status == "completed" && t.Deadline == 0 })
	if maxActive.Load() > 2 || got.Strategy == "" {
		t.Fatal("concurrency/strategy lost")
	}
}
func TestLongRunReplansAroundFailedBranch(t *testing.T) {
	store, task := fixture(t)
	r := normalRuntime()
	r.complete = func(ctx context.Context, t *Task, m []agent.ChatMessage, tools []agent.Tool) (*ModelReply, error) {
		if strings.Contains(m[0].Content, "You plan an authorized") {
			if len(t.Steps) == 0 {
				return planFixture("Try original route", map[string]any{"key": "original", "title": "original", "instruction": "original"}), nil
			}
			if stepState(t, "alternative") == nil {
				return reply(map[string]any{"status": "continue", "strategy": "Original route unavailable; use alternative", "retire": []map[string]string{{"key": "original", "reason": "Unavailable; alternative covers the goal, original gap remains"}}, "steps": []map[string]any{{"key": "alternative", "title": "alternative", "instruction": "alternative"}}}), nil
			}
			return reply(map[string]any{"status": "completed", "summary": "Alternative verified the goal"}), nil
		}
		if strings.Contains(m[1].Content, "YOUR TASK: original\n") {
			return reply(map[string]any{"status": "blocked", "summary": "Unavailable"}), nil
		}
		if m[len(m)-1].Role == "tool" {
			return reply(map[string]any{"status": "completed", "evidence": []Evidence{{Kind: "fact", Title: "Alternative observed", ExecutionIDs: []string{"exec-1"}}}}), nil
		}
		return &ModelReply{Message: agent.ChatMessage{Role: "assistant", ToolCalls: []agent.ToolCall{{ID: "call", Function: agent.FunctionCall{Name: "inspect", Arguments: map[string]interface{}{}}}}}, Tokens: 100}, nil
	}
	e := NewEngine(store, autonomousFixture{r}, nil)
	defer e.Close()
	if err := e.Start(task.ID); err != nil {
		t.Fatal(err)
	}
	got := waitTask(t, store, task.ID, func(t *Task) bool { return t.Status == "completed" && t.Deadline == 0 })
	if stepState(got, "original").Status != "skipped" || !strings.Contains(Report(got), "original gap remains") {
		t.Fatal("retired branch gap was hidden")
	}
	if r.calls.Load() != 1 {
		t.Fatal("failed branch blindly replayed")
	}
}
func TestMemoryReadsTailAndCrossWorkerProvenance(t *testing.T) {
	store, task := fixture(t)
	r := normalRuntime()
	e := NewEngine(store, r, nil)
	if err := store.Change(task.ID, "", "", "", func(t *Task) error {
		t.Status = "running"
		t.Steps = []*Step{{ID: "source", Key: "source", Status: "completed", SuccessfulIDs: []string{"exec-source"}}, {ID: "reader", Key: "reader", Status: "running"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Event(task.ID, "source", "tool_result", "preview", map[string]any{"output": strings.Repeat("x", 30000) + "TAIL_SECRET_FIXTURE", "execution_id": "exec-source"}); err != nil {
		t.Fatal(err)
	}
	invoke := func(args map[string]interface{}) (string, error) {
		return e.internalTool(context.Background(), task.ID, "reader", agent.ToolCall{Function: agent.FunctionCall{Name: memoryTool, Arguments: args}})
	}
	text, err := invoke(map[string]interface{}{"query": "TAIL_SECRET_FIXTURE"})
	if err != nil {
		t.Fatal(err)
	}
	var found struct {
		Observations []struct {
			ID int64 `json:"event_id"`
		} `json:"observations"`
	}
	if err = json.Unmarshal([]byte(text), &found); err != nil || len(found.Observations) != 1 {
		t.Fatal("tail not searchable", text, err)
	}
	text, err = invoke(map[string]interface{}{"event_id": found.Observations[0].ID, "offset": 30000})
	if err != nil || !strings.Contains(text, "TAIL_SECRET_FIXTURE") {
		t.Fatal("tail not retrievable", text, err)
	}
	current, _ := store.Get(task.ID)
	evidence := []Evidence{{Kind: "fact", Title: "Reused observation", ExecutionIDs: []string{"exec-source"}}}
	if err = validateEvidence(current, current.Step("reader"), evidence); err != nil {
		t.Fatal(err)
	}
	// Same execution label in another assessment must never be readable by ID.
	other := &Task{Name: "other", Goal: "other", Scope: "other", Tools: []string{"inspect"}}
	other.Validate()
	store.Create(other)
	store.Event(other.ID, "source", "tool_result", "foreign", nil)
	events, _ := store.Events(other.ID, 0)
	if _, err = invoke(map[string]interface{}{"event_id": events[0].ID}); err == nil {
		t.Fatal("cross-assessment memory leak")
	}
}
func TestCheckpointSurvivesInterruptAndCannotForgeEvidence(t *testing.T) {
	store, task := fixture(t)
	e := NewEngine(store, normalRuntime(), nil)
	store.Change(task.ID, "", "", "", func(t *Task) error {
		t.Status = "running"
		t.Steps = []*Step{{ID: "worker", Status: "running", SuccessfulIDs: []string{"exec"}}}
		return nil
	})
	invoke := func(ref string) error {
		_, err := e.internalTool(context.Background(), task.ID, "worker", agent.ToolCall{Function: agent.FunctionCall{Name: checkpointTool, Arguments: map[string]interface{}{"summary": "partial breakthrough", "evidence": []Evidence{{Kind: "fact", Title: "Partial", ExecutionIDs: []string{ref}}}}}})
		return err
	}
	if err := invoke("invented"); err == nil {
		t.Fatal("forged checkpoint accepted")
	}
	if err := invoke("exec"); err != nil {
		t.Fatal(err)
	}
	if err := store.Recover(); err != nil {
		t.Fatal(err)
	}
	got, _ := store.Get(task.ID)
	if got.Step("worker").Checkpoint != "partial breakthrough" || len(got.Step("worker").Evidence) != 1 {
		t.Fatal("partial progress lost on restart")
	}
}
func TestMalformedPlannerResponseRepairsWithoutExternalActions(t *testing.T) {
	store, task := fixture(t)
	var calls atomic.Int32
	r := &fakeRuntime{complete: func(_ context.Context, _ *Task, _ []agent.ChatMessage, tools []agent.Tool) (*ModelReply, error) {
		if calls.Add(1) == 1 {
			return &ModelReply{Message: agent.ChatMessage{Role: "assistant", Content: "not json"}, Tokens: 100}, nil
		}
		if len(tools) != 0 {
			t.Error("repair must have no tools")
		}
		return reply(map[string]string{"status": "blocked", "summary": "Need material"}), nil
	}}
	e := NewEngine(store, r, nil)
	defer e.Close()
	e.Start(task.ID)
	got := waitTask(t, store, task.ID, func(t *Task) bool { return t.Status == "blocked" && t.Deadline == 0 })
	if got.Tokens != 200 || calls.Load() != 2 || r.calls.Load() != 0 {
		t.Fatal("repair was not bounded/accounted")
	}
}
func TestContextKeepsLateDiscoveriesAndAllDirectionStates(t *testing.T) {
	task := &Task{Goal: "goal", Scope: "scope", Strategy: "roadmap"}
	for i := 0; i < 100; i++ {
		task.Steps = append(task.Steps, &Step{Key: fmt.Sprintf("s%d", i), Status: "completed", Evidence: []Evidence{{Detail: strings.Repeat("old", 2000)}}})
	}
	task.Steps[99].Evidence = []Evidence{{Title: "LATEST_DISCOVERY", Detail: "next route"}}
	text := taskContext(task)
	if !json.Valid([]byte(text)) || !strings.Contains(text, "LATEST_DISCOVERY") || !strings.Contains(text, `"key":"s0"`) || !strings.Contains(text, `"key":"s99"`) {
		t.Fatal("context truncated critical state")
	}
}

func TestWorkerReusesMemoryAndCheckpointsWithoutExternalReplay(t *testing.T) {
	store, task := fixture(t)
	store.Change(task.ID, "", "", "", func(t *Task) error {
		t.Steps = []*Step{{ID: "source", Key: "source", Title: "Source", Status: "completed", SuccessfulIDs: []string{"exec-source"}, Evidence: []Evidence{{Kind: "fact", Title: "Source", ExecutionIDs: []string{"exec-source"}}}}, {ID: "reader", Key: "reader", Title: "Reader", Instruction: "Reuse source observation", Status: "pending"}}
		return nil
	})
	store.Event(task.ID, "source", "tool_result", "source observation", map[string]any{"output": "CHAIN_SECRET_FIXTURE", "execution_id": "exec-source"})
	var turns atomic.Int32
	r := &fakeRuntime{complete: func(_ context.Context, _ *Task, m []agent.ChatMessage, _ []agent.Tool) (*ModelReply, error) {
		if strings.Contains(m[0].Content, "You plan an authorized") {
			return reply(map[string]string{"status": "completed", "summary": "Cross-worker evidence reused"}), nil
		}
		n := turns.Add(1)
		call := agent.ToolCall{ID: fmt.Sprintf("memory-%d", n), Function: agent.FunctionCall{Name: memoryTool, Arguments: map[string]interface{}{"query": "CHAIN_SECRET_FIXTURE"}}}
		if n == 2 {
			var hits struct {
				Rows []struct {
					ID int64 `json:"event_id"`
				} `json:"observations"`
			}
			if err := json.Unmarshal([]byte(m[len(m)-1].Content), &hits); err != nil || len(hits.Rows) == 0 {
				return nil, fmt.Errorf("missing search result: %s", m[len(m)-1].Content)
			}
			call.Function.Arguments = map[string]interface{}{"event_id": hits.Rows[0].ID}
		}
		if n == 3 {
			if !strings.Contains(m[len(m)-1].Content, "CHAIN_SECRET_FIXTURE") {
				return nil, fmt.Errorf("observation not retrieved")
			}
			call.Function.Name = checkpointTool
			call.Function.Arguments = map[string]interface{}{"summary": "Durable chain progress", "evidence": []Evidence{{Kind: "fact", Title: "Reused", ExecutionIDs: []string{"exec-source"}}}}
		}
		if n >= 4 {
			return reply(map[string]any{"status": "completed", "summary": "Existing proof sufficient", "evidence": []Evidence{{Kind: "fact", Title: "Reused", ExecutionIDs: []string{"exec-source"}}}}), nil
		}
		return &ModelReply{Message: agent.ChatMessage{Role: "assistant", ToolCalls: []agent.ToolCall{call}}, Tokens: 100}, nil
	}}
	e := NewEngine(store, r, nil)
	defer e.Close()
	if err := e.Start(task.ID); err != nil {
		t.Fatal(err)
	}
	got := waitTask(t, store, task.ID, func(t *Task) bool { return t.Status == "completed" && t.Deadline == 0 })
	if r.calls.Load() != 0 || got.Step("reader").Checkpoint != "Durable chain progress" || !contains(got.Step("reader").RetrievedIDs, "exec-source") {
		t.Fatal("memory bridge/checkpoint failed")
	}
}
func TestPaidFinalResponseIsNotLostAtBudgetBoundary(t *testing.T) {
	store, task := fixture(t)
	store.Change(task.ID, "", "", "", func(t *Task) error { t.Status = "running"; t.TokenBudget = 10000; return nil })
	r := &fakeRuntime{complete: func(context.Context, *Task, []agent.ChatMessage, []agent.Tool) (*ModelReply, error) {
		return &ModelReply{Message: agent.ChatMessage{Role: "assistant", Content: "valuable final evidence"}, Tokens: 10000}, nil
	}}
	e := NewEngine(store, r, nil)
	response, err := e.call(context.Background(), task.ID, []agent.ChatMessage{{Role: "user", Content: "fixture"}}, nil)
	if err != nil || response.Message.Content != "valuable final evidence" {
		t.Fatal("paid result discarded", err)
	}
	if _, err = e.call(context.Background(), task.ID, []agent.ChatMessage{{Role: "user", Content: "again"}}, nil); !errors.Is(err, ErrBudget) {
		t.Fatal("new request exceeded budget", err)
	}
}

func TestSlowPlannerDoesNotBlockWorkerFrontier(t *testing.T) {
	store, task := fixture(t)
	plannerStarted := make(chan struct{})
	childDone := make(chan struct{})
	var plans atomic.Int32
	r := normalRuntime()
	r.complete = func(ctx context.Context, t *Task, m []agent.ChatMessage, _ []agent.Tool) (*ModelReply, error) {
		if strings.Contains(m[0].Content, "You plan an authorized") {
			n := plans.Add(1)
			if n == 1 {
				return planFixture("a and b, then child", map[string]any{"key": "a", "title": "a", "instruction": "a"}, map[string]any{"key": "b", "title": "b", "instruction": "b"}, map[string]any{"key": "child", "title": "child", "instruction": "child", "depends_on": []string{"b"}}), nil
			}
			if n == 2 {
				close(plannerStarted)
				select {
				case <-childDone:
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
			return reply(map[string]string{"status": "completed", "summary": "done"}), nil
		}
		key := ""
		for _, k := range []string{"a", "b", "child"} {
			if strings.Contains(m[1].Content, "YOUR TASK: "+k+"\n") {
				key = k
			}
		}
		if m[len(m)-1].Role == "tool" {
			return reply(map[string]any{"status": "completed", "evidence": []Evidence{{Kind: "fact", Title: key, ExecutionIDs: []string{"exec-" + key}}}}), nil
		}
		return &ModelReply{Message: agent.ChatMessage{Role: "assistant", ToolCalls: []agent.ToolCall{{ID: "call-" + key, Function: agent.FunctionCall{Name: "inspect", Arguments: map[string]interface{}{"key": key}}}}}, Tokens: 100}, nil
	}
	r.execute = func(ctx context.Context, _ *Task, _ string, c agent.ToolCall) (*agent.ToolExecutionResult, error) {
		key := c.Function.Arguments["key"].(string)
		if key == "b" {
			select {
			case <-plannerStarted:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		if key == "child" {
			close(childDone)
		}
		return &agent.ToolExecutionResult{ExecutionID: "exec-" + key, Result: key}, nil
	}
	e := NewEngine(store, autonomousFixture{r}, nil)
	defer e.Close()
	e.Start(task.ID)
	select {
	case <-childDone:
	case <-time.After(5 * time.Second):
		t.Fatal("planner model call stalled worker frontier")
	}
	waitTask(t, store, task.ID, func(t *Task) bool { return t.Status == "completed" && t.Deadline == 0 })
}

func TestTwelveStageChainPreservesRoadmapAndProgress(t *testing.T) {
	store, task := fixture(t)
	store.Change(task.ID, "", "", "", func(t *Task) error { t.MaxRounds = 20; t.TokenBudget = 1000000; return nil })
	r := normalRuntime()
	r.complete = func(_ context.Context, t *Task, m []agent.ChatMessage, _ []agent.Tool) (*ModelReply, error) {
		if strings.Contains(m[0].Content, "You plan an authorized") {
			count := 0
			for _, s := range t.Steps {
				if s.Status == "completed" {
					count++
				}
			}
			if count == 12 {
				return reply(map[string]string{"status": "completed", "summary": "All 12 dependent fixture stages verified"}), nil
			}
			key := fmt.Sprintf("stage-%d", count)
			deps := []string{}
			if count > 0 {
				deps = append(deps, fmt.Sprintf("stage-%d", count-1))
			}
			return planFixture("Finish all 12 stages in dependency order", map[string]any{"key": key, "title": key, "instruction": key, "depends_on": deps}), nil
		}
		var step *Step
		for _, s := range t.Steps {
			if s.Status == "running" {
				step = s
				break
			}
		}
		if step == nil {
			return nil, fmt.Errorf("no running worker")
		}
		if m[len(m)-1].Role == "tool" {
			return reply(map[string]any{"status": "completed", "summary": step.Key, "evidence": []Evidence{{Kind: "fact", Title: step.Key, ExecutionIDs: []string{"exec-" + step.Key}}}}), nil
		}
		return &ModelReply{Message: agent.ChatMessage{Role: "assistant", ToolCalls: []agent.ToolCall{{ID: "call-" + step.Key, Function: agent.FunctionCall{Name: "inspect", Arguments: map[string]interface{}{"key": step.Key}}}}}, Tokens: 100}, nil
	}
	r.execute = func(_ context.Context, t *Task, _ string, c agent.ToolCall) (*agent.ToolExecutionResult, error) {
		key := c.Function.Arguments["key"].(string)
		s := stepState(t, key)
		if !dependenciesReady(t, s) {
			tCopy := *t
			return nil, fmt.Errorf("dependency violated: %s", tCopy.ID)
		}
		return &agent.ToolExecutionResult{ExecutionID: "exec-" + key, Result: "stage evidence"}, nil
	}
	e := NewEngine(store, autonomousFixture{r}, nil)
	defer e.Close()
	e.Start(task.ID)
	got := waitTask(t, store, task.ID, func(t *Task) bool { return t.Status == "completed" && t.Deadline == 0 })
	if len(got.Steps) != 12 || r.calls.Load() != 12 || got.Strategy == "" {
		t.Fatalf("chain lost progress: %d %d", len(got.Steps), r.calls.Load())
	}
}

func TestContextCompactionKeepsToolPairsAndLatestObservations(t *testing.T) {
	task := &Task{Goal: "goal", Scope: "scope"}
	step := &Step{Title: "step", Instruction: "continue"}
	msgs := []agent.ChatMessage{{Role: "system", Content: "system"}, {Role: "user", Content: "old state"}}
	for i := 0; i < 10; i++ {
		id := fmt.Sprint(i)
		msgs = append(msgs, agent.ChatMessage{Role: "assistant", ToolCalls: []agent.ToolCall{{ID: id, Function: agent.FunctionCall{Name: "inspect"}}}}, agent.ChatMessage{Role: "tool", ToolCallID: id, Content: strings.Repeat("x", 12000)})
	}
	reduced := workerMessages(task, step, msgs)
	if len(reduced) >= len(msgs) {
		t.Fatal("history was never bounded")
	}
	ids := map[string]bool{}
	for _, m := range reduced {
		for _, c := range m.ToolCalls {
			ids[c.ID] = true
		}
		if m.Role == "tool" && !ids[m.ToolCallID] {
			t.Fatal("orphan tool result after compaction")
		}
	}
	if !ids["9"] || outstanding(reduced) != nil {
		t.Fatal("latest exchange lost or replayed")
	}
}
