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
