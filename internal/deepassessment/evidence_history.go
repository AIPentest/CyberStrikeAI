package deepassessment

import (
	"crypto/sha256"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"cyberstrike-ai/internal/database"
)

type AssetEvidenceRecord struct {
	TaskID            string             `json:"task_id"`
	Name              string             `json:"name"`
	State             string             `json:"state"`
	CreatedAt         time.Time          `json:"created_at"`
	LastObservation   string             `json:"last_observation,omitempty"`
	ObservedFacts     int                `json:"observed_facts"`
	ObservedEndpoints int                `json:"observed_endpoints"`
	PendingCriteria   []int              `json:"pending_criteria"`
	Recent            []AcceptanceSource `json:"recent"`
	endpointIDs       []string
	observationKeys   map[[32]byte]struct{}
}

type AssetEvidenceComparison struct {
	AssetID string               `json:"asset_id"`
	Title   string               `json:"title"`
	Current AssetEvidenceRecord  `json:"current"`
	Prior   []AssetEvidencePrior `json:"prior"`
	HasMore bool                 `json:"has_more"`
}

type AssetEvidencePrior struct {
	AssetEvidenceRecord
	Comparable      bool `json:"comparable"`
	NewObservations int  `json:"new_observations"`
	NewEndpoints    int  `json:"new_endpoints"`
	UnseenNow       int  `json:"unseen_now"`
}

func (s *Store) assetEvidenceRecord(t *Task, asset string) (AssetEvidenceRecord, error) {
	record := AssetEvidenceRecord{TaskID: t.ID, Name: t.Name, State: t.Status, CreatedAt: t.CreatedAt, PendingCriteria: []int{}, observationKeys: map[[32]byte]struct{}{}}
	observations, err := s.DB.Query(`SELECT f.summary,f.body,CAST(f.created_at AS TEXT)
		FROM deep_assessment_facts f WHERE f.task_id=? AND EXISTS(SELECT 1 FROM json_each(f.assets_json) j WHERE j.value=?) AND `+trustedObservation+
		` ORDER BY f.created_at DESC,id`, t.ID, asset)
	if err != nil {
		return record, err
	}
	defer observations.Close()
	for observations.Next() {
		var summary, body, createdAt string
		if err = observations.Scan(&summary, &body, &createdAt); err != nil {
			return record, err
		}
		if record.ObservedFacts == 0 {
			record.LastObservation = createdAt
		}
		record.ObservedFacts++
		identity, _ := json.Marshal([2]string{summary, body})
		record.observationKeys[sha256.Sum256(identity)] = struct{}{}
	}
	if err = observations.Err(); err != nil {
		return record, err
	}
	rows, err := s.DB.Query(`SELECT DISTINCT e.id FROM asset_endpoints e JOIN deep_assessment_endpoint_evidence x ON x.endpoint_id=e.id
		JOIN deep_assessment_facts f ON f.id=x.fact_id WHERE f.task_id=? AND e.asset_id=? AND `+trustedObservation+` ORDER BY e.id`, t.ID, asset)
	if err != nil {
		return record, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return record, err
		}
		record.endpointIDs = append(record.endpointIDs, id)
	}
	if err = rows.Err(); err != nil {
		return record, err
	}
	record.ObservedEndpoints = len(record.endpointIDs)
	if record.Recent, err = s.EvidenceAssetFacts(t.ID, asset, "", 0, 3); err != nil {
		return record, err
	}
	matrix, err := s.AcceptanceMatrix(t)
	if err != nil {
		return record, err
	}
	for _, row := range matrix {
		if row.Status == "needs_review" && slices.ContainsFunc(row.Facts, func(f AcceptanceSource) bool {
			return slices.Contains(f.AssetIDs, asset)
		}) {
			record.PendingCriteria = append(record.PendingCriteria, row.Number)
		}
	}
	return record, nil
}

// Prior tasks are filtered in SQL before pagination using the assessment read scope.
func (s *Store) AssetEvidenceHistory(t *Task, asset, title string, access database.RBACListAccess, offset, limit int) (AssetEvidenceComparison, error) {
	out := AssetEvidenceComparison{AssetID: asset, Title: title, Prior: []AssetEvidencePrior{}}
	query := `SELECT json_remove(d.payload,'$.steps','$.guidance','$.metrics')
		FROM deep_assessment_assets a JOIN deep_assessments d ON d.id=a.task_id
		WHERE a.asset_id=? AND d.id<>? AND d.status<>'draft' AND julianday(json_extract(d.payload,'$.created_at'))<=julianday(?)`
	args := []any{asset, t.ID, t.CreatedAt.Format(time.RFC3339Nano)}
	if access.Scope != database.RBACScopeAll {
		assignment := `EXISTS(SELECT 1 FROM rbac_resource_assignments r WHERE r.user_id=? AND r.resource_type='deep_assessment' AND r.resource_id=d.id)`
		if access.Scope == database.RBACScopeOwn {
			query += ` AND (d.owner_user_id=? OR ` + assignment + `)`
			args = append(args, access.UserID, access.UserID)
		} else {
			query += ` AND ` + assignment
			args = append(args, access.UserID)
		}
	}
	rows, err := s.DB.Query(query+` ORDER BY julianday(json_extract(d.payload,'$.created_at')) DESC,d.id LIMIT ? OFFSET ?`, append(args, limit+1, max(0, offset))...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	var prior []Task
	for rows.Next() {
		var payload string
		var task Task
		if err = rows.Scan(&payload); err != nil {
			return out, err
		}
		if err = json.Unmarshal([]byte(payload), &task); err != nil {
			return out, err
		}
		prior = append(prior, task)
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	out.HasMore = len(prior) > limit
	out.Current, err = s.assetEvidenceRecord(t, asset)
	if err != nil {
		return out, err
	}
	scope := strings.Join(strings.Fields(t.Scope), " ")
	for _, previous := range prior[:min(len(prior), limit)] {
		record, err := s.assetEvidenceRecord(&previous, asset)
		if err != nil {
			return out, err
		}
		item := AssetEvidencePrior{AssetEvidenceRecord: record, Comparable: scope != "" && scope == strings.Join(strings.Fields(previous.Scope), " ") && t.ProjectID == previous.ProjectID}
		if item.Comparable {
			item.NewEndpoints, item.UnseenNow = endpointDifferences(out.Current.endpointIDs, record.endpointIDs)
			for key := range out.Current.observationKeys {
				if _, found := record.observationKeys[key]; !found {
					item.NewObservations++
				}
			}
		}
		out.Prior = append(out.Prior, item)
	}
	return out, nil
}

func endpointDifferences(current, prior []string) (added, unseen int) {
	for len(current) > 0 && len(prior) > 0 {
		switch strings.Compare(current[0], prior[0]) {
		case -1:
			added++
			current = current[1:]
		case 1:
			unseen++
			prior = prior[1:]
		default:
			current, prior = current[1:], prior[1:]
		}
	}
	return added + len(current), unseen + len(prior)
}
