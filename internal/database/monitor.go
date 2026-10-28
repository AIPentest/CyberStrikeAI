package database

import (
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"cyberstrike-ai/internal/mcp"

	"go.uber.org/zap"
)

// SaveToolExecution 保存工具执行记录
func (db *DB) SaveToolExecution(exec *mcp.ToolExecution) error {
	argsJSON, err := json.Marshal(exec.Arguments)
	if err != nil {
		db.logger.Warn("failed to serialize the execution arguments", zap.Error(err))
		argsJSON = []byte("{}")
	}

	var resultJSON sql.NullString
	if exec.Result != nil {
		resultBytes, err := json.Marshal(exec.Result)
		if err != nil {
			db.logger.Warn("failed to serialize the execution result", zap.Error(err))
		} else {
			resultJSON = sql.NullString{String: string(resultBytes), Valid: true}
		}
	}

	var errorText sql.NullString
	if exec.Error != "" {
		errorText = sql.NullString{String: exec.Error, Valid: true}
	}

	var endTime sql.NullTime
	if exec.EndTime != nil {
		endTime = sql.NullTime{Time: *exec.EndTime, Valid: true}
	}

	var durationMs sql.NullInt64
	if exec.Duration > 0 {
		durationMs = sql.NullInt64{Int64: exec.Duration.Milliseconds(), Valid: true}
	}
	var partialUpdatedAt sql.NullTime
	if exec.PartialOutputUpdatedAt != nil {
		partialUpdatedAt = sql.NullTime{Time: *exec.PartialOutputUpdatedAt, Valid: true}
	}
	partialTruncated := 0
	if exec.PartialOutputTruncated {
		partialTruncated = 1
	}

	query := `
		INSERT OR REPLACE INTO tool_executions 
		(id, tool_name, arguments, status, result, error, start_time, end_time, duration_ms, partial_output, partial_output_bytes, partial_output_truncated, partial_output_updated_at, owner_user_id, conversation_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err = db.Exec(query,
		exec.ID,
		exec.ToolName,
		string(argsJSON),
		exec.Status,
		resultJSON,
		errorText,
		exec.StartTime,
		endTime,
		durationMs,
		sqlNullString(exec.PartialOutput),
		exec.PartialOutputBytes,
		partialTruncated,
		partialUpdatedAt,
		strings.TrimSpace(exec.OwnerUserID),
		strings.TrimSpace(exec.ConversationID),
		time.Now(),
	)

	if err != nil {
		db.logger.Error("failed to save the tool execution record", zap.Error(err), zap.String("executionId", exec.ID))
		return err
	}

	return nil
}

// UpdateToolExecutionResult 仅更新结果字段（用于 reduction 后将监控展示与模型上下文对齐）。
func (db *DB) UpdateToolExecutionResult(id string, result *mcp.ToolResult) error {
	id = strings.TrimSpace(id)
	if id == "" || result == nil {
		return nil
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM tool_executions WHERE id = ?`, id).Scan(&status); err != nil && err != sql.ErrNoRows {
		return err
	}
	if status == mcp.ToolExecutionStatusBlocked {
		copy := *result
		copy.Blocked, copy.IsError = true, true
		result = &copy
	}
	resultBytes, err := json.Marshal(result)
	if err != nil {
		return err
	}
	_, err = db.Exec(`UPDATE tool_executions SET result = ? WHERE id = ?`, string(resultBytes), id)
	if err != nil {
		db.logger.Warn("failed to update the tool execution result", zap.Error(err), zap.String("executionId", id))
	}
	return err
}

func sqlNullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// CountToolExecutions 统计工具执行记录总数
func (db *DB) CountToolExecutions(status, toolName string) (int, error) {
	return db.CountToolExecutionsForAccess(status, toolName, RBACListAccess{Scope: RBACScopeAll})
}

func (db *DB) CountToolExecutionsForAccess(status, toolName string, access RBACListAccess) (int, error) {
	query := `SELECT COUNT(*) FROM tool_executions`
	args := []interface{}{}
	conditions := []string{}
	if status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, status)
	}
	if toolName != "" {
		// 支持部分匹配（模糊搜索），不区分大小写
		conditions = append(conditions, "LOWER(tool_name) LIKE ?")
		args = append(args, "%"+strings.ToLower(toolName)+"%")
	}
	if len(conditions) > 0 {
		query += ` WHERE ` + conditions[0]
		for i := 1; i < len(conditions); i++ {
			query += ` AND ` + conditions[i]
		}
	}
	query, args = appendToolExecutionAccessSQL(query, args, access, len(conditions) > 0)
	var count int
	err := db.QueryRow(query, args...).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// LoadToolExecutions 加载所有工具执行记录（支持分页）
