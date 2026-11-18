package deepassessment

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"

	"cyberstrike-ai/internal/database"
)

const evidenceGraphPageSize = 12

type EvidenceFocus struct {
	AssetID    string
	EndpointID string
	FactID     string
	Criterion  int
	Offset     int
}

type EvidenceView struct {
	Graph
	Root               string `json:"root"`
	Title              string `json:"title"`
	Unattributed       int    `json:"unattributed_receipts"`
	RelatedIntentCount int    `json:"related_intent_count"`
}

func (s *Store) EvidenceAssetFacts(task, asset, endpoint string, offset, limit int) ([]AcceptanceSource, error) {
	suffix := ` AND EXISTS(SELECT 1 FROM json_each(assets_json) j WHERE j.value=?)`
	args := []any{asset}
	if endpoint != "" {
		suffix += ` AND EXISTS(SELECT 1 FROM deep_assessment_endpoint_evidence x WHERE x.fact_id=deep_assessment_facts.id AND x.endpoint_id=?)`
		args = append(args, endpoint)
	}
	return queryAcceptanceSources(s.DB, task, "fact", suffix+` ORDER BY created_at DESC,id LIMIT ? OFFSET ?`, append(args, limit, max(0, offset))...)
}

func evidenceNodeSource(data any) json.RawMessage {
	raw, _ := json.Marshal(data)
	return raw
}

