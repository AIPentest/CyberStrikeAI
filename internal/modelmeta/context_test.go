package modelmeta

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
)

func TestDetectContextOllamaLoadedRuntime(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/version", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"version": "0.12.0"})
	})
	mux.HandleFunc("/api/ps", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"models": []map[string]interface{}{
				{"name": "qwen3:8b", "model": "qwen3:8b", "context_length": 32768},
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	info := DetectContext(context.Background(), srv.URL+"/v1", "qwen3:8b", "", srv.Client())
	if info.Backend != BackendOllama || info.ContextWindow != 32768 || info.Source != "ollama_ps" || !info.Loaded {
		t.Fatalf("unexpected info: %+v", info)
	}
	if !contains(info.Warnings, WarningOllamaV1IgnoresNumCtx) {
		t.Fatalf("missing ollama v1 warning: %+v", info.Warnings)
	}
}

func TestDetectContextOllamaShowNumCtx(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/version", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"version": "0.12.0"})
	})
	mux.HandleFunc("/api/ps", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"models": []interface{}{}})
	})
	mux.HandleFunc("/api/show", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"parameters": "num_ctx 16384\nstop \"<|endoftext|>\"",
			"model_info": map[string]interface{}{
				"general.architecture": "qwen3",
				"qwen3.context_length": 131072,
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	info := DetectContext(context.Background(), srv.URL+"/v1", "qwen3:8b", "", srv.Client())
	if info.ContextWindow != 16384 || info.Source != "ollama_show_num_ctx" || info.RuntimeUnknown {
		t.Fatalf("unexpected info: %+v", info)
	}
}

func TestDetectContextOllamaShowModelMax(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/version", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"version": "0.12.0"})
	})
	mux.HandleFunc("/api/ps", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"models": []interface{}{}})
	})
	mux.HandleFunc("/api/show", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"parameters": "",
			"model_info": map[string]interface{}{
				"general.architecture": "qwen3",
				"qwen3.context_length": 131072,
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	info := DetectContext(context.Background(), srv.URL+"/v1", "qwen3:8b", "", srv.Client())
	if info.ContextWindow != 131072 || info.Source != "ollama_show_model_max" || !info.RuntimeUnknown {
		t.Fatalf("unexpected info: %+v", info)
	}
	if !contains(info.Warnings, WarningOllamaRuntimeUnknown) {
		t.Fatalf("missing runtime-unknown warning: %+v", info.Warnings)
	}
}

func TestDetectContextLlamaCppParallelSlotsWarns(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/props", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"default_generation_settings": map[string]interface{}{"n_ctx": 32768},
			"total_slots":                 2,
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	info := DetectContext(context.Background(), srv.URL+"/v1", "local-model", "", srv.Client())
	if info.ContextWindow != 32768 || info.RuntimeSlots != 2 {
		t.Fatalf("unexpected info: %+v", info)
	}
	if !contains(info.Warnings, WarningLlamaCppParallelSlots) {
		t.Fatalf("missing parallel-slots warning: %+v", info.Warnings)
	}
}

func TestDetectContextLlamaCppSingleSlotNoWarning(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/props", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"default_generation_settings": map[string]interface{}{"n_ctx": 32768},
			"total_slots":                 1,
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	info := DetectContext(context.Background(), srv.URL+"/v1", "local-model", "", srv.Client())
	if info.RuntimeSlots != 1 {
		t.Fatalf("slots = %d, want 1", info.RuntimeSlots)
	}
	if contains(info.Warnings, WarningLlamaCppParallelSlots) {
		t.Fatalf("unexpected parallel-slots warning: %+v", info.Warnings)
	}
}

func TestDetectContextLlamaCpp(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/props", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"default_generation_settings": map[string]interface{}{
				"n_ctx":  8192,
				"params": map[string]interface{}{"n_ctx": 8192},
			},
			"total_slots": 1,
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	info := DetectContext(context.Background(), srv.URL+"/v1", "local-model", "", srv.Client())
	if info.Backend != BackendLlamaCpp || info.ContextWindow != 8192 || info.Source != "llamacpp_props" || !info.Loaded {
		t.Fatalf("unexpected info: %+v", info)
	}
}

