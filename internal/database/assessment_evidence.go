package database

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"strings"
	"time"
)

type AssessmentFindingReview struct {
	ID        string    `json:"id"`
	StepID    string    `json:"step_id"`
	Verdict   string    `json:"verdict"`
	Reason    string    `json:"reason"`
	Evidence  []string  `json:"evidence"`
	CreatedAt time.Time `json:"created_at"`
}
type assessmentQuery interface{ QueryRow(string, ...any) *sql.Row }

func assessmentSession(q assessmentQuery, conversation string) (string, string, error) {
	var task, step string
	err := q.QueryRow("SELECT task_id,step_id FROM deep_assessment_sessions s WHERE conversation_id=? AND (SELECT COUNT(*) FROM deep_assessment_sessions x WHERE x.conversation_id=s.conversation_id)=1 AND NOT EXISTS(SELECT 1 FROM batch_tasks b WHERE b.conversation_id=s.conversation_id)", conversation).Scan(&task, &step)
	return task, step, err
}
func assessmentReceipts(q assessmentQuery, task, step string, refs []string) ([]string, error) {
	if len(refs) == 0 || len(refs) > 32 {
		return nil, fmt.Errorf("深度测评漏洞须引用 1–32 条本意图的真实执行回执；推断或特征匹配请记录为 inferred 事实")
	}
	out := []string{}
	seen := map[string]bool{}
	for _, ref := range refs {
		var id string
		err := q.QueryRow("SELECT id FROM deep_assessment_calls WHERE task_id=? AND step_id=? AND (id=? OR execution_id=?) AND status='completed' AND tool NOT LIKE 'assessment_%' AND tool NOT IN ('record_vulnerability','list_vulnerabilities','get_vulnerability')", task, step, ref, ref).Scan(&id)
		if err != nil {
			return nil, fmt.Errorf("证据 %s 不属于本意图的成功原始执行回执", ref)
		}
		if !seen[id] {
			out = append(out, id)
			seen[id] = true
		}
	}
	return out, nil
}
func (db *DB) ValidateAssessmentFinding(conversation string, refs []string) error {
	task, step, err := assessmentSession(db, conversation)
	if err == sql.ErrNoRows {
		assessment, checkErr := db.IsAssessmentConversation(conversation)
		if checkErr != nil {
			return checkErr
		}
		if assessment {
			return fmt.Errorf("测评会话来源不唯一，不能登记漏洞")
		}
		return nil
	}
	if err != nil {
		return err
	}
	_, err = assessmentReceipts(db, task, step, refs)
	return err
}
func (db *DB) IsAssessmentConversation(conversation string) (bool, error) {
	var found bool
	err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM deep_assessment_sessions WHERE conversation_id=?) AND NOT EXISTS(SELECT 1 FROM batch_tasks WHERE conversation_id=?)", conversation, conversation).Scan(&found)
	return found, err
}
func findingGraph(tx *sql.Tx, id, task, step, title, body, state string, refs []string) error {
	now := time.Now().UTC()
	source, _ := json.Marshal(map[string]any{"vulnerability_id": id, "evidence": refs})
	_, err := tx.Exec("INSERT INTO deep_assessment_nodes(task_id,id,step_id,kind,state,title,body,source,updated_round,updated_at) VALUES(?,?,?,'finding',?,?,?,?,0,?) ON CONFLICT(task_id,id) DO UPDATE SET state=excluded.state,body=excluded.body,source=excluded.source,updated_at=excluded.updated_at", task, "finding:"+id, step, state, title, body, string(source), now)
	if err != nil {
		return err
	}
	if _, err = tx.Exec("INSERT OR IGNORE INTO deep_assessment_edges(task_id,source_id,target_id,relation) VALUES(?,?,?,'produces')", task, "step:"+step, "finding:"+id); err != nil {
		return err
	}
	for _, ref := range refs {
		if _, err = tx.Exec("INSERT OR IGNORE INTO deep_assessment_edges(task_id,source_id,target_id,relation) VALUES(?,?,?,'supports')", task, "receipt:"+ref, "finding:"+id); err != nil {
			return err
		}
	}
	_, err = tx.Exec("INSERT INTO deep_assessment_changes(task_id,version) VALUES(?,1) ON CONFLICT(task_id) DO UPDATE SET version=version+1", task)
	return err
}
func recordAssessmentReport(tx *sql.Tx, v *Vulnerability) error {
	task, step, err := assessmentSession(tx, v.ConversationID)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	refs := []string{}
	state := "unreviewed"
	if len(v.EvidenceReceiptIDs) > 0 {
		refs, err = assessmentReceipts(tx, task, step, v.EvidenceReceiptIDs)
		if err != nil {
			return err
		}
		raw, _ := json.Marshal(refs)
		state = "reported"
		if _, err = tx.Exec("INSERT INTO deep_assessment_finding_reviews(id,vulnerability_id,task_id,step_id,verdict,reason,evidence_json,created_at) VALUES(?,?,?,?,'reported',?,?,?)", uuid.NewString(), v.ID, task, step, "原始复现回执已绑定，待独立复核。", string(raw), time.Now().UTC()); err != nil {
			return err
		}
	}
	return findingGraph(tx, v.ID, task, step, v.Title, "查看关联漏洞中的复现步骤、证据及独立复核记录。", state, refs)
}
func (db *DB) ReviewAssessmentFinding(task, step, id, verdict, reason string, refs []string, requestIDs ...string) error {
	if !map[string]bool{"confirmed": true, "refuted": true, "inconclusive": true}[verdict] || strings.TrimSpace(reason) == "" || len(reason) > 8000 {
		return fmt.Errorf("invalid independent review")
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var origin, title, conversation string
	if err = tx.QueryRow("SELECT s.step_id,v.title,s.conversation_id FROM vulnerability_assessment_sources s JOIN vulnerabilities v ON v.id=s.vulnerability_id WHERE s.assessment_id=? AND s.vulnerability_id=?", task, id).Scan(&origin, &title, &conversation); err != nil {
		return err
	}
	if origin == step {
		return fmt.Errorf("独立复核必须由另一条意图执行，不能以原报告会话自证")
	}
	var running int
	if err = tx.QueryRow("SELECT COUNT(*) FROM deep_assessments d,json_each(d.payload,'$.steps') st WHERE d.id=? AND json_extract(st.value,'$.id')=? AND json_extract(st.value,'$.status')='running'", task, step).Scan(&running); err != nil {
		return err
	}
	if running != 1 {
		return fmt.Errorf("复核意图未执行")
	}
	canonical, err := assessmentReceipts(tx, task, step, refs)
	if err != nil {
		return err
	}
	raw, _ := json.Marshal(canonical)
	reviewID := uuid.NewString()
	if len(requestIDs) > 0 && requestIDs[0] != "" {
		reviewID = fmt.Sprintf("review:%x", sha256.Sum256([]byte(task+"\n"+step+"\n"+requestIDs[0])))
	}
	result, err := tx.Exec("INSERT INTO deep_assessment_finding_reviews(id,vulnerability_id,task_id,step_id,verdict,reason,evidence_json,created_at) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING", reviewID, id, task, step, verdict, reason, string(raw), time.Now().UTC())
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return tx.Commit()
	}
	if verdict == "confirmed" || verdict == "refuted" {
		status := "confirmed"
		if verdict == "refuted" {
			status = "false_positive"
		}
		if _, err = tx.Exec("UPDATE vulnerabilities SET status=?,updated_at=? WHERE id=? AND status NOT IN ('fixed','ignored')", status, time.Now().UTC(), id); err != nil {
			return err
		}
	}
	// Keep the original producing intent; review edges identify the independent path.
	if err = findingGraph(tx, id, task, origin, title, reason, verdict, canonical); err != nil {
		return err
	}
	if _, err = tx.Exec("INSERT OR IGNORE INTO deep_assessment_edges(task_id,source_id,target_id,relation) VALUES(?,?,?,'reviews')", task, "step:"+step, "finding:"+id); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	db.refreshAssetRiskCacheForConversationsBestEffort(conversation)
	return nil
}
func (db *DB) AssessmentFindingReviews(id string) ([]AssessmentFindingReview, error) {
	rows, err := db.Query("SELECT id,step_id,verdict,reason,evidence_json,created_at FROM deep_assessment_finding_reviews WHERE vulnerability_id=? ORDER BY created_at DESC,id DESC LIMIT 100", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AssessmentFindingReview{}
	for rows.Next() {
		var r AssessmentFindingReview
		var raw string
		if err = rows.Scan(&r.ID, &r.StepID, &r.Verdict, &r.Reason, &raw, &r.CreatedAt); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &r.Evidence); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
