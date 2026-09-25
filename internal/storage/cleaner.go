package storage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cyberstrike-ai/internal/config"

	"go.uber.org/zap"
)

// ErrCleanupInProgress 表示已有一轮清理在执行；handler 应映射为 409。
var ErrCleanupInProgress = errors.New("已有一轮存储清理正在执行")

// ErrUnknownCategory 表示请求里带了未注册的类别键。
var ErrUnknownCategory = errors.New("未知的清理类别")

// 判定结果原因，用于报表与日志可解释性。
const (
	reasonExpired  = "expired"  // 超过保留期
	reasonOrphan   = "orphan"   // 会话/项目已删除，目录残留
	reasonLeftover = "leftover" // 上一轮清理崩溃残留的标记目录
	reasonActive   = "active"   // 最近有活动，受保护
	reasonRecent   = "recent"   // 尚未达到宽限期
	reasonKept     = "kept"     // 未到期或该类别保留期为 0
	reasonUnknown  = "unknown"  // 活跃状态查询失败，保守跳过
)

// Activity 查询会话/项目最近活动时间。实现方出错时清理会保守跳过该单元。
type Activity interface {
	ConversationLastActivity(id string) (time.Time, bool, error)
	ProjectLastActivity(id string) (time.Time, bool, error)
}

// CleanRequest 描述一次清理请求。
type CleanRequest struct {
	// DryRun 为 true 时只统计不删除。
	DryRun bool `json:"dry_run"`
	// Categories 为空表示全部已启用类别。
	Categories []string `json:"categories"`
	// Trigger 取值 manual / schedule，仅用于审计与日志。
	Trigger string `json:"trigger"`
}

// CategoryReport 是单个类别的统计与执行结果。
type CategoryReport struct {
	Key           string `json:"key"`
	Label         string `json:"label"`
	Hint          string `json:"hint"`
	Root          string `json:"root"`
	Enabled       bool   `json:"enabled"`
	RetentionDays int    `json:"retention_days"`
	// Missing 表示根目录尚未创建（系统还没产生过该类垃圾）。
	Missing bool `json:"missing"`

	Units int   `json:"units"`
	Bytes int64 `json:"bytes"`

	ReclaimableUnits int   `json:"reclaimable_units"`
	ReclaimableBytes int64 `json:"reclaimable_bytes"`
	OrphanUnits      int   `json:"orphan_units"`
	SkippedActive    int   `json:"skipped_active"`
	SkippedUnsafe    int   `json:"skipped_unsafe"`

	RemovedUnits     int   `json:"removed_units"`
	FreedBytes       int64 `json:"freed_bytes"`
	RemovedEmptyDirs int   `json:"removed_empty_dirs"`

	Errors []string `json:"errors,omitempty"`
}

// Totals 是全部类别的汇总。
type Totals struct {
	Units            int   `json:"units"`
	Bytes            int64 `json:"bytes"`
	ReclaimableUnits int   `json:"reclaimable_units"`
	ReclaimableBytes int64 `json:"reclaimable_bytes"`
	RemovedUnits     int   `json:"removed_units"`
	FreedBytes       int64 `json:"freed_bytes"`
	SkippedActive    int   `json:"skipped_active"`
	Errors           int   `json:"errors"`
}

// Report 是一次统计或清理的完整结果。
type Report struct {
	DryRun     bool             `json:"dry_run"`
	Trigger    string           `json:"trigger"`
	StartedAt  time.Time        `json:"started_at"`
	DurationMS int64            `json:"duration_ms"`
	Filesystem Filesystem       `json:"filesystem"`
	Categories []CategoryReport `json:"categories"`
	Totals     Totals           `json:"totals"`
	Note       string           `json:"note,omitempty"`
}

// defaultCacheTTL 限制 status 接口的目录遍历频率：大工作区下一次全量 walk 可能耗时数秒。
const defaultCacheTTL = time.Minute

// Options 构造 Cleaner 所需依赖。
type Options struct {
	Config   *config.Config
	Paths    Paths
	Activity Activity
	Logger   *zap.Logger
	// Now 便于测试注入固定时钟；省略时使用 time.Now。
	Now func() time.Time
	// CacheTTL 省略时使用 defaultCacheTTL；<=0 表示禁用缓存。
	CacheTTL time.Duration
}

