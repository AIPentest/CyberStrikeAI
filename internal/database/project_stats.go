package database

import (
	"database/sql"
	"fmt"
	"strings"
)

// ProjectStats 项目聚合统计。
type ProjectStats struct {
	FactCount         int `json:"fact_count"`
	VulnCount         int `json:"vuln_count"`
	ConversationCount int `json:"conversation_count"`
	SparseFactCount   int `json:"sparse_fact_count"`
}

// GetProjectStatsCounts 统计项目下事实、漏洞、对话数量（不含 sparse，由 project 包补全）。
func (db *DB) GetProjectStatsCounts(projectID string) (*ProjectStats, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, fmt.Errorf("project_id is required")
	}
	if _, err := db.GetProject(projectID); err != nil {
		return nil, err
	}
	stats := &ProjectStats{}
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM project_facts WHERE project_id = ? AND confidence != 'deprecated'`,
		projectID,
	).Scan(&stats.FactCount); err != nil {
		return nil, fmt.Errorf("failed to count facts: %w", err)
	}
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM vulnerabilities WHERE project_id = ?`,
		projectID,
	).Scan(&stats.VulnCount); err != nil {
		return nil, fmt.Errorf("failed to count vulnerabilities: %w", err)
	}
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM conversations WHERE project_id = ?`,
		projectID,
	).Scan(&stats.ConversationCount); err != nil {
		return nil, fmt.Errorf("failed to count conversations: %w", err)
	}
	return stats, nil
}

// ListProjectFactsForSparseCheck 返回用于待补全检测的事实字段（非 deprecated）。
func (db *DB) ListProjectFactsForSparseCheck(projectID string) ([]struct {
	Category string
	FactKey  string
	Body     string
}, error) {
	rows, err := db.Query(
		`SELECT category, fact_key, COALESCE(body,'') FROM project_facts WHERE project_id = ? AND confidence != 'deprecated'`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []struct {
		Category string
		FactKey  string
		Body     string
	}
	for rows.Next() {
		var row struct {
			Category string
			FactKey  string
			Body     string
		}
		if err := rows.Scan(&row.Category, &row.FactKey, &row.Body); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// ListConversationsByProjectID 列出绑定到项目的对话。
func (db *DB) ListConversationsByProjectID(projectID string, limit, offset int) ([]*Conversation, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := db.Query(
		`SELECT id, title, COALESCE(pinned, 0), created_at, updated_at, project_id, role_name
		 FROM conversations WHERE project_id = ? ORDER BY updated_at DESC LIMIT ? OFFSET ?`,
		projectID, limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query project conversations: %w", err)
	}
	defer rows.Close()

	var conversations []*Conversation
	for rows.Next() {
		var conv Conversation
		var createdAt, updatedAt string
		var pinned int
		var pid sql.NullString
		var roleName sql.NullString
		if err := rows.Scan(&conv.ID, &conv.Title, &pinned, &createdAt, &updatedAt, &pid, &roleName); err != nil {
			return nil, err
		}
		if pid.Valid {
			conv.ProjectID = strings.TrimSpace(pid.String)
		}
		if roleName.Valid {
			conv.RoleName = normalizeConversationRoleName(roleName.String)
		}
		conv.CreatedAt = parseDBTime(createdAt)
		conv.UpdatedAt = parseDBTime(updatedAt)
		conv.Pinned = pinned != 0
		conversations = append(conversations, &conv)
	}
	return conversations, rows.Err()
}

// CountConversationsByProjectID 统计项目绑定对话数。
func (db *DB) CountConversationsByProjectID(projectID string) (int, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM conversations WHERE project_id = ?`, projectID).Scan(&n)
	return n, err
}
