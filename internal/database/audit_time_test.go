package database

import (
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestBuildAuditLogsWhere_timeFilterSQL(t *testing.T) {
	since := time.Date(2026, 6, 16, 17, 2, 0, 0, time.UTC)
	until := time.Date(2026, 6, 17, 3, 3, 0, 0, time.UTC)
	where, args := buildAuditLogsWhere(ListAuditLogsFilter{Since: &since, Until: &until})
	if !strings.Contains(where, "strftime('%s', created_at) >=") {
		t.Fatalf("expected epoch comparison for since, got %q", where)
	}
	if !strings.Contains(where, "strftime('%s', created_at) <=") {
		t.Fatalf("expected epoch comparison for until, got %q", where)
	}
	if len(args) != 2 {
		t.Fatalf("expected 2 time args, got %d", len(args))
	}
	for i, arg := range args {
		s, ok := arg.(string)
		if !ok || s == "" {
			t.Fatalf("arg %d: want non-empty UTC RFC3339 string, got %v", i, arg)
		}
	}
}

func TestBuildAuditLogsWhere_relatedUserID(t *testing.T) {
	where, args := buildAuditLogsWhere(ListAuditLogsFilter{Category: "rbac", RelatedUserID: "user-123"})
	if !strings.Contains(where, "resource_id = ?") || !strings.Contains(where, "detail_json LIKE ?") {
		t.Fatalf("expected related-user predicates, got %q", where)
	}
	if len(args) != 4 {
		t.Fatalf("expected category plus 3 related-user args, got %#v", args)
	}
	if args[1] != "user-123" || args[2] != `%"user_id":"user-123"%` || args[3] != `%"userId":"user-123"%` {
		t.Fatalf("unexpected related-user args: %#v", args)
	}
}

func TestListAuditLogs_timeFilterMixedStorageFormats(t *testing.T) {
	dbPath := copyRepoDBToTemp(t)
	db, err := NewDB(dbPath, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	since, _ := ParseRFC3339Time("2026-06-16T17:02:00Z")
	until, _ := ParseRFC3339Time("2026-06-17T03:03:00Z")
	filter := ListAuditLogsFilter{Since: &since, Until: &until, Limit: 50}
	logs, err := db.ListAuditLogs(filter)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range logs {
		at := row.CreatedAt.UTC()
		if at.Before(since) || at.After(until) {
			t.Fatalf("log %s at %s outside [%s, %s]", row.ID, at, since, until)
		}
	}
}
