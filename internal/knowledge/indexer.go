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

// Indexer 使用 Eino Compose 索引链（Markdown/递归分块、Lambda  enrich、SQLite 索引）与嵌入写入。
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

// NewIndexer 创建索引器并编译 Eino 索引链；kcfg 为完整知识库配置（含 indexing 与路径相关行为）。
func NewIndexer(ctx context.Context, db *sql.DB, embedder *Embedder, logger *zap.Logger, kcfg *config.KnowledgeConfig) (*Indexer, error) {
	if db == nil {
		return nil, fmt.Errorf("db is nil")
	}
	if embedder == nil {
		return nil, fmt.Errorf("embedder is nil")
	}
	if err := EnsureKnowledgeEmbeddingsSchema(db); err != nil {
		return nil, fmt.Errorf("knowledge_embeddings schema migration: %w", err)
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
			logger.Warn("Eino FileLoader init failed, prefer_source_file will fall back to database content", zap.Error(err))
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

// RecompileIndexChain 在配置或嵌入模型变更后重建 Eino 索引链（无需重启进程）。
func (idx *Indexer) RecompileIndexChain(ctx context.Context) error {
	if idx == nil || idx.db == nil || idx.embedder == nil {
		return fmt.Errorf("indexer not initialized")
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

// IndexItem 索引单个知识项：先清空旧向量，再走 Compose 链（分块、嵌入、写入）。
func (idx *Indexer) IndexItem(ctx context.Context, itemID string) error {
	if idx.indexChain == nil {
		return fmt.Errorf("index chain not initialized")
	}
	if idx.embedder == nil {
		return fmt.Errorf("embedder not initialized")
	}

	var content, category, title, filePath string
	err := idx.db.QueryRow("SELECT content, category, title, file_path FROM knowledge_base_items WHERE id = ?", itemID).Scan(&content, &category, &title, &filePath)
	if err != nil {
		return fmt.Errorf("failed to get knowledge item: %w", err)
	}

	if _, err := idx.db.Exec("DELETE FROM knowledge_embeddings WHERE item_id = ?", itemID); err != nil {
		return fmt.Errorf("failed to delete old vectors: %w", err)
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
			idx.logger.Warn("preferred source file read failed, using database content",
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
		msg := fmt.Sprintf("index write failed (item: %s): %v", itemID, err)
		idx.mu.Lock()
		idx.lastError = msg
		idx.lastErrorTime = time.Now()
		idx.mu.Unlock()
		return err
	}

	if idx.logger != nil {
		idx.logger.Info("knowledge item indexed", zap.String("itemId", itemID), zap.Int("chunks", len(ids)))
	}
	idx.rebuildMu.Lock()
	idx.rebuildLastItemID = itemID
	idx.rebuildLastChunks = len(ids)
	idx.rebuildMu.Unlock()
	return nil
}

// HasIndex 检查是否存在索引
func (idx *Indexer) HasIndex() (bool, error) {
	var count int
	err := idx.db.QueryRow("SELECT COUNT(*) FROM knowledge_embeddings").Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check index: %w", err)
	}
	return count > 0, nil
}

func (idx *Indexer) beginIndexRun() error {
	idx.rebuildMu.Lock()
	defer idx.rebuildMu.Unlock()

	if idx.isRebuilding {
		return fmt.Errorf("an indexing task is already in progress")
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

// TryBeginIndexRun 同步占用索引任务槽位；调用方必须在后台任务结束时调用 FinishIndexRun。
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

// IndexMissing 为尚无向量的知识项构建索引（默认推荐路径，适合冷启动与中断续跑）。
func (idx *Indexer) IndexMissing(ctx context.Context) error {
	if err := idx.beginIndexRun(); err != nil {
		return err
	}
	defer idx.FinishIndexRun()
	return idx.runIndexMissing(ctx)
}

// RebuildIndex 全量重建所有知识项索引（显式 opt-in，成本更高）。
func (idx *Indexer) RebuildIndex(ctx context.Context) error {
	if err := idx.beginIndexRun(); err != nil {
		return err
	}
	defer idx.FinishIndexRun()
	return idx.runRebuildIndex(ctx)
}

// RunRebuildIndex 在已占用索引任务槽位后执行全量重建（供 HTTP handler 后台任务使用）。
func (idx *Indexer) RunRebuildIndex(ctx context.Context) error {
	return idx.runRebuildIndex(ctx)
}

// RunIndexMissing 在已占用索引任务槽位后执行缺失索引补齐（供 HTTP handler 后台任务使用）。
func (idx *Indexer) RunIndexMissing(ctx context.Context) error {
	return idx.runIndexMissing(ctx)
}

func (idx *Indexer) runRebuildIndex(ctx context.Context) error {
	idx.resetLastError()

	rows, err := idx.db.QueryContext(ctx, "SELECT id FROM knowledge_base_items ORDER BY updated_at ASC, id ASC")
	if err != nil {
		return fmt.Errorf("failed to query knowledge items: %w", err)
	}
	defer rows.Close()

	itemIDs, err := scanKnowledgeItemIDs(rows)
	if err != nil {
		return err
	}

	idx.setIndexRunTotal(len(itemIDs))
	idx.logger.Info("starting full index rebuild", zap.Int("totalItems", len(itemIDs)))

	return idx.indexItemIDs(ctx, itemIDs, "index rebuild complete")
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
		return fmt.Errorf("failed to query un-indexed knowledge items: %w", err)
	}
	defer rows.Close()

	itemIDs, err := scanKnowledgeItemIDs(rows)
	if err != nil {
		return fmt.Errorf("failed to scan un-indexed knowledge item IDs: %w", err)
	}

	idx.setIndexRunTotal(len(itemIDs))
	idx.logger.Info("starting missing-index fill", zap.Int("totalItems", len(itemIDs)))

	return idx.indexItemIDs(ctx, itemIDs, "index build complete")
}

func scanKnowledgeItemIDs(rows *sql.Rows) ([]string, error) {
	var itemIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("failed to scan knowledge item ID: %w", err)
		}
		itemIDs = append(itemIDs, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan knowledge item ID: %w", err)
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
				idx.logger.Warn("failed to index knowledge item",
					zap.String("itemId", itemID),
					zap.Int("totalItems", len(itemIDs)),
					zap.Error(err),
				)
			}

			if consecutiveFailures >= maxConsecutiveFailures {
				errorMsg := fmt.Sprintf("%d consecutive knowledge items failed to index, likely a configuration problem (e.g. wrong embedding model, invalid API key, insufficient quota). First failed item: %s, error: %v", consecutiveFailures, firstFailureItemID, firstFailureError)
				idx.mu.Lock()
				idx.lastError = errorMsg
				idx.lastErrorTime = time.Now()
				idx.mu.Unlock()

				idx.logger.Error("too many consecutive index failures, stopping indexing immediately",
					zap.Int("consecutiveFailures", consecutiveFailures),
					zap.Int("totalItems", len(itemIDs)),
					zap.Int("processedItems", i+1),
					zap.String("firstFailureItemId", firstFailureItemID),
					zap.Error(firstFailureError),
				)
				return fmt.Errorf("too many consecutive index failures: %v", firstFailureError)
			}

			if failedCount > len(itemIDs)*3/10 && failedCount == len(itemIDs)*3/10+1 {
				errorMsg := fmt.Sprintf("too many knowledge items failed to index (%d/%d), likely a configuration problem. First failed item: %s, error: %v", failedCount, len(itemIDs), firstFailureItemID, firstFailureError)
				idx.mu.Lock()
				idx.lastError = errorMsg
				idx.lastErrorTime = time.Now()
				idx.mu.Unlock()

				idx.logger.Error("too many knowledge items failed to index, likely a configuration problem",
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
			idx.logger.Info("indexing progress", zap.Int("current", i+1), zap.Int("total", len(itemIDs)), zap.Int("failed", failedCount))
		}
	}

	idx.logger.Info(doneMessage, zap.Int("totalItems", len(itemIDs)), zap.Int("failedCount", failedCount))
	return nil
}

// GetLastError 获取最近一次错误信息
func (idx *Indexer) GetLastError() (string, time.Time) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.lastError, idx.lastErrorTime
}

// GetRebuildStatus 获取重建索引状态
func (idx *Indexer) GetRebuildStatus() (isRebuilding bool, totalItems int, current int, failed int, lastItemID string, lastChunks int, startTime time.Time) {
	idx.rebuildMu.RLock()
	defer idx.rebuildMu.RUnlock()
	return idx.isRebuilding, idx.rebuildTotalItems, idx.rebuildCurrent, idx.rebuildFailed, idx.rebuildLastItemID, idx.rebuildLastChunks, idx.rebuildStartTime
}