func (db *DB) LoadToolExecutions() ([]*mcp.ToolExecution, error) {
	return db.LoadToolExecutionsWithPagination(0, 1000, "", "")
}

// LoadToolExecutionsWithPagination 分页加载工具执行记录
// limit: 最大返回记录数，0 表示使用默认值 1000
// offset: 跳过的记录数，用于分页
// status: 状态筛选，空字符串表示不过滤
// toolName: 工具名称筛选，空字符串表示不过滤
func (db *DB) LoadToolExecutionsWithPagination(offset, limit int, status, toolName string) ([]*mcp.ToolExecution, error) {
	if limit <= 0 {
		limit = 1000 // 默认限制
	}
	if limit > 10000 {
		limit = 10000 // 最大限制，防止一次性加载过多数据
	}

	query := `
		SELECT id, tool_name, arguments, status, result, error, start_time, end_time, duration_ms, COALESCE(owner_user_id, ''), COALESCE(conversation_id, '')
		FROM tool_executions
	`
	args := []interface{}{}
	conditions := []string{}
	if status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, status)
	}
	if toolName != "" {
		// 支持部分匹配（模糊搜索），不区分大小写
		conditions = append(conditions, "LOWER(tool_name) LIKE ?")
		args = append(args, "%"+strings.ToLower(toolName)+"%")
	}
	if len(conditions) > 0 {
		query += ` WHERE ` + conditions[0]
		for i := 1; i < len(conditions); i++ {
			query += ` AND ` + conditions[i]
		}
	}
	query += ` ORDER BY start_time DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var executions []*mcp.ToolExecution
	for rows.Next() {
		var exec mcp.ToolExecution
		var argsJSON string
		var resultJSON sql.NullString
		var errorText sql.NullString
		var endTime sql.NullTime
		var durationMs sql.NullInt64

		err := rows.Scan(
			&exec.ID,
			&exec.ToolName,
			&argsJSON,
			&exec.Status,
			&resultJSON,
			&errorText,
			&exec.StartTime,
			&endTime,
			&durationMs,
			&exec.OwnerUserID,
			&exec.ConversationID,
		)
		if err != nil {
			db.logger.Warn("failed to load the execution record", zap.Error(err))
			continue
		}

		// 解析参数
		if err := json.Unmarshal([]byte(argsJSON), &exec.Arguments); err != nil {
			db.logger.Warn("failed to parse the execution arguments", zap.Error(err))
			exec.Arguments = make(map[string]interface{})
		}

		// 解析结果
		if resultJSON.Valid && resultJSON.String != "" {
			var result mcp.ToolResult
			if err := json.Unmarshal([]byte(resultJSON.String), &result); err != nil {
				db.logger.Warn("failed to parse the execution result", zap.Error(err))
			} else {
				exec.Result = &result
			}
		}

		// 设置错误
		if errorText.Valid {
			exec.Error = errorText.String
		}

		// 设置结束时间
		if endTime.Valid {
			exec.EndTime = &endTime.Time
		}

		// 设置持续时间
		if durationMs.Valid {
			exec.Duration = time.Duration(durationMs.Int64) * time.Millisecond
		}

		executions = append(executions, &exec)
	}

	return executions, nil
}

func toolExecutionsFilterSQL(status, toolName string) (string, []interface{}) {
	args := []interface{}{}
	conditions := []string{}
	if status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, status)
	}
	if toolName != "" {
		conditions = append(conditions, "LOWER(tool_name) LIKE ?")
		args = append(args, "%"+strings.ToLower(toolName)+"%")
	}
	if len(conditions) == 0 {
		return "", args
	}
	return ` WHERE ` + strings.Join(conditions, ` AND `), args
}

// ToolStatsSummary 工具调用汇总（全量聚合，不含逐工具明细）
type ToolStatsSummary struct {
	TotalCalls   int
	SuccessCalls int
	FailedCalls  int
	BlockedCalls int
	LastCallTime *time.Time
	ToolCount    int
}

// ToolStatsSummaryResult 汇总 + Top N 工具排行
type ToolStatsSummaryResult struct {
	Summary  ToolStatsSummary
	TopTools []*mcp.ToolStats
}

// LoadToolStatsSummary 聚合统计信息，仅返回汇总与 Top N 工具（避免全量 map 传输）。
// 监控页的失败口径只包含真实失败/异常终止；用户主动取消的 cancelled 保留在总调用中，不计入失败。
func (db *DB) LoadToolStatsSummary(topN int) (*ToolStatsSummaryResult, error) {
	if topN <= 0 {
		topN = 6
	}
	if topN > 100 {
		topN = 100
	}

	result := &ToolStatsSummaryResult{
		TopTools: make([]*mcp.ToolStats, 0, topN),
	}

	summaryQuery := `
		SELECT COUNT(*),
			COALESCE(SUM(CASE WHEN status = 'completed' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status IN ('failed', 'hard_timeout', 'orphaned') THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'blocked' THEN 1 ELSE 0 END), 0),
			MAX(start_time),
			COUNT(DISTINCT tool_name)
		FROM tool_executions
	`
	var lastCallRaw sql.NullString
	err := db.QueryRow(summaryQuery).Scan(
		&result.Summary.TotalCalls,
		&result.Summary.SuccessCalls,
		&result.Summary.FailedCalls,
		&result.Summary.BlockedCalls,
		&lastCallRaw,
		&result.Summary.ToolCount,
	)
	if err != nil {
		return nil, err
	}
	if lastCallRaw.Valid && strings.TrimSpace(lastCallRaw.String) != "" {
		if t, parseErr := time.Parse(time.RFC3339Nano, lastCallRaw.String); parseErr == nil {
			result.Summary.LastCallTime = &t
		} else if t, parseErr := time.Parse("2006-01-02 15:04:05.999999999-07:00", lastCallRaw.String); parseErr == nil {
			result.Summary.LastCallTime = &t
		} else if t, parseErr := time.Parse("2006-01-02 15:04:05", lastCallRaw.String); parseErr == nil {
			result.Summary.LastCallTime = &t
		}
	}

	topQuery := `
		SELECT tool_name,
			COUNT(*) AS total_calls,
			SUM(CASE WHEN status = 'completed' THEN 1 ELSE 0 END) AS success_calls,
			SUM(CASE WHEN status IN ('failed', 'hard_timeout', 'orphaned') THEN 1 ELSE 0 END) AS failed_calls,
			SUM(CASE WHEN status = 'blocked' THEN 1 ELSE 0 END) AS blocked_calls,
			MAX(start_time) AS last_call_time
		FROM tool_executions
		GROUP BY tool_name
		ORDER BY total_calls DESC, tool_name ASC
		LIMIT ?
	`
	rows, err := db.Query(topQuery, topN)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var stat mcp.ToolStats
		var lastCallTime sql.NullString
		if err := rows.Scan(
			&stat.ToolName,
			&stat.TotalCalls,
			&stat.SuccessCalls,
			&stat.FailedCalls,
			&stat.BlockedCalls,
			&lastCallTime,
		); err != nil {
			db.logger.Warn("failed to load top tool stats", zap.Error(err))
			continue
		}
		if lastCallTime.Valid {
			parsed := parseDBTime(lastCallTime.String)
			stat.LastCallTime = &parsed
		}
		result.TopTools = append(result.TopTools, &stat)
	}

	return result, nil
}

func (db *DB) LoadToolStatsSummaryForAccess(topN int, access RBACListAccess) (*ToolStatsSummaryResult, error) {
	if access.Scope == RBACScopeAll {
		return db.LoadToolStatsSummary(topN)
	}
	if topN <= 0 {
		topN = 6
	}
	if topN > 100 {
		topN = 100
	}
	result := &ToolStatsSummaryResult{TopTools: make([]*mcp.ToolStats, 0, topN)}
	fromSQL, args := appendToolExecutionAccessSQL(` FROM tool_executions`, nil, access, false)
	var lastCall sql.NullString
	err := db.QueryRow(`SELECT COUNT(*),
		COALESCE(SUM(CASE WHEN status = 'completed' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN status IN ('failed', 'hard_timeout', 'orphaned') THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN status = 'blocked' THEN 1 ELSE 0 END), 0),
		MAX(start_time), COUNT(DISTINCT tool_name)`+fromSQL, args...).Scan(
		&result.Summary.TotalCalls, &result.Summary.SuccessCalls, &result.Summary.FailedCalls, &result.Summary.BlockedCalls,
		&lastCall, &result.Summary.ToolCount,
	)
	if err != nil {
		return nil, err
	}
	if lastCall.Valid {
		parsed := parseDBTime(lastCall.String)
		result.Summary.LastCallTime = &parsed
	}
	rows, err := db.Query(`SELECT tool_name, COUNT(*),
		SUM(CASE WHEN status = 'completed' THEN 1 ELSE 0 END),
		SUM(CASE WHEN status IN ('failed', 'hard_timeout', 'orphaned') THEN 1 ELSE 0 END),
		SUM(CASE WHEN status = 'blocked' THEN 1 ELSE 0 END), MAX(start_time)`+
		fromSQL+` GROUP BY tool_name ORDER BY COUNT(*) DESC, tool_name ASC LIMIT ?`, append(args, topN)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var stat mcp.ToolStats
		var last sql.NullString
		if err := rows.Scan(&stat.ToolName, &stat.TotalCalls, &stat.SuccessCalls, &stat.FailedCalls, &stat.BlockedCalls, &last); err != nil {
			return nil, err
		}
		if last.Valid {
			parsed := parseDBTime(last.String)
			stat.LastCallTime = &parsed
		}
		result.TopTools = append(result.TopTools, &stat)
	}
	return result, rows.Err()
}

// LoadToolExecutionListPage 分页加载执行记录列表（不含 arguments/result，供监控列表使用）
func (db *DB) LoadToolExecutionListPage(offset, limit int, status, toolName string) ([]*mcp.ToolExecution, error) {
	return db.LoadToolExecutionListPageForAccess(offset, limit, status, toolName, RBACListAccess{Scope: RBACScopeAll})
}

func (db *DB) LoadToolExecutionListPageForAccess(offset, limit int, status, toolName string, access RBACListAccess) ([]*mcp.ToolExecution, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	query := `
		SELECT id, tool_name, status, start_time, end_time, duration_ms, COALESCE(owner_user_id, ''), COALESCE(conversation_id, '')
		FROM tool_executions
	`
	whereSQL, args := toolExecutionsFilterSQL(status, toolName)
	query += whereSQL
	query, args = appendToolExecutionAccessSQL(query, args, access, whereSQL != "")
	query += ` ORDER BY start_time DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	executions := make([]*mcp.ToolExecution, 0, limit)
	for rows.Next() {
		var exec mcp.ToolExecution
		var endTime sql.NullTime
		var durationMs sql.NullInt64

		if err := rows.Scan(
			&exec.ID,
			&exec.ToolName,
			&exec.Status,
			&exec.StartTime,
			&endTime,
			&durationMs,
			&exec.OwnerUserID,
			&exec.ConversationID,
		); err != nil {
			db.logger.Warn("failed to load the execution record list", zap.Error(err))
			continue
		}
		if endTime.Valid {
			exec.EndTime = &endTime.Time
		}
		if durationMs.Valid {
			exec.Duration = time.Duration(durationMs.Int64) * time.Millisecond
		}
		executions = append(executions, &exec)
	}

	return executions, nil
}

