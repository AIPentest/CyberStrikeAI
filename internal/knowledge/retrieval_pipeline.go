package knowledge

import (
	"context"
	"fmt"
	"strings"

	"cyberstrike-ai/internal/config"

	"go.uber.org/zap"
)

// knowledgePipelineRetriever: MultiQuery → vector candidates → rerank → post-process.
type knowledgePipelineRetriever struct {
	inner DocumentRetriever
	base  *Retriever
}

func newKnowledgePipelineRetriever(inner DocumentRetriever, base *Retriever) *knowledgePipelineRetriever {
	if inner == nil || base == nil {
		return nil
	}
	return &knowledgePipelineRetriever{inner: inner, base: base}
}

func (p *knowledgePipelineRetriever) GetType() string {
	return "KnowledgeRAGPipeline"
}

func (p *knowledgePipelineRetriever) Retrieve(ctx context.Context, query string, opts ...RetrievalOption) (out []*Document, err error) {
	if p == nil || p.inner == nil || p.base == nil {
		return nil, fmt.Errorf("knowledge pipeline retriever: nil")
	}
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, fmt.Errorf("查询不能为空")
	}

	ro := retrievalOptions(opts...)
	finalTopK := p.base.config.TopK
	if finalTopK <= 0 {
		finalTopK = 5
	}
	if ro.TopK != nil && *ro.TopK > 0 {
		finalTopK = *ro.TopK
	}

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
				p.base.logger.Warn("知识检索重排失败，已使用融合序", zap.Error(rerr))
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

var _ DocumentRetriever = (*knowledgePipelineRetriever)(nil)
