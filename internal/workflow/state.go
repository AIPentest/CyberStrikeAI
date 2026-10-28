package workflow

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/cloudwego/eino/schema"
)

func init() {
	schema.RegisterName[*WorkflowLocalState]("_cyberstrike_workflow_local_state")
	schema.RegisterName[NodeOutputEnvelope]("_cyberstrike_workflow_node_output_envelope")
	schema.RegisterName[StartOutput]("_cyberstrike_workflow_start_output")
	schema.RegisterName[ConditionOutput]("_cyberstrike_workflow_condition_output")
	schema.RegisterName[ToolOutput]("_cyberstrike_workflow_tool_output")
	schema.RegisterName[AgentOutput]("_cyberstrike_workflow_agent_output")
	schema.RegisterName[HITLOutput]("_cyberstrike_workflow_hitl_output")
	schema.RegisterName[OutputNodeOutput]("_cyberstrike_workflow_output_node_output")
}

// WorkflowLocalState is the Eino WithGenLocalState payload (checkpoint-serializable).
type WorkflowLocalState struct {
	Inputs              map[string]any            `json:"inputs,omitempty"`
	Outputs             map[string]any            `json:"outputs,omitempty"`
	NodeOutputs         map[string]map[string]any `json:"nodeOutputs,omitempty"`
	NodeProceed         map[string]bool           `json:"nodeProceed,omitempty"`
	LastOutput          map[string]any            `json:"lastOutput,omitempty"`
	Metrics             map[string]any            `json:"metrics,omitempty"`
	Executed            []string                  `json:"executed,omitempty"`
	Skipped             []string                  `json:"skipped,omitempty"`
	WorkflowRunID       string                    `json:"workflowRunId,omitempty"`
	MainIterationOffset int                       `json:"mainIterationOffset,omitempty"`
	SegmentMaxIteration int                       `json:"segmentMaxIteration,omitempty"`
}

func newWorkflowLocalState(inputs map[string]interface{}, runID string) *WorkflowLocalState {
	in := make(map[string]any, len(inputs))
	for k, v := range inputs {
		in[k] = v
	}
	return &WorkflowLocalState{
		Inputs:        in,
		Outputs:       make(map[string]any),
		NodeOutputs:   make(map[string]map[string]any),
		NodeProceed:   make(map[string]bool),
		Metrics:       make(map[string]any),
		WorkflowRunID: runID,
	}
}

var templateVarRe = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_.-]+)\s*\}\}`)

func resolveTemplate(s string, state *WorkflowLocalState) string {
	if strings.TrimSpace(s) == "" {
		return fmt.Sprint(valueFromPath("previous.output", state))
	}
	return templateVarRe.ReplaceAllStringFunc(s, func(match string) string {
		m := templateVarRe.FindStringSubmatch(match)
		if len(m) != 2 {
			return match
		}
		return fmt.Sprint(valueFromPath(m[1], state))
	})
}

func valueFromPath(path string, state *WorkflowLocalState) any {
	parts := strings.Split(path, ".")
	if len(parts) == 0 {
		return ""
	}
	var cur any
	switch parts[0] {
	case "inputs", "input":
		cur = state.Inputs
	case "previous", "prev":
		cur = state.LastOutput
	case "outputs":
		cur = state.Outputs
	default:
		if v, ok := state.Inputs[parts[0]]; ok {
			cur = v
		} else if v, ok := state.NodeOutputs[parts[0]]; ok {
			cur = v
		} else {
			return ""
		}
	}
	for _, p := range parts[1:] {
		m, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = m[p]
	}
	if cur == nil {
		return ""
	}
	return cur
}

func cleanComparable(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, `"'`)
	return s
}

func edgeAllowed(edge graphEdge, sourceNode graphNode, edgeIndex int, state *WorkflowLocalState) bool {
	cond := firstNonEmpty(cfgString(edge.Config, "condition"), cfgString(edge.Config, "expression"))
	if cond != "" {
		return evalCondition(cond, state)
	}
	if strings.EqualFold(strings.TrimSpace(sourceNode.Type), "condition") {
		return conditionBranchAllowed(edge, edgeIndex, state)
	}
	return true
}

func conditionBranchAllowed(edge graphEdge, edgeIndex int, state *WorkflowLocalState) bool {
	matched := conditionMatched(state)
	if branch := conditionBranchHint(edge); branch != "" {
		return (branch == "true" && matched) || (branch == "false" && !matched)
	}
	switch edgeIndex {
	case 0:
		return matched
	case 1:
		return !matched
	default:
		return false
	}
}

func conditionMatched(state *WorkflowLocalState) bool {
	v := strings.ToLower(cleanComparable(fmt.Sprint(valueFromPath("previous.matched", state))))
	return v == "true" || v == "1"
}

func conditionBranchHint(edge graphEdge) string {
	if edge.Config != nil {
		switch strings.ToLower(strings.TrimSpace(cfgString(edge.Config, "branch"))) {
		case "true", "yes", "y", "是":
			return "true"
		case "false", "no", "n", "否":
			return "false"
		}
	}
	switch strings.ToLower(strings.TrimSpace(edge.Label)) {
	case "true", "yes", "y", "是":
		return "true"
	case "false", "no", "n", "否":
		return "false"
	}
	return ""
}

func cfgString(cfg map[string]any, key string) string {
	if cfg == nil {
		return ""
	}
	if v, ok := cfg[key]; ok {
		return strings.TrimSpace(fmt.Sprint(v))
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if s := strings.TrimSpace(value); s != "" {
			return s
		}
	}
	return ""
}

func truncateWorkflowPreview(s string, limit int) string {
	s = strings.TrimSpace(s)
	if limit <= 0 || len([]rune(s)) <= limit {
		return s
	}
	runes := []rune(s)
	return string(runes[:limit]) + "..."
}

func renderWorkflowResponse(roleName, workflowName string, version int, runID string, state *WorkflowLocalState) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Role %q completed workflow %q (version %d).\n\n", roleName, workflowName, version))
	sb.WriteString(fmt.Sprintf("Run ID: %s\n", runID))
	sb.WriteString(fmt.Sprintf("Executed nodes: %d", len(state.Executed)))
	if len(state.Skipped) > 0 {
		sb.WriteString(fmt.Sprintf(", skipped nodes: %d", len(state.Skipped)))
	}
	sb.WriteString("\n\n")
	if len(state.Outputs) > 0 {
		sb.WriteString("Outputs:\n")
		keys := make([]string, 0, len(state.Outputs))
		for k := range state.Outputs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			sb.WriteString(fmt.Sprintf("- %s: %v\n", k, state.Outputs[k]))
		}
	} else {
		sb.WriteString("No outputs yet. Check whether an output node is configured, or whether a condition branch was hit.\n")
	}
	if len(state.Skipped) > 0 {
		sb.WriteString("\nNon-executed node types still keep run records: ")
		sb.WriteString(strings.Join(state.Skipped, ", "))
		sb.WriteString(".")
	}
	return strings.TrimSpace(sb.String())
}