func appendToolExecutionAccessSQL(query string, args []interface{}, access RBACListAccess, hasWhere bool) (string, []interface{}) {
	if access.Scope == RBACScopeAll {
		return query, args
	}
	userID := strings.TrimSpace(access.UserID)
	joiner := " WHERE "
	if hasWhere {
		joiner = " AND "
	}
	if userID == "" {
		return query + joiner + "1=0", args
	}
	query += joiner + `(
		owner_user_id = ?
		OR (conversation_id IS NOT NULL AND conversation_id <> '' AND (
			EXISTS (SELECT 1 FROM conversations c WHERE c.id = tool_executions.conversation_id AND c.owner_user_id = ?)
			OR EXISTS (SELECT 1 FROM rbac_resource_assignments ra WHERE ra.user_id = ? AND ra.resource_type = 'conversation' AND ra.resource_id = tool_executions.conversation_id)
			OR EXISTS (SELECT 1 FROM conversations c JOIN projects p ON p.id = c.project_id WHERE c.id = tool_executions.conversation_id AND p.owner_user_id = ?)
			OR EXISTS (SELECT 1 FROM conversations c JOIN rbac_resource_assignments pra ON pra.resource_id = c.project_id WHERE c.id = tool_executions.conversation_id AND pra.user_id = ? AND pra.resource_type = 'project')
		))
	)`
	args = append(args, userID, userID, userID, userID, userID)
	return query, args
}

