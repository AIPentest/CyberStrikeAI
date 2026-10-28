package project

import (
	"strings"
	"testing"

	"cyberstrike-ai/internal/database"
)

func TestBuildScopeBlock_targetsExcludeNotes(t *testing.T) {
	proj := &database.Project{
		ID:   "p1",
		Name: "Acme",
		ScopeJSON: `{"targets":["https://app.example.com"],"exclude":["*.cdn.example.com"],"notes":"仅 Web 层"}`,
	}
	block := BuildScopeBlock(proj)
	if !strings.Contains(block, "https://app.example.com") {
		t.Fatalf("missing target: %s", block)
	}
	if !strings.Contains(block, "cdn.example.com") {
		t.Fatalf("missing exclude: %s", block)
	}
	if !strings.Contains(block, "仅 Web 层") {
		t.Fatalf("missing notes: %s", block)
	}
}

func TestBuildScopeBlock_empty(t *testing.T) {
	if BuildScopeBlock(&database.Project{Name: "X"}) != "" {
		t.Fatal("expected empty")
	}
}

func TestBuildScopeBlock_invalidJSON(t *testing.T) {
	proj := &database.Project{Name: "X", ScopeJSON: `{not json`}
	block := BuildScopeBlock(proj)
	if !strings.Contains(block, "not valid JSON") {
		t.Fatalf("unexpected: %s", block)
	}
}
