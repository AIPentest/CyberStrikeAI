package project

import (
	"fmt"
	"strings"

	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/projectprompt"
)

// PathGraphCategories The fact categories included in the attack-path view.
var PathGraphCategories = map[string]struct{}{
	FactCategoryTarget:  {},
	FactCategoryFinding: {},
	FactCategoryChain:   {},
	FactCategoryExploit: {},
	FactCategoryPOC:     {},
	"vuln":              {},
}

// GraphNodeType Maps a fact category to a graph node type (for frontend styling and ELK layering).
// Prefer category; only fall back to the fact_key prefix for synthetic nodes (vuln:) or when there is no category.
func GraphNodeType(category, factKey string) string {
	key := strings.ToLower(strings.TrimSpace(factKey))
	if strings.HasPrefix(key, "vuln:") {
		return "vulnerability"
	}
	c := strings.ToLower(strings.TrimSpace(category))
	if c != "" {
		switch c {
		case FactCategoryTarget:
			return "target"
		case FactCategoryExploit:
			return "exploit"
		case FactCategoryPOC:
			return "poc"
		case FactCategoryChain:
			return "chain"
		case FactCategoryFinding:
			return "finding"
		case "vuln":
			return "vulnerability"
		case FactCategoryAuth:
			return "auth"
		case FactCategoryInfra, FactCategoryBusiness:
			return "infra"
		case FactCategoryNote:
			return "note"
		case "missing":
			return "missing"
		default:
			return c
		}
	}
	switch {
	case strings.HasPrefix(key, "target/"):
		return "target"
	case strings.HasPrefix(key, "exploit/"), strings.HasPrefix(key, "evidence/"):
		return "exploit"
	case strings.HasPrefix(key, "poc/"):
		return "poc"
	case strings.HasPrefix(key, "chain/"):
		return "chain"
	case strings.HasPrefix(key, "finding/"):
		return "finding"
	case strings.HasPrefix(key, "auth/"):
		return "auth"
	case strings.HasPrefix(key, "infra/"), strings.HasPrefix(key, "business/"):
		return "infra"
	default:
		return "note"
	}
}

func truncateGraphLabel(summary string, maxRunes int) string {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return "-"
	}
	r := []rune(summary)
	if len(r) <= maxRunes {
		return summary
	}
	return string(r[:maxRunes]) + "..."
}

