package handler

import (
	"errors"
	"strings"
	"testing"

	"go.uber.org/zap"
)

func TestNormalizeBatchQueueConcurrency(t *testing.T) {
	if got := normalizeBatchQueueConcurrency(0); got != DefaultBatchQueueConcurrency {
		t.Fatalf("expected default %d, got %d", DefaultBatchQueueConcurrency, got)
	}
	if got := normalizeBatchQueueConcurrency(99); got != MaxBatchQueueConcurrency {
		t.Fatalf("expected max %d, got %d", MaxBatchQueueConcurrency, got)
	}
}

func TestClaimNextPendingTaskParallel(t *testing.T) {
	m := NewBatchTaskManager(zap.NewNop())
	queue, err := m.CreateBatchQueue("test", "", "single", "manual", "", "", nil, 3, []string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("CreateBatchQueue: %v", err)
	}
	m.UpdateQueueStatus(queue.ID, BatchQueueStatusRunning)

	t1, ok1 := m.ClaimNextPendingTask(queue.ID)
	t2, ok2 := m.ClaimNextPendingTask(queue.ID)
	if !ok1 || !ok2 || t1.ID == t2.ID {
		t.Fatalf("expected two distinct claims, got ok1=%v ok2=%v t1=%v t2=%v", ok1, ok2, t1, t2)
	}
	if t1.Status != BatchTaskStatusRunning || t2.Status != BatchTaskStatusRunning {
		t.Fatalf("claimed tasks should be running")
	}
	t3, ok3 := m.ClaimNextPendingTask(queue.ID)
	if !ok3 {
		t.Fatal("expected third claim")
	}
	_, ok4 := m.ClaimNextPendingTask(queue.ID)
	if ok4 {
		t.Fatal("expected no fourth pending task")
	}
	_ = t3
}

func TestBatchQueueExecutionShouldStop(t *testing.T) {
	t.Parallel()
	if !batchQueueExecutionShouldStop(nil, false) {
		t.Fatal("expected stop when queue missing")
	}
	if !batchQueueExecutionShouldStop(nil, true) {
		t.Fatal("expected stop when queue is nil but exists=true")
	}
	q := &BatchTaskQueue{Status: BatchQueueStatusRunning}
	if batchQueueExecutionShouldStop(q, true) {
		t.Fatal("expected continue when running")
	}
	q.Status = BatchQueueStatusCancelled
	if !batchQueueExecutionShouldStop(q, true) {
		t.Fatal("expected stop when cancelled")
	}
}

func TestBatchSubTaskConversationMetaKeepsQueueRole(t *testing.T) {
	t.Parallel()

	meta := batchSubTaskConversationMeta(nil, &BatchTaskQueue{Role: " 渗透测试 "})
	if meta.Source != "batch_task" {
		t.Fatalf("expected batch_task source, got %q", meta.Source)
	}
	if meta.RoleName != "渗透测试" {
		t.Fatalf("expected queue role to be stored on child conversation, got %q", meta.RoleName)
	}
}

func TestDeleteQueueBlockedWhileExecutorActive(t *testing.T) {
	t.Parallel()
	m := NewBatchTaskManager(zap.NewNop())
	queue, err := m.CreateBatchQueue("test", "", "single", "manual", "", "", nil, 1, []string{"hello"})
	if err != nil {
		t.Fatalf("CreateBatchQueue: %v", err)
	}
	if !m.TryMarkQueueExecutor(queue.ID) {
		t.Fatal("expected to mark executor")
	}
	m.UpdateQueueStatus(queue.ID, BatchQueueStatusCancelled)

	err = m.DeleteQueue(queue.ID)
	if !errors.Is(err, ErrBatchQueueExecutorActive) {
		t.Fatalf("expected ErrBatchQueueExecutorActive, got %v", err)
	}
	if _, ok := m.GetBatchQueue(queue.ID); !ok {
		t.Fatal("queue should still exist while executor active")
	}

	m.UnmarkQueueExecutor(queue.ID)
	if err := m.DeleteQueue(queue.ID); err != nil {
		t.Fatalf("expected delete after executor unmarked, got %v", err)
	}
	if _, ok := m.GetBatchQueue(queue.ID); ok {
		t.Fatal("queue should be deleted")
	}
}

