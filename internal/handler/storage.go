package handler

import (
	"errors"
	"net/http"
	"strconv"

	"cyberstrike-ai/internal/audit"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/storage"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// StorageHandler 提供运行空间占用统计与垃圾清理 API。
type StorageHandler struct {
	cleaner *storage.Cleaner
	cfg     *config.Config
	audit   *audit.Service
	logger  *zap.Logger
}

// NewStorageHandler 创建存储清理 handler。
func NewStorageHandler(cleaner *storage.Cleaner, cfg *config.Config, logger *zap.Logger) *StorageHandler {
	return &StorageHandler{cleaner: cleaner, cfg: cfg, logger: logger}
}

// SetAudit wires platform audit logging.
func (h *StorageHandler) SetAudit(s *audit.Service) {
	if h != nil {
		h.audit = s
	}
}

// storageCleanupRequest 是 POST /api/storage/cleanup 的请求体。
type storageCleanupRequest struct {
	// DryRun 省略时按 true 处理：只统计不删除。真正删除必须显式传 false。
	DryRun *bool `json:"dry_run"`
	// Confirm 为 false 时即使 dry_run=false 也拒绝执行。
	// 磁盘删除不可逆，确认必须是 API 层的显式动作，而不只依赖前端弹窗。
	Confirm    bool     `json:"confirm"`
	Categories []string `json:"categories"`
}

// Meta GET /api/storage/meta 返回清理策略与各类别元信息。
func (h *StorageHandler) Meta(c *gin.Context) {
	st := h.effectiveConfig()
	items := make([]gin.H, 0, len(config.StorageCategoryOrder))
	for _, info := range storage.DescribeCategories() {
		items = append(items, gin.H{
			"key":               info.Key,
			"label":             info.Label,
			"hint":              info.Hint,
			"enabled":           st.CategoryEnabled(info.Key),
			"retention_days":    st.CategoryRetentionDays(info.Key),
			"default_retention": config.StorageCategoryDefaults[info.Key],
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"auto_clean":         st.AutoCleanEffective(),
		"interval_minutes":   st.IntervalMinutesEffective(),
		"orphan_grace_days":  st.OrphanGraceDaysEffective(),
		"active_grace_hours": st.ActiveGraceHoursEffective(),
		"categories":         items,
	})
}

// Status GET /api/storage/status 返回文件系统容量与各类别占用/可回收量。
// ?refresh=1 强制重新遍历目录，否则使用短 TTL 缓存。
func (h *StorageHandler) Status(c *gin.Context) {
	if h.cleaner == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "存储清理未初始化"})
		return
	}
	refresh, _ := strconv.ParseBool(c.Query("refresh"))
	rep := h.cleaner.Inspect(refresh)
	c.JSON(http.StatusOK, gin.H{
		"filesystem":  rep.Filesystem,
		"categories":  rep.Categories,
		"totals":      rep.Totals,
		"scanned_at":  rep.StartedAt,
		"duration_ms": rep.DurationMS,
	})
}

// Cleanup POST /api/storage/cleanup 执行清理（或预览）。
func (h *StorageHandler) Cleanup(c *gin.Context) {
	if h.cleaner == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "存储清理未初始化"})
		return
	}
	var req storageCleanupRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "无效的请求参数: " + err.Error()})
			return
		}
	}
	dryRun := req.DryRun == nil || *req.DryRun
	if !dryRun && !req.Confirm {
		c.JSON(http.StatusBadRequest, gin.H{"error": "删除不可逆，执行真实清理必须同时传 dry_run=false 与 confirm=true"})
		return
	}

	rep, err := h.cleaner.Clean(storage.CleanRequest{
		DryRun:     dryRun,
		Categories: req.Categories,
		Trigger:    "manual",
	})
	switch {
	case errors.Is(err, storage.ErrCleanupInProgress):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	case errors.Is(err, storage.ErrUnknownCategory):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if !dryRun {
		h.recordCleanup(c, rep)
	}
	c.JSON(http.StatusOK, gin.H{
		"dry_run":     rep.DryRun,
		"filesystem":  rep.Filesystem,
		"categories":  rep.Categories,
		"totals":      rep.Totals,
		"duration_ms": rep.DurationMS,
	})
}

func (h *StorageHandler) recordCleanup(c *gin.Context, rep *storage.Report) {
	if h.audit == nil {
		return
	}
	detail := map[string]interface{}{
		"removed_units": rep.Totals.RemovedUnits,
		"freed_bytes":   rep.Totals.FreedBytes,
		"errors":        rep.Totals.Errors,
	}
	if rep.Totals.Errors > 0 {
		h.audit.RecordFail(c, "storage", "cleanup", "运行空间清理完成但存在失败项", detail)
		return
	}
	h.audit.RecordOK(c, "storage", "cleanup", "清理运行空间垃圾", "storage", "", detail)
}

func (h *StorageHandler) effectiveConfig() config.StorageConfig {
	if h.cfg == nil {
		return config.StorageConfig{}
	}
	return h.cfg.Storage
}
