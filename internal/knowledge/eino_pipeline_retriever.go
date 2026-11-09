package knowledge

import (
	"context"
	"fmt"
	"strings"

	"cyberstrike-ai/internal/config"

	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components"
	"github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// knowledgePipelineRetriever: MultiQuery → vector candidates → rerank → post-process.
type knowledgePipelineRetriever struct {
	inner retriever.Retriever
	base  *Retriever
}

func newKnowledgePipelineRetriever(inner retriever.Retriever, base *Retriever) *knowledgePipelineRetriever {
	if inner == nil || base == nil {
		return nil
	}
	return &knowledgePipelineRetriever{inner: inner, base: base}
}

func (p *knowledgePipelineRetriever) GetType() string {
	return "KnowledgeRAGPipeline"
}

func (p *knowledgePipelineRetriever) Retrieve(ctx context.Context, query string, opts ...retriever.Option) (out []*schema.Document, err error) {
	if p == nil || p.inner == nil || p.base == nil {
		return nil, fmt.Errorf("knowledge pipeline retriever: nil")
	}
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, fmt.Errorf("query must not be empty")
	}

	ro := retriever.GetCommonOptions(nil, opts...)
	finalTopK := p.base.config.TopK
	if finalTopK <= 0 {
		finalTopK = 5
	}
	if ro.TopK != nil && *ro.TopK > 0 {
		finalTopK = *ro.TopK
	}

	ctx = callbacks.EnsureRunInfo(ctx, p.GetType(), components.ComponentOfRetriever)
	ctx = callbacks.OnStart(ctx, &retriever.CallbackInput{Query: q, TopK: finalTopK, Extra: ro.DSLInfo})
	defer func() {
		if err != nil {
			_ = callbacks.OnError(ctx, err)
			return
		}
		_ = callbacks.OnEnd(ctx, &retriever.CallbackOutput{Docs: out})
	}()

	out, err = p.inner.Retrieve(ctx, q, opts...)
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return out, nil
	}

	if rr := p.base.documentReranker(); rr != nil && len(out) > 1 {
		reranked, rerr := rr.Rerank(ctx, q, out)
		if rerr != nil {
			if p.base.logger != nil {
				p.base.logger.Warn("knowledge retrieval rerank failed, using fused order", zap.Error(rerr))
			}
		} else if len(reranked) > 0 {
			out = reranked
		}
	}

	tokenModel := ""
	if p.base.embedder != nil {
		tokenModel = p.base.embedder.EmbeddingModelName()
	}
	var postPO *config.PostRetrieveConfig
	if p.base.config != nil {
		postPO = &p.base.config.PostRetrieve
	}
	out, err = ApplyPostRetrieve(out, postPO, tokenModel, finalTopK)
	if err != nil {
		return nil, err
	}
	return out, nil
}

var _ retriever.Retriever = (*knowledgePipelineRetriever)(nil)
