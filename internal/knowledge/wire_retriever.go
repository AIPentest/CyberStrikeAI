package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/openai"
	"go.uber.org/zap"
)

// WireRetrieverPipeline composes query expansion, vector search, reranking and context budgeting.
func WireRetrieverPipeline(ctx context.Context, r *Retriever, oa *config.OpenAIConfig) error {
	if r == nil || oa == nil || r.config == nil {
		return fmt.Errorf("knowledge retrieval configuration is incomplete")
	}
	r.wireOpenAI = oa
	reranker, err := NewHTTPReranker(&r.config.Rerank, oa, r.logger)
	if err != nil {
		return err
	}
	r.SetDocumentReranker(reranker)
	logger := r.logger
	if logger == nil {
		logger = zap.NewNop()
	}
	mq := &nativeMultiQuery{inner: NewVectorRetriever(r), max: r.config.MultiQuery.MaxQueriesEffective(), client: openai.NewClient(oa, &http.Client{Timeout: 120 * time.Second}, logger), model: oa.Model, logger: logger}
	r.pipeline = newKnowledgePipelineRetriever(mq, r)
	return nil
}

type nativeMultiQuery struct {
	inner  DocumentRetriever
	max    int
	client *openai.Client
	model  string
	logger *zap.Logger
}

func (q *nativeMultiQuery) Retrieve(ctx context.Context, query string, opts ...RetrievalOption) ([]*Document, error) {
	queries := []string{query}
	if q.max > 1 {
		payload := map[string]any{"model": q.model, "messages": []map[string]string{{"role": "system", "content": fmt.Sprintf("Return only a JSON array containing at most %d alternative search queries for the user's knowledge search. Preserve technical identifiers and the user's language.", q.max-1)}, {"role": "user", "content": query}}}
		var answer struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := q.client.ChatCompletion(ctx, payload, &answer); err == nil && len(answer.Choices) > 0 {
			text := strings.TrimSpace(answer.Choices[0].Message.Content)
			text = strings.TrimPrefix(text, "```json")
			text = strings.TrimPrefix(text, "```")
			text = strings.TrimSuffix(text, "```")
			var generated []string
			if json.Unmarshal([]byte(strings.TrimSpace(text)), &generated) == nil {
				seen := map[string]bool{query: true}
				for _, v := range generated {
					v = strings.TrimSpace(v)
					if v != "" && !seen[v] {
						queries = append(queries, v)
						seen[v] = true
					}
					if len(queries) >= q.max {
						break
					}
				}
			}
		} else if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			q.logger.Warn("查询扩展失败，使用原始查询", zap.Error(err))
		}
	}
	scores := map[string]float64{}
	docs := map[string]*Document{}
	var order []string
	for _, query := range queries {
		rows, err := q.inner.Retrieve(ctx, query, opts...)
		if err != nil {
			return nil, err
		}
		for rank, d := range rows {
			key := d.ID
			if key == "" {
				key = d.Content
			}
			if _, ok := docs[key]; !ok {
				docs[key] = d
				order = append(order, key)
			}
			scores[key] += 1 / float64(60+rank+1)
		}
	}
	sort.SliceStable(order, func(i, j int) bool { return scores[order[i]] > scores[order[j]] })
	out := make([]*Document, 0, len(order))
	for _, key := range order {
		out = append(out, docs[key])
	}
	return out, nil
}
