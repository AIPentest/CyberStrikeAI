package deepassessment

import (
	"database/sql"
	"time"
)

type ToolActivity struct {
	ID          string    `json:"id"`
	StepID      string    `json:"step_id"`
	Tool        string    `json:"tool"`
	Status      string    `json:"status"`
	ExecutionID string    `json:"execution_id,omitempty"`
	StartedAt   time.Time `json:"started_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	OutputBytes int64     `json:"output_bytes,omitempty"`
}
type RuntimeOverview struct {
	Sessions        map[string]SessionActivity `json:"sessions"`
	Intents         []IntentActivity           `json:"intents"`
	Live            LivePhase                  `json:"live"`
	Counts          map[string]int             `json:"counts"`
	Tools           []ToolActivity             `json:"tools"`
	Recent          []Event                    `json:"recent"`
	Facts           int                        `json:"facts"`
	Findings        int                        `json:"findings"`
	FindingsVisible bool                       `json:"findings_visible"`
	AssetNames      map[string]string          `json:"asset_names"`
}
type SessionActivity struct {
	Stage     string    `json:"stage"`
	Tool      string    `json:"tool,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}
type IntentActivity struct {
	ID             string   `json:"id"`
	Title          string   `json:"title"`
	Status         string   `json:"status"`
	AssetIDs       []string `json:"asset_ids"`
	ConversationID string   `json:"conversation_id"`
	Summary        string   `json:"summary"`
}

func (s *Store) RecentEvents(task string, limit int) ([]Event, error) {
	if limit < 1 || limit > 100 {
		limit = 8
	}
	rows, err := s.DB.Query("SELECT id,step_id,kind,message,created_at FROM deep_assessment_events WHERE task_id=? ORDER BY id DESC LIMIT ?", task, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var e Event
		if err = rows.Scan(&e.ID, &e.StepID, &e.Kind, &e.Message, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Read from the existing host execution registry and Pi-backed task ledger.
// No model call, tool execution or task transition is needed to refresh the view.
func (s *Store) RuntimeOverview(t *Task) (RuntimeOverview, error) {
	out := RuntimeOverview{Counts: map[string]int{}, Tools: []ToolActivity{}, Recent: []Event{}, AssetNames: map[string]string{}}
	for _, st := range t.Steps {
		out.Counts[st.Status]++
		if st.Status == "running" || st.Status == "pausing" || st.Status == "uncertain" || st.Status == "cleanup_unconfirmed" {
			out.Intents = append(out.Intents, IntentActivity{st.ID, st.Title, st.Status, st.AssetIDs, st.ConversationID, Clip(st.Summary, 300)})
		}
	}
	if err := s.DB.QueryRow("SELECT COUNT(*) FROM deep_assessment_facts WHERE task_id=?", t.ID).Scan(&out.Facts); err != nil {
		return out, err
	}
	events, err := s.RecentEvents(t.ID, 8)
	if err != nil {
		return out, err
	}
	out.Recent = events
	for i := range out.Recent {
		out.Recent[i].Message = Clip(out.Recent[i].Message, 400)
	}
	rows, err := s.DB.Query(`SELECT e.id,ss.step_id,e.tool_name,e.status,e.start_time,e.partial_output_updated_at,COALESCE(e.partial_output_bytes,0)
 FROM tool_executions e JOIN deep_assessment_sessions ss ON ss.conversation_id=e.conversation_id
 WHERE ss.task_id=? AND e.status IN ('queued','running') ORDER BY e.start_time DESC LIMIT 16`, t.ID)
	if err != nil {
		return out, err
	}
	represented := map[string]bool{}
	for rows.Next() {
		var v ToolActivity
		var updated sql.NullTime
		if err = rows.Scan(&v.ID, &v.StepID, &v.Tool, &v.Status, &v.StartedAt, &updated, &v.OutputBytes); err != nil {
			rows.Close()
			return out, err
		}
		v.ExecutionID = v.ID
		v.UpdatedAt = v.StartedAt
		if updated.Valid {
			v.UpdatedAt = updated.Time
		}
		out.Tools = append(out.Tools, v)
		represented[v.StepID+"\x00"+v.Tool] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	rows, err = s.DB.Query("SELECT id,step_id,tool,status,execution_id,updated_at FROM deep_assessment_calls WHERE task_id=? AND status IN ('running','uncertain') ORDER BY updated_at DESC LIMIT 16", t.ID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var v ToolActivity
		if err = rows.Scan(&v.ID, &v.StepID, &v.Tool, &v.Status, &v.ExecutionID, &v.UpdatedAt); err != nil {
			return out, err
		}
		v.StartedAt = v.UpdatedAt
		if !represented[v.StepID+"\x00"+v.Tool] {
			out.Tools = append(out.Tools, v)
		}
	}
	return out, rows.Err()
}
