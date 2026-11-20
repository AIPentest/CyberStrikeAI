package database

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// AssetCollection is a project-owned set of existing assets.
type AssetCollection struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"project_id"`
	Name        string    `json:"name"`
	Source      string    `json:"source,omitempty"`
	SourceQuery string    `json:"source_query,omitempty"`
	MemberCount int       `json:"member_count"`
	Observed    int       `json:"observed"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

const observationEvidenceMeaning = "已有观察证据：成功原始回执支持的测评观察，或带成功工具回执的已确认项目事实。同一资产只计一次。这不是扫描覆盖，也不表示没有漏洞。"

// An asset counts once when either evidence path is present.
func observationAssetPredicate(assetIDExpr, projectClause string) string {
	return `(
		EXISTS (
			SELECT 1 FROM deep_assessment_facts f
			WHERE f.confidence='observed'
			AND EXISTS (SELECT 1 FROM json_each(f.assets_json) j WHERE j.value=` + assetIDExpr + `)
			AND EXISTS (
				SELECT 1 FROM json_each(f.evidence_json) e
				JOIN deep_assessment_calls c ON c.id=e.value
				WHERE c.task_id=f.task_id AND c.step_id=f.step_id AND c.status='completed'
				AND c.tool NOT GLOB 'assessment_*'
				AND c.tool NOT IN ('record_vulnerability','list_vulnerabilities','get_vulnerability')
			)
		)
		OR EXISTS (
			SELECT 1 FROM project_fact_assets pa
			JOIN project_facts pf ON pf.id=pa.fact_id AND pf.confidence='confirmed'
			JOIN project_fact_receipts pr ON pr.fact_id=pf.id
			JOIN tool_executions te ON te.id=pr.execution_id AND te.status='completed'
			WHERE pa.asset_id=` + assetIDExpr + projectClause + `
		)
	)`
}

// ObservationCoverage counts accessible assets, or the members of one collection.
// total is the population; observed is how many of them have observation evidence.
func (db *DB) ObservationCoverage(access RBACListAccess, collectionID string) (int, int, error) {
	collectionID = strings.TrimSpace(collectionID)
	var query string
	var args []interface{}
	if collectionID == "" {
		query = `SELECT COUNT(*), COALESCE(SUM(CASE WHEN ` + observationAssetPredicate("assets.id", "") + ` THEN 1 ELSE 0 END),0) FROM assets WHERE 1=1`
		query, args = appendAssetAccess(query, nil, access, "assets")
	} else {
		query = `SELECT COUNT(*), COALESCE(SUM(CASE WHEN ` + observationAssetPredicate("assets.id", "") + ` THEN 1 ELSE 0 END),0)
			FROM asset_collection_members m JOIN assets ON assets.id=m.asset_id WHERE m.collection_id=?`
		args = []interface{}{collectionID}
		query, args = appendAssetAccess(query, args, access, "assets")
	}
	var total, observed int
	err := db.QueryRow(query, args...).Scan(&total, &observed)
	return total, observed, err
}

// ObservationCoverageVisible applies the caller's project read boundary to shared facts.
func (db *DB) ObservationCoverageVisible(access RBACListAccess, vis FactVisibility, collectionID string) (int, int, error) {
	clause, clauseArgs := vis.projectSQL("pf")
	collectionID = strings.TrimSpace(collectionID)
	var query string
	args := append([]any{}, clauseArgs...)
	if collectionID == "" {
		query = `SELECT COUNT(*), COALESCE(SUM(CASE WHEN ` + observationAssetPredicate("assets.id", clause) + ` THEN 1 ELSE 0 END),0) FROM assets WHERE 1=1`
		query, args = appendAssetAccess(query, args, access, "assets")
	} else {
		query = `SELECT COUNT(*), COALESCE(SUM(CASE WHEN ` + observationAssetPredicate("assets.id", clause) + ` THEN 1 ELSE 0 END),0)
			FROM asset_collection_members m JOIN assets ON assets.id=m.asset_id WHERE m.collection_id=?`
		args = append(args, collectionID)
		query, args = appendAssetAccess(query, args, access, "assets")
	}
	var total, observed int
	err := db.QueryRow(query, args...).Scan(&total, &observed)
	return total, observed, err
}

