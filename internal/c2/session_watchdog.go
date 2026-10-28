package c2

import (
	"context"
	"time"

	"cyberstrike-ai/internal/database"

	"go.uber.org/zap"
)

// SessionWatchdog 会话心跳看门狗：周期扫描所有 active/sleeping 会话，
// 把超过 (sleep * (1 + jitter%) * graceFactor + minGrace) 仍未心跳的标为 dead。
//
// 设计要点：
//   - 单 goroutine + ticker，避免对每个会话开 timer，session 数量大时也线性 OK；
//   - 阈值随会话自身 sleep/jitter 自适应（sleep=300s 的会话不能用 sleep=5s 的判定）；
//   - 全局最小宽限期 minGrace 避免 sleep 配置错误的会话被误判；
//   - 不读 implant_uuid，纯按 last_check_in 字段，与 listener 类型解耦。
type SessionWatchdog struct {
	manager  *Manager
	logger   *zap.Logger
	interval time.Duration // 扫描周期，默认 15s
	minGrace time.Duration // 最小宽限期，默认 30s
	gracePct float64       // 心跳超时倍数，默认 3.0（即 3 倍 sleep 周期没心跳算掉线）
	stopCh   chan struct{}
}

// NewSessionWatchdog 创建看门狗
func NewSessionWatchdog(m *Manager) *SessionWatchdog {
	return &SessionWatchdog{
		manager:  m,
		logger:   m.Logger().With(zap.String("component", "c2-watchdog")),
		interval: 15 * time.Second,
		minGrace: 30 * time.Second,
		gracePct: 3.0,
		stopCh:   make(chan struct{}),
	}
}

// Run 阻塞执行，直到 ctx.Done() 或 Stop()
func (w *SessionWatchdog) Run(ctx context.Context) {
	t := time.NewTicker(w.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopCh:
			return
		case <-t.C:
			w.tick()
		}
	}
}

// Stop 停止
func (w *SessionWatchdog) Stop() {
	select {
	case <-w.stopCh:
	default:
		close(w.stopCh)
	}
}

func (w *SessionWatchdog) tick() {
	now := time.Now()
	for _, status := range []string{string(SessionActive), string(SessionSleeping)} {
		sessions, err := w.manager.DB().ListC2Sessions(database.ListC2SessionsFilter{Status: status})
		if err != nil {
			w.logger.Warn("watchdog list query failed", zap.Error(err))
			continue
		}
		for _, s := range sessions {
			if w.isStale(s, now) {
				if err := w.manager.MarkSessionDead(s.ID); err != nil {
					w.logger.Warn("Failed to mark the session offline", zap.String("session_id", s.ID), zap.Error(err))
				}
			}
		}
	}
	sessions, err := w.manager.DB().ListC2Sessions(database.ListC2SessionsFilter{Status: string(SessionDead)})
	if err != nil {
		w.logger.Warn("Offline task query failed", zap.Error(err))
		return
	}
	for _, session := range sessions {
		w.expireOfflineCommands(session, now)
	}
}

func (w *SessionWatchdog) expireOfflineCommands(session *database.C2Session, now time.Time) {
	tasks, err := w.manager.DB().ListC2Tasks(database.ListC2TasksFilter{SessionID: session.ID, Status: string(TaskSent)})
	if err != nil {
		w.logger.Warn("Offline command query failed", zap.Error(err))
		return
	}
	for _, task := range tasks {
		if task.TaskType != string(TaskTypeExec) && task.TaskType != string(TaskTypeShell) {
			continue
		}
		if task.SentAt == nil {
			continue
		}
		seconds := 60.0
		if value, ok := task.Payload["timeout_seconds"].(float64); ok && value > 0 {
			seconds = value
		}
		// Invalid huge deadlines remain unexpired rather than wrapping a duration.
		if seconds > float64((1<<63-1)/int64(time.Second)) {
			continue
		}
		if now.Sub(*task.SentAt) <= time.Duration(seconds)*time.Second+w.minGrace {
			continue
		}
		status := string(TaskFailed)
		errText := "Session went offline and the task result exceeded its deadline; whether the remote process terminated is not confirmed"
		if err := w.manager.DB().UpdateC2Task(task.ID, database.C2TaskUpdate{ExpectedStatus: &task.Status, Status: &status, Error: &errText, CompletedAt: &now}); err != nil {
			w.logger.Warn("Failed to finalize offline commands", zap.Error(err))
			continue
		}
		w.manager.publishEvent("warn", "task", session.ID, task.ID, errText, nil)
	}
}

// isStale 判断会话是否超时
func (w *SessionWatchdog) isStale(s *database.C2Session, now time.Time) bool {
	// 无心跳记录：以 first_seen_at 兜底
	last := s.LastCheckIn
	if last.IsZero() {
		last = s.FirstSeenAt
	}
	sleep := s.SleepSeconds
	if sleep <= 0 {
		// TCP reverse 模式 sleep=0 → 用最小宽限期判定
		return now.Sub(last) > w.minGrace*2
	}
	jitter := s.JitterPercent
	if jitter < 0 {
		jitter = 0
	}
	if jitter > 100 {
		jitter = 100
	}
	// 阈值 = sleep * (1 + jitter%) * gracePct，再加 minGrace 兜底
	expected := time.Duration(float64(sleep)*(1+float64(jitter)/100.0)*w.gracePct) * time.Second
	if expected < w.minGrace {
		expected = w.minGrace
	}
	return now.Sub(last) > expected
}
