package database

import (
	"path/filepath"
	"testing"

	"go.uber.org/zap"
)

func TestModelTokenUsagePersistsFromUsageProcessDetail(t *testing.T) {
	db := newModelTokenUsageTestDB(t)
	conv, err := db.CreateConversation("usage", ConversationCreateMeta{})
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	msg, err := db.AddMessage(conv.ID, "assistant", "done", nil)
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}
	if err := db.AddProcessDetail(msg.ID, conv.ID, modelTokenUsageEventType, "usage", map[string]interface{}{
		"source":           "eino",
		"orchestration":    "deep",
		"reason":           "final",
		"model":            "gpt-test",
		"modelCalls":       2,
		"promptTokens":     10,
		"completionTokens": 3,
		"totalTokens":      13,
		"cachedTokens":     4,
		"reasoningTokens":  1,
		"cacheWriteTokens": 2,
	}); err != nil {
		t.Fatalf("AddProcessDetail: %v", err)
	}

	stats, err := db.GetModelTokenUsageStats(ModelTokenUsageFilter{})
	if err != nil {
		t.Fatalf("GetModelTokenUsageStats: %v", err)
	}
	if stats.Summary.Events != 1 || stats.Summary.ModelCalls != 2 || stats.Summary.TotalTokens != 13 || stats.Summary.CachedTokens != 4 || stats.Summary.ReasoningTokens != 1 || stats.Summary.CacheWriteTokens != 2 {
		t.Fatalf("summary = %#v", stats.Summary)
	}
	if len(stats.ByModel) != 1 || stats.ByModel[0].Key != "gpt-test" || stats.ByModel[0].TotalTokens != 13 || stats.ByModel[0].CacheWriteTokens != 2 {
		t.Fatalf("by model = %#v", stats.ByModel)
	}
	if len(stats.Recent) != 1 || stats.Recent[0].CacheWriteTokens != 2 || stats.Today.CacheWriteTokens != 2 || len(stats.ByDay) != 1 || stats.ByDay[0].CacheWriteTokens != 2 {
		t.Fatalf("cache write usage lost: %#v", stats)
	}
}

func TestModelTokenUsageBackfillIsIdempotent(t *testing.T) {
	db := newModelTokenUsageTestDB(t)
	conv, err := db.CreateConversation("usage", ConversationCreateMeta{})
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	msg, err := db.AddMessage(conv.ID, "assistant", "done", nil)
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}
	if err := db.AddProcessDetail(msg.ID, conv.ID, modelTokenUsageEventType, "usage", map[string]interface{}{
		"source": "eino", "modelCalls": 1, "promptTokens": 7, "completionTokens": 5, "totalTokens": 12,
	}); err != nil {
		t.Fatalf("AddProcessDetail: %v", err)
	}
	if err := db.BackfillModelTokenUsageFromProcessDetails(); err != nil {
		t.Fatalf("Backfill 1: %v", err)
	}
	if err := db.BackfillModelTokenUsageFromProcessDetails(); err != nil {
		t.Fatalf("Backfill 2: %v", err)
	}
	stats, err := db.GetModelTokenUsageStats(ModelTokenUsageFilter{})
	if err != nil {
		t.Fatalf("GetModelTokenUsageStats: %v", err)
	}
	if stats.Summary.Events != 1 || stats.Summary.TotalTokens != 12 {
		t.Fatalf("summary after backfill = %#v", stats.Summary)
	}
}

func newModelTokenUsageTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := NewDB(filepath.Join(t.TempDir(), "usage.db"), zap.NewNop())
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestModelTokenUsageStatsRespectConversationAccess(t *testing.T) {
	db := newModelTokenUsageTestDB(t)
	var ownID string
	for _, owner := range []string{"alice", "bob"} {
		conv, err := db.CreateConversation(owner, ConversationCreateMeta{})
		if err != nil {
			t.Fatal(err)
		}
		if owner == "alice" {
			ownID = conv.ID
		}
		if _, err := db.Exec(`UPDATE conversations SET owner_user_id = ? WHERE id = ?`, owner, conv.ID); err != nil {
			t.Fatal(err)
		}
		msg, err := db.AddMessage(conv.ID, "assistant", "done", nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.AddProcessDetail(msg.ID, conv.ID, modelTokenUsageEventType, "usage", map[string]interface{}{
			"model": owner, "modelCalls": 1, "promptTokens": 100, "completionTokens": 20, "cacheWriteTokens": 30,
		}); err != nil {
			t.Fatal(err)
		}
	}
	stats, err := db.GetModelTokenUsageStats(ModelTokenUsageFilter{Access: RBACListAccess{UserID: "alice", Scope: "own"}})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Summary.Events != 1 || stats.Summary.TotalTokens != 120 || stats.Summary.CacheWriteTokens != 30 || len(stats.Recent) != 1 || stats.Recent[0].ConversationID != ownID || len(stats.ByModel) != 1 || stats.ByModel[0].Key != "alice" {
		t.Fatalf("usage leaks across users: %#v", stats)
	}
}
