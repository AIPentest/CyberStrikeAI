package knowledge

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	"cyberstrike-ai/internal/config"

	fileloader "github.com/cloudwego/eino-ext/components/document/loader/file"
	"github.com/cloudwego/eino/components/document"
	"github.com/cloudwego/eino/components/indexer"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// Indexer Use the Eino Compose index chain (Markdown/recursive chunking, Lambda enrich, SQLite index) with embedding writes.
type Indexer struct {
	db          *sql.DB
	embedder    *Embedder
	logger      *zap.Logger
	chunkSize   int
	overlap     int
	indexingCfg *config.IndexingConfig

	indexChain compose.Runnable[[]*schema.Document, []string]
	fileLoader *fileloader.FileLoader

	mu            sync.RWMutex
	lastError     string
	lastErrorTime time.Time
	errorCount    int

	rebuildMu         sync.RWMutex
	isRebuilding      bool
	rebuildTotalItems int
	rebuildCurrent    int
	rebuildFailed     int
	rebuildStartTime  time.Time
	rebuildLastItemID string
	rebuildLastChunks int
}

// NewIndexer Create the indexer and compile the Eino index chain; kcfg is the full knowledge-base config (including indexing and path-related behavior).
func NewIndexer(ctx context.Context, db *sql.DB, embedder *Embedder, logger *zap.Logger, kcfg *config.KnowledgeConfig) (*Indexer, error) {
	if db == nil {
		return nil, fmt.Errorf("db is nil")
	}
	if embedder == nil {
		return nil, fmt.Errorf("embedder is nil")
	}
	if err := EnsureKnowledgeEmbeddingsSchema(db); err != nil {
		return nil, fmt.Errorf("knowledge_embeddings Structure migration: %w", err)
	}
	if kcfg == nil {
		kcfg = &config.KnowledgeConfig{}
	}
	indexingCfg := &kcfg.Indexing

	chunkSize := 512
	overlap := 50
	if indexingCfg.ChunkSize > 0 {
		chunkSize = indexingCfg.ChunkSize
	}
	if indexingCfg.ChunkOverlap >= 0 {
		overlap = indexingCfg.ChunkOverlap
	}

	embedModel := embedder.EmbeddingModelName()
	splitter, err := newKnowledgeSplitter(chunkSize, overlap, embedModel)
	if err != nil {
		return nil, fmt.Errorf("eino recursive splitter: %w", err)
	}

	chain, err := buildKnowledgeIndexChain(ctx, indexingCfg, db, splitter, embedModel)
	if err != nil {
		return nil, fmt.Errorf("knowledge index chain: %w", err)
	}

	var fl *fileloader.FileLoader
	fl, err = fileloader.NewFileLoader(ctx, nil)
	if err != nil {
		if logger != nil {
			logger.Warn("Eino FileLoader Initialization failed; prefer_source_file will fall back to the database body", zap.Error(err))
		}
		fl = nil
		err = nil
	}

	return &Indexer{
		db:          db,
		embedder:    embedder,
		logger:      logger,
		chunkSize:   chunkSize,
		overlap:     overlap,
		indexingCfg: indexingCfg,
		indexChain:  chain,
		fileLoader:  fl,
	}, nil
}

// RecompileIndexChain Rebuild the Eino index chain after a config or embedding-model change (no process restart needed).
func (idx *Indexer) RecompileIndexChain(ctx context.Context) error {
	if idx == nil || idx.db == nil || idx.embedder == nil {
		return fmt.Errorf("indexer Not initialized")
	}
	if err := EnsureKnowledgeEmbeddingsSchema(idx.db); err != nil {
		return err
	}
	embedModel := idx.embedder.EmbeddingModelName()
	splitter, err := newKnowledgeSplitter(idx.chunkSize, idx.overlap, embedModel)
	if err != nil {
		return fmt.Errorf("eino recursive splitter: %w", err)
	}
	chain, err := buildKnowledgeIndexChain(ctx, idx.indexingCfg, idx.db, splitter, embedModel)
	if err != nil {
		return fmt.Errorf("knowledge index chain: %w", err)
	}
	idx.indexChain = chain
	return nil
}

