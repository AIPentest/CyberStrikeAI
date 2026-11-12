package knowledge

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

// BuildKnowledgeRetrieveChain Compile the 'query string' → of the 'document list' Eino Chain (MultiQuery → Vector → Rerank → Post-processing).
func BuildKnowledgeRetrieveChain(ctx context.Context, r *Retriever) (compose.Runnable[string, []*schema.Document], error) {
	if r == nil {
		return nil, fmt.Errorf("retriever is nil")
	}
	ch := compose.NewChain[string, []*schema.Document]()
	ch.AppendRetriever(r.AsEinoRetriever())
	return ch.Compile(ctx)
}

// CompileRetrieveChain equivalent to [BuildKnowledgeRetrieveChain](ctx, r).
func (r *Retriever) CompileRetrieveChain(ctx context.Context) (compose.Runnable[string, []*schema.Document], error) {
	return BuildKnowledgeRetrieveChain(ctx, r)
}