// Cleaner 枚举、评估并删除运行空间垃圾。
type Cleaner struct {
	cfg      *config.Config
	paths    Paths
	activity Activity
	logger   *zap.Logger
	now      func() time.Time
	cacheTTL time.Duration

	// running 保证同一时刻只有一轮清理，避免两个管理员同时点「立即清理」互相踩。
	running atomic.Bool

	mu       sync.Mutex
	cached   *Report
	cachedAt time.Time
}

// NewCleaner 创建清理器。cfg 为 nil 时使用零值配置（等价于全部默认策略）。
func NewCleaner(opts Options) *Cleaner {
	c := &Cleaner{
		cfg:      opts.Config,
		paths:    opts.Paths,
		activity: opts.Activity,
		logger:   opts.Logger,
		now:      opts.Now,
		cacheTTL: opts.CacheTTL,
	}
	if c.cfg == nil {
		c.cfg = &config.Config{}
	}
	if c.now == nil {
		c.now = time.Now
	}
	if opts.CacheTTL == 0 {
		c.cacheTTL = defaultCacheTTL
	} else if opts.CacheTTL < 0 {
		c.cacheTTL = 0
	}
	return c
}

// storageConfig 返回当前生效的存储策略（读取时取值，因此 PUT /api/config 后即时生效）。
func (c *Cleaner) storageConfig() config.StorageConfig {
	if c.cfg == nil {
		return config.StorageConfig{}
	}
	return c.cfg.Storage
}

// rootOf 返回类别根目录的绝对路径；未配置时返回空串。
func (c *Cleaner) rootOf(cat category) string {
	root := strings.TrimSpace(cat.root(c.paths))
	if root == "" {
		return ""
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return ""
	}
	return filepath.Clean(abs)
}

// selectCategories 按注册表顺序返回待处理类别，保证报表顺序稳定。
// onlyEnabled 为 true 时跳过被显式关闭的类别。
func (c *Cleaner) selectCategories(keys []string, onlyEnabled bool) ([]category, error) {
	all := categories()
	if len(keys) == 0 {
		out := make([]category, 0, len(all))
		for _, cat := range all {
			if onlyEnabled && !c.storageConfig().CategoryEnabled(cat.key) {
				continue
			}
			out = append(out, cat)
		}
		return out, nil
	}

	wanted := make(map[string]bool, len(keys))
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		wanted[k] = true
	}
	known := make(map[string]bool, len(all))
	for _, cat := range all {
		known[cat.key] = true
	}
	for k := range wanted {
		if !known[k] {
			return nil, fmt.Errorf("%w: %s", ErrUnknownCategory, k)
		}
	}
	out := make([]category, 0, len(wanted))
	for _, cat := range all {
		if !wanted[cat.key] {
			continue
		}
		if onlyEnabled && !c.storageConfig().CategoryEnabled(cat.key) {
			continue
		}
		out = append(out, cat)
	}
	return out, nil
}

// scanResult 是一次类别扫描的产物：报表 + 可删除单元。
type scanResult struct {
	report   CategoryReport
	eligible []Unit
}

// scan 枚举类别下全部单元并逐个判定，同时产出统计与可删除清单。
func (c *Cleaner) scan(cat category, now time.Time) scanResult {
	st := c.storageConfig()
	rep := CategoryReport{
		Key:           cat.key,
		Label:         cat.label,
		Hint:          cat.hint,
		Root:          c.rootOf(cat),
		Enabled:       st.CategoryEnabled(cat.key),
		RetentionDays: st.CategoryRetentionDays(cat.key),
	}
	res := scanResult{report: rep}
	if rep.Root == "" {
		rep.Missing = true
		res.report = rep
		return res
	}
	if _, err := os.Stat(rep.Root); err != nil {
		rep.Missing = true
		res.report = rep
		return res
	}

	units, err := cat.scan(rep.Root)
	if err != nil {
		rep.Errors = append(rep.Errors, err.Error())
		res.report = rep
		return res
	}

	for _, u := range units {
		rep.Units++
		rep.Bytes += u.Size
		// 纵深防御：scanner 只会产出 root 之下的路径，此处再断言一次。
		if !confined(rep.Root, u.Path) {
			rep.SkippedUnsafe++
			continue
		}
		d := c.evaluate(u, rep.RetentionDays, now)
		if !d.eligible {
			if d.reason == reasonActive || d.reason == reasonUnknown {
				rep.SkippedActive++
			}
			continue
		}
		rep.ReclaimableUnits++
		rep.ReclaimableBytes += u.Size
		if d.reason == reasonOrphan {
			rep.OrphanUnits++
		}
		res.eligible = append(res.eligible, u)
	}
	res.report = rep
	return res
}

