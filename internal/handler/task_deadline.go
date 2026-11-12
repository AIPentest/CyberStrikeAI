package handler

import (
	"context"
	"errors"

	"cyberstrike-ai/internal/config"
)

// taskDeadlineExceeded distinguishes the task's deadline from a child operation's
// timeout. A run error (including a cancellation cause) may wrap DeadlineExceeded
// while the task itself still has time remaining; it must not exhaust the task budget.
func taskDeadlineExceeded(taskCtx context.Context) bool {
	return taskCtx != nil && errors.Is(taskCtx.Err(), context.DeadlineExceeded)
}

// newTaskContext starts a fresh execution budget. Unlimited tasks remain cancellable.
// A parent's cancellation or earlier deadline is always respected.
func (h *AgentHandler) newTaskContext(parent context.Context) (context.Context, context.CancelFunc) {
	var cfg config.AgentConfig
	if h.config != nil {
		cfg = h.config.Agent
	}
	timeout := cfg.EffectiveTaskTimeout()
	if timeout == 0 {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, timeout)
}
