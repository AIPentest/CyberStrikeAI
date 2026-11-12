package database

import (
	"path/filepath"
	"testing"

	"go.uber.org/zap"
)

func TestLegacyPaddedFactCanUpdateWithoutChangingIdentity(t *testing.T) {
	db, err := NewDB(filepath.Join(t.TempDir(), "legacy-update.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p, err := db.CreateProject(&Project{Name: "legacy"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := db.CreateProject(&Project{Name: "other"})
	if err != nil {
		t.Fatal(err)
	}
	f, err := db.UpsertProjectFact(&ProjectFact{ProjectID: p.ID, FactKey: "finding/legacy", Summary: "before", Body: "original evidence"})
	if err != nil {
		t.Fatal(err)
	}
	legacyKey := "\u3000 finding/legacy \t"
	if _, err := db.Exec("UPDATE project_facts SET fact_key=? WHERE id=?", legacyKey, f.ID); err != nil {
		t.Fatal(err)
	}
	loaded, err := db.GetProjectFact(f.ID)
	if err != nil {
		t.Fatal(err)
	}
	loaded.Summary = "after"
	loaded.Body = ""
	updated, err := db.UpsertProjectFact(loaded)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != f.ID || updated.FactKey != legacyKey || updated.Summary != "after" || updated.Body != "original evidence" {
		t.Fatalf("legacy edit changed identity or evidence: %+v", updated)
	}
	stored, err := db.GetProjectFactByKey(p.ID, legacyKey)
	if err != nil || stored.ID != f.ID || stored.FactKey != legacyKey || stored.Summary != "after" {
		t.Fatalf("legacy key was rewritten: %+v %v", stored, err)
	}
	for name, candidate := range map[string]*ProjectFact{
		"new key":           {ProjectID: p.ID, FactKey: " finding/new ", Summary: "new"},
		"existing key only": {ProjectID: p.ID, FactKey: legacyKey, Summary: "overwrite"},
		"wrong record":      {ID: "not-the-stored-id", ProjectID: p.ID, FactKey: legacyKey, Summary: "overwrite"},
		"wrong project":     {ID: f.ID, ProjectID: other.ID, FactKey: legacyKey, Summary: "overwrite"},
		"changed key":       {ID: f.ID, ProjectID: p.ID, FactKey: " finding/new ", Summary: "overwrite"},
		"invalid core":      {ID: f.ID, ProjectID: p.ID, FactKey: " finding/invalid? ", Summary: "overwrite"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := db.UpsertProjectFact(candidate); err == nil {
				t.Fatal("legacy exception accepted a new or mismatched key")
			}
		})
	}
	stored, err = db.GetProjectFact(f.ID)
	if err != nil || stored.FactKey != legacyKey || stored.Summary != "after" || stored.Body != "original evidence" {
		t.Fatalf("rejected write modified stored evidence: %+v %v", stored, err)
	}
	// Padding was accepted by the old validator, but arbitrary invalid keys
	// were not. A stored malformed core must not widen this compatibility path.
	malformed, err := db.UpsertProjectFact(&ProjectFact{ProjectID: p.ID, FactKey: "finding/malformed", Summary: "untouched"})
	if err != nil {
		t.Fatal(err)
	}
	malformed.FactKey = " finding/invalid? "
	if _, err := db.Exec("UPDATE project_facts SET fact_key=? WHERE id=?", malformed.FactKey, malformed.ID); err != nil {
		t.Fatal(err)
	}
	malformed.Summary = "overwrite"
	if _, err := db.UpsertProjectFact(malformed); err == nil {
		t.Fatal("compatibility exception accepted an invalid trimmed key")
	}
}
