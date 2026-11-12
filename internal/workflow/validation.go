package workflow

import (
	"fmt"
	"strconv"
	"strings"
)

var allowedWorkflowNodeTypes = map[string]bool{
	"start":     true,
	"tool":      true,
	"agent":     true,
	"condition": true,
	"hitl":      true,
	"output":    true,
	"end":       true,
}

func validateGraphDefinition(g *graphDef, idx *graphIndex) error {
	if g == nil || idx == nil {
		return fmt.Errorf("The workflow graph is empty")
	}
	if err := validateNodeIDsAndTypes(g); err != nil {
		return err
	}
	if err := validateEdges(g, idx); err != nil {
		return err
	}
	if err := validateNodeTopology(idx); err != nil {
		return err
	}
	if err := validateNodeConfigs(idx); err != nil {
		return err
	}
	if err := validateDAG(idx); err != nil {
		return err
	}
	if err := validateReachability(idx); err != nil {
		return err
	}
	return nil
}

func validateNodeIDsAndTypes(g *graphDef) error {
	seen := make(map[string]bool, len(g.Nodes))
	for _, node := range g.Nodes {
		id := strings.TrimSpace(node.ID)
		if id == "" {
			return fmt.Errorf("The workflow has an empty node ID")
		}
		if seen[id] {
			return fmt.Errorf("The workflow has a duplicate node ID: %s", id)
		}
		seen[id] = true
		nodeType := strings.ToLower(strings.TrimSpace(node.Type))
		if nodeType == "" {
			return fmt.Errorf("Node '%s' is missing a node type", id)
		}
		if !allowedWorkflowNodeTypes[nodeType] {
			return fmt.Errorf("Node '%s' uses an unknown node type: %s", id, node.Type)
		}
	}
	return nil
}

func validateEdges(g *graphDef, idx *graphIndex) error {
	seen := make(map[string]bool, len(g.Edges))
	for _, edge := range g.Edges {
		if id := strings.TrimSpace(edge.ID); id != "" {
			if seen[id] {
				return fmt.Errorf("The workflow has a duplicate edge ID: %s", id)
			}
			seen[id] = true
		}
		source := strings.TrimSpace(edge.Source)
		target := strings.TrimSpace(edge.Target)
		if source == "" || target == "" {
			return fmt.Errorf("The workflow has an edge with an empty source or target")
		}
		if source == target {
			return fmt.Errorf("Edge '%s' cannot be a self-loop", firstNonEmpty(edge.ID, source))
		}
		if _, ok := idx.nodes[source]; !ok {
			return fmt.Errorf("Edge '%s' references a non-existent source node: %s", firstNonEmpty(edge.ID, source), source)
		}
		if _, ok := idx.nodes[target]; !ok {
			return fmt.Errorf("Edge '%s' references a non-existent target node: %s", firstNonEmpty(edge.ID, target), target)
		}
	}
	return nil
}

func validateNodeTopology(idx *graphIndex) error {
	starts := explicitStartNodeIDs(idx)
	if len(starts) == 0 {
		return fmt.Errorf("A workflow needs at least one start node")
	}
	outputs := outputNodeIDs(idx)
	if len(outputs) == 0 {
		return fmt.Errorf("A workflow needs at least one output node")
	}
	for id, node := range idx.nodes {
		inDegree := len(idx.incoming[id])
		outDegree := len(idx.outgoing[id])
		nodeType := strings.ToLower(strings.TrimSpace(node.Type))
		switch nodeType {
		case "start":
			if inDegree > 0 {
				return fmt.Errorf("Start node '%s' cannot have an incoming edge", firstNonEmpty(node.Label, id))
			}
			if outDegree == 0 {
				return fmt.Errorf("Start node '%s' needs at least one outgoing edge", firstNonEmpty(node.Label, id))
			}
		case "output", "end":
			if outDegree > 0 {
				return fmt.Errorf("%s Node '%s' cannot have an outgoing edge", displayNodeType(nodeType), firstNonEmpty(node.Label, id))
			}
			if inDegree == 0 {
				return fmt.Errorf("%s Node '%s' needs at least one incoming edge", displayNodeType(nodeType), firstNonEmpty(node.Label, id))
			}
		default:
			if inDegree == 0 {
				return fmt.Errorf("Node '%s' is unreachable: a non-start node must have an incoming edge", firstNonEmpty(node.Label, id))
			}
			if outDegree == 0 {
				return fmt.Errorf("Node '%s' has no outgoing edge; please connect it to an output/end node", firstNonEmpty(node.Label, id))
			}
		}
	}
	return nil
}

