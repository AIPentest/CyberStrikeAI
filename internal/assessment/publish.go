package assessment

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// PublishCandidate creates both the candidate and its owner/assignment atomically.
// A stable ID makes concurrent publish requests idempotent without overwriting a
// candidate that a reviewer has subsequently edited in vulnerability management.
func (s *Store) PublishCandidate(id, projectID, owner, title, detail, evidence, tag string) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	_, err = tx.Exec(`INSERT INTO vulnerabilities
 (id,conversation_id,project_id,owner_user_id,title,description,severity,status,evidence,task_tag,conversation_tag,vulnerability_type,target,preconditions,reproduction_steps,impact,recommendation,retest_notes,created_at,updated_at)
 VALUES(?,NULL,?,?,?,?, 'info','open',?,?, '', '', '', '', '', '', '', '',?,?) ON CONFLICT(id) DO NOTHING`, id, projectID, owner, title, detail, evidence, tag, now, now)
	if err != nil {
		return err
	}
	// Only the original publisher receives an ownership assignment. Publishing an
	// existing candidate must not grant a new principal access to the edited record.
	_, err = tx.Exec(`INSERT OR IGNORE INTO rbac_resource_assignments(id,user_id,resource_type,resource_id,created_at)
 SELECT ?,?,'vulnerability',?,? WHERE EXISTS(SELECT 1 FROM vulnerabilities WHERE id=? AND owner_user_id=?)`, uuid.NewString(), owner, id, now, id, owner)
	if err != nil {
		return fmt.Errorf("assign candidate: %w", err)
	}
	return tx.Commit()
}
