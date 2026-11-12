package workflow

import (
	"context"
	"testing"
)

func TestEvalCondition_extendedOperators(t *testing.T) {
	state := newWorkflowLocalState(map[string]interface{}{"score": 9, "message": "status: ok"}, "run-expr")
	state.LastOutput = map[string]any{"output": "asset-123.example.com"}

	tests := []string{
		"{{inputs.score}} >= 9",
		"{{inputs.message}} contains ok",
		"{{previous.output}} matches ^asset-[0-9]+\\.example\\.com$",
		"{{inputs.score}} > 5 && {{inputs.message}} contains status",
	}
	for _, expr := range tests {
		if err := validateConditionExpression(expr); err != nil {
			t.Fatalf("validate %q: %v", expr, err)
		}
		if !evalCondition(expr, state) {
			t.Fatalf("evalCondition(%q) = false, want true", expr)
		}
	}
}

func TestEvalCondition_jsonPathAndJQSafeSubset(t *testing.T) {
	state := newWorkflowLocalState(map[string]interface{}{
		"payload": map[string]any{
			"risk": 9,
			"items": []any{
				map[string]any{"name": "first"},
			},
		},
	}, "run-jsonpath")
	state.LastOutput = map[string]any{"output": `{"status":"ok","score":7}`}

	tests := []string{
		`jsonpath({{inputs.payload}}, "$.risk") >= 8`,
		`jq({{inputs.payload}}, ".items[0].name") == first`,
		`jsonpath({{previous.output}}, "$.status") == ok`,
	}
	for _, expr := range tests {
		if err := validateConditionExpression(expr); err != nil {
			t.Fatalf("validate %q: %v", expr, err)
		}
		if !evalCondition(expr, state) {
			t.Fatalf("evalCondition(%q) = false, want true", expr)
		}
	}
}

func TestMergeUpstreamOutputs_allMerge(t *testing.T) {
	got := mergeUpstreamOutputs(JoinAllMerge, []map[string]any{
		{"output": "a", "left": 1},
		{"output": "b", "right": 2},
	})
	if got["kind"] != "join" || got["strategy"] != JoinAllMerge {
		t.Fatalf("join metadata = %#v", got)
	}
	values, ok := got["output"].([]any)
	if !ok || len(values) != 2 || values[0] != "a" || values[1] != "b" {
		t.Fatalf("merged output = %#v", got["output"])
	}
	if got["left"] != 1 || got["right"] != 2 {
		t.Fatalf("merged fields = %#v", got)
	}
}

func TestMergeUpstreamOutputs_firstNonEmpty(t *testing.T) {
	got := mergeUpstreamOutputs(JoinFirstNonEmpty, []map[string]any{
		{"output": ""},
		{"output": "winner"},
	})
	if got["output"] != "winner" {
		t.Fatalf("output = %#v, want winner", got["output"])
	}
}

func TestDryRunGraphJSON_simulatesUnsafeNodes(t *testing.T) {
	graph := `{
  "nodes": [
    {"id": "start-1", "type": "start", "label": "Start", "position": {"x": 0, "y": 0}, "config": {}},
    {"id": "agent-1", "type": "agent", "label": "Agent", "position": {"x": 0, "y": 80}, "config": {"instruction": "noop", "output_key": "agent_result"}},
    {"id": "out-1", "type": "output", "label": "Output", "position": {"x": 0, "y": 160}, "config": {"output_key": "result", "source_binding": {"from": "outputs", "field": "agent_result"}}}
  ],
  "edges": [
    {"id": "e1", "source": "start-1", "target": "agent-1"},
    {"id": "e2", "source": "agent-1", "target": "out-1"}
  ]
}`
	result, err := DryRunGraphJSON(nilContext(), graph, map[string]any{"message": "hello"})
	if err != nil {
		t.Fatalf("DryRunGraphJSON: %v", err)
	}
	if got := result.Outputs["result"]; got != "[dry-run] agent execution skipped" {
		t.Fatalf("result output = %#v", got)
	}
	if len(result.Trace) != 3 {
		t.Fatalf("trace len = %d, want 3", len(result.Trace))
	}
}

func nilContext() context.Context {
	return context.Background()
}
