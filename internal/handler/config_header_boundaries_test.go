package handler

import (
	"context"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/openai"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSavedHeaderProbeRestoresScopedSecret(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-Api-Key")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":"pong"}}]}`))
	}))
	defer srv.Close()
	cfg := &config.Config{AI: config.AIConfig{Channels: map[string]config.AIChannelConfig{"gateway": {APIKey: "dummy", BaseURL: srv.URL, Model: "m", Headers: map[string]string{"X-Api-Key": "review-test-secret"}}}}}
	masked, err := maskedConfigResponse(configSecretSnapshot(cfg))
	if err != nil {
		t.Fatal(err)
	}
	tree := masked.(map[string]interface{})
	hdr := tree["ai"].(map[string]interface{})["channels"].(map[string]interface{})["gateway"].(map[string]interface{})["headers"]
	payload, _ := json.Marshal(map[string]interface{}{"channel_id": "gateway", "base_url": srv.URL, "api_key": "********", "model": "m", "headers": hdr})
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/api/config/test-openai", strings.NewReader(string(payload)))
	c.Request.Header.Set("Content-Type", "application/json")
	h := &ConfigHandler{config: cfg, logger: zap.NewNop()}
	h.TestOpenAI(c)
	if got != "review-test-secret" {
		t.Fatalf("expected stored header, got %q", got)
	}
	t.Log("Saved header restored from matching channel")
}
func TestAuditIndependentProviderDoesNotReceiveMainHeaders(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	main := config.OpenAIConfig{BaseURL: "https://main-provider.invalid", APIKey: "main-key", Model: "m", Headers: map[string]string{"Authorization": "Bearer main-header-secret"}}
	audit := (config.HitlConfig{AuditModel: config.OpenAIConfig{BaseURL: srv.URL, APIKey: "audit-key", Model: "audit"}}).AuditModelEffective(main)
	var out map[string]interface{}
	err := openai.NewClient(&audit, nil, zap.NewNop()).ChatCompletion(context.Background(), map[string]string{"model": "audit"}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if got != "Bearer audit-key" {
		t.Fatalf("unexpected auth %q", got)
	}
	t.Log("Independent audit identity retained")
}
func TestProbeHeaderScopeAndTargetFailures(t *testing.T) {
	stored := config.AIChannelConfig{Provider: "openai", BaseURL: "https://model.invalid/v1", APIKey: "key", Headers: map[string]string{"X-Api-Key": "stored-secret"}}
	h := &ConfigHandler{config: &config.Config{AI: config.AIConfig{Channels: map[string]config.AIChannelConfig{"a": stored}}, OpenAI: stored.ToOpenAIConfig()}, logger: zap.NewNop()}
	base := TestOpenAIRequest{ChannelID: "a", Provider: "openai", BaseURL: stored.BaseURL, APIKey: "key", Headers: map[string]string{"x-api-key": maskedSecret, "X-Label": "label"}}
	got, err := h.resolveProbeHeaders(base)
	if err != nil || got["x-api-key"] != "stored-secret" || got["X-Label"] != "label" {
		t.Fatalf("restore: %v %v", got, err)
	}
	for _, tc := range []struct {
		name string
		edit func(*TestOpenAIRequest)
	}{
		{"unknown channel", func(r *TestOpenAIRequest) { r.ChannelID = "other" }},
		{"unscoped", func(r *TestOpenAIRequest) { r.ChannelID = "" }},
		{"different destination", func(r *TestOpenAIRequest) { r.BaseURL = "https://other.invalid/v1" }},
		{"different path", func(r *TestOpenAIRequest) { r.BaseURL = "https://model.invalid/another" }},
		{"different provider", func(r *TestOpenAIRequest) { r.Provider = "claude" }},
		{"different key", func(r *TestOpenAIRequest) { r.APIKey = "another" }},
		{"unknown scope", func(r *TestOpenAIRequest) { r.CredentialScope = "other" }},
		{"missing saved header", func(r *TestOpenAIRequest) { r.Headers = map[string]string{"Authorization": maskedSecret} }},
		{"partial marker", func(r *TestOpenAIRequest) { r.Headers = map[string]string{"X-Api-Key": "Bearer " + maskedSecret} }},
		{"duplicate case", func(r *TestOpenAIRequest) {
			r.Headers = map[string]string{"X-Api-Key": maskedSecret, "x-api-key": "override"}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := base
			tc.edit(&r)
			if _, err := h.resolveProbeHeaders(r); err == nil {
				t.Fatal("unsafe marker accepted")
			}
		})
	}
	for _, scope := range []string{"openai", "hitlAudit"} {
		r := base
		r.ChannelID = ""
		r.CredentialScope = scope
		got, err = h.resolveProbeHeaders(r)
		if err != nil || got["x-api-key"] != "stored-secret" {
			t.Fatalf("scope %s: %v", scope, err)
		}
	}
	r := base
	r.Headers = map[string]string{"X-Api-Key": "fresh"}
	r.BaseURL = "https://new.invalid"
	got, err = h.resolveProbeHeaders(r)
	if err != nil || got["X-Api-Key"] != "fresh" {
		t.Fatal("fresh credential rejected")
	}
	h.config.Hitl.AuditModel = config.OpenAIConfig{BaseURL: "https://audit.invalid", APIKey: "audit-key"}
	r = base
	r.CredentialScope = "hitlAudit"
	r.BaseURL = "https://audit.invalid"
	r.APIKey = "audit-key"
	if _, err = h.resolveProbeHeaders(r); err == nil {
		t.Fatal("main credential restored for independent audit")
	}
}
func TestProbeHeaderFailureMakesNoRequest(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	defer srv.Close()
	h := &ConfigHandler{config: &config.Config{AI: config.AIConfig{Channels: map[string]config.AIChannelConfig{"a": {APIKey: "key", BaseURL: "https://saved.invalid", Headers: map[string]string{"Authorization": "Bearer secret"}}}}}, logger: zap.NewNop()}
	payload, _ := json.Marshal(TestOpenAIRequest{ChannelID: "a", APIKey: "key", BaseURL: srv.URL, Model: "m", Headers: map[string]string{"Authorization": maskedSecret}})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/test", strings.NewReader(string(payload)))
	c.Request.Header.Set("Content-Type", "application/json")
	h.TestOpenAI(c)
	if w.Code != 400 || calls != 0 {
		t.Fatalf("status=%d outbound=%d", w.Code, calls)
	}
}

func TestAuditHeadersPersistAndRestoreMaskedConfig(t *testing.T) {
	cfg := &config.Config{Hitl: config.HitlConfig{AuditModel: config.OpenAIConfig{Headers: map[string]string{"X-Api-Key": "audit-test-secret"}}}}
	response, err := maskedConfigResponse(configSecretSnapshot(cfg))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	var req UpdateConfigRequest
	if err = json.Unmarshal(raw, &req); err != nil {
		t.Fatal(err)
	}
	if err = restoreConfigRequestSecrets(&req, cfg); err != nil {
		t.Fatal(err)
	}
	var doc yaml.Node
	if err = yaml.Unmarshal([]byte("hitl: {}\n"), &doc); err != nil {
		t.Fatal(err)
	}
	updateHitlConfig(&doc, *req.Hitl)
	raw, err = yaml.Marshal(&doc)
	if err != nil {
		t.Fatal(err)
	}
	var saved config.Config
	if err = yaml.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Hitl.AuditModel.Headers["X-Api-Key"] != "audit-test-secret" {
		t.Fatal("audit header lost on save")
	}
}

func TestPrivateHeaderNamesAreMaskedAndRestored(t *testing.T) {
	cfg := &config.Config{AI: config.AIConfig{Channels: map[string]config.AIChannelConfig{"a": {Headers: map[string]string{"X-Gateway-Token": "private-token", "X-Client-Name": "client-name"}}, "b": {Headers: map[string]string{"X-Gateway-Token": "other-token"}}}}}
	response, err := maskedConfigResponse(configSecretSnapshot(cfg))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(response)
	if strings.Contains(string(raw), "private-token") || strings.Contains(string(raw), "other-token") || strings.Contains(string(raw), "client-name") {
		t.Fatal("custom header disclosed")
	}
	var req UpdateConfigRequest
	if err = json.Unmarshal(raw, &req); err != nil {
		t.Fatal(err)
	}
	// HTTP field casing is not an identity change, but channels must remain isolated.
	ch := req.AI.Channels["a"]
	delete(ch.Headers, "X-Gateway-Token")
	ch.Headers["x-gateway-token"] = maskedSecret
	req.AI.Channels["a"] = ch
	if err = restoreConfigRequestSecrets(&req, cfg); err != nil {
		t.Fatal(err)
	}
	if req.AI.Channels["a"].Headers["x-gateway-token"] != "private-token" || req.AI.Channels["b"].Headers["X-Gateway-Token"] != "other-token" {
		t.Fatal("wrong channel credential")
	}
	if cfg.AI.Channels["a"].Headers["X-Gateway-Token"] != "private-token" {
		t.Fatal("live config mutated")
	}
	for _, headers := range []map[string]interface{}{{"Missing": maskedSecret}, {"X-Gateway-Token": maskedSecret, "x-gateway-token": maskedSecret}} {
		if transformHeaderSecrets(headers, map[string]interface{}{"X-Gateway-Token": "private-token"}, true) == nil {
			t.Fatal("ambiguous or missing header accepted")
		}
	}
}
func TestEmptyAuditHeadersRoundTripPreservesPolicy(t *testing.T) {
	main := config.OpenAIConfig{Headers: map[string]string{"Authorization": "Bearer main"}}
	for _, identity := range []string{"", "https://audit.invalid"} {
		hitl := config.HitlConfig{AuditModel: config.OpenAIConfig{BaseURL: identity, Headers: map[string]string{}}}
		before := hitl.AuditModelEffective(main).Headers["Authorization"]
		var doc yaml.Node
		yaml.Unmarshal([]byte("hitl: {}\n"), &doc)
		updateHitlConfig(&doc, hitl)
		raw, _ := yaml.Marshal(&doc)
		var saved config.Config
		if err := yaml.Unmarshal(raw, &saved); err != nil {
			t.Fatal(err)
		}
		if after := saved.Hitl.AuditModelEffective(main).Headers["Authorization"]; after != before {
			t.Fatalf("round trip changed header policy: %q -> %q", before, after)
		}
	}
}
