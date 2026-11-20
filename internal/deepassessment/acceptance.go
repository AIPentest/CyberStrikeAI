package deepassessment

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// AcceptanceModePerItem requires every acceptance line to have evidence before completion.
const AcceptanceModePerItem = "per_item"

const AcceptanceSourcePageSize = 20

type AcceptanceMapping struct {
	FactIDs     []string `json:"fact_ids"`
	ReceiptIDs  []string `json:"receipt_ids"`
	NeedsReview bool     `json:"needs_review"`
	ReviewNote  string   `json:"review_note"`
	Revision    int64    `json:"revision"`
}

type AcceptanceSource struct {
	ID         string   `json:"id"`
	StepID     string   `json:"step_id"`
	Title      string   `json:"title"`
	State      string   `json:"state"`
	ReceiptIDs []string `json:"receipt_ids,omitempty"`
	AssetIDs   []string `json:"-"`
	CreatedAt  string   `json:"created_at,omitempty"`
}

type AcceptanceRow struct {
	Number int    `json:"number"`
	Text   string `json:"text"`
	Status string `json:"status"`
	AcceptanceMapping
	Facts    []AcceptanceSource `json:"facts"`
	Receipts []AcceptanceSource `json:"receipts"`
}

func acceptanceCriteria(t *Task) []string {
	text := strings.TrimSpace(t.Criteria)
	if text == "" || text == strings.TrimSpace(t.Objective) {
		return []string{strings.TrimSpace(t.Objective)}
	}
	var criteria []string
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			criteria = append(criteria, line)
		}
	}
	return criteria
}

