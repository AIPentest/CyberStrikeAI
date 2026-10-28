package c2

import (
	"cyberstrike-ai/internal/database"
	"strings"
	"testing"
	"time"
)

func TestWatchdogClosesExpiredOfflineCommandsWithoutTouchingQueuedOrLongTasks(t *testing.T) {
	manager, db := terminalTestManager(t)
	now := time.Now()
	sent := now.Add(-100 * time.Second)
	if _, err := db.Exec(`UPDATE c2_sessions SET last_check_in=?,sleep_seconds=5 WHERE id=?`, now.Add(-time.Hour), "session"); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		id, status, kind string
		timeout          float64
	}{{"expired", "sent", "exec", 0}, {"long", "sent", "exec", 300}, {"queued", "queued", "exec", 0}, {"done", "success", "exec", 0}, {"cancelled", "cancelled", "exec", 0}, {"other", "sent", "download", 0}} {
		task := &database.C2Task{ID: c.id, SessionID: "session", TaskType: c.kind, Status: c.status, SentAt: &sent, CreatedAt: sent, Payload: map[string]interface{}{"timeout_seconds": c.timeout}}
		if err := db.CreateC2Task(task); err != nil {
			t.Fatal(err)
		}
		if err := db.UpdateC2Task(c.id, database.C2TaskUpdate{SentAt: &sent}); err != nil {
			t.Fatal(err)
		}
	}
	watchdog := NewSessionWatchdog(manager)
	watchdog.tick()
	session, _ := db.GetC2Session("session")
	if session.Status != string(SessionDead) {
		t.Fatal("stale session not closed")
	}
	expired, _ := db.GetC2Task("expired")
	if expired.Status != "failed" || expired.CompletedAt == nil || !strings.Contains(expired.Error, "not confirmed") {
		t.Fatalf("offline command left hanging: %#v", expired)
	}
	for id, want := range map[string]string{"long": "sent", "queued": "queued", "done": "success", "cancelled": "cancelled", "other": "sent"} {
		task, _ := db.GetC2Task(id)
		if task.Status != want {
			t.Fatalf("%s status=%s want %s", id, task.Status, want)
		}
	}
	if err := manager.IngestTaskResult(TaskResultReport{TaskID: "expired", Success: true, Output: "late"}); err != nil {
		t.Fatal(err)
	}
	expired, _ = db.GetC2Task("expired")
	if expired.Status != "failed" || expired.ResultText != "" {
		t.Fatal("late success replaced offline terminal")
	}
}

func TestTCPSerialDispatchKeepsRemainingTasksQueuedUntilResult(t *testing.T) {
	manager, db := terminalTestManager(t)
	if _, err := db.Exec(`UPDATE c2_listeners SET type=? WHERE id=?`, string(ListenerTypeTCPReverse), "listener"); err != nil {
		t.Fatal(err)
	}
	for i, id := range []string{"first", "second"} {
		if err := db.CreateC2Task(&database.C2Task{ID: id, SessionID: "session", TaskType: "exec", Status: "queued", ApprovalStatus: "approved", CreatedAt: time.Now().Add(time.Duration(i) * time.Millisecond)}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := manager.PopTasksForBeacon("session", 50)
	if err != nil || len(first) != 1 {
		t.Fatalf("first dispatch=%v err=%v", first, err)
	}
	again, err := manager.PopTasksForBeacon("session", 50)
	if err != nil || len(again) != 0 {
		t.Fatal("second task sent before first result")
	}
	queued, _ := db.GetC2Task("second")
	if queued.Status != "queued" {
		t.Fatal("serial sibling marked sent prematurely")
	}
	if err := manager.IngestTaskResult(TaskResultReport{TaskID: first[0].TaskID, Success: true}); err != nil {
		t.Fatal(err)
	}
	next, err := manager.PopTasksForBeacon("session", 50)
	if err != nil || len(next) != 1 || next[0].TaskID != "second" {
		t.Fatalf("next dispatch=%v err=%v", next, err)
	}
}
