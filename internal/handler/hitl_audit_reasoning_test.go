package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"cyberstrike-ai/internal/config"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

func TestAuditAgentRequestCompatibility(t *testing.T) {
	cases := []struct {
		name, model, profile, mainProfile string
		wantThinking, fail                bool
	}{
		{name: "glm", model: "tencent:glm-5.3"},
		{name: "deepseek", model: "deepseek-chat", wantThinking: true},
		{name: "qwen", model: "qwen-plus", wantThinking: true},
		{name: "unknown", model: "other"},
		{name: "explicit deepseek", model: "glm-5", profile: "deepseek_compat", wantThinking: true},
		{name: "explicit openai", model: "deepseek-chat", profile: "openai_compat"},
		{name: "isolated main profile", model: "glm-5", mainProfile: "deepseek_compat"},
		{name: "failure is rejected", model: "glm-5", fail: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				thinking, present := body["thinking"]
				if present != tc.wantThinking {
					t.Errorf("thinking present=%v, want %v", present, tc.wantThinking)
				}
				if present {
					v, ok := thinking.(map[string]any)
					if !ok || v["type"] != "disabled" {
						t.Errorf("thinking=%v", thinking)
					}
				}
				if body["max_completion_tokens"] != float64(1024) || body["temperature"] != 0.1 {
					t.Errorf("unexpected request limits: %v", body)
				}
				if tc.fail {
					w.WriteHeader(400)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"decision\":\"approve\"}"}}]}`))
			}))
			defer server.Close()
			h := &AgentHandler{logger: zap.NewNop(), config: &config.Config{OpenAI: config.OpenAIConfig{APIKey: "test", Model: "main", Reasoning: config.OpenAIReasoningConfig{Profile: tc.mainProfile}}, Hitl: config.HitlConfig{AuditModel: config.OpenAIConfig{BaseURL: server.URL, Model: tc.model, Reasoning: config.OpenAIReasoningConfig{Profile: tc.profile}}}}}
			decision := h.auditAgentReview(context.Background(), "approval", "test", nil)
			want := "approve"
			if tc.fail {
				want = "reject"
			}
			if decision.Decision != want || calls != 1 {
				t.Fatalf("decision=%+v calls=%d", decision, calls)
			}
		})
	}
}

func TestAuditThinkingDetectionAddressAndPrecedence(t *testing.T) {
	for _, tc := range []struct {
		model, url, profile string
		want                bool
	}{
		{"other", "https://deepseek.example/v1", "", true},
		{"other", "https://qwen.example/v1", "", true},
		{"glm-5", "https://deepseek.example/v1", "", false},
		{"deepseek", "https://example/v1", "openai_compat", false},
	} {
		got := auditDisableThinking(config.OpenAIConfig{Model: tc.model, BaseURL: tc.url, Reasoning: config.OpenAIReasoningConfig{Profile: tc.profile}})
		if got != tc.want {
			t.Errorf("model=%s url=%s profile=%s got %v", tc.model, tc.url, tc.profile, got)
		}
	}
}

func TestAuditProfileConfigPersistence(t *testing.T) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte("hitl:\n  audit_model:\n    reasoning:\n      profile: openai_compat\n"), &doc); err != nil {
		t.Fatal(err)
	}
	updateHitlConfig(&doc, config.HitlConfig{AuditModel: config.OpenAIConfig{Reasoning: config.OpenAIReasoningConfig{Profile: "deepseek_compat"}}})
	raw, err := yaml.Marshal(&doc)
	if err != nil {
		t.Fatal(err)
	}
	var decoded config.Config
	if err := yaml.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if got := decoded.Hitl.AuditModel.Reasoning.Profile; got != "deepseek_compat" {
		t.Fatalf("profile=%q", got)
	}
}
