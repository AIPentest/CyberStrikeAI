package multiagent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/mcp"
	"go.uber.org/zap"
)

func piTestConfig(t *testing.T) *config.Config {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node not installed")
	}
	worker, err := filepath.Abs("../../runtime/pi/dist/worker.js")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(worker); err != nil {
		t.Skip("build runtime/pi before SDK integration tests")
	}
	cfg := &config.Config{AgentRuntime: config.AgentRuntimeConfig{Engine: "pi", WorkerPath: worker, SessionDir: t.TempDir()}, OpenAI: config.OpenAIConfig{Provider: "openai", APIKey: "fixture-key", Model: "fixture", MaxTotalTokens: 32000, MaxCompletionTokens: 1024}}
	cfg.Agent.MaxIterations = 8
	cfg.MultiAgent.EinoSkills.Disable = true
	return cfg
}

func piReply(w http.ResponseWriter, tool, id string, args map[string]any, text string) {
	w.Header().Set("Content-Type", "text/event-stream")
	delta := map[string]any{"role": "assistant"}
	reason := "stop"
	if tool != "" {
		body, _ := json.Marshal(args)
		delta["tool_calls"] = []any{map[string]any{"index": 0, "id": id, "type": "function", "function": map[string]any{"name": tool, "arguments": string(body)}}}
		reason = "tool_calls"
	} else {
		delta["content"] = text
	}
	for _, choice := range []map[string]any{{"index": 0, "delta": delta, "finish_reason": nil}, {"index": 0, "delta": map[string]any{}, "finish_reason": reason}} {
		b, _ := json.Marshal(map[string]any{"id": "fixture", "object": "chat.completion.chunk", "model": "fixture", "choices": []any{choice}, "usage": map[string]any{"prompt_tokens": 20, "completion_tokens": 10, "total_tokens": 30}})
		fmt.Fprintf(w, "data: %s\n\n", b)
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
}

func piTestAgent(cfg *config.Config, handler mcp.ToolHandler) *agent.Agent {
	srv := mcp.NewServer(zap.NewNop())
	schema := map[string]any{"type": "object", "properties": map[string]any{"text": map[string]any{"type": "string"}}, "required": []string{"text"}}
	srv.RegisterTool(mcp.Tool{Name: "echo", Description: "Echo fixture", InputSchema: schema}, handler)
	srv.RegisterTool(mcp.Tool{Name: "secret", Description: "Excluded fixture tool", InputSchema: schema}, func(context.Context, map[string]interface{}) (*mcp.ToolResult, error) {
		return nil, fmt.Errorf("secret must never execute")
	})
	return agent.NewAgent(&cfg.OpenAI, &cfg.Agent, srv, nil, zap.NewNop(), 8)
}

func TestPiSinglePreservesHITLAndExecutionEvidence(t *testing.T) {
	cfg := piTestConfig(t)
	var calls atomic.Int32
	var executed atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if calls.Add(1) == 1 {
			piReply(w, "echo", "echo-1", map[string]any{"text": "original"}, "")
		} else {
			raw, _ := json.Marshal(body)
			if !strings.Contains(string(raw), "approved") {
				t.Error("approved result missing from next model request")
			}
			piReply(w, "", "", nil, "verified")
		}
	}))
	defer server.Close()
	cfg.OpenAI.BaseURL = server.URL + "/v1"
	ag := piTestAgent(cfg, func(_ context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		executed.Add(1)
		if args["text"] != "approved" {
			t.Errorf("unreviewed args: %v", args)
		}
		return &mcp.ToolResult{Content: []mcp.Content{{Type: "text", Text: "approved"}}}, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	ctx = WithHITLToolInterceptor(ctx, func(context.Context, string, string) (string, error) { return `{"text":"approved"}`, nil })
	res, err := RunSingleAgent(ctx, cfg, &cfg.MultiAgent, ag, nil, zap.NewNop(), "single-fixture", "", "echo", nil, []string{"echo"}, nil, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Response != "verified" || executed.Load() != 1 || len(res.MCPExecutionIDs) != 1 {
		t.Fatalf("result=%+v executions=%d", res, executed.Load())
	}
}

func TestPiMultiUsesNativeChildAndRestrictsItsTools(t *testing.T) {
	cfg := piTestConfig(t)
	cfg.MultiAgent.SubAgents = []config.MultiAgentSubConfig{{ID: "specialist", Description: "Fixture specialist", Instruction: "Return observed evidence"}}
	var calls atomic.Int32
	var executions atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Tools []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
			Messages []map[string]any `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		for _, tool := range body.Tools {
			if tool.Function.Name == "secret" {
				t.Error("child expanded the parent tool catalog")
			}
		}
		switch calls.Add(1) {
		case 1:
			piReply(w, "task", "task-1", map[string]any{"subagent_type": "specialist", "description": "echo evidence"}, "")
		case 2:
			raw, _ := json.Marshal(body.Messages)
			if !strings.Contains(string(raw), "Return observed evidence") {
				t.Error("child did not receive its specialist instructions")
			}
			piReply(w, "echo", "echo-2", map[string]any{"text": "evidence"}, "")
		case 3:
			piReply(w, "", "", nil, "child evidence")
		case 4:
			piReply(w, "exit", "exit-1", map[string]any{"final_result": "verified multi"}, "")
		default:
			t.Error("unexpected request after exit")
			piReply(w, "", "", nil, "unexpected")
		}
	}))
	defer server.Close()
	cfg.OpenAI.BaseURL = server.URL + "/v1"
	ag := piTestAgent(cfg, func(context.Context, map[string]interface{}) (*mcp.ToolResult, error) {
		executions.Add(1)
		return &mcp.ToolResult{Content: []mcp.Content{{Type: "text", Text: "evidence"}}}, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	res, err := RunMultiAgent(ctx, cfg, &cfg.MultiAgent, ag, nil, zap.NewNop(), "multi-fixture", "", "delegate", nil, []string{"echo"}, nil, "", "deep", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Response != "verified multi" || calls.Load() != 4 || executions.Load() != 1 || len(res.MCPExecutionIDs) != 1 {
		t.Fatalf("result=%+v calls=%d executions=%d", res, calls.Load(), executions.Load())
	}
}
