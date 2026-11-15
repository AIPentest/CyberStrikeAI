package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/modelmeta"

	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"
)

func TestDetectedContextSettingsRoundTripThroughSavedYAML(t *testing.T) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte("ai:\n  default_channel: local\n  channels:\n    local:\n      model: fixture\n      context_profile: custom\n      context_window: 131072\n"), &doc); err != nil {
		t.Fatal(err)
	}
	for _, profile := range []string{"local_auto", "custom", ""} {
		ch := config.AIChannelConfig{Model: "fixture", ContextWindow: 32768, ContextProfile: profile}
		updateAIConfig(&doc, config.AIConfig{DefaultChannel: "local", Channels: map[string]config.AIChannelConfig{"local": ch}})
		raw, err := yaml.Marshal(&doc)
		if err != nil {
			t.Fatal(err)
		}
		var saved config.Config
		if err := yaml.Unmarshal(raw, &saved); err != nil {
			t.Fatal(err)
		}
		oa := saved.AI.Channels["local"].ToOpenAIConfig()
		if oa.ContextWindow != 32768 || oa.ContextProfile != profile || oa.Model != "fixture" {
			t.Fatalf("context settings lost on save: %+v", oa)
		}
	}
}

func TestAddModelContextToResponseLocalProfilePreview(t *testing.T) {
	t.Parallel()
	resp := gin.H{}
	addModelContextToResponse(resp, modelmeta.ContextInfo{
		Backend:       modelmeta.BackendOllama,
		ContextWindow: 131072,
		Source:        "ollama_ps",
		Loaded:        true,
		RuntimeSlots:  2,
	}, 120000, 0, "local")

	if got := resp["context_window"]; got != 131072 {
		t.Fatalf("context_window = %#v, want 131072", got)
	}
	if got := resp["context_slots"]; got != 2 {
		t.Fatalf("context_slots = %#v, want 2", got)
	}
	if got := resp["effective_max_total_tokens"]; got != 78643 {
		t.Fatalf("effective_max_total_tokens = %#v, want 78643", got)
	}
	warnings, ok := resp["context_warnings"].([]string)
	if !ok {
		t.Fatalf("context_warnings type = %T", resp["context_warnings"])
	}
	if !containsWarning(warnings, "max_total_tokens_clamped") {
		t.Fatalf("missing clamp warning: %#v", warnings)
	}
	if countWarning(warnings, "max_total_tokens_clamped") != 1 {
		t.Fatalf("clamp warning duplicated: %#v", warnings)
	}
}

func TestAddModelContextToResponseLocalUnknownWindowAssumes32K(t *testing.T) {
	t.Parallel()
	resp := gin.H{}
	addModelContextToResponse(resp, modelmeta.ContextInfo{}, 120000, 0, "local")
	if got := resp["effective_max_total_tokens"]; got != 16384 {
		t.Fatalf("effective_max_total_tokens = %#v, want 16384", got)
	}
	warnings := resp["context_warnings"].([]string)
	if !containsWarning(warnings, "local_context_window_unknown") {
		t.Fatalf("missing unknown-window warning: %#v", warnings)
	}
}

func TestAddModelContextToResponseHonorsOutputCap(t *testing.T) {
	t.Parallel()
	resp := gin.H{}
	addModelContextToResponse(resp, modelmeta.ContextInfo{ContextWindow: 32768}, 120000, 4096, "local")
	if got := resp["effective_max_total_tokens"]; got != 19660 {
		t.Fatalf("effective_max_total_tokens = %#v, want 19660", got)
	}
}

func TestAddModelContextToResponseAutoProfileKeepsNoPreviewWithoutWindow(t *testing.T) {
	t.Parallel()
	resp := gin.H{}
	addModelContextToResponse(resp, modelmeta.ContextInfo{}, 120000, 0, "auto")
	if _, ok := resp["effective_max_total_tokens"]; ok {
		t.Fatalf("auto profile without window should not preview a budget: %#v", resp)
	}
}