// IndexItem Index a single knowledge item: first clear old vectors, then run the Compose chain (chunking, embedding, writing).
func (idx *Indexer) IndexItem(ctx context.Context, itemID string) error {
	if idx.indexChain == nil {
		return fmt.Errorf("The index chain is not initialized")
	}
	if idx.embedder == nil {
		return fmt.Errorf("The embedder is not initialized")
	}

	var content, category, title, filePath string
	err := idx.db.QueryRow("SELECT content, category, title, file_path FROM knowledge_base_items WHERE id = ?", itemID).Scan(&content, &category, &title, &filePath)
	if err != nil {
		return fmt.Errorf("Failed to get the knowledge item: %w", err)
	}

	if _, err := idx.db.Exec("DELETE FROM knowledge_embeddings WHERE item_id = ?", itemID); err != nil {
		return fmt.Errorf("Failed to delete old vectors: %w", err)
	}

	body := strings.TrimSpace(content)
	if idx.indexingCfg != nil && idx.indexingCfg.PreferSourceFile && strings.TrimSpace(filePath) != "" && idx.fileLoader != nil {
		docs, lerr := idx.fileLoader.Load(ctx, document.Source{URI: strings.TrimSpace(filePath)})
		if lerr == nil && len(docs) > 0 {
			var b strings.Builder
			for i, d := range docs {
				if d == nil {
					continue
				}
				if i > 0 {
					b.WriteString("\n\n")
				}
				b.WriteString(d.Content)
			}
			if s := strings.TrimSpace(b.String()); s != "" {
				body = s
			}
		} else if idx.logger != nil {
			idx.logger.Warn("Preferred source-file read failed; use the database body",
				zap.String("itemId", itemID),
				zap.String("path", filePath),
				zap.Error(lerr))
		}
	}

	root := &schema.Document{
		ID:      itemID,
		Content: body,
		MetaData: map[string]any{
			metaKBCategory: category,
			metaKBTitle:    title,
			metaKBItemID:   itemID,
		},
	}

	idxOpts := []indexer.Option{indexer.WithEmbedding(idx.embedder.EinoEmbeddingComponent())}
	if idx.indexingCfg != nil && len(idx.indexingCfg.SubIndexes) > 0 {
		idxOpts = append(idxOpts, indexer.WithSubIndexes(idx.indexingCfg.SubIndexes))
	}

	ids, err := idx.indexChain.Invoke(ctx, []*schema.Document{root}, compose.WithIndexerOption(idxOpts...))
	if err != nil {
		msg := fmt.Sprintf("Index write failed (knowledge item: %s): %v", itemID, err)
		idx.mu.Lock()
		idx.lastError = msg
		idx.lastErrorTime = time.Now()
		idx.mu.Unlock()
		return err
	}

	if idx.logger != nil {
		idx.logger.Info("Knowledge-item indexing complete", zap.String("itemId", itemID), zap.Int("chunks", len(ids)))
	}
	idx.rebuildMu.Lock()
	idx.rebuildLastItemID = itemID
	idx.rebuildLastChunks = len(ids)
	idx.rebuildMu.Unlock()
	return nil
}

// HasIndex Check whether an index exists
func (idx *Indexer) HasIndex() (bool, error) {
	var count int
	err := idx.db.QueryRow("SELECT COUNT(*) FROM knowledge_embeddings").Scan(&count)
	if err != nil {
		return false, fmt.Errorf("Failed to check the index: %w", err)
	}
	return count > 0, nil
}

func (idx *Indexer) beginIndexRun() error {
	idx.rebuildMu.Lock()
	defer idx.rebuildMu.Unlock()

	if idx.isRebuilding {
		return fmt.Errorf("An index task is already in progress")
	}
	idx.isRebuilding = true
	idx.rebuildTotalItems = 0
	idx.rebuildCurrent = 0
	idx.rebuildFailed = 0
	idx.rebuildStartTime = time.Now()
	idx.rebuildLastItemID = ""
	idx.rebuildLastChunks = 0
	return nil
}

