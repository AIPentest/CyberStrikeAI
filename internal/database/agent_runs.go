package database

import (
	"encoding/json"
	"time"
)

// AgentRun is a durable receipt for an accepted asynchronous invocation.
type AgentRun struct {
	RunID              string          `json:"runId"`
	UserID             string          `json:"-"`
	Key                string          `json:"-"`
	Fingerprint        string          `json:"-"`
	ConversationID     string          `json:"conversationId"`
	AssistantMessageID string          `json:"assistantMessageId"`
	Status             string          `json:"status"`
	Result             json.RawMessage `json:"result,omitempty"`
	CreatedAt          string          `json:"createdAt"`
	UpdatedAt          string          `json:"updatedAt"`
}

func (db *DB) initAgentRuns() error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS agent_async_runs (
 run_id TEXT PRIMARY KEY, user_id TEXT NOT NULL, idempotency_key TEXT NOT NULL,
 fingerprint TEXT NOT NULL, conversation_id TEXT NOT NULL, assistant_message_id TEXT NOT NULL,
 status TEXT NOT NULL, result TEXT NOT NULL DEFAULT '{}', created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
 UNIQUE(user_id, idempotency_key))`)
	if err != nil {
		return err
	}
	_, err = db.Exec(`UPDATE agent_async_runs SET status='interrupted', result='{"error":"server restarted before completion"}', updated_at=? WHERE status='running'`, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}
func (db *DB) CreateAgentRun(r *AgentRun) error {
	r.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	r.UpdatedAt = r.CreatedAt
	_, err := db.Exec(`INSERT INTO agent_async_runs(run_id,user_id,idempotency_key,fingerprint,conversation_id,assistant_message_id,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, r.RunID, r.UserID, r.Key, r.Fingerprint, r.ConversationID, r.AssistantMessageID, r.Status, r.CreatedAt, r.UpdatedAt)
	return err
}
func (db *DB) GetAgentRun(id string) (*AgentRun, error) { return db.readAgentRun("run_id=?", id) }
func (db *DB) FindAgentRun(user, key string) (*AgentRun, error) {
	return db.readAgentRun("user_id=? AND idempotency_key=?", user, key)
}
func (db *DB) readAgentRun(where string, args ...interface{}) (*AgentRun, error) {
	r := &AgentRun{}
	var raw string
	err := db.QueryRow(`SELECT run_id,user_id,idempotency_key,fingerprint,conversation_id,assistant_message_id,status,result,created_at,updated_at FROM agent_async_runs WHERE `+where, args...).Scan(&r.RunID, &r.UserID, &r.Key, &r.Fingerprint, &r.ConversationID, &r.AssistantMessageID, &r.Status, &raw, &r.CreatedAt, &r.UpdatedAt)
	r.Result = json.RawMessage(raw)
	return r, err
}
func (db *DB) FinishAgentRun(id, status string, result interface{}) error {
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	_, err = db.Exec(`UPDATE agent_async_runs SET status=?,result=?,updated_at=? WHERE run_id=?`, status, string(raw), time.Now().UTC().Format(time.RFC3339Nano), id)
	return err
}