// BuildProjectFactGraph Builds the project fact graph (nodes + edges).
func BuildProjectFactGraph(db *database.DB, projectID string, view string, excludeDeprecated bool) (*database.ProjectFactGraph, error) {
	if db == nil {
		return nil, fmt.Errorf("database is not initialized")
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, fmt.Errorf("project_id cannot be empty")
	}

	view = strings.TrimSpace(strings.ToLower(view))
	if view == "" {
		view = "path"
	}

	filter := database.ProjectFactListFilter{}
	if excludeDeprecated {
		filter.ExcludeDeprecated = true
	}
	facts, err := db.ListProjectFacts(projectID, filter, 1000, 0)
	if err != nil {
		return nil, err
	}

	edges, err := db.ListProjectFactEdgesByProject(projectID)
	if err != nil {
		return nil, err
	}
	if excludeDeprecated {
		edges = filterDeprecatedEdges(edges)
	}

	factByKey := make(map[string]*database.ProjectFact, len(facts))
	for _, f := range facts {
		factByKey[f.FactKey] = f
	}

	pathMode := view == "path"
	nodeKeys := make(map[string]struct{})

	if pathMode {
		for _, f := range facts {
			if isPathGraphFact(f.Category, f.FactKey) {
				nodeKeys[f.FactKey] = struct{}{}
			}
		}
		// In the path view, keeps auth/infra nodes that serve as dependency targets
		for _, e := range edges {
			if _, ok := nodeKeys[e.SourceFactKey]; !ok {
				continue
			}
			if f, ok := factByKey[e.TargetFactKey]; ok && isDependencyGraphFact(f.Category, f.FactKey) {
				nodeKeys[e.TargetFactKey] = struct{}{}
			}
		}
	} else {
		for _, f := range facts {
			nodeKeys[f.FactKey] = struct{}{}
		}
	}

	// Endpoints referenced on edges are included in the node set
	for _, e := range edges {
		if pathMode {
			if _, ok := nodeKeys[e.SourceFactKey]; !ok {
				continue
			}
			if _, ok := nodeKeys[e.TargetFactKey]; ok {
				// already included
			} else if f, ok := factByKey[e.TargetFactKey]; !ok {
				nodeKeys[e.TargetFactKey] = struct{}{} // Placeholder node
			} else if isPathGraphFact(f.Category, f.FactKey) || isDependencyGraphFact(f.Category, f.FactKey) {
				nodeKeys[e.TargetFactKey] = struct{}{}
			} else {
				continue
			}
		} else {
			nodeKeys[e.SourceFactKey] = struct{}{}
			nodeKeys[e.TargetFactKey] = struct{}{}
		}
	}

	nodes := make([]database.ProjectFactGraphNode, 0, len(nodeKeys))
	for key := range nodeKeys {
		if f, ok := factByKey[key]; ok {
			nodes = append(nodes, database.ProjectFactGraphNode{
				ID:         f.FactKey,
				FactKey:    f.FactKey,
				Category:   f.Category,
				Label:      truncateGraphLabel(f.Summary, 48),
				Summary:    strings.TrimSpace(f.Summary),
				Confidence: f.Confidence,
				Type:       GraphNodeType(f.Category, f.FactKey),
				Pinned:     f.Pinned,
			})
			continue
		}
		nodes = append(nodes, database.ProjectFactGraphNode{
			ID:         key,
			FactKey:    key,
			Category:   "missing",
			Label:      key,
			Confidence: "tentative",
			Type:       "missing",
			Pinned:     false,
		})
	}

	graphEdges := make([]database.ProjectFactGraphEdge, 0, len(edges))
	for _, e := range edges {
		if pathMode {
			if _, ok := nodeKeys[e.SourceFactKey]; !ok {
				continue
			}
			if _, ok := nodeKeys[e.TargetFactKey]; !ok {
				continue
			}
		} else {
			if _, ok := nodeKeys[e.SourceFactKey]; !ok {
				continue
			}
			if _, ok := nodeKeys[e.TargetFactKey]; !ok {
				continue
			}
		}
		graphEdges = append(graphEdges, database.ProjectFactGraphEdge{
			ID:         e.ID,
			Source:     e.SourceFactKey,
			Target:     e.TargetFactKey,
			Type:       e.EdgeType,
			Confidence: e.Confidence,
		})
	}

	// related_vulnerability_id Synthetic edge (source=fact → target=vuln:<id>)
	for _, f := range facts {
		if _, ok := nodeKeys[f.FactKey]; !ok {
			continue
		}
		vid := strings.TrimSpace(f.RelatedVulnerabilityID)
		if vid == "" {
			continue
		}
		vulnNodeID := "vuln:" + vid
		if _, exists := nodeKeys[vulnNodeID]; !exists {
			nodeKeys[vulnNodeID] = struct{}{}
			label := "Vulnerability"
			if len(vid) >= 8 {
				label += " " + vid[:8] + "..."
			} else {
				label += " " + vid
			}
			nodes = append(nodes, database.ProjectFactGraphNode{
				ID:         vulnNodeID,
				FactKey:    vulnNodeID,
				Category:   "vuln",
				Label:      label,
				Confidence: f.Confidence,
				Type:       "vulnerability",
				Pinned:     false,
			})
		}
		graphEdges = append(graphEdges, database.ProjectFactGraphEdge{
			ID:         "vuln-link:" + f.FactKey + ":" + vid,
			Source:     f.FactKey,
			Target:     vulnNodeID,
			Type:       "links_vuln",
			Confidence: f.Confidence,
		})
	}

	return &database.ProjectFactGraph{Nodes: nodes, Edges: graphEdges}, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func isPathGraphFact(category, factKey string) bool {
	c := strings.ToLower(strings.TrimSpace(category))
	if _, ok := PathGraphCategories[c]; ok {
		return true
	}
	if c != "" {
		return false
	}
	key := strings.ToLower(strings.TrimSpace(factKey))
	for _, p := range []string{"target/", "finding/", "chain/", "exploit/", "poc/", "evidence/"} {
		if strings.HasPrefix(key, p) {
			return true
		}
	}
	return false
}

func isDependencyGraphFact(category, factKey string) bool {
	c := strings.ToLower(strings.TrimSpace(category))
	if c == FactCategoryAuth || c == FactCategoryInfra || c == FactCategoryBusiness {
		return true
	}
	if c != "" {
		return false
	}
	key := strings.ToLower(strings.TrimSpace(factKey))
	return strings.HasPrefix(key, "auth/") || strings.HasPrefix(key, "infra/") || strings.HasPrefix(key, "business/")
}

func filterDeprecatedEdges(edges []*database.ProjectFactEdge) []*database.ProjectFactEdge {
	out := make([]*database.ProjectFactEdge, 0, len(edges))
	for _, e := range edges {
		if strings.EqualFold(strings.TrimSpace(e.Confidence), "deprecated") {
			continue
		}
		out = append(out, e)
	}
	return out
}

// ParsedFactLinks Parses the links parameter (from → Current fact).
type ParsedFactLinks struct {
	Incoming []database.ProjectFactEdgeFromInput
}

// ParseFactLinkInputs Parsed from the MCP links parameter; an empty array means clear all incoming edges.
func ParseFactLinkInputs(raw interface{}) (*ParsedFactLinks, error) {
	if raw == nil {
		return nil, nil
	}
	items, ok := raw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("links must be an array")
	}
	if len(items) == 0 {
		return &ParsedFactLinks{
			Incoming: []database.ProjectFactEdgeFromInput{},
		}, nil
	}
	parsed := &ParsedFactLinks{}
	for i, item := range items {
		m, ok := item.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("links[%d] Invalid format", i)
		}
		from, _ := m["from"].(string)
		edgeType, _ := m["type"].(string)
		from = strings.TrimSpace(from)
		edgeType = strings.TrimSpace(edgeType)
		if from == "" {
			return nil, fmt.Errorf("links[%d] must include from", i)
		}
		if edgeType == "" {
			return nil, fmt.Errorf("links[%d] must include type", i)
		}
		conf, _ := m["confidence"].(string)
		parsed.Incoming = append(parsed.Incoming, database.ProjectFactEdgeFromInput{
			From: from, Type: edgeType, Confidence: strings.TrimSpace(conf),
		})
	}
	return parsed, nil
}

