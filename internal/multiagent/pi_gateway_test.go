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
	for _, base := range []string{"", "https://api.openai.com/v1", "https://gateway.example/v1"} {
		for _, tc := range []struct {
			name   string
			fields map[string]interface{}
			omit   bool
		}{
			{"default", nil, false},
			{"explicit false", map[string]interface{}{"store": false}, false},
			{"explicit true", map[string]interface{}{"store": true}, false},
			{"explicit omission", map[string]interface{}{"store": nil}, true},
		} {
			t.Run(base+"/"+tc.name, func(t *testing.T) {
				oa := config.OpenAIConfig{BaseURL: base, Provider: "openai_compatible", Model: "fixture", Reasoning: config.OpenAIReasoningConfig{ExtraRequestFields: tc.fields}}
				model := piModelConfig(oa, nil, "")
				value, exists := model.Compat["supportsStore"]
				if tc.omit {
					if !exists || value != false {
						t.Fatalf("expected omission: %v", model.Compat)
					}
					if _, exists := model.ExtraFields["store"]; exists {
						t.Fatal("null must not reach provider")
					}
					if _, exists := oa.Reasoning.ExtraRequestFields["store"]; !exists {
						t.Fatal("channel config mutated")
					}
				} else {
					if exists {
						t.Fatalf("default compatibility changed: %v", model.Compat)
					}
					if got, exists := model.ExtraFields["store"]; exists != (tc.fields != nil) || got != tc.fields["store"] {
						t.Fatalf("explicit field changed: %v", model.ExtraFields)
					}
				}
			})
		}
	}
}

// This fixture validates the real Go -> Pi worker -> HTTP path, not just the
// compatibility map. It models a strict gateway; it is not a live vendor test.
func TestPiGatewayStoreHTTP(t *testing.T) {
	for _, tc := range []struct {
		name    string
		fields  map[string]interface{}
		want    any
		present bool
	}{
		{"default", nil, false, true},
		{"explicit false", map[string]interface{}{"store": false}, false, true},
		{"explicit true", map[string]interface{}{"store": true}, true, true},
		{"strict gateway omission", map[string]interface{}{"store": nil}, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := piTestConfig(t)
			cfg.OpenAI.Model = "deepseek:fixture"
			cfg.OpenAI.Provider = "openai_compatible"
			cfg.OpenAI.Reasoning.ExtraRequestFields = tc.fields
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				if value, present := body["store"]; present != tc.present || value != tc.want {
					t.Errorf("store=%v present=%v, want %v present=%v", value, present, tc.want, tc.present)
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

		})
	}
}

func TestPiStoreOmissionDoesNotChangeOtherAPIs(t *testing.T) {
	for _, oa := range []config.OpenAIConfig{
		{Provider: "claude", BaseURL: "https://api.anthropic.com", Model: "fixture"},
		{Provider: "openai", BaseURL: "https://api.openai.com/v1", Model: "gpt-6-astra"},
	} {
		oa.Reasoning.ExtraRequestFields = map[string]interface{}{"store": nil}
		model := piModelConfig(oa, nil, "")
		if model.API == "openai-completions" {
			t.Fatal("fixture did not select other API")
		}
		if _, ok := model.Compat["supportsStore"]; ok {
			t.Fatalf("other API modified: %v", model)
		}
		if value, ok := model.ExtraFields["store"]; !ok || value != nil {
			t.Fatal("other API extension changed")
		}
	}
}
