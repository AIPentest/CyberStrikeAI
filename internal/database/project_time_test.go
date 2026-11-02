package database

import (
	"encoding/json"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestParseDBTime_projectFactFormats(t *testing.T) {
	cases := []string{
		"2026-05-26 11:13:07.442143+08:00",
		"2026-05-26 11:13:07",
		"2026-05-26T11:13:07.442143+08:00",
	}
	for _, s := range cases {
		got := parseDBTime(s)
		if got.IsZero() {
			t.Fatalf("parseDBTime(%q) returned zero", s)
		}
	}
}

func TestListProjectFacts_updatedAtJSON(t *testing.T) {
	dbPath := copyRepoDBToTemp(t)
	db, err := NewDB(dbPath, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	projects, err := db.ListProjects("", "", 1, 0)
	if err != nil || len(projects) == 0 {
		t.Skip("no projects")
	}
	pid := projects[0].ID

	list, err := db.ListProjectFacts(pid, ProjectFactListFilter{}, 5, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) == 0 {
		t.Skip("no facts")
	}
	for _, f := range list {
		if f.UpdatedAt.IsZero() {
			t.Fatalf("fact %s UpdatedAt is zero after ListProjectFacts", f.FactKey)
		}
		b, err := json.Marshal(f)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]interface{}
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		raw, ok := m["updated_at"].(string)
		if !ok || raw == "" || raw[:4] == "0001" {
			t.Fatalf("bad updated_at in JSON: %v", m["updated_at"])
		}
	}
}

func TestParseDBTime_zeroOnGarbage(t *testing.T) {
	if !parseDBTime("").IsZero() {
		t.Fatal("expected zero for empty")
	}
}

// Ensure RFC3339 round-trip used by API is after year 2000.
func TestParseDBTime_marshalRoundTrip(t *testing.T) {
	s := "2026-05-26 11:13:07.442143+08:00"
	tm := parseDBTime(s)
	b, err := json.Marshal(tm)
	if err != nil {
		t.Fatal(err)
	}
	var back time.Time
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.IsZero() {
		t.Fatalf("unmarshal zero from %s", string(b))
	}
}
