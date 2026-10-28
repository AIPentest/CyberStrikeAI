package workflow

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"

	"github.com/cloudwego/eino/compose"
	"go.uber.org/zap"
)

func testWorkflowDB(t *testing.T) *database.DB {
	t.Helper()
	dir := t.TempDir()
	db, err := database.NewDB(filepath.Join(dir, "workflow.db"), zap.NewNop())
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func linearStartOutputGraph() string {
	return `{
  "nodes": [
    {"id": "start-1", "type": "start", "label": "开始", "position": {"x": 0, "y": 0}, "config": {}},
    {"id": "out-1", "type": "output", "label": "输出", "position": {"x": 0, "y": 120}, "config": {"output_key": "result", "source_binding": {"from": "inputs", "field": "message"}}}
  ],
  "edges": [
    {"id": "e1", "source": "start-1", "target": "out-1"}
  ],
  "config": {"schema_version": 1}
}`
}

func conditionBranchGraph() string {
	return `{
  "nodes": [
    {"id": "start-1", "type": "start", "label": "开始", "position": {"x": 0, "y": 0}, "config": {}},
    {"id": "cond-1", "type": "condition", "label": "判断", "position": {"x": 0, "y": 80}, "config": {"expression": "{{inputs.message}} == yes"}},
    {"id": "out-yes", "type": "output", "label": "是", "position": {"x": -80, "y": 160}, "config": {"output_key": "branch", "static_value": "yes"}},
    {"id": "out-no", "type": "output", "label": "否", "position": {"x": 80, "y": 160}, "config": {"output_key": "branch", "static_value": "no"}}
  ],
  "edges": [
    {"id": "e1", "source": "start-1", "target": "cond-1"},
    {"id": "e2", "source": "cond-1", "target": "out-yes", "label": "是"},
    {"id": "e3", "source": "cond-1", "target": "out-no", "label": "否"}
  ],
  "config": {"schema_version": 1}
}`
}

func TestValidateGraphJSON_linear(t *testing.T) {
	if err := ValidateGraphJSON(context.Background(), linearStartOutputGraph()); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestValidateGraphJSON_rejectsInvalidGraphs(t *testing.T) {
	tests := []struct {
		name    string
		graph   string
		wantErr string
	}{
		{
			name: "start with incoming edge",
			graph: `{
  "nodes": [
    {"id": "start-1", "type": "start", "label": "开始", "position": {"x": 0, "y": 0}, "config": {}},
    {"id": "agent-1", "type": "agent", "label": "Agent", "position": {"x": 0, "y": 80}, "config": {"instruction": "noop"}},
    {"id": "out-1", "type": "output", "label": "输出", "position": {"x": 0, "y": 160}, "config": {"output_key": "result"}}
  ],
  "edges": [
    {"id": "e1", "source": "start-1", "target": "agent-1"},
    {"id": "e2", "source": "agent-1", "target": "start-1"},
    {"id": "e3", "source": "agent-1", "target": "out-1"}
  ]
}`,
			wantErr: "start node",
		},
		{
			name: "output with outgoing edge",
			graph: `{
  "nodes": [
    {"id": "start-1", "type": "start", "label": "开始", "position": {"x": 0, "y": 0}, "config": {}},
    {"id": "out-1", "type": "output", "label": "输出", "position": {"x": 0, "y": 80}, "config": {"output_key": "result"}},
    {"id": "end-1", "type": "end", "label": "结束", "position": {"x": 0, "y": 160}, "config": {}}
  ],
  "edges": [
    {"id": "e1", "source": "start-1", "target": "out-1"},
    {"id": "e2", "source": "out-1", "target": "end-1"}
  ]
}`,
			wantErr: "cannot have outgoing edges",
		},
		{
			name: "tool without name",
			graph: `{
  "nodes": [
    {"id": "start-1", "type": "start", "label": "开始", "position": {"x": 0, "y": 0}, "config": {}},
    {"id": "tool-1", "type": "tool", "label": "工具", "position": {"x": 0, "y": 80}, "config": {}},
    {"id": "out-1", "type": "output", "label": "输出", "position": {"x": 0, "y": 160}, "config": {"output_key": "result"}}
  ],
  "edges": [
    {"id": "e1", "source": "start-1", "target": "tool-1"},
    {"id": "e2", "source": "tool-1", "target": "out-1"}
  ]
}`,
			wantErr: "must select an MCP tool",
		},
		{
			name: "condition with too many branches",
			graph: `{
  "nodes": [
    {"id": "start-1", "type": "start", "label": "开始", "position": {"x": 0, "y": 0}, "config": {}},
    {"id": "cond-1", "type": "condition", "label": "判断", "position": {"x": 0, "y": 80}, "config": {"expression": "{{inputs.message}}"}},
    {"id": "out-1", "type": "output", "label": "输出1", "position": {"x": -80, "y": 160}, "config": {"output_key": "a"}},
    {"id": "out-2", "type": "output", "label": "输出2", "position": {"x": 0, "y": 160}, "config": {"output_key": "b"}},
    {"id": "out-3", "type": "output", "label": "输出3", "position": {"x": 80, "y": 160}, "config": {"output_key": "c"}}
  ],
  "edges": [
    {"id": "e1", "source": "start-1", "target": "cond-1"},
    {"id": "e2", "source": "cond-1", "target": "out-1"},
    {"id": "e3", "source": "cond-1", "target": "out-2"},
    {"id": "e4", "source": "cond-1", "target": "out-3"}
  ]
}`,
			wantErr: "1 to 2 outgoing edges",
		},
		{
			name: "orphan node",
			graph: `{
  "nodes": [
    {"id": "start-1", "type": "start", "label": "开始", "position": {"x": 0, "y": 0}, "config": {}},
    {"id": "out-1", "type": "output", "label": "输出", "position": {"x": 0, "y": 80}, "config": {"output_key": "result"}},
    {"id": "agent-1", "type": "agent", "label": "孤岛", "position": {"x": 200, "y": 80}, "config": {"instruction": "noop"}}
  ],
  "edges": [
    {"id": "e1", "source": "start-1", "target": "out-1"}
  ]
}`,
			wantErr: "unreachable",
		},
		{
			name: "cycle",
			graph: `{
  "nodes": [
    {"id": "start-1", "type": "start", "label": "开始", "position": {"x": 0, "y": 0}, "config": {}},
    {"id": "agent-1", "type": "agent", "label": "Agent1", "position": {"x": 0, "y": 80}, "config": {"instruction": "noop", "output_key": "a1"}},
    {"id": "agent-2", "type": "agent", "label": "Agent2", "position": {"x": 0, "y": 160}, "config": {"instruction": "noop", "output_key": "a2"}},
    {"id": "out-1", "type": "output", "label": "输出", "position": {"x": 0, "y": 240}, "config": {"output_key": "result"}}
  ],
  "edges": [
    {"id": "e1", "source": "start-1", "target": "agent-1"},
    {"id": "e2", "source": "agent-1", "target": "agent-2"},
    {"id": "e3", "source": "agent-2", "target": "agent-1"},
    {"id": "e4", "source": "agent-2", "target": "out-1"}
  ]
}`,
			wantErr: "cycle",
		},
		{
			name: "output without key",
			graph: `{
  "nodes": [
    {"id": "start-1", "type": "start", "label": "开始", "position": {"x": 0, "y": 0}, "config": {}},
    {"id": "out-1", "type": "output", "label": "输出", "position": {"x": 0, "y": 80}, "config": {}}
  ],
  "edges": [
    {"id": "e1", "source": "start-1", "target": "out-1"}
  ]
}`,
			wantErr: "output variable name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateGraphJSON(context.Background(), tt.graph)
			if err == nil {
				t.Fatal("expected validation error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestCompileEngine_linear(t *testing.T) {
	ctx := context.Background()
	SetCheckpointDir(t.TempDir())
	g, err := parseGraph(linearStartOutputGraph())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := defaultEngine.compile(ctx, g); err != nil {
		t.Fatalf("compile: %v", err)
	}
}

func createTestWorkflowRun(t *testing.T, db *database.DB, runID string) {
	t.Helper()
	if err := db.CreateWorkflowRun(&database.WorkflowRun{
		ID:         runID,
		WorkflowID: "test-wf",
		Status:     "running",
	}); err != nil {
		t.Fatalf("CreateWorkflowRun: %v", err)
	}
}

func TestExecuteEinoGraph_linearStartOutput(t *testing.T) {
	ctx := context.Background()
	SetCheckpointDir(t.TempDir())
	db := testWorkflowDB(t)
	createTestWorkflowRun(t, db, "run-linear")
	g, err := parseGraph(linearStartOutputGraph())
	if err != nil {
		t.Fatal(err)
	}
	state := newWorkflowLocalState(map[string]interface{}{"message": "ping"}, "run-linear")
	args := RunArgs{DB: db}
	if err := executeEinoGraph(ctx, args, "run-linear", "test-wf", 1, g, state); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got := state.Outputs["result"]; got != "ping" {
		t.Fatalf("outputs[result] = %v, want ping", got)
	}
	if len(state.Executed) != 2 {
		t.Fatalf("executed nodes = %d, want 2", len(state.Executed))
	}
}

func TestExecuteEinoGraph_checkpointRestoresStartOutput(t *testing.T) {
	ctx := context.Background()
	checkpointStore, err := newFileCheckPointStore(t.TempDir())
	if err != nil {
		t.Fatalf("new checkpoint store: %v", err)
	}
	state := newWorkflowLocalState(map[string]interface{}{"message": "ping"}, "run-checkpoint")
	node := graphNode{ID: "start-1", Type: "start"}
	wf := compose.NewWorkflow[WorkflowInput, WorkflowOutput](
		compose.WithGenLocalState(func(context.Context) *WorkflowLocalState { return state }),
	)
	start := wf.AddLambdaNode("start-1", compose.InvokableLambda(func(_ context.Context, input WorkflowInput) (WorkflowNodeOutput, error) {
		result := startOutputMap(node, input.Message, input.ConversationID, input.ProjectID)
		state.NodeOutputs[node.ID] = result
		state.NodeOutputs["condition-1"] = conditionOutputMap(graphNode{ID: "condition-1", Type: "condition"}, "{{inputs.message}} == ping", true)
		state.NodeOutputs["tool-1"] = toolOutputMap(graphNode{ID: "tool-1", Type: "tool"}, "tool result", "lookup", map[string]any{"id": "1"}, "exec-1", false)
		state.NodeOutputs["agent-1"] = agentOutputMap(graphNode{ID: "agent-1", Type: "agent"}, "agent result", "chat", []string{"exec-1"})
		state.NodeOutputs["hitl-1"] = hitlOutputMap(graphNode{ID: "hitl-1", Type: "hitl"}, "completed", "approved", "continue?", "reviewer", true)
		state.NodeOutputs["output-1"] = outputNodeOutputMap(graphNode{ID: "output-1", Type: "output"}, "result", "ping")
		state.NodeOutputs["end-1"] = endOutputMap(graphNode{ID: "end-1", Type: "end"}, "done")
		state.LastOutput = result
		state.Outputs["seed"] = "preserved"
		return result, nil
	}))
	outputNode := wf.AddLambdaNode("out-1", compose.InvokableLambda(func(_ context.Context, input WorkflowNodeOutput) (WorkflowNodeOutput, error) {
		return input, nil
	}))
	start.AddInput(compose.START)
	outputNode.AddInput("start-1")
	wf.End().AddInput("out-1", compose.ToField("out-1"))
	runnable, err := wf.Compile(ctx,
		compose.WithCheckPointStore(checkpointStore),
		compose.WithInterruptAfterNodes([]string{"start-1"}),
	)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	_, err = runnable.Invoke(ctx, workflowInputFromMap(state.Inputs), compose.WithCheckPointID("run-checkpoint"))
	info, ok := compose.ExtractInterruptInfo(err)
	if !ok {
		t.Fatalf("invoke error = %v, want checkpoint interrupt", err)
	}
	restored, ok := info.State.(*WorkflowLocalState)
	if !ok {
		t.Fatalf("checkpoint state = %T, want *WorkflowLocalState", info.State)
	}
	for nodeID, wantType := range map[string]string{
		"start-1":     "StartOutput",
		"condition-1": "ConditionOutput",
		"tool-1":      "ToolOutput",
		"agent-1":     "AgentOutput",
		"hitl-1":      "HITLOutput",
		"output-1":    "OutputNodeOutput",
		"end-1":       "NodeOutputEnvelope",
	} {
		if got := fmt.Sprintf("%T", restored.NodeOutputs[nodeID]["typed"]); got != "workflow."+wantType {
			t.Fatalf("restored %s typed output = %s, want workflow.%s", nodeID, got, wantType)
		}
	}
	if got := valueFromPath("previous.message", restored); got != "ping" {
		t.Fatalf("restored previous.message = %v, want ping", got)
	}
	if got := valueFromPath("inputs.message", restored); got != "ping" {
		t.Fatalf("restored inputs.message = %v, want ping", got)
	}
	if got := valueFromPath("outputs.seed", restored); got != "preserved" {
		t.Fatalf("restored outputs.seed = %v, want preserved", got)
	}

	result, err := runnable.Invoke(ctx, WorkflowInput{}, compose.WithCheckPointID("run-checkpoint"))
	if err != nil {
		t.Fatalf("resume checkpoint: %v", err)
	}
	output, ok := result["out-1"].(map[string]any)
	if !ok {
		t.Fatalf("resumed output type = %T, want map[string]any", result["out-1"])
	}
	if got := output["output"]; got != "ping" {
		t.Fatalf("resumed output = %v, want ping", got)
	}
}

func TestExecuteEinoGraph_conditionBranch(t *testing.T) {
	ctx := context.Background()
	SetCheckpointDir(t.TempDir())
	db := testWorkflowDB(t)
	createTestWorkflowRun(t, db, "run-yes")
	createTestWorkflowRun(t, db, "run-no")
	g, err := parseGraph(conditionBranchGraph())
	if err != nil {
		t.Fatal(err)
	}

	stateYes := newWorkflowLocalState(map[string]interface{}{"message": "yes"}, "run-yes")
	if err := executeEinoGraph(ctx, RunArgs{DB: db}, "run-yes", "test-wf-branch", 1, g, stateYes); err != nil {
		t.Fatalf("execute yes: %v", err)
	}
	if got := stateYes.Outputs["branch"]; got != "yes" {
		t.Fatalf("yes branch output = %v", got)
	}

	stateNo := newWorkflowLocalState(map[string]interface{}{"message": "no"}, "run-no")
	if err := executeEinoGraph(ctx, RunArgs{DB: db}, "run-no", "test-wf-branch", 1, g, stateNo); err != nil {
		t.Fatalf("execute no: %v", err)
	}
	if got := stateNo.Outputs["branch"]; got != "no" {
		t.Fatalf("no branch output = %v", got)
	}
}

func TestRunRoleBoundWorkflow_integration(t *testing.T) {
	ctx := context.Background()
	SetCheckpointDir(t.TempDir())
	db := testWorkflowDB(t)
	graph := linearStartOutputGraph()
	if err := db.UpsertWorkflowDefinition(&database.WorkflowDefinition{
		ID:        "wf-linear",
		Name:      "线性流程",
		Version:   1,
		GraphJSON: graph,
		Enabled:   true,
	}); err != nil {
		t.Fatal(err)
	}
	role := config.RoleConfig{
		Name:           "tester",
		Enabled:        true,
		WorkflowID:     "wf-linear",
		WorkflowPolicy: "auto",
	}
	result, err := RunRoleBoundWorkflow(ctx, RunArgs{
		DB:          db,
		Logger:      zap.NewNop(),
		Role:        role,
		UserMessage: "from-role",
	})
	if err != nil {
		t.Fatalf("RunRoleBoundWorkflow: %v", err)
	}
	if result == nil || result.RunID == "" {
		t.Fatal("expected run result")
	}
}

func TestCompiledCache_reuse(t *testing.T) {
	ctx := context.Background()
	SetCheckpointDir(t.TempDir())
	InvalidateCompiledCache("cache-wf")
	g, err := parseGraph(linearStartOutputGraph())
	if err != nil {
		t.Fatal(err)
	}
	a1, err := defaultEngine.getOrCompile(ctx, "cache-wf", 1, g)
	if err != nil {
		t.Fatal(err)
	}
	a2, err := defaultEngine.getOrCompile(ctx, "cache-wf", 1, g)
	if err != nil {
		t.Fatal(err)
	}
	if a1 != a2 {
		t.Fatal("expected cached artifact pointer reuse")
	}
	InvalidateCompiledCache("cache-wf")
	a3, err := defaultEngine.getOrCompile(ctx, "cache-wf", 1, g)
	if err != nil {
		t.Fatal(err)
	}
	if a1 == a3 {
		t.Fatal("expected new artifact after invalidation")
	}
}