type acceptanceQueryer interface {
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

func queryAcceptanceSources(db acceptanceQueryer, task, kind, suffix string, args ...any) ([]AcceptanceSource, error) {
	var query string
	switch kind {
	case "fact":
		query = `SELECT id,step_id,summary,confidence,evidence_json,assets_json,created_at FROM deep_assessment_facts WHERE task_id=?`
	case "receipt":
		query = `SELECT id,step_id,tool,status,'[]','[]',updated_at FROM deep_assessment_calls WHERE task_id=?
			AND ` + originalEvidenceTool
	default:
		return nil, fmt.Errorf("invalid evidence source kind")
	}
	rows, err := db.Query(query+suffix, append([]any{task}, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sources := []AcceptanceSource{}
	for rows.Next() {
		var item AcceptanceSource
		var refs, assets string
		if err = rows.Scan(&item.ID, &item.StepID, &item.Title, &item.State, &refs, &assets, &item.CreatedAt); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(refs), &item.ReceiptIDs); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(assets), &item.AssetIDs); err != nil {
			return nil, err
		}
		sources = append(sources, item)
	}
	return sources, rows.Err()
}

func (s *Store) AcceptanceSources(task, kind, search string, offset int) ([]AcceptanceSource, error) {
	column, order := "summary", "created_at"
	if kind == "receipt" {
		column, order = "tool", "updated_at"
	}
	search = Clip(strings.TrimSpace(search), 300)
	return queryAcceptanceSources(s.DB, task, kind, " AND (instr(lower("+column+"),lower(?))>0 OR instr(id,?)>0 OR instr(step_id,?)>0) ORDER BY "+order+" DESC,id LIMIT ? OFFSET ?", search, search, search, AcceptanceSourcePageSize+1, max(0, offset))
}

func acceptanceSourcesByID(db acceptanceQueryer, task, kind string, ids []string) (map[string]AcceptanceSource, error) {
	out := make(map[string]AcceptanceSource, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	data, _ := json.Marshal(ids)
	sources, err := queryAcceptanceSources(db, task, kind, ` AND id IN (SELECT value FROM json_each(?))`, string(data))
	for _, source := range sources {
		out[source.ID] = source
	}
	return out, err
}

// AcceptanceGate is the completion rule for one saved task version.
// A task with no per-item mode and no saved mapping is legacy: the model verdict
// can still finish it. Enabling per-item acceptance, or saving any mapping, keeps
// the task incomplete while any required line is missing evidence or waiting for review.
type AcceptanceGate struct {
	Legacy   bool
	Blocking bool
	Version  string
	Reason   string
}

func (s *Store) AcceptanceGate(t *Task) (AcceptanceGate, error) {
	return acceptanceGate(s.DB, t)
}

func acceptanceGate(db acceptanceQueryer, t *Task) (AcceptanceGate, error) {
	var saved int
	if err := db.QueryRow(`SELECT COUNT(*) FROM deep_assessment_acceptance WHERE task_id=?`, t.ID).Scan(&saved); err != nil {
		return AcceptanceGate{}, err
	}
	matrix, err := acceptanceMatrix(db, t)
	if err != nil {
		return AcceptanceGate{}, err
	}
	gate := AcceptanceGate{Legacy: t.AcceptanceMode != AcceptanceModePerItem && saved == 0, Version: acceptanceVersion(t, matrix)}
	if gate.Legacy {
		return gate, nil
	}
	for _, row := range matrix {
		switch row.Status {
		case "missing":
			gate.Blocking = true
			gate.Reason = "仍有必验项没有对应证据"
		case "needs_review":
			gate.Blocking = true
			if gate.Reason == "" {
				gate.Reason = "仍有必验项待复核"
			}
		}
	}
	return gate, nil
}

func acceptanceVersion(t *Task, rows []AcceptanceRow) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n%s\n", t.AcceptanceMode, t.Criteria)
	for _, row := range rows {
		fmt.Fprintf(&b, "%d\t%s\t%d\t%t\t%s\t%s\t%s\n", row.Number, row.Status, row.Revision, row.NeedsReview, strings.Join(row.FactIDs, ","), strings.Join(row.ReceiptIDs, ","), row.ReviewNote)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

func (s *Store) AcceptanceMatrix(t *Task) ([]AcceptanceRow, error) {
	return acceptanceMatrix(s.DB, t)
}

func acceptanceMatrix(db acceptanceQueryer, t *Task) ([]AcceptanceRow, error) {
	criteria := acceptanceCriteria(t)
	matrix := make([]AcceptanceRow, len(criteria))
	for i, text := range criteria {
		matrix[i] = AcceptanceRow{Number: i + 1, Text: text, Status: "missing"}
	}
	rows, err := db.Query(`SELECT criterion,revision,mapping FROM deep_assessment_acceptance WHERE task_id=? ORDER BY criterion`, t.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var factIDs, receiptIDs []string
	for rows.Next() {
		var number int
		var revision int64
		var mapping string
		if err = rows.Scan(&number, &revision, &mapping); err != nil {
			return nil, err
		}
		if number < 1 || number > len(matrix) {
			continue
		}
		row := &matrix[number-1]
		if err = json.Unmarshal([]byte(mapping), &row.AcceptanceMapping); err != nil {
			return nil, err
		}
		row.Revision = revision
		factIDs = append(factIDs, row.FactIDs...)
		receiptIDs = append(receiptIDs, row.ReceiptIDs...)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	facts, err := acceptanceSourcesByID(db, t.ID, "fact", factIDs)
	if err != nil {
		return nil, err
	}
	for _, fact := range facts {
		receiptIDs = append(receiptIDs, fact.ReceiptIDs...)
	}
	receipts, err := acceptanceSourcesByID(db, t.ID, "receipt", receiptIDs)
	if err != nil {
		return nil, err
	}
	for i := range matrix {
		resolveAcceptanceRow(&matrix[i], facts, receipts)
	}
	return matrix, nil
}

func resolveAcceptanceRow(row *AcceptanceRow, facts, receipts map[string]AcceptanceSource) {
	review := row.NeedsReview
	seen := map[string]bool{}
	for _, id := range row.ReceiptIDs {
		receipt, ok := receipts[id]
		review = review || !ok || receipt.State != "completed"
		if ok {
			row.Receipts = append(row.Receipts, receipt)
			seen[id] = true
		}
	}
	for _, id := range row.FactIDs {
		fact, ok := facts[id]
		if !ok {
			review = true
			continue
		}
		review = review || fact.State != "observed" || len(fact.ReceiptIDs) == 0
		validIDs := make([]string, 0, len(fact.ReceiptIDs))
		for _, ref := range fact.ReceiptIDs {
			receipt, ok := receipts[ref]
			if !ok || receipt.StepID != fact.StepID {
				review = true
				continue
			}
			review = review || receipt.State != "completed"
			validIDs = append(validIDs, ref)
			if !seen[ref] {
				row.Receipts = append(row.Receipts, receipt)
				seen[ref] = true
			}
		}
		fact.ReceiptIDs = validIDs
		row.Facts = append(row.Facts, fact)
	}
	switch {
	case review:
		row.Status = "needs_review"
	case len(row.Receipts) > 0:
		row.Status = "has_evidence"
	}
}

func (s *Store) SaveAcceptanceMapping(t *Task, number int, mapping AcceptanceMapping) error {
	if number < 1 || number > len(acceptanceCriteria(t)) {
		return sql.ErrNoRows
	}
	mapping.ReviewNote = strings.TrimSpace(mapping.ReviewNote)
	if len(mapping.FactIDs) > 32 || len(mapping.ReceiptIDs) > 32 || len([]rune(mapping.ReviewNote)) > 2000 || mapping.Revision < 0 {
		return fmt.Errorf("验收项最多关联 32 条事实、32 条直接回执，复核说明最多 2000 字符")
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var revision int64
	if err = tx.QueryRow(`SELECT COALESCE((SELECT revision FROM deep_assessment_acceptance WHERE task_id=? AND criterion=?),0)`, t.ID, number).Scan(&revision); err != nil {
		return err
	}
	if mapping.Revision != revision {
		return ErrConflict
	}
	for _, source := range []struct {
		kind string
		ids  *[]string
	}{{"fact", &mapping.FactIDs}, {"receipt", &mapping.ReceiptIDs}} {
		ids := slices.Clone(*source.ids)
		slices.Sort(ids)
		ids = slices.Compact(ids)
		matches, err := acceptanceSourcesByID(tx, t.ID, source.kind, ids)
		if err != nil {
			return err
		}
		if len(matches) != len(ids) {
			return fmt.Errorf("证据不存在、不是原始回执或不属于本次测评")
		}
		*source.ids = ids
	}
	mapping.Revision = 0 // The column is the source of truth for optimistic edits.
	data, _ := json.Marshal(mapping)
	if _, err = tx.Exec(`INSERT INTO deep_assessment_acceptance(task_id,criterion,revision,mapping) VALUES(?,?,?,?)
		ON CONFLICT(task_id,criterion) DO UPDATE SET revision=excluded.revision,mapping=excluded.mapping`, t.ID, number, revision+1, string(data)); err != nil {
		return err
	}
	return tx.Commit()
}
