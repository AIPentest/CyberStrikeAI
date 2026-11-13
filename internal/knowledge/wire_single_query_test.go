package knowledge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"

	"go.uber.org/zap"
)

func TestWireSingleQueryPreservesFiltersAndSkipsRewrite(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Errorf("unexpected model request to %s", r.URL.Path)
			http.Error(w, "unexpected model request", http.StatusBadRequest)
			return
		}
		var body struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Input) != 1 {
			t.Errorf("expected one embedding input, got %v (%v)", body.Input, err)
			http.Error(w, "invalid embedding request", http.StatusBadRequest)
			return
		}
		vector := []float64{1, 0}
		if strings.Contains(body.Input[0], "muffin") {
			vector = []float64{0, 1}
		}
		response := map[string]any{"data": []map[string]any{{"index": 0, "embedding": vector}}}
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()

	logger := zap.NewNop()
	db, err := database.NewKnowledgeDB(filepath.Join(t.TempDir(), "knowledge.db"), logger)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := EnsureKnowledgeEmbeddingsSchema(db.DB); err != nil {
		t.Fatal(err)
	}
	for _, category := range []string{"documentation", "operations"} {
		_, err := db.Exec(`INSERT INTO knowledge_base_items(id,category,title,file_path,content,created_at,updated_at)
VALUES (?,?,?,?,?,datetime('now'),datetime('now'))`, category, category, category, category+".md", "Backup policy")
		if err != nil {
			t.Fatal(err)
		}
		_, err = db.Exec(`INSERT INTO knowledge_embeddings(id,item_id,chunk_index,chunk_text,embedding,embedding_model,embedding_dim,created_at)
VALUES (?,?,0,?,'[1,0]','test-embedding',2,datetime('now'))`, category+"-chunk", category, "Backup policy")
		if err != nil {
			t.Fatal(err)
		}
	}

	ctx := context.Background()
	baseURL := server.URL + "/v1"
	kc := &config.KnowledgeConfig{Embedding: config.EmbeddingConfig{Model: "test-embedding", BaseURL: baseURL, APIKey: "fixture-key"}}
	embedder, err := NewEmbedder(ctx, kc, nil, logger)
	if err != nil {
		t.Fatal(err)
	}
	rc := &RetrievalConfig{
		TopK:                5,
		SimilarityThreshold: 0.4,
		MultiQuery:          config.MultiQueryConfig{MaxQueries: 1},
		Rerank:              config.RerankConfig{Provider: "none"},
	}
	r := NewRetriever(db.DB, embedder, rc, logger)
	oa := &config.OpenAIConfig{Model: "fixture-model", APIKey: "fixture-key", BaseURL: baseURL}
	if err := WireRetrieverPipeline(ctx, r, oa); err != nil {
		t.Fatal(err)
	}
	pipeline, ok := r.pipeline.(*knowledgePipelineRetriever)
	if !ok {
		t.Fatalf("single-query pipeline has type %T", r.pipeline)
	}
	if _, ok := pipeline.inner.(*VectorRetriever); !ok {
		t.Fatalf("single-query pipeline uses %T, want *VectorRetriever", pipeline.inner)
	}

	results, err := r.Search(ctx, &SearchRequest{Query: "backup", RiskType: "documentation"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Item.Category != "documentation" {
		t.Fatalf("category filter returned %v", results)
	}
	results, err = r.Search(ctx, &SearchRequest{Query: "muffin recipe"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("irrelevant query returned %d hits", len(results))
	}
}
