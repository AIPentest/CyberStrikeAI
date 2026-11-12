package database

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/mattn/go-sqlite3"
	"go.uber.org/zap"
)

func TestSQLiteBeginReservesWriterWithoutBlockingReaders(t *testing.T) {
	for name, open := range map[string]func(string, *zap.Logger) (*DB, error){
		"conversations": NewDB,
		"knowledge":     NewKnowledgeDB,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "transactions.db")
			db, err := open(path, zap.NewNop())
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			// A separate connection with no busy wait makes the lock assertion
			// deterministic, without relying on goroutine scheduling or sleeps.
			other, err := sql.Open("sqlite3", path+"?_busy_timeout=0")
			if err != nil {
				t.Fatal(err)
			}
			defer other.Close()
			other.SetMaxOpenConns(1)
			if err := other.Ping(); err != nil {
				t.Fatal(err)
			}

			tx, err := db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			// Begin must already own the writer reservation, even before SELECT.
			_, err = other.Exec("BEGIN IMMEDIATE")
			if err == nil {
				_, _ = other.Exec("ROLLBACK")
				t.Fatal("Begin did not reserve the writer lock")
			}
			var sqliteErr sqlite3.Error
			if !errors.As(err, &sqliteErr) || sqliteErr.Code != sqlite3.ErrBusy {
				t.Fatalf("competing writer: want SQLITE_BUSY, got %v", err)
			}
			var tables int
			if err := other.QueryRow("SELECT COUNT(*) FROM sqlite_master").Scan(&tables); err != nil {
				t.Fatalf("WAL reader blocked by writer reservation: %v", err)
			}
			if err := tx.Rollback(); err != nil {
				t.Fatal(err)
			}
			if _, err := other.Exec("BEGIN IMMEDIATE"); err != nil {
				t.Fatalf("writer reservation was not released: %v", err)
			}
			if _, err := other.Exec("ROLLBACK"); err != nil {
				t.Fatal(err)
			}

			// A temporarily busy writer should make Begin wait, then succeed
			// when that writer releases its reservation.
			if _, err := other.Exec("BEGIN IMMEDIATE"); err != nil {
				t.Fatal(err)
			}
			defer other.Exec("ROLLBACK")
			done := make(chan error, 1)
			go func() {
				pending, err := db.Begin()
				if err == nil {
					err = pending.Rollback()
				}
				done <- err
			}()
			select {
			case err := <-done:
				t.Fatalf("Begin returned before the competing writer released its lock: %v", err)
			case <-time.After(50 * time.Millisecond):
			}
			if _, err := other.Exec("ROLLBACK"); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("Begin did not recover after writer released its lock: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("Begin did not resume after writer released its lock")
			}
		})
	}
}
