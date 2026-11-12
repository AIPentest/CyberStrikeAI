package database

import (
	"cyberstrike-ai/internal/agentruntime"
	"go.uber.org/zap"
	"os"
	"path/filepath"
	"testing"
)

func TestDeleteConversationRemovesOnlyItsPiSessions(t *testing.T) {
	root := t.TempDir()
	db, err := NewDB(filepath.Join(root, "sessions.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	base := filepath.Join(root, "pi-sessions")
	db.SetAgentSessionDir(base)
	db.SetAgentConversationDirs(filepath.Join(root, "plans"), filepath.Join(root, "checkpoints"), filepath.Join(root, "outputs"), filepath.Join(root, "workspaces"))
	a, err := db.CreateConversation("a", ConversationCreateMeta{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := db.CreateConversation("b", ConversationCreateMeta{})
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string]string{}
	for _, id := range []string{a.ID, b.ID} {
		p := filepath.Join(base, agentruntime.ConversationStorageKey(id), "child", "session.jsonl")
		if err = os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(p, []byte("fixture"), 0o600); err != nil {
			t.Fatal(err)
		}
		paths[id] = p
	}
	if err = db.DeleteConversation(a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(paths[a.ID]); !os.IsNotExist(err) {
		t.Fatalf("deleted conversation still has session data: %v", err)
	}
	if _, err = os.Stat(paths[b.ID]); err != nil {
		t.Fatalf("other conversation was affected: %v", err)
	}
}
