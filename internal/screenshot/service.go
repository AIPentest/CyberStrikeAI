// Package screenshot 负责「站点截图」的排队、调用外部渲染服务与落库。
//
// 真正的浏览器渲染由独立的本地 Node 服务（Playwright + Chromium）完成：
// 主服务以 root 运行，不应该自己去渲染不可信的目标站点；把浏览器放到
// 非 root 的独立进程里，并通过 localhost HTTP 调用，是这里的安全边界。
package screenshot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"cyberstrike-ai/internal/database"

	"go.uber.org/zap"
)

// Options 截图服务参数（由 config.ScreenshotConfig 换算而来）。
type Options struct {
	ServiceURL     string
	Dir            string
	Timeout        time.Duration
	Concurrency    int
	ViewportWidth  int
	ViewportHeight int
	MinInterval    time.Duration
}

type job struct {
	assetID string
	url     string
}

// Service 截图调度器：有界队列 + 固定并发 worker。
type Service struct {
	opts   Options
	db     *database.DB
	logger *zap.Logger
	client *http.Client

	jobs    chan job
	mu      sync.Mutex
	queued  map[string]bool // 已排队/执行中的 assetID，避免重复入队
	closed  bool
	workers sync.WaitGroup
	cancel  context.CancelFunc
}

const jobQueueSize = 512

// New 构造截图服务。db 为 nil 或未启用时返回 nil。
func New(opts Options, db *database.DB, logger *zap.Logger) *Service {
	if db == nil || logger == nil || !opts.Enabled() {
		return nil
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = 2
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 30 * time.Second
	}
	return &Service{
		opts:   opts,
		db:     db,
		logger: logger,
		client: &http.Client{Timeout: opts.Timeout + 10*time.Second},
		jobs:   make(chan job, jobQueueSize),
		queued: make(map[string]bool),
	}
}

// Enabled 判断是否配置了有效的服务地址。
func (o Options) Enabled() bool {
	return strings.TrimSpace(o.ServiceURL) != ""
}

// Start 启动 worker。ctx 取消时所有 worker 退出。
func (s *Service) Start(ctx context.Context) {
	if s == nil {
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	for i := 0; i < s.opts.Concurrency; i++ {
		s.workers.Add(1)
		go func(index int) {
			defer s.workers.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case j, ok := <-s.jobs:
					if !ok {
						return
					}
					s.capture(j)
					s.mu.Lock()
					delete(s.queued, j.assetID)
					s.mu.Unlock()
				}
			}
		}(i)
	}
	s.logger.Info("站点截图服务已启动",
		zap.String("service", s.opts.ServiceURL),
		zap.String("dir", s.opts.Dir),
		zap.Int("concurrency", s.opts.Concurrency))
}

// Stop 停止接收新任务并等待 worker 退出。
func (s *Service) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	close(s.jobs)
	s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
	s.workers.Wait()
}

