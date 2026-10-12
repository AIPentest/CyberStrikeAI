package database

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const LearningExtractorVersion = "draft-v1"

type LearningJob struct {
	ID               string `json:"id"`
	ConversationID   string `json:"conversationId"`
	ProjectID        string `json:"projectId,omitempty"`
	OwnerUserID      string `json:"-"`
	AnchorMessageID  string `json:"anchorMessageId"`
	RunStatus        string `json:"runStatus"`
	Outcome          string `json:"outcome"`
	Status           string `json:"status"`
	Attempts         int    `json:"attempts"`
	CandidateJSON    string `json:"candidateJson,omitempty"`
	Reason           string `json:"reason,omitempty"`
	PromptTokens     int    `json:"promptTokens"`
	CompletionTokens int    `json:"completionTokens"`
	CreatedAt        int64  `json:"createdAt"`
	UpdatedAt        int64  `json:"updatedAt"`
	LeaseToken       string `json:"-"`
	StartRowID       int64  `json:"-"`
	EndRowID         int64  `json:"-"`
}

type LearningEvidence struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Text      string `json:"text"`
	Truncated bool   `json:"truncated,omitempty"`
}

// BeginLearningRun captures identity and the user-message anchor before the run
// starts. No transcript copies or files are created.
func (db *DB) BeginLearningRun(runID, conversationID string) error {
	now := time.Now().Unix()
	_, err := db.Exec(`INSERT OR IGNORE INTO learning_jobs
    (id,extractor_version,conversation_id,anchor_message_id,project_id,owner_user_id,start_rowid,created_at,updated_at)
    SELECT ?,?,c.id,m.id,c.project_id,c.owner_user_id,m.rowid,?,?
    FROM conversations c JOIN messages m ON m.conversation_id=c.id
    WHERE c.id=? AND m.role='user' AND COALESCE(c.owner_user_id,'')<>''
    AND (SELECT COUNT(*) FROM learning_jobs WHERE status IN ('waiting','queued','processing'))<200
    AND m.rowid=(SELECT MAX(rowid) FROM messages WHERE conversation_id=c.id AND role='user')`,
		runID, LearningExtractorVersion, now, now, conversationID)
	return err
}

// FinishLearningRun is idempotent. Unknown business outcomes never become
// successful just because the execution completed.
func (db *DB) FinishLearningRun(runID, status string) error {
	state, reason := "skipped", "运行未正常收尾，不提炼技能"
	if status == "completed" || status == "failed" {
		state, reason = "queued", ""
	}
	_, err := db.Exec(`UPDATE learning_jobs SET run_status=?, status=?, reason=?,
    end_rowid=COALESCE((SELECT MIN(rowid)-1 FROM messages WHERE conversation_id=learning_jobs.conversation_id AND role='user' AND rowid>start_rowid),
      (SELECT MAX(rowid) FROM messages WHERE conversation_id=learning_jobs.conversation_id),start_rowid),updated_at=?
    WHERE id=? AND status='waiting'`, status, state, reason, time.Now().Unix(), runID)
	return err
}

// RecoverLearningRuns is called before accepting requests on process startup.
// A crash cannot be interpreted as a completed task. Expired worker leases are
// recovered by ClaimLearningJob without inventing a task result.
func (db *DB) RecoverLearningRuns() error {
	_, err := db.Exec(`UPDATE learning_jobs SET status='skipped',run_status='interrupted',reason='服务中断，缺少可靠运行终态',updated_at=? WHERE status='waiting'`, time.Now().Unix())
	return err
}

const learningJobColumns = `id,conversation_id,COALESCE(project_id,''),owner_user_id,anchor_message_id,run_status,outcome,status,attempts,candidate_json,reason,prompt_tokens,completion_tokens,created_at,updated_at,lease_token,start_rowid,end_rowid`

type learningScanner interface{ Scan(...any) error }

func scanLearningJob(row learningScanner) (*LearningJob, error) {
	j := new(LearningJob)
	err := row.Scan(&j.ID, &j.ConversationID, &j.ProjectID, &j.OwnerUserID, &j.AnchorMessageID, &j.RunStatus, &j.Outcome, &j.Status, &j.Attempts, &j.CandidateJSON, &j.Reason, &j.PromptTokens, &j.CompletionTokens, &j.CreatedAt, &j.UpdatedAt, &j.LeaseToken, &j.StartRowID, &j.EndRowID)
	return j, err
}