func validateNodeConfigs(idx *graphIndex) error {
	for id, node := range idx.nodes {
		label := firstNonEmpty(node.Label, id)
		switch strings.ToLower(strings.TrimSpace(node.Type)) {
		case "tool":
			if cfgString(node.Config, "tool_name") == "" {
				return fmt.Errorf("Tool node '%s' must select an MCP tool", label)
			}
			if err := validateToolConfig(node); err != nil {
				return err
			}
		case "agent":
			if cfgString(node.Config, "instruction") == "" {
				if _, ok := parseFieldBinding(node.Config, "input_binding"); !ok {
					return fmt.Errorf("Agent Node '%s' must fill in a node instruction or input binding", label)
				}
			}
			if cfgString(node.Config, "output_key") == "" {
				return fmt.Errorf("Agent Node '%s' must fill in the output variable name", label)
			}
		case "condition":
			if cfgString(node.Config, "expression") == "" {
				return fmt.Errorf("Condition node '%s' must fill in an expression", label)
			}
			if err := validateConditionExpression(cfgString(node.Config, "expression")); err != nil {
				return fmt.Errorf("Condition node '%s' has an invalid expression: %w", label, err)
			}
			if n := len(idx.outgoing[id]); n < 1 || n > 2 {
				return fmt.Errorf("Condition node '%s' needs 1 to 2 outgoing edges (Yes/No)", label)
			}
			if err := validateConditionBranchLabels(idx, id, node); err != nil {
				return err
			}
		case "output":
			if cfgString(node.Config, "output_key") == "" {
				return fmt.Errorf("Output node '%s' must fill in the output variable name", label)
			}
		}
		if err := validateJoinConfig(idx, id, node); err != nil {
			return err
		}
		if hasConditionalOutgoingEdges(idx, id) {
			if err := validateConditionalOutgoingEdges(idx, id, node); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateConditionalOutgoingEdges(idx *graphIndex, nodeID string, node graphNode) error {
	unconditional := 0
	for _, edge := range idx.outgoing[nodeID] {
		cond := firstNonEmpty(cfgString(edge.Config, "condition"), cfgString(edge.Config, "expression"))
		if cond != "" {
			if err := validateConditionExpression(cond); err != nil {
				return fmt.Errorf("Node '%s' has an invalid edge condition: %w", firstNonEmpty(node.Label, nodeID), err)
			}
		}
		if cond == "" {
			unconditional++
		}
	}
	if unconditional > 1 {
		return fmt.Errorf("Node '%s' can have at most one default branch among its conditional outgoing edges", firstNonEmpty(node.Label, nodeID))
	}
	return nil
}

func validateToolConfig(node graphNode) error {
	rawArgs := cfgString(node.Config, "arguments")
	if rawArgs != "" {
		if _, err := resolveToolArguments(node.Config, &WorkflowLocalState{}); err != nil {
			return fmt.Errorf("Tool node '%s' has invalid parameter JSON: %w", firstNonEmpty(node.Label, node.ID), err)
		}
	}
	if timeout := cfgString(node.Config, "timeout_seconds"); timeout != "" {
		if _, err := parsePositiveInt(timeout); err != nil {
			return fmt.Errorf("Tool node '%s' timeout must be a positive integer", firstNonEmpty(node.Label, node.ID))
		}
	}
	return nil
}

func validateJoinConfig(idx *graphIndex, nodeID string, node graphNode) error {
	strategy := joinStrategy(node)
	if !allowedJoinStrategies[strategy] {
		return fmt.Errorf("Node '%s' uses an unknown join strategy: %s", firstNonEmpty(node.Label, nodeID), strategy)
	}
	if len(idx.incoming[nodeID]) > 1 && strategy == "" {
		return fmt.Errorf("Node '%s' must declare a join strategy when it has multiple upstreams", firstNonEmpty(node.Label, nodeID))
	}
	return nil
}

func validateConditionBranchLabels(idx *graphIndex, nodeID string, node graphNode) error {
	seen := map[string]bool{}
	for _, edge := range idx.outgoing[nodeID] {
		hint := conditionBranchHint(edge)
		if hint == "" {
			return fmt.Errorf("The outgoing edges of condition node '%s' must be labeled Yes/No or true/false", firstNonEmpty(node.Label, nodeID))
		}
		if seen[hint] {
			return fmt.Errorf("Condition node '%s' has duplicate branch labels: %s", firstNonEmpty(node.Label, nodeID), hint)
		}
		seen[hint] = true
	}
	return nil
}

func validateDAG(idx *graphIndex) error {
	color := make(map[string]int, len(idx.nodes))
	var visit func(string) error
	visit = func(id string) error {
		switch color[id] {
		case 1:
			return fmt.Errorf("The workflow has a cycle; workflow orchestration must be a DAG: %s", id)
		case 2:
			return nil
		}
		color[id] = 1
		for _, edge := range idx.outgoing[id] {
			if err := visit(edge.Target); err != nil {
				return err
			}
		}
		color[id] = 2
		return nil
	}
	for id := range idx.nodes {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}

func validateReachability(idx *graphIndex) error {
	starts := explicitStartNodeIDs(idx)
	reached := make(map[string]bool, len(idx.nodes))
	queue := append([]string(nil), starts...)
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if reached[id] {
			continue
		}
		reached[id] = true
		for _, edge := range idx.outgoing[id] {
			queue = append(queue, edge.Target)
		}
	}
	for id, node := range idx.nodes {
		if !reached[id] {
			return fmt.Errorf("Node '%s' is unreachable: there is no path from the start node to it", firstNonEmpty(node.Label, id))
		}
	}

	canReachTerminal := make(map[string]bool, len(idx.nodes))
	visiting := make(map[string]bool, len(idx.nodes))
	var reachesTerminal func(string) bool
	reachesTerminal = func(id string) bool {
		if canReachTerminal[id] {
			return true
		}
		if visiting[id] {
			return false
		}
		visiting[id] = true
		node := idx.nodes[id]
		nodeType := strings.ToLower(strings.TrimSpace(node.Type))
		if nodeType == "output" || nodeType == "end" {
			canReachTerminal[id] = true
			visiting[id] = false
			return true
		}
		for _, edge := range idx.outgoing[id] {
			if reachesTerminal(edge.Target) {
				canReachTerminal[id] = true
				visiting[id] = false
				return true
			}
		}
		visiting[id] = false
		return false
	}
	for id, node := range idx.nodes {
		if !reachesTerminal(id) {
			return fmt.Errorf("Node '%s' cannot reach an output/end endpoint", firstNonEmpty(node.Label, id))
		}
	}
	return nil
}

func explicitStartNodeIDs(idx *graphIndex) []string {
	var ids []string
	for id, node := range idx.nodes {
		if strings.EqualFold(node.Type, "start") {
			ids = append(ids, id)
		}
	}
	sortNodeIDsByCanvas(ids, idx.nodes)
	return ids
}

func outputNodeIDs(idx *graphIndex) []string {
	var ids []string
	for id, node := range idx.nodes {
		if strings.EqualFold(node.Type, "output") {
			ids = append(ids, id)
		}
	}
	sortNodeIDsByCanvas(ids, idx.nodes)
	return ids
}

func displayNodeType(nodeType string) string {
	switch strings.ToLower(strings.TrimSpace(nodeType)) {
	case "output":
		return "Output"
	case "end":
		return "End"
	default:
		return nodeType
	}
}

func parsePositiveInt(s string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("not positive integer")
	}
	return n, nil
}
