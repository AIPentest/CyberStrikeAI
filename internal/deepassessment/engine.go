package deepassessment

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var ErrRetryable = errors.New("temporary model failure")
var ErrBusy = errors.New("assessment execution slots are busy")

type Engine struct {
	schedulerCancel context.CancelFunc
	Store           *Store
	Runtime         Runtime
	mu              sync.Mutex
	active          map[string]context.CancelFunc
	workers         map[string]map[string]context.CancelFunc
	phases          map[string]LivePhase
	closed          bool
	wg              sync.WaitGroup
}

func NewEngine(s *Store, r Runtime) *Engine {
	return &Engine{Store: s, Runtime: r, active: map[string]context.CancelFunc{}, workers: map[string]map[string]context.CancelFunc{}, phases: map[string]LivePhase{}}
}

func (e *Engine) Start(id, executor string, revision int64) error {
	return e.start(id, executor, revision, false)
}

func (e *Engine) start(id, executor string, revision int64, automatic bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return fmt.Errorf("task service is shutting down")
	}
	if _, ok := e.active[id]; ok {
		return ErrBusy
	}
	if len(e.active) >= 8 {
		return ErrBusy
	}
	current, err := e.Store.Get(id)
	if err != nil {
		return err
	}
	if current.Revision != revision || current.Archived || Active(current.Status) || current.Status == "completed" || current.Status == "cancelled" {
		return ErrConflict
	}
	if automatic && current.Status != "recovering" && current.Status != "waiting_retry" && current.Status != "waiting_progress" {
		return ErrConflict
	}
	resolved, err := e.Store.ReconcileKnownCalls(id, "")
	if err != nil {
		return err
	}
	unknown, err := e.Store.UncertainSteps(id)
	if err != nil {
		return err
	}
	err = e.Store.Change(id, "", "started", "开始/继续任务", func(t *Task) error {
		if t.Archived {
			return fmt.Errorf("请先恢复归档，再开始任务")
		}
		if t.Revision != revision || Active(t.Status) || t.Status == "completed" || t.Status == "cancelled" {
			return ErrConflict
		}
		if automatic && t.Status != "recovering" && t.Status != "waiting_retry" && t.Status != "waiting_progress" {
			return ErrConflict
		}
		if len(unknown) > 0 {
			return fmt.Errorf("仍有结果不明的工具调用，请先查看待核对步骤和回执")
		}
		repairedStep := false
		for i := range t.Steps {
			step := &t.Steps[i]
			if step.Status == "uncertain" && resolved[step.ID] && !unknown[step.ID] {
				step.Status = "pending"
				if step.UserPaused {
					step.Status = "paused"
				}
				repairedStep = true
			}
		}
		for _, step := range t.Steps {
			if step.Status == "cleanup_unconfirmed" || step.Status == "uncertain" {
				return fmt.Errorf("请先确认未停止的执行已结束，再明确重试该步骤")
			}
		}
		if t.Question != "" {
			return fmt.Errorf("answer the pending question before continuing")
		}
		if t.Status != "waiting_retry" && t.Status != "recovering" {
			t.RetryCount = 0
		}
		t.Status = "running"
		if repairedStep {
			t.Summary = "已核对已结束的工具回执，正从原会话继续。"
		}
		t.ExecutorID = executor
		now := time.Now().UTC()
		t.RunStartedAt = &now
		t.RestartPending = false
		t.NextWake = nil
		return nil
	})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	e.active[id] = cancel
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		defer cancel()
		defer func() { e.mu.Lock(); delete(e.active, id); delete(e.workers, id); delete(e.phases, id); e.mu.Unlock() }()
		e.run(ctx, id)
	}()
	return nil
}

