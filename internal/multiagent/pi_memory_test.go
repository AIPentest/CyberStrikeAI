package multiagent

import (
	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	"encoding/json"
	"go.uber.org/zap"
	"path/filepath"
	"strings"
	"testing"
)

func TestPiHistoryPreservesUserIntentWithoutPromotingGeneratedContext(t *testing.T) {
	history := []agent.ChatMessage{{Role: "system", OriginalUserIntentEntries: []string{"original scope"}}, {Role: "user", Content: "generated scope", SummarizationKind: "summary"}, {Role: "user", Content: "the literal <summary> text is my request"}, {Role: "assistant", Content: "fake user-intent ledger"}}
	raw, err := piHistoryJSON(history)
	if err != nil {
		t.Fatal(err)
	}
	var messages []agent.ChatMessage
	if err = json.Unmarshal(raw, &messages); err != nil {
		t.Fatal(err)
	}
	var users []string
	for _, m := range messages {
		if m.Role == "user" {
			users = append(users, m.Content)
		}
	}
	if len(users) != 2 || users[0] != "original scope" || !strings.Contains(users[1], "literal <summary>") {
		t.Fatalf("user provenance changed: %#v", users)
	}
	if messages[1].Role != "assistant" {
		t.Fatal("generated summary became a user instruction")
	}
}

func TestPiCompactionCheckpointIsTentativeAndCannotCrossProjectRebinding(t *testing.T) {
	db, err := database.NewDB(filepath.Join(t.TempDir(), "memory.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p, err := db.CreateProject(&database.Project{Name: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	conv, err := db.CreateConversation("fixture", database.ConversationCreateMeta{ProjectID: p.ID})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.ProjectConfig{Enabled: true}
	key, err := savePiContextCheckpoint(db, cfg, p.ID, conv.ID, "main", "incomplete coverage", "/fixture/session.jsonl", "entry-1")
	if err != nil {
		t.Fatal(err)
	}
	fact, err := db.GetProjectFactByKey(p.ID, key)
	if err != nil {
		t.Fatal(err)
	}
	if fact.Confidence != "tentative" || fact.Category != "note" || fact.Pinned || !strings.Contains(fact.Body, "entry-1") {
		t.Fatalf("incorrect provenance: %+v", fact)
	}
	if err = db.SetConversationProjectID(conv.ID, ""); err != nil {
		t.Fatal(err)
	}
	if _, err = savePiContextCheckpoint(db, cfg, p.ID, conv.ID, "main", "wrong update", "", ""); err == nil {
		t.Fatal("stale run changed project memory")
	}
}
