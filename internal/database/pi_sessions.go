package database

import (
	"cyberstrike-ai/internal/agentruntime"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SetAgentSessionDir remembers every configured root durably. Switching roots
// must not orphan the old sessions, even when deletion happens after a restart.
func (db *DB) SetAgentSessionDir(base string) error {
	if db == nil || strings.TrimSpace(base) == "" {
		return nil
	}
	root, err := filepath.Abs(strings.TrimSpace(base))
	if err != nil {
		return err
	}
	_, err = db.Exec(`INSERT OR IGNORE INTO pi_session_roots (path) VALUES (?)`, root)
	return err
}

func (db *DB) AgentSessionRevision(conversationID string) (int64, error) {
	var revision int64
	err := db.QueryRow(`SELECT pi_session_revision FROM conversations WHERE id = ?`, conversationID).Scan(&revision)
	return revision, err
}

func (db *DB) removePiSessions(conversationID string) error {
	rows, err := db.Query(`SELECT path FROM pi_session_roots`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var root string
		if err := rows.Scan(&root); err != nil {
			return err
		}
		dir := filepath.Join(root, agentruntime.ConversationStorageKey(conversationID))
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("remove Pi sessions: %w", err)
		}
	}
	return rows.Err()
}