func TestDetectedBackendAutomaticallySelectsLocalProfile(t *testing.T) {
	for _, backend := range []string{modelmeta.BackendOllama, modelmeta.BackendLlamaCpp, modelmeta.BackendLMStudio, modelmeta.BackendOMLX, modelmeta.BackendVLLM} {
		resp := gin.H{}
		addModelContextToResponse(resp, modelmeta.ContextInfo{Backend: backend, ContextWindow: 65536}, 120000, 8192, "auto")
		if resp["context_profile"] != "local_auto" || resp["effective_max_total_tokens"] != 39321 {
			t.Fatalf("%s: %+v", backend, resp)
		}
	}
	for _, profile := range []string{"custom", "economy", "local"} {
		resp := gin.H{}
		addModelContextToResponse(resp, modelmeta.ContextInfo{Backend: modelmeta.BackendOMLX, ContextWindow: 65536}, 120000, 8192, profile)
		if resp["context_profile"] != profile {
			t.Fatalf("explicit profile changed: %+v", resp)
		}
	}
	resp := gin.H{}
	addModelContextToResponse(resp, modelmeta.ContextInfo{Backend: modelmeta.BackendOpenAICompatible, ContextWindow: 131072, Source: "openai_models_context_length"}, 120000, 8192, "local_auto")
	if resp["context_profile"] != "" {
		t.Fatalf("generic gateway was treated as local: %+v", resp)
	}
	resp = gin.H{}
	addModelContextToResponse(resp, modelmeta.ContextInfo{Backend: modelmeta.BackendOllama, ContextWindow: 262144, RuntimeUnknown: true}, 120000, 8192, "auto")
	if resp["effective_max_total_tokens"] != 19660 {
		t.Fatalf("architecture limit treated as runtime limit: %+v", resp)
	}
}

func TestAddModelContextToResponseDedupesWarnings(t *testing.T) {
	t.Parallel()
	resp := gin.H{}
	addModelContextToResponse(resp, modelmeta.ContextInfo{
		ContextWindow: 32768,
		Warnings:      []string{modelmeta.WarningContextUnknown, modelmeta.WarningContextUnknown},
	}, 64000, 0, "auto")
	warnings := resp["context_warnings"].([]string)
	if countWarning(warnings, modelmeta.WarningContextUnknown) != 1 {
		t.Fatalf("warnings not deduped: %#v", warnings)
	}
	if !containsWarning(warnings, modelmeta.WarningBudgetExceedsWindow) {
		t.Fatalf("missing budget-exceeds-window warning: %#v", warnings)
	}
}

func containsWarning(warnings []string, want string) bool {
	for _, code := range warnings {
		if code == want {
			return true
		}
	}
	return false
}

func countWarning(warnings []string, want string) int {
	count := 0
	for _, code := range warnings {
		if code == want {
			count++
		}
	}
	return count
}

func TestContextProbePreservesCustomHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Gateway-Key") != "test-gateway" || r.Header.Get("X-Session") != "connect-test" {
			t.Errorf("context probe omitted resolved gateway/session headers")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/props" {
			w.Write([]byte(`{"default_generation_settings":{"n_ctx":32768}}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	info := probeModelContext(context.Background(), "openai", server.URL+"/v1", "test-key", "test-model", map[string]string{"X-Gateway-Key": "test-gateway", "X-Session": "%session_id%"})
	if info.ContextWindow != 32768 {
		t.Fatalf("context window = %d", info.ContextWindow)
	}
}

func TestContextProbeDoesNotRedirectCredentials(t *testing.T) {
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("context probe followed credential-bearing redirect")
	}))
	defer destination.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, http.StatusFound) }))
	defer source.Close()
	probeModelContext(context.Background(), "openai", source.URL+"/v1", "test-key", "test-model", map[string]string{"X-Gateway-Key": "test-gateway"})
}
