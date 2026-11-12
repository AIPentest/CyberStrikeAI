package database

import (
	"fmt"
	"go.uber.org/zap"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyWhitespaceCheckpointKeysRespectConversationScope(t *testing.T) {
	db, err := NewDB(filepath.Join(t.TempDir(), "legacy.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p, err := db.CreateProject(&Project{Name: "scope"})
	if err != nil {
		t.Fatal(err)
	}
	for i, space := range []string{" ", "\t", "\r\n", "\u00a0", "\u3000"} {
		canonical := fmt.Sprintf("note/context-legacy%d", i)
		f, err := db.UpsertProjectFact(&ProjectFact{ProjectID: p.ID, FactKey: canonical, SourceConversationID: "other", Summary: "LEGACY_PROGRESS"})
		if err != nil {
			t.Fatal(err)
		}
		oldKey := space + canonical + space
		// Simulate an already-stored legacy row, without migrating it on read.
		if _, err := db.Exec("UPDATE project_facts SET fact_key=? WHERE id=?", oldKey, f.ID); err != nil {
			t.Fatal(err)
		}
		if !IsContextCheckpointKey(oldKey) {
			t.Fatal("helper did not identify legacy key")
		}
		if err := ValidateFactKey(oldKey); err == nil {
			t.Fatal("write validation accepts noncanonical key")
		}
		if _, err := db.UpsertProjectFact(&ProjectFact{ProjectID: p.ID, FactKey: space + "finding/new"}); err == nil {
			t.Fatal("write boundary stored noncanonical key")
		}
	}
	if _, err := db.UpsertProjectFact(&ProjectFact{ProjectID: p.ID, FactKey: "finding/shared", Summary: "shared"}); err != nil {
		t.Fatal(err)
	}
	for _, filter := range []ProjectFactListFilter{{RestrictContextCheckpoints: true, ContextConversationID: "current"}, {RestrictContextCheckpoints: true, ContextConversationID: "current", Search: "LEGACY_PROGRESS"}, {RestrictContextCheckpoints: true}} {
		facts, err := db.ListProjectFacts(p.ID, filter, 100, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range facts {
			if IsContextCheckpointKey(f.FactKey) {
				t.Fatalf("foreign legacy note crossed scope: %q", f.FactKey)
			}
		}
	}
	index, err := db.ListProjectFactsForIndex(p.ID, false, "current")
	if err != nil || len(index) != 1 || index[0].FactKey != "finding/shared" {
		t.Fatalf("legacy note leaked into index: %#v %v", index, err)
	}
	history, err := db.ListProjectFacts(p.ID, ProjectFactListFilter{Search: "LEGACY_PROGRESS"}, 100, 0)
	if err != nil || len(history) != 5 {
		t.Fatalf("historical records lost: %#v %v", history, err)
	}
	for _, f := range history {
		if f.FactKey == strings.TrimSpace(f.FactKey) {
			t.Fatal("read mutated legacy evidence")
		}
	}
	own, err := db.ListProjectFactsForIndex(p.ID, false, "other")
	if err != nil || len(own) != 6 {
		t.Fatalf("legacy origin no longer visible to owner: %#v %v", own, err)
	}
}
