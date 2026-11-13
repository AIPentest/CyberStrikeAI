package knowledge

import (
	"testing"

	"cyberstrike-ai/internal/config"
)

func doc(id, content string, score float64) *Document {
	d := &Document{ID: id, Content: content, MetaData: map[string]any{metaKBItemID: "it1"}}
	d.WithScore(score)
	return d
}

func TestDedupeByNormalizedContent(t *testing.T) {
	a := doc("1", "hello   world", 0.9)
	b := doc("2", "hello world", 0.8)
	c := doc("3", "other", 0.7)
	out := dedupeByNormalizedContent([]*Document{a, b, c})
	if len(out) != 2 {
		t.Fatalf("len=%d want 2", len(out))
	}
	if out[0].ID != "1" || out[1].ID != "3" {
		t.Fatalf("order/ids wrong: %#v", out)
	}
}

func TestEffectivePrefetchTopK(t *testing.T) {
	if g := EffectivePrefetchTopK(5, nil); g != 20 {
		t.Fatalf("default prefetch got %d want 20", g)
	}
	if g := EffectivePrefetchTopK(5, &config.PostRetrieveConfig{PrefetchTopK: 50}); g != 50 {
		t.Fatalf("got %d", g)
	}
	if g := EffectivePrefetchTopK(5, &config.PostRetrieveConfig{PrefetchTopK: 9999}); g != postRetrieveMaxPrefetchCap {
		t.Fatalf("cap: got %d", g)
	}
}

func TestApplyPostRetrieveTruncateAndTopK(t *testing.T) {
	d1 := doc("1", "ab", 0.9)
	d2 := doc("2", "cd", 0.8)
	d3 := doc("3", "ef", 0.7)
	po := &config.PostRetrieveConfig{MaxContextChars: 3}
	out, err := ApplyPostRetrieve([]*Document{d1, d2, d3}, po, "gpt-4", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].ID != "1" {
		t.Fatalf("got %#v", out)
	}

	out2, err := ApplyPostRetrieve([]*Document{d1, d2, d3}, nil, "gpt-4", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(out2) != 2 {
		t.Fatalf("topk: len=%d", len(out2))
	}
}
