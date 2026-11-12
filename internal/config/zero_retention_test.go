package config

import "testing"

func TestZeroRetentionStoragePreset(t *testing.T) {
	s := ZeroRetentionStorage()
	if !s.AutoCleanEffective() {
		t.Error("zero-retention must enable auto-clean")
	}
	if s.IntervalMinutesEffective() != 5 {
		t.Errorf("interval = %d, want 5 (floor)", s.IntervalMinutesEffective())
	}
	if s.OrphanGraceDaysEffective() != 0 {
		t.Errorf("orphan grace = %d, want 0", s.OrphanGraceDaysEffective())
	}
	for _, key := range StorageCategoryOrder {
		if !s.CategoryEnabled(key) {
			t.Errorf("category %s should be enabled", key)
		}
		if got := s.CategoryRetentionDays(key); got != 1 {
			t.Errorf("category %s retention = %d, want 1", key, got)
		}
	}
}
