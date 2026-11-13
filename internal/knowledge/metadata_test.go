package knowledge

import "testing"

func TestFormatQueryEmbeddingText_AlignsWithIndexPrefix(t *testing.T) {
	q := FormatQueryEmbeddingText("XSS", "payload")
	want := FormatEmbeddingInput("XSS", "", "payload")
	if q != want {
		t.Fatalf("query embed text mismatch:\n got: %q\nwant: %q", q, want)
	}
	if FormatQueryEmbeddingText("", "hello") != "hello" {
		t.Fatalf("expected bare query without risk type")
	}
}
