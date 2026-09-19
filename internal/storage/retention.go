package storage

import (
	"errors"
	"time"

	"cyberstrike-ai/internal/config"

	"go.uber.org/zap"
)

// minSweepInterval 是后台清理的下限，避免把 interval_minutes 配成极小值后持续遍历磁盘。
const minSweepInterval = 5 * time.Minute

// sweepCheckInterval 是后台循环的唤醒粒度。
// 不能按 interval 直接睡一整段：那样把 interval_minutes 从 60 改成 5 后，
// 必须等当前这段 60 分钟睡眠结束才生效。短粒度唤醒 + 到期判断把延迟限制在一个粒度内。
const sweepCheckInterval = time.Minute

// Service 驱动后台自动清理。
type Service struct {
	cleaner *Cleaner
	cfg     *config.Config
	logger  *zap.Logger
}

// NewService 创建后台清理服务。
func NewService(cleaner *Cleaner, cfg *config.Config, logger *zap.Logger) *Service {
	return &Service{cleaner: cleaner, cfg: cfg, logger: logger}
}

// AutoCleanEnabled 返回后台自动清理是否开启（默认关闭）。
func (s *Service) AutoCleanEnabled() bool {
	if s == nil || s.cfg == nil {
		return false
	}
	return s.cfg.Storage.AutoCleanEffective()
}

// Interval 返回清理间隔；每轮重新读取，因此改配置无需重启。
func (s *Service) Interval() time.Duration {
	if s == nil || s.cfg == nil {
		return minSweepInterval
	}
	d := time.Duration(s.cfg.Storage.IntervalMinutesEffective()) * time.Minute
	if d < minSweepInterval {
		return minSweepInterval
	}
	return d
}

// PurgeExpired 执行一轮自动清理；未开启自动清理时直接返回。
func (s *Service) PurgeExpired() {
	if s == nil || s.cleaner == nil || !s.AutoCleanEnabled() {
		return
	}
	if _, err := s.cleaner.Clean(CleanRequest{Trigger: "schedule"}); err != nil {
		if s.logger != nil {
			// 并发冲突不是故障：说明已有一轮在跑，跳过即可。
			if errors.Is(err, ErrCleanupInProgress) {
				s.logger.Debug("已有存储清理在执行，跳过本轮")
				return
			}
			s.logger.Warn("运行空间自动清理失败", zap.Error(err))
		}
	}
}

// StartRetentionLoop 按配置间隔周期性清理运行空间垃圾。
// 以 sweepCheckInterval 粒度唤醒、到期才执行，因此 interval_minutes 的改动最多一个粒度后生效。
func StartRetentionLoop(s *Service, logger *zap.Logger) {
	if s == nil || s.cleaner == nil {
		return
	}
	// 启动后先跑一轮，避免「配置好了但要等一个间隔才见效」；
	// 放在 goroutine 里，全量目录遍历不阻塞启动流程。
	go func() {
		s.PurgeExpired()
		last := time.Now()
		ticker := time.NewTicker(sweepCheckInterval)
		defer ticker.Stop()
		for range ticker.C {
			if time.Since(last) < s.Interval() {
				continue
			}
			last = time.Now()
			s.PurgeExpired()
			if logger != nil {
				logger.Debug("storage retention tick completed")
			}
		}
	}()
}