// GetToolExecution 根据ID获取单条工具执行记录
func (db *DB) GetToolExecution(id string) (*mcp.ToolExecution, error) {
	query := `
		SELECT id, tool_name, arguments, status, result, error, start_time, end_time, duration_ms,
		       COALESCE(partial_output, ''), COALESCE(partial_output_bytes, 0), COALESCE(partial_output_truncated, 0), partial_output_updated_at,
		       COALESCE(owner_user_id, ''), COALESCE(conversation_id, '')
		FROM tool_executions
		WHERE id = ?
	`

	row := db.QueryRow(query, id)

	var exec mcp.ToolExecution
	var argsJSON string
	var resultJSON sql.NullString
	var errorText sql.NullString
	var endTime sql.NullTime
	var durationMs sql.NullInt64
	var partialTruncated int
	var partialUpdatedAt sql.NullTime

	err := row.Scan(
		&exec.ID,
		&exec.ToolName,
		&argsJSON,
		&exec.Status,
		&resultJSON,
		&errorText,
		&exec.StartTime,
		&endTime,
		&durationMs,
		&exec.PartialOutput,
		&exec.PartialOutputBytes,
		&partialTruncated,
		&partialUpdatedAt,
		&exec.OwnerUserID,
		&exec.ConversationID,
	)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal([]byte(argsJSON), &exec.Arguments); err != nil {
		db.logger.Warn("failed to parse the execution arguments", zap.Error(err))
		exec.Arguments = make(map[string]interface{})
	}

	if resultJSON.Valid && resultJSON.String != "" {
		var result mcp.ToolResult
		if err := json.Unmarshal([]byte(resultJSON.String), &result); err != nil {
			db.logger.Warn("failed to parse the execution result", zap.Error(err))
		} else {
			exec.Result = &result
		}
	}

	if errorText.Valid {
		exec.Error = errorText.String
	}

	if endTime.Valid {
		exec.EndTime = &endTime.Time
	}

	if durationMs.Valid {
		exec.Duration = time.Duration(durationMs.Int64) * time.Millisecond
	}
	exec.PartialOutputTruncated = partialTruncated != 0
	if partialUpdatedAt.Valid {
		exec.PartialOutputUpdatedAt = &partialUpdatedAt.Time
	}

	return &exec, nil
}

