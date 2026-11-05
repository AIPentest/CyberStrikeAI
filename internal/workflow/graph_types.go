package workflow

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type graphDef struct {
	Nodes  []graphNode    `json:"nodes"`
	Edges  []graphEdge    `json:"edges"`
	Config map[string]any `json:"config"`
}

type graphNode struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"`
	Label    string         `json:"label"`
	Position graphPosition  `json:"position"`
	Config   map[string]any `json:"config"`
}

type graphEdge struct {
	ID     string         `json:"id"`
	Source string         `json:"source"`
	Target string         `json:"target"`
	Label  string         `json:"label"`
	Config map[string]any `json:"config"`
}

type graphPosition struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type graphIndex struct {
	nodes    map[string]graphNode
	outgoing map[string][]graphEdge
	incoming map[string][]graphEdge
}

func parseGraph(raw string) (*graphDef, error) {
	var g graphDef
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &g); err != nil {
		return nil, fmt.Errorf("failed to parse workflow graph: %w", err)
	}
	if len(g.Nodes) == 0 {
		return nil, fmt.Errorf("workflow has no nodes")
	}
	if g.Config == nil {
		g.Config = make(map[string]any)
	}
	return &g, nil
}

func indexGraph(g *graphDef) *graphIndex {
	idx := &graphIndex{
		nodes:    make(map[string]graphNode, len(g.Nodes)),
		outgoing: make(map[string][]graphEdge),
		incoming: make(map[string][]graphEdge),
	}
	for _, node := range g.Nodes {
		node.ID = strings.TrimSpace(node.ID)
		if node.ID == "" {
			continue
		}
		if strings.TrimSpace(node.Type) == "" {
			node.Type = "tool"
		}
		if node.Config == nil {
			node.Config = make(map[string]any)
		}
		idx.nodes[node.ID] = node
	}
	for _, edge := range g.Edges {
		if _, ok := idx.nodes[edge.Source]; !ok {
			continue
		}
		if _, ok := idx.nodes[edge.Target]; !ok {
			continue
		}
		idx.outgoing[edge.Source] = append(idx.outgoing[edge.Source], edge)
		idx.incoming[edge.Target] = append(idx.incoming[edge.Target], edge)
	}
	for source := range idx.outgoing {
		sortEdgesByCanvas(idx.outgoing[source], idx.nodes)
	}
	return idx
}

func sortEdgesByCanvas(edges []graphEdge, nodes map[string]graphNode) {
	sort.SliceStable(edges, func(i, j int) bool {
		a := nodes[edges[i].Target]
		b := nodes[edges[j].Target]
		if a.Position.Y != b.Position.Y {
			return a.Position.Y < b.Position.Y
		}
		if a.Position.X != b.Position.X {
			return a.Position.X < b.Position.X
		}
		return edges[i].Target < edges[j].Target
	})
}

func sortNodeIDsByCanvas(ids []string, nodes map[string]graphNode) {
	sort.SliceStable(ids, func(i, j int) bool {
		a := nodes[ids[i]]
		b := nodes[ids[j]]
		if a.Position.Y != b.Position.Y {
			return a.Position.Y < b.Position.Y
		}
		if a.Position.X != b.Position.X {
			return a.Position.X < b.Position.X
		}
		return ids[i] < ids[j]
	})
}

func findStartNodeIDs(idx *graphIndex) []string {
	var queue []string
	for id, node := range idx.nodes {
		if strings.EqualFold(node.Type, "start") {
			queue = append(queue, id)
		}
	}
	if len(queue) == 0 {
		inDegree := make(map[string]int, len(idx.nodes))
		for id := range idx.nodes {
			inDegree[id] = 0
		}
		for _, edges := range idx.outgoing {
			for _, edge := range edges {
				inDegree[edge.Target]++
			}
		}
		for id, deg := range inDegree {
			if deg == 0 {
				queue = append(queue, id)
			}
		}
	}
	sortNodeIDsByCanvas(queue, idx.nodes)
	return queue
}

func branchNodeID(nodeID string) string {
	return nodeID + "__eino_branch"
}

func edgeBranchNodeID(nodeID string) string {
	return nodeID + "__eino_edge_branch"
}
