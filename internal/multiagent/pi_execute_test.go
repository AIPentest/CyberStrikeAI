package multiagent

import (
	"context"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/agentruntime"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/mcp"
	"go.uber.org/zap"
)

type nativeExecuteRegistry struct {
	mu           sync.Mutex
	cancel       context.CancelFunc
	note         string
	started      chan struct{}
	unregistered bool
}

func (r *nativeExecuteRegistry) RegisterActiveAgentExecute(_ string, cancel context.CancelFunc) {
	r.mu.Lock()
	r.cancel = cancel
	r.mu.Unlock()
	close(r.started)
}
func (r *nativeExecuteRegistry) UnregisterActiveAgentExecute(string) {
	r.mu.Lock()
	r.unregistered = true
	r.mu.Unlock()
}
func (r *nativeExecuteRegistry) AbortActiveAgentExecute(_, note string) bool {
	r.mu.Lock()
	r.note = note
	c := r.cancel
	r.mu.Unlock()
	if c == nil {
		return false
	}
	c()
	return true
}
func (r *nativeExecuteRegistry) TakeAgentExecuteAbortNote(string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	note := r.note
	r.note = ""
	return note
}

func nativeExecuteFixture(t *testing.T) *piRun {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses the POSIX host shell")
	}
	cfg := &config.Config{}
	cfg.MultiAgent.Runtime.ReductionRootDir = t.TempDir()
	cfg.Agent.ShellNoOutputTimeoutSeconds = -1
	srv := mcp.NewServer(zap.NewNop())
	ag := agent.NewAgent(&cfg.OpenAI, &cfg.Agent, srv, nil, zap.NewNop(), 2)
	return &piRun{cfg: cfg, ag: ag, conversationID: "fixture"}
}

func TestPiNativeExecuteUsesCurrentToolCancellationRegistry(t *testing.T) {
	r := nativeExecuteFixture(t)
	registry := &nativeExecuteRegistry{started: make(chan struct{})}
	parent, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx := mcp.WithAgentExecuteRunRegistry(parent, registry)
	done := make(chan agentruntime.ToolResult, 1)
	go func() {
		result, err := r.executePiLocal(ctx, agentruntime.ToolCall{Name: "execute", Arguments: map[string]any{"command": "sleep 30"}})
		if err != nil {
			result.Content = err.Error()
			result.IsError = true
		}
		done <- result
	}()
	select {
	case <-registry.started:
	case <-parent.Done():
		t.Fatal("execute was not registered")
	}
	if !registry.AbortActiveAgentExecute("fixture", "inspect existing evidence instead") {
		t.Fatal("cancel not accepted")
	}
	select {
	case result := <-done:
		if !result.IsError || !strings.Contains(result.Content, "inspect existing evidence instead") {
			t.Fatalf("cancel result: %+v", result)
		}
	case <-parent.Done():
		t.Fatal("execute did not stop")
	}
	if parent.Err() != nil {
		t.Fatal("tool cancellation canceled the parent task")
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if !registry.unregistered {
		t.Fatal("cancel hook leaked")
	}
}

func TestPiNativeExecuteStopsAfterConfiguredIdleTimeout(t *testing.T) {
	r := nativeExecuteFixture(t)
	r.cfg.Agent.ShellNoOutputTimeoutSeconds = 1
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := r.executePiLocal(ctx, agentruntime.ToolCall{Name: "execute", Arguments: map[string]any{"command": "sleep 30"}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError || !strings.Contains(result.Content, "without stdout/stderr") {
		t.Fatalf("idle timeout lost: %+v", result)
	}
	if ctx.Err() != nil {
		t.Fatal("idle timeout depended on parent deadline")
	}
}
