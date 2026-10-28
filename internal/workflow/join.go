package workflow

import (
	"fmt"
	"strings"
)

const (
	JoinAllMerge      = "all_merge"
	JoinLastByCanvas  = "last_by_canvas"
	JoinFirstNonEmpty = "first_non_empty"
	JoinFailFast      = "fail_fast"
)

var allowedJoinStrategies = map[string]bool{
	JoinAllMerge:      true,
	JoinLastByCanvas:  true,
	JoinFirstNonEmpty: true,
	JoinFailFast:      true,
}

func joinStrategy(node graphNode) string {
	strategy := strings.ToLower(cfgString(node.Config, "join_strategy"))
	if strategy == "" {
		return JoinAllMerge
	}
	return strategy
}

func prepareNodeInputState(rt *workflowRuntime, node graphNode) error {
	if rt == nil || rt.idx == nil || rt.state == nil {
		return nil
	}
	incoming := rt.idx.incoming[node.ID]
	if len(incoming) <= 1 {
		return nil
	}
	strategy := joinStrategy(node)
	if !allowedJoinStrategies[strategy] {
		return fmt.Errorf("node %q uses an unknown join strategy: %s", firstNonEmpty(node.Label, node.ID), strategy)
	}
	upstreams := make([]map[string]any, 0, len(incoming))
	for _, edge := range incoming {
		out := rt.state.NodeOutputs[edge.Source]
		if out == nil {
			continue
		}
		if isFailedNodeOutput(out) && strategy == JoinFailFast {
			return fmt.Errorf("upstream node %q failed, join strategy fail_fast aborted", edge.Source)
		}
		upstreams = append(upstreams, out)
	}
	if len(upstreams) == 0 {
		return nil
	}
	rt.state.LastOutput = mergeUpstreamOutputs(strategy, upstreams)
	return nil
}

func mergeUpstreamOutputs(strategy string, upstreams []map[string]any) map[string]any {
	switch strategy {
	case JoinLastByCanvas:
		return cloneNodeOutput(upstreams[len(upstreams)-1])
	case JoinFirstNonEmpty:
		for _, out := range upstreams {
			if !isEmptyOutputValue(out["output"]) {
				return cloneNodeOutput(out)
			}
		}
		return cloneNodeOutput(upstreams[0])
	default:
		merged := map[string]any{
			"kind":      "join",
			"strategy":  strategy,
			"upstreams": upstreams,
		}
		values := make([]any, 0, len(upstreams))
		for _, out := range upstreams {
			values = append(values, out["output"])
			for k, v := range out {
				if _, exists := merged[k]; !exists {
					merged[k] = v
				}
			}
		}
		merged["output"] = values
		return merged
	}
}

func cloneNodeOutput(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func isEmptyOutputValue(v any) bool {
	if v == nil {
		return true
	}
	return strings.TrimSpace(fmt.Sprint(v)) == ""
}

func isFailedNodeOutput(out map[string]any) bool {
	if out == nil {
		return false
	}
	if v, ok := out["error"]; ok && strings.TrimSpace(fmt.Sprint(v)) != "" {
		return true
	}
	if v, ok := out["is_error"]; ok {
		return strings.EqualFold(fmt.Sprint(v), "true")
	}
	return false
}