func TestDetectContextLMStudioLoadedInstance(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v0/models", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": []map[string]interface{}{
				{
					"id":                 "qwen/qwen3-8b",
					"type":               "llm",
					"state":              "loaded",
					"max_context_length": 131072,
					"loaded_instances": []map[string]interface{}{
						{"id": "qwen/qwen3-8b", "config": map[string]interface{}{"context_length": 16384}},
					},
				},
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	info := DetectContext(context.Background(), srv.URL+"/v1", "qwen/qwen3-8b", "", srv.Client())
	if info.Backend != BackendLMStudio || info.ContextWindow != 16384 || info.Source != "lmstudio_loaded_instance_config" || !info.Loaded {
		t.Fatalf("unexpected info: %+v", info)
	}
}

func TestDetectContextVLLMModels(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": []map[string]interface{}{
				{"id": "Qwen/Qwen3-8B", "max_model_len": 65536},
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	info := DetectContext(context.Background(), srv.URL+"/v1", "Qwen/Qwen3-8B", "", srv.Client())
	if info.Backend != BackendOpenAICompatible || info.ContextWindow != 65536 || info.Source != "openai_models_max_model_len" {
		t.Fatalf("unexpected info: %+v", info)
	}
}

func TestDetectContextOMLXAndVLLMIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, owner, backend, source string
		status, window               int
	}{
		{"omlx runtime alias", "omlx", BackendOMLX, "omlx_models_status", 200, 32768},
		{"omlx inference key", "omlx", BackendOMLX, "omlx_models_max_model_len", 403, 262144},
		{"vllm", "vllm", BackendVLLM, "openai_models_max_model_len", 404, 262144},
		{"gateway", "gateway", BackendOpenAICompatible, "openai_models_max_model_len", 404, 262144},
	} {
		t.Run(tc.name, func(t *testing.T) {
			statusCalls := 0
			mux := http.NewServeMux()
			mux.HandleFunc("/edge/v1/models", func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer fixture" {
					t.Error("missing credential")
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"id": "alias", "owned_by": tc.owner, "max_model_len": 262144}}})
			})
			mux.HandleFunc("/edge/v1/models/status", func(w http.ResponseWriter, r *http.Request) {
				statusCalls++
				if r.Method != http.MethodGet {
					t.Error("probe must be read-only")
				}
				w.WriteHeader(tc.status)
				_ = json.NewEncoder(w).Encode(map[string]any{"models": []map[string]any{{"id": "mlx-community/model", "model_alias": "alias", "max_context_window": 32768, "loaded": true}}})
			})
			srv := httptest.NewServer(mux)
			defer srv.Close()
			info := DetectContext(context.Background(), srv.URL+"/edge/v1", "alias", "fixture", srv.Client())
			if info.Backend != tc.backend || info.ContextWindow != tc.window || info.Source != tc.source {
				t.Fatalf("unexpected metadata: %+v", info)
			}
			if (statusCalls > 0) != (tc.owner == "omlx") || info.IsLocalBackend() != (tc.owner != "gateway") {
				t.Fatalf("backend identity mismatch: %+v, status probes=%d", info, statusCalls)
			}
		})
	}
}

func TestGenericGatewaySuccessResponsesDoNotIdentifyLocalBackend(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"id": "generic", "context_length": 65536}}})
	}))
	defer srv.Close()
	info := DetectContext(context.Background(), srv.URL+"/v1", "generic", "", srv.Client())
	if info.IsLocalBackend() || info.Backend != BackendOpenAICompatible || info.ContextWindow != 65536 {
		t.Fatalf("generic JSON success misidentified: %+v", info)
	}
}

func TestDetectContextOpenAISingleModelFallback(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": []map[string]interface{}{
				{"id": "Qwen/Qwen3-8B", "max_model_len": 65536},
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	info := DetectContext(context.Background(), srv.URL+"/v1", "local-alias", "", srv.Client())
	if info.ContextWindow != 65536 {
		t.Fatalf("single-model fallback failed: %+v", info)
	}
}

func TestDetectContextUnknown(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()

	info := DetectContext(context.Background(), srv.URL+"/v1", "x", "", srv.Client())
	if info.ContextWindow != 0 || !contains(info.Warnings, WarningContextUnknown) {
		t.Fatalf("unexpected info: %+v", info)
	}
}

func TestDetectContextOllamaBehindPrefix(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/ollama/api/version", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"version": "0.12.0"})
	})
	mux.HandleFunc("/ollama/api/ps", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"models": []map[string]interface{}{
				{"name": "qwen3:8b", "context_length": 65536},
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	info := DetectContext(context.Background(), srv.URL+"/ollama/v1", "qwen3:8b", "", srv.Client())
	if info.Backend != BackendOllama || info.ContextWindow != 65536 || info.Source != "ollama_ps" {
		t.Fatalf("unexpected info behind prefix: %+v", info)
	}
}

func TestDetectContextLlamaCppBehindPrefix(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/llama/props", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"n_ctx": 16384})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	info := DetectContext(context.Background(), srv.URL+"/llama/v1", "local-model", "", srv.Client())
	if info.Backend != BackendLlamaCpp || info.ContextWindow != 16384 {
		t.Fatalf("unexpected info behind prefix: %+v", info)
	}
}

func TestProbeRootsPrefersPrefix(t *testing.T) {
	t.Parallel()
	parsed, err := url.Parse("https://example.com/ollama/v1/")
	if err != nil {
		t.Fatal(err)
	}
	got := probeRoots(parsed)
	want := []string{"https://example.com/ollama", "https://example.com"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("probeRoots() = %#v, want %#v", got, want)
	}
}

func TestModelMatches(t *testing.T) {
	t.Parallel()
	cases := []struct {
		candidate string
		wanted    string
		want      bool
	}{
		{"qwen3:8b", "qwen3:8b", true},
		{"qwen3:latest", "qwen3", true},
		{"qwen/qwen3-8b", "qwen3-8b", true},
		{"Qwen/Qwen3-8B", "qwen/qwen3-8b", true},
		{"other", "qwen3", false},
	}
	for _, tc := range cases {
		if got := modelMatches(tc.candidate, tc.wanted); got != tc.want {
			t.Fatalf("modelMatches(%q,%q) = %v, want %v", tc.candidate, tc.wanted, got, tc.want)
		}
	}
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