func (e *Engine) Stop(id string, revision int64, cancelTask bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	cancel, running := e.active[id]
	err := e.Store.Change(id, "", "stop_requested", "停止派发新步骤，等待执行资源收尾。", func(t *Task) error {
		if revision > t.Revision || t.Status == "completed" || t.Status == "cancelled" {
			return ErrConflict
		}
		t.RestartPending = false
		t.NextWake = nil
		if cancelTask {
			t.Status = "cancelled"
			if running {
				t.Status = "cancelling"
			}
		} else {
			t.Status = "paused"
			if running {
				t.Status = "pausing"
			}
		}
		return nil
	})
	if err == nil && cancel != nil {
		cancel()
	}
	return err
}

func (e *Engine) Close() {
	e.mu.Lock()
	e.closed = true
	if e.schedulerCancel != nil {
		e.schedulerCancel()
	}
	for id, cancel := range e.active {
		_ = e.Store.Change(id, "", "shutdown", "服务关闭，保存进度并停止执行。", func(t *Task) error {
			if t.Status == "running" {
				t.Status = "pausing"
				t.RestartPending = t.AutoResume
			}
			return nil
		})
		cancel()
	}
	e.mu.Unlock()
	e.wg.Wait()
}

func (e *Engine) Delete(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.active[id]; ok {
		return ErrConflict
	}
	return e.Store.Delete(id)
}

// Archiving never stops active work or deletes evidence. Restoring only returns
// the record to the list; it does not opt it back into automatic scheduling.
func (e *Engine) SetArchived(id string, revision int64, archived bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, running := e.active[id]; running {
		return ErrBusy
	}
	kind, message := "archived", "已归档，保留所有进度与证据。"
	if !archived {
		kind, message = "restored", "已恢复到列表，等待手动开始。"
	}
	return e.Store.Change(id, "", kind, message, func(t *Task) error {
		if t.Revision != revision || Active(t.Status) || t.Archived == archived {
			return ErrConflict
		}
		if archived {
			for _, step := range t.Steps {
				if step.Status == "running" || step.Status == "uncertain" || step.Status == "cleanup_unconfirmed" {
					return fmt.Errorf("请先核对执行结果并确认资源已停止，再归档")
				}
			}
			now := time.Now().UTC()
			t.ArchivedAt = &now
			if t.Status != "draft" && t.Status != "completed" && t.Status != "cancelled" {
				t.Status = "paused"
				if t.Question != "" {
					t.Status = "waiting_input"
				}
			}
		} else {
			t.ArchivedAt = nil
		}
		t.Archived = archived
		t.RestartPending = false
		t.NextWake = nil
		return nil
	})
}

type result struct {
	changeVersion int64
	kind, id      string
	proposal      Proposal
	outcome       Outcome
	verdict       Verdict
	err           error
	guidance      int
	revision      int64
}