// decision 是单个单元的判定结果。
type decision struct {
	eligible bool
	reason   string
}

// evaluate 判定单元是否可删除。
// 优先级：崩溃残留标记 > 最近活动保护 > 会话存活状态 > 保留期。
func (c *Cleaner) evaluate(u Unit, retentionDays int, now time.Time) decision {
	// 上一轮清理中途崩溃留下的标记目录：无条件补删。
	// 必须放在最前面，否则它的 mtime 是刚刚改名的时间，会被活跃保护永久挡住。
	if strings.HasSuffix(filepath.Base(u.Path), deletionMarkerSuffix) {
		return decision{eligible: true, reason: reasonLeftover}
	}

	st := c.storageConfig()
	activeGrace := time.Duration(st.ActiveGraceHoursEffective()) * time.Hour
	age := u.Age(now)
	if age < activeGrace {
		return decision{reason: reasonActive}
	}

	retention := time.Duration(retentionDays) * 24 * time.Hour

	if u.Scope != ScopeNone && u.Session != "" && c.activity != nil {
		last, exists, err := c.lastActivity(u)
		if err != nil {
			// 查不到活跃状态时保守跳过：宁可少删，不可误删正在跑的任务数据。
			return decision{reason: reasonUnknown}
		}
		if exists {
			if now.Sub(last) < activeGrace {
				return decision{reason: reasonActive}
			}
			if retentionDays > 0 && age >= retention {
				return decision{eligible: true, reason: reasonExpired}
			}
			return decision{reason: reasonKept}
		}
		// 会话/项目已不存在 → 孤儿目录，按较短的宽限期回收。
		if age >= time.Duration(st.OrphanGraceDaysEffective())*24*time.Hour {
			return decision{eligible: true, reason: reasonOrphan}
		}
		return decision{reason: reasonRecent}
	}

	// retention_days: 0 表示不按保留期清理（沿用本项目既有约定）。
	if retentionDays <= 0 {
		return decision{reason: reasonKept}
	}
	if age >= retention {
		return decision{eligible: true, reason: reasonExpired}
	}
	return decision{reason: reasonKept}
}

func (c *Cleaner) lastActivity(u Unit) (time.Time, bool, error) {
	switch u.Scope {
	case ScopeConversation:
		return c.activity.ConversationLastActivity(u.Session)
	case ScopeProject:
		return c.activity.ProjectLastActivity(u.Session)
	default:
		return time.Time{}, false, nil
	}
}

// Inspect 统计全部类别的占用与可回收量，不删除任何文件。
// 结果按 CacheTTL 缓存，refresh 为 true 时强制重算。
func (c *Cleaner) Inspect(refresh bool) *Report {
	c.mu.Lock()
	if !refresh && c.cacheTTL > 0 && c.cached != nil && c.now().Sub(c.cachedAt) < c.cacheTTL {
		cached := c.cached
		c.mu.Unlock()
		return cached
	}
	c.mu.Unlock()

	rep := c.buildReport(CleanRequest{DryRun: true, Trigger: "inspect"}, true)

	c.mu.Lock()
	c.cached = rep
	c.cachedAt = c.now()
	c.mu.Unlock()
	return rep
}

// invalidateCache 让下一次 Inspect 重新遍历。
func (c *Cleaner) invalidateCache() {
	c.mu.Lock()
	c.cached = nil
	c.mu.Unlock()
}

// Clean 执行一次清理；DryRun 为 true 时只统计。
// 只处理已启用的类别，且同一时刻只允许一轮执行。
func (c *Cleaner) Clean(req CleanRequest) (*Report, error) {
	if !c.running.CompareAndSwap(false, true) {
		return nil, ErrCleanupInProgress
	}
	defer c.running.Store(false)

	req.Trigger = strings.TrimSpace(req.Trigger)
	if req.Trigger == "" {
		req.Trigger = "manual"
	}
	if _, err := c.selectCategories(req.Categories, false); err != nil {
		return nil, err
	}
	rep := c.buildReport(req, false)
	c.invalidateCache()

	if !req.DryRun {
		c.logClean(rep, req)
	}
	return rep, nil
}

