package knowledge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestNativeSplitterPreservesUnicodeAndMakesProgressWithLargeOverlap(t *testing.T) {
	splitter, err := newKnowledgeSplitter(8, 100, "unknown-tokenizer")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Repeat("中文🙂 evidence。\n", 50)
	docs, err := splitter.Transform(context.Background(), []*Document{{ID: "source", Content: text, MetaData: map[string]any{"source": "fixture"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) < 2 || len(docs) > len([]rune(text)) {
		t.Fatalf("unexpected chunk count: %d", len(docs))
	}
	for _, doc := range docs {
		if !utf8.ValidString(doc.Content) || !strings.Contains(text, doc.Content) || doc.MetaData["source"] != "fixture" {
			t.Fatalf("corrupted chunk: %+v", doc)
		}
	}
}

func TestNativeEmbeddingUsesResponseIndices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-key" {
			t.Error("missing auth")
		}
		json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"index": 1, "embedding": []float64{2, 3}}, {"index": 0, "embedding": []float64{0, 1}}}})
	}))
	defer server.Close()
	e := &httpTextEmbedder{client: server.Client(), endpoint: server.URL, key: "fixture-key", model: "fixture"}
	vec, err := e.EmbedStrings(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if vec[0][0] != 0 || vec[1][0] != 2 {
		t.Fatalf("unordered response: %v", vec)
	}
}

func TestNativeEmbeddingRejectsDuplicateIndices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[{"index":0,"embedding":[1]},{"index":0,"embedding":[2]}]}`))
	}))
	defer server.Close()
	e := &httpTextEmbedder{client: server.Client(), endpoint: server.URL, model: "fixture"}
	if _, err := e.EmbedStrings(context.Background(), []string{"a", "b"}); err == nil {
		t.Fatal("duplicate index accepted")
	}
}