// UserCanAccessToolExecution enforces ownership for monitor detail and mutation
// endpoints. Legacy records without an owner or conversation fail closed for
// non-global users.
func (db *DB) UserCanAccessToolExecution(userID, scope, executionID string) bool {
	userID = strings.TrimSpace(userID)
	executionID = strings.TrimSpace(executionID)
	if userID == "" || executionID == "" {
		return false
	}
	if scope == RBACScopeAll {
		return true
	}
	var ownerUserID, conversationID sql.NullString
	if err := db.QueryRow(`SELECT owner_user_id, conversation_id FROM tool_executions WHERE id = ?`, executionID).Scan(&ownerUserID, &conversationID); err != nil {
		return false
	}
	if strings.TrimSpace(ownerUserID.String) == userID {
		return true
	}
	conversation := strings.TrimSpace(conversationID.String)
	return conversation != "" && db.UserCanAccessResource(userID, scope, "conversation", conversation)
}

// CancelOrphanedRunningToolExecutions 将仍为 running 的记录批量标记为 orphaned（如进程重启后无对应执行协程）。
func (db *DB) CancelOrphanedRunningToolExecutions(endTime time.Time, errMsg string) (int64, error) {
	errMsg = strings.TrimSpace(errMsg)
	if errMsg == "" {
		errMsg = "execution interrupted (service restart or session ended)"
	}
	query := `
		UPDATE tool_executions
		SET status = 'orphaned',
		    error = ?,
		    end_time = ?,
		    duration_ms = MAX(0, CAST((julianday(?) - julianday(start_time)) * 86400000 AS INTEGER))
		WHERE status = 'running'
	`
	res, err := db.Exec(query, errMsg, endTime, endTime)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// FinalizeStaleRunningToolExecutions 将「非活跃且超过 minAge」的 running 记录标记为 orphaned。
// activeIDs 为当前进程内仍登记 cancel 的 executionId；不在集合内且已超时的视为孤儿记录。
func (db *DB) FinalizeStaleRunningToolExecutions(endTime time.Time, minAge time.Duration, activeIDs map[string]struct{}, errMsg string) (int64, error) {
	errMsg = strings.TrimSpace(errMsg)
	if errMsg == "" {
		errMsg = "execution interrupted (session ended)"
	}
	if minAge < 0 {
		minAge = 0
	}
	cutoff := endTime.Add(-minAge)
	rows, err := db.Query(`
		SELECT id, start_time FROM tool_executions
		WHERE status = 'running' AND start_time <= ?
	`, cutoff)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	type staleRow struct {
		id        string
		startTime time.Time
	}
	var stale []staleRow
	for rows.Next() {
		var row staleRow
		if err := rows.Scan(&row.id, &row.startTime); err != nil {
			db.logger.Warn("failed to read stale running execution records", zap.Error(err))
			continue
		}
		if activeIDs != nil {
			if _, active := activeIDs[row.id]; active {
				continue
			}
		}
		stale = append(stale, row)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(stale) == 0 {
		return 0, nil
	}

	var affected int64
	for _, row := range stale {
		durationMs := endTime.Sub(row.startTime).Milliseconds()
		if durationMs < 0 {
			durationMs = 0
		}
		res, err := db.Exec(`
			UPDATE tool_executions
			SET status = 'orphaned', error = ?, end_time = ?, duration_ms = ?
			WHERE id = ? AND status = 'running'
		`, errMsg, endTime, durationMs, row.id)
		if err != nil {
			db.logger.Warn("failed to update stale running execution records", zap.Error(err), zap.String("executionId", row.id))
			continue
		}
		n, _ := res.RowsAffected()
		affected += n
	}
	return affected, nil
}

// DeleteToolExecution 删除工具执行记录
func (db *DB) DeleteToolExecution(id string) error {
	query := `DELETE FROM tool_executions WHERE id = ?`
	_, err := db.Exec(query, id)
	if err != nil {
		db.logger.Error("failed to delete the tool execution record", zap.Error(err), zap.String("executionId", id))
		return err
	}
	return nil
}

// DeleteToolExecutions 批量删除工具执行记录
func (db *DB) DeleteToolExecutions(ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	// 构建 IN 查询的占位符
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	query := `DELETE FROM tool_executions WHERE id IN (` + strings.Join(placeholders, ",") + `)`
	_, err := db.Exec(query, args...)
	if err != nil {
		db.logger.Error("failed to bulk delete tool execution records", zap.Error(err), zap.Int("count", len(ids)))
		return err
	}
	return nil
}

// GetToolExecutionsByIds 根据ID列表获取工具执行记录（用于批量删除前获取统计信息）
func (db *DB) GetToolExecutionsByIds(ids []string) ([]*mcp.ToolExecution, error) {
	if len(ids) == 0 {
		return []*mcp.ToolExecution{}, nil
	}

	// 构建 IN 查询的占位符
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	query := `
		SELECT id, tool_name, arguments, status, result, error, start_time, end_time, duration_ms, COALESCE(owner_user_id, ''), COALESCE(conversation_id, '')
		FROM tool_executions
		WHERE id IN (` + strings.Join(placeholders, ",") + `)
	`

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var executions []*mcp.ToolExecution
	for rows.Next() {
		var exec mcp.ToolExecution
		var argsJSON string
		var resultJSON sql.NullString
		var errorText sql.NullString
		var endTime sql.NullTime
		var durationMs sql.NullInt64

		err := rows.Scan(
			&exec.ID,
			&exec.ToolName,
			&argsJSON,
			&exec.Status,
			&resultJSON,
			&errorText,
			&exec.StartTime,
			&endTime,
			&durationMs,
			&exec.OwnerUserID,
			&exec.ConversationID,
		)
		if err != nil {
			db.logger.Warn("failed to load the execution record", zap.Error(err))
			continue
		}

		// 解析参数
		if err := json.Unmarshal([]byte(argsJSON), &exec.Arguments); err != nil {
			db.logger.Warn("failed to parse the execution arguments", zap.Error(err))
			exec.Arguments = make(map[string]interface{})
		}

		// 解析结果
		if resultJSON.Valid && resultJSON.String != "" {
			var result mcp.ToolResult
			if err := json.Unmarshal([]byte(resultJSON.String), &result); err != nil {
				db.logger.Warn("failed to parse the execution result", zap.Error(err))
			} else {
				exec.Result = &result
			}
		}

		// 设置错误
		if errorText.Valid {
			exec.Error = errorText.String
		}

		// 设置结束时间
		if endTime.Valid {
			exec.EndTime = &endTime.Time
		}

		// 设置持续时间
		if durationMs.Valid {
			exec.Duration = time.Duration(durationMs.Int64) * time.Millisecond
		}

		executions = append(executions, &exec)
	}

	return executions, nil
}

type toolExecutionStatDelta struct {
	totalCalls   int
	successCalls int
	failedCalls  int
}

// PurgeToolExecutionsBefore deletes executions older than cutoff and adjusts tool_stats.
func (db *DB) PurgeToolExecutionsBefore(cutoff time.Time) (int64, error) {
	query := `
		SELECT tool_name, status, COUNT(*) AS cnt
		FROM tool_executions
		WHERE ` + sqliteEpochGE("start_time", "<") + `
		GROUP BY tool_name, status
	`
	rows, err := db.Query(query, formatSQLiteUTC(cutoff))
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	deltas := make(map[string]*toolExecutionStatDelta)
	for rows.Next() {
		var toolName, status string
		var count int
		if err := rows.Scan(&toolName, &status, &count); err != nil {
			db.logger.Warn("failed to read the count of execution records pending cleanup", zap.Error(err))
			continue
		}
		toolName = strings.TrimSpace(toolName)
		if toolName == "" || count <= 0 {
			continue
		}
		delta := deltas[toolName]
		if delta == nil {
			delta = &toolExecutionStatDelta{}
			deltas[toolName] = delta
		}
		delta.totalCalls += count
		switch status {
		case "failed", "hard_timeout", "orphaned":
			delta.failedCalls += count
		case "completed":
			delta.successCalls += count
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	res, err := db.Exec(`DELETE FROM tool_executions WHERE `+sqliteEpochGE("start_time", "<"), formatSQLiteUTC(cutoff))
	if err != nil {
		return 0, err
	}
	deleted, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}

	for toolName, delta := range deltas {
		if err := db.DecreaseToolStats(toolName, delta.totalCalls, delta.successCalls, delta.failedCalls); err != nil {
			db.logger.Warn("failed to update stats after purging expired execution records",
				zap.Error(err),
				zap.String("toolName", toolName),
			)
		}
	}

	return deleted, nil
}

// SaveToolStats 保存工具统计信息
func (db *DB) SaveToolStats(toolName string, stats *mcp.ToolStats) error {
	var lastCallTime sql.NullTime
	if stats.LastCallTime != nil {
		lastCallTime = sql.NullTime{Time: *stats.LastCallTime, Valid: true}
	}

	query := `
		INSERT OR REPLACE INTO tool_stats 
		(tool_name, total_calls, success_calls, failed_calls, last_call_time, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`

	_, err := db.Exec(query,
		toolName,
		stats.TotalCalls,
		stats.SuccessCalls,
		stats.FailedCalls,
		lastCallTime,
		time.Now(),
	)

	if err != nil {
		db.logger.Error("failed to save tool stats", zap.Error(err), zap.String("toolName", toolName))
		return err
	}

	return nil
}

// LoadToolStats 加载所有工具统计信息
func (db *DB) LoadToolStats() (map[string]*mcp.ToolStats, error) {
	query := `
		SELECT stats.tool_name, total_calls, success_calls, failed_calls, last_call_time,
			COALESCE(blocked.calls, 0)
		FROM tool_stats stats
		LEFT JOIN (SELECT tool_name, COUNT(*) AS calls FROM tool_executions WHERE status = 'blocked' GROUP BY tool_name) blocked
		ON blocked.tool_name = stats.tool_name
	`

	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	stats := make(map[string]*mcp.ToolStats)
	for rows.Next() {
		var stat mcp.ToolStats
		var lastCallTime sql.NullTime

		err := rows.Scan(
			&stat.ToolName,
			&stat.TotalCalls,
			&stat.SuccessCalls,
			&stat.FailedCalls,
			&lastCallTime,
			&stat.BlockedCalls,
		)
		if err != nil {
			db.logger.Warn("failed to load stats", zap.Error(err))
			continue
		}

		if lastCallTime.Valid {
			stat.LastCallTime = &lastCallTime.Time
		}

		stats[stat.ToolName] = &stat
	}

	return stats, nil
}

// UpdateToolStats 更新工具统计信息（累加模式）
func (db *DB) UpdateToolStats(toolName string, totalCalls, successCalls, failedCalls int, lastCallTime *time.Time) error {
	var lastCallTimeSQL sql.NullTime
	if lastCallTime != nil {
		lastCallTimeSQL = sql.NullTime{Time: *lastCallTime, Valid: true}
	}

	query := `
		INSERT INTO tool_stats (tool_name, total_calls, success_calls, failed_calls, last_call_time, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(tool_name) DO UPDATE SET
			total_calls = total_calls + ?,
			success_calls = success_calls + ?,
			failed_calls = failed_calls + ?,
			last_call_time = COALESCE(?, last_call_time),
			updated_at = ?
	`

	_, err := db.Exec(query,
		toolName, totalCalls, successCalls, failedCalls, lastCallTimeSQL, time.Now(),
		totalCalls, successCalls, failedCalls, lastCallTimeSQL, time.Now(),
	)

	if err != nil {
		db.logger.Error("failed to update tool stats", zap.Error(err), zap.String("toolName", toolName))
		return err
	}

	return nil
}

// CallsTimelineBucket 调用趋势时间桶
type CallsTimelineBucket struct {
	BucketTime time.Time
	Total      int
	Failed     int
	Blocked    int
}

// truncateCallsTimelineBucket 将时间截断到趋势图桶边界（本地时区，与 handler 侧 truncateToBucket 一致）
func truncateCallsTimelineBucket(t time.Time, dailyBuckets bool) time.Time {
	t = t.In(time.Local)
	if dailyBuckets {
		y, m, d := t.Date()
		return time.Date(y, m, d, 0, 0, 0, 0, time.Local)
	}
	return t.Truncate(time.Hour)
}

// LoadCallsTimeline 按时间范围加载调用趋势（since 起至今，含边界）
func (db *DB) LoadCallsTimeline(since time.Time, dailyBuckets bool) ([]CallsTimelineBucket, error) {
	var query string
	if dailyBuckets {
		query = `
			SELECT date(start_time, 'localtime') AS bucket,
				COUNT(*) AS total,
				SUM(CASE WHEN status IN ('failed', 'hard_timeout', 'orphaned') THEN 1 ELSE 0 END) AS failed,
				SUM(CASE WHEN status = 'blocked' THEN 1 ELSE 0 END) AS blocked
			FROM tool_executions
			WHERE start_time >= ?
			GROUP BY bucket
			ORDER BY bucket
		`
	} else {
		query = `
			SELECT strftime('%Y-%m-%d %H:00:00', start_time, 'localtime') AS bucket,
				COUNT(*) AS total,
				SUM(CASE WHEN status IN ('failed', 'hard_timeout', 'orphaned') THEN 1 ELSE 0 END) AS failed,
				SUM(CASE WHEN status = 'blocked' THEN 1 ELSE 0 END) AS blocked
			FROM tool_executions
			WHERE start_time >= ?
			GROUP BY bucket
			ORDER BY bucket
		`
	}

	rows, err := db.Query(query, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	buckets := make([]CallsTimelineBucket, 0)
	for rows.Next() {
		var bucketStr string
		var total, failed, blocked int
		if err := rows.Scan(&bucketStr, &total, &failed, &blocked); err != nil {
			db.logger.Warn("failed to load the call trend", zap.Error(err))
			continue
		}
		bucketTime, err := parseCallsTimelineBucket(bucketStr, dailyBuckets)
		if err != nil {
			db.logger.Warn("failed to parse a call trend time bucket", zap.Error(err), zap.String("bucket", bucketStr))
			continue
		}
		buckets = append(buckets, CallsTimelineBucket{
			BucketTime: bucketTime,
			Total:      total,
			Failed:     failed,
			Blocked:    blocked,
		})
	}
	return buckets, nil
}

func parseCallsTimelineBucket(bucketStr string, dailyBuckets bool) (time.Time, error) {
	if dailyBuckets {
		return time.ParseInLocation("2006-01-02", bucketStr, time.Local)
	}
	return time.ParseInLocation("2006-01-02 15:04:05", bucketStr, time.Local)
}

// DecreaseToolStats 减少工具统计信息（用于删除执行记录时）
// 如果统计信息变为0，则删除该统计记录
func (db *DB) DecreaseToolStats(toolName string, totalCalls, successCalls, failedCalls int) error {
	// 先更新统计信息
	query := `
		UPDATE tool_stats SET
			total_calls = CASE WHEN total_calls - ? < 0 THEN 0 ELSE total_calls - ? END,
			success_calls = CASE WHEN success_calls - ? < 0 THEN 0 ELSE success_calls - ? END,
			failed_calls = CASE WHEN failed_calls - ? < 0 THEN 0 ELSE failed_calls - ? END,
			updated_at = ?
		WHERE tool_name = ?
	`

	_, err := db.Exec(query, totalCalls, totalCalls, successCalls, successCalls, failedCalls, failedCalls, time.Now(), toolName)
	if err != nil {
		db.logger.Error("failed to decrement tool stats", zap.Error(err), zap.String("toolName", toolName))
		return err
	}

	// 检查更新后的 total_calls 是否为 0，如果是则删除该统计记录
	checkQuery := `SELECT total_calls FROM tool_stats WHERE tool_name = ?`
	var newTotalCalls int
	err = db.QueryRow(checkQuery, toolName).Scan(&newTotalCalls)
	if err != nil {
		// 如果查询失败（记录不存在），直接返回
		return nil
	}

	// 如果 total_calls 为 0，删除该统计记录
	if newTotalCalls == 0 {
		deleteQuery := `DELETE FROM tool_stats WHERE tool_name = ?`
		_, err = db.Exec(deleteQuery, toolName)
		if err != nil {
			db.logger.Warn("failed to delete zero-stats records", zap.Error(err), zap.String("toolName", toolName))
			// 不返回错误，因为主要操作（更新统计）已成功
		} else {
			db.logger.Info("deleted zero-stats records", zap.String("toolName", toolName))
		}
	}

	return nil
}
