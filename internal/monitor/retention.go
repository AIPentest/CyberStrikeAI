package monitor

import (
	"time"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"

	"go.uber.org/zap"
)

const retentionPurgeInterval = time.Hour

// Service manages MCP tool execution monitor retention.
type Service struct {
	db     *database.DB
	cfg    *config.Config
	logger *zap.Logger
}

// NewService creates a monitor retention service.
func NewService(db *database.DB, cfg *config.Config, logger *zap.Logger) *Service {
	return &Service{db: db, cfg: cfg, logger: logger}
}

// RetentionDays returns configured retention; 0 means keep forever.
func (s *Service) RetentionDays() int {
	if s == nil || s.cfg == nil {
		return config.MonitorConfig{}.RetentionDaysEffective()
	}
	return s.cfg.Monitor.RetentionDaysEffective()
}

// PurgeExpired deletes tool execution rows older than retention_days when configured.
func (s *Service) PurgeExpired() {
	if s == nil || s.db == nil || s.cfg == nil {
		return
	}
	days := s.cfg.Monitor.RetentionDaysEffective()
	if days <= 0 {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -days)
	n, err := s.db.PurgeToolExecutionsBefore(cutoff)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("Failed to clean up expired MCP execution records", zap.Error(err))
		}
		return
	}
	if n > 0 && s.logger != nil {
		s.logger.Info("Cleaned up expired MCP execution records", zap.Int64("deleted", n), zap.Int("retention_days", days))
	}
}

// StartRetentionLoop periodically purges expired tool execution rows.
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
				logger.Debug("monitor retention tick completed")
			}
		}
	}()
}
