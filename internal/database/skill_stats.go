package database

import (
	"database/sql"
	"time"

	"go.uber.org/zap"
)

// SkillStats Skills统计信息
type SkillStats struct {
	SkillName    string
	TotalCalls   int
	SuccessCalls int
	FailedCalls  int
	LastCallTime *time.Time
}

// SaveSkillStats 保存Skills统计信息
func (db *DB) SaveSkillStats(skillName string, stats *SkillStats) error {
	var lastCallTime sql.NullTime
	if stats.LastCallTime != nil {
		lastCallTime = sql.NullTime{Time: *stats.LastCallTime, Valid: true}
	}

	query := `
		INSERT OR REPLACE INTO skill_stats 
		(skill_name, total_calls, success_calls, failed_calls, last_call_time, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`

	_, err := db.Exec(query,
		skillName,
		stats.TotalCalls,
		stats.SuccessCalls,
		stats.FailedCalls,
		lastCallTime,
		time.Now(),
	)

	if err != nil {
		db.logger.Error("failed to save skill stats", zap.Error(err), zap.String("skillName", skillName))
		return err
	}

	return nil
}

// LoadSkillStats 加载所有Skills统计信息
func (db *DB) LoadSkillStats() (map[string]*SkillStats, error) {
	query := `
		SELECT skill_name, total_calls, success_calls, failed_calls, last_call_time
		FROM skill_stats
	`

	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	stats := make(map[string]*SkillStats)
	for rows.Next() {
		var stat SkillStats
		var lastCallTime sql.NullTime

		err := rows.Scan(
			&stat.SkillName,
			&stat.TotalCalls,
			&stat.SuccessCalls,
			&stat.FailedCalls,
			&lastCallTime,
		)
		if err != nil {
			db.logger.Warn("failed to load skill stats", zap.Error(err))
			continue
		}

		if lastCallTime.Valid {
			stat.LastCallTime = &lastCallTime.Time
		}

		stats[stat.SkillName] = &stat
	}

	return stats, nil
}

// UpdateSkillStats 更新Skills统计信息（累加模式）
func (db *DB) UpdateSkillStats(skillName string, totalCalls, successCalls, failedCalls int, lastCallTime *time.Time) error {
	var lastCallTimeSQL sql.NullTime
	if lastCallTime != nil {
		lastCallTimeSQL = sql.NullTime{Time: *lastCallTime, Valid: true}
	}

	query := `
		INSERT INTO skill_stats (skill_name, total_calls, success_calls, failed_calls, last_call_time, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(skill_name) DO UPDATE SET
			total_calls = total_calls + ?,
			success_calls = success_calls + ?,
			failed_calls = failed_calls + ?,
			last_call_time = COALESCE(?, last_call_time),
			updated_at = ?
	`

	_, err := db.Exec(query,
		skillName, totalCalls, successCalls, failedCalls, lastCallTimeSQL, time.Now(),
		totalCalls, successCalls, failedCalls, lastCallTimeSQL, time.Now(),
	)

	if err != nil {
		db.logger.Error("failed to update skill stats", zap.Error(err), zap.String("skillName", skillName))
		return err
	}

	return nil
}

// ClearSkillStats 清空所有Skills统计信息
func (db *DB) ClearSkillStats() error {
	query := `DELETE FROM skill_stats`
	_, err := db.Exec(query)
	if err != nil {
		db.logger.Error("failed to clear skill stats", zap.Error(err))
		return err
	}
	db.logger.Info("cleared all skill stats")
	return nil
}

// ClearSkillStatsByName 清空指定skill的统计信息
func (db *DB) ClearSkillStatsByName(skillName string) error {
	query := `DELETE FROM skill_stats WHERE skill_name = ?`
	_, err := db.Exec(query, skillName)
	if err != nil {
		db.logger.Error("failed to clear skill stats by name", zap.Error(err), zap.String("skillName", skillName))
		return err
	}
	db.logger.Info("cleared skill stats by name", zap.String("skillName", skillName))
	return nil
}
