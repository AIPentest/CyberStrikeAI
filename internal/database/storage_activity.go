package database

import (
	"database/sql"
	"strings"
	"time"
)

// ConversationLastActivity 返回会话最近活动时间；ok=false 表示会话已不存在。
// 供存储清理判断目录是否为孤儿、以及会话是否仍在活跃使用。
func (db *DB) ConversationLastActivity(id string) (time.Time, bool, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return time.Time{}, false, nil
	}
	var createdAt, updatedAt string
	err := db.QueryRow(
		"SELECT created_at, updated_at FROM conversations WHERE id = ? LIMIT 1", id,
	).Scan(&createdAt, &updatedAt)
	if err == sql.ErrNoRows {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	created, updated := parseDBTime(createdAt), parseDBTime(updatedAt)
	if created.After(updated) {
		return created, true, nil
	}
	return updated, true, nil
}

// ProjectLastActivity 返回项目最近活动时间；ok=false 表示项目已不存在。
func (db *DB) ProjectLastActivity(id string) (time.Time, bool, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return time.Time{}, false, nil
	}
	var createdAt, updatedAt string
	err := db.QueryRow(
		"SELECT created_at, updated_at FROM projects WHERE id = ? LIMIT 1", id,
	).Scan(&createdAt, &updatedAt)
	if err == sql.ErrNoRows {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	created, updated := parseDBTime(createdAt), parseDBTime(updatedAt)
	if created.After(updated) {
		return created, true, nil
	}
	return updated, true, nil
}