func (e *Engine) run(parent context.Context, id string) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	results := make(chan result, 8)
	var jobs sync.WaitGroup
	defer func() {
		cancel()
		jobs.Wait()
		unknown, unknownErr := e.Store.UncertainSteps(id)
		_ = e.Store.Change(id, "", "settled", "本轮调度结束，进度已保存。", func(t *Task) error {
			accountTime(t)
			for i := range t.Steps {
				if t.Steps[i].Status == "running" {
					t.Steps[i].Status = "pending"
				}
			}
			switch t.Status {
			case "cancelling":
				t.Status = "cancelled"
			case "pausing":
				t.Status = "paused"
				if t.RestartPending {
					t.Status = "recovering"
				}
			case "running":
				t.Status = "paused"
			}
			for i := range t.Steps {
				if unknown[t.Steps[i].ID] {
					t.Steps[i].Status = "uncertain"
				}
			}
			if (unknownErr != nil || len(unknown) > 0) && t.Status != "cancelled" {
				t.Status = "needs_review"
				t.Summary = "工具调用结果尚未确认，进度已保存，请核对回执。"
			}
			return nil
		})
	}()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	workers := map[string]bool{}
	planning, reviewing, needPlan := false, false, true
	lastChange, changeErr := e.Store.ChangeVersion(id)
	if changeErr != nil {
		return
	}
	noProgress := 0
	waitForProgress := func(message string) {
		_ = e.Store.Change(id, "", "waiting_progress", message, func(t *Task) error {
			if t.Status != "running" {
				return ErrConflict
			}
			t.Status = "waiting_progress"
			t.Summary = Clip(message, 12000)
			wake := time.Now().UTC().Add(2 * time.Minute)
			t.NextWake = &wake
			return nil
		})
	}
	lastGuidance := 0
	stop := func(status, message string) {
		_ = e.Store.Change(id, "", status, message, func(t *Task) error {
			if t.Status == "running" {
				t.Status = status
				t.Summary = Clip(message, 12000)
			}
			return nil
		})
	}
	launch := func(fn func() result) {
		jobs.Add(1)
		go func() {
			defer jobs.Done()
			r := fn()
			select {
			case results <- r:
			case <-ctx.Done():
			}
		}()
	}
	for {
		t, err := e.Store.Get(id)
		if err != nil {
			return
		}
		if len(t.Guidance) > lastGuidance {
			lastGuidance = len(t.Guidance)
			needPlan = true
		}
		if ctx.Err() != nil || t.Status != "running" {
			return
		}
		// A durable revision coalesces graph changes between ticks, including
		// changes committed while a planner or worker is still running.
		change, err := e.Store.ChangeVersion(id)
		if err != nil {
			stop("blocked", err.Error())
			return
		}
		if change != lastChange {
			lastChange = change
			needPlan = true
		}
		// A worker completion makes dependent work runnable immediately, even
		// while another worker or the planner is still waiting for a model.
		for _, step := range t.Queue() {
			if len(workers) >= t.Concurrency {
				break
			}
			if workers[step.ID] || !t.Ready(step) {
				continue
			}
			workerCtx, workerCancel := context.WithCancel(ctx)
			e.mu.Lock()
			err = e.Store.Change(id, step.ID, "step_started", step.Title, func(current *Task) error {
				s := current.Step(step.ID)
				if current.Status != "running" || ctx.Err() != nil || s == nil || !current.Ready(*s) {
					return ErrConflict
				}
				s.Status = "running"
				s.Attempts++
				return nil
			})
			if err == nil {
				if e.workers[id] == nil {
					e.workers[id] = map[string]context.CancelFunc{}
				}
				e.workers[id][step.ID] = workerCancel
			}
			e.mu.Unlock()
			if err != nil {
				workerCancel()
				stop("blocked", err.Error())
				return
			}
			workers[step.ID] = true
			launch(func() result {
				defer workerCancel()
				defer func() { e.mu.Lock(); delete(e.workers[id], step.ID); e.mu.Unlock() }()
				out, runErr := e.Runtime.Run(workerCtx, t, step, func(conversation, summary string) error {
					return e.Store.Change(id, step.ID, "checkpoint", summary, func(current *Task) error {
						s := current.Step(step.ID)
						if s == nil || (s.Status != "running" && !(s.Status == "pausing" && s.UserPaused)) {
							return ErrConflict
						}
						if conversation != "" {
							if s.ConversationID != conversation {
								s.Conversations = append(s.Conversations, conversation)
							}
							s.ConversationID = conversation
						}
						if summary != "" {
							s.Summary = Clip(summary, 12000)
						}
						return nil
					})
				})
				// Persist observations even if a pause wins the scheduling race.
				resultMessage := out.Summary
				if runErr != nil {
					resultMessage = runErr.Error()
				}
				userPausedResult := false
				persistErr := e.Store.Change(id, step.ID, "step_result", resultMessage, func(current *Task) error {
					s := current.Step(step.ID)
					if s == nil || (s.Status != "running" && !(s.Status == "pausing" && s.UserPaused)) {
						return ErrConflict
					}
					userPausedResult = s.UserPaused
					if len(out.Evidence) > 0 {
						s.Evidence = out.Evidence
					}
					if out.ConversationID != "" {
						s.ConversationID = out.ConversationID
					}
					if out.Summary != "" {
						s.Summary = Clip(out.Summary, 16000)
					}
					if out.Status == "cleanup_unconfirmed" {
						s.Status = "cleanup_unconfirmed"
						if runErr != nil {
							s.Summary = runErr.Error()
						}
					} else if out.Status == "completed" && out.Summary != "" && runErr == nil && workerCtx.Err() == nil && !s.UserPaused {
						s.Status = "completed"
					} else if s.UserPaused {
						s.Status = "paused"
					} else if ctx.Err() != nil {
						s.Status = "pending"
					} else if errors.Is(runErr, ErrRetryable) {
						s.Status = "pending"
					} else if runErr != nil {
						s.Status = "failed"
						s.Summary = Clip(runErr.Error(), 16000)
					} else if out.Status == "yielded" {
						if out.Progress == s.LastProgress {
							s.StalledSegments++
						} else {
							s.StalledSegments = 0
						}
						s.LastProgress = out.Progress
						s.Status = "pending"
						if s.StalledSegments >= 3 {
							s.Status = "blocked"
							s.Summary += "\n连续分段没有新增证据，等待规划器换路。"
						}
					} else if (out.Status == "completed" || out.Status == "blocked") && out.Summary != "" {
						s.Status = out.Status
					} else {
						s.Status = "failed"
						s.Summary = "执行未提交有效结果。"
					}
					return nil
				})
				if persistErr == nil && !userPausedResult && errors.Is(runErr, ErrRetryable) {
					persistErr = runErr
				}
				return result{kind: "worker", id: step.ID, err: persistErr}
			})
		}
		if needPlan && !planning && !reviewing {
			err = e.Store.Change(id, "", "planning", "根据最新结果更新计划。", func(current *Task) error {
				if current.Status != "running" {
					return ErrConflict
				}
				current.Rounds++
				return nil
			})
			if err != nil {
				return
			}
			snapshot, err := e.Store.Get(id)
			if err != nil {
				return
			}
			planning = true
			e.setPhase(id, "planning", true)
			needPlan = false
			launch(func() result {
				defer e.setPhase(id, "planning", false)
				p, err := e.Runtime.Plan(ctx, snapshot)
				return result{kind: "plan", proposal: p, err: err, guidance: len(snapshot.Guidance), revision: snapshot.Revision, changeVersion: change}
			})
		}
		if len(workers) == 0 && !planning && !reviewing && !needPlan {
			if t.Question != "" {
				stop("waiting_input", t.Question)
				return
			}
			ready := false
			for _, s := range t.Steps {
				ready = ready || t.Ready(s)
			}
			if ready {
				continue
			}
			if len(t.Steps) == 0 {
				waitForProgress("当前没有可执行的新方向，已保留探索图，稍后重新评估；也可补充线索。")
				return
			}
			for _, s := range t.Steps {
				if s.Status != "completed" && s.Status != "retired" {
					waitForProgress("仍有受阻方向或依赖缺口，稍后结合已有证据重新规划。")
					return
				}
			}
			reviewing = true
			e.setPhase(id, "reviewing", true)
			launch(func() result {
				defer e.setPhase(id, "reviewing", false)
				v, err := e.Runtime.Review(ctx, t)
				return result{kind: "review", verdict: v, err: err, guidance: len(t.Guidance), changeVersion: change}
			})
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			continue
		case r := <-results:
			if errors.Is(r.err, ErrRetryable) {
				_ = e.Store.Change(id, "", "retry_scheduled", r.err.Error(), func(t *Task) error {
					if t.Status != "running" || ctx.Err() != nil {
						return ErrConflict
					}
					t.RetryCount++
					t.Status = "waiting_retry"
					wake := time.Now().UTC().Add(time.Duration(min(t.RetryCount, 5)) * 30 * time.Second)
					t.NextWake = &wake

					return nil
				})
				return
			}
			switch r.kind {
			case "worker":
				delete(workers, r.id)
				needPlan = true
				if r.err != nil {
					stop("blocked", "保存执行结果失败: "+r.err.Error())
					return
				}
			case "plan":
				planning = false
				if r.err != nil {
					stop("blocked", "规划失败: "+r.err.Error())
					return
				}
				err = e.Store.ChangeAtVersion(id, "", "plan_updated", r.proposal.Summary, r.changeVersion, func(current *Task) error {
					if current.Status != "running" || len(current.Guidance) != r.guidance {
						return ErrConflict
					}
					current.RetryCount = 0
					current.PlannedGraphVersion = r.changeVersion
					err := ApplyProposal(current, r.proposal)
					if err != nil && current.Revision != r.revision {
						return ErrConflict
					}
					return err
				})
				if err == ErrConflict {
					needPlan = true
					continue
				}
				if err != nil {
					stop("blocked", "计划无效: "+err.Error())
					return
				}
				// Keep a replan requested by a completion during this model call.
				if len(r.proposal.Steps) > 0 {
					noProgress = 0
				}
			case "review":
				reviewing = false
				if r.err != nil {
					stop("blocked", "验收失败: "+r.err.Error())
					return
				}
				current, err := e.Store.Get(id)
				if err != nil {
					return
				}
				latestChange, err := e.Store.ChangeVersion(id)
				if err != nil {
					stop("blocked", err.Error())
					return
				}
				if len(current.Guidance) != r.guidance || latestChange != r.changeVersion {
					needPlan = true
					continue
				}
				if AcceptVerdict(current, r.verdict) {
					err = e.Store.ChangeAtVersion(id, "", "completed", r.verdict.Summary, r.changeVersion, func(latest *Task) error {
						if latest.Status != "running" || latest.Revision != current.Revision {
							return ErrConflict
						}
						latest.Status = "completed"
						latest.Summary = Clip(r.verdict.Summary, 16000)
						return nil
					})
					if err == ErrConflict {
						needPlan = true
						continue
					}
					return
				}
				noProgress++
				if noProgress >= 2 {
					waitForProgress("验收仍有缺口，已保存证据，稍后继续评估: " + r.verdict.Missing)
					return
				}
				if err = e.Store.Change(id, "", "review_gap", r.verdict.Missing, func(latest *Task) error {
					latest.Summary = "验收缺口: " + Clip(r.verdict.Missing, 12000)
					return nil
				}); err != nil {
					return
				}
				needPlan = true
			}
		}
	}
}

