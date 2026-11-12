package knowledge

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"cyberstrike-ai/internal/config"
)

// normalizeChunkStrategy returns "recursive" or "markdown_then_recursive".
func normalizeChunkStrategy(s string) string {
	v := strings.TrimSpace(strings.ToLower(s))
	switch v {
	case "recursive":
		return "recursive"
	case "markdown_then_recursive", "markdown_recursive", "markdown":
		return "markdown_then_recursive"
	case "":
		return "markdown_then_recursive"
	default:
		return "markdown_then_recursive"
	}
}

type knowledgeIndexChain struct {
	splitters []DocumentTransformer
	indexer   *SQLiteIndexer
	maxChunks int
}

func buildKnowledgeIndexChain(ctx context.Context, cfg *config.IndexingConfig, db *sql.DB, recursive DocumentTransformer, model string) (*knowledgeIndexChain, error) {
	if recursive == nil || db == nil {
		return nil, fmt.Errorf("knowledge index requires splitter and database")
	}
	ch := &knowledgeIndexChain{}
	batch := 64
	strategy := "markdown_then_recursive"
	if cfg != nil {
		strategy = normalizeChunkStrategy(cfg.ChunkStrategy)
		if cfg.BatchSize > 0 {
			batch = cfg.BatchSize
		}
		ch.maxChunks = cfg.MaxChunksPerItem
	}
	if strategy != "recursive" {
		md, err := newMarkdownHeaderSplitter(ctx)
		if err != nil {
			return nil, err
		}
		ch.splitters = append(ch.splitters, md)
	}
	ch.splitters = append(ch.splitters, recursive)
	ch.indexer = NewSQLiteIndexer(db, batch, model)
	return ch, nil
}
func (ch *knowledgeIndexChain) Invoke(ctx context.Context, docs []*Document, opts ...IndexOption) ([]string, error) {
	var err error
	for _, splitter := range ch.splitters {
		docs, err = splitter.Transform(ctx, docs)
		if err != nil {
			return nil, err
		}
	}
	out := make([]*Document, 0, len(docs))
	for _, d := range docs {
		if d != nil && strings.TrimSpace(d.Content) != "" {
			out = append(out, d)
		}
	}
	if ch.maxChunks > 0 && len(out) > ch.maxChunks {
		out = out[:ch.maxChunks]
	}
	for i, d := range out {
		if d.MetaData == nil {
			d.MetaData = map[string]any{}
		}
		d.MetaData[metaKBChunkIndex] = i
	}
	return ch.indexer.Store(ctx, out, opts...)
}
