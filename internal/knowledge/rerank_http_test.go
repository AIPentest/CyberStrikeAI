package knowledge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"cyberstrike-ai/internal/config"
)

func TestHTTPReranker_CohereOrder(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/rerank" {
			t.Fatalf("path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{"index": 2, "relevance_score": 0.9},
				{"index": 0, "relevance_score": 0.5},
			},
		})
	}))
	defer srv.Close()

	rr, err := NewHTTPReranker(&config.RerankConfig{
		Provider: "cohere",
		Model:    "rerank-multilingual-v3.0",
		BaseURL:  srv.URL,
		APIKey:   "test-key",
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	docs := []*Document{
		{ID: "a", Content: "alpha"},
		{ID: "b", Content: "beta"},
		{ID: "c", Content: "gamma"},
	}
	out, err := rr.Rerank(context.Background(), "query", docs)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || out[0].ID != "c" || out[1].ID != "a" {
		t.Fatalf("order wrong: %#v", out)
	}
}

func TestHTTPReranker_DashScopeOrder(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"output": map[string]any{
				"results": []map[string]any{
					{"index": 1, "relevance_score": 0.88},
				},
			},
		})
	}))
	defer srv.Close()

	rr, err := NewHTTPReranker(&config.RerankConfig{
		Provider: "dashscope",
		Model:    "gte-rerank",
		BaseURL:  srv.URL,
		APIKey:   "test-key",
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	docs := []*Document{{ID: "a", Content: "a"}, {ID: "b", Content: "b"}}
	out, err := rr.Rerank(context.Background(), "q", docs)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].ID != "b" {
		t.Fatalf("got %#v", out)
	}
}

func TestRerankConfigDefaults(t *testing.T) {
	t.Parallel()
	rc := config.RerankConfig{}
	if rc.ProviderEffective("https://dashscope.aliyuncs.com/x") != "dashscope" {
		t.Fatal("dashscope detect")
	}
	if rc.ModelEffective("dashscope") != "gte-rerank" {
		t.Fatal("dashscope model")
	}
	if rc.ModelEffective("cohere") != "rerank-multilingual-v3.0" {
		t.Fatal("cohere model")
	}
}
