package deepassessment

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
)

// Group only steps connected by real evidence/dependency relations. A shared
// global goal does not make otherwise unrelated directions one historical branch.
func coldBranches(cold, nodes []Node, edges []Edge) [][]Node {
	byID := map[string]Node{}
	owner := map[string]string{}
	adj := map[string][]string{}
	for _, n := range cold {
		byID[n.ID] = n
		adj[n.ID] = nil
	}
	for _, n := range nodes {
		if n.StepID != "" && n.Kind != "finding" && n.Kind != "guidance" {
			owner[n.ID] = stepNode(n.StepID)
		}
	}
	for _, e := range edges {
		a, b := owner[e.Source], owner[e.Target]
		if a != b && a != "" && b != "" && e.Relation != "contains" {
			if _, ok := byID[a]; ok {
				if _, ok = byID[b]; ok {
					adj[a] = append(adj[a], b)
					adj[b] = append(adj[b], a)
				}
			}
		}
	}
	keys := []string{}
	for id := range byID {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	seen := map[string]bool{}
	out := [][]Node{}
	for _, id := range keys {
		if seen[id] {
			continue
		}
		queue := []string{id}
		seen[id] = true
		group := []Node{}
		for len(queue) > 0 {
			next := queue[0]
			queue = queue[1:]
			group = append(group, byID[next])
			sort.Strings(adj[next])
			for _, child := range adj[next] {
				if !seen[child] {
					seen[child] = true
					queue = append(queue, child)
				}
			}
		}
		// Components are reference indexes, not generated summaries or model budgets.
		for len(group) > 0 {
			n := min(20, len(group))
			chunk := append([]Node(nil), group[:n]...)
			sort.Slice(chunk, func(i, j int) bool { return chunk[i].ID < chunk[j].ID })
			out = append(out, chunk)
			group = group[n:]
		}
	}
	return out
}

func visibleGraph(nodes []Node, edges []Edge, allow func(Node) bool) ([]Node, []Edge) {
	ids := map[string]bool{}
	kept := []Node{}
	for _, n := range nodes {
		if allow == nil || allow(n) {
			ids[n.ID] = true
			kept = append(kept, n)
		}
	}
	links := []Edge{}
	for _, e := range edges {
		if ids[e.Source] && ids[e.Target] {
			links = append(links, e)
		}
	}
	return kept, links
}

func projectGraph(nodes []Node, edges []Edge, scope, view string, coldEligible map[string]bool) ([]Node, []Edge, map[string]Node, int, int, error) {
	byID := map[string]Node{}
	for _, n := range nodes {
		byID[n.ID] = n
	}
	if scope != "" {
		if _, ok := byID[scope]; !ok {
			return nil, nil, nil, 0, 0, sql.ErrNoRows
		}
	}
	hot := hotGraph(nodes, edges)
	expanded := map[string]bool{}
	if scope != "" {
		n := byID[scope]
		if n.StepID != "" {
			expanded[n.StepID] = true
		}
		if n.Kind == "digest" {
			for _, e := range edges {
				if e.Source == scope && e.Relation == "contains" {
					expanded[byID[e.Target].StepID] = true
				}
			}
		}
	}
	archived := map[string]string{}
	digestCounts := map[string]int{}
	for _, e := range edges {
		if e.Relation == "contains" && byID[e.Source].Kind == "digest" && byID[e.Source].State == "folded" && coldEligible[e.Target] && !hot[e.Target] && !expanded[byID[e.Target].StepID] {
			archived[byID[e.Target].StepID] = e.Source
			digestCounts[e.Source]++
		}
	}
	representatives := map[string]string{}
	folded, hidden := 0, 0
	detailCounts := map[string]int{}
	for _, n := range nodes {
		representatives[n.ID] = n.ID
		if n.Kind == "digest" && (n.State != "folded" || digestCounts[n.ID] == 0) && n.ID != scope {
			representatives[n.ID] = ""
			continue
		}
		if n.Kind != "finding" && n.Kind != "goal" && n.Kind != "guidance" && n.StepID != "" && archived[n.StepID] != "" {
			representatives[n.ID] = archived[n.StepID]
			folded++
			continue
		}
		if view == "summary" && (n.Kind == "receipt" || n.Kind == "observation") {
			parent := stepNode(n.StepID)
			if _, ok := byID[parent]; !ok {
				parent = "goal"
			}
			representatives[n.ID] = parent
			hidden++
			detailCounts[parent]++
		}
	}
	representative := func(id string) string {
		for i := 0; i < 3; i++ {
			next := representatives[id]
			if next == id || next == "" {
				return next
			}
			id = next
		}
		return id
	}
	allVisible := map[string]Node{}
	primary := []Node{}
	for _, n := range nodes {
		if representative(n.ID) != n.ID {
			continue
		}
		if n.Kind == "digest" && n.ID != scope {
			n.Title = fmt.Sprintf("历史分支 · %d", digestCounts[n.ID])
		}
		n.DetailCount = detailCounts[n.ID]
		allVisible[n.ID] = n
		if n.ID != "goal" && (scope == "" || n.ID == scope || expanded[n.StepID]) {
			primary = append(primary, n)
		}
	}
	projected := []Edge{}
	seen := map[string]int{}
	for _, e := range edges {
		if e.Relation == "contains" && e.Source != scope {
			continue
		}
		a, b := representative(e.Source), representative(e.Target)
		if a == "" || b == "" || a == b {
			continue
		}
		if a != e.Source || b != e.Target {
			e.Projection = "details"
			if byID[a].Kind == "digest" || byID[b].Kind == "digest" {
				e.Projection = "archive"
			}
			e.References = 1
		}
		e.Source, e.Target = a, b
		key := a + "\x00" + e.Relation + "\x00" + b + "\x00" + e.Projection
		if at, ok := seen[key]; ok {
			projected[at].References += e.References
			continue
		}
		seen[key] = len(projected)
		projected = append(projected, e)
	}
	return primary, projected, allVisible, folded, hidden, nil
}

func (s *Store) GraphViewVisible(id, scope, view string, limit, offset int, allow func(Node) bool) (Graph, error) {
	if scope == "goal" {
		scope = ""
	}
	if limit < 1 || limit > 200 {
		limit = 80
	}
	if offset < 0 {
		return Graph{}, fmt.Errorf("invalid offset")
	}
	if view != "" && view != "full" && view != "summary" {
		return Graph{}, fmt.Errorf("invalid graph view")
	}
	if err := s.PrepareGraph(id); err != nil {
		return Graph{}, err
	}
	nodes, edges, err := readGraph(s.DB, id)
	if err != nil {
		return Graph{}, err
	}
	nodes, edges = visibleGraph(nodes, edges, allow)
	eligible := map[string]bool{}
	rows, err := s.DB.Query("SELECT node_id FROM deep_assessment_cold WHERE task_id=? AND since_round <= (SELECT COALESCE(json_extract(payload,'$.rounds'),0)-6 FROM deep_assessments WHERE id=?)", id, id)
	if err != nil {
		return Graph{}, err
	}
	for rows.Next() {
		var key string
		if err = rows.Scan(&key); err != nil {
			rows.Close()
			return Graph{}, err
		}
		eligible[key] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return Graph{}, err
	}
	primary, links, byID, folded, hidden, err := projectGraph(nodes, edges, scope, view, eligible)
	if err != nil {
		return Graph{}, err
	}
	priority := func(n Node) int {
		if n.ID == scope {
			return -1
		}
		switch n.Kind {
		case "finding":
			return 0
		case "guidance":
			return 1
		case "step":
			if n.State != "completed" && n.State != "retired" {
				return 2
			}
			return 6
		case "fact":
			return 3
		case "memory":
			return 4
		case "digest":
			return 5
		case "observation":
			return 7
		}
		return 8
	}
	sort.SliceStable(primary, func(i, j int) bool { return priority(primary[i]) < priority(primary[j]) })
	out := Graph{Nodes: []Node{}, Edges: []Edge{}, Total: len(primary), Folded: folded, DetailsHidden: hidden, View: view}
	included := map[string]bool{}
	page := map[string]bool{}
	appendNode := func(n Node, boundary bool) {
		n.Body = Clip(n.Body, 400)
		n.Source = json.RawMessage(`{}`)
		n.Boundary = boundary
		out.Nodes = append(out.Nodes, n)
		included[n.ID] = true
	}
	if root, ok := byID["goal"]; ok {
		appendNode(root, false)
	}
	offset = min(offset, len(primary))
	end := min(len(primary), offset+limit)
	for _, n := range primary[offset:end] {
		appendNode(n, false)
		page[n.ID] = true
	}
	out.PrimaryCount = end - offset
	out.NextOffset = end
	out.HasMore = end < len(primary)
	references := map[string]bool{}
	ids := []string{}
	addRef := func(id string) {
		if !included[id] && !references[id] {
			references[id] = true
			ids = append(ids, id)
		}
	}
	parents := map[string][]string{}
	for _, e := range links {
		parents[e.Target] = append(parents[e.Target], e.Source)
	}
	// Prefer the nearest upstream lineage before downstream context. This keeps
	// a paged dependency chain connected back to its goal whenever it fits.
	for _, n := range primary[offset:end] {
		for _, parent := range parents[n.ID] {
			addRef(parent)
		}
	}
	for i := 0; i < len(ids); i++ {
		for _, parent := range parents[ids[i]] {
			addRef(parent)
		}
	}
	for _, e := range links {
		if page[e.Source] {
			addRef(e.Target)
		}
	}
	out.BoundaryTotal = len(ids)
	for i, id := range ids {
		if i >= 80 {
			out.BoundaryMore++
			continue
		}
		appendNode(byID[id], true)
	}
	for _, e := range links {
		if included[e.Source] && included[e.Target] {
			out.Edges = append(out.Edges, e)
		}
	}
	return out, nil
}

type RelationReference struct {
	Direction string `json:"direction"`
	Relation  string `json:"relation"`
	Node      Node   `json:"node"`
}

func (s *Store) RelationsVisible(task, node string, offset int, allow func(Node) bool) (map[string]any, error) {
	if offset < 0 {
		return nil, fmt.Errorf("invalid offset")
	}
	nodes, edges, err := readGraph(s.DB, task)
	if err != nil {
		return nil, err
	}
	nodes, edges = visibleGraph(nodes, edges, allow)
	byID := map[string]Node{}
	for _, n := range nodes {
		n.Body = Clip(n.Body, 160)
		n.Source = json.RawMessage(`{}`)
		byID[n.ID] = n
	}
	if _, ok := byID[node]; !ok {
		return nil, sql.ErrNoRows
	}
	refs := []RelationReference{}
	for _, e := range edges {
		if e.Target == node {
			refs = append(refs, RelationReference{"upstream", e.Relation, byID[e.Source]})
		}
		if e.Source == node {
			refs = append(refs, RelationReference{"downstream", e.Relation, byID[e.Target]})
		}
	}
	offset = min(offset, len(refs))
	end := min(offset+30, len(refs))
	return map[string]any{"relations": refs[offset:end], "relations_total": len(refs), "relations_next_offset": end, "relations_has_more": end < len(refs)}, nil
}
