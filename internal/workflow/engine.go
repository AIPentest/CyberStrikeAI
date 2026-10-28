package workflow

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/cloudwego/eino/compose"
)

type compiledArtifact struct {
	runnable compose.Runnable[WorkflowInput, WorkflowOutput]
	idx      *graphIndex
	hitlIDs  []string
}

// Engine compiles and caches Eino Workflow artifacts.
type Engine struct {
	mu            sync.RWMutex
	cache         map[string]*compiledArtifact
	cpStore       compose.CheckPointStore
	cpStoreMu     sync.Once
	cpStoreErr    error
	checkpointDir string
}

var defaultEngine = &Engine{
	cache:         make(map[string]*compiledArtifact),
	checkpointDir: "data/workflow-checkpoints",
}

// SetCheckpointDir overrides the workflow checkpoint root (mainly for tests).
func SetCheckpointDir(dir string) {
	defaultEngine.mu.Lock()
	defer defaultEngine.mu.Unlock()
	defaultEngine.checkpointDir = strings.TrimSpace(dir)
	defaultEngine.cpStore = nil
	defaultEngine.cpStoreErr = nil
	defaultEngine.cpStoreMu = sync.Once{}
}

func (e *Engine) checkpointStore() (compose.CheckPointStore, error) {
	e.cpStoreMu.Do(func() {
		e.cpStore, e.cpStoreErr = newFileCheckPointStore(e.checkpointDir)
	})
	return e.cpStore, e.cpStoreErr
}

// InvalidateCompiledCache drops cached compilations for a workflow id.
func InvalidateCompiledCache(workflowID string) {
	workflowID = strings.TrimSpace(workflowID)
	if workflowID == "" {
		return
	}
	defaultEngine.mu.Lock()
	defer defaultEngine.mu.Unlock()
	for key := range defaultEngine.cache {
		if strings.HasPrefix(key, workflowID+":") {
			delete(defaultEngine.cache, key)
		}
	}
}

// ValidateGraphJSON parses and trial-compiles a canvas graph (save-time gate).
func ValidateGraphJSON(ctx context.Context, graphJSON string) error {
	g, err := parseGraph(graphJSON)
	if err != nil {
		return err
	}
	idx := indexGraph(g)
	if err := validateGraphDefinition(g, idx); err != nil {
		return err
	}
	_, err = defaultEngine.compile(ctx, g)
	return err
}

func hasTerminalNode(idx *graphIndex) bool {
	for id, node := range idx.nodes {
		if len(idx.outgoing[id]) == 0 {
			return true
		}
		if strings.EqualFold(node.Type, "end") || strings.EqualFold(node.Type, "output") {
			return true
		}
	}
	return false
}

func (e *Engine) getOrCompile(ctx context.Context, workflowID string, version int, g *graphDef) (*compiledArtifact, error) {
	key := cacheKey(workflowID, version)
	e.mu.RLock()
	if art, ok := e.cache[key]; ok {
		e.mu.RUnlock()
		return art, nil
	}
	e.mu.RUnlock()

	art, err := e.compile(ctx, g)
	if err != nil {
		return nil, err
	}
	e.mu.Lock()
	if existing, ok := e.cache[key]; ok {
		e.mu.Unlock()
		return existing, nil
	}
	e.cache[key] = art
	e.mu.Unlock()
	return art, nil
}