// buildReport 是 Inspect 与 Clean 的共用主体。
// inspectAll 为 true 时统计全部类别（含被关闭的），供状态页展示；
// 为 false 时只处理已启用类别并真正执行删除。
func (c *Cleaner) buildReport(req CleanRequest, inspectAll bool) *Report {
	startedAt := c.now()
	cats, err := c.selectCategories(req.Categories, !inspectAll)
	rep := &Report{
		DryRun:    req.DryRun,
		Trigger:   req.Trigger,
		StartedAt: startedAt,
	}
	if err != nil {
		rep.Note = err.Error()
		rep.Categories = []CategoryReport{}
		return rep
	}

	for _, cat := range cats {
		res := c.scan(cat, startedAt)
		cr := res.report

		if !req.DryRun && cr.Enabled {
			root := cr.Root
			for _, u := range res.eligible {
				if rmErr := removeUnit(u); rmErr != nil {
					cr.Errors = append(cr.Errors, fmt.Sprintf("%s: %v", filepath.Base(u.Path), rmErr))
					continue
				}
				cr.RemovedUnits++
				cr.FreedBytes += u.Size
			}
			if cat.pruneEmpty && cr.RemovedUnits > 0 && root != "" {
				// 日期层 + 会话层，最多两层。
				cr.RemovedEmptyDirs = pruneEmptyDirs(root, 2)
			}
		}

		rep.Categories = append(rep.Categories, cr)
	}
	if rep.Categories == nil {
		rep.Categories = []CategoryReport{}
	}

	for _, cr := range rep.Categories {
		rep.Totals.Units += cr.Units
		rep.Totals.Bytes += cr.Bytes
		rep.Totals.ReclaimableUnits += cr.ReclaimableUnits
		rep.Totals.ReclaimableBytes += cr.ReclaimableBytes
		rep.Totals.RemovedUnits += cr.RemovedUnits
		rep.Totals.FreedBytes += cr.FreedBytes
		rep.Totals.SkippedActive += cr.SkippedActive
		rep.Totals.Errors += len(cr.Errors)
	}

	rep.Filesystem = c.filesystem()
	rep.DurationMS = time.Since(startedAt).Milliseconds()
	return rep
}

// filesystem 取第一个存在的类别根目录所在文件系统，作为概览卡片的容量来源。
func (c *Cleaner) filesystem() Filesystem {
	probe := ""
	for _, cat := range categories() {
		if root := c.rootOf(cat); root != "" {
			probe = root
			break
		}
	}
	if probe == "" {
		probe = "."
	}
	fs, err := FilesystemUsage(probe)
	if err != nil && c.logger != nil {
		c.logger.Debug("查询文件系统容量失败", zap.String("path", probe), zap.Error(err))
	}
	return fs
}

// removeUnit 删除单元。目录先原子改名再递归删除：
// 中途崩溃只会留下带 deletionMarkerSuffix 的目录，下一轮 evaluate 会无条件补删。
func removeUnit(u Unit) error {
	if u.IsDir {
		marker := u.Path + deletionMarkerSuffix
		if err := os.Rename(u.Path, marker); err == nil {
			return ignoreMissing(os.RemoveAll(marker))
		}
		// 改名失败（跨设备、权限、同名残留）时退化为直接删除。
	}
	return ignoreMissing(os.RemoveAll(u.Path))
}

func ignoreMissing(err error) error {
	if err == nil || os.IsNotExist(err) {
		return nil
	}
	return err
}

func (c *Cleaner) logClean(rep *Report, req CleanRequest) {
	if c.logger == nil {
		return
	}
	summary := []zap.Field{
		zap.String("trigger", req.Trigger),
		zap.Int("removed_units", rep.Totals.RemovedUnits),
		zap.Int64("freed_bytes", rep.Totals.FreedBytes),
		zap.Int("skipped_active", rep.Totals.SkippedActive),
		zap.Int("errors", rep.Totals.Errors),
	}
	if rep.Totals.RemovedUnits == 0 && rep.Totals.Errors == 0 {
		c.logger.Debug("运行空间清理完成，无可回收内容", summary...)
		return
	}
	c.logger.Info("运行空间清理完成", summary...)
	for _, cr := range rep.Categories {
		if cr.RemovedUnits == 0 && len(cr.Errors) == 0 {
			continue
		}
		c.logger.Info("清理类别明细",
			zap.String("category", cr.Key),
			zap.Int("removed", cr.RemovedUnits),
			zap.Int64("freed_bytes", cr.FreedBytes),
			zap.Strings("errors", cr.Errors))
	}
}
