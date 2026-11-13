package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// FindNearestToolExecutionArguments returns the arguments for the execution record
// closest to a persisted tool_call detail. Agent can persist a tool_call with empty
// model arguments while the monitor execution row still has the real command/URL.
func (db *DB) FindNearestToolExecutionArguments(conversationID, toolName string, at time.Time, window time.Duration) (string, map[string]interface{}, error) {
	conversationID = strings.TrimSpace(conversationID)
	toolName = strings.TrimSpace(toolName)
	if db == nil || conversationID == "" || toolName == "" || at.IsZero() {
		return "", nil, sql.ErrNoRows
	}
	if window <= 0 {
		window = 5 * time.Second
	}
	names := []string{toolName}
	if !strings.Contains(toolName, "::") {
		names = append(names, "eino_fs::"+toolName)
	}
	start := at.Add(-window)
	end := at.Add(window)
	rows, err := db.Query(`
SELECT id, arguments
FROM tool_executions
WHERE conversation_id = ?
  AND tool_name IN (?, ?)
  AND julianday(start_time) BETWEEN julianday(?) AND julianday(?)
ORDER BY ABS(julianday(start_time) - julianday(?)) ASC, start_time ASC
LIMIT 1`, conversationID, names[0], names[len(names)-1], start, end, at)
	if err != nil {
		return "", nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return "", nil, err
		}
		return "", nil, sql.ErrNoRows
	}
	var id string
	var raw string
	if err := rows.Scan(&id, &raw); err != nil {
		return "", nil, err
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return "", nil, fmt.Errorf("parse tool execution arguments: %w", err)
	}
	return strings.TrimSpace(id), args, nil
}
