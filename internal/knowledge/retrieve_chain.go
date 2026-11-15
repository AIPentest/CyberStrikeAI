package knowledge

import (
	"context"
	"fmt"
)

type RetrieveChain struct{ retriever *Retriever }

func BuildKnowledgeRetrieveChain(ctx context.Context, r *Retriever) (*RetrieveChain, error) {
	if r == nil {
		return nil, fmt.Errorf("retriever is nil")
	}
	return &RetrieveChain{r}, nil
}
func (r *Retriever) CompileRetrieveChain(ctx context.Context) (*RetrieveChain, error) {
	return BuildKnowledgeRetrieveChain(ctx, r)
}
func (c *RetrieveChain) Invoke(ctx context.Context, query string, opts ...RetrievalOption) ([]*Document, error) {
	return c.retriever.activeRetriever().Retrieve(ctx, query, opts...)
}
