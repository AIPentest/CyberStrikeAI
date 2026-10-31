package config

import (
	"fmt"
	"time"
)

// EffectiveTaskTimeout returns the execution budget for each task segment.
// An omitted value retains the historical ten-hour limit; zero disables it.
func (a AgentConfig) EffectiveTaskTimeout() time.Duration {
	if a.TaskTimeoutMinutes == nil {
		return 600 * time.Minute
	}
	return time.Duration(*a.TaskTimeoutMinutes) * time.Minute
}

func (a AgentConfig) validateTaskTimeout() error {
	if a.TaskTimeoutMinutes == nil {
		return nil
	}
	const maxMinutes = int64((1<<63 - 1) / time.Minute)
	minutes := int64(*a.TaskTimeoutMinutes)
	if minutes < 0 || minutes > maxMinutes {
		return fmt.Errorf("agent.task_timeout_minutes must be between 0 and %d minutes", maxMinutes)
	}
	return nil
}
