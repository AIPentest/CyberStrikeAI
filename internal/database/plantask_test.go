package database

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestListConversationPlanTasksSortedAndToleratesMissingDirectory(t *testing.T) {
	tmp := t.TempDir()
	db, err := NewDB(filepath.Join(tmp, "plantask.db"), zap.NewNop())
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	base := filepath.Join(tmp, "skills", ".eino", "plantask")
	db.SetAgentConversationDirs(base, "", "", "")
	missing, err := db.ListConversationPlanTasks("missing")
	if err != nil || len(missing) != 0 {
		t.Fatalf("missing task board = %#v, err=%v", missing, err)
	}

	dir := filepath.Join(base, "conversation-1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	files := map[string]string{
		"10.json":  `{"id":"10","subject":"最后检查","status":"pending"}`,
		"2.json":   `{"id":"2","subject":"实现接口","status":"in_progress","activeForm":"正在实现接口"}`,
		"1.json":   `{"id":"1","subject":"梳理需求","status":"completed"}`,
		"bad.json": `{`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile(%s): %v", name, err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, ".highwatermark"), []byte("10"), 0o644); err != nil {
		t.Fatalf("WriteFile(highwatermark): %v", err)
	}

	tasks, err := db.ListConversationPlanTasks("conversation-1")
	if err != nil {
		t.Fatalf("ListConversationPlanTasks: %v", err)
	}
	if len(tasks) != 3 {
		t.Fatalf("tasks = %#v, want 3", tasks)
	}
	if tasks[0].ID != "1" || tasks[1].ID != "2" || tasks[2].ID != "10" {
		t.Fatalf("task order = %q, %q, %q", tasks[0].ID, tasks[1].ID, tasks[2].ID)
	}
	if tasks[1].ActiveForm != "正在实现接口" {
		t.Fatalf("activeForm = %q", tasks[1].ActiveForm)
	}
}

func TestListConversationPlanTasksSinceHidesPreviousRunUntilTaskCreate(t *testing.T) {
	tmp := t.TempDir()
	db, err := NewDB(filepath.Join(tmp, "plantask-current-run.db"), zap.NewNop())
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	base := filepath.Join(tmp, "plantask")
	db.SetAgentConversationDirs(base, "", "", "")
	dir := filepath.Join(base, "conversation-current-run")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	oldPath := filepath.Join(dir, "1.json")
	if err := os.WriteFile(oldPath, []byte(`{"id":"1","subject":"上一轮任务","status":"in_progress"}`), 0o644); err != nil {
		t.Fatalf("WriteFile(old): %v", err)
	}
	runStartedAt := time.Now().Add(-time.Second)
	oldTime := runStartedAt.Add(-time.Minute)
	if err := os.Chtimes(oldPath, oldTime, oldTime); err != nil {
		t.Fatalf("Chtimes(old): %v", err)
	}

	tasks, err := db.ListConversationPlanTasksSince("conversation-current-run", runStartedAt)
	if err != nil {
		t.Fatalf("ListConversationPlanTasksSince(before TaskCreate): %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("stale tasks shown before current TaskCreate: %#v", tasks)
	}

	newPath := filepath.Join(dir, "2.json")
	if err := os.WriteFile(newPath, []byte(`{"id":"2","subject":"本轮任务","status":"pending"}`), 0o644); err != nil {
		t.Fatalf("WriteFile(new): %v", err)
	}
	tasks, err = db.ListConversationPlanTasksSince("conversation-current-run", runStartedAt)
	if err != nil {
		t.Fatalf("ListConversationPlanTasksSince(after TaskCreate): %v", err)
	}
	if len(tasks) != 1 || tasks[0].ID != "2" {
		t.Fatalf("current tasks = %#v, want task 2 only", tasks)
	}
}
