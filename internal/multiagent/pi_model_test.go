package multiagent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cyberstrike-ai/internal/agentruntime"
	"cyberstrike-ai/internal/config"
	"go.uber.org/zap"
)

func TestPiModelInstructionRoleCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name, provider, url, profile, model, api string
		systemOnly                               bool
	}{
		{"gateway", "openai", "https://gateway.example/v1", "auto", "fixture", "openai-completions", true},
		{"deepseek gateway", "openai", "https://gateway.example/v1", "deepseek_compat", "fixture", "openai-completions", true},
		{"local", "openai_compatible", "http://localhost:8000/v1", "auto", "fixture", "openai-completions", true},
		{"official", "openai", "https://api.openai.com/v1/", "auto", "o3", "openai-completions", false},
		{"default endpoint", "openai", "", "auto", "o3", "openai-completions", false},
		{"official with deepseek profile", "openai", "https://api.openai.com/v1", "deepseek_compat", "fixture", "openai-completions", true},
		{"responses", "openai", "https://api.openai.com/v1", "auto", "gpt-6.1-sol", "openai-responses", false},
		{"anthropic", "claude", "https://api.anthropic.com", "auto", "fixture", "anthropic-messages", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			oa := config.OpenAIConfig{Provider: tc.provider, BaseURL: tc.url, Model: tc.model,
				Reasoning: config.OpenAIReasoningConfig{Mode: "on", Effort: "low", Profile: tc.profile}}
			model := piModelConfig(oa, nil, "")
			if model.API != tc.api || model.Thinking != "low" {
				t.Fatalf("unexpected API or thinking: %+v", model)
			}
			value, set := model.Compat["supportsDeveloperRole"]
			if tc.systemOnly && (!set || value != false) {
				t.Fatalf("compatible endpoint must explicitly use system messages: %+v", model.Compat)
			}
			if !tc.systemOnly && set {
				t.Fatalf("native protocol compatibility must remain SDK-controlled: %+v", model.Compat)
			}
		})
	}
}

func TestPiCompatibleReasoningUsesSystemMessages(t *testing.T) {
	for _, profile := range []string{"auto", "deepseek_compat"} {
		for _, background := range []bool{false, true} {
			name := profile + "/agent"
			if background {
				name = profile + "/background"
			}
			t.Run(name, func(t *testing.T) {
				cfg := piTestConfig(t)
				cfg.OpenAI.Reasoning = config.OpenAIReasoningConfig{Mode: "on", Effort: "low", Profile: profile}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var body struct {
						Messages []struct {
							Role    string
							Content json.RawMessage
						}
						Effort   string `json:"reasoning_effort"`
						Thinking struct{ Type string }
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
						http.Error(w, "invalid request", http.StatusBadRequest)
						return
					}
					for _, message := range body.Messages {
						if message.Role == "developer" {
							w.Header().Set("Content-Type", "application/json")
							w.WriteHeader(http.StatusBadRequest)
							_, _ = w.Write([]byte(`{"message":"developer is not one of ['system', 'assistant', 'user', 'tool', 'function']","type":"invalid_request_error","param":null,"code":"invalid_parameter_error"}`))
							return
						}
					}
					if len(body.Messages) == 0 || body.Messages[0].Role != "system" || !strings.Contains(string(body.Messages[0].Content), "instruction-role fixture") {
						t.Errorf("system instruction missing: %+v", body.Messages)
					}
					if body.Effort != "low" || (profile == "deepseek_compat" && body.Thinking.Type != "enabled") {
						t.Errorf("reasoning was not preserved: %+v", body)
					}
					piReply(w, "", "", nil, "role verified")
				}))
				defer server.Close()
				cfg.OpenAI.BaseURL = server.URL + "/v1"
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				if background {
					result, err := (agentruntime.Pi{WorkerPath: cfg.AgentRuntime.WorkerPath}).Run(ctx, agentruntime.Request{
						RunID: "instruction-role", GenerateOnly: true, SystemPrompt: "instruction-role fixture",
						History: json.RawMessage(`[{"role":"user","content":"Say ok"}]`),
						Model:   piModelConfig(cfg.OpenAI, nil, ""), Tools: []agentruntime.Tool{}, MaxTurns: 1,
					}, nil, nil, nil)
					if err != nil || result == nil || result.Response != "role verified" || result.Status != "completed" {
						t.Fatalf("result=%+v err=%v", result, err)
					}
				} else {
					result, err := RunSingleAgent(ctx, cfg, &cfg.MultiAgent, piTestAgent(cfg, nil), nil, zap.NewNop(), "", "", "Say ok", nil, nil, nil, nil, "instruction-role fixture")
					if err != nil || result == nil || result.Response != "role verified" || result.RuntimeStatus != "completed" {
						t.Fatalf("result=%+v err=%v", result, err)
					}
				}
			})
		}
	}
}
