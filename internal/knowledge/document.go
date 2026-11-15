package knowledge

import "context"

// Document is the knowledge-domain record. Retrieval no longer exposes framework types.
type Document struct {
	ID       string         `json:"id"`
	Content  string         `json:"content"`
	MetaData map[string]any `json:"metadata,omitempty"`
}

func (d *Document) WithScore(score float64) *Document {
	if d.MetaData == nil {
		d.MetaData = map[string]any{}
	}
	d.MetaData["_score"] = score
	return d
}
func (d *Document) Score() float64 { v, _ := d.MetaData["_score"].(float64); return v }

type RetrievalOptions struct {
	TopK    *int
	DSLInfo map[string]any
}
type RetrievalOption func(*RetrievalOptions)

func WithTopK(k int) RetrievalOption { return func(o *RetrievalOptions) { o.TopK = &k } }
func WithDSLInfo(d map[string]any) RetrievalOption {
	return func(o *RetrievalOptions) { o.DSLInfo = d }
}
func retrievalOptions(opts ...RetrievalOption) *RetrievalOptions {
	o := &RetrievalOptions{}
	for _, f := range opts {
		f(o)
	}
	return o
}

type DocumentRetriever interface {
	Retrieve(context.Context, string, ...RetrievalOption) ([]*Document, error)
}
type DocumentTransformer interface {
	Transform(context.Context, []*Document) ([]*Document, error)
}
type EmbeddingOption struct{}
type TextEmbedder interface {
	EmbedStrings(context.Context, []string, ...EmbeddingOption) ([][]float64, error)
}
type IndexOptions struct {
	Embedding  TextEmbedder
	SubIndexes []string
}
type IndexOption func(*IndexOptions)

func WithEmbedding(e TextEmbedder) IndexOption { return func(o *IndexOptions) { o.Embedding = e } }
func WithSubIndexes(s []string) IndexOption    { return func(o *IndexOptions) { o.SubIndexes = s } }
func indexOptions(opts ...IndexOption) *IndexOptions {
	o := &IndexOptions{}
	for _, f := range opts {
		f(o)
	}
	return o
}
