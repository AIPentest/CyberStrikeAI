package project

import (
	"fmt"
	"sort"
	"strings"

	"cyberstrike-ai/internal/database"
)

var factIndexEdgeTypeOrder = []string{
	"discovered_on", "leads_to", "enables", "depends_on", "exploits", "contains", "part_of", "supports",
}

func filterIndexEdges(edges []*database.ProjectFactEdge) []*database.ProjectFactEdge {
	if len(edges) == 0 {
		return nil
	}
	out := make([]*database.ProjectFactEdge, 0, len(edges))
	for _, e := range edges {
		if e == nil {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(e.Confidence), "deprecated") {
			continue
		}
		edgeType := strings.ToLower(strings.TrimSpace(e.EdgeType))
		if _, ok := database.ValidProjectFactEdgeTypes[edgeType]; !ok {
			continue
		}
		out = append(out, e)
	}
	return out
}

func edgeConfidenceSuffix(confidence string) string {
	c := strings.ToLower(strings.TrimSpace(confidence))
	if c == "" || c == "confirmed" {
		return ""
	}
	return " (" + c + ")"
}

func formatRelationHintPart(e *database.ProjectFactEdge) string {
	return fmt.Sprintf("%s←%s%s", e.EdgeType, e.SourceFactKey, edgeConfidenceSuffix(e.Confidence))
}

func formatOutgoingHintPart(e *database.ProjectFactEdge) string {
	return fmt.Sprintf("%s→%s%s", e.EdgeType, e.TargetFactKey, edgeConfidenceSuffix(e.Confidence))
}

func formatIncomingHintPart(e *database.ProjectFactEdge) string {
	return formatRelationHintPart(e)
}

func joinEdgeHintParts(edges []*database.ProjectFactEdge, formatter func(*database.ProjectFactEdge) string) string {
	parts := make([]string, 0, len(edges))
	for _, e := range edges {
		parts = append(parts, formatter(e))
	}
	return strings.Join(parts, ", ")
}

// FormatOutgoingLinksHint 黑板索引用出边摘要（全部有效边类型，不截断）。
func FormatOutgoingLinksHint(edges []*database.ProjectFactEdge) string {
	edges = filterIndexEdges(edges)
	if len(edges) == 0 {
		return ""
	}
	return " {outgoing: " + joinEdgeHintParts(edges, formatOutgoingHintPart) + "}"
}

// FormatIncomingLinksHint 黑板索引用入边摘要（全部有效边类型，不截断）。
func FormatIncomingLinksHint(edges []*database.ProjectFactEdge) string {
	edges = filterIndexEdges(edges)
	if len(edges) == 0 {
		return ""
	}
	return " {incoming: " + joinEdgeHintParts(edges, formatIncomingHintPart) + "}"
}

// FormatFactIndexLinksHint 黑板索引行内关系边（from → 当前 fact，与 upsert links 一致）。
func FormatFactIndexLinksHint(_ string, incoming []*database.ProjectFactEdge) string {
	in := filterIndexEdges(incoming)
	if len(in) == 0 {
		return ""
	}
	return " {links: " + joinEdgeHintParts(in, formatRelationHintPart) + "}"
}

func indexEdgeGroupMaps(edges []*database.ProjectFactEdge) (outgoing, incoming map[string][]*database.ProjectFactEdge) {
	outgoing = map[string][]*database.ProjectFactEdge{}
	incoming = map[string][]*database.ProjectFactEdge{}
	for _, e := range filterIndexEdges(edges) {
		outgoing[e.SourceFactKey] = append(outgoing[e.SourceFactKey], e)
		incoming[e.TargetFactKey] = append(incoming[e.TargetFactKey], e)
	}
	return outgoing, incoming
}

func relationOverviewLine(e *database.ProjectFactEdge) string {
	return fmt.Sprintf("- %s → %s%s · %s", e.SourceFactKey, e.TargetFactKey, edgeConfidenceSuffix(e.Confidence), e.EdgeType)
}