// TryBeginIndexRun Synchronously occupy the index-task slot; the caller must call it when the background task ends FinishIndexRun.
func (idx *Indexer) TryBeginIndexRun() error {
	return idx.beginIndexRun()
}

func (idx *Indexer) FinishIndexRun() {
	idx.rebuildMu.Lock()
	idx.isRebuilding = false
	idx.rebuildMu.Unlock()
}

func (idx *Indexer) resetLastError() {
	idx.mu.Lock()
	idx.lastError = ""
	idx.lastErrorTime = time.Time{}
	idx.errorCount = 0
	idx.mu.Unlock()
}

func (idx *Indexer) setIndexRunTotal(total int) {
	idx.rebuildMu.Lock()
	idx.rebuildTotalItems = total
	idx.rebuildMu.Unlock()
}

// IndexMissing Build indexes for knowledge items that have no vectors yet (the default recommended path, suitable for cold start and resuming after interruption).
func (idx *Indexer) IndexMissing(ctx context.Context) error {
	if err := idx.beginIndexRun(); err != nil {
		return err
	}
	defer idx.FinishIndexRun()
	return idx.runIndexMissing(ctx)
}

// RebuildIndex Full rebuild of all knowledge-item indexes (explicit opt-in, more costly).
func (idx *Indexer) RebuildIndex(ctx context.Context) error {
	if err := idx.beginIndexRun(); err != nil {
		return err
	}
	defer idx.FinishIndexRun()
	return idx.runRebuildIndex(ctx)
}

// RunRebuildIndex After occupying the index-task slot, perform a full rebuild (for use by the HTTP handler background task).
func (idx *Indexer) RunRebuildIndex(ctx context.Context) error {
	return idx.runRebuildIndex(ctx)
}

// RunIndexMissing After occupying the index-task slot, fill in missing indexes (for use by the HTTP handler background task).
func (idx *Indexer) RunIndexMissing(ctx context.Context) error {
	return idx.runIndexMissing(ctx)
}

func (idx *Indexer) runRebuildIndex(ctx context.Context) error {
	idx.resetLastError()

	rows, err := idx.db.QueryContext(ctx, "SELECT id FROM knowledge_base_items ORDER BY updated_at ASC, id ASC")
	if err != nil {
		return fmt.Errorf("Failed to query knowledge items: %w", err)
	}
	defer rows.Close()

	itemIDs, err := scanKnowledgeItemIDs(rows)
	if err != nil {
		return err
	}

	idx.setIndexRunTotal(len(itemIDs))
	idx.logger.Info("Start rebuilding the index", zap.Int("totalItems", len(itemIDs)))

	return idx.indexItemIDs(ctx, itemIDs, "Index rebuild complete")
}

func (idx *Indexer) runIndexMissing(ctx context.Context) error {
	idx.resetLastError()

	rows, err := idx.db.QueryContext(ctx, `
		SELECT i.id
		FROM knowledge_base_items i
		LEFT JOIN knowledge_embeddings e ON e.item_id = i.id
		WHERE e.item_id IS NULL
		ORDER BY i.updated_at ASC, i.id ASC
	`)
	if err != nil {
		return fmt.Errorf("Failed to query unindexed knowledge items: %w", err)
	}
	defer rows.Close()

	itemIDs, err := scanKnowledgeItemIDs(rows)
	if err != nil {
		return fmt.Errorf("Failed to scan unindexed knowledge-item IDs: %w", err)
	}

	idx.setIndexRunTotal(len(itemIDs))
	idx.logger.Info("Start filling in missing indexes", zap.Int("totalItems", len(itemIDs)))

	return idx.indexItemIDs(ctx, itemIDs, "Index build complete")
}

func scanKnowledgeItemIDs(rows *sql.Rows) ([]string, error) {
	var itemIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("Failed to scan knowledge-item IDs: %w", err)
		}
		itemIDs = append(itemIDs, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("Failed to scan knowledge-item IDs: %w", err)
	}
	return itemIDs, nil
}

