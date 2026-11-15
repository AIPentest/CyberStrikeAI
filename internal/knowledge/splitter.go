package knowledge

import (
	"context"
	"fmt"
	"strings"

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

type nativeSplitter struct {
	size, overlap int
	length        func(string) int
	markdown      bool
}

func newKnowledgeSplitter(size, overlap int, model string) (DocumentTransformer, error) {
	if size <= 0 {
		return nil, fmt.Errorf("chunk size must be positive")
	}
	if overlap < 0 {
		overlap = 0
	}
	if overlap >= size {
		overlap = size / 4
	}
	return &nativeSplitter{size: size, overlap: overlap, length: tokenizerLenFunc(model)}, nil
}
func newMarkdownHeaderSplitter(context.Context) (DocumentTransformer, error) {
	return &nativeSplitter{markdown: true}, nil
}
func cloneDocument(d *Document, content string) *Document {
	meta := map[string]any{}
	for k, v := range d.MetaData {
		meta[k] = v
	}
	return &Document{ID: d.ID, Content: content, MetaData: meta}
}
func (s *nativeSplitter) Transform(ctx context.Context, docs []*Document) ([]*Document, error) {
	var out []*Document
	for _, d := range docs {
		if d == nil {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if s.markdown {
			var section strings.Builder
			for _, line := range strings.SplitAfter(d.Content, "\n") {
				trimmed := strings.TrimLeft(line, " ")
				heading := strings.HasPrefix(trimmed, "# ") || strings.HasPrefix(trimmed, "## ") || strings.HasPrefix(trimmed, "### ") || strings.HasPrefix(trimmed, "#### ")
				if heading && section.Len() > 0 {
					out = append(out, cloneDocument(d, section.String()))
					section.Reset()
				}
				section.WriteString(line)
			}
			if section.Len() > 0 {
				out = append(out, cloneDocument(d, section.String()))
			}
			continue
		}
		runes := []rune(d.Content)
		for start := 0; start < len(runes); {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			low, high := start+1, len(runes)
			for low < high {
				mid := (low + high + 1) / 2
				if s.length(string(runes[start:mid])) <= s.size {
					low = mid
				} else {
					high = mid - 1
				}
			}
			end := low
			if end < len(runes) {
				for j := end - 1; j > start+(end-start)/2; j-- {
					if strings.ContainsRune("\n。！？ ", runes[j]) {
						end = j + 1
						break
					}
				}
			}
			out = append(out, cloneDocument(d, string(runes[start:end])))
			if end == len(runes) {
				break
			}
			next := end
			for next > start+1 && s.length(string(runes[next-1:end])) <= s.overlap {
				next--
			}
			if next <= start {
				next = end
			}
			start = next
		}
	}
	return out, nil
}
