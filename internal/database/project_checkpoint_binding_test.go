package database

import (
	"path/filepath"
	"testing"

	"go.uber.org/zap"
)

func TestCheckpointWriteRechecksBindingAtomically(t *testing.T) {
	db, err := NewDB(filepath.Join(t.TempDir(), "binding.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a, err := db.CreateProject(&Project{Name: "a"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := db.CreateProject(&Project{Name: "b"})
	if err != nil {
		t.Fatal(err)
	}
	conv, err := db.CreateConversation("run", ConversationCreateMeta{ProjectID: a.ID})
	if err != nil {
		t.Fatal(err)
	}
	f := &ProjectFact{ProjectID: a.ID, FactKey: "note/context-test", SourceConversationID: conv.ID, Summary: "first", Body: "first body"}
	if err := db.SaveContextCheckpoint(f); err != nil {
		t.Fatal(err)
	}
	first, err := db.GetProjectFactByKey(a.ID, f.FactKey)
	if err != nil {
		t.Fatal(err)
	}
	// A running agent has already read a.ID; binding changes before its write.
	if err := db.SetConversationProjectID(conv.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	f.Body = "wrong-project update"
	if err := db.SaveContextCheckpoint(f); err == nil {
		t.Fatal("stale run updated old-project memory")
	}
	f.FactKey = "note/context-new"
	if err := db.SaveContextCheckpoint(f); err == nil {
		t.Fatal("stale run inserted old-project memory")
	}
	existing, err := db.GetProjectFactByKey(a.ID, first.FactKey)
	if err != nil || existing.Body != "first body" {
		t.Fatalf("failed write changed evidence: %#v %v", existing, err)
	}
	if _, err := db.GetProjectFactByKey(a.ID, f.FactKey); err == nil {
		t.Fatal("failed write left a row")
	}
	f.ProjectID = b.ID
	if err := db.SaveContextCheckpoint(f); err != nil {
		t.Fatal(err)
	}
	if err := db.SetConversationProjectID(conv.ID, ""); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveContextCheckpoint(f); err == nil {
		t.Fatal("unbound conversation still persisted project memory")
	}
}
