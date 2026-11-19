package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cyberstrike-ai/internal/config"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func runAuditProbe(t *testing.T, cfg *config.Config, body string) (int, map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/config/test-audit-model", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	h := &ConfigHandler{config: cfg, logger: zap.NewNop()}
	h.TestAuditModel(c)
	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return w.Code, result
}

func TestAuditProbeConfiguredProtocolAndInheritance(t *testing.T) {
	for _, tc := range []struct {
		name, backend, provider, body, path, model, key, header string
		inherit                                                 bool
	}{
		{"qwen independent of Claude main", "openai", "openai_compatible", `{"api_key":"********"}`, "/compatible-mode/v1/chat/completions", "qwen4-max", "audit-key", "audit-header", false},
		{"blank key keeps audit credential", "openai", "openai", `{"api_key":""}`, "/compatible-mode/v1/chat/completions", "qwen4-max", "audit-key", "audit-header", false},
		{"per-test overrides", "openai", "openai", `{"api_key":"draft-key","model":"draft-model"}`, "/compatible-mode/v1/chat/completions", "draft-model", "draft-key", "audit-header", false},
		{"default backend inherits main", "", "openai", `{}`, "/v1/chat/completions", "main-model", "main-key", "main-header", true},
		{"blank fields inherit main", "openai", "openai", `{"base_url":"","api_key":"********","model":""}`, "/v1/chat/completions", "main-model", "main-key", "main-header", true},
		{"Claude audit", "openai", "claude", `{}`, "/compatible-mode/v1/messages", "qwen4-max", "audit-key", "audit-header", false},
		{"TypeSafe", "typesafe", "openai", `{}`, "/v1/systemone", "jev-latest", "audit-key", "", false},
		{"Jev alias", " Jev ", "openai", `{"api_key":"********"}`, "/v1/systemone", "jev-latest", "audit-key", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			posts := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodGet {
					_, _ = w.Write([]byte(`{"data":[]}`))
					return
				}
				posts++
				if r.Method != http.MethodPost || r.URL.Path != tc.path {
					t.Errorf("request = %s %s, want POST %s", r.Method, r.URL.Path, tc.path)
				}
				if got := r.Header.Get("X-Audit-Test"); got != tc.header {
					t.Errorf("custom header = %q, want %q", got, tc.header)
				}
				auth := r.Header.Get("Authorization")
				if tc.provider == "claude" {
					auth = "Bearer " + r.Header.Get("X-Api-Key")
				}
				if auth != "Bearer "+tc.key {
					t.Errorf("unexpected authentication credential")
				}
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
				}
				if payload["model"] != tc.model {
					t.Errorf("model = %v, want %s", payload["model"], tc.model)
				}
				if strings.HasSuffix(tc.path, "/systemone") {
					if payload["state"] == nil || payload["questions"] == nil || payload["messages"] != nil {
						t.Errorf("not a Jev request: %v", payload)
					}
					_, _ = w.Write([]byte(`{"model":"jev-latest","answers":{"ok":{"noul":1}}}`))
				} else {
					if payload["messages"] == nil || payload["questions"] != nil {
						t.Errorf("not a chat request: %v", payload)
					}
					if tc.provider == "claude" {
						_, _ = w.Write([]byte(`{"id":"msg-test","model":"qwen4-max","type":"message","role":"assistant","content":[{"type":"text","text":"Hi"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
					} else {
						_ = json.NewEncoder(w).Encode(map[string]any{"id": "chat-test", "model": tc.model, "choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": "Hi"}}}})
					}
				}
			}))
			defer srv.Close()
			cfg := &config.Config{
				OpenAI: config.OpenAIConfig{Provider: "claude", BaseURL: srv.URL + "/v1", Model: "main-model", APIKey: "main-key", Headers: map[string]string{"X-Audit-Test": "main-header"}},
				Hitl: config.HitlConfig{AuditBackend: tc.backend, AuditModel: config.OpenAIConfig{
					Provider: tc.provider, BaseURL: srv.URL + "/compatible-mode/v1/", Model: "qwen4-max", APIKey: "audit-key", Headers: map[string]string{"X-Audit-Test": "audit-header"},
				}},
			}
			if tc.inherit {
				cfg.OpenAI.Provider = "openai"
				cfg.Hitl.AuditModel = config.OpenAIConfig{}
			} else if strings.HasSuffix(tc.path, "/systemone") {
				cfg.Hitl.AuditModel.BaseURL = srv.URL
				cfg.Hitl.AuditModel.Model = ""
			}
			before, _ := json.Marshal(cfg)
			status, result := runAuditProbe(t, cfg, tc.body)
			if status != http.StatusOK || result["success"] != true || result["model"] != tc.model || posts != 1 {
				t.Fatalf("status=%d result=%v posts=%d", status, result, posts)
			}
			after, _ := json.Marshal(cfg)
			if string(after) != string(before) {
				t.Fatal("connection test mutated saved configuration")
			}
		})
	}
}

func TestAuditProbeValidationAndUpstreamErrors(t *testing.T) {
	for _, tc := range []struct {
		name, backend, body, wantError string
		status                         int
	}{
		{"bare host", "openai", `{"base_url":"ws-example.cn-beijing.maas.aliyuncs.com"}`, "https://", 400},
		{"unsupported scheme", "typesafe", `{"base_url":"ftp://example.test"}`, "https://", 400},
		{"missing model", "openai", `{}`, "模型不能为空", 400},
		{"TypeSafe never inherits main key", "typesafe", `{"api_key":"********"}`, "TypeSafe API Key 不能为空", 400},
		{"invalid JSON", "openai", `{`, "无效的请求参数", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{OpenAI: config.OpenAIConfig{APIKey: "main-key"}, Hitl: config.HitlConfig{AuditBackend: tc.backend}}
			status, result := runAuditProbe(t, cfg, tc.body)
			if status != tc.status || !strings.Contains(result["error"].(string), tc.wantError) {
				t.Fatalf("status=%d result=%v", status, result)
			}
		})
	}
	for _, backend := range []string{"openai", "typesafe"} {
		t.Run(backend+" upstream error", func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					_, _ = w.Write([]byte(`{"data":[]}`))
					return
				}
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"invalid credential"}`))
			}))
			defer srv.Close()
			cfg := &config.Config{Hitl: config.HitlConfig{AuditBackend: backend, AuditModel: config.OpenAIConfig{BaseURL: srv.URL, Model: "model", APIKey: "test-key"}}}
			status, result := runAuditProbe(t, cfg, `{}`)
			if status != 200 || result["success"] != false || result["status_code"] != float64(401) || !strings.Contains(result["error"].(string), "invalid credential") {
				t.Fatalf("status=%d result=%v", status, result)
			}
		})
	}
}
