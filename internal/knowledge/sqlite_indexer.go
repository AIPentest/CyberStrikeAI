package knowledge

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// SQLiteIndexer implements [indexer.Indexer] against knowledge_embeddings + existing schema.
type SQLiteIndexer struct {
	db             *sql.DB
	batchSize      int
	embeddingModel string
}

// NewSQLiteIndexer returns an indexer that writes chunk rows for one knowledge item per Store call.
// batchSize is the embedding batch size; if <= 0, default 64 is used.
// embeddingModel is persisted per row for retrieval-time consistency checks (may be empty).
func NewSQLiteIndexer(db *sql.DB, batchSize int, embeddingModel string) *SQLiteIndexer {
	return &SQLiteIndexer{db: db, batchSize: batchSize, embeddingModel: strings.TrimSpace(embeddingModel)}
}

// GetType implements eino callback run info.
func (s *SQLiteIndexer) GetType() string {
	return "SQLiteKnowledgeIndexer"
}

// Store embeds documents and inserts rows. Each doc must carry MetaData:
// kb_item_id, kb_category, kb_title, kb_chunk_index (int). Content is chunk text only.
func (s *SQLiteIndexer) Store(ctx context.Context, docs []*Document, opts ...IndexOption) (ids []string, err error) {
	options := indexOptions(opts...)
	if options.Embedding == nil {
		return nil, fmt.Errorf("sqlite indexer: embedding is required")
	}
	if len(docs) == 0 {
		return nil, nil
	}

	subIdxStr := strings.Join(options.SubIndexes, ",")

	texts := make([]string, len(docs))
	for i, d := range docs {
		if d == nil {
			return nil, fmt.Errorf("sqlite indexer: nil document at %d", i)
		}
		cat := MetaLookupString(d.MetaData, metaKBCategory)
		title := MetaLookupString(d.MetaData, metaKBTitle)
		texts[i] = FormatEmbeddingInput(cat, title, d.Content)
	}

	bs := s.batchSize
	if bs <= 0 {
		bs = 64
	}

	var allVecs [][]float64
	for start := 0; start < len(texts); start += bs {
		end := start + bs
		if end > len(texts) {
			end = len(texts)
		}
		batch := texts[start:end]
		vecs, embedErr := options.Embedding.EmbedStrings(ctx, batch)
		if embedErr != nil {
			return nil, fmt.Errorf("sqlite indexer: embed batch %d-%d: %w", start, end, embedErr)
		}
		if len(vecs) != len(batch) {
			return nil, fmt.Errorf("sqlite indexer: embed count mismatch: got %d want %d", len(vecs), len(batch))
		}
		allVecs = append(allVecs, vecs...)
	}

	embedDim := 0
	if len(allVecs) > 0 {
		embedDim = len(allVecs[0])
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("sqlite indexer: begin tx: %w", err)
	}
	defer tx.Rollback()

	items := map[string]bool{}
	for _, d := range docs {
		id, err := RequireMetaString(d.MetaData, metaKBItemID)
		if err != nil {
			return nil, err
		}
		items[id] = true
	}
	for id := range items {
		if _, err := tx.ExecContext(ctx, "DELETE FROM knowledge_embeddings WHERE item_id = ?", id); err != nil {
			return nil, err
		}
	}
	ids = make([]string, 0, len(docs))
	for i, d := range docs {
		chunkID := uuid.New().String()
		itemID, metaErr := RequireMetaString(d.MetaData, metaKBItemID)
		if metaErr != nil {
			return nil, fmt.Errorf("sqlite indexer: doc %d: %w", i, metaErr)
		}
		chunkIdx, metaErr := RequireMetaInt(d.MetaData, metaKBChunkIndex)
		if metaErr != nil {
			return nil, fmt.Errorf("sqlite indexer: doc %d: %w", i, metaErr)
		}
		vec := allVecs[i]
		if embedDim > 0 && len(vec) != embedDim {
			return nil, fmt.Errorf("sqlite indexer: inconsistent embedding dim at doc %d: got %d want %d", i, len(vec), embedDim)
		}
		vec32 := make([]float32, len(vec))
		for j, v := range vec {
			vec32[j] = float32(v)
		}
		embeddingJSON, jsonErr := json.Marshal(vec32)
		if jsonErr != nil {
			return nil, fmt.Errorf("sqlite indexer: marshal embedding: %w", jsonErr)
		}
		_, err = tx.ExecContext(ctx,
			`INSERT INTO knowledge_embeddings (id, item_id, chunk_index, chunk_text, embedding, sub_indexes, embedding_model, embedding_dim, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
			chunkID, itemID, chunkIdx, d.Content, string(embeddingJSON), subIdxStr, s.embeddingModel, embedDim,
		)
		if err != nil {
			return nil, fmt.Errorf("sqlite indexer: insert chunk %d: %w", i, err)
		}
		ids = append(ids, chunkID)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("sqlite indexer: commit: %w", err)
	}
	return ids, nil
}
