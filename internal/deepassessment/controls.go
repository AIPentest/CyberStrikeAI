package deepassessment

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

type LivePhase struct {
	Active    bool `json:"active"`
	Planning  bool `json:"planning"`
	Reviewing bool `json:"reviewing"`
	Workers   int  `json:"workers"`
}

func (e *Engine) setPhase(task, phase string, value bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	p := e.phases[task]
	if phase == "planning" {
		p.Planning = value
	} else {
		p.Reviewing = value
	}
	e.phases[task] = p
}
func (e *Engine) Live(task string) LivePhase {
	e.mu.Lock()
	defer e.mu.Unlock()
	p := e.phases[task]
	_, p.Active = e.active[task]
	p.Workers = len(e.workers[task])
	return p
}

// User control is persisted before cancellation. A step pause neither cancels
// another worker nor changes the task's manual pause latch.
func (e *Engine) ControlStep(task, step, action, message string, priority int, revision int64) error {
	return e.ControlStepAs(task, step, action, message, priority, revision, "")
}
func (e *Engine) ControlStepAs(task, step, action, message string, priority int, revision int64, actor string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	cancel := e.workers[task][step]
	kind := "intent_" + action
	err := e.Store.Change(task, step, kind, message, func(t *Task) error {
		if t.Archived || t.Status == "completed" || t.Status == "cancelled" || Active(t.Status) && t.Status != "running" {
			return ErrConflict
		}
		automatic := Active(t.Status) || t.Status == "recovering" || t.Status == "waiting_retry" || t.Status == "waiting_progress"
		if actor != "" && (action == "resume" || action == "guidance") && automatic && t.ExecutorID != actor {
			return fmt.Errorf("请先暂停测评，再以当前账号继续后操作意图")
		}
		if revision > t.Revision || (action != "pause" && action != "guidance" && revision != t.Revision) {
			return ErrConflict
		}
		st := t.Step(step)
		if st == nil {
			return fmt.Errorf("unknown intent")
		}
		if st.Status == "completed" || st.Status == "retired" {
			return fmt.Errorf("intent has already finished")
		}
		switch action {
		case "pause":
			if st.Status == "uncertain" || st.Status == "cleanup_unconfirmed" {
				return fmt.Errorf("请先核对原执行结果")
			}
			if st.Status == "running" && cancel == nil {
				return fmt.Errorf("执行会话状态已变化，请刷新后重试")
			}
			st.UserPaused = true
			if st.Status == "running" || st.Status == "pausing" {
				st.Status = "pausing"
			} else {
				st.Status = "paused"
			}
		case "resume":
			if st.Status != "paused" || cancel != nil {
				return fmt.Errorf("请等待此意图暂停完成后再继续")
			}
			st.UserPaused = false
			st.Status = "pending"
		case "priority":
			if priority < 1 || priority > 1000 {
				return fmt.Errorf("priority must be between 1 and 1000")
			}
			st.Priority = priority
		case "guidance":
			note := strings.TrimSpace(message)
			if note == "" || utf8.RuneCountInString(note) > 8000 {
				return fmt.Errorf("意图补充内容为空或过长")
			}
			st.Guidance = append(st.Guidance, note)
		default:
			return fmt.Errorf("unknown intent action")
		}
		return nil
	})
	if err == nil && action == "pause" && cancel != nil {
		cancel()
	}
	return err
}
