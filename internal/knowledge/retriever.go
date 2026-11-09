package knowledge

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"

	"cyberstrike-ai/internal/config"

	"github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// Retriever 检索器：SQLite 存向量 + Eino 嵌入，**纯向量检索**（余弦相似度、TopK、阈值），
// 实现语义与 [retriever.Retriever] 适配层 [VectorEinoRetriever] 一致。
type Retriever struct {
	db       *sql.DB
	embedder *Embedder
	config   *RetrievalConfig
	logger   *zap.Logger

	rerankMu sync.RWMutex
	reranker DocumentReranker

	pipeline retriever.Retriever
	wireOpenAI *config.OpenAIConfig
}

// RetrievalConfig 检索配置
type RetrievalConfig struct {
	TopK                int
	SimilarityThreshold float64
	SubIndexFilter      string
	MultiQuery          config.MultiQueryConfig
	Rerank              config.RerankConfig
	PostRetrieve        config.PostRetrieveConfig
}

// NewRetriever 创建新的检索器
func NewRetriever(db *sql.DB, embedder *Embedder, config *RetrievalConfig, logger *zap.Logger) *Retriever {
	return &Retriever{
		db:       db,
		embedder: embedder,
		config:   config,
		logger:   logger,
	}
}

// UpdateConfig 更新检索配置并重建 Eino MultiQuery + 重排流水线。
func (r *Retriever) UpdateConfig(cfg *RetrievalConfig) {
	if cfg != nil {
		r.config = cfg
		if r.logger != nil {
			r.logger.Info("retriever config updated",
				zap.Int("top_k", cfg.TopK),
				zap.Float64("similarity_threshold", cfg.SimilarityThreshold),
				zap.String("sub_index_filter", cfg.SubIndexFilter),
				zap.Int("multi_query_max", cfg.MultiQuery.MaxQueriesEffective()),
				zap.Int("post_retrieve_prefetch_top_k", cfg.PostRetrieve.PrefetchTopK),
				zap.Int("post_retrieve_max_context_chars", cfg.PostRetrieve.MaxContextChars),
				zap.Int("post_retrieve_max_context_tokens", cfg.PostRetrieve.MaxContextTokens),
			)
		}
	}
	if r.wireOpenAI != nil {
		if err := WireRetrieverPipeline(context.Background(), r, r.wireOpenAI); err != nil && r.logger != nil {
			r.logger.Warn("retrieval pipeline rebuild failed", zap.Error(err))
		}
	}
}

// SetDocumentReranker 注入可选重排器（并发安全）；nil 表示禁用。
func (r *Retriever) SetDocumentReranker(rr DocumentReranker) {
	if r == nil {
		return
	}
	r.rerankMu.Lock()
	defer r.rerankMu.Unlock()
	r.reranker = rr
}

func (r *Retriever) documentReranker() DocumentReranker {
	if r == nil {
		return nil
	}
	r.rerankMu.RLock()
	defer r.rerankMu.RUnlock()
	return r.reranker
}

func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0.0
	}

	var dotProduct, normA, normB float64
	for i := range a {
		dotProduct += float64(a[i] * b[i])
		normA += float64(a[i] * a[i])
		normB += float64(b[i] * b[i])
	}

	if normA == 0 || normB == 0 {
		return 0.0
	}

	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}

// Search 搜索知识库（Eino MultiQuery → 向量检索 → 重排 → 后处理）。
func (r *Retriever) Search(ctx context.Context, req *SearchRequest) ([]*RetrievalResult, error) {
	if req == nil {
		return nil, fmt.Errorf("request must not be empty")
	}
	q := strings.TrimSpace(req.Query)
	if q == "" {
		return nil, fmt.Errorf("query must not be empty")
	}
	opts := r.einoRetrieverOptions(req)
	docs, err := r.activeEinoRetriever().Retrieve(ctx, q, opts...)
	if err != nil {
		return nil, err
	}
	return documentsToRetrievalResults(docs)
}

func (r *Retriever) einoRetrieverOptions(req *SearchRequest) []retriever.Option {
	var opts []retriever.Option
	if req.TopK > 0 {
		opts = append(opts, retriever.WithTopK(req.TopK))
	}
	dsl := map[string]any{}
	if strings.TrimSpace(req.RiskType) != "" {
		dsl[DSLRiskType] = strings.TrimSpace(req.RiskType)
	}
	if req.Threshold > 0 {
		dsl[DSLSimilarityThreshold] = req.Threshold
	}
	if strings.TrimSpace(req.SubIndexFilter) != "" {
		dsl[DSLSubIndexFilter] = strings.TrimSpace(req.SubIndexFilter)
	}
	if len(dsl) > 0 {
		opts = append(opts, retriever.WithDSLInfo(dsl))
	}
	return opts
}

// EinoRetrieve 直接返回 [schema.Document]，供 Eino Graph / Chain 使用。
func (r *Retriever) EinoRetrieve(ctx context.Context, query string, opts ...retriever.Option) ([]*schema.Document, error) {
	return r.activeEinoRetriever().Retrieve(ctx, query, opts...)
}

func (r *Retriever) activeEinoRetriever() retriever.Retriever {
	if r != nil && r.pipeline != nil {
		return r.pipeline
	}
	return NewVectorEinoRetriever(r)
}

// AsEinoRetriever 将知识库检索流水线暴露为 Eino [retriever.Retriever]。
func (r *Retriever) AsEinoRetriever() retriever.Retriever {
	return r.activeEinoRetriever()
}