// EvidencePath is a read-only projection of stored anchors and review mappings.
// Keeping it separate from the execution graph avoids changing scheduling state.
func (s *Store) EvidencePath(t *Task, access database.RBACListAccess, focus EvidenceFocus) (EvidenceView, error) {
	out := EvidenceView{Graph: Graph{Nodes: []Node{}, Edges: []Edge{}, View: "evidence_path"}}
	matrix, err := s.AcceptanceMatrix(t)
	if err != nil {
		return out, err
	}
	var facts []AcceptanceSource
	var rootCriterion *AcceptanceRow
	var endpoint database.AssetEndpoint
	var root Node
	switch {
	case focus.Criterion > 0:
		if focus.Criterion > len(matrix) {
			return out, sql.ErrNoRows
		}
		rootCriterion = &matrix[focus.Criterion-1]
		root = criterionEvidenceNode(*rootCriterion)
		start := min(max(0, focus.Offset), len(rootCriterion.Facts))
		end := min(start+evidenceGraphPageSize, len(rootCriterion.Facts))
		facts = rootCriterion.Facts[start:end]
		out.HasMore = end < len(rootCriterion.Facts)
	case focus.FactID != "":
		facts, err = queryAcceptanceSources(s.DB, t.ID, "fact", ` AND id=?`, focus.FactID)
		if err != nil {
			return out, err
		}
		if len(facts) == 0 {
			return out, sql.ErrNoRows
		}
		root = factEvidenceNode(facts[0])
	case focus.AssetID != "":
		assets, err := s.DB.AssessmentEvidenceAssets(t.ID, t.ProjectID, []string{focus.AssetID}, access)
		if err != nil {
			return out, err
		}
		title, ok := assets[focus.AssetID]
		if !ok {
			return out, sql.ErrNoRows
		}
		root = assetEvidenceNode(focus.AssetID, title)
		if focus.EndpointID != "" {
			endpoint, err = s.EvidenceEndpoint(focus.AssetID, focus.EndpointID)
			if err != nil {
				return out, err
			}
			root = endpointEvidenceNode(endpoint)
		}
		facts, err = s.EvidenceAssetFacts(t.ID, focus.AssetID, focus.EndpointID, focus.Offset, evidenceGraphPageSize+1)
		if err != nil {
			return out, err
		}
		out.HasMore = len(facts) > evidenceGraphPageSize
		facts = facts[:min(len(facts), evidenceGraphPageSize)]
	default:
		return out, fmt.Errorf("select an asset, fact or criterion")
	}
	out.Root, out.Title = root.ID, root.Title
	out.NextOffset = focus.Offset + len(facts)
	nodeIDs := map[string]bool{}
	edges := map[Edge]bool{}
	add := func(node Node) {
		if nodeIDs[node.ID] {
			return
		}
		nodeIDs[node.ID] = true
		out.Nodes = append(out.Nodes, node)
	}
	link := func(from, to, relation string) {
		item := Edge{Source: from, Target: to, Relation: relation}
		if !edges[item] && from != to {
			edges[item] = true
			out.Edges = append(out.Edges, item)
		}
	}
	add(root)
	if endpoint.ID != "" {
		a, err := s.DB.AssessmentEvidenceAssets(t.ID, t.ProjectID, []string{endpoint.AssetID}, access)
		if err != nil {
			return out, err
		}
		if title, ok := a[endpoint.AssetID]; ok {
			asset := assetEvidenceNode(endpoint.AssetID, title)
			add(asset)
			link(asset.ID, root.ID, "contains")
		}
	}

	var assetIDs, receiptIDs []string
	for _, fact := range facts {
		assetIDs = append(assetIDs, fact.AssetIDs...)
		receiptIDs = append(receiptIDs, fact.ReceiptIDs...)
	}
	if rootCriterion != nil {
		receiptIDs = append(receiptIDs, rootCriterion.ReceiptIDs...)
		for _, fact := range rootCriterion.Facts {
			assetIDs = append(assetIDs, fact.AssetIDs...)
		}
	}
	assets, err := s.DB.AssessmentEvidenceAssets(t.ID, t.ProjectID, assetIDs, access)
	if err != nil {
		return out, err
	}
	receipts, err := acceptanceSourcesByID(s.DB, t.ID, "receipt", receiptIDs)
	if err != nil {
		return out, err
	}
	addStep := func(id string) string {
		if st := t.Step(id); st != nil {
			node := Node{ID: stepNode(id), StepID: id, Kind: "step", Title: st.Title, State: st.Status}
			add(node)
			return node.ID
		}
		return ""
	}
	if focus.AssetID != "" && focus.EndpointID == "" {
		for _, st := range t.Steps {
			if slices.Contains(st.AssetIDs, focus.AssetID) {
				out.RelatedIntentCount++
				if focus.Offset == 0 && out.RelatedIntentCount <= evidenceGraphPageSize {
					link(root.ID, addStep(st.ID), "targeted_by")
				}
			}
		}
	}
	addReceipt := func(r AcceptanceSource) string {
		id := "receipt:" + r.ID
		add(Node{ID: id, StepID: r.StepID, Kind: "receipt", Title: r.Title, State: r.State, Source: evidenceNodeSource(map[string]string{"receipt_id": r.ID})})
		return id
	}
	for _, fact := range facts {
		add(factEvidenceNode(fact))
		if step := addStep(fact.StepID); step != "" {
			link(step, fact.ID, "produces")
		}
		// A fact can name several assets. Asset-focused views remain on the selected asset.
		for _, id := range fact.AssetIDs {
			if focus.AssetID != "" && id != focus.AssetID {
				continue
			}
			if title, ok := assets[id]; ok {
				a := assetEvidenceNode(id, title)
				add(a)
				relation := "observed_on"
				if fact.State != "observed" {
					relation = "inferred_on"
				}
				link(a.ID, fact.ID, relation)
			}
		}
		if endpoint.ID != "" {
			link(root.ID, fact.ID, "endpoint_annotation")
		}
		for _, ref := range fact.ReceiptIDs {
			if receipt, ok := receipts[ref]; ok && receipt.StepID == fact.StepID {
				link(fact.ID, addReceipt(receipt), "backed_by")
			}
		}
	}
	shownFacts := map[string]bool{}
	for _, fact := range facts {
		shownFacts[fact.ID] = true
	}
	for _, criterion := range matrix {
		if rootCriterion != nil && criterion.Number != rootCriterion.Number {
			continue
		}
		n := criterionEvidenceNode(criterion)
		for _, fact := range criterion.Facts {
			if shownFacts[fact.ID] {
				add(n)
				link(n.ID, fact.ID, "review_link")
			}
		}
		for _, ref := range criterion.ReceiptIDs {
			if r, ok := receipts[ref]; ok {
				id := "receipt:" + r.ID
				if rootCriterion != nil {
					var anchored bool
					for _, f := range criterion.Facts {
						if slices.Contains(f.ReceiptIDs, ref) {
							for _, a := range f.AssetIDs {
								if _, visible := assets[a]; visible {
									anchored = true
								}
							}
						}
					}
					if !anchored {
						out.Unattributed++
					}
					addReceipt(r)
				}
				if nodeIDs[id] {
					add(n)
					link(n.ID, id, "review_link")
				}
			}
		}
	}
	out.Total, out.PrimaryCount = len(out.Nodes), len(out.Nodes)
	return out, nil
}

func criterionEvidenceNode(row AcceptanceRow) Node {
	return Node{ID: fmt.Sprintf("criterion:%d", row.Number), Kind: "criterion", Title: Clip(row.Text, 180), Body: row.Text,
		State: row.Status, Source: evidenceNodeSource(map[string]int{"criterion": row.Number})}
}
func factEvidenceNode(fact AcceptanceSource) Node {
	return Node{ID: fact.ID, StepID: fact.StepID, Kind: "fact", Title: fact.Title, State: fact.State}
}
func assetEvidenceNode(id, title string) Node {
	return Node{ID: "asset:" + id, Kind: "service", Title: title, Source: evidenceNodeSource(map[string]string{"asset_id": id})}
}
func endpointEvidenceNode(e database.AssetEndpoint) Node {
	return Node{ID: e.ID, Kind: "endpoint", Title: e.Method + " " + e.Path, Body: e.Title, Source: evidenceNodeSource(map[string]string{"asset_id": e.AssetID, "endpoint_id": e.ID})}
}
