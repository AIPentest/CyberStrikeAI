package workflow

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

type compiledArtifact struct {
	order   []string
	idx     *graphIndex
	hitlIDs []string
}

// Engine compiles and caches Eino Workflow artifacts.
type Engine struct {
	mu            sync.RWMutex
	cache         map[string]*compiledArtifact
	cpStore       *fileCheckPointStore
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

func (e *Engine) checkpointStore() (*fileCheckPointStore, error) {
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
	idx := indexGraph(g)
	if err := validateGraphDefinition(g, idx); err != nil {
		return nil, err
	}
	indegree := map[string]int{}
	var ready []string
	for id := range idx.nodes {
		indegree[id] = len(idx.incoming[id])
		if indegree[id] == 0 {
			ready = append(ready, id)
		}
	}
	var order []string
	for len(ready) > 0 {
		sortNodeIDsByCanvas(ready, idx.nodes)
		id := ready[0]
		ready = ready[1:]
		order = append(order, id)
		for _, edge := range idx.outgoing[id] {
			indegree[edge.Target]--
			if indegree[edge.Target] == 0 {
				ready = append(ready, edge.Target)
			}
		}
	}
	if len(order) != len(idx.nodes) {
		return nil, fmt.Errorf("workflow contains a cycle")
	}
	return &compiledArtifact{order: order, idx: idx, hitlIDs: collectHITLNodeIDs(idx)}, nil
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
			return result, fmt.Errorf("节点「%s」失败: %s", label, errText)
		}
		return result, fmt.Errorf("节点「%s」未继续执行", label)
	}
	return result, nil
}