func (idx *Indexer) indexItemIDs(ctx context.Context, itemIDs []string, doneMessage string) error {
	failedCount := 0
	consecutiveFailures := 0
	maxConsecutiveFailures := 5
	firstFailureItemID := ""
	var firstFailureError error

	for i, itemID := range itemIDs {
		if err := idx.IndexItem(ctx, itemID); err != nil {
			failedCount++
			consecutiveFailures++

			if consecutiveFailures == 1 {
				firstFailureItemID = itemID
				firstFailureError = err
				idx.logger.Warn("Failed to index the knowledge item",
					zap.String("itemId", itemID),
					zap.Int("totalItems", len(itemIDs)),
					zap.Error(err),
				)
			}

			if consecutiveFailures >= maxConsecutiveFailures {
				errorMsg := fmt.Sprintf("%d consecutive knowledge items failed to index; there may be a config problem (such as a wrong embedding model config, an invalid API key, or insufficient balance). First failed item: %s, error: %v", consecutiveFailures, firstFailureItemID, firstFailureError)
				idx.mu.Lock()
				idx.lastError = errorMsg
				idx.lastErrorTime = time.Now()
				idx.mu.Unlock()

				idx.logger.Error("Too many consecutive index failures; stop indexing immediately",
					zap.Int("consecutiveFailures", consecutiveFailures),
					zap.Int("totalItems", len(itemIDs)),
					zap.Int("processedItems", i+1),
					zap.String("firstFailureItemId", firstFailureItemID),
					zap.Error(firstFailureError),
				)
				return fmt.Errorf("Too many consecutive index failures: %v", firstFailureError)
			}

			if failedCount > len(itemIDs)*3/10 && failedCount == len(itemIDs)*3/10+1 {
				errorMsg := fmt.Sprintf("Too many knowledge items failed to index (%d/%d); there may be a config problem. First failed item: %s, error: %v", failedCount, len(itemIDs), firstFailureItemID, firstFailureError)
				idx.mu.Lock()
				idx.lastError = errorMsg
				idx.lastErrorTime = time.Now()
				idx.mu.Unlock()

				idx.logger.Error("Too many knowledge items failed to index; there may be a config problem",
					zap.Int("failedCount", failedCount),
					zap.Int("totalItems", len(itemIDs)),
					zap.String("firstFailureItemId", firstFailureItemID),
					zap.Error(firstFailureError),
				)
			}
			continue
		}

		if consecutiveFailures > 0 {
			consecutiveFailures = 0
			firstFailureItemID = ""
			firstFailureError = nil
		}

		idx.rebuildMu.Lock()
		idx.rebuildCurrent = i + 1
		idx.rebuildFailed = failedCount
		idx.rebuildMu.Unlock()

		if (i+1)%10 == 0 || (len(itemIDs) > 0 && (i+1)*100/len(itemIDs)%10 == 0 && (i+1)*100/len(itemIDs) > 0) {
			idx.logger.Info("Index progress", zap.Int("current", i+1), zap.Int("total", len(itemIDs)), zap.Int("failed", failedCount))
		}
	}

	idx.logger.Info(doneMessage, zap.Int("totalItems", len(itemIDs)), zap.Int("failedCount", failedCount))
	return nil
}

// GetLastError Get the most recent error message
func (idx *Indexer) GetLastError() (string, time.Time) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.lastError, idx.lastErrorTime
}

// GetRebuildStatus Get the index-rebuild status
func (idx *Indexer) GetRebuildStatus() (isRebuilding bool, totalItems int, current int, failed int, lastItemID string, lastChunks int, startTime time.Time) {
	idx.rebuildMu.RLock()
	defer idx.rebuildMu.RUnlock()
	return idx.isRebuilding, idx.rebuildTotalItems, idx.rebuildCurrent, idx.rebuildFailed, idx.rebuildLastItemID, idx.rebuildLastChunks, idx.rebuildStartTime
}
