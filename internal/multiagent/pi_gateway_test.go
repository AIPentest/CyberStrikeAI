package multiagent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"cyberstrike-ai/internal/config"
	"go.uber.org/zap"
)

func TestPiGatewayStoreCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name, base, provider string
		omit                 bool
	}{
		{"default OpenAI", "", "openai", false},
		{"official OpenAI", "https://api.openai.com/v1", "openai", false},
		{"case insensitive host", "https://API.OPENAI.COM/v1", "openai", false},
		{"custom gateway", "https://gateway.example/v1", "openai_compatible", true},
		{"misleading hostname", "https://api.openai.com.example/v1", "openai", true},
		{"local endpoint", "http://localhost:8000/v1", "openai", true},
		{"DeepSeek", "https://api.deepseek.com/v1", "openai", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := piModelConfig(config.OpenAIConfig{BaseURL: tc.base, Provider: tc.provider, Model: "fixture"}, nil, "")
			value, exists := model.Compat["supportsStore"]
			if tc.omit && (!exists || value != false) {
				t.Fatalf("expected store omission: %v", model.Compat)
			}
			if !tc.omit && exists {
				t.Fatalf("official default changed: %v", model.Compat)
			}
		})
	}
}

// This fixture validates the real Go -> Pi worker -> HTTP path, not just the
// compatibility map. It models a strict gateway; it is not a live vendor test.
func TestPiStrictGatewayOmitsStore(t *testing.T) {
	cfg := piTestConfig(t)
	cfg.OpenAI.Model = "deepseek:fixture"
	cfg.OpenAI.Provider = "openai_compatible"
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		if _, present := body["store"]; present {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":{"message":"request field is not supported by provider"}}`))
			return
		}
		if body["stream"] != true || body["stream_options"] == nil || body["tools"] == nil {
			t.Errorf("streaming, usage or tools lost: %v", body)
		}
		piReply(w, "", "", nil, "ok")
	}))
	defer server.Close()
	cfg.OpenAI.BaseURL = server.URL + "/v1"
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, err := RunSingleAgent(ctx, cfg, &cfg.MultiAgent, piTestAgent(cfg, nil), nil, zap.NewNop(), "", "", "Say ok", nil, nil, nil, nil, "")
	if err != nil || result == nil || result.Response != "ok" || result.RuntimeStatus != "completed" || calls.Load() != 1 {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, calls.Load())
	}
}