func (db *DB) CreateAssetCollection(projectID, name, source, sourceQuery, owner string, assetIDs []string) (*AssetCollection, error) {
	projectID = strings.TrimSpace(projectID)
	name = strings.TrimSpace(name)
	if projectID == "" {
		return nil, fmt.Errorf("资产集合必须属于项目")
	}
	if name == "" {
		return nil, fmt.Errorf("资产集合名称不能为空")
	}
	if utf8.RuneCountInString(name) > 120 {
		name = string([]rune(name)[:120])
	}
	var projectExists int
	if err := db.QueryRow(`SELECT COUNT(*) FROM projects WHERE id=?`, projectID).Scan(&projectExists); err != nil {
		return nil, err
	}
	if projectExists != 1 {
		return nil, fmt.Errorf("项目不存在")
	}
	now := time.Now().UTC()
	id := uuid.NewString()
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT INTO asset_collections(id,project_id,name,source,source_query,created_by,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`,
		id, projectID, name, strings.TrimSpace(source), strings.TrimSpace(sourceQuery), strings.TrimSpace(owner), now, now); err != nil {
		return nil, err
	}
	members, err := insertCollectionMembers(tx, id, projectID, assetIDs, now)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &AssetCollection{ID: id, ProjectID: projectID, Name: name, Source: strings.TrimSpace(source), SourceQuery: strings.TrimSpace(sourceQuery), MemberCount: members, CreatedAt: now, UpdatedAt: now}, nil
}

func insertCollectionMembers(tx *sql.Tx, collectionID, projectID string, assetIDs []string, now time.Time) (int, error) {
	seen := map[string]bool{}
	n := 0
	for _, id := range assetIDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		var assetProject string
		err := tx.QueryRow(`SELECT COALESCE(project_id,'') FROM assets WHERE id=?`, id).Scan(&assetProject)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return 0, err
		}
		if assetProject != "" && assetProject != projectID {
			continue
		}
		if _, err = tx.Exec(`INSERT OR IGNORE INTO asset_collection_members(collection_id,asset_id,created_at) VALUES(?,?,?)`, collectionID, id, now); err != nil {
			return 0, err
		}
		n++
	}
	return n, nil
}

func (db *DB) ListAssetCollections(projectID string) ([]AssetCollection, error) {
	rows, err := db.Query(`SELECT id,project_id,name,source,source_query,created_at,updated_at,
		(SELECT COUNT(*) FROM asset_collection_members m WHERE m.collection_id=asset_collections.id)
		FROM asset_collections WHERE project_id=? ORDER BY updated_at DESC, id`, strings.TrimSpace(projectID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AssetCollection{}
	for rows.Next() {
		var item AssetCollection
		if err = rows.Scan(&item.ID, &item.ProjectID, &item.Name, &item.Source, &item.SourceQuery, &item.CreatedAt, &item.UpdatedAt, &item.MemberCount); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (db *DB) GetAssetCollection(id string) (*AssetCollection, error) {
	var item AssetCollection
	err := db.QueryRow(`SELECT id,project_id,name,source,source_query,created_at,updated_at,
		(SELECT COUNT(*) FROM asset_collection_members m WHERE m.collection_id=asset_collections.id)
		FROM asset_collections WHERE id=?`, strings.TrimSpace(id)).Scan(&item.ID, &item.ProjectID, &item.Name, &item.Source, &item.SourceQuery, &item.CreatedAt, &item.UpdatedAt, &item.MemberCount)
	if err != nil {
		return nil, err
	}
	_, observed, err := db.ObservationCoverage(RBACListAccess{Scope: RBACScopeAll}, item.ID)
	if err != nil {
		return nil, err
	}
	item.Observed = observed
	return &item, nil
}

func (db *DB) CollectionMemberIDs(id string, limit int) ([]string, error) {
	if limit <= 0 || limit > 5000 {
		limit = 5000
	}
	rows, err := db.Query(`SELECT asset_id FROM asset_collection_members WHERE collection_id=? ORDER BY created_at, asset_id LIMIT ?`, strings.TrimSpace(id), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var assetID string
		if err = rows.Scan(&assetID); err != nil {
			return nil, err
		}
		out = append(out, assetID)
	}
	return out, rows.Err()
}