func (r *Retriever) knowledgeEmbeddingSelectSQL(riskType, subIndexFilter string) (string, []interface{}) {
	q := `SELECT e.id, e.item_id, e.chunk_index, e.chunk_text, e.embedding, e.embedding_model, e.embedding_dim, i.category, i.title
FROM knowledge_embeddings e
JOIN knowledge_base_items i ON e.item_id = i.id
WHERE 1=1`
	var args []interface{}
	if strings.TrimSpace(riskType) != "" {
		q += ` AND TRIM(i.category) = TRIM(?) COLLATE NOCASE`
		args = append(args, riskType)
	}
	if tag := strings.TrimSpace(subIndexFilter); tag != "" {
		tag = strings.ToLower(strings.ReplaceAll(tag, " ", ""))
		q += ` AND (TRIM(COALESCE(e.sub_indexes,'')) = '' OR INSTR(',' || LOWER(REPLACE(e.sub_indexes,' ','')) || ',', ',' || ? || ',') > 0)`
		args = append(args, tag)
	}
	return q, args
}

// vectorSearch 纯向量检索：余弦相似度排序，按相似度阈值与 TopK 截断（无 BM25、无混合分、无邻块扩展）。
func (r *Retriever) vectorSearch(ctx context.Context, req *SearchRequest) ([]*RetrievalResult, error) {
	if req.Query == "" {
		return nil, fmt.Errorf("query must not be empty")
	}

	topK := req.TopK
	if topK <= 0 && r.config != nil {
		topK = r.config.TopK
	}
	if topK <= 0 {
		topK = 5
	}

	threshold := req.Threshold
	if threshold <= 0 && r.config != nil {
		threshold = r.config.SimilarityThreshold
	}
	if threshold <= 0 {
		threshold = 0.7
	}

	subIdxFilter := strings.TrimSpace(req.SubIndexFilter)
	if subIdxFilter == "" && r.config != nil {
		subIdxFilter = strings.TrimSpace(r.config.SubIndexFilter)
	}

	queryText := FormatQueryEmbeddingText(req.RiskType, req.Query)
	queryEmbedding, err := r.embedder.EmbedText(ctx, queryText)
	if err != nil {
		return nil, fmt.Errorf("failed to embed query: %w", err)
	}
	queryDim := len(queryEmbedding)
	expectedModel := ""
	if r.embedder != nil {
		expectedModel = r.embedder.EmbeddingModelName()
	}

	sqlStr, sqlArgs := r.knowledgeEmbeddingSelectSQL(strings.TrimSpace(req.RiskType), subIdxFilter)
	rows, err := r.db.QueryContext(ctx, sqlStr, sqlArgs...)
	if err != nil {
		return nil, fmt.Errorf("failed to query vectors: %w", err)
	}
	defer rows.Close()

	type candidate struct {
		chunk      *KnowledgeChunk
		item       *KnowledgeItem
		similarity float64
	}

	candidates := make([]candidate, 0)
	rowNum := 0
	for rows.Next() {
		rowNum++
		if rowNum%48 == 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}
		}

		var chunkID, itemID, chunkText, embeddingJSON, category, title, rowModel string
		var chunkIndex, rowDim int

		if err := rows.Scan(&chunkID, &itemID, &chunkIndex, &chunkText, &embeddingJSON, &rowModel, &rowDim, &category, &title); err != nil {
			r.logger.Warn("failed to scan vector row", zap.Error(err))
			continue
		}

		var embedding []float32
		if err := json.Unmarshal([]byte(embeddingJSON), &embedding); err != nil {
			r.logger.Warn("failed to parse vector", zap.Error(err))
			continue
		}

		if rowDim > 0 && len(embedding) != rowDim {
			r.logger.Debug("skipping vector row with mismatched dimension", zap.String("chunkId", chunkID), zap.Int("rowDim", rowDim), zap.Int("got", len(embedding)))
			continue
		}
		if queryDim > 0 && len(embedding) != queryDim {
			r.logger.Debug("skipping vector whose dimension mismatches the query", zap.String("chunkId", chunkID), zap.Int("queryDim", queryDim), zap.Int("got", len(embedding)))
			continue
		}
		if expectedModel != "" && strings.TrimSpace(rowModel) != "" && strings.TrimSpace(rowModel) != expectedModel {
			r.logger.Debug("skipping row with mismatched embedding model", zap.String("chunkId", chunkID), zap.String("rowModel", rowModel), zap.String("expected", expectedModel))
			continue
		}

		similarity := cosineSimilarity(queryEmbedding, embedding)
		candidates = append(candidates, candidate{
			chunk: &KnowledgeChunk{
				ID:         chunkID,
				ItemID:     itemID,
				ChunkIndex: chunkIndex,
				ChunkText:  chunkText,
				Embedding:  embedding,
			},
			item: &KnowledgeItem{
				ID:       itemID,
				Category: category,
				Title:    title,
			},
			similarity: similarity,
		})
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].similarity > candidates[j].similarity
	})

	filtered := make([]candidate, 0, len(candidates))
	for _, c := range candidates {
		if c.similarity >= threshold {
			filtered = append(filtered, c)
		}
	}

	if len(filtered) > topK {
		filtered = filtered[:topK]
	}

	results := make([]*RetrievalResult, len(filtered))
	for i, c := range filtered {
		results[i] = &RetrievalResult{
			Chunk:      c.chunk,
			Item:       c.item,
			Similarity: c.similarity,
			Score:      c.similarity,
		}
	}
	return results, nil
}

// RetrievalConfigFromYAML maps API/YAML retrieval settings into the knowledge package.
func RetrievalConfigFromYAML(r config.RetrievalConfig) *RetrievalConfig {
	return &RetrievalConfig{
		TopK:                r.TopK,
		SimilarityThreshold: r.SimilarityThreshold,
		SubIndexFilter:      r.SubIndexFilter,
		MultiQuery:          r.MultiQuery,
		Rerank:              r.Rerank,
		PostRetrieve:        r.PostRetrieve,
	}
}