// Healthy 探测外部截图服务是否可用。
func (s *Service) Healthy(ctx context.Context) bool {
	if s == nil {
		return false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(s.opts.ServiceURL, "/")+"/health", nil)
	if err != nil {
		return false
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// Enqueue 把资产加入截图队列。
//
// force=false 时会跳过「最近已成功截图」的资产（按 MinInterval 判断）；
// 返回 true 表示任务已入队。
func (s *Service) Enqueue(assetID, url string, force bool) bool {
	if s == nil {
		return false
	}
	assetID = strings.TrimSpace(assetID)
	url = strings.TrimSpace(url)
	if assetID == "" || url == "" {
		return false
	}
	if !force && s.recentlyCaptured(assetID) {
		return false
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return false
	}
	if s.queued[assetID] {
		s.mu.Unlock()
		return false
	}
	s.queued[assetID] = true
	s.mu.Unlock()

	// 先落一条 pending 记录，前端可以立刻显示「排队中」。
	if err := s.db.SaveAssetScreenshot(&database.AssetScreenshot{
		AssetID: assetID,
		Status:  database.AssetScreenshotStatusPending,
	}); err != nil {
		s.logger.Warn("截图：写入排队状态失败", zap.String("asset_id", assetID), zap.Error(err))
	}

	select {
	case s.jobs <- job{assetID: assetID, url: url}:
		return true
	default:
		// 队列满：回滚排队标记，避免永久占位。
		s.mu.Lock()
		delete(s.queued, assetID)
		s.mu.Unlock()
		s.logger.Warn("截图队列已满，丢弃任务", zap.String("asset_id", assetID), zap.String("url", url))
		return false
	}
}

func (s *Service) recentlyCaptured(assetID string) bool {
	if s.opts.MinInterval <= 0 {
		return false
	}
	existing, err := s.db.GetAssetScreenshot(assetID)
	if err != nil || existing == nil {
		return false
	}
	if existing.Status != database.AssetScreenshotStatusDone || existing.CapturedAt == nil {
		return false
	}
	return time.Since(*existing.CapturedAt) < s.opts.MinInterval
}

// capture 同步执行一次截图（在 worker 内调用）。
func (s *Service) capture(j job) {
	if err := s.db.MarkAssetScreenshotRunning(j.assetID); err != nil {
		s.logger.Warn("截图：写入 running 状态失败", zap.String("asset_id", j.assetID), zap.Error(err))
	}

	outPath := filepath.Join(s.opts.Dir, j.assetID+".png")
	if abs, err := filepath.Abs(outPath); err == nil {
		outPath = abs
	}

	reqBody, _ := json.Marshal(map[string]interface{}{
		"url":        j.url,
		"path":       outPath,
		"timeout_ms": int(s.opts.Timeout / time.Millisecond),
		"width":      s.opts.ViewportWidth,
		"height":     s.opts.ViewportHeight,
	})
	var respBody bytes.Buffer
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(s.opts.ServiceURL, "/")+"/shot", bytes.NewReader(reqBody))
	if err != nil {
		s.fail(j.assetID, "构造截图请求失败: "+err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		s.fail(j.assetID, "调用截图服务失败: "+err.Error())
		return
	}
	defer resp.Body.Close()
	if _, err := respBody.ReadFrom(resp.Body); err != nil {
		s.fail(j.assetID, "读取截图服务响应失败: "+err.Error())
		return
	}

	var parsed struct {
		OK         bool   `json:"ok"`
		Path       string `json:"path"`
		StatusCode int    `json:"status_code"`
		Title      string `json:"title"`
		Width      int    `json:"width"`
		Height     int    `json:"height"`
		Error      string `json:"error"`
	}
	if err := json.Unmarshal(respBody.Bytes(), &parsed); err != nil {
		s.fail(j.assetID, fmt.Sprintf("解析截图服务响应失败（HTTP %d）", resp.StatusCode))
		return
	}
	if !parsed.OK || parsed.Error != "" {
		msg := parsed.Error
		if msg == "" {
			msg = fmt.Sprintf("截图服务返回失败（HTTP %d）", resp.StatusCode)
		}
		s.fail(j.assetID, msg)
		return
	}

	now := time.Now()
	record := &database.AssetScreenshot{
		AssetID:    j.assetID,
		Status:     database.AssetScreenshotStatusDone,
		FilePath:   outPath,
		StatusCode: parsed.StatusCode,
		Title:      parsed.Title,
		Width:      parsed.Width,
		Height:     parsed.Height,
		CapturedAt: &now,
	}
	if err := s.db.SaveAssetScreenshot(record); err != nil {
		s.logger.Warn("截图：保存结果失败", zap.String("asset_id", j.assetID), zap.Error(err))
		return
	}
	s.logger.Info("站点截图完成",
		zap.String("asset_id", j.assetID),
		zap.String("url", j.url),
		zap.Int("status_code", parsed.StatusCode))
}

func (s *Service) fail(assetID, message string) {
	if err := s.db.SaveAssetScreenshot(&database.AssetScreenshot{
		AssetID: assetID,
		Status:  database.AssetScreenshotStatusFailed,
		Error:   message,
	}); err != nil {
		s.logger.Warn("截图：保存失败状态失败", zap.String("asset_id", assetID), zap.Error(err))
	}
	s.logger.Warn("站点截图失败", zap.String("asset_id", assetID), zap.String("error", message))
}

// FilePath 返回某个资产截图文件的落盘路径（仅用于校验与下发）。
func (s *Service) FilePath(assetID string) string {
	return filepath.Join(s.opts.Dir, strings.TrimSpace(assetID)+".png")
}

// Dir 返回截图目录。
func (s *Service) Dir() string { return s.opts.Dir }

// BuildURL 把资产的主机/端口/协议拼成可截图的 URL。
// 非 http/https 协议返回空字符串。
func BuildURL(protocol, host string, port int) string {
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	host = strings.TrimSpace(host)
	if host == "" || (protocol != "http" && protocol != "https") {
		return ""
	}
	if strings.Contains(host, "://") {
		return host
	}
	defaultPort := 80
	if protocol == "https" {
		defaultPort = 443
	}
	if port > 0 && port != defaultPort {
		return protocol + "://" + host + ":" + strconv.Itoa(port)
	}
	return protocol + "://" + host
}

// EnsureDir 预创建截图目录（供装配层在启动时调用）。
func EnsureDir(dir string) error {
	if strings.TrimSpace(dir) == "" {
		return nil
	}
	return os.MkdirAll(dir, 0o755)
}
