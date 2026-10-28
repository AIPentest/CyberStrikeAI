package database

import (
	"path/filepath"
	"testing"
	"time"

	"cyberstrike-ai/internal/mcp"

	"go.uber.org/zap"
)

func TestCancelOrphanedRunningToolExecutions(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "monitor.db")
	db, err := NewDB(dbPath, zap.NewNop())
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer db.Close()

	start := time.Now().Add(-2 * time.Hour)
	exec := &mcp.ToolExecution{
		ID:        "orphan-hydra",
		ToolName:  "hydra",
		Arguments: map[string]interface{}{"target": "127.0.0.1"},
		Status:    "running",
		StartTime: start,
	}
	if err := db.SaveToolExecution(exec); err != nil {
		t.Fatalf("SaveToolExecution: %v", err)
	}

	end := time.Now()
	n, err := db.CancelOrphanedRunningToolExecutions(end, "Execution was interrupted (service restarted)")
	if err != nil {
		t.Fatalf("CancelOrphanedRunningToolExecutions: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 row updated, got %d", n)
	}

	got, err := db.GetToolExecution("orphan-hydra")
	if err != nil {
		t.Fatalf("GetToolExecution: %v", err)
	}
	if got.Status != "orphaned" {
		t.Fatalf("expected orphaned, got %s", got.Status)
	}
	if got.EndTime == nil {
		t.Fatal("expected end_time to be set")
	}
	if got.Duration <= 0 {
		t.Fatalf("expected positive duration, got %v", got.Duration)
	}
}

func TestFinalizeStaleRunningToolExecutions_skipsActive(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "monitor.db")
	db, err := NewDB(dbPath, zap.NewNop())
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer db.Close()

	now := time.Now()
	oldStart := now.Add(-5 * time.Minute)
	if err := db.SaveToolExecution(&mcp.ToolExecution{
		ID: "stale", ToolName: "hydra", Status: "running", StartTime: oldStart,
	}); err != nil {
		t.Fatalf("SaveToolExecution stale: %v", err)
	}
	if err := db.SaveToolExecution(&mcp.ToolExecution{
		ID: "active", ToolName: "hydra", Status: "running", StartTime: oldStart,
	}); err != nil {
		t.Fatalf("SaveToolExecution active: %v", err)
	}

	active := map[string]struct{}{"active": {}}
	n, err := db.FinalizeStaleRunningToolExecutions(now, time.Minute, active, "Execution was interrupted (the session has ended)")
	if err != nil {
		t.Fatalf("FinalizeStaleRunningToolExecutions: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 stale row updated, got %d", n)
	}

	stale, err := db.GetToolExecution("stale")
	if err != nil {
		t.Fatalf("GetToolExecution stale: %v", err)
	}
	if stale.Status != "orphaned" {
		t.Fatalf("stale expected orphaned, got %s", stale.Status)
	}

	activeExec, err := db.GetToolExecution("active")
	if err != nil {
		t.Fatalf("GetToolExecution active: %v", err)
	}
	if activeExec.Status != "running" {
		t.Fatalf("active expected running, got %s", activeExec.Status)
	}
}
