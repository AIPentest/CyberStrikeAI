package database

import (
	"path/filepath"
	"testing"

	"go.uber.org/zap"
)

func TestBatchHITLLegacyMigration(t *testing.T) {
	db, err := NewDB(filepath.Join(t.TempDir(), "legacy.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.CreateBatchQueue("legacy", "test", "", "eino_single", "manual", "", nil, "", 1, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("ALTER TABLE batch_task_queues DROP COLUMN hitl_policy"); err != nil {
		t.Fatal(err)
	}
	if err := db.migrateBatchTaskQueuesTable(); err != nil {
		t.Fatal(err)
	}
	if err := db.migrateBatchTaskQueuesTable(); err != nil {
		t.Fatal(err)
	}
	row, err := db.GetBatchQueue("legacy")
	if err != nil {
		t.Fatal(err)
	}
	if row.HITLPolicy != "" {
		t.Fatalf("legacy queue must inherit: %+v", row)
	}
}