// ClaimLearningJob uses one atomic UPDATE, so competing workers cannot share a
// lease. Daily limits count jobs, including retries of already-started jobs.
func (db *DB) ClaimLearningJob(now time.Time, dailyLimit int, lease time.Duration) (*LearningJob, error) {
	if dailyLimit < 1 || lease <= 0 {
		return nil, fmt.Errorf("invalid learning queue limits")
	}
	if _, err := db.Exec(`UPDATE learning_jobs SET status='failed',lease_token='',lease_until=0,reason='提炼重试已耗尽',updated_at=? WHERE status='processing' AND lease_until<=? AND attempts>=3`, now.Unix(), now.Unix()); err != nil {
		return nil, err
	}
	day := now.UTC().Format("2006-01-02")
	j, err := scanLearningJob(db.QueryRow(`UPDATE learning_jobs SET status='processing',attempts=attempts+1,
    lease_token=?,lease_until=?,started_day=?,updated_at=?
    WHERE id=(SELECT id FROM learning_jobs WHERE extractor_version=? AND attempts<3
      AND ((status='queued' AND next_attempt_at<=?) OR (status='processing' AND lease_until<=?))
      AND (started_day=? OR (SELECT COUNT(*) FROM learning_jobs WHERE started_day=?)<?)
      ORDER BY created_at,id LIMIT 1) RETURNING `+learningJobColumns,
		uuid.NewString(), now.Add(lease).Unix(), day, now.Unix(), LearningExtractorVersion, now.Unix(), now.Unix(), day, day, dailyLimit))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return j, err
}

// LearningSourceValid rechecks both the original scope and current permissions.
func (db *DB) LearningSourceValid(j *LearningJob) bool {
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM learning_jobs j JOIN conversations c ON c.id=j.conversation_id
    JOIN messages m ON m.id=j.anchor_message_id WHERE j.id=? AND j.lease_token=? AND j.status='processing'
    AND c.owner_user_id=j.owner_user_id AND COALESCE(c.project_id,'')=COALESCE(j.project_id,'')`, j.ID, j.LeaseToken).Scan(&n); err != nil || n != 1 {
		return false
	}
	a, err := db.ResolveRBACAccess(j.OwnerUserID)
	if err != nil || !a.User.Enabled || !a.Permissions["chat:read"] || !db.UserCanAccessResource(j.OwnerUserID, a.PermissionScopes["chat:read"], "conversation", j.ConversationID) {
		return false
	}
	return j.ProjectID == "" || (a.Permissions["project:read"] && db.UserCanAccessResource(j.OwnerUserID, a.PermissionScopes["project:read"], "project", j.ProjectID))
}

// LearningRunEvidence reads a bounded slice of source messages/tool results.
// The message rowid boundary prevents a later run leaking into a delayed job.
func (db *DB) LearningRunEvidence(j *LearningJob, maxRunes int) ([]LearningEvidence, error) {
	if maxRunes < 1 || maxRunes > 32000 {
		return nil, fmt.Errorf("invalid evidence budget")
	}
	perItem := maxRunes / 4
	if perItem > 2000 {
		perItem = 2000
	}
	if perItem < 1 {
		perItem = 1
	}
	rows, err := db.Query(`SELECT id,kind,text FROM (
    SELECT m.id,m.role AS kind,substr(m.content,1,?) AS text,m.rowid AS seq,0 AS sub
      FROM messages m WHERE m.conversation_id=? AND m.rowid BETWEEN ? AND ?
    UNION ALL
    SELECT p.id,p.event_type,substr(COALESCE(p.data,p.message,''),1,?),m.rowid,p.rowid
      FROM process_details p JOIN messages m ON m.id=p.message_id
      WHERE m.conversation_id=? AND m.rowid BETWEEN ? AND ? AND p.event_type IN ('tool_result','error')
    ) ORDER BY seq,sub LIMIT 100`, perItem+1, j.ConversationID, j.StartRowID, j.EndRowID, perItem+1, j.ConversationID, j.StartRowID, j.EndRowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LearningEvidence{}
	for rows.Next() {
		var e LearningEvidence
		if err := rows.Scan(&e.ID, &e.Kind, &e.Text); err != nil {
			return nil, err
		}
		r := []rune(e.Text)
		if len(r) > perItem {
			r = r[:perItem]
			e.Truncated = true
		}
		if len(r) > maxRunes {
			r = r[:maxRunes]
			e.Truncated = true
		}
		e.Text = string(r)
		maxRunes -= len(r)
		out = append(out, e)
		if maxRunes <= 0 {
			break
		}
	}
	return out, rows.Err()
}

func (db *DB) CompleteLearningJob(j *LearningJob, status, candidate, hash, reason string, promptTokens, completionTokens int) error {
	if status != "drafted" && status != "skipped" {
		return fmt.Errorf("invalid learning result")
	}
	if !db.LearningSourceValid(j) {
		status, candidate, hash, reason = "skipped", "", "", "来源已删除、范围变更或权限失效"
	}
	if status == "drafted" && !db.learningCandidateEvidenceValid(j, candidate) {
		status, candidate, hash, reason = "skipped", "", "", "来源证据已删除或无效"
	}
	// Exact duplicate suppression is deliberately conversation-scoped; no
	// implicit cross-project sharing or deletion dependencies are introduced.
	if hash != "" {
		if err := db.InvalidateLearningDrafts(j.ConversationID); err != nil {
			return err
		}
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM learning_jobs WHERE conversation_id=? AND id<>? AND content_hash=? AND status='drafted'`, j.ConversationID, j.ID, hash).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			status, candidate, hash, reason = "skipped", "", "", "相同会话已有相同技能草稿"
		}
	}
	_, err := db.Exec(`UPDATE learning_jobs SET status=?,candidate_json=?,content_hash=?,reason=?,prompt_tokens=prompt_tokens+?,completion_tokens=completion_tokens+?,lease_token='',lease_until=0,updated_at=?
    WHERE id=? AND lease_token=? AND status='processing'
    AND (?='skipped' OR EXISTS(SELECT 1 FROM conversations c WHERE c.id=learning_jobs.conversation_id AND c.owner_user_id=learning_jobs.owner_user_id AND COALESCE(c.project_id,'')=COALESCE(learning_jobs.project_id,'')))`,
		status, candidate, hash, reason, promptTokens, completionTokens, time.Now().Unix(), j.ID, j.LeaseToken, status)
	return err
}