// StartRecovery runs only the service's durable resume queue. It never starts drafts.
func (e *Engine) StartRecovery() {
	e.mu.Lock()
	if e.schedulerCancel != nil || e.closed {
		e.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	e.schedulerCancel = cancel
	e.wg.Add(1)
	e.mu.Unlock()
	go func() {
		defer e.wg.Done()
		tick := time.NewTicker(2 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
			tasks, err := e.Store.List("", "all", "recovering", "", 100, 0)
			if err != nil {
				continue
			}
			waiting, err := e.Store.List("", "all", "waiting_retry", "", 100, 0)
			if err == nil {
				tasks = append(tasks, waiting...)
				more, err := e.Store.List("", "all", "waiting_progress", "", 100, 0)
				if err == nil {
					tasks = append(tasks, more...)
				}
			}
			for _, row := range tasks {
				if ctx.Err() != nil {
					return
				}
				t, err := e.Store.Get(row.ID)
				if err != nil {
					continue
				}
				changed := false
				if t.Status == "waiting_progress" {
					version, err := e.Store.ChangeVersion(t.ID)
					changed = err == nil && version > t.PlannedGraphVersion
				}
				if !changed && t.NextWake != nil && time.Now().Before(*t.NextWake) {
					continue
				}
				if err = e.start(t.ID, t.ExecutorID, t.Revision, true); err != nil && !errors.Is(err, ErrConflict) && !errors.Is(err, ErrBusy) {
					if ctx.Err() != nil {
						return
					}
					_ = e.Store.Change(t.ID, "", "recovery_wait", err.Error(), func(current *Task) error {
						if current.Status == "recovering" || current.Status == "waiting_retry" || current.Status == "waiting_progress" {
							current.Status = "paused"
							current.Summary = err.Error()
						}
						return nil
					})
				}
			}
		}
	}()
}
