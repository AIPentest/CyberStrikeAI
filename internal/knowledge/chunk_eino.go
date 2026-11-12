package knowledge

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino-ext/components/document/transformer/splitter/markdown"
	"github.com/cloudwego/eino-ext/components/document/transformer/splitter/recursive"
	"github.com/cloudwego/eino/components/document"
	"github.com/pkoukk/tiktoken-go"
)

func tokenizerLenFunc(embeddingModel string) func(string) int {
	fallback := func(s string) int {
		r := []rune(s)
		if len(r) == 0 {
			return 0
		}
		return (len(r) + 3) / 4
	}
	m := strings.TrimSpace(embeddingModel)
	if m == "" {
		return fallback
	}
	tok, err := tiktoken.EncodingForModel(m)
	if err != nil {
		return fallback
	}
	return func(s string) int {
		return len(tok.Encode(s, nil, nil))
	}
}

// newKnowledgeSplitter builds an Eino recursive text splitter. LenFunc uses tiktoken for
// embeddingModel when available, else rune/4 approximation.
func newKnowledgeSplitter(chunkSize, overlap int, embeddingModel string) (document.Transformer, error) {
	if chunkSize <= 0 {
		return nil, fmt.Errorf("chunk size must be positive")
	}
	if overlap < 0 {
		overlap = 0
	}
	return recursive.NewSplitter(context.Background(), &recursive.Config{
		ChunkSize:   chunkSize,
		OverlapSize: overlap,
		LenFunc:     tokenizerLenFunc(embeddingModel),
		Separators: []string{
			"\n\n", "\n## ", "\n### ", "\n#### ", "\n",
			".", "!", "?", ". ", "? ", "! ",
			" ",
		},
	})
}

// newMarkdownHeaderSplitter Eino-ext Markdown Split by title (#-####), Suitable for technical/Markdown knowledge bases.
func newMarkdownHeaderSplitter(ctx context.Context) (document.Transformer, error) {
	return markdown.NewHeaderSplitter(ctx, &markdown.HeaderConfig{
		Headers: map[string]string{
			"#":    "h1",
			"##":   "h2",
			"###":  "h3",
			"####": "h4",
		},
		TrimHeaders: false,
	})
}
