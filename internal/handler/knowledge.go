package handler

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"cyberstrike-ai/internal/audit"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/knowledge"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// KnowledgeHandler 知识库处理器
type KnowledgeHandler struct {
	manager   *knowledge.Manager
	retriever *knowledge.Retriever
	indexer   *knowledge.Indexer
	db        *database.DB
	logger    *zap.Logger
	audit     *audit.Service
}

// SetAudit wires platform audit logging.
func (h *KnowledgeHandler) SetAudit(s *audit.Service) {
	h.audit = s
}

// NewKnowledgeHandler 创建新的知识库处理器
func NewKnowledgeHandler(
	manager *knowledge.Manager,
	retriever *knowledge.Retriever,
	indexer *knowledge.Indexer,
	db *database.DB,
	logger *zap.Logger,
) *KnowledgeHandler {
	return &KnowledgeHandler{
		manager:   manager,
		retriever: retriever,
		indexer:   indexer,
		db:        db,
		logger:    logger,
	}
}

// GetCategories 获取所有分类
func (h *KnowledgeHandler) GetCategories(c *gin.Context) {
	categories, err := h.manager.GetCategories()
	if err != nil {
		h.logger.Error("获取分类失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"categories": categories})
}

// GetItems 获取知识项列表（支持按分类分页和关键字搜索，默认不返回完整内容）
func (h *KnowledgeHandler) GetItems(c *gin.Context) {
	category := c.Query("category")
	searchKeyword := c.Query("search") // 搜索关键字

	// 如果提供了搜索关键字，执行关键字搜索（在所有数据中搜索）
	if searchKeyword != "" {
		items, err := h.manager.SearchItemsByKeyword(searchKeyword, category)
		if err != nil {
			h.logger.Error("搜索知识项失败", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// 按分类分组结果
		groupedByCategory := make(map[string][]*knowledge.KnowledgeItemSummary)
		for _, item := range items {
			cat := item.Category
			if cat == "" {
				cat = "未分类"
			}
			groupedByCategory[cat] = append(groupedByCategory[cat], item)
		}

		// 转换为 CategoryWithItems 格式
		categoriesWithItems := make([]*knowledge.CategoryWithItems, 0, len(groupedByCategory))
		for cat, catItems := range groupedByCategory {
			categoriesWithItems = append(categoriesWithItems, &knowledge.CategoryWithItems{
				Category:  cat,
				ItemCount: len(catItems),
				Items:     catItems,
			})
		}

		// 按分类名称排序
		for i := 0; i < len(categoriesWithItems)-1; i++ {
			for j := i + 1; j < len(categoriesWithItems); j++ {
				if categoriesWithItems[i].Category > categoriesWithItems[j].Category {
					categoriesWithItems[i], categoriesWithItems[j] = categoriesWithItems[j], categoriesWithItems[i]
				}
			}
		}

		c.JSON(http.StatusOK, gin.H{
			"categories": categoriesWithItems,
			"total":      len(categoriesWithItems),
			"search":     searchKeyword,
			"is_search":  true,
		})
		return
	}

	// 分页模式：categoryPage=true 表示按分类分页，否则按项分页（向后兼容）
	categoryPageMode := c.Query("categoryPage") != "false" // 默认使用分类分页

	// 分页参数
	limit := 50 // 默认每页 50 条（分类分页时为分类数，项分页时为项数）
	offset := 0
	if limitStr := c.Query("limit"); limitStr != "" {
		if parsed, err := parseInt(limitStr); err == nil && parsed > 0 && parsed <= 500 {
			limit = parsed
		}
	}
	if offsetStr := c.Query("offset"); offsetStr != "" {
		if parsed, err := parseInt(offsetStr); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	// 如果指定了 category 参数，且使用分类分页模式，则只返回该分类
	if category != "" && categoryPageMode {
		// 单分类模式：返回该分类的所有知识项（不分页）
		items, total, err := h.manager.GetItemsSummary(category, 0, 0)
		if err != nil {
			h.logger.Error("获取知识项失败", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// 包装成分类结构
		categoriesWithItems := []*knowledge.CategoryWithItems{
			{
				Category:  category,
				ItemCount: total,
				Items:     items,
			},
		}

		c.JSON(http.StatusOK, gin.H{
			"categories": categoriesWithItems,
			"total":      1, // 只有一个分类
			"limit":      limit,
			"offset":     offset,
		})
		return
	}

	if categoryPageMode {
		// 按分类分页模式（默认）
		// limit 表示每页分类数，推荐 5-10 个分类
		if limit <= 0 || limit > 100 {
			limit = 10 // 默认每页 10 个分类
		}

		categoriesWithItems, totalCategories, err := h.manager.GetCategoriesWithItems(limit, offset)
		if err != nil {
			h.logger.Error("获取分类知识项失败", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"categories": categoriesWithItems,
			"total":      totalCategories,
			"limit":      limit,
			"offset":     offset,
		})
		return
	}

	// 按项分页模式（向后兼容）
	// 是否包含完整内容（默认 false，只返回摘要）
	includeContent := c.Query("includeContent") == "true"

	if includeContent {
		// 返回完整内容（向后兼容）
		items, err := h.manager.GetItemsWithOptions(category, limit, offset, true)
		if err != nil {
			h.logger.Error("获取知识项失败", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// 获取总数
		total, err := h.manager.GetItemsCount(category)
		if err != nil {
			h.logger.Warn("获取知识项总数失败", zap.Error(err))
			total = len(items)
		}

		c.JSON(http.StatusOK, gin.H{
			"items":  items,
			"total":  total,
			"limit":  limit,
			"offset": offset,
		})
	} else {
		// 返回摘要（不包含完整内容，推荐方式）
		items, total, err := h.manager.GetItemsSummary(category, limit, offset)
		if err != nil {
			h.logger.Error("获取知识项失败", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"items":  items,
			"total":  total,
			"limit":  limit,
			"offset": offset,
		})
	}
}

// GetItem 获取单个知识项
func (h *KnowledgeHandler) GetItem(c *gin.Context) {
	id := c.Param("id")

	item, err := h.manager.GetItem(id)
	if err != nil {
		h.logger.Error("获取知识项失败", zap.Error(err))
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, item)
}

// CreateItem 创建知识项
func (h *KnowledgeHandler) CreateItem(c *gin.Context) {
	var req struct {
		Category string `json:"category" binding:"required"`
		Title    string `json:"title" binding:"required"`
		Content  string `json:"content" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	item, err := h.manager.CreateItem(req.Category, req.Title, req.Content)
	if err != nil {
		h.logger.Error("创建知识项失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 异步索引
	go func() {
		ctx := context.Background()
		if err := h.indexer.IndexItem(ctx, item.ID); err != nil {
			h.logger.Warn("索引知识项失败", zap.String("itemId", item.ID), zap.Error(err))
		}
	}()

	c.JSON(http.StatusOK, item)
}

// UpdateItem 更新知识项
func (h *KnowledgeHandler) UpdateItem(c *gin.Context) {
	id := c.Param("id")

	var req struct {
		Category string `json:"category" binding:"required"`
		Title    string `json:"title" binding:"required"`
		Content  string `json:"content" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	item, err := h.manager.UpdateItem(id, req.Category, req.Title, req.Content)
	if err != nil {
		h.logger.Error("更新知识项失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 异步重新索引
	go func() {
		ctx := context.Background()
		if err := h.indexer.IndexItem(ctx, item.ID); err != nil {
			h.logger.Warn("重新索引知识项失败", zap.String("itemId", item.ID), zap.Error(err))
		}
	}()

	c.JSON(http.StatusOK, item)
}

// DeleteItem 删除知识项
func (h *KnowledgeHandler) DeleteItem(c *gin.Context) {
	id := c.Param("id")

	if err := h.manager.DeleteItem(id); err != nil {
		h.logger.Error("删除知识项失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if h.audit != nil {
		h.audit.RecordOK(c, "knowledge", "item_delete", "删除知识项", "knowledge_item", id, nil)
	}
	c.JSON(http.StatusOK, gin.H{"message": i18nText(requestWantsEnglish(c), "删除成功", "deleted successfully")})
}

// StartIndex 构建知识库向量索引。默认仅补齐尚无向量的知识项；mode=full 时全量重建。
func (h *KnowledgeHandler) StartIndex(c *gin.Context) {
	if err := h.indexer.TryBeginIndexRun(); err != nil {
		respondError(c, http.StatusConflict, "已有索引任务正在进行，请等待完成", "an indexing job is already running; wait for it to finish")
		return
	}

	mode := strings.TrimSpace(c.Query("mode"))
	if mode == "" {
		mode = "missing"
	}
	if mode != "full" && mode != "missing" {
		h.indexer.FinishIndexRun()
		respondError(c, http.StatusBadRequest, "无效的 mode 参数，可选值：missing、full", "invalid mode parameter; choose missing or full")
		return
	}

	fullRebuild := mode == "full"
	message := "索引构建已开始，将在后台进行"
	auditAction := "index_build"
	auditDetail := "构建知识库索引"
	if fullRebuild {
		message = "全量索引重建已开始，将在后台进行"
		auditAction = "index_rebuild_full"
		auditDetail = "全量重建知识库索引"
	}

	go func() {
		defer h.indexer.FinishIndexRun()
		ctx := context.Background()
		var err error
		if fullRebuild {
			err = h.indexer.RunRebuildIndex(ctx)
		} else {
			err = h.indexer.RunIndexMissing(ctx)
		}
		if err != nil {
			if fullRebuild {
				h.logger.Error("全量重建索引失败", zap.Error(err))
			} else {
				h.logger.Error("构建知识库索引失败", zap.Error(err))
			}
		}
	}()

	if h.audit != nil {
		h.audit.RecordOK(c, "knowledge", auditAction, auditDetail, "knowledge", "", nil)
	}
	c.JSON(http.StatusOK, gin.H{"message": message, "mode": mode})
}

// ScanKnowledgeBase 扫描知识库
func (h *KnowledgeHandler) ScanKnowledgeBase(c *gin.Context) {
	itemsToIndex, err := h.manager.ScanKnowledgeBase()
	if err != nil {
		h.logger.Error("扫描知识库失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if len(itemsToIndex) == 0 {
		c.JSON(http.StatusOK, gin.H{"message": i18nText(requestWantsEnglish(c), "扫描完成，没有需要索引的新项或更新项", "scan complete; no new or updated items to index")})
		return
	}

	// 异步索引新添加或更新的项（增量索引）
	go func() {
		ctx := context.Background()
		h.logger.Info("开始增量索引", zap.Int("count", len(itemsToIndex)))
		failedCount := 0
		consecutiveFailures := 0
		var firstFailureItemID string
		var firstFailureError error

		for i, itemID := range itemsToIndex {
			if err := h.indexer.IndexItem(ctx, itemID); err != nil {
				failedCount++
				consecutiveFailures++

				// 只在第一个失败时记录详细日志
				if consecutiveFailures == 1 {
					firstFailureItemID = itemID
					firstFailureError = err
					h.logger.Warn("索引知识项失败",
						zap.String("itemId", itemID),
						zap.Int("totalItems", len(itemsToIndex)),
						zap.Error(err),
					)
				}

				// 如果连续失败 2 次，立即停止增量索引
				if consecutiveFailures >= 2 {
					h.logger.Error("连续索引失败次数过多，立即停止增量索引",
						zap.Int("consecutiveFailures", consecutiveFailures),
						zap.Int("totalItems", len(itemsToIndex)),
						zap.Int("processedItems", i+1),
						zap.String("firstFailureItemId", firstFailureItemID),
						zap.Error(firstFailureError),
					)
					break
				}
				continue
			}

			// 成功时重置连续失败计数
			if consecutiveFailures > 0 {
				consecutiveFailures = 0
				firstFailureItemID = ""
				firstFailureError = nil
			}

			// 减少进度日志频率
			if (i+1)%10 == 0 || i+1 == len(itemsToIndex) {
				h.logger.Info("索引进度", zap.Int("current", i+1), zap.Int("total", len(itemsToIndex)), zap.Int("failed", failedCount))
			}
		}
		h.logger.Info("增量索引完成", zap.Int("totalItems", len(itemsToIndex)), zap.Int("failedCount", failedCount))
	}()

	c.JSON(http.StatusOK, gin.H{
		"message":        fmt.Sprintf("扫描完成，开始索引 %d 个新添加或更新的知识项", len(itemsToIndex)),
		"items_to_index": len(itemsToIndex),
	})
}

// GetRetrievalLogs 获取检索日志
func (h *KnowledgeHandler) GetRetrievalLogs(c *gin.Context) {
	conversationID := c.Query("conversationId")
	messageID := c.Query("messageId")
	limit := 50 // 默认 50 条

	if limitStr := c.Query("limit"); limitStr != "" {
		if parsed, err := parseInt(limitStr); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	logs, err := h.manager.GetRetrievalLogs(conversationID, messageID, limit)
	if err != nil {
		h.logger.Error("获取检索日志失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"logs": logs})
}

// DeleteRetrievalLog 删除检索日志
func (h *KnowledgeHandler) DeleteRetrievalLog(c *gin.Context) {
	id := c.Param("id")

	if err := h.manager.DeleteRetrievalLog(id); err != nil {
		h.logger.Error("删除检索日志失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": i18nText(requestWantsEnglish(c), "删除成功", "deleted successfully")})
}

// GetIndexStatus 获取索引状态
func (h *KnowledgeHandler) GetIndexStatus(c *gin.Context) {
	status, err := h.manager.GetIndexStatus()
	if err != nil {
		h.logger.Error("获取索引状态失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 获取索引器的错误信息
	if h.indexer != nil {
		lastError, lastErrorTime := h.indexer.GetLastError()
		// Keep a failed operation visible until the indexer clears it for the next run.
		appendKnowledgeIndexError(status, lastError, lastErrorTime)

		// 获取重建索引状态
		isRebuilding, totalItems, current, failed, lastItemID, lastChunks, startTime := h.indexer.GetRebuildStatus()
		if isRebuilding {
			status["is_rebuilding"] = true
			status["rebuild_total"] = totalItems
			status["rebuild_current"] = current
			status["rebuild_failed"] = failed
			status["rebuild_start_time"] = startTime.Format(time.RFC3339)
			if lastItemID != "" {
				status["rebuild_last_item_id"] = lastItemID
			}
			if lastChunks > 0 {
				status["rebuild_last_chunks"] = lastChunks
			}
			// 重建中时，is_complete 为 false
			status["is_complete"] = false
			// 计算重建进度百分比
			if totalItems > 0 {
				status["progress_percent"] = float64(current) / float64(totalItems) * 100
			}
		}
	}

	c.JSON(http.StatusOK, status)
}

// Search 搜索知识库（用于 API 调用，Agent 内部使用 Retriever）
func (h *KnowledgeHandler) Search(c *gin.Context) {
	var req knowledge.SearchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Retriever.Search 经 Eino VectorEinoRetriever，与 MCP 工具链一致。
	results, err := h.retriever.Search(c.Request.Context(), &req)
	if err != nil {
		h.logger.Error("搜索知识库失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"results": results})
}

// GetStats 获取知识库统计信息
func (h *KnowledgeHandler) GetStats(c *gin.Context) {
	totalCategories, totalItems, err := h.manager.GetStats()
	if err != nil {
		h.logger.Error("获取知识库统计信息失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"enabled":          true,
		"total_categories": totalCategories,
		"total_items":      totalItems,
	})
}

// 辅助函数：解析整数
func parseInt(s string) (int, error) {
	var result int
	_, err := fmt.Sscanf(s, "%d", &result)
	return result, err
}

func appendKnowledgeIndexError(status map[string]interface{}, message string, occurredAt time.Time) {
	if message == "" {
		return
	}
	status["last_error"] = message
	status["last_error_time"] = occurredAt.Format(time.RFC3339)
}