func (e *Engine) compile(ctx context.Context, g *graphDef) (*compiledArtifact, error) {
	cpStore, err := e.checkpointStore()
	if err != nil {
		return nil, err
	}
	idx := indexGraph(g)
	if err := validateGraphDefinition(g, idx); err != nil {
		return nil, err
	}
	hitlIDs := collectHITLNodeIDs(idx)
	compileOpts := []compose.GraphCompileOption{
		compose.WithGraphName("CyberStrikeWorkflow"),
		compose.WithCheckPointStore(cpStore),
	}
	if len(hitlIDs) > 0 {
		compileOpts = append(compileOpts, compose.WithInterruptBeforeNodes(hitlIDs))
	}

	wf := compose.NewWorkflow[WorkflowInput, WorkflowOutput](
		compose.WithGenLocalState(func(runCtx context.Context) *WorkflowLocalState {
			if rt := workflowRuntimeFrom(runCtx); rt != nil && rt.state != nil {
				return rt.state
			}
			return &WorkflowLocalState{
				Outputs:     make(map[string]any),
				NodeOutputs: make(map[string]map[string]any),
				NodeProceed: make(map[string]bool),
			}
		}),
	)

	nodeRefs := make(map[string]*compose.WorkflowNode, len(idx.nodes))
	for id, node := range idx.nodes {
		n := node
		if strings.EqualFold(n.Type, "agent") {
			sub, err := compileAgentSubgraph(ctx, n)
			if err != nil {
				return nil, fmt.Errorf("failed to compile Agent subgraph %s: %w", id, err)
			}
			nodeRefs[id] = wf.AddGraphNode(id, sub)
			continue
		}
		if strings.EqualFold(n.Type, "start") {
			nodeRefs[id] = wf.AddLambdaNode(id, compose.InvokableLambda(func(runCtx context.Context, _ WorkflowInput) (WorkflowNodeOutput, error) {
				return runWorkflowNodeLambda(runCtx, n)
			}))
			continue
		}
		nodeRefs[id] = wf.AddLambdaNode(id, compose.InvokableLambda(func(runCtx context.Context, _ WorkflowNodeOutput) (WorkflowNodeOutput, error) {
			return runWorkflowNodeLambda(runCtx, n)
		}))
	}

	for id, node := range idx.nodes {
		if strings.EqualFold(node.Type, "condition") {
			if err := wireConditionBranch(wf, nodeRefs, idx, id, node); err != nil {
				return nil, err
			}
			continue
		}
		if hasConditionalOutgoingEdges(idx, id) {
			if err := wireEdgeConditionBranch(wf, nodeRefs, idx, id, node); err != nil {
				return nil, err
			}
			continue
		}
		for _, edge := range idx.outgoing[id] {
			if target, ok := nodeRefs[edge.Target]; ok {
				target.AddInput(id)
			}
		}
	}

	for _, startID := range findStartNodeIDs(idx) {
		if ref, ok := nodeRefs[startID]; ok {
			ref.AddInput(compose.START)
		}
	}

	endNode := wf.End()
	for id, node := range idx.nodes {
		if len(idx.outgoing[id]) == 0 || strings.EqualFold(node.Type, "end") {
			endNode.AddInput(id, compose.ToField(id))
		}
	}

	runnable, err := wf.Compile(ctx, compileOpts...)
	if err != nil {
		return nil, err
	}
	return &compiledArtifact{runnable: runnable, idx: idx, hitlIDs: hitlIDs}, nil
}

func collectHITLNodeIDs(idx *graphIndex) []string {
	var ids []string
	for id, node := range idx.nodes {
		if strings.EqualFold(node.Type, "hitl") {
			ids = append(ids, id)
		}
	}
	return ids
}

func runWorkflowNodeLambda(runCtx context.Context, n graphNode) (WorkflowNodeOutput, error) {
	localRT := workflowRuntimeFrom(runCtx)
	if localRT == nil {
		return nil, fmt.Errorf("workflow runtime missing in context")
	}
	if err := prepareNodeInputState(localRT, n); err != nil {
		return nil, err
	}
	result, proceed, err := executeNode(runCtx, localRT.args, localRT.runID, n, localRT.state)
	if err != nil {
		return nil, err
	}
	localRT.state.NodeOutputs[n.ID] = result
	localRT.state.LastOutput = result
	if !proceed && !strings.EqualFold(n.Type, "end") {
		label := firstNonEmpty(n.Label, n.ID)
		if errText := cfgString(result, "error"); errText != "" {
			return result, fmt.Errorf("node %q failed: %s", label, errText)
		}
			return result, fmt.Errorf("node %q did not proceed", label)
	}
	return result, nil
}
