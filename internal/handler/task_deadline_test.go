package handler

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cyberstrike-ai/internal/database"
	"go.uber.org/zap"
)

func TestBatchTaskDeadlineClassification(t *testing.T) {
	for _, tc := range []struct {
		name    string
		expired bool
		runErr  error
		want    string
	}{
		{"child deadline", false, context.DeadlineExceeded, "failed"},
		{"wrapped HTTP timeout", false, &url.Error{Op: "Post", URL: "https://model.invalid", Err: fmt.Errorf("request timeout: %w", context.DeadlineExceeded)}, "failed"},
		{"task deadline", true, context.DeadlineExceeded, "timeout"},
		{"task deadline with unrelated error", true, errors.New("stream ended"), "timeout"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logger := zap.NewNop()
			db, err := database.NewDB(filepath.Join(t.TempDir(), "deadline.db"), logger)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })
			conv, err := db.CreateConversation("deadline regression", database.ConversationCreateMeta{})
			if err != nil {
				t.Fatal(err)
			}
			msg, err := db.AddMessage(conv.ID, "assistant", "", nil)
			if err != nil {
				t.Fatal(err)
			}
			manager := NewBatchTaskManager(logger)
			queue, err := manager.CreateBatchQueue("deadline", "", "eino_single", "manual", "", "", nil, 1, []string{"test"})
			if err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(10 * time.Hour)
			if tc.expired {
				deadline = time.Now().Add(-time.Second)
			}
			taskCtx, cancel := context.WithDeadline(context.Background(), deadline)
			defer cancel()
			h := &AgentHandler{db: db, logger: logger, batchTaskManager: manager}
			status := "running"
			h.handleBatchSubTaskRunError(queue.ID, queue.Tasks[0], conv.ID, msg.ID, context.Background(), taskCtx, nil, tc.runErr, &status)
			if status != tc.want {
				t.Fatalf("finish status = %q, want %q", status, tc.want)
			}
			messages, err := db.GetMessages(conv.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(messages) != 1 || !strings.Contains(messages[0].Content, tc.runErr.Error()) {
				t.Fatalf("original diagnostic not preserved: %+v", messages)
			}
			updated, ok := manager.GetBatchQueue(queue.ID)
			if !ok || updated.Tasks[0].Status != BatchTaskStatusFailed {
				t.Fatalf("batch failure not recorded: %+v", updated)
			}
		})
	}
}

func TestTaskDeadlineExceededContextOwnership(t *testing.T) {
	taskCtx, stop := context.WithTimeout(context.Background(), 10*time.Hour)
	defer stop()
	child, stopChild := context.WithDeadline(taskCtx, time.Now().Add(-time.Second))
	defer stopChild()
	if !errors.Is(child.Err(), context.DeadlineExceeded) {
		t.Fatal("child deadline did not expire")
	}
	if taskDeadlineExceeded(taskCtx) {
		t.Fatal("child expiry exhausted parent task")
	}
	if !taskDeadlineExceeded(child) {
		t.Fatal("expired task deadline not recognized")
	}

	cancelled, cancel := context.WithCancelCause(context.Background())
	cancel(fmt.Errorf("child failed: %w", context.DeadlineExceeded))
	if taskDeadlineExceeded(cancelled) {
		t.Fatal("cancellation cause misclassified as task deadline")
	}

	userCtx, userCancel := context.WithCancelCause(context.Background())
	userCancel(ErrTaskCancelled)
	if taskDeadlineExceeded(userCtx) {
		t.Fatal("user cancellation misclassified as task deadline")
	}

	customDeadline, stopCustom := context.WithDeadlineCause(context.Background(), time.Now().Add(-time.Second), errors.New("budget exhausted"))
	defer stopCustom()
	if !taskDeadlineExceeded(customDeadline) {
		t.Fatal("custom deadline cause hid real task expiry")
	}
	if taskDeadlineExceeded(nil) || taskDeadlineExceeded(context.Background()) {
		t.Fatal("context without deadline classified as expired")
	}
}
