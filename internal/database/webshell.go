package database

import (
	"database/sql"
	"strings"
	"time"

	"go.uber.org/zap"
)

// WebShellConnection WebShell 连接配置
type WebShellConnection struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id,omitempty"`
	URL       string    `json:"url"`
	Password  string    `json:"password"`
	Type      string    `json:"type"`
	Method    string    `json:"method"`
	CmdParam  string    `json:"cmdParam"`
	Remark    string    `json:"remark"`
	Encoding  string    `json:"encoding"` // 目标响应编码：auto / utf-8 / gbk / gb18030，空值视为 auto
	OS        string    `json:"os"`       // 目标操作系统：auto / linux / windows，空值/未知视为 auto
	CreatedAt time.Time `json:"createdAt"`
}

// GetWebshellConnectionState 获取连接关联的持久化状态 JSON，不存在时返回 "{}"
func (db *DB) GetWebshellConnectionState(connectionID string) (string, error) {
	var stateJSON string
	err := db.QueryRow(`SELECT state_json FROM webshell_connection_states WHERE connection_id = ?`, connectionID).Scan(&stateJSON)
	if err == sql.ErrNoRows {
		return "{}", nil
	}
	if err != nil {
		db.logger.Error("failed to query the WebShell connection state", zap.Error(err), zap.String("connectionID", connectionID))
		return "", err
	}
	if stateJSON == "" {
		stateJSON = "{}"
	}
	return stateJSON, nil
}

// UpsertWebshellConnectionState 保存连接关联的持久化状态 JSON
func (db *DB) UpsertWebshellConnectionState(connectionID, stateJSON string) error {
	if stateJSON == "" {
		stateJSON = "{}"
	}
	query := `
		INSERT INTO webshell_connection_states (connection_id, state_json, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(connection_id) DO UPDATE SET
			state_json = excluded.state_json,
			updated_at = excluded.updated_at
	`
	if _, err := db.Exec(query, connectionID, stateJSON, time.Now()); err != nil {
		db.logger.Error("failed to save the WebShell connection state", zap.Error(err), zap.String("connectionID", connectionID))
		return err
	}
	return nil
}

// ListWebshellConnections 列出所有 WebShell 连接，按创建时间倒序
func (db *DB) ListWebshellConnections() ([]WebShellConnection, error) {
	return db.ListWebshellConnectionsForAccess("", "", "")
}

func (db *DB) ListWebshellConnectionsForAccess(userID, scope, projectID string) ([]WebShellConnection, error) {
	query := `
		SELECT id, COALESCE(project_id, '') AS project_id, url, password, type, method, cmd_param, remark,
			COALESCE(encoding, '') AS encoding, COALESCE(os, '') AS os, created_at
		FROM webshell_connections
		WHERE 1=1
	`
	args := []interface{}{}
	projectID = strings.TrimSpace(projectID)
	if projectID == ProjectFilterUnbound {
		query += ` AND COALESCE(project_id, '') = ''`
	} else if projectID != "" {
		query += ` AND COALESCE(project_id, '') = ?`
		args = append(args, projectID)
	}
	userID = strings.TrimSpace(userID)
	if userID != "" && scope != RBACScopeAll {
		query += ` AND (
			owner_user_id = ?
			OR EXISTS (
				SELECT 1 FROM rbac_resource_assignments ra
				WHERE ra.user_id = ? AND ra.resource_type = 'webshell' AND ra.resource_id = webshell_connections.id
			)
		)`
		args = append(args, userID, userID)
	}
	query += ` ORDER BY created_at DESC`
	rows, err := db.Query(query, args...)
	if err != nil {
		db.logger.Error("failed to list WebShell connections", zap.Error(err))
		return nil, err
	}
	defer rows.Close()

	var list []WebShellConnection
	for rows.Next() {
		var c WebShellConnection
		err := rows.Scan(&c.ID, &c.ProjectID, &c.URL, &c.Password, &c.Type, &c.Method, &c.CmdParam, &c.Remark, &c.Encoding, &c.OS, &c.CreatedAt)
		if err != nil {
			db.logger.Warn("failed to scan a WebShell connection row", zap.Error(err))
			continue
		}
		list = append(list, c)
	}
	return list, rows.Err()
}

// GetWebshellConnection 根据 ID 获取一条连接
func (db *DB) GetWebshellConnection(id string) (*WebShellConnection, error) {
	query := `
		SELECT id, COALESCE(project_id, '') AS project_id, url, password, type, method, cmd_param, remark,
			COALESCE(encoding, '') AS encoding, COALESCE(os, '') AS os, created_at
		FROM webshell_connections WHERE id = ?
	`
	var c WebShellConnection
	err := db.QueryRow(query, id).Scan(&c.ID, &c.ProjectID, &c.URL, &c.Password, &c.Type, &c.Method, &c.CmdParam, &c.Remark, &c.Encoding, &c.OS, &c.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		db.logger.Error("failed to get the WebShell connection", zap.Error(err), zap.String("id", id))
		return nil, err
	}
	return &c, nil
}

// CreateWebshellConnection 创建 WebShell 连接
func (db *DB) CreateWebshellConnection(c *WebShellConnection) error {
	query := `
		INSERT INTO webshell_connections (id, project_id, url, password, type, method, cmd_param, remark, encoding, os, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := db.Exec(query, c.ID, strings.TrimSpace(c.ProjectID), c.URL, c.Password, c.Type, c.Method, c.CmdParam, c.Remark, c.Encoding, c.OS, c.CreatedAt)
	if err != nil {
		db.logger.Error("failed to create the WebShell connection", zap.Error(err), zap.String("id", c.ID))
		return err
	}
	return nil
}

// UpdateWebshellConnection 更新 WebShell 连接
func (db *DB) UpdateWebshellConnection(c *WebShellConnection) error {
	query := `
		UPDATE webshell_connections
		SET project_id = ?, url = ?, password = ?, type = ?, method = ?, cmd_param = ?, remark = ?, encoding = ?, os = ?
		WHERE id = ?
	`
	result, err := db.Exec(query, strings.TrimSpace(c.ProjectID), c.URL, c.Password, c.Type, c.Method, c.CmdParam, c.Remark, c.Encoding, c.OS, c.ID)
	if err != nil {
		db.logger.Error("failed to update the WebShell connection", zap.Error(err), zap.String("id", c.ID))
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteWebshellConnection 删除 WebShell 连接
func (db *DB) DeleteWebshellConnection(id string) error {
	result, err := db.Exec(`DELETE FROM webshell_connections WHERE id = ?`, id)
	if err != nil {
		db.logger.Error("failed to delete the WebShell connection", zap.Error(err), zap.String("id", id))
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}
