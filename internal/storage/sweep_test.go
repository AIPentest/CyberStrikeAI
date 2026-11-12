package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func mkSweepDirs(t *testing.T, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if err := os.MkdirAll(p, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
		if err := os.WriteFile(filepath.Join(p, "artifact.txt"), []byte("junk"), 0644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
}

func newSweepCleaner(t *testing.T, workspace, reduction string) *Cleaner {
	t.Helper()
	return NewCleaner(Options{
		Paths:    Paths{Workspace: workspace, Reduction: reduction},
		CacheTTL: -1,
	})
}

func TestSweepTargetRemovesOnlyMatchingConversation(t *testing.T) {
	base := t.TempDir()
	ws := filepath.Join(base, "workspace")
	red := filepath.Join(base, "reduction")

	keep := "conv-KEEP"
	wipe := "conv-WIPE"
	mkSweepDirs(t,
		filepath.Join(ws, "conversations", wipe),
		filepath.Join(ws, "conversations", keep),
		filepath.Join(red, "conversations", wipe),
	)

	c := newSweepCleaner(t, ws, red)
	rep, err := c.SweepTarget(SweepTargetRequest{Scope: ScopeConversation, ID: wipe, DryRun: false, Trigger: "test"})
	if err != nil {
		t.Fatalf("SweepTarget: %v", err)
	}
	if rep.Totals.RemovedUnits != 2 {
		t.Fatalf("removed units = %d, want 2", rep.Totals.RemovedUnits)
	}
	if _, err := os.Stat(filepath.Join(ws, "conversations", wipe)); !os.IsNotExist(err) {
		t.Error("wiped conversation workspace dir still exists")
	}
	if _, err := os.Stat(filepath.Join(ws, "conversations", keep)); err != nil {
		t.Error("non-target conversation was wrongly removed")
	}
}

func TestSweepTargetDryRunDeletesNothing(t *testing.T) {
	base := t.TempDir()
	ws := filepath.Join(base, "workspace")
	mkSweepDirs(t, filepath.Join(ws, "conversations", "conv-1"))

	c := newSweepCleaner(t, ws, "")
	rep, err := c.SweepTarget(SweepTargetRequest{Scope: ScopeConversation, ID: "conv-1", DryRun: true})
	if err != nil {
		t.Fatalf("SweepTarget: %v", err)
	}
	if rep.Totals.RemovedUnits != 0 {
		t.Fatalf("dry-run removed %d units, want 0", rep.Totals.RemovedUnits)
	}
	if rep.Totals.ReclaimableUnits != 1 {
		t.Fatalf("dry-run reclaimable = %d, want 1", rep.Totals.ReclaimableUnits)
	}
	if _, err := os.Stat(filepath.Join(ws, "conversations", "conv-1")); err != nil {
		t.Error("dry-run must not delete anything")
	}
}

func TestSweepTargetProjectScope(t *testing.T) {
	base := t.TempDir()
	ws := filepath.Join(base, "workspace")
	mkSweepDirs(t,
		filepath.Join(ws, "projects", "proj-9"),
		filepath.Join(ws, "conversations", "proj-9"), // same name, different scope: must NOT match
	)

	c := newSweepCleaner(t, ws, "")
	rep, err := c.SweepTarget(SweepTargetRequest{Scope: ScopeProject, ID: "proj-9", DryRun: false})
	if err != nil {
		t.Fatalf("SweepTarget: %v", err)
	}
	if rep.Totals.RemovedUnits != 1 {
		t.Fatalf("removed = %d, want 1 (project only)", rep.Totals.RemovedUnits)
	}
	if _, err := os.Stat(filepath.Join(ws, "conversations", "proj-9")); err != nil {
		t.Error("conversation with same name as project was wrongly removed")
	}
}

func TestSweepTargetRejectsBadInput(t *testing.T) {
	c := newSweepCleaner(t, t.TempDir(), "")
	for _, tc := range []SweepTargetRequest{
		{Scope: ScopeConversation, ID: ""},
		{Scope: ScopeNone, ID: "x"},
		{Scope: ScopeConversation, ID: "../escape"},
		{Scope: ScopeConversation, ID: "a/b"},
		{Scope: ScopeProject, ID: ".."},
	} {
		if _, err := c.SweepTarget(tc); err == nil {
			t.Errorf("expected rejection for %+v", tc)
		}
	}
}
