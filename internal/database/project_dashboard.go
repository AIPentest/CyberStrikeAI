package database

import (
	"fmt"
	"strings"
	"time"
)

// ProjectDashboardFact 仪表盘跨项目近期事实条目。
type ProjectDashboardFact struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"project_id"`
	ProjectName string    `json:"project_name"`
	FactKey     string    `json:"fact_key"`
	Category    string    `json:"category"`
	Summary     string    `json:"summary"`
	Confidence  string    `json:"confidence"`
	Pinned      bool      `json:"pinned"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ProjectDashboardTotals 仪表盘项目事实汇总计数。
type ProjectDashboardTotals struct {
	ActiveProjects int `json:"active_projects"`
	TotalFacts     int `json:"total_facts"`
}

// ProjectDashboardSummary 仪表盘项目情报摘要。
type ProjectDashboardSummary struct {
	RecentFacts []ProjectDashboardFact `json:"recent_facts"`
	Totals      ProjectDashboardTotals `json:"totals"`
}

// GetProjectDashboardSummary 聚合跨项目近期事实（仅活跃项目、排除 deprecated）。
func (db *DB) GetProjectDashboardSummary(factLimit int) (*ProjectDashboardSummary, error) {
	return db.GetProjectDashboardSummaryForAccess(factLimit, "", "")
}

func (db *DB) GetProjectDashboardSummaryForAccess(factLimit int, userID, scope string) (*ProjectDashboardSummary, error) {
	if factLimit <= 0 {
		factLimit = 5
	}
	if factLimit > 50 {
		factLimit = 50
	}

	out := &ProjectDashboardSummary{
		RecentFacts: []ProjectDashboardFact{},
	}

	projectAccess := ""
	args := []interface{}{}
	userID = strings.TrimSpace(userID)
	if userID != "" && scope != RBACScopeAll {
		projectAccess = ` AND (
			p.owner_user_id = ?
			OR EXISTS (
				SELECT 1 FROM rbac_resource_assignments ra
				WHERE ra.user_id = ? AND ra.resource_type = 'project' AND ra.resource_id = p.id
			)
		)`
		args = append(args, userID, userID)
	}

	if err := db.QueryRow(`SELECT COUNT(*) FROM projects p WHERE p.status = 'active'`+projectAccess, args...).Scan(&out.Totals.ActiveProjects); err != nil {
		return nil, fmt.Errorf("failed to count active projects: %w", err)
	}
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM project_facts f
		 INNER JOIN projects p ON p.id = f.project_id
		 WHERE f.confidence != 'deprecated' AND p.status = 'active'`+projectAccess,
		args...,
	).Scan(&out.Totals.TotalFacts); err != nil {
		return nil, fmt.Errorf("failed to count facts: %w", err)
	}

	queryArgs := append([]interface{}{}, args...)
	queryArgs = append(queryArgs, factLimit)
	rows, err := db.Query(
		`SELECT f.id, f.project_id, p.name, f.fact_key, f.category, f.summary, f.confidence, f.pinned, f.updated_at
		 FROM project_facts f
		 INNER JOIN projects p ON p.id = f.project_id
		 WHERE f.confidence != 'deprecated' AND p.status = 'active'`+projectAccess+`
		 ORDER BY f.pinned DESC, f.updated_at DESC
		 LIMIT ?`,
		queryArgs...,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query recent facts: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var item ProjectDashboardFact
		var pinned int
		var updatedAt string
		if err := rows.Scan(
			&item.ID, &item.ProjectID, &item.ProjectName, &item.FactKey,
			&item.Category, &item.Summary, &item.Confidence, &pinned, &updatedAt,
		); err != nil {
			return nil, err
		}
		item.Pinned = pinned != 0
		item.ProjectName = strings.TrimSpace(item.ProjectName)
		item.UpdatedAt = parseDBTime(updatedAt)
		out.RecentFacts = append(out.RecentFacts, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