func TestDeleteQueueBlockedWhileRunning(t *testing.T) {
	t.Parallel()
	m := NewBatchTaskManager(zap.NewNop())
	queue, err := m.CreateBatchQueue("test", "", "single", "manual", "", "", nil, 1, []string{"hello"})
	if err != nil {
		t.Fatalf("CreateBatchQueue: %v", err)
	}
	m.UpdateQueueStatus(queue.ID, BatchQueueStatusRunning)

	err = m.DeleteQueue(queue.ID)
	if !errors.Is(err, ErrBatchQueueStillRunning) {
		t.Fatalf("expected ErrBatchQueueStillRunning, got %v", err)
	}
}

func TestBlockedTasksEnterQueueSummary(t *testing.T) {
	h := &AgentHandler{batchTaskManager: NewBatchTaskManager(zap.NewNop()), logger: zap.NewNop()}
	all, err := h.batchTaskManager.CreateBatchQueue("all blocked", "", "single", "manual", "", "", nil, 1, []string{"one", "two"})
	if err != nil {
		t.Fatal(err)
	}
	h.batchTaskManager.UpdateQueueStatus(all.ID, BatchQueueStatusRunning)
	h.batchTaskManager.UpdateTaskStatus(all.ID, all.Tasks[0].ID, BatchTaskStatusBlocked, "", "资产集合无法确认")
	h.batchTaskManager.UpdateTaskStatus(all.ID, all.Tasks[1].ID, BatchTaskStatusBlocked, "", "资产集合无法确认")
	h.tryFinalizeBatchQueue(all.ID)
	got, ok := h.batchTaskManager.GetBatchQueue(all.ID)
	if !ok || got.LastRunError == "" || !strings.Contains(got.LastRunError, "受阻 2 项") || !strings.Contains(got.LastRunError, "资产集合无法确认") {
		t.Fatalf("all blocked finished without a reason: ok=%v err=%q", ok, got.LastRunError)
	}

	mixed, err := h.batchTaskManager.CreateBatchQueue("mixed", "", "single", "manual", "", "", nil, 1, []string{"done", "held"})
	if err != nil {
		t.Fatal(err)
	}
	h.batchTaskManager.UpdateQueueStatus(mixed.ID, BatchQueueStatusRunning)
	h.batchTaskManager.UpdateTaskStatus(mixed.ID, mixed.Tasks[0].ID, BatchTaskStatusCompleted, "ok", "")
	h.batchTaskManager.UpdateTaskStatus(mixed.ID, mixed.Tasks[1].ID, BatchTaskStatusBlocked, "", "成员当前不可见")
	h.tryFinalizeBatchQueue(mixed.ID)
	got, ok = h.batchTaskManager.GetBatchQueue(mixed.ID)
	if !ok || got.Status != BatchQueueStatusCompleted || !strings.Contains(got.LastRunError, "受阻 1 项") || !strings.Contains(got.LastRunError, "成员当前不可见") {
		t.Fatalf("mixed result: ok=%v status=%s err=%q", ok, got.Status, got.LastRunError)
	}
}

func TestTryMarkQueueExecutorDedupes(t *testing.T) {
	t.Parallel()
	m := NewBatchTaskManager(zap.NewNop())
	if !m.TryMarkQueueExecutor("q-1") {
		t.Fatal("first mark should succeed")
	}
	if m.TryMarkQueueExecutor("q-1") {
		t.Fatal("second mark should fail")
	}
	m.UnmarkQueueExecutor("q-1")
	if !m.TryMarkQueueExecutor("q-1") {
		t.Fatal("mark after unmark should succeed")
	}
}