func (db *DB) RetryLearningJob(j *LearningJob, reason string, now time.Time) error {
	status := "queued"
	if j.Attempts >= 3 {
		status = "failed"
	}
	_, err := db.Exec(`UPDATE learning_jobs SET status=?,reason=?,next_attempt_at=?,lease_token='',lease_until=0,updated_at=? WHERE id=? AND lease_token=? AND status='processing'`,
		status, reason, now.Add(time.Minute).Unix(), now.Unix(), j.ID, j.LeaseToken)
	return err
}

func (db *DB) ListLearningJobs(conversationID string) ([]LearningJob, error) {
	if err := db.InvalidateLearningDrafts(conversationID); err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT `+learningJobColumns+` FROM learning_jobs j WHERE conversation_id=?
    AND EXISTS(SELECT 1 FROM conversations c WHERE c.id=j.conversation_id AND c.owner_user_id=j.owner_user_id AND COALESCE(c.project_id,'')=COALESCE(j.project_id,''))
    ORDER BY created_at DESC,id LIMIT 50`, strings.TrimSpace(conversationID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	jobs := []LearningJob{}
	for rows.Next() {
		j, err := scanLearningJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, *j)
	}
	return jobs, rows.Err()
}

func (db *DB) learningCandidateEvidenceValid(j *LearningJob, candidate string) bool {
	var refs struct {
		IDs []string `json:"evidenceIds"`
	}
	if json.Unmarshal([]byte(candidate), &refs) != nil || len(refs.IDs) == 0 {
		return false
	}
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM json_each(?,'$.evidenceIds') e WHERE
    NOT EXISTS(SELECT 1 FROM messages m WHERE m.id=e.value AND m.conversation_id=? AND m.rowid BETWEEN ? AND ?)
    AND NOT EXISTS(SELECT 1 FROM process_details p JOIN messages m ON m.id=p.message_id WHERE p.id=e.value AND m.conversation_id=? AND m.rowid BETWEEN ? AND ?)`,
		candidate, j.ConversationID, j.StartRowID, j.EndRowID, j.ConversationID, j.StartRowID, j.EndRowID).Scan(&n)
	return err == nil && n == 0
}

// InvalidateLearningDrafts also handles removal of individual evidence records.
func (db *DB) InvalidateLearningDrafts(conversationID string) error {
	_, err := db.Exec(`UPDATE learning_jobs SET status='skipped',candidate_json='',content_hash='',reason='来源证据已删除或范围变更',updated_at=?
    WHERE conversation_id=? AND status='drafted' AND (
      NOT EXISTS(SELECT 1 FROM conversations c WHERE c.id=learning_jobs.conversation_id AND c.owner_user_id=learning_jobs.owner_user_id AND COALESCE(c.project_id,'')=COALESCE(learning_jobs.project_id,''))
      OR EXISTS(SELECT 1 FROM json_each(learning_jobs.candidate_json,'$.evidenceIds') e WHERE
        NOT EXISTS(SELECT 1 FROM messages m WHERE m.id=e.value AND m.conversation_id=learning_jobs.conversation_id AND m.rowid BETWEEN start_rowid AND end_rowid)
        AND NOT EXISTS(SELECT 1 FROM process_details p JOIN messages m ON m.id=p.message_id WHERE p.id=e.value AND m.conversation_id=learning_jobs.conversation_id AND m.rowid BETWEEN start_rowid AND end_rowid)))`, time.Now().Unix(), conversationID)
	return err
}

func (db *DB) RecordLearningUsage(j *LearningJob, prompt, completion int) error {
	if prompt < 0 || completion < 0 {
		return fmt.Errorf("invalid usage")
	}
	_, err := db.Exec(`UPDATE learning_jobs SET prompt_tokens=prompt_tokens+?,completion_tokens=completion_tokens+? WHERE id=? AND lease_token=? AND status='processing'`, prompt, completion, j.ID, j.LeaseToken)
	return err
}

func (db *DB) PruneLearningJobs(now time.Time) error {
	_, err := db.Exec(`DELETE FROM learning_jobs WHERE created_at<? AND (status<>'processing' OR lease_until<=?)`, now.Add(-30*24*time.Hour).Unix(), now.Unix())
	return err
}
