package handler

import (
	"context"
	"errors"
	"gopkg.in/yaml.v3"
	"strconv"
	"testing"
	"time"

	"cyberstrike-ai/internal/config"
)

func TestConfiguredTaskContext(t *testing.T) {
	for _, minutes := range []int{-1, 0, 1440} {
		t.Run(taskTimeoutCaseName(minutes), func(t *testing.T) {
			h := &AgentHandler{}
			want := 600 * time.Minute
			if minutes >= 0 {
				h.config = &config.Config{Agent: config.AgentConfig{TaskTimeoutMinutes: &minutes}}
				want = time.Duration(minutes) * time.Minute
			}
			parent, cancelParent := context.WithCancelCause(context.Background())
			defer cancelParent(nil)
			before := time.Now()
			ctx, cancel := h.newTaskContext(parent)
			defer cancel()
			deadline, ok := ctx.Deadline()
			if want == 0 {
				if ok {
					t.Fatal("unlimited task has a deadline")
				}
			} else if !ok || deadline.Before(before.Add(want)) || deadline.After(time.Now().Add(want)) {
				t.Fatalf("unexpected deadline %v", deadline)
			}
			cancelParent(ErrTaskCancelled)
			if !errors.Is(context.Cause(ctx), ErrTaskCancelled) {
				t.Fatal("parent cancellation lost")
			}

			ctx, cancel = h.newTaskContext(context.Background())
			cancel()
			if !errors.Is(ctx.Err(), context.Canceled) {
				t.Fatal("explicit cancellation lost")
			}

			early := time.Now().Add(time.Minute)
			parentDeadline, stop := context.WithDeadline(context.Background(), early)
			defer stop()
			ctx, cancel = h.newTaskContext(parentDeadline)
			defer cancel()
			if got, ok := ctx.Deadline(); !ok || !got.Equal(early) {
				t.Fatal("earlier parent deadline lost")
			}
		})
	}
}

func taskTimeoutCaseName(minutes int) string {
	if minutes < 0 {
		return "default"
	}
	if minutes == 0 {
		return "unlimited"
	}
	return "custom"
}

func TestRebindTaskRestartsConfiguredBudget(t *testing.T) {
	for _, minutes := range []int{0, 1440} {
		t.Run(taskTimeoutCaseName(minutes), func(t *testing.T) {
			h := &AgentHandler{config: &config.Config{Agent: config.AgentConfig{TaskTimeoutMinutes: &minutes}}, tasks: NewAgentTaskManager()}
			base, stop := context.WithCancelCause(context.Background())
			defer stop(nil)
			if _, err := h.tasks.StartTask("resume", "test", stop); err != nil {
				t.Fatal(err)
			}
			expired, cancelExpired := context.WithDeadline(base, time.Now().Add(-time.Second))
			before := time.Now()
			_, stopNew, resumed, cancelNew := h.rebindEinoRunningTask(expired, "resume", cancelExpired)
			defer stopNew(nil)
			defer cancelNew()
			if resumed.Err() != nil {
				t.Fatalf("resumed task inherited expired context: %v", resumed.Err())
			}
			deadline, ok := resumed.Deadline()
			if minutes == 0 {
				if ok {
					t.Fatal("unlimited resume has deadline")
				}
			} else if !ok || deadline.Before(before.Add(24*time.Hour)) || deadline.After(time.Now().Add(24*time.Hour)) {
				t.Fatalf("resume budget was not reset: %v", deadline)
			}
			stopNew(ErrTaskCancelled)
			if !errors.Is(context.Cause(resumed), ErrTaskCancelled) {
				t.Fatal("resumed task cannot be cancelled")
			}
		})
	}
}

// Saving unrelated settings must not erase a manually configured task budget.
func TestSettingsSavePreservesTaskTimeout(t *testing.T) {
	for _, value := range []string{"0", "1440"} {
		var doc yaml.Node
		if err := yaml.Unmarshal([]byte("agent:\n  task_timeout_minutes: "+value+"\n"), &doc); err != nil {
			t.Fatal(err)
		}
		updateAgentConfig(&doc, config.AgentConfig{MaxIterations: 100})
		data, err := yaml.Marshal(&doc)
		if err != nil {
			t.Fatal(err)
		}
		var cfg config.Config
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			t.Fatal(err)
		}
		if cfg.Agent.TaskTimeoutMinutes == nil || strconv.Itoa(*cfg.Agent.TaskTimeoutMinutes) != value {
			t.Fatalf("settings save changed timeout: %s", data)
		}
	}
}
