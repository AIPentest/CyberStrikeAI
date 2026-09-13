package handler

import (
	"path/filepath"
	"testing"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	"go.uber.org/zap"
)

func TestBatchHITLPolicyPersistence(t *testing.T) {
	db, err := database.NewDB(filepath.Join(t.TempDir(), "batch.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m := NewBatchTaskManager(zap.NewNop())
	m.SetDB(db)
	q, err := m.CreateBatchQueue("approval", "", "eino_single", "manual", "", "", nil, 1, []string{"test"}, "audit_agent")
	if err != nil {
		t.Fatal(err)
	}
	reloaded := NewBatchTaskManager(zap.NewNop())
	reloaded.SetDB(db)
	if err := reloaded.LoadFromDB(); err != nil {
		t.Fatal(err)
	}
	got, ok := reloaded.GetBatchQueue(q.ID)
	if !ok || got.HITLPolicy != "audit_agent" {
		t.Fatalf("reload: %+v", got)
	}
	if err := reloaded.UpdateQueueMetadata(q.ID, "renamed", "", "", nil); err != nil {
		t.Fatal(err)
	}
	row, err := db.GetBatchQueue(q.ID)
	if err != nil || row.HITLPolicy != "audit_agent" {
		t.Fatalf("unrelated edit lost policy: %+v, %v", row, err)
	}
	if err := reloaded.UpdateQueueMetadata(q.ID, "renamed", "", "", nil, ""); err != nil {
		t.Fatal(err)
	}
	row, err = db.GetBatchQueue(q.ID)
	if err != nil || row.HITLPolicy != "" {
		t.Fatalf("reset failed: %+v, %v", row, err)
	}
	if err := reloaded.UpdateQueueMetadata(q.ID, "renamed", "", "", nil, "invalid"); err == nil {
		t.Fatal("accepted invalid policy")
	}
	reloaded.UpdateTaskStatus(q.ID, got.Tasks[0].ID, BatchTaskStatusRunning, "", "")
	if err := reloaded.UpdateQueueMetadata(q.ID, "renamed", "", "", nil, "off"); err == nil {
		t.Fatal("changed policy during single-task execution")
	}
}

func TestBatchHITLActivation(t *testing.T) {
	db, err := database.NewDB(filepath.Join(t.TempDir(), "hitl.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	timeout := 60
	h := &AgentHandler{
		config: &config.Config{Hitl: config.HitlConfig{
			DefaultMode: "review_edit", DefaultReviewer: "audit_agent",
			DefaultTimeoutSeconds: &timeout, ToolWhitelist: []string{"safe_tool"},
		}},
		hitlManager: NewHITLManager(db, zap.NewNop()),
	}
	if err := h.hitlManager.EnsureSchema(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		policy, mode, reviewer string
		enabled                bool
	}{
		{"", "review_edit", "audit_agent", true},
		{"off", "off", "audit_agent", false},
		{"human", "approval", "human", true},
		{"audit_agent", "approval", "audit_agent", true},
		{"review_edit", "review_edit", "audit_agent", true},
	} {
		t.Run(tc.policy, func(t *testing.T) {
			req := h.batchHITLRequest(tc.policy)
			if req.Mode != tc.mode || req.Reviewer != tc.reviewer || req.Enabled != tc.enabled || req.TimeoutSeconds != timeout {
				t.Fatalf("bad request: %+v", req)
			}
			h.activateHITLForConversation("batch-test", req)
			defer h.hitlManager.DeactivateConversation("batch-test")
			if h.HITLNeedsToolApproval("batch-test", "unsafe_tool") != tc.enabled {
				t.Fatal("approval gate differs from policy")
			}
			if h.HITLNeedsToolApproval("batch-test", "safe_tool") {
				t.Fatal("global whitelist lost")
			}
		})
	}
}

func TestBatchHITLPersistenceFailure(t *testing.T) {
	db, err := database.NewDB(filepath.Join(t.TempDir(), "closed.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	m := NewBatchTaskManager(zap.NewNop())
	m.SetDB(db)
	q, err := m.CreateBatchQueue("test", "", "eino_single", "manual", "", "", nil, 1, []string{"test"}, "human")
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	if err := m.UpdateQueueMetadata(q.ID, "changed", "", "", nil, "off"); err == nil {
		t.Fatal("save failure hidden")
	}
	if q.HITLPolicy != "human" || q.Title != "test" {
		t.Fatal("failed write changed in-memory policy")
	}
	if _, err := m.CreateBatchQueue("test", "", "eino_single", "manual", "", "", nil, 1, []string{"test"}, "audit_agent"); err == nil {
		t.Fatal("create failure hidden")
	}
}
