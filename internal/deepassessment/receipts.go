package deepassessment

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"cyberstrike-ai/internal/agentruntime"
)

func (s *Store) RetryStep(id, step string, revision int64, reconciled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var raw string
	if err = tx.QueryRow(`SELECT payload FROM deep_assessments WHERE id=?`, id).Scan(&raw); err != nil {
		return err
	}
	var t Task
	if err = json.Unmarshal([]byte(raw), &t); err != nil {
		return err
	}
	if t.Revision != revision || Active(t.Status) || t.Status == "completed" || t.Status == "cancelled" {
		return ErrConflict
	}
	st := t.Step(step)
	if st == nil {
		return fmt.Errorf("unknown step")
	}
	if st.Status != "failed" && st.Status != "blocked" && st.Status != "interrupted" && st.Status != "uncertain" && st.Status != "cleanup_unconfirmed" {
		return ErrConflict
	}
	if (st.Status == "uncertain" || st.Status == "cleanup_unconfirmed") && !reconciled {
		return fmt.Errorf("review the uncertain execution before retrying")
	}
	if _, err = tx.Exec(`UPDATE deep_assessment_calls SET status='reconciled' WHERE task_id=? AND step_id=? AND status IN ('running','uncertain')`, id, step); err != nil {
		return err
	}
	st.Status = "pending"
	st.StalledSegments = 0
	t.Status = "paused"
	if t.Question != "" {
		t.Status = "waiting_input"
	}
	for _, pending := range t.Steps {
		if pending.Status == "uncertain" || pending.Status == "cleanup_unconfirmed" {
			t.Status = "needs_review"
			break
		}
	}
	// Acknowledging one receipt is not an instruction to restart the assessment.
	t.RestartPending = false
	t.NextWake = nil
	if reconciled {
		if t.Status == "needs_review" {
			t.Summary = "仍有步骤的执行结果待核对，请继续查看回执。"
		} else {
			t.Summary = "待核对步骤已处理，原会话和证据保留；任务尚未开始。"
		}
	}
	t.RetryCount = 0
	t.Revision++
	t.UpdatedAt = time.Now().UTC()
	body, err := json.Marshal(t)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(`UPDATE deep_assessments SET status=?,payload=?,updated_at=? WHERE id=?`, t.Status, string(body), t.UpdatedAt, id); err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT INTO deep_assessment_events(task_id,step_id,kind,message,created_at) VALUES(?,?,'retry','步骤已重新排队，保留原会话；等待手动开始。',?)`, id, step, t.UpdatedAt); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ValidateEvidence(task string, ids []string) error {
	if len(ids) == 0 || len(ids) > 32 {
		return fmt.Errorf("completed requires 1–32 real evidence receipt IDs")
	}
	for _, id := range ids {
		if strings.TrimSpace(id) == "" {
			return fmt.Errorf("empty evidence reference")
		}
		var n int
		if err := s.DB.QueryRow(`SELECT COUNT(*) FROM deep_assessment_calls WHERE task_id=? AND (id=? OR execution_id=?) AND status='completed'`, task, id, id).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("receipt %s is not a successful operation in this assessment", id)
		}
	}
	return nil
}

// Intent is committed before dispatching a tool. The receipt is committed before
// its result goes back to Pi. A process loss between the two remains visible.
func (s *Store) BeginCall(task, step string, call agentruntime.ToolCall) error {
	b, err := json.Marshal(call.Arguments)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(`INSERT INTO deep_assessment_calls(id,task_id,step_id,tool,arguments,status,updated_at,session_entry_id,provider_id) VALUES(?,?,?,?,?,'running',?,?,?)`, call.ID, task, step, call.Name, string(b), time.Now().UTC(), call.SessionEntryID, call.ProviderID)
	return err
}
func (s *Store) FinishCall(call agentruntime.ToolCall, result agentruntime.ToolResult, uncertain bool) error {
	state := "completed"
	if result.IsError {
		state = "failed"
	}
	if uncertain {
		state = "uncertain"
	}
	return s.finishCall(call, result, state, false)
}

func (s *Store) finishCall(call agentruntime.ToolCall, result agentruntime.ToolResult, state string, repair bool) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	changed, err := tx.Exec(`UPDATE deep_assessment_calls SET status=?,execution_id=?,result=?,updated_at=? WHERE id=? AND (?=0 OR status IN ('running','uncertain')) AND NOT (?='uncertain' AND status IN ('completed','failed','cancelled','reconciled'))`, state, result.ExecutionID, Clip(result.Content, 16000), time.Now().UTC(), call.ID, repair, state)
	if err != nil {
		return err
	}
	if n, countErr := changed.RowsAffected(); countErr != nil {
		return countErr
	} else if n == 0 {
		if repair {
			return nil
		}
		if state == "uncertain" {
			var exists int
			return tx.QueryRow("SELECT 1 FROM deep_assessment_calls WHERE id=?", call.ID).Scan(&exists)
		}
		return sql.ErrNoRows
	}
	var task, step string
	var round int
	if err = tx.QueryRow(`SELECT c.task_id,c.step_id,COALESCE(json_extract(t.payload,'$.rounds'),0) FROM deep_assessment_calls c JOIN deep_assessments t ON t.id=c.task_id WHERE c.id=?`, call.ID).Scan(&task, &step, &round); err != nil {
		return err
	}
	src, _ := json.Marshal(map[string]any{"call_id": call.ID, "execution_id": result.ExecutionID})
	if err = graphNode(tx, task, Node{ID: "receipt:" + call.ID, StepID: step, Kind: "receipt", State: state, Title: call.Name, Body: Clip(result.Content, 16000), Source: src, Round: round}); err != nil {
		return err
	}
	if err = graphEdge(tx, task, stepNode(step), "receipt:"+call.ID, "executes"); err != nil {
		return err
	}
	return tx.Commit()
}

// A cancelled wait can return before the owned execution finishes cleanup.
// Repair only from a terminal host receipt tied to the same task, step, session
// and tool. Orphaned/unknown remote work is deliberately left unresolved.
func (s *Store) ReconcileKnownCalls(task, step string) (map[string]bool, error) {
	rows, err := s.DB.Query(`SELECT c.id,c.step_id,c.tool,c.execution_id
 FROM deep_assessment_calls c JOIN tool_executions e ON e.id=c.execution_id
 WHERE c.task_id=? AND (?='' OR c.step_id=?) AND c.status IN ('running','uncertain')
 AND e.tool_name=c.tool AND e.end_time IS NOT NULL
 AND e.status IN ('completed','failed','blocked','cancelled','hard_timeout')
 AND EXISTS(SELECT 1 FROM deep_assessment_sessions ss WHERE ss.task_id=c.task_id AND ss.step_id=c.step_id AND ss.conversation_id=e.conversation_id)`, task, step, step)
	if err != nil {
		return nil, err
	}
	type repairCall struct{ id, step, tool, execution string }
	var calls []repairCall
	for rows.Next() {
		var c repairCall
		if err = rows.Scan(&c.id, &c.step, &c.tool, &c.execution); err != nil {
			rows.Close()
			return nil, err
		}
		calls = append(calls, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	resolved := map[string]bool{}
	for _, call := range calls {
		execution, readErr := s.DB.GetToolExecution(call.execution)
		if readErr != nil {
			return nil, readErr
		}
		if execution.EndTime == nil || execution.ToolName != call.tool {
			continue
		}
		state := "failed"
		switch execution.Status {
		case "completed":
			state = "completed"
		case "cancelled":
			state = "cancelled"
		case "failed", "blocked", "hard_timeout":
		default:
			continue
		}
		content := execution.Error
		if execution.Result != nil {
			var parts []string
			for _, item := range execution.Result.Content {
				if item.Type == "text" {
					parts = append(parts, item.Text)
				}
			}
			if len(parts) > 0 {
				content = strings.Join(parts, "\n")
			}
		}
		if content == "" {
			content = "Host execution settled: " + execution.Status
		}
		if state == "cancelled" {
			content = "Execution was cancelled and its resources have stopped. It did not complete successfully.\n" + content
		}
		if execution.PartialOutput != "" {
			content += "\nPartial output (not a successful result):\n" + execution.PartialOutput
		}
		result := agentruntime.ToolResult{Content: content, ExecutionID: call.execution, IsError: state != "completed"}
		if err = s.finishCall(agentruntime.ToolCall{ID: call.id}, result, state, true); err != nil {
			return nil, err
		}
		resolved[call.step] = true
	}
	// Include already repaired receipts so a failed start (another step is still
	// unknown) does not require re-acknowledging a known result on the next start.
	known, err := s.DB.Query(`SELECT DISTINCT step_id FROM deep_assessment_calls WHERE task_id=? AND (?='' OR step_id=?) AND status IN ('completed','failed','cancelled','reconciled')`, task, step, step)
	if err != nil {
		return nil, err
	}
	defer known.Close()
	for known.Next() {
		var id string
		if err = known.Scan(&id); err != nil {
			return nil, err
		}
		resolved[id] = true
	}
	return resolved, known.Err()
}

func (s *Store) UncertainSteps(id string) (map[string]bool, error) {
	rows, err := s.DB.Query(`SELECT DISTINCT step_id FROM deep_assessment_calls WHERE task_id=? AND status IN ('running','uncertain')`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var step string
		if err = rows.Scan(&step); err != nil {
			return nil, err
		}
		out[step] = true
	}
	return out, rows.Err()
}
func (s *Store) CallCount(id, step string) (int, error) {
	var count int
	err := s.DB.QueryRow(`SELECT COUNT(DISTINCT json_array(tool,arguments,result)) FROM deep_assessment_calls WHERE task_id=? AND step_id=? AND status='completed'`, id, step).Scan(&count)
	return count, err
}
func (s *Store) RecordMetrics(id, step string, data map[string]any) error {
	session, _ := data["sessionId"].(string)
	if session == "" {
		return fmt.Errorf("missing Pi session identity")
	}
	number := func(key string) int64 { v, _ := data[key].(float64); return int64(v) }
	m := SessionMetrics{StepID: step, Tokens: number("tokens"), Compactions: int(number("compactions")), ToolCount: int(number("toolCount"))}
	m.ToolDefinitionBytes = number("toolDefinitionBytes")
	if context, ok := data["context"].(map[string]any); ok {
		if n, ok := context["tokens"].(float64); ok {
			value := int64(n)
			m.ContextTokens = &value
		}
		if n, ok := context["contextWindow"].(float64); ok {
			m.ContextWindow = int64(n)
		}
	}
	return s.Change(id, "", "", "", func(t *Task) error {
		if t.Metrics == nil {
			t.Metrics = map[string]SessionMetrics{}
		}
		old := t.Metrics[session]
		if m.Tokens < old.Tokens {
			return nil
		}
		t.TokensUsed += m.Tokens - old.Tokens
		t.Metrics[session] = m
		return nil
	})
}

// Only known results may repair a missing Pi tool result. Unknown calls remain
// gated by Recover; identity includes the native entry, not just a provider ID.
func (s *Store) NativeReceipts(task, step string) ([]agentruntime.NativeReceipt, error) {
	rows, err := s.DB.Query(`SELECT session_entry_id,provider_id,tool,result,execution_id,status FROM deep_assessment_calls WHERE task_id=? AND step_id=? AND session_entry_id<>'' AND status IN ('completed','failed','cancelled') ORDER BY rowid DESC LIMIT 128`, task, step)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []agentruntime.NativeReceipt{}
	for rows.Next() {
		var receipt agentruntime.NativeReceipt
		var state string
		if err = rows.Scan(&receipt.SessionEntryID, &receipt.ProviderID, &receipt.Name, &receipt.Content, &receipt.ExecutionID, &state); err != nil {
			return nil, err
		}
		receipt.IsError = state != "completed"
		out = append(out, receipt)
	}
	return out, rows.Err()
}
