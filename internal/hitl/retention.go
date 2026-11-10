package hitl

import (
	"time"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"

	"go.uber.org/zap"
)

const retentionPurgeInterval = time.Hour

// Service manages HITL audit log retention (decided hitl_interrupts rows).
type Service struct {
	db     *database.DB
	cfg    *config.Config
	logger *zap.Logger
}

// NewService creates a HITL audit log retention service.
func NewService(db *database.DB, cfg *config.Config, logger *zap.Logger) *Service {
	return &Service{db: db, cfg: cfg, logger: logger}
}

// RetentionDays returns configured retention; 0 means keep forever.
func (s *Service) RetentionDays() int {
	if s == nil || s.cfg == nil {
		return config.HitlConfig{}.RetentionDaysEffective()
	}
	return s.cfg.Hitl.RetentionDaysEffective()
}

// PurgeExpired deletes decided HITL log rows older than retention_days when configured.
func (s *Service) PurgeExpired() {
	if s == nil || s.db == nil || s.cfg == nil {
		return
	}
	days := s.cfg.Hitl.RetentionDaysEffective()
	if days <= 0 {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -days)
	n, err := s.db.PurgeHitlInterruptLogsBefore(cutoff)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("Failed to clean up expired human-in-the-loop audit logs", zap.Error(err))
		}
		return
	}
	if n > 0 && s.logger != nil {
		s.logger.Info("Cleaned up expired human-in-the-loop audit logs", zap.Int64("deleted", n), zap.Int("retention_days", days))
	}
}

// StartRetentionLoop periodically purges expired HITL audit log rows.
func StartRetentionLoop(s *Service, logger *zap.Logger) {
	if s == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(retentionPurgeInterval)
		defer ticker.Stop()
		for range ticker.C {
			s.PurgeExpired()
			if logger != nil {
				logger.Debug("hitl audit log retention tick completed")
			}
		}
	}()
}
