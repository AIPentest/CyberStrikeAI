package database

import (
	"cyberstrike-ai/internal/agentruntime"
	"go.uber.org/zap"
	"os"
	"path/filepath"
	"testing"
)

func TestPiCleanupKeepsAllRootsAcrossRestartAndInvalidatesEveryMode(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "sessions.db")
	db, err := NewDB(path, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	a, err := db.CreateConversation("a", ConversationCreateMeta{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := db.CreateConversation("b", ConversationCreateMeta{})
	if err != nil {
		t.Fatal(err)
	}
	deleted, err := db.AddMessage(a.ID, "user", "delete this turn", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.AddMessage(a.ID, "assistant", "deleted reply", nil); err != nil {
		t.Fatal(err)
	}
	if _, err = db.AddMessage(a.ID, "user", "keep this turn", nil); err != nil {
		t.Fatal(err)
	}
	var deletedPaths, keptPaths []string
	for _, name := range []string{"old", "new"} {
		base := filepath.Join(root, name)
		if err := db.SetAgentSessionDir(base); err != nil {
			t.Fatal(err)
		}
		for _, id := range []string{a.ID, b.ID} {
			for _, mode := range []string{"single", "deep", "child"} {
				file := filepath.Join(base, agentruntime.ConversationStorageKey(id), mode, "session.jsonl")
				if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(file, []byte("fixture"), 0o600); err != nil {
					t.Fatal(err)
				}
				if id == a.ID {
					deletedPaths = append(deletedPaths, file)
				} else {
					keptPaths = append(keptPaths, file)
				}
			}
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = NewDB(path, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.DeleteConversationTurn(a.ID, deleted.ID); err != nil {
		t.Fatal(err)
	}
	revision, err := db.AgentSessionRevision(a.ID)
	if err != nil || revision != 1 {
		t.Fatalf("revision=%d err=%v", revision, err)
	}
	messages, err := db.GetMessages(a.ID)
	if err != nil || len(messages) != 1 || messages[0].Content != "keep this turn" {
		t.Fatalf("messages=%v err=%v", messages, err)
	}
	for _, file := range deletedPaths {
		if _, err := os.Stat(file); !os.IsNotExist(err) {
			t.Fatalf("deleted session survived: %s: %v", file, err)
		}
	}
	for _, file := range keptPaths {
		if _, err := os.Stat(file); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.DeleteConversation(b.ID); err != nil {
		t.Fatal(err)
	}
	for _, file := range keptPaths {
		if _, err := os.Stat(file); !os.IsNotExist(err) {
			t.Fatalf("conversation session survived: %s: %v", file, err)
		}
	}
}

func TestDeleteTurnRollsBackWhenPiCleanupFails(t *testing.T) {
	root := t.TempDir()
	db, err := NewDB(filepath.Join(root, "sessions.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	conv, err := db.CreateConversation("cleanup failure", ConversationCreateMeta{})
	if err != nil {
		t.Fatal(err)
	}
	msg, err := db.AddMessage(conv.ID, "user", "must remain on failure", nil)
	if err != nil {
		t.Fatal(err)
	}
	// A regular file as the parent is treated as an absent target by Windows
	// RemoveAll. NUL is invalid on every supported OS and reliably exercises
	// the cleanup error/transaction rollback path without filesystem privileges.
	blocked := filepath.Join(root, "invalid\x00root")
	if err := db.SetAgentSessionDir(blocked); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DeleteConversationTurn(conv.ID, msg.ID); err == nil {
		t.Fatal("cleanup failure reported success")
	}
	messages, err := db.GetMessages(conv.ID)
	if err != nil || len(messages) != 1 {
		t.Fatalf("messages lost on failure: %v %v", messages, err)
	}
	revision, err := db.AgentSessionRevision(conv.ID)
	if err != nil || revision != 0 {
		t.Fatalf("revision changed on failure: %d %v", revision, err)
	}
}

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
