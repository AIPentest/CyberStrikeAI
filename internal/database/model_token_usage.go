package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

const modelTokenUsageEventType = "eino_usage_summary"

// ModelTokenUsage records one model-usage summary emitted by an Agent run.
type ModelTokenUsage struct {
	ID               string    `json:"id"`
	ProcessDetailID  string    `json:"processDetailId"`
	MessageID        string    `json:"messageId"`
	ConversationID   string    `json:"conversationId"`
	ProjectID        string    `json:"projectId,omitempty"`
	Source           string    `json:"source"`
	Orchestration    string    `json:"orchestration"`
	Reason           string    `json:"reason"`
	Model            string    `json:"model,omitempty"`
	ModelCalls       int64     `json:"modelCalls"`
	PromptTokens     int64     `json:"promptTokens"`
	CompletionTokens int64     `json:"completionTokens"`
	TotalTokens      int64     `json:"totalTokens"`
	CacheWriteTokens int64     `json:"cacheWriteTokens"`
	CachedTokens     int64     `json:"cachedTokens"`
	ReasoningTokens  int64     `json:"reasoningTokens"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

// ModelTokenUsageSummary is the aggregate shape used by dashboard and APIs.
type ModelTokenUsageSummary struct {
	Events           int64 `json:"events"`
	ModelCalls       int64 `json:"modelCalls"`
	PromptTokens     int64 `json:"promptTokens"`
	CompletionTokens int64 `json:"completionTokens"`
	TotalTokens      int64 `json:"totalTokens"`
	CacheWriteTokens int64 `json:"cacheWriteTokens"`
	CachedTokens     int64 `json:"cachedTokens"`
	ReasoningTokens  int64 `json:"reasoningTokens"`
}

// ModelTokenUsageBreakdown is a grouped aggregate row.
type ModelTokenUsageBreakdown struct {
	Key              string `json:"key"`
	Label            string `json:"label,omitempty"`
	Events           int64  `json:"events"`
	ModelCalls       int64  `json:"modelCalls"`
	PromptTokens     int64  `json:"promptTokens"`
	CompletionTokens int64  `json:"completionTokens"`
	TotalTokens      int64  `json:"totalTokens"`
	CacheWriteTokens int64  `json:"cacheWriteTokens"`
	CachedTokens     int64  `json:"cachedTokens"`
	ReasoningTokens  int64  `json:"reasoningTokens"`
}

// ModelTokenUsageStats is a compact API response for usage dashboards.
type ModelTokenUsageStats struct {
	Summary         ModelTokenUsageSummary     `json:"summary"`
	Today           ModelTokenUsageSummary     `json:"today"`
	ByDay           []ModelTokenUsageBreakdown `json:"byDay"`
	ByModel         []ModelTokenUsageBreakdown `json:"byModel"`
	ByOrchestration []ModelTokenUsageBreakdown `json:"byOrchestration"`
	Recent          []ModelTokenUsage          `json:"recent"`
}

// ModelTokenUsageFilter scopes usage queries.
type ModelTokenUsageFilter struct {
	ConversationID string
	ProjectID      string
	Since          time.Time
	Until          time.Time
	Days           int
	Access         RBACListAccess
	Limit          int
}

func modelTokenUsageFromProcessDetail(messageID, conversationID, processDetailID string, data interface{}) (ModelTokenUsage, bool) {
	m := mapFromUsageData(data)
	if len(m) == 0 {
		return ModelTokenUsage{}, false
	}
	usage := ModelTokenUsage{
		ID:               uuid.New().String(),
		ProcessDetailID:  strings.TrimSpace(processDetailID),
		MessageID:        strings.TrimSpace(messageID),
		ConversationID:   strings.TrimSpace(conversationID),
		Source:           strings.TrimSpace(fmt.Sprint(m["source"])),
		Orchestration:    strings.TrimSpace(fmt.Sprint(m["orchestration"])),
		Reason:           strings.TrimSpace(fmt.Sprint(m["reason"])),
		Model:            strings.TrimSpace(fmt.Sprint(m["model"])),
		ModelCalls:       usageInt64(m["modelCalls"]),
		PromptTokens:     usageInt64(m["promptTokens"]),
		CompletionTokens: usageInt64(m["completionTokens"]),
		TotalTokens:      usageInt64(m["totalTokens"]),
		CachedTokens:     usageInt64(m["cachedTokens"]),
		CacheWriteTokens: usageInt64(m["cacheWriteTokens"]),
		ReasoningTokens:  usageInt64(m["reasoningTokens"]),
	}
	if usage.TotalTokens == 0 && (usage.PromptTokens > 0 || usage.CompletionTokens > 0) {
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}
	if usage.ProcessDetailID == "" || usage.MessageID == "" || usage.ConversationID == "" {
		return ModelTokenUsage{}, false
	}
	if usage.ModelCalls == 0 && usage.TotalTokens == 0 && usage.PromptTokens == 0 && usage.CompletionTokens == 0 && usage.CachedTokens == 0 && usage.CacheWriteTokens == 0 && usage.ReasoningTokens == 0 {
		return ModelTokenUsage{}, false
	}
	return usage, true
}

func mapFromUsageData(data interface{}) map[string]interface{} {
	switch v := data.(type) {
	case nil:
		return nil
	case map[string]interface{}:
		return v
	case string:
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(v), &m); err == nil {
			return m
		}
	case []byte:
		var m map[string]interface{}
		if err := json.Unmarshal(v, &m); err == nil {
			return m
		}
	default:
		raw, err := json.Marshal(v)
		if err == nil {
			var m map[string]interface{}
			if err := json.Unmarshal(raw, &m); err == nil {
				return m
			}
		}
	}
	return nil
}

func usageInt64(v interface{}) int64 {
	switch n := v.(type) {
	case int:
		return int64(n)
	case int8:
		return int64(n)
	case int16:
		return int64(n)
	case int32:
		return int64(n)
	case int64:
		return n
	case uint:
		return int64(n)
	case uint8:
		return int64(n)
	case uint16:
		return int64(n)
	case uint32:
		return int64(n)
	case uint64:
		if n > math.MaxInt64 {
			return math.MaxInt64
		}
		return int64(n)
	case float32:
		return int64(n)
	case float64:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	case string:
		i, _ := strconv.ParseInt(strings.TrimSpace(n), 10, 64)
		return i
	default:
		i, _ := strconv.ParseInt(strings.TrimSpace(fmt.Sprint(v)), 10, 64)
		return i
	}
}

func (db *DB) maybeRecordModelTokenUsage(messageID, conversationID, processDetailID, eventType string, data interface{}) {
	if db == nil || eventType != modelTokenUsageEventType {
		return
	}
	usage, ok := modelTokenUsageFromProcessDetail(messageID, conversationID, processDetailID, data)
	if !ok {
		return
	}
	if err := db.UpsertModelTokenUsage(usage); err != nil && db.logger != nil {
		db.logger.Warn("failed to save model token usage",
			zap.String("processDetailId", processDetailID),
			zap.String("conversationId", conversationID),
			zap.Error(err))
	}
}

// UpsertModelTokenUsage persists usage with process_detail_id idempotency.
func (db *DB) UpsertModelTokenUsage(usage ModelTokenUsage) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}
	now := time.Now()
	createdAt := usage.CreatedAt
	if createdAt.IsZero() {
		createdAt = now
	}
	if usage.ID == "" {
		usage.ID = uuid.New().String()
	}
	var projectID sql.NullString
	if err := db.QueryRow(`SELECT project_id FROM conversations WHERE id = ?`, usage.ConversationID).Scan(&projectID); err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("failed to query the conversation project: %w", err)
	}
	projectValue := interface{}(nil)
	if projectID.Valid && strings.TrimSpace(projectID.String) != "" {
		projectValue = strings.TrimSpace(projectID.String)
	}
	_, err := db.Exec(`
INSERT INTO model_token_usage (
	id, process_detail_id, message_id, conversation_id, project_id,
	source, orchestration, reason, model, model_calls,
	prompt_tokens, completion_tokens, total_tokens, cached_tokens, cache_write_tokens, reasoning_tokens,
	created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(process_detail_id) DO UPDATE SET
	message_id = excluded.message_id,
	conversation_id = excluded.conversation_id,
	project_id = excluded.project_id,
	source = excluded.source,
	orchestration = excluded.orchestration,
	reason = excluded.reason,
	model = excluded.model,
	model_calls = excluded.model_calls,
	prompt_tokens = excluded.prompt_tokens,
	completion_tokens = excluded.completion_tokens,
	total_tokens = excluded.total_tokens,
	cached_tokens = excluded.cached_tokens,
	cache_write_tokens = excluded.cache_write_tokens,
	reasoning_tokens = excluded.reasoning_tokens,
	created_at = excluded.created_at,
	updated_at = excluded.updated_at`,
		usage.ID, usage.ProcessDetailID, usage.MessageID, usage.ConversationID, projectValue,
		usage.Source, usage.Orchestration, usage.Reason, usage.Model, usage.ModelCalls,
		usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens, usage.CachedTokens, usage.CacheWriteTokens, usage.ReasoningTokens,
		createdAt, now,
	)
	if err != nil {
		return fmt.Errorf("failed to write model token usage: %w", err)
	}
	return nil
}

// BackfillModelTokenUsageFromProcessDetails makes existing timeline usage events queryable.
func (db *DB) BackfillModelTokenUsageFromProcessDetails() error {
	if db == nil {
		return nil
	}
	rows, err := db.Query(`
SELECT pd.id, pd.message_id, pd.conversation_id, pd.data, pd.created_at
FROM process_details pd
LEFT JOIN model_token_usage mtu ON mtu.process_detail_id = pd.id
WHERE pd.event_type = ?
	AND (mtu.id IS NULL OR mtu.created_at != pd.created_at)`, modelTokenUsageEventType)
	if err != nil {
		return fmt.Errorf("failed to query historical model token usage: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var processDetailID, messageID, conversationID string
		var data sql.NullString
		var createdAt string
		if err := rows.Scan(&processDetailID, &messageID, &conversationID, &data, &createdAt); err != nil {
			return fmt.Errorf("failed to scan historical model token usage: %w", err)
		}
		if !data.Valid {
			continue
		}
		usage, ok := modelTokenUsageFromProcessDetail(messageID, conversationID, processDetailID, data.String)
		if !ok {
			continue
		}
		usage.CreatedAt = parseModelTokenUsageTime(createdAt)
		if err := db.UpsertModelTokenUsage(usage); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("failed to iterate historical model token usage: %w", err)
	}
	return nil
}

func (db *DB) GetModelTokenUsageStats(filter ModelTokenUsageFilter) (*ModelTokenUsageStats, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}
	if filter.Days <= 0 {
		filter.Days = 7
	}
	if filter.Limit <= 0 {
		filter.Limit = 10
	}
	where, args := buildModelTokenUsageWhere(filter, "mtu", "c")
	summary, err := db.queryModelTokenUsageSummary("SELECT "+modelTokenUsageSummarySelect("mtu")+" FROM model_token_usage mtu JOIN conversations c ON c.id = mtu.conversation_id"+where, args...)
	if err != nil {
		return nil, err
	}
	todayFilter := filter
	now := time.Now()
	todayFilter.Since = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	todayWhere, todayArgs := buildModelTokenUsageWhere(todayFilter, "mtu", "c")
	today, err := db.queryModelTokenUsageSummary("SELECT "+modelTokenUsageSummarySelect("mtu")+" FROM model_token_usage mtu JOIN conversations c ON c.id = mtu.conversation_id"+todayWhere, todayArgs...)
	if err != nil {
		return nil, err
	}
	byDay, err := db.queryModelTokenUsageBreakdown(
		"SELECT date(mtu.created_at, 'localtime') AS k, date(mtu.created_at, 'localtime') AS label, "+modelTokenUsageSummarySelect("mtu")+" FROM model_token_usage mtu JOIN conversations c ON c.id = mtu.conversation_id"+where+" GROUP BY date(mtu.created_at, 'localtime') ORDER BY k DESC LIMIT ?",
		append(args, filter.Days)...,
	)
	if err != nil {
		return nil, err
	}
	byModel, err := db.queryModelTokenUsageBreakdown(
		"SELECT COALESCE(NULLIF(TRIM(mtu.model), ''), 'unknown') AS k, COALESCE(NULLIF(TRIM(mtu.model), ''), 'Unknown') AS label, "+modelTokenUsageSummarySelect("mtu")+" FROM model_token_usage mtu JOIN conversations c ON c.id = mtu.conversation_id"+where+" GROUP BY k ORDER BY SUM(mtu.total_tokens) DESC LIMIT ?",
		append(args, filter.Limit)...,
	)
	if err != nil {
		return nil, err
	}
	byOrch, err := db.queryModelTokenUsageBreakdown(
		"SELECT COALESCE(NULLIF(TRIM(mtu.orchestration), ''), 'unknown') AS k, COALESCE(NULLIF(TRIM(mtu.orchestration), ''), 'Unknown') AS label, "+modelTokenUsageSummarySelect("mtu")+" FROM model_token_usage mtu JOIN conversations c ON c.id = mtu.conversation_id"+where+" GROUP BY k ORDER BY SUM(mtu.total_tokens) DESC LIMIT ?",
		append(args, filter.Limit)...,
	)
	if err != nil {
		return nil, err
	}
	recent, err := db.ListModelTokenUsage(filter)
	if err != nil {
		return nil, err
	}
	return &ModelTokenUsageStats{
		Summary:         summary,
		Today:           today,
		ByDay:           byDay,
		ByModel:         byModel,
		ByOrchestration: byOrch,
		Recent:          recent,
	}, nil
}

func modelTokenUsageSummarySelect(alias string) string {
	p := ""
	if alias != "" {
		p = alias + "."
	}
	return fmt.Sprintf(`COUNT(%sid),
COALESCE(SUM(%smodel_calls), 0),
COALESCE(SUM(%sprompt_tokens), 0),
COALESCE(SUM(%scompletion_tokens), 0),
COALESCE(SUM(%stotal_tokens), 0),
COALESCE(SUM(%scached_tokens), 0),
COALESCE(SUM(%scache_write_tokens), 0),
COALESCE(SUM(%sreasoning_tokens), 0)`, p, p, p, p, p, p, p, p)
}

func buildModelTokenUsageWhere(filter ModelTokenUsageFilter, usageAlias, convAlias string) (string, []interface{}) {
	where := " WHERE 1=1"
	args := []interface{}{}
	uPrefix := ""
	if usageAlias != "" {
		uPrefix = usageAlias + "."
	}
	if cid := strings.TrimSpace(filter.ConversationID); cid != "" {
		where += " AND " + uPrefix + "conversation_id = ?"
		args = append(args, cid)
	}
	where, args = appendConversationProjectFilter(where, args, filter.ProjectID, usageAlias)
	if !filter.Since.IsZero() {
		where += " AND " + uPrefix + "created_at >= ?"
		args = append(args, filter.Since)
	}
	if !filter.Until.IsZero() {
		where += " AND " + uPrefix + "created_at <= ?"
		args = append(args, filter.Until)
	}
	where, args = appendConversationAccessFilter(where, args, filter.Access.UserID, filter.Access.Scope, convAlias)
	return where, args
}

func (db *DB) queryModelTokenUsageSummary(query string, args ...interface{}) (ModelTokenUsageSummary, error) {
	var s ModelTokenUsageSummary
	err := db.QueryRow(query, args...).Scan(
		&s.Events, &s.ModelCalls, &s.PromptTokens, &s.CompletionTokens,
		&s.TotalTokens, &s.CachedTokens, &s.CacheWriteTokens, &s.ReasoningTokens,
	)
	if err != nil {
		return s, fmt.Errorf("failed to query the model token usage totals: %w", err)
	}
	return s, nil
}

func (db *DB) queryModelTokenUsageBreakdown(query string, args ...interface{}) ([]ModelTokenUsageBreakdown, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query model token usage groupings: %w", err)
	}
	defer rows.Close()
	out := []ModelTokenUsageBreakdown{}
	for rows.Next() {
		var row ModelTokenUsageBreakdown
		if err := rows.Scan(
			&row.Key, &row.Label, &row.Events, &row.ModelCalls, &row.PromptTokens,
			&row.CompletionTokens, &row.TotalTokens, &row.CachedTokens, &row.CacheWriteTokens, &row.ReasoningTokens,
		); err != nil {
			return nil, fmt.Errorf("failed to scan model token usage groupings: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate model token usage groupings: %w", err)
	}
	return out, nil
}

func (db *DB) ListModelTokenUsage(filter ModelTokenUsageFilter) ([]ModelTokenUsage, error) {
	if filter.Limit <= 0 {
		filter.Limit = 20
	}
	if filter.Limit > 500 {
		filter.Limit = 500
	}
	where, args := buildModelTokenUsageWhere(filter, "mtu", "c")
	args = append(args, filter.Limit)
	rows, err := db.Query(`
SELECT mtu.id, mtu.process_detail_id, mtu.message_id, mtu.conversation_id,
	COALESCE(mtu.project_id, ''), mtu.source, mtu.orchestration, mtu.reason, mtu.model,
	mtu.model_calls, mtu.prompt_tokens, mtu.completion_tokens, mtu.total_tokens,
	mtu.cached_tokens, mtu.cache_write_tokens, mtu.reasoning_tokens, mtu.created_at, mtu.updated_at
FROM model_token_usage mtu
JOIN conversations c ON c.id = mtu.conversation_id`+where+`
ORDER BY mtu.created_at DESC, mtu.rowid DESC
LIMIT ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query model token usage details: %w", err)
	}
	defer rows.Close()
	out := []ModelTokenUsage{}
	for rows.Next() {
		var u ModelTokenUsage
		var createdAt, updatedAt string
		if err := rows.Scan(
			&u.ID, &u.ProcessDetailID, &u.MessageID, &u.ConversationID, &u.ProjectID,
			&u.Source, &u.Orchestration, &u.Reason, &u.Model, &u.ModelCalls,
			&u.PromptTokens, &u.CompletionTokens, &u.TotalTokens, &u.CachedTokens,
			&u.CacheWriteTokens, &u.ReasoningTokens, &createdAt, &updatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan model token usage details: %w", err)
		}
		u.CreatedAt = parseModelTokenUsageTime(createdAt)
		u.UpdatedAt = parseModelTokenUsageTime(updatedAt)
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate model token usage details: %w", err)
	}
	return out, nil
}

func parseModelTokenUsageTime(s string) time.Time {
	for _, layout := range []string{
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999-07:00",
		"2006-01-02 15:04:05",
		time.RFC3339Nano,
		time.RFC3339,
	} {
		if t, err := time.Parse(layout, strings.TrimSpace(s)); err == nil {
			return t
		}
	}
	return time.Time{}
}
