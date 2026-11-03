package multiagent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/config"
)

func TestBackgroundCompletionPreservesToolsReasoningAndUsage(t *testing.T) {
	for _, provider := range []string{"openai", "claude"} {
		t.Run(provider, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				tools, ok := body["tools"].([]any)
				if !ok || len(tools) != 1 {
					t.Errorf("tool definitions lost: %v", body["tools"])
				}
				w.Header().Set("Content-Type", "application/json")
				if provider == "claude" {
					if r.URL.Path != "/v1/messages" {
						t.Errorf("native path %s", r.URL.Path)
					}
					w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-test","content":[{"type":"tool_use","id":"call-1","name":"inspect","input":{"target":"fixture"}}],"stop_reason":"tool_use","usage":{"input_tokens":20,"output_tokens":10}}`))
				} else {
					if r.URL.Path != "/chat/completions" {
						t.Errorf("chat path %s", r.URL.Path)
					}
					w.Write([]byte(`{"id":"chat_1","object":"chat.completion","model":"fixture-model","choices":[{"index":0,"message":{"role":"assistant","content":"","reasoning_content":"reason","tool_calls":[{"id":"call-1","type":"function","function":{"name":"inspect","arguments":"{\"target\":\"fixture\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":20,"completion_tokens":10,"total_tokens":30}}`))
				}
			}))
			defer server.Close()
			cfg := config.OpenAIConfig{Provider: provider, APIKey: "fixture-not-a-secret", BaseURL: server.URL, Model: "fixture-model"}
			if provider == "claude" {
				cfg.Model = "claude-test"
			}
			tools := []agent.Tool{{Type: "function", Function: agent.FunctionDefinition{Name: "inspect", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"target": map[string]interface{}{"type": "string"}}}}}}
			message, tokens, err := CompleteBackgroundStep(context.Background(), cfg, []agent.ChatMessage{{Role: "user", Content: "inspect fixture"}}, tools, 512, nil)
			if err != nil {
				t.Fatal(err)
			}
			if tokens != 30 || len(message.ToolCalls) != 1 || message.ToolCalls[0].Function.Arguments["target"] != "fixture" {
				t.Fatalf("tool/usage lost: %+v tokens=%d", message, tokens)
			}
			if provider == "openai" && message.ReasoningContent != "reason" {
				t.Fatal("reasoning lost")
			}
		})
	}
}