// ParseFactLinksText Parses UI text: `type: source_fact_key` One per line (from semantics).
func ParseFactLinksText(text string) ([]database.ProjectFactEdgeFromInput, error) {
	return ParseFactIncomingLinksText(text)
}

// FormatFactLinksText Formats incoming edges into UI text.
func FormatFactLinksText(edges []*database.ProjectFactEdge) string {
	return FormatFactIncomingLinksText(edges)
}

// ParseFactIncomingLinksText Parses the UI incoming-edge text: `type: source_fact_key` One per line.
func ParseFactIncomingLinksText(text string) ([]database.ProjectFactEdgeFromInput, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, nil
	}
	var out []database.ProjectFactEdgeFromInput
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		edgeType, source, ok := strings.Cut(line, ":")
		if !ok {
			return nil, fmt.Errorf("Line %d has an invalid format; it should be type: fact_key", i+1)
		}
		edgeType = strings.TrimSpace(edgeType)
		source = strings.TrimSpace(source)
		if edgeType == "" || source == "" {
			return nil, fmt.Errorf("Line %d has an empty type or fact_key", i+1)
		}
		out = append(out, database.ProjectFactEdgeFromInput{From: source, Type: edgeType})
	}
	return out, nil
}

// FormatFactIncomingLinksText Formats incoming edges into UI text.
func FormatFactIncomingLinksText(edges []*database.ProjectFactEdge) string {
	if len(edges) == 0 {
		return ""
	}
	var b strings.Builder
	for i, e := range edges {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(e.EdgeType)
		b.WriteString(": ")
		b.WriteString(e.SourceFactKey)
	}
	return b.String()
}

// FactEdgeRecordingGuidance The agent spec for writing edges.
func FactEdgeRecordingGuidance() string {
	return projectprompt.FactEdgeRecordingGuidance()
}
