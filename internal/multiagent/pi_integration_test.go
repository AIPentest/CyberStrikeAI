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
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/authctx"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/mcp/builtin"
	"go.uber.org/zap"
)

func TestPiDeleteTurnExcludesDeletedContentFromNextModelRequest(t *testing.T) {
	for _, deletedIndex := range []int{0, 1, 2} {
		t.Run(fmt.Sprint(deletedIndex), func(t *testing.T) {
			cfg := piTestConfig(t)
			db, err := database.NewDB(filepath.Join(t.TempDir(), "sessions.db"), zap.NewNop())
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			conv, err := db.CreateConversation("deletion fixture", database.ConversationCreateMeta{})
			if err != nil {
				t.Fatal(err)
			}
			var requestsMu sync.Mutex
			var requests []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				var body struct {
					Messages []map[string]any `json:"messages"`
				}
				if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				raw, _ := json.Marshal(body.Messages)
				requestsMu.Lock()
				defer requestsMu.Unlock()
				requests = append(requests, string(raw))
				piReply(w, "", "", nil, fmt.Sprintf("reply-marker-%d", len(requests)-1))
			}))
			defer server.Close()
			cfg.OpenAI.BaseURL = server.URL + "/v1"
			ag := piTestAgent(cfg, nil)
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			var anchors []string
			for i := 0; i < 3; i++ {
				message := fmt.Sprintf("user-marker-%d", i)
				msg, err := db.AddMessage(conv.ID, "user", message, nil)
				if err != nil {
					t.Fatal(err)
				}
				anchors = append(anchors, msg.ID)
				res, err := RunSingleAgent(ctx, cfg, &cfg.MultiAgent, ag, db, zap.NewNop(), conv.ID, "", message, nil, []string{"echo"}, nil, nil, "")
				if err != nil {
					t.Fatal(err)
				}
				if res.RuntimeStatus != "completed" {
					t.Fatalf("run failed: %+v", res)
				}
				if _, err := db.AddMessage(conv.ID, "assistant", res.Response, nil); err != nil {
					t.Fatal(err)
				}
			}
			// Preserve the old generation and simulate a late publish after deletion.
			oldFiles := map[string][]byte{}
			if err := filepath.WalkDir(cfg.AgentSessionDir(), func(path string, entry os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !entry.IsDir() {
					oldFiles[path], err = os.ReadFile(path)
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if len(oldFiles) == 0 {
				t.Fatal("no persisted Pi session to invalidate")
			}
			if _, err := db.DeleteConversationTurn(conv.ID, anchors[deletedIndex]); err != nil {
				t.Fatal(err)
			}
			for path, body := range oldFiles {
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, body, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			messages, err := db.GetMessages(conv.ID)
			if err != nil {
				t.Fatal(err)
			}
			var history []agent.ChatMessage
			for _, m := range messages {
				history = append(history, agent.ChatMessage{Role: m.Role, Content: m.Content})
			}
			res, err := RunSingleAgent(ctx, cfg, &cfg.MultiAgent, ag, db, zap.NewNop(), conv.ID, "", "continue", history, []string{"echo"}, nil, nil, "")
			if err != nil {
				t.Fatal(err)
			}
			requestsMu.Lock()
			defer requestsMu.Unlock()
			if res.RuntimeStatus != "completed" || len(requests) != 4 {
				t.Fatalf("result=%+v requests=%d", res, len(requests))
			}
			for i := 0; i < 3; i++ {
				for _, prefix := range []string{"user-marker-", "reply-marker-"} {
					present := strings.Contains(requests[3], fmt.Sprintf("%s%d", prefix, i))
					if present != (i != deletedIndex) {
						t.Fatalf("wrong history after deleting turn %d: %s", deletedIndex, requests[3])
					}
				}
			}
		})
	}
}

func piTestWorkerPath(t *testing.T) string {
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
	return worker
}

func piTestConfig(t *testing.T) *config.Config {
	t.Helper()
	worker := piTestWorkerPath(t)
	cfg := &config.Config{AgentRuntime: config.AgentRuntimeConfig{Engine: "pi", WorkerPath: worker, SessionDir: t.TempDir()}, OpenAI: config.OpenAIConfig{Provider: "openai", APIKey: "fixture-key", Model: "fixture", MaxTotalTokens: 32000, MaxCompletionTokens: 1024}}
	cfg.Agent.MaxIterations = 8
	cfg.MultiAgent.Skills.Disable = true
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

func TestPiDeepSeekOutputBudgetAndUsage(t *testing.T) {
	for _, tc := range []struct {
		profile, mode, thinking, effort string
		reasoning                       int
	}{
		{profile: "deepseek", mode: "auto", thinking: "enabled", effort: "low", reasoning: 6},
		{profile: "deepseek_compat", mode: "off", thinking: "disabled"},
	} {
		t.Run(tc.profile, func(t *testing.T) {
			cfg := piTestConfig(t)
			cfg.OpenAI.Model = "deepseek-flash"
			cfg.OpenAI.Reasoning = config.OpenAIReasoningConfig{Profile: tc.profile, Mode: tc.mode, Effort: "low"}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					MaxTokens           int    `json:"max_tokens"`
					MaxCompletionTokens *int   `json:"max_completion_tokens"`
					Effort              string `json:"reasoning_effort"`
					Thinking            struct {
						Type string `json:"type"`
					} `json:"thinking"`
					StreamOptions struct {
						IncludeUsage bool `json:"include_usage"`
					} `json:"stream_options"`
					Tools []struct {
						Function struct {
							Name string `json:"name"`
						} `json:"function"`
					} `json:"tools"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					http.Error(w, "invalid fixture request", http.StatusBadRequest)
					return
				}
				if body.MaxTokens != 1024 || body.MaxCompletionTokens != nil || !body.StreamOptions.IncludeUsage {
					t.Errorf("unexpected budget or usage options: %+v", body)
				}
				if body.Thinking.Type != tc.thinking || body.Effort != tc.effort {
					t.Errorf("unexpected thinking options: %+v", body)
				}
				var names []string
				for _, tool := range body.Tools {
					names = append(names, tool.Function.Name)
				}
				if !slices.Equal(names, []string{"echo", "secret"}) {
					t.Errorf("unexpected tool catalog: %v", names)
				}
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, `data: {"id":"fixture","object":"chat.completion.chunk","model":"deepseek-flash","choices":[{"index":0,"delta":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`+"\n\n")
				fmt.Fprintf(w, `data: {"id":"fixture","object":"chat.completion.chunk","model":"deepseek-flash","choices":[],"usage":{"prompt_tokens":100,"prompt_cache_hit_tokens":75,"prompt_cache_miss_tokens":25,"completion_tokens":20,"completion_tokens_details":{"reasoning_tokens":%d},"total_tokens":120}}`+"\n\n", tc.reasoning)
				fmt.Fprint(w, "data: [DONE]\n\n")
			}))
			defer server.Close()
			cfg.OpenAI.BaseURL = server.URL + "/v1"
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			var usage map[string]any
			result, err := RunSingleAgent(ctx, cfg, &cfg.MultiAgent, piTestAgent(cfg, nil), nil, zap.NewNop(), "", "", "Say ok", nil, nil, func(kind, _ string, data interface{}) {
				if kind == "agent_usage_summary" {
					usage = data.(map[string]any)
				}
			}, nil, "")
			if err != nil || result == nil || result.Response != "ok" || result.RuntimeStatus != "completed" {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			for field, want := range map[string]int{
				"modelCalls": 1, "promptTokens": 100, "completionTokens": 20,
				"cachedTokens": 75, "cacheWriteTokens": 0, "reasoningTokens": tc.reasoning, "totalTokens": 120,
			} {
				if usage[field] != want {
					t.Errorf("%s=%v, want %d (usage=%v)", field, usage[field], want, usage)
				}
			}
		})
	}
}

func TestPiDeepSeekEconomyDiscoversToolsAndReadsFullEvidence(t *testing.T) {
	// Skip the parent too: its final assertion compares measurements from both subtests.
	piTestWorkerPath(t)
	full := strings.Repeat("fixture output line with details\n", 150) + "MIDDLE_EVIDENCE\n" + strings.Repeat("trailing output line with details\n", 150) + "end"
	var baselineBytes, optimizedBytes int
	for _, profile := range []string{"custom", "auto"} {
		t.Run(profile, func(t *testing.T) {
			cfg := piTestConfig(t)
			cfg.OpenAI.Reasoning.Profile = "deepseek"
			cfg.OpenAI.ContextProfile = profile
			cfg.MultiAgent.Skills.Disable = false
			cfg.MultiAgent.Runtime.ToolSearchEnable = true
			cfg.MultiAgent.Runtime.ReductionRootDir = t.TempDir()
			var calls int
			var originalPath string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				var body struct {
					Tools    json.RawMessage `json:"tools"`
					Messages []struct {
						Role    string
						Content any
					} `json:"messages"`
				}
				if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
					t.Error(err)
					return
				}
				calls++
				if profile == "custom" {
					baselineBytes = len(body.Tools)
					piReply(w, "", "", nil, "baseline")
					return
				}
				switch calls {
				case 1:
					optimizedBytes = len(body.Tools)
					if strings.Contains(string(body.Tools), `"name":"record_vulnerability"`) {
						t.Error("unused builtin schema was exposed before discovery")
					}
					piReply(w, "tool_search", "discover", map[string]any{"query": "record_vulnerability", "limit": 1}, "")
				case 2:
					if !strings.Contains(string(body.Tools), `"name":"record_vulnerability"`) {
						t.Error("discovered tool is unavailable")
					}
					if !strings.Contains(string(body.Tools), `"name":"read_file"`) {
						t.Error("read_file was lost during tool discovery")
					}
					piReply(w, "record_vulnerability", "record", map[string]any{"text": "fixture"}, "")
				case 3:
					preview, _ := body.Messages[len(body.Messages)-1].Content.(string)
					match := regexp.MustCompile(`Full output saved to: ([^\n]+)`).FindStringSubmatch(preview)
					if len(match) != 2 || len(preview) > 8000 || strings.Contains(preview, "MIDDLE_EVIDENCE") {
						t.Errorf("expected recoverable preview, got %d bytes", len(preview))
						piReply(w, "", "", nil, "failed preview")
						return
					}
					originalPath = match[1]
					t.Logf("tool result bytes: full=%d preview=%d", len(full)+1, len(preview))
					piReply(w, "read_file", "read", map[string]any{"path": originalPath, "offset": 151, "limit": 1}, "")
				case 4:
					if got := body.Messages[len(body.Messages)-1].Content; got != "MIDDLE_EVIDENCE" {
						t.Errorf("evidence readback=%q", got)
					}
					piReply(w, "", "", nil, "verified middle evidence")
				default:
					t.Error("unexpected retry")
					piReply(w, "", "", nil, "unexpected")
				}
			}))
			defer server.Close()
			cfg.OpenAI.BaseURL = server.URL + "/v1"
			srv := mcp.NewServer(zap.NewNop())
			srv.ConfigureToolResultSpillRoot(t.TempDir())
			srv.SetToolAuthorizer(func(context.Context, string, map[string]interface{}) error { return nil })
			var executed int
			for _, name := range builtin.GetAllBuiltinTools() {
				srv.RegisterTool(mcp.Tool{Name: name, Description: "Fixture operation " + name,
					InputSchema: piToolSchema(map[string]any{"text": map[string]any{"type": "string", "description": "Fixture evidence text"}}, "text")},
					func(context.Context, map[string]interface{}) (*mcp.ToolResult, error) {
						executed++
						return &mcp.ToolResult{Content: []mcp.Content{{Type: "text", Text: full}}}, nil
					})
			}
			ag := agent.NewAgent(&cfg.OpenAI, &cfg.Agent, srv, nil, zap.NewNop(), 8)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			ctx = authctx.WithPrincipal(ctx, authctx.NewPrincipal("fixture", "fixture", "all", map[string]bool{"agent:local-execute": true}))
			res, err := RunSingleAgent(ctx, cfg, &cfg.MultiAgent, ag, nil, zap.NewNop(), "economy-fixture", "", "Read the evidence", nil, nil, nil, nil, "")
			if err != nil || res == nil || res.RuntimeStatus != "completed" {
				t.Fatalf("result=%+v error=%v", res, err)
			}
			if profile == "auto" {
				if calls != 4 || executed != 1 || res.Response != "verified middle evidence" {
					t.Fatalf("calls=%d executed=%d result=%+v", calls, executed, res)
				}
				b, err := os.ReadFile(originalPath)
				// The host adds a newline after each MCP text block.
				if err != nil || string(b) != full+"\n" {
					t.Fatalf("full evidence changed: err=%v got=%d want=%d", err, len(b), len(full)+1)
				}
			}
		})
	}
	if optimizedBytes >= baselineBytes/2 {
		t.Errorf("tool schema savings too small: custom=%d auto=%d", baselineBytes, optimizedBytes)
	}
	t.Logf("first-request tool schema bytes: custom=%d auto=%d", baselineBytes, optimizedBytes)
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

func TestPiSingleChatAcceptsParameterlessTaskListSchema(t *testing.T) {
	cfg := piTestConfig(t)
	cfg.SkillsDir = t.TempDir()
	cfg.MultiAgent.Runtime.PlantaskEnable = true
	cfg.MultiAgent.Skills = config.AgentSkillsConfig{FilesystemTools: new(bool)}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Tools []struct {
				Function struct {
					Name       string         `json:"name"`
					Parameters map[string]any `json:"parameters"`
				} `json:"function"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		for _, tool := range body.Tools {
			if tool.Function.Name != "TaskList" {
				continue
			}
			if required, exists := tool.Function.Parameters["required"]; exists {
				if names, ok := required.([]any); !ok || len(names) != 0 {
					http.Error(w, "Invalid schema for function 'TaskList': expected an array of required parameters", http.StatusBadRequest)
					return
				}
			}
			piReply(w, "", "", nil, "hello")
			return
		}
		t.Error("TaskList is missing from the request")
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()
	cfg.OpenAI.BaseURL = server.URL + "/v1"
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	ag := piTestAgent(cfg, func(context.Context, map[string]any) (*mcp.ToolResult, error) {
		t.Error("plain chat must not execute the fixture tool")
		return nil, fmt.Errorf("unexpected tool call")
	})
	res, err := RunSingleAgent(ctx, cfg, &cfg.MultiAgent, ag, nil, zap.NewNop(), "schema-fixture", "", "say hello", nil, []string{"echo"}, nil, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.RuntimeStatus != "completed" || res.Response != "hello" {
		t.Fatalf("expected a chat reply, got %+v", res)
	}
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
	if res.Response != "verified" || res.RuntimeStatus != "completed" || executed.Load() != 1 || len(res.MCPExecutionIDs) != 1 {
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

func TestPiRepeatedProviderIDDoesNotSuppressNewToolInvocation(t *testing.T) {
	cfg := piTestConfig(t)
	var requests atomic.Int32
	var executions atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch requests.Add(1) {
		case 1:
			piReply(w, "echo", "reused", map[string]any{"text": "first"}, "")
		case 2:
			piReply(w, "echo", "reused", map[string]any{"text": "second"}, "")
		default:
			piReply(w, "", "", nil, "both completed")
		}
	}))
	defer server.Close()
	cfg.OpenAI.BaseURL = server.URL + "/v1"
	ag := piTestAgent(cfg, func(_ context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		executions.Add(1)
		return &mcp.ToolResult{Content: []mcp.Content{{Type: "text", Text: fmt.Sprint(args["text"])}}}, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	res, err := RunSingleAgent(ctx, cfg, &cfg.MultiAgent, ag, nil, zap.NewNop(), "reused-id", "", "perform both", nil, []string{"echo"}, nil, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if executions.Load() != 2 || len(res.MCPExecutionIDs) != 2 {
		t.Fatalf("invocations incorrectly deduplicated: %+v count=%d", res, executions.Load())
	}
}
