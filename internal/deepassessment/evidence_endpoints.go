package deepassessment

import (
	"database/sql"
	"fmt"

	"cyberstrike-ai/internal/database"
)

type EndpointEvidence struct {
	database.AssetEndpoint
	Status string `json:"status"`
}

const evidencePageSize = 20
const originalEvidenceTool = `tool NOT GLOB 'assessment_*' AND tool NOT IN ('record_vulnerability','list_vulnerabilities','get_vulnerability')`
const trustedObservation = `f.confidence='observed' AND EXISTS(
	SELECT 1 FROM json_each(f.evidence_json) j JOIN deep_assessment_calls c ON c.id=j.value
	WHERE c.task_id=f.task_id AND c.step_id=f.step_id AND c.status='completed' AND c.` + originalEvidenceTool + `)`

// A link is an annotation on an existing fact and endpoint. It never runs a tool.
func (s *Store) SetEndpointEvidence(task, asset, endpoint, fact string, linked bool) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var valid bool
	if err = tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM asset_endpoints e JOIN deep_assessment_assets a ON a.asset_id=e.asset_id
		JOIN deep_assessment_facts f ON f.task_id=a.task_id
		WHERE e.id=? AND e.asset_id=? AND a.task_id=? AND f.id=? AND EXISTS(SELECT 1 FROM json_each(f.assets_json) j WHERE j.value=e.asset_id))`, endpoint, asset, task, fact).Scan(&valid); err != nil {
		return err
	}
	if !valid {
		return sql.ErrNoRows
	}
	query := `INSERT OR IGNORE INTO deep_assessment_endpoint_evidence(endpoint_id,fact_id) VALUES(?,?)`
	if !linked {
		query = `DELETE FROM deep_assessment_endpoint_evidence WHERE endpoint_id=? AND fact_id=?`
	}
	if _, err = tx.Exec(query, endpoint, fact); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) EvidenceEndpoint(asset, endpoint string) (database.AssetEndpoint, error) {
	var e database.AssetEndpoint
	err := s.DB.QueryRow(`SELECT id,asset_id,method,path,title FROM asset_endpoints WHERE asset_id=? AND id=?`, asset, endpoint).
		Scan(&e.ID, &e.AssetID, &e.Method, &e.Path, &e.Title)
	return e, err
}

func (s *Store) EndpointEvidence(task, asset, status string, offset int) ([]EndpointEvidence, map[string]int, bool, error) {
	if status != "" && status != "observed" && status != "needs_review" && status != "missing" {
		return nil, nil, false, fmt.Errorf("invalid endpoint evidence status")
	}
	base := `WITH coverage AS (
		SELECT e.id,e.asset_id,e.method,e.path,e.title,
			CASE WHEN MAX(CASE WHEN ` + trustedObservation + ` THEN 1 ELSE 0 END)=1 THEN 'observed'
				WHEN COUNT(f.id)>0 THEN 'needs_review' ELSE 'missing' END AS evidence_status
		FROM asset_endpoints e LEFT JOIN deep_assessment_endpoint_evidence x ON x.endpoint_id=e.id
		LEFT JOIN deep_assessment_facts f ON f.id=x.fact_id AND f.task_id=?
		WHERE e.asset_id=? GROUP BY e.id
	)`
	rows, err := s.DB.Query(base+` SELECT evidence_status,COUNT(*) FROM coverage GROUP BY evidence_status`, task, asset)
	if err != nil {
		return nil, nil, false, err
	}
	counts := map[string]int{"observed": 0, "needs_review": 0, "missing": 0}
	for rows.Next() {
		var state string
		var count int
		if err = rows.Scan(&state, &count); err != nil {
			rows.Close()
			return nil, nil, false, err
		}
		counts[state] = count
	}
	if err = rows.Err(); err != nil {
		return nil, nil, false, err
	}
	rows, err = s.DB.Query(base+` SELECT id,asset_id,method,path,title,evidence_status FROM coverage
		WHERE (?='' OR evidence_status=?) ORDER BY path,method,id LIMIT ? OFFSET ?`, task, asset, status, status, evidencePageSize+1, max(0, offset))
	if err != nil {
		return nil, nil, false, err
	}
	defer rows.Close()
	out := []EndpointEvidence{}
	for rows.Next() {
		var endpoint EndpointEvidence
		if err = rows.Scan(&endpoint.ID, &endpoint.AssetID, &endpoint.Method, &endpoint.Path, &endpoint.Title, &endpoint.Status); err != nil {
			return nil, nil, false, err
		}
		out = append(out, endpoint)
	}
	return out[:min(len(out), evidencePageSize)], counts, len(out) > evidencePageSize, rows.Err()
}
