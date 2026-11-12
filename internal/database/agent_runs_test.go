package database

import (
	"go.uber.org/zap"
	"path/filepath"
	"testing"
)

func TestAgentRunPersistenceAndRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runs.db")
	db, err := NewDB(path, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	r := &AgentRun{RunID: "one", UserID: "user", Key: "key", Fingerprint: "hash", ConversationID: "conv", Status: "running"}
	if err = db.CreateAgentRun(r); err != nil {
		t.Fatal(err)
	}
	dup := *r
	dup.RunID = "duplicate"
	if err = db.CreateAgentRun(&dup); err == nil {
		t.Fatal("duplicate key accepted")
	}
	done := *r
	done.RunID = "done"
	done.Key = "done"
	if err = db.CreateAgentRun(&done); err != nil {
		t.Fatal(err)
	}
	if err = db.FinishAgentRun("done", "completed", map[string]string{"response": "saved"}); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = NewDB(path, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	got, err := db.FindAgentRun("user", "key")
	if err != nil || got.Status != "interrupted" {
		t.Fatalf("recovery: %+v %v", got, err)
	}
	got, err = db.GetAgentRun("done")
	if err != nil || got.Status != "completed" || string(got.Result) != `{"response":"saved"}` {
		t.Fatalf("terminal result: %+v %v", got, err)
	}
}
