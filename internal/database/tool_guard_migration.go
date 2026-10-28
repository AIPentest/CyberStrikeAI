package database

import (
	"encoding/json"
	"strconv"
	"strings"

	"cyberstrike-ai/internal/mcp"
)

// legacyToolGuardPrefix matches historical rows written by the old local guard
// before the structured marker existed. It is a byte-identical legacy protocol
// marker, not display text: new executions are recognized structurally.
const legacyToolGuardPrefix = "工具调用已被安全规则拦截"

// Only the exact envelope emitted by the old local guard is recognized here.
// New executions use the structured marker and never infer policy from text.
func isLegacyToolGuardRefusal(text string) bool {
	if !strings.HasPrefix(text, legacyToolGuardPrefix+"：") && !strings.HasPrefix(text, legacyToolGuardPrefix+"\n规则: ") {
		return false
	}
	matchIndex := strings.LastIndex(text, "\n匹配内容: ")
	if matchIndex < 0 {
		return false
	}
	if _, err := strconv.Unquote(text[matchIndex+len("\n匹配内容: "):]); err != nil {
		return false
	}
	ruleIndex := strings.LastIndex(text[:matchIndex], "\n规则: ")
	if ruleIndex < 0 {
		return false
	}
	rule := text[ruleIndex+len("\n规则: ") : matchIndex]
	idIndex := strings.LastIndex(rule, " (")
	return idIndex > 0 && strings.HasSuffix(rule, ")") && len(rule[idIndex+2:len(rule)-1]) > 0 && !strings.Contains(rule, "\n")
}

// migrateLegacyToolGuardBlocks is idempotent because only failed records qualify.
// Keeping status and accumulated failure counts in one transaction makes monitor
// filters, badges and statistics agree immediately after upgrading.
func (db *DB) migrateLegacyToolGuardBlocks() error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.Query(`SELECT id, tool_name, error, COALESCE(result, '') FROM tool_executions WHERE status = 'failed' AND error LIKE ?`, legacyToolGuardPrefix+"%")
	if err != nil {
		return err
	}
	type record struct{ id, tool, reason, result string }
	var records []record
	for rows.Next() {
		var r record
		if err := rows.Scan(&r.id, &r.tool, &r.reason, &r.result); err != nil {
			rows.Close()
			return err
		}
		if isLegacyToolGuardRefusal(r.reason) {
			records = append(records, r)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, r := range records {
		var result mcp.ToolResult
		_ = json.Unmarshal([]byte(r.result), &result)
		if len(result.Content) == 0 {
			result.Content = []mcp.Content{{Type: "text", Text: r.reason}}
		}
		result.Blocked, result.IsError = true, true
		encoded, err := json.Marshal(result)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE tool_executions SET status = 'blocked', result = ? WHERE id = ?`, string(encoded), r.id); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE tool_stats SET failed_calls = MAX(0, failed_calls - 1) WHERE tool_name = ?`, r.tool); err != nil {
			return err
		}
	}
	return tx.Commit()
}
