package assessment

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"cyberstrike-ai/internal/database"
)

var ErrConflict = errors.New("assessment state changed; refresh and retry")

type Store struct{ DB *database.DB }

func (s *Store) Create(t *Task) error {
	b, err := json.Marshal(t)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(`INSERT INTO assessments(id,owner_user_id,project_id,name,status,payload,updated_at) VALUES(?,?,?,?,?,?,?)`, t.ID, t.OwnerID, t.ProjectID, t.Name, t.Status, string(b), t.UpdatedAt)
	return err
}
func (s *Store) Get(id string) (*Task, error) {
	var b string
	if err := s.DB.QueryRow(`SELECT payload FROM assessments WHERE id=?`, id).Scan(&b); err != nil {
		return nil, err
	}
	var t Task
	err := json.Unmarshal([]byte(b), &t)
	return &t, err
}

// Change serializes read-modify-write with SQLite's immediate write transaction.
// Event insertion and the state transition commit together.
func (s *Store) Change(id, stepID, kind, message string, fn func(*Task) error, eventData ...any) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var b string
	if err = tx.QueryRow(`SELECT payload FROM assessments WHERE id=?`, id).Scan(&b); err != nil {
		return err
	}
	var t Task
	if err = json.Unmarshal([]byte(b), &t); err != nil {
		return err
	}
	if err = fn(&t); err != nil {
		return err
	}
	t.UpdatedAt = time.Now().UTC()
	out, err := json.Marshal(t)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(`UPDATE assessments SET name=?,status=?,payload=?,updated_at=? WHERE id=?`, t.Name, t.Status, string(out), t.UpdatedAt, id); err != nil {
		return err
	}
	if kind != "" {
		data := []byte(`{}`)
		if len(eventData) > 0 {
			data, err = json.Marshal(eventData[0])
			if err != nil {
				return err
			}
		}
		if _, err = tx.Exec(`INSERT INTO assessment_events(assessment_id,step_id,kind,message,data) VALUES(?,?,?,?,?)`, id, stepID, kind, message, string(data)); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *Store) Event(id, stepID, kind, message string, data any) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(`INSERT INTO assessment_events(assessment_id,step_id,kind,message,data) VALUES(?,?,?,?,?)`, id, stepID, kind, clip(message, 20000), string(b))
	return err
}
func (s *Store) Events(id string, after int64) ([]Event, error) {
	rows, err := s.DB.Query(`SELECT id,step_id,kind,message,data,created_at FROM assessment_events WHERE assessment_id=? AND id>? ORDER BY id LIMIT 200`, id, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	bytes := 0
	for rows.Next() {
		var e Event
		var b string
		if err = rows.Scan(&e.ID, &e.StepID, &e.Kind, &e.Message, &b, &e.CreatedAt); err != nil {
			return nil, err
		}
		size := len(b) + len(e.Message) + len(e.StepID) + len(e.Kind) + 128
		if bytes+size > 1<<20 && len(out) > 0 {
			break
		}
		bytes += size
		e.Data = json.RawMessage(b)
		out = append(out, e)
	}
	return out, rows.Err()
}
func (s *Store) List(user, scope string, limit, offset int) ([]*Task, error) {
	q := `SELECT json_remove(payload, '$.steps', '$.material') FROM assessments a WHERE 1=1`
	args := []any{}
	if scope != database.RBACScopeAll {
		if scope == database.RBACScopeOwn {
			q += ` AND (a.owner_user_id=? OR EXISTS(SELECT 1 FROM rbac_resource_assignments r WHERE r.user_id=? AND r.resource_type='assessment' AND r.resource_id=a.id))`
			args = append(args, user, user)
		} else {
			q += ` AND EXISTS(SELECT 1 FROM rbac_resource_assignments r WHERE r.user_id=? AND r.resource_type='assessment' AND r.resource_id=a.id)`
			args = append(args, user)
		}
	}
	q += ` ORDER BY updated_at DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Task{}
	for rows.Next() {
		var b string
		if err = rows.Scan(&b); err != nil {
			return nil, err
		}
		var t Task
		if err = json.Unmarshal([]byte(b), &t); err != nil {
			return nil, err
		}
		out = append(out, &t)
	}
	return out, rows.Err()
}
func (s *Store) Delete(id string) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var status, payload string
	if err = tx.QueryRow(`SELECT status,payload FROM assessments WHERE id=?`, id).Scan(&status, &payload); err != nil {
		return err
	}
	var task Task
	if err = json.Unmarshal([]byte(payload), &task); err != nil {
		return err
	}
	if task.Deadline > 0 || status == "running" || status == "awaiting_approval" || status == "pausing" || status == "stopping" {
		return ErrConflict
	}
	if _, err = tx.Exec(`DELETE FROM rbac_resource_assignments WHERE resource_type='assessment' AND resource_id=?`, id); err != nil {
		return err
	}
	if _, err = tx.Exec(`DELETE FROM assessments WHERE id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// Recover never replays an interrupted external action automatically.
func (s *Store) Recover() error {
	rows, err := s.DB.Query(`SELECT id,payload FROM assessments WHERE status IN ('running','awaiting_approval','pausing','stopping','completed','partial','blocked','failed','exhausted')`)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id, payload string
		if err = rows.Scan(&id, &payload); err != nil {
			rows.Close()
			return err
		}
		var task Task
		if err = json.Unmarshal([]byte(payload), &task); err != nil {
			rows.Close()
			return err
		}
		if task.Deadline > 0 || task.Status == "running" || task.Status == "awaiting_approval" || task.Status == "pausing" || task.Status == "stopping" {
			ids = append(ids, id)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err = s.Change(id, "", "recovery", "Server restarted; review interrupted work before resuming", func(t *Task) error {
			settleTime(t)
			t.Tokens += t.Reserved
			t.Reserved = 0
			if t.Status == "stopping" {
				t.Status = "stopped"
			} else {
				t.Status = "paused"
			}
			t.Reason = "server_restarted"
			for _, st := range t.Steps {
				if st.Status == "running" {
					interrupt(st, "Server restarted; external effects may be incomplete. Explicit retry required.")
				}
			}
			return nil
		}); err != nil {
			return fmt.Errorf("recover %s: %w", id, err)
		}
	}
	return nil
}
func settleTime(t *Task) {
	if t.Deadline > 0 {
		t.RemainingSeconds = t.Deadline - time.Now().Unix()
		if t.RemainingSeconds < 0 {
			t.RemainingSeconds = 0
		}
		t.Deadline = 0
	}
}
func interrupt(s *Step, reason string) {
	s.Status = "blocked"
	s.Result = reason
	clearApproval(s)
	s.Pending = nil
	// Trace is retained as evidence; retry starts a fresh bounded session.
}