func indexEdgeSortKey(e *database.ProjectFactEdge) (int, int, string) {
	confRank := 0
	if strings.EqualFold(strings.TrimSpace(e.Confidence), "tentative") {
		confRank = 1
	}
	typeRank := len(factIndexEdgeTypeOrder) + 1
	for i, t := range factIndexEdgeTypeOrder {
		if strings.EqualFold(e.EdgeType, t) {
			typeRank = i
			break
		}
	}
	return confRank, typeRank, e.SourceFactKey + ">" + e.TargetFactKey + ">" + e.EdgeType
}

func sortIndexOverviewEdges(edges []*database.ProjectFactEdge) {
	sort.SliceStable(edges, func(i, j int) bool {
		ci, ti, ki := indexEdgeSortKey(edges[i])
		cj, tj, kj := indexEdgeSortKey(edges[j])
		if ci != cj {
			return ci < cj
		}
		if ti != tj {
			return ti < tj
		}
		return ki < kj
	})
}

// BuildFactPathOverviewSection 生成事实关系速览（全部有效边类型，不含 body）。
func BuildFactPathOverviewSection(edges []*database.ProjectFactEdge, indexedKeys map[string]struct{}, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	candidates := filterIndexEdges(edges)
	if len(candidates) == 0 {
		return ""
	}
	filtered := make([]*database.ProjectFactEdge, 0, len(candidates))
	for _, e := range candidates {
		// No indexed facts means nothing may be related; filtering fails closed.
		if _, ok := indexedKeys[e.SourceFactKey]; !ok {
			continue
		}
		if _, ok := indexedKeys[e.TargetFactKey]; !ok {
			continue
		}
		filtered = append(filtered, e)
	}
	if len(filtered) == 0 {
		return ""
	}
	sortIndexOverviewEdges(filtered)

	header := "### Attack path (fact relations)\n"
	header += "source → target · type (direction matches the attack-path graph and database; when writing, declare the source via from in the target fact's links)\n"
	var b strings.Builder
	b.WriteString(header)
	used := len([]rune(header))
	omitted := 0

	for _, e := range filtered {
		line := relationOverviewLine(e) + "\n"
		lineRunes := len([]rune(line))
		if used+lineRunes > maxRunes {
			omitted++
			continue
		}
		b.WriteString(line)
		used += lineRunes
	}
	if omitted > 0 {
		extra := fmt.Sprintf("(There are %d more relation edges not listed; use get_project_fact to view the complete relations.)\n", omitted)
		if used+len([]rune(extra)) <= maxRunes {
			b.WriteString(extra)
		}
	}
	if used <= len([]rune(header)) {
		return ""
	}
	return b.String()
}

func factIndexSortPriority(f *database.ProjectFact) int {
	if f == nil {
		return 0
	}
	score := 0
	if f.Pinned {
		score += 1000
	}
	c := strings.ToLower(strings.TrimSpace(f.Category))
	switch c {
	case FactCategoryTarget:
		score += 400
	case FactCategoryFinding, FactCategoryChain:
		score += 300
	case FactCategoryExploit, FactCategoryPOC:
		score += 250
	case "auth", "infra", "business":
		score += 200
	case "note":
		score += 50
	default:
		key := strings.ToLower(strings.TrimSpace(f.FactKey))
		if strings.HasPrefix(key, "target/") {
			score += 400
		} else if strings.HasPrefix(key, "finding/") || strings.HasPrefix(key, "chain/") {
			score += 300
		}
	}
	if strings.EqualFold(strings.TrimSpace(f.Confidence), "confirmed") {
		score += 80
	}
	return score
}

func sortFactsForIndex(facts []*database.ProjectFact) {
	sort.SliceStable(facts, func(i, j int) bool {
		pi, pj := factIndexSortPriority(facts[i]), factIndexSortPriority(facts[j])
		if pi != pj {
			return pi > pj
		}
		return facts[i].UpdatedAt.After(facts[j].UpdatedAt)
	})
}
