package knowledge

import (
	"context"
	"fmt"
	"strings"

	"cyberstrike-ai/internal/config"
)

// VectorRetriever implements [DocumentRetriever] on top of SQLite-stored embeddings + cosine similarity.
// It returns prefetch-sized vector candidates only; rerank and post-process run in [knowledgePipelineRetriever].
type VectorRetriever struct {
	inner *Retriever
}

// NewVectorRetriever wraps r for Eino compose / tooling.
func NewVectorRetriever(r *Retriever) *VectorRetriever {
	if r == nil {
		return nil
	}
	return &VectorRetriever{inner: r}
}

// GetType identifies this retriever for Eino callbacks.
func (h *VectorRetriever) GetType() string {
	return "SQLiteVectorKnowledgeRetriever"
}

// Retrieve runs vector search and returns [Document] rows.
func (h *VectorRetriever) Retrieve(ctx context.Context, query string, opts ...RetrievalOption) (out []*Document, err error) {
	if h == nil || h.inner == nil {
		return nil, fmt.Errorf("VectorRetriever: nil retriever")
	}
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, fmt.Errorf("查询不能为空")
	}

	ro := retrievalOptions(opts...)
	cfg := h.inner.config

	req := &SearchRequest{Query: q}

	if ro.TopK != nil && *ro.TopK > 0 {
		req.TopK = *ro.TopK
	} else if cfg != nil && cfg.TopK > 0 {
		req.TopK = cfg.TopK
	} else {
		req.TopK = 5
	}

	req.Threshold = 0
	if ro.DSLInfo != nil {
		if rt, ok := ro.DSLInfo[DSLRiskType].(string); ok {
			req.RiskType = strings.TrimSpace(rt)
		}
		if v, ok := ro.DSLInfo[DSLSimilarityThreshold]; ok {
			if f, ok2 := DSLNumeric(v); ok2 && f > 0 {
				req.Threshold = f
			}
		}
		if sf, ok := ro.DSLInfo[DSLSubIndexFilter].(string); ok {
			req.SubIndexFilter = strings.TrimSpace(sf)
		}
	}
	if req.SubIndexFilter == "" && cfg != nil && strings.TrimSpace(cfg.SubIndexFilter) != "" {
		req.SubIndexFilter = strings.TrimSpace(cfg.SubIndexFilter)
	}
	if req.Threshold <= 0 && cfg != nil && cfg.SimilarityThreshold > 0 {
		req.Threshold = cfg.SimilarityThreshold
	}
	if req.Threshold <= 0 {
		req.Threshold = 0.7
	}

	finalTopK := req.TopK
	var postPO *config.PostRetrieveConfig
	if cfg != nil {
		postPO = &cfg.PostRetrieve
	}
	fetchK := EffectivePrefetchTopK(finalTopK, postPO)
	searchReq := *req
	searchReq.TopK = fetchK

	results, err := h.inner.vectorSearch(ctx, &searchReq)
	if err != nil {
		return nil, err
	}
	out = retrievalResultsToDocuments(results)
	return out, nil
}

func retrievalResultsToDocuments(results []*RetrievalResult) []*Document {
	out := make([]*Document, 0, len(results))
	for _, res := range results {
		if res == nil || res.Chunk == nil || res.Item == nil {
			continue
		}
		d := &Document{
			ID:      res.Chunk.ID,
			Content: res.Chunk.ChunkText,
			MetaData: map[string]any{
				metaKBItemID:     res.Item.ID,
				metaKBCategory:   res.Item.Category,
				metaKBTitle:      res.Item.Title,
				metaKBChunkIndex: res.Chunk.ChunkIndex,
				metaSimilarity:   res.Similarity,
			},
		}
		d.WithScore(res.Score)
		out = append(out, d)
	}
	return out
}

func documentsToRetrievalResults(docs []*Document) ([]*RetrievalResult, error) {
	out := make([]*RetrievalResult, 0, len(docs))
	for i, d := range docs {
		if d == nil {
			continue
		}
		itemID, err := RequireMetaString(d.MetaData, metaKBItemID)
		if err != nil {
			return nil, fmt.Errorf("document %d: %w", i, err)
		}
		cat := MetaLookupString(d.MetaData, metaKBCategory)
		title := MetaLookupString(d.MetaData, metaKBTitle)
		chunkIdx, err := RequireMetaInt(d.MetaData, metaKBChunkIndex)
		if err != nil {
			return nil, fmt.Errorf("document %d: %w", i, err)
		}
		sim, _ := MetaFloat64OK(d.MetaData, metaSimilarity)
		item := &KnowledgeItem{ID: itemID, Category: cat, Title: title}
		chunk := &KnowledgeChunk{
			ID:         d.ID,
			ItemID:     itemID,
			ChunkIndex: chunkIdx,
			ChunkText:  d.Content,
		}
		out = append(out, &RetrievalResult{
			Chunk:      chunk,
			Item:       item,
			Similarity: sim,
			Score:      d.Score(),
		})
	}
	return out, nil
}

var _ DocumentRetriever = (*VectorRetriever)(nil)
