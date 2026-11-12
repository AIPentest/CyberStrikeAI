package storage

import (
	"errors"
	"time"

	"cyberstrike-ai/internal/config"

	"go.uber.org/zap"
)

// minSweepInterval is the lower bound for background cleanup, to avoid continuously walking the disk after interval_minutes is configured too small.
const minSweepInterval = 5 * time.Minute

// sweepCheckInterval is the wakeup granularity of the background loop.
// Cannot sleep for a whole interval directly: that way, after changing interval_minutes from 60 to 5,
// would only take effect after the current 60-minute sleep ends. Short-granularity wakeups + The due check limits the delay to within one granularity.
const sweepCheckInterval = time.Minute

// Service Drives background automatic cleanup.
type Service struct {
	cleaner *Cleaner
	cfg     *config.Config
	logger  *zap.Logger
}

// NewService Creates the background cleanup service.
func NewService(cleaner *Cleaner, cfg *config.Config, logger *zap.Logger) *Service {
	return &Service{cleaner: cleaner, cfg: cfg, logger: logger}
}

// AutoCleanEnabled Returns whether background automatic cleanup is enabled (disabled by default).
func (s *Service) AutoCleanEnabled() bool {
	if s == nil || s.cfg == nil {
		return false
	}
	return s.cfg.Storage.AutoCleanEffective()
}

// Interval Returns the cleanup interval; re-read each round, so changing the config needs no restart.
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

// PurgeExpired Runs one round of automatic cleanup; returns directly when automatic cleanup is not enabled.
func (s *Service) PurgeExpired() {
	if s == nil || s.cleaner == nil || !s.AutoCleanEnabled() {
		return
	}
	if _, err := s.cleaner.Clean(CleanRequest{Trigger: "schedule"}); err != nil {
		if s.logger != nil {
			// A concurrency conflict is not a fault: it means a round is already running, so just skip.
			if errors.Is(err, ErrCleanupInProgress) {
				s.logger.Debug("A storage cleanup is already running; skipping this round")
				return
			}
			s.logger.Warn("Automatic runtime-workspace cleanup failed", zap.Error(err))
		}
	}
}

// StartRetentionLoop Periodically cleans up runtime-workspace garbage at the configured interval.
// Wakes at sweepCheckInterval granularity and only runs when due, so a change to interval_minutes takes effect at most one granularity later.
func StartRetentionLoop(s *Service, logger *zap.Logger) {
	if s == nil || s.cleaner == nil {
		return
	}
	// Runs one round right after startup, to avoid 'configured but having to wait an interval before it takes effect'";
	// Placed in a goroutine so the full directory walk does not block the startup flow.
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
