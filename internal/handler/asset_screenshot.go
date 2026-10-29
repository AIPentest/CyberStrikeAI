package handler

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/screenshot"
	"go.uber.org/zap"

	"github.com/gin-gonic/gin"
)

// 站点截图相关接口。
//
// 路由刻意用 query 参数（?id=）而不是 `/assets/:id/screenshot`：
// 资产路由的 GET 树里已经有 /assets/tree、/assets/table、/assets/stats 等静态段，
// 再引入 :id 参数段会带来路由冲突风险，用 query 更稳。

// assetScreenshotInfo 是下发给前端的截图元信息。
type assetScreenshotInfo struct {
	Status     string     `json:"status"`
	URL        string     `json:"url"`
	Error      string     `json:"error"`
	Width      int        `json:"width"`
	Height     int        `json:"height"`
	CapturedAt *time.Time `json:"captured_at,omitempty"`
}

// SetScreenshotService 注入截图调度器（由装配层调用）。
func (h *AssetHandler) SetScreenshotService(service *screenshot.Service) {
	h.shots = service
}

func screenshotInfoFor(assetID string, item *database.AssetScreenshot, ready bool) assetScreenshotInfo {
	info := assetScreenshotInfo{Status: "none"}
	if !ready {
		return info
	}
	info.Status = "disabled"
	if item == nil {
		info.Status = "none"
		return info
	}
	info.Status = item.Status
	info.Error = item.Error
	info.Width = item.Width
	info.Height = item.Height
	info.CapturedAt = item.CapturedAt
	if item.Status == database.AssetScreenshotStatusDone {
		// 走带鉴权的接口取图，前端用 fetch + blob 加载。
		info.URL = "/api/assets/screenshot?id=" + assetID
	}
	return info
}

// GetScreenshot 下发截图文件（需要登录态，避免截图外泄）。
func (h *AssetHandler) GetScreenshot(c *gin.Context) {
	if h.shots == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "站点截图功能未启用"})
		return
	}
	assetID := strings.TrimSpace(c.Query("id"))
	if assetID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 id 参数"})
		return
	}
	meta, err := h.db.GetAssetScreenshot(assetID)
	if err != nil {
		h.logger.Error("截图：读取元信息失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取截图失败"})
		return
	}
	if meta == nil || meta.Status != database.AssetScreenshotStatusDone || meta.FilePath == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "该资产尚无截图"})
		return
	}
	path, ok := safeScreenshotPath(h.shots.Dir(), meta.FilePath)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "截图文件路径非法"})
		return
	}
	if _, err := os.Stat(path); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "截图文件不存在"})
		return
	}
	c.Header("Cache-Control", "private, max-age=300")
	c.Header("Content-Type", "image/png")
	c.File(path)
}

// CaptureScreenshot 手动触发一次截图（强制重截，忽略最小间隔）。
func (h *AssetHandler) CaptureScreenshot(c *gin.Context) {
	if h.shots == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "站点截图功能未启用（截图服务不可用）"})
		return
	}
	assetID := strings.TrimSpace(c.Query("id"))
	if assetID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 id 参数"})
		return
	}
	asset, err := h.db.GetAsset(assetID, assetAccess(c))
	if err != nil || asset == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "资产不存在或无权访问"})
		return
	}
	target := screenshot.BuildURL(asset.Protocol, asset.Host, asset.Port)
	if target == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "只有 http/https 站点支持截图"})
		return
	}
	if !h.shots.Enqueue(assetID, target, true) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "截图任务未能入队（队列已满或服务不可用）"})
		return
	}
	meta, _ := h.db.GetAssetScreenshot(assetID)
	c.JSON(http.StatusAccepted, gin.H{"screenshot": screenshotInfoFor(assetID, meta, true)})
}

// ScreenshotStatus 查询单个资产的截图状态（前端轮询用）。
func (h *AssetHandler) ScreenshotStatus(c *gin.Context) {
	assetID := strings.TrimSpace(c.Query("id"))
	if assetID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 id 参数"})
		return
	}
	meta, err := h.db.GetAssetScreenshot(assetID)
	if err != nil {
		h.logger.Error("截图：读取状态失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取截图状态失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"screenshot": screenshotInfoFor(assetID, meta, h.shots != nil)})
}

// safeScreenshotPath 校验落盘路径确实在截图目录内，避免目录穿越。
func safeScreenshotPath(dir, path string) (string, bool) {
	if strings.TrimSpace(dir) == "" || strings.TrimSpace(path) == "" {
		return "", false
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", false
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(absDir, absPath)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return "", false
	}
	return absPath, true
}
