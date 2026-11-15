package deepassessment

import (
	"cyberstrike-ai/internal/database"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"strings"
	"time"
)

type Fact struct {
	ID         string    `json:"id"`
	StepID     string    `json:"step_id"`
	Confidence string    `json:"confidence"`
	Summary    string    `json:"summary"`
	Body       string    `json:"body"`
	Evidence   []string  `json:"evidence"`
	AssetIDs   []string  `json:"asset_ids"`
	Sources    []string  `json:"source_ids"`
	CreatedAt  time.Time `json:"created_at"`
}

func Signal(tx *sql.Tx, task string) error {
	_, err := tx.Exec("INSERT INTO deep_assessment_changes(task_id,version) VALUES(?,1) ON CONFLICT(task_id) DO UPDATE SET version=version+1", task)
	return err
}
func (s *Store) ChangeVersion(task string) (int64, error) {
	var n int64
	err := s.DB.QueryRow("SELECT COALESCE((SELECT version FROM deep_assessment_changes WHERE task_id=?),0)", task).Scan(&n)
	return n, err
}
func (s *Store) BindAssets(task string, ids []string) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	changed := false
	for _, id := range ids {
		r, err := tx.Exec("INSERT OR IGNORE INTO deep_assessment_assets(task_id,asset_id) VALUES(?,?)", task, id)
		if err != nil {
			return err
		}
		n, _ := r.RowsAffected()
		changed = changed || n > 0
	}
	if changed {
		if err = Signal(tx, task); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Callers authorize assets before invoking this; snapshots never appear in public task payloads.
func (s *Store) IntentAssets(task, step string, assets []*database.Asset) ([]json.RawMessage, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	out := []json.RawMessage{}
	for _, a := range assets {
		raw, err := json.Marshal(a)
		if err != nil {
			return nil, err
		}
		if _, err = tx.Exec("INSERT OR IGNORE INTO deep_assessment_asset_snapshots(task_id,step_id,asset_id,snapshot) VALUES(?,?,?,?)", task, step, a.ID, string(raw)); err != nil {
			return nil, err
		}
		var snapshot string
		if err = tx.QueryRow("SELECT snapshot FROM deep_assessment_asset_snapshots WHERE task_id=? AND step_id=? AND asset_id=?", task, step, a.ID).Scan(&snapshot); err != nil {
			return nil, err
		}
		out = append(out, json.RawMessage(snapshot))
	}
	return out, tx.Commit()
}
func (s *Store) AddFact(task string, f *Fact) error {
	if strings.TrimSpace(f.Summary) == "" || len(f.Summary) > 2000 || len(f.Body) > 24000 || len(f.AssetIDs) > 32 || len(f.Sources) > 32 || len(f.Evidence) > 32 {
		return fmt.Errorf("invalid fact")
	}
	if f.Confidence != "observed" && f.Confidence != "inferred" {
		return fmt.Errorf("confidence must be observed or inferred")
	}
	if f.Confidence == "observed" && len(f.Evidence) == 0 {
		return fmt.Errorf("observed fact requires original execution receipts")
	}
	if f.Confidence == "inferred" && len(f.Sources) == 0 && len(f.Evidence) == 0 {
		return fmt.Errorf("inference requires source nodes or receipts")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var raw string
	if err = tx.QueryRow("SELECT payload FROM deep_assessments WHERE id=?", task).Scan(&raw); err != nil {
		return err
	}
	var t Task
	if err = json.Unmarshal([]byte(raw), &t); err != nil {
		return err
	}
	st := t.Step(f.StepID)
	if st == nil || st.Status != "running" {
		return fmt.Errorf("fact must belong to a running intent")
	}
	for _, id := range f.AssetIDs {
		var count int
		if err = tx.QueryRow("SELECT COUNT(*) FROM deep_assessment_assets WHERE task_id=? AND asset_id=?", task, id).Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			return fmt.Errorf("asset is not anchored to this assessment")
		}
	}
	receipts := []string{}
	for _, ref := range f.Evidence {
		var id string
		err = tx.QueryRow("SELECT id FROM deep_assessment_calls WHERE task_id=? AND step_id=? AND (id=? OR execution_id=?) AND status='completed' AND tool NOT LIKE 'assessment_%' AND tool NOT IN ('record_vulnerability','list_vulnerabilities','get_vulnerability')", task, f.StepID, ref, ref).Scan(&id)
		if err != nil {
			return fmt.Errorf("receipt %s is not a successful original operation in this intent", ref)
		}
		receipts = append(receipts, id)
	}
	for _, ref := range f.Sources {
		var count int
		if err = tx.QueryRow("SELECT COUNT(*) FROM deep_assessment_nodes WHERE task_id=? AND id=?", task, ref).Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			return fmt.Errorf("source node is not in this assessment")
		}
	}
	if f.ID == "" {
		f.ID = "fact:" + uuid.NewString()
	}
	f.CreatedAt = time.Now().UTC()
	f.Evidence = receipts
	evidence, _ := json.Marshal(f.Evidence)
	assets, _ := json.Marshal(f.AssetIDs)
	sources, _ := json.Marshal(f.Sources)
	result, err := tx.Exec("INSERT INTO deep_assessment_facts(id,task_id,step_id,confidence,summary,body,evidence_json,assets_json,sources_json,created_at) VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING", f.ID, task, f.StepID, f.Confidence, f.Summary, f.Body, string(evidence), string(assets), string(sources), f.CreatedAt)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		err = tx.QueryRow("SELECT created_at FROM deep_assessment_facts WHERE id=? AND task_id=? AND step_id=? AND confidence=? AND summary=? AND body=? AND evidence_json=? AND assets_json=? AND sources_json=?", f.ID, task, f.StepID, f.Confidence, f.Summary, f.Body, string(evidence), string(assets), string(sources)).Scan(&f.CreatedAt)
		if err != nil {
			return fmt.Errorf("fact ID already exists with different content")
		}
		return tx.Commit()
	}
	source, _ := json.Marshal(map[string]any{"evidence": f.Evidence, "asset_ids": f.AssetIDs, "source_ids": f.Sources, "confidence": f.Confidence})
	if err = graphNode(tx, task, Node{ID: f.ID, StepID: f.StepID, Kind: "fact", State: f.Confidence, Title: f.Summary, Body: f.Body, Source: source, Round: t.Rounds}); err != nil {
		return err
	}
	if err = graphEdge(tx, task, stepNode(f.StepID), f.ID, "produces"); err != nil {
		return err
	}
	for _, ref := range receipts {
		if err = graphEdge(tx, task, "receipt:"+ref, f.ID, "supports"); err != nil {
			return err
		}
	}
	for _, ref := range f.Sources {
		if err = graphEdge(tx, task, ref, f.ID, "derives"); err != nil {
			return err
		}
	}
	if err = Signal(tx, task); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) Facts(task, confidence string, offset int) ([]Fact, error) {
	if offset < 0 {
		offset = 0
	}
	rows, err := s.DB.Query("SELECT id,step_id,confidence,summary,body,evidence_json,assets_json,sources_json,created_at FROM deep_assessment_facts WHERE task_id=? AND (?='' OR confidence=?) ORDER BY created_at DESC,id DESC LIMIT 20 OFFSET ?", task, confidence, confidence, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Fact{}
	for rows.Next() {
		var f Fact
		var ev, assets, sources string
		if err = rows.Scan(&f.ID, &f.StepID, &f.Confidence, &f.Summary, &f.Body, &ev, &assets, &sources, &f.CreatedAt); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(ev), &f.Evidence); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(assets), &f.AssetIDs); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(sources), &f.Sources); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
