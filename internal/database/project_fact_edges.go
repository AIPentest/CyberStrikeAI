package database

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ValidProjectFactEdgeTypes 项目事实图允许的边类型。
var ValidProjectFactEdgeTypes = map[string]struct{}{
	"depends_on":    {},
	"leads_to":      {},
	"enables":       {},
	"exploits":      {},
	"discovered_on": {},
	"contains":      {},
	"part_of":       {},
	"supports":      {},
}

// ProjectFactEdge 项目事实关系边（source → target）。
type ProjectFactEdge struct {
	ID                   string    `json:"id"`
	ProjectID            string    `json:"project_id"`
	SourceFactKey        string    `json:"source_fact_key"`
	TargetFactKey        string    `json:"target_fact_key"`
	EdgeType             string    `json:"edge_type"`
	Confidence           string    `json:"confidence"` // confirmed | tentative | deprecated
	SourceConversationID string    `json:"source_conversation_id,omitempty"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

// ProjectFactEdgeInput 写入边时的输入（出边：source → To）。
type ProjectFactEdgeInput struct {
	To         string `json:"to"`
	Type       string `json:"type"`
	Confidence string `json:"confidence,omitempty"`
}

// ProjectFactEdgeFromInput 写入入边时的输入（From → 当前事实）。
type ProjectFactEdgeFromInput struct {
	From       string `json:"from"`
	Type       string `json:"type"`
	Confidence string `json:"confidence,omitempty"`
}

// ProjectFactGraphNode 图 API 节点。
type ProjectFactGraphNode struct {
	ID         string `json:"id"`
	FactKey    string `json:"fact_key"`
	Category   string `json:"category"`
	Label      string `json:"label"`   // 图节点短标签（截断）
	Summary    string `json:"summary"` // 完整摘要（侧栏等详情用）
	Confidence string `json:"confidence"`
	Type       string `json:"type"`
	Pinned     bool   `json:"pinned"`
}

// ProjectFactGraphEdge 图 API 边。
type ProjectFactGraphEdge struct {
	ID         string `json:"id"`
	Source     string `json:"source"`
	Target     string `json:"target"`
	Type       string `json:"type"`
	Confidence string `json:"confidence"`
}

// ProjectFactGraph 项目事实图。
type ProjectFactGraph struct {
	Nodes []ProjectFactGraphNode `json:"nodes"`
	Edges []ProjectFactGraphEdge `json:"edges"`
}

// ValidateProjectFactEdgeType 校验边类型。
func ValidateProjectFactEdgeType(edgeType string) error {
	edgeType = strings.TrimSpace(strings.ToLower(edgeType))
	if edgeType == "" {
		return fmt.Errorf("edge type is required")
	}
	if _, ok := ValidProjectFactEdgeTypes[edgeType]; !ok {
		return fmt.Errorf("invalid edge type: %s", edgeType)
	}
	return nil
}

func normalizeEdgeConfidence(confidence string) string {
	confidence = strings.TrimSpace(strings.ToLower(confidence))
	switch confidence {
	case "confirmed", "deprecated":
		return confidence
	default:
		return "tentative"
	}
}

// ListProjectFactEdgesByProject 列出项目全部边。
func (db *DB) ListProjectFactEdgesByProject(projectID string) ([]*ProjectFactEdge, error) {
	rows, err := db.Query(
		`SELECT id, project_id, source_fact_key, target_fact_key, edge_type, confidence,
		        COALESCE(source_conversation_id,''), created_at, updated_at
		   FROM project_fact_edges
		  WHERE project_id = ?
		  ORDER BY created_at ASC, rowid ASC`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanProjectFactEdges(rows)
}

// ListOutgoingProjectFactEdges 列出某事实的全部出边。
func (db *DB) ListOutgoingProjectFactEdges(projectID, sourceFactKey string) ([]*ProjectFactEdge, error) {
	rows, err := db.Query(
		`SELECT id, project_id, source_fact_key, target_fact_key, edge_type, confidence,
		        COALESCE(source_conversation_id,''), created_at, updated_at
		   FROM project_fact_edges
		  WHERE project_id = ? AND source_fact_key = ?
		  ORDER BY created_at ASC, rowid ASC`,
		projectID, sourceFactKey,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanProjectFactEdges(rows)
}

// ListIncomingProjectFactEdges 列出某事实的全部入边。
func (db *DB) ListIncomingProjectFactEdges(projectID, targetFactKey string) ([]*ProjectFactEdge, error) {
	rows, err := db.Query(
		`SELECT id, project_id, source_fact_key, target_fact_key, edge_type, confidence,
		        COALESCE(source_conversation_id,''), created_at, updated_at
		   FROM project_fact_edges
		  WHERE project_id = ? AND target_fact_key = ?
		  ORDER BY created_at ASC, rowid ASC`,
		projectID, targetFactKey,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanProjectFactEdges(rows)
}

// ReplaceOutgoingProjectFactEdges 替换某事实的全部出边（links 省略时不调用）。
func (db *DB) ReplaceOutgoingProjectFactEdges(projectID, sourceFactKey, sourceConversationID string, inputs []ProjectFactEdgeInput) error {
	sourceFactKey = strings.TrimSpace(sourceFactKey)
	if sourceFactKey == "" {
		return fmt.Errorf("source_fact_key is required")
	}
	if _, err := db.Exec(
		`DELETE FROM project_fact_edges WHERE project_id = ? AND source_fact_key = ?`,
		projectID, sourceFactKey,
	); err != nil {
		return fmt.Errorf("failed to clear the old edges: %w", err)
	}
	for _, in := range inputs {
		target := strings.TrimSpace(in.To)
		if target == "" {
			continue
		}
		if err := ValidateFactKey(target); err != nil {
			return fmt.Errorf("invalid target fact_key (%s): %w", target, err)
		}
		if target == sourceFactKey {
			return fmt.Errorf("an edge cannot point to itself: %s", sourceFactKey)
		}
		if err := ValidateProjectFactEdgeType(in.Type); err != nil {
			return err
		}
		edge := &ProjectFactEdge{
			ID:                   uuid.New().String(),
			ProjectID:            projectID,
			SourceFactKey:        sourceFactKey,
			TargetFactKey:        target,
			EdgeType:             strings.ToLower(strings.TrimSpace(in.Type)),
			Confidence:           normalizeEdgeConfidence(in.Confidence),
			SourceConversationID: sourceConversationID,
			CreatedAt:            time.Now(),
			UpdatedAt:            time.Now(),
		}
		if err := db.insertProjectFactEdge(edge); err != nil {
			return err
		}
	}
	return nil
}

// ReplaceIncomingProjectFactEdges 替换某事实的全部入边（From 为来源 fact_key）。
func (db *DB) ReplaceIncomingProjectFactEdges(projectID, targetFactKey string, inputs []ProjectFactEdgeFromInput) error {
	targetFactKey = strings.TrimSpace(targetFactKey)
	if targetFactKey == "" {
		return fmt.Errorf("target_fact_key is required")
	}
	if _, err := db.Exec(
		`DELETE FROM project_fact_edges WHERE project_id = ? AND target_fact_key = ?`,
		projectID, targetFactKey,
	); err != nil {
		return fmt.Errorf("failed to clear the old incoming edges: %w", err)
	}
	for _, in := range inputs {
		source := strings.TrimSpace(in.From)
		if source == "" {
			continue
		}
		if err := ValidateFactKey(source); err != nil {
			return fmt.Errorf("invalid source fact_key (%s): %w", source, err)
		}
		if source == targetFactKey {
			return fmt.Errorf("an edge cannot point to itself: %s", targetFactKey)
		}
		if err := ValidateProjectFactEdgeType(in.Type); err != nil {
			return err
		}
		sourceConversationID := ""
		if srcFact, err := db.GetProjectFactByKey(projectID, source); err == nil && srcFact != nil {
			sourceConversationID = srcFact.SourceConversationID
		}
		edge := &ProjectFactEdge{
			ID:                   uuid.New().String(),
			ProjectID:            projectID,
			SourceFactKey:        source,
			TargetFactKey:        targetFactKey,
			EdgeType:             strings.ToLower(strings.TrimSpace(in.Type)),
			Confidence:           normalizeEdgeConfidence(in.Confidence),
			SourceConversationID: sourceConversationID,
			CreatedAt:            time.Now(),
			UpdatedAt:            time.Now(),
		}
		if err := db.insertProjectFactEdge(edge); err != nil {
			return err
		}
	}
	return nil
}

// GetProjectFactEdge 按 ID 获取边。
func (db *DB) GetProjectFactEdge(edgeID string) (*ProjectFactEdge, error) {
	var e ProjectFactEdge
	var createdAt, updatedAt string
	err := db.QueryRow(
		`SELECT id, project_id, source_fact_key, target_fact_key, edge_type, confidence,
		        COALESCE(source_conversation_id,''), created_at, updated_at
		   FROM project_fact_edges WHERE id = ?`, edgeID,
	).Scan(&e.ID, &e.ProjectID, &e.SourceFactKey, &e.TargetFactKey, &e.EdgeType, &e.Confidence,
		&e.SourceConversationID, &createdAt, &updatedAt)
	if err != nil {
		return nil, fmt.Errorf("edge not found")
	}
	e.CreatedAt = parseDBTime(createdAt)
	e.UpdatedAt = parseDBTime(updatedAt)
	return &e, nil
}

// AddProjectFactEdge 新增单条边（已存在则更新 confidence）。
func (db *DB) AddProjectFactEdge(projectID string, in ProjectFactEdgeInput, sourceFactKey, sourceConversationID string) (*ProjectFactEdge, error) {
	sourceFactKey = strings.TrimSpace(sourceFactKey)
	target := strings.TrimSpace(in.To)
	if sourceFactKey == "" || target == "" {
		return nil, fmt.Errorf("source and target are required")
	}
	if sourceFactKey == target {
		return nil, fmt.Errorf("an edge cannot point to itself")
	}
	if err := ValidateProjectFactEdgeType(in.Type); err != nil {
		return nil, err
	}
	if err := ValidateFactKey(target); err != nil {
		return nil, err
	}
	now := time.Now()
	e := &ProjectFactEdge{
		ID:                   uuid.New().String(),
		ProjectID:            projectID,
		SourceFactKey:        sourceFactKey,
		TargetFactKey:        target,
		EdgeType:             strings.ToLower(strings.TrimSpace(in.Type)),
		Confidence:           normalizeEdgeConfidence(in.Confidence),
		SourceConversationID: sourceConversationID,
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	_, err := db.Exec(
		`INSERT INTO project_fact_edges (
			id, project_id, source_fact_key, target_fact_key, edge_type, confidence,
			source_conversation_id, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(project_id, source_fact_key, target_fact_key, edge_type)
		DO UPDATE SET confidence = excluded.confidence, updated_at = excluded.updated_at`,
		e.ID, e.ProjectID, e.SourceFactKey, e.TargetFactKey, e.EdgeType, e.Confidence,
		nullIfEmpty(e.SourceConversationID), e.CreatedAt, e.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to add the edge: %w", err)
	}
	// 返回最新
	rows, err := db.Query(
		`SELECT id, project_id, source_fact_key, target_fact_key, edge_type, confidence,
		        COALESCE(source_conversation_id,''), created_at, updated_at
		   FROM project_fact_edges
		  WHERE project_id = ? AND source_fact_key = ? AND target_fact_key = ? AND edge_type = ?`,
		projectID, sourceFactKey, target, e.EdgeType,
	)
	if err != nil {
		return e, nil
	}
	defer rows.Close()
	list, err := scanProjectFactEdges(rows)
	if err != nil || len(list) == 0 {
		return e, nil
	}
	return list[0], nil
}

// DeleteProjectFactEdge 删除单条边。
func (db *DB) DeleteProjectFactEdge(edgeID string) error {
	res, err := db.Exec(`DELETE FROM project_fact_edges WHERE id = ?`, edgeID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("edge not found")
	}
	return nil
}

func (db *DB) insertProjectFactEdge(e *ProjectFactEdge) error {
	_, err := db.Exec(
		`INSERT INTO project_fact_edges (
			id, project_id, source_fact_key, target_fact_key, edge_type, confidence,
			source_conversation_id, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.ProjectID, e.SourceFactKey, e.TargetFactKey, e.EdgeType, e.Confidence,
		nullIfEmpty(e.SourceConversationID), e.CreatedAt, e.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to write the edge: %w", err)
	}
	return nil
}

// RenameProjectFactKeyEdges 事实 key 变更时同步边上的引用。
func (db *DB) RenameProjectFactKeyEdges(projectID, oldKey, newKey string) error {
	oldKey = strings.TrimSpace(oldKey)
	newKey = strings.TrimSpace(newKey)
	if oldKey == "" || newKey == "" || oldKey == newKey {
		return nil
	}
	now := time.Now()
	if _, err := db.Exec(
		`UPDATE project_fact_edges SET source_fact_key = ?, updated_at = ?
		  WHERE project_id = ? AND source_fact_key = ?`,
		newKey, now, projectID, oldKey,
	); err != nil {
		return err
	}
	_, err := db.Exec(
		`UPDATE project_fact_edges SET target_fact_key = ?, updated_at = ?
		  WHERE project_id = ? AND target_fact_key = ?`,
		newKey, now, projectID, oldKey,
	)
	return err
}

// DeleteProjectFactEdgesForKey 删除与某 fact_key 相关的全部边。
func (db *DB) DeleteProjectFactEdgesForKey(projectID, factKey string) error {
	_, err := db.Exec(
		`DELETE FROM project_fact_edges
		  WHERE project_id = ? AND (source_fact_key = ? OR target_fact_key = ?)`,
		projectID, factKey, factKey,
	)
	return err
}

// DeprecateProjectFactEdgesForKey 将关联边标记为 deprecated。
func (db *DB) DeprecateProjectFactEdgesForKey(projectID, factKey string) error {
	now := time.Now()
	_, err := db.Exec(
		`UPDATE project_fact_edges SET confidence = 'deprecated', updated_at = ?
		  WHERE project_id = ? AND (source_fact_key = ? OR target_fact_key = ?)
		    AND confidence != 'deprecated'`,
		now, projectID, factKey, factKey,
	)
	return err
}

func scanProjectFactEdges(rows *sql.Rows) ([]*ProjectFactEdge, error) {
	var out []*ProjectFactEdge
	for rows.Next() {
		var e ProjectFactEdge
		var createdAt, updatedAt string
		if err := rows.Scan(
			&e.ID, &e.ProjectID, &e.SourceFactKey, &e.TargetFactKey, &e.EdgeType, &e.Confidence,
			&e.SourceConversationID, &createdAt, &updatedAt,
		); err != nil {
			return nil, err
		}
		e.CreatedAt = parseDBTime(createdAt)
		e.UpdatedAt = parseDBTime(updatedAt)
		out = append(out, &e)
	}
	return out, rows.Err()
}
