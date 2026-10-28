package database

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"
)

// ErrNoValidC2EventIDs 批量删除事件时未提供任何合法 ID
var ErrNoValidC2EventIDs = errors.New("no valid event ids")

// ErrNoValidC2TaskIDs 批量删除任务时未提供任何合法 ID
var ErrNoValidC2TaskIDs = errors.New("no valid task ids")

// ErrNoValidC2SessionIDs 批量删除会话时未提供任何合法 ID
var ErrNoValidC2SessionIDs = errors.New("no valid session ids")

// validC2TextIDForDelete 校验 C2 文本主键（e_/t_/s_/… 等）用于批量删除入参
func validC2TextIDForDelete(id string) bool {
	if len(id) < 2 || len(id) > 80 {
		return false
	}
	for _, c := range id {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' {
			continue
		}
		return false
	}
	return true
}

// ============================================================================
// C2 模块数据模型 — 6 张表的领域类型
// 设计要点：
//   - 全部使用文本主键（l_/s_/t_/f_/e_/p_ 前缀），与项目现有 ws_/v_ 风格一致；
//   - 时间字段统一 time.Time，由 SQLite 自动序列化为 ISO8601；
//   - 大字段（profile 配置、心跳元数据、任务结果）走 JSON 文本，避免频繁加列；
//   - 任意会话/任务/文件均可按 listener_id / session_id 级联删除（FOREIGN KEY ON DELETE CASCADE）。
// ============================================================================

// C2Listener 监听器实体
type C2Listener struct {
	ID            string     `json:"id"`
	ProjectID     string     `json:"project_id,omitempty"`
	Name          string     `json:"name"`
	Type          string     `json:"type"`       // tcp_reverse|http_beacon|https_beacon|websocket|dns
	BindHost      string     `json:"bindHost"`   // 默认 127.0.0.1
	BindPort      int        `json:"bindPort"`   // 1-65535
	ProfileID     string     `json:"profileId"`  // 可空：关联 c2_profiles.id
	EncryptionKey string     `json:"-"`          // base64(AES-256)，前端不返回
	ImplantToken  string     `json:"-"`          // beacon 携带的鉴权 token，前端不返回
	Status        string     `json:"status"`     // stopped|running|error
	ConfigJSON    string     `json:"configJson"` // TLS 证书路径 / URI 模式 / 上限并发 等
	Remark        string     `json:"remark"`
	OwnerUserID   string     `json:"ownerUserId,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
	StartedAt     *time.Time `json:"startedAt,omitempty"`
	LastError     string     `json:"lastError,omitempty"`
}

// C2Session 已上线会话
type C2Session struct {
	ID            string                 `json:"id"`
	ListenerID    string                 `json:"listenerId"`
	ImplantUUID   string                 `json:"implantUuid"`
	Hostname      string                 `json:"hostname"`
	Username      string                 `json:"username"`
	OS            string                 `json:"os"`
	Arch          string                 `json:"arch"`
	PID           int                    `json:"pid"`
	ProcessName   string                 `json:"processName"`
	IsAdmin       bool                   `json:"isAdmin"`
	InternalIP    string                 `json:"internalIp"`
	ExternalIP    string                 `json:"externalIp"`
	UserAgent     string                 `json:"userAgent"`
	SleepSeconds  int                    `json:"sleepSeconds"`
	JitterPercent int                    `json:"jitterPercent"`
	Status        string                 `json:"status"` // active|sleeping|dead|killed
	FirstSeenAt   time.Time              `json:"firstSeenAt"`
	LastCheckIn   time.Time              `json:"lastCheckIn"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
	Note          string                 `json:"note"`
}

// C2Task 下发任务
type C2Task struct {
	ID             string                 `json:"id"`
	SessionID      string                 `json:"sessionId"`
	TaskType       string                 `json:"taskType"`
	Payload        map[string]interface{} `json:"payload,omitempty"`
	Status         string                 `json:"status"` // queued|sent|running|success|failed|cancelled
	ResultText     string                 `json:"resultText,omitempty"`
	ResultBlobPath string                 `json:"resultBlobPath,omitempty"`
	Error          string                 `json:"error,omitempty"`
	Source         string                 `json:"source"` // manual|ai|batch|api
	ConversationID string                 `json:"conversationId,omitempty"`
	ApprovalStatus string                 `json:"approvalStatus,omitempty"` // pending|approved|rejected
	CreatedAt      time.Time              `json:"createdAt"`
	SentAt         *time.Time             `json:"sentAt,omitempty"`
	StartedAt      *time.Time             `json:"startedAt,omitempty"`
	CompletedAt    *time.Time             `json:"completedAt,omitempty"`
	DurationMS     int64                  `json:"durationMs,omitempty"`
}

// C2File 上传/下载凭证
type C2File struct {
	ID         string    `json:"id"`
	SessionID  string    `json:"sessionId"`
	TaskID     string    `json:"taskId"`
	Direction  string    `json:"direction"` // upload|download
	RemotePath string    `json:"remotePath"`
	LocalPath  string    `json:"localPath"`
	SizeBytes  int64     `json:"sizeBytes"`
	SHA256     string    `json:"sha256"`
	CreatedAt  time.Time `json:"createdAt"`
}

// C2Event 事件审计
type C2Event struct {
	ID        string                 `json:"id"`
	Level     string                 `json:"level"`    // info|warn|critical
	Category  string                 `json:"category"` // listener|session|task|payload|opsec
	SessionID string                 `json:"sessionId,omitempty"`
	TaskID    string                 `json:"taskId,omitempty"`
	Message   string                 `json:"message"`
	Data      map[string]interface{} `json:"data,omitempty"`
	CreatedAt time.Time              `json:"createdAt"`
}

// C2Profile Malleable Profile
type C2Profile struct {
	ID              string                 `json:"id"`
	Name            string                 `json:"name"`
	UserAgent       string                 `json:"userAgent"`
	URIs            []string               `json:"uris"`
	RequestHeaders  map[string]string      `json:"requestHeaders,omitempty"`
	ResponseHeaders map[string]string      `json:"responseHeaders,omitempty"`
	BodyTemplate    string                 `json:"bodyTemplate"`
	JitterMinMS     int                    `json:"jitterMinMs"`
	JitterMaxMS     int                    `json:"jitterMaxMs"`
	Extra           map[string]interface{} `json:"extra,omitempty"`
	CreatedAt       time.Time              `json:"createdAt"`
}

// ----------------------------------------------------------------------------
// CRUD：C2 监听器
// ----------------------------------------------------------------------------

// CreateC2Listener 写入新监听器；ID/Name 由调用方生成校验
func (db *DB) CreateC2Listener(l *C2Listener) error {
	if l == nil || strings.TrimSpace(l.ID) == "" {
		return errors.New("listener id is required")
	}
	if l.CreatedAt.IsZero() {
		l.CreatedAt = time.Now()
	}
	if strings.TrimSpace(l.Status) == "" {
		l.Status = "stopped"
	}
	if strings.TrimSpace(l.ConfigJSON) == "" {
		l.ConfigJSON = "{}"
	}
	query := `
		INSERT INTO c2_listeners (id, project_id, name, type, bind_host, bind_port, profile_id, encryption_key,
			implant_token, status, config_json, remark, owner_user_id, created_at, started_at, last_error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := db.Exec(query,
		l.ID, strings.TrimSpace(l.ProjectID), l.Name, l.Type, l.BindHost, l.BindPort, l.ProfileID, l.EncryptionKey,
		l.ImplantToken, l.Status, l.ConfigJSON, l.Remark, l.OwnerUserID, l.CreatedAt, l.StartedAt, l.LastError,
	)
	if err != nil {
		db.logger.Error("failed to create the C2 listener", zap.Error(err), zap.String("id", l.ID))
		return err
	}
	return nil
}

// UpdateC2Listener 更新监听器；空字段也会被覆盖（请先 GetC2Listener 拿到完整对象再改）
func (db *DB) UpdateC2Listener(l *C2Listener) error {
	if l == nil || strings.TrimSpace(l.ID) == "" {
		return errors.New("listener id is required")
	}
	if strings.TrimSpace(l.ConfigJSON) == "" {
		l.ConfigJSON = "{}"
	}
	query := `
		UPDATE c2_listeners SET
			project_id = ?, name = ?, type = ?, bind_host = ?, bind_port = ?, profile_id = ?, encryption_key = ?,
			implant_token = ?, status = ?, config_json = ?, remark = ?, owner_user_id = ?, started_at = ?, last_error = ?
		WHERE id = ?
	`
	res, err := db.Exec(query,
		strings.TrimSpace(l.ProjectID), l.Name, l.Type, l.BindHost, l.BindPort, l.ProfileID, l.EncryptionKey,
		l.ImplantToken, l.Status, l.ConfigJSON, l.Remark, l.OwnerUserID, l.StartedAt, l.LastError, l.ID,
	)
	if err != nil {
		db.logger.Error("failed to update the C2 listener", zap.Error(err), zap.String("id", l.ID))
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// SetC2ListenerStatus 仅更新状态/started_at/last_error 三个字段，避免与全量更新竞争
func (db *DB) SetC2ListenerStatus(id, status, lastError string, startedAt *time.Time) error {
	query := `
		UPDATE c2_listeners SET status = ?, last_error = ?, started_at = COALESCE(?, started_at)
		WHERE id = ?
	`
	res, err := db.Exec(query, status, lastError, startedAt, id)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// GetC2Listener 单条查询
func (db *DB) GetC2Listener(id string) (*C2Listener, error) {
	query := `
		SELECT id, COALESCE(project_id, ''), name, type, bind_host, bind_port, COALESCE(profile_id, ''),
			COALESCE(encryption_key, ''), COALESCE(implant_token, ''), status,
			COALESCE(config_json, '{}'), COALESCE(remark, ''),
			COALESCE(owner_user_id, ''), created_at, started_at, COALESCE(last_error, '')
		FROM c2_listeners WHERE id = ?
	`
	var l C2Listener
	var startedAt sql.NullTime
	err := db.QueryRow(query, id).Scan(
		&l.ID, &l.ProjectID, &l.Name, &l.Type, &l.BindHost, &l.BindPort, &l.ProfileID,
		&l.EncryptionKey, &l.ImplantToken, &l.Status,
		&l.ConfigJSON, &l.Remark,
		&l.OwnerUserID, &l.CreatedAt, &startedAt, &l.LastError,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if startedAt.Valid {
		t := startedAt.Time
		l.StartedAt = &t
	}
	return &l, nil
}

// ListC2Listeners 全量列表，按创建时间倒序
func (db *DB) ListC2Listeners() ([]*C2Listener, error) {
	query := `
		SELECT id, COALESCE(project_id, ''), name, type, bind_host, bind_port, COALESCE(profile_id, ''),
			COALESCE(encryption_key, ''), COALESCE(implant_token, ''), status,
			COALESCE(config_json, '{}'), COALESCE(remark, ''),
			COALESCE(owner_user_id, ''), created_at, started_at, COALESCE(last_error, '')
		FROM c2_listeners ORDER BY created_at DESC
	`
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*C2Listener
	for rows.Next() {
		var l C2Listener
		var startedAt sql.NullTime
		if err := rows.Scan(
			&l.ID, &l.ProjectID, &l.Name, &l.Type, &l.BindHost, &l.BindPort, &l.ProfileID,
			&l.EncryptionKey, &l.ImplantToken, &l.Status,
			&l.ConfigJSON, &l.Remark,
			&l.OwnerUserID, &l.CreatedAt, &startedAt, &l.LastError,
		); err != nil {
			db.logger.Warn("failed to scan a c2_listeners row", zap.Error(err))
			continue
		}
		if startedAt.Valid {
			t := startedAt.Time
			l.StartedAt = &t
		}
		list = append(list, &l)
	}
	return list, rows.Err()
}

// ListC2ListenersForAccess lists listeners visible to the resolved RBAC scope.
func (db *DB) ListC2ListenersForAccess(access RBACListAccess, projectID string) ([]*C2Listener, error) {
	conditions := []string{"1=1"}
	args := []interface{}{}
	if projectID = strings.TrimSpace(projectID); projectID == ProjectFilterUnbound {
		conditions = append(conditions, "COALESCE(project_id, '') = ''")
	} else if projectID != "" {
		conditions = append(conditions, "COALESCE(project_id, '') = ?")
		args = append(args, projectID)
	}
	appendC2ListenerAccessFilter(&conditions, &args, access)
	query := `
		SELECT id, COALESCE(project_id, ''), name, type, bind_host, bind_port, COALESCE(profile_id, ''),
			COALESCE(encryption_key, ''), COALESCE(implant_token, ''), status,
			COALESCE(config_json, '{}'), COALESCE(remark, ''),
			COALESCE(owner_user_id, ''), created_at, started_at, COALESCE(last_error, '')
		FROM c2_listeners
		WHERE ` + strings.Join(conditions, " AND ") + `
		ORDER BY created_at DESC
	`
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*C2Listener
	for rows.Next() {
		var l C2Listener
		var startedAt sql.NullTime
		if err := rows.Scan(
			&l.ID, &l.ProjectID, &l.Name, &l.Type, &l.BindHost, &l.BindPort, &l.ProfileID,
			&l.EncryptionKey, &l.ImplantToken, &l.Status,
			&l.ConfigJSON, &l.Remark, &l.OwnerUserID,
			&l.CreatedAt, &startedAt, &l.LastError,
		); err != nil {
			db.logger.Warn("failed to scan a c2_listeners row", zap.Error(err))
			continue
		}
		if startedAt.Valid {
			t := startedAt.Time
			l.StartedAt = &t
		}
		list = append(list, &l)
	}
	return list, rows.Err()
}

func appendC2ListenerAccessFilter(conditions *[]string, args *[]interface{}, access RBACListAccess) {
	if access.Scope == RBACScopeAll {
		return
	}
	if access.UserID == "" {
		*conditions = append(*conditions, "1=0")
		return
	}
	clauses := []string{"owner_user_id = ?"}
	*args = append(*args, access.UserID)
	if access.Scope == RBACScopeAssigned {
		clauses = append(clauses, `EXISTS (
			SELECT 1 FROM rbac_resource_assignments ra
			WHERE ra.user_id = ? AND ra.resource_type = 'c2_listener' AND ra.resource_id = c2_listeners.id
		)`)
		*args = append(*args, access.UserID)
	}
	*conditions = append(*conditions, "("+strings.Join(clauses, " OR ")+")")
}

// DeleteC2Listener 级联删除（会话/任务/文件/事件随之消失）
func (db *DB) DeleteC2Listener(id string) error {
	res, err := db.Exec(`DELETE FROM c2_listeners WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ----------------------------------------------------------------------------
// CRUD：C2 会话
// ----------------------------------------------------------------------------

// UpsertC2Session 按 implant_uuid 唯一约束：首次插入 / 已存在则更新心跳和状态
func (db *DB) UpsertC2Session(s *C2Session) error {
	if s == nil || strings.TrimSpace(s.ID) == "" || strings.TrimSpace(s.ImplantUUID) == "" {
		return errors.New("session id and implant_uuid are required")
	}
	if s.FirstSeenAt.IsZero() {
		s.FirstSeenAt = time.Now()
	}
	if s.LastCheckIn.IsZero() {
		s.LastCheckIn = s.FirstSeenAt
	}
	if strings.TrimSpace(s.Status) == "" {
		s.Status = "active"
	}
	metadataJSON := "{}"
	if len(s.Metadata) > 0 {
		if b, err := json.Marshal(s.Metadata); err == nil {
			metadataJSON = string(b)
		}
	}
	query := `
		INSERT INTO c2_sessions (id, listener_id, implant_uuid, hostname, username, os, arch,
			pid, process_name, is_admin, internal_ip, external_ip, user_agent,
			sleep_seconds, jitter_percent, status, first_seen_at, last_check_in,
			metadata_json, note)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(implant_uuid) DO UPDATE SET
			hostname = excluded.hostname,
			username = excluded.username,
			os = excluded.os,
			arch = excluded.arch,
			pid = excluded.pid,
			process_name = excluded.process_name,
			is_admin = excluded.is_admin,
			internal_ip = excluded.internal_ip,
			external_ip = excluded.external_ip,
			user_agent = excluded.user_agent,
			sleep_seconds = excluded.sleep_seconds,
			jitter_percent = excluded.jitter_percent,
			status = excluded.status,
			last_check_in = excluded.last_check_in,
			metadata_json = excluded.metadata_json
	`
	isAdminInt := 0
	if s.IsAdmin {
		isAdminInt = 1
	}
	_, err := db.Exec(query,
		s.ID, s.ListenerID, s.ImplantUUID, s.Hostname, s.Username, s.OS, s.Arch,
		s.PID, s.ProcessName, isAdminInt, s.InternalIP, s.ExternalIP, s.UserAgent,
		s.SleepSeconds, s.JitterPercent, s.Status, s.FirstSeenAt, s.LastCheckIn,
		metadataJSON, s.Note,
	)
	if err != nil {
		db.logger.Error("failed to upsert the C2 session", zap.Error(err), zap.String("implant_uuid", s.ImplantUUID))
		return err
	}
	return nil
}

// TouchC2Session 仅更新 last_check_in / status，性能比 UpsertC2Session 高，给 beacon 高频心跳用
func (db *DB) TouchC2Session(id, status string, t time.Time) error {
	if t.IsZero() {
		t = time.Now()
	}
	res, err := db.Exec(`UPDATE c2_sessions SET last_check_in = ?, status = ? WHERE id = ?`, t, status, id)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// SetC2SessionStatus 单独改状态
func (db *DB) SetC2SessionStatus(id, status string) error {
	res, err := db.Exec(`UPDATE c2_sessions SET status = ? WHERE id = ?`, status, id)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// SetC2SessionSleep 改 sleep / jitter（操作员或 AI 主动调整心跳节律）
func (db *DB) SetC2SessionSleep(id string, sleepSeconds, jitterPercent int) error {
	if sleepSeconds < 0 {
		sleepSeconds = 0
	}
	if jitterPercent < 0 {
		jitterPercent = 0
	}
	if jitterPercent > 100 {
		jitterPercent = 100
	}
	res, err := db.Exec(`UPDATE c2_sessions SET sleep_seconds = ?, jitter_percent = ? WHERE id = ?`,
		sleepSeconds, jitterPercent, id)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// SetC2SessionNote 改备注
func (db *DB) SetC2SessionNote(id, note string) error {
	_, err := db.Exec(`UPDATE c2_sessions SET note = ? WHERE id = ?`, note, id)
	return err
}

// GetC2Session 按内部 ID 查
func (db *DB) GetC2Session(id string) (*C2Session, error) {
	return db.queryC2SessionWhere(`id = ?`, id)
}

// GetC2SessionByImplantUUID 按 implant 自报的 UUID 查（重连必需）
func (db *DB) GetC2SessionByImplantUUID(uuid string) (*C2Session, error) {
	return db.queryC2SessionWhere(`implant_uuid = ?`, uuid)
}

func (db *DB) queryC2SessionWhere(whereClause string, args ...interface{}) (*C2Session, error) {
	query := `
		SELECT id, listener_id, implant_uuid, COALESCE(hostname,''), COALESCE(username,''),
			COALESCE(os,''), COALESCE(arch,''), COALESCE(pid, 0), COALESCE(process_name,''),
			COALESCE(is_admin, 0), COALESCE(internal_ip,''), COALESCE(external_ip,''),
			COALESCE(user_agent,''), COALESCE(sleep_seconds, 5), COALESCE(jitter_percent, 0),
			status, first_seen_at, last_check_in, COALESCE(metadata_json, '{}'),
			COALESCE(note, '')
		FROM c2_sessions WHERE ` + whereClause
	row := db.QueryRow(query, args...)
	var s C2Session
	var isAdminInt int
	var metadataJSON string
	err := row.Scan(
		&s.ID, &s.ListenerID, &s.ImplantUUID, &s.Hostname, &s.Username,
		&s.OS, &s.Arch, &s.PID, &s.ProcessName,
		&isAdminInt, &s.InternalIP, &s.ExternalIP,
		&s.UserAgent, &s.SleepSeconds, &s.JitterPercent,
		&s.Status, &s.FirstSeenAt, &s.LastCheckIn, &metadataJSON,
		&s.Note,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s.IsAdmin = isAdminInt != 0
	if metadataJSON != "" && metadataJSON != "{}" {
		_ = json.Unmarshal([]byte(metadataJSON), &s.Metadata)
	}
	return &s, nil
}

// ListC2SessionsFilter 列表过滤参数
type ListC2SessionsFilter struct {
	ListenerID string
	ProjectID  string
	Status     string // active|sleeping|dead|killed；空表示全部
	OS         string
	Search     string // 模糊匹配 hostname/username/internal_ip
	Suspicious bool   // 疑似误报：离线且 hostname 为 tcp_* / 用户名为 unknown / PID 为 0
	Limit      int    // 0 表示无限制
}

// ListC2Sessions 列表，按 last_check_in 倒序
func (db *DB) ListC2Sessions(filter ListC2SessionsFilter) ([]*C2Session, error) {
	conditions := []string{"1=1"}
	args := []interface{}{}
	if filter.ListenerID != "" {
		conditions = append(conditions, "listener_id = ?")
		args = append(args, filter.ListenerID)
	}
	if strings.TrimSpace(filter.ProjectID) == ProjectFilterUnbound {
		conditions = append(conditions, `EXISTS (
			SELECT 1 FROM c2_listeners l
			WHERE l.id = c2_sessions.listener_id AND COALESCE(l.project_id, '') = ''
		)`)
	} else if strings.TrimSpace(filter.ProjectID) != "" {
		conditions = append(conditions, `EXISTS (
			SELECT 1 FROM c2_listeners l
			WHERE l.id = c2_sessions.listener_id AND COALESCE(l.project_id, '') = ?
		)`)
		args = append(args, strings.TrimSpace(filter.ProjectID))
	}
	if filter.Status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, filter.Status)
	}
	if filter.OS != "" {
		conditions = append(conditions, "os = ?")
		args = append(args, filter.OS)
	}
	if filter.Search != "" {
		conditions = append(conditions, "(hostname LIKE ? OR username LIKE ? OR internal_ip LIKE ?)")
		kw := "%" + filter.Search + "%"
		args = append(args, kw, kw, kw)
	}
	if filter.Suspicious {
		conditions = append(conditions, `status = 'dead' AND (
			hostname LIKE 'tcp_%' OR LOWER(COALESCE(username,'')) = 'unknown' OR COALESCE(pid, 0) = 0
		)`)
	}
	query := `
		SELECT id, listener_id, implant_uuid, COALESCE(hostname,''), COALESCE(username,''),
			COALESCE(os,''), COALESCE(arch,''), COALESCE(pid, 0), COALESCE(process_name,''),
			COALESCE(is_admin, 0), COALESCE(internal_ip,''), COALESCE(external_ip,''),
			COALESCE(user_agent,''), COALESCE(sleep_seconds, 5), COALESCE(jitter_percent, 0),
			status, first_seen_at, last_check_in, COALESCE(metadata_json, '{}'),
			COALESCE(note, '')
		FROM c2_sessions
		WHERE ` + strings.Join(conditions, " AND ") + `
		ORDER BY last_check_in DESC
	`
	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", filter.Limit)
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*C2Session
	for rows.Next() {
		var s C2Session
		var isAdminInt int
		var metadataJSON string
		if err := rows.Scan(
			&s.ID, &s.ListenerID, &s.ImplantUUID, &s.Hostname, &s.Username,
			&s.OS, &s.Arch, &s.PID, &s.ProcessName,
			&isAdminInt, &s.InternalIP, &s.ExternalIP,
			&s.UserAgent, &s.SleepSeconds, &s.JitterPercent,
			&s.Status, &s.FirstSeenAt, &s.LastCheckIn, &metadataJSON,
			&s.Note,
		); err != nil {
			db.logger.Warn("failed to scan a c2_sessions row", zap.Error(err))
			continue
		}
		s.IsAdmin = isAdminInt != 0
		if metadataJSON != "" && metadataJSON != "{}" {
			_ = json.Unmarshal([]byte(metadataJSON), &s.Metadata)
		}
		list = append(list, &s)
	}
	return list, rows.Err()
}

// ListC2SessionsForAccess lists sessions whose parent listener is visible.
func (db *DB) ListC2SessionsForAccess(filter ListC2SessionsFilter, access RBACListAccess) ([]*C2Session, error) {
	conditions, args := buildC2SessionsWhere(filter)
	appendC2SessionAccessFilter(&conditions, &args, access)
	query := `
		SELECT id, listener_id, implant_uuid, COALESCE(hostname,''), COALESCE(username,''),
			COALESCE(os,''), COALESCE(arch,''), COALESCE(pid, 0), COALESCE(process_name,''),
			COALESCE(is_admin, 0), COALESCE(internal_ip,''), COALESCE(external_ip,''),
			COALESCE(user_agent,''), COALESCE(sleep_seconds, 5), COALESCE(jitter_percent, 0),
			status, first_seen_at, last_check_in, COALESCE(metadata_json, '{}'),
			COALESCE(note, '')
		FROM c2_sessions
		WHERE ` + strings.Join(conditions, " AND ") + `
		ORDER BY last_check_in DESC
	`
	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", filter.Limit)
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return db.scanC2SessionRows(rows)
}

func buildC2SessionsWhere(filter ListC2SessionsFilter) ([]string, []interface{}) {
	conditions := []string{"1=1"}
	args := []interface{}{}
	if filter.ListenerID != "" {
		conditions = append(conditions, "listener_id = ?")
		args = append(args, filter.ListenerID)
	}
	if strings.TrimSpace(filter.ProjectID) == ProjectFilterUnbound {
		conditions = append(conditions, `EXISTS (
			SELECT 1 FROM c2_listeners l
			WHERE l.id = c2_sessions.listener_id AND COALESCE(l.project_id, '') = ''
		)`)
	} else if strings.TrimSpace(filter.ProjectID) != "" {
		conditions = append(conditions, `EXISTS (
			SELECT 1 FROM c2_listeners l
			WHERE l.id = c2_sessions.listener_id AND COALESCE(l.project_id, '') = ?
		)`)
		args = append(args, strings.TrimSpace(filter.ProjectID))
	}
	if filter.Status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, filter.Status)
	}
	if filter.OS != "" {
		conditions = append(conditions, "os = ?")
		args = append(args, filter.OS)
	}
	if filter.Search != "" {
		conditions = append(conditions, "(hostname LIKE ? OR username LIKE ? OR internal_ip LIKE ?)")
		kw := "%" + filter.Search + "%"
		args = append(args, kw, kw, kw)
	}
	if filter.Suspicious {
		conditions = append(conditions, `status = 'dead' AND (
			hostname LIKE 'tcp_%' OR LOWER(COALESCE(username,'')) = 'unknown' OR COALESCE(pid, 0) = 0
		)`)
	}
	return conditions, args
}

func (db *DB) scanC2SessionRows(rows *sql.Rows) ([]*C2Session, error) {
	var list []*C2Session
	for rows.Next() {
		var s C2Session
		var isAdminInt int
		var metadataJSON string
		if err := rows.Scan(
			&s.ID, &s.ListenerID, &s.ImplantUUID, &s.Hostname, &s.Username,
			&s.OS, &s.Arch, &s.PID, &s.ProcessName,
			&isAdminInt, &s.InternalIP, &s.ExternalIP,
			&s.UserAgent, &s.SleepSeconds, &s.JitterPercent,
			&s.Status, &s.FirstSeenAt, &s.LastCheckIn, &metadataJSON,
			&s.Note,
		); err != nil {
			db.logger.Warn("failed to scan a c2_sessions row", zap.Error(err))
			continue
		}
		s.IsAdmin = isAdminInt != 0
		if metadataJSON != "" && metadataJSON != "{}" {
			_ = json.Unmarshal([]byte(metadataJSON), &s.Metadata)
		}
		list = append(list, &s)
	}
	return list, rows.Err()
}

func appendC2SessionAccessFilter(conditions *[]string, args *[]interface{}, access RBACListAccess) {
	if access.Scope == RBACScopeAll {
		return
	}
	if access.UserID == "" {
		*conditions = append(*conditions, "1=0")
		return
	}
	clauses := []string{`EXISTS (
		SELECT 1 FROM c2_listeners
		WHERE c2_listeners.id = c2_sessions.listener_id AND c2_listeners.owner_user_id = ?
	)`}
	*args = append(*args, access.UserID)
	if access.Scope == RBACScopeAssigned {
		clauses = append(clauses, `EXISTS (
			SELECT 1 FROM rbac_resource_assignments ra
			WHERE ra.user_id = ? AND ra.resource_type = 'c2_listener' AND ra.resource_id = c2_sessions.listener_id
		)`)
		*args = append(*args, access.UserID)
	}
	*conditions = append(*conditions, "("+strings.Join(clauses, " OR ")+")")
}

// DeleteC2Session 级联删除其 tasks/files
func (db *DB) DeleteC2Session(id string) error {
	res, err := db.Exec(`DELETE FROM c2_sessions WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteC2SessionsByIDs 按主键批量删除会话
func (db *DB) DeleteC2SessionsByIDs(ids []string) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	const maxBatch = 500
	if len(ids) > maxBatch {
		ids = ids[:maxBatch]
	}
	clean := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if !validC2TextIDForDelete(id) {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		clean = append(clean, id)
	}
	if len(clean) == 0 {
		return 0, ErrNoValidC2SessionIDs
	}
	placeholders := strings.Repeat("?,", len(clean)-1) + "?"
	args := make([]interface{}, len(clean))
	for i := range clean {
		args[i] = clean[i]
	}
	query := `DELETE FROM c2_sessions WHERE id IN (` + placeholders + `)`
	res, err := db.Exec(query, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (db *DB) DeleteC2SessionsByIDsForAccess(ids []string, access RBACListAccess) (int64, error) {
	if access.Scope == RBACScopeAll {
		return db.DeleteC2SessionsByIDs(ids)
	}
	clean := cleanC2IDs(ids)
	if len(clean) == 0 {
		return 0, ErrNoValidC2SessionIDs
	}
	placeholders := strings.Repeat("?,", len(clean)-1) + "?"
	args := make([]interface{}, 0, len(clean)+2)
	for _, id := range clean {
		args = append(args, id)
	}
	conditions := []string{"id IN (" + placeholders + ")"}
	appendC2SessionAccessFilter(&conditions, &args, access)
	query := `DELETE FROM c2_sessions WHERE ` + strings.Join(conditions, " AND ")
	res, err := db.Exec(query, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ----------------------------------------------------------------------------
// CRUD：C2 任务
// ----------------------------------------------------------------------------

// CreateC2Task 入队一个新任务
func (db *DB) CreateC2Task(t *C2Task) error {
	if t == nil || strings.TrimSpace(t.ID) == "" {
		return errors.New("task id is required")
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now()
	}
	if strings.TrimSpace(t.Status) == "" {
		t.Status = "queued"
	}
	if strings.TrimSpace(t.Source) == "" {
		t.Source = "manual"
	}
	payloadJSON := "{}"
	if len(t.Payload) > 0 {
		if b, err := json.Marshal(t.Payload); err == nil {
			payloadJSON = string(b)
		}
	}
	query := `
		INSERT INTO c2_tasks (id, session_id, task_type, payload_json, status,
			result_text, result_blob_path, error, source, conversation_id, approval_status,
			created_at, sent_at, started_at, completed_at, duration_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := db.Exec(query,
		t.ID, t.SessionID, t.TaskType, payloadJSON, t.Status,
		t.ResultText, t.ResultBlobPath, t.Error, t.Source, t.ConversationID, t.ApprovalStatus,
		t.CreatedAt, t.SentAt, t.StartedAt, t.CompletedAt, t.DurationMS,
	)
	if err != nil {
		db.logger.Error("failed to create the C2 task", zap.Error(err), zap.String("id", t.ID))
		return err
	}
	return nil
}

// SetC2TaskStatus 更新任务的状态/结果/错误/时间戳
type C2TaskUpdate struct {
	ExpectedStatus *string // Optional compare-and-swap guard for terminal transitions.
	Status         *string
	ResultText     *string
	ResultBlobPath *string
	Error          *string
	ApprovalStatus *string
	SentAt         *time.Time
	StartedAt      *time.Time
	CompletedAt    *time.Time
	DurationMS     *int64
}

// UpdateC2Task 增量更新任务字段；nil 字段保持原值
func (db *DB) UpdateC2Task(id string, u C2TaskUpdate) error {
	sets := []string{}
	args := []interface{}{}
	if u.Status != nil {
		sets = append(sets, "status = ?")
		args = append(args, *u.Status)
	}
	if u.ResultText != nil {
		sets = append(sets, "result_text = ?")
		args = append(args, *u.ResultText)
	}
	if u.ResultBlobPath != nil {
		sets = append(sets, "result_blob_path = ?")
		args = append(args, *u.ResultBlobPath)
	}
	if u.Error != nil {
		sets = append(sets, "error = ?")
		args = append(args, *u.Error)
	}
	if u.ApprovalStatus != nil {
		sets = append(sets, "approval_status = ?")
		args = append(args, *u.ApprovalStatus)
	}
	if u.SentAt != nil {
		sets = append(sets, "sent_at = ?")
		args = append(args, *u.SentAt)
	}
	if u.StartedAt != nil {
		sets = append(sets, "started_at = ?")
		args = append(args, *u.StartedAt)
	}
	if u.CompletedAt != nil {
		sets = append(sets, "completed_at = ?")
		args = append(args, *u.CompletedAt)
	}
	if u.DurationMS != nil {
		sets = append(sets, "duration_ms = ?")
		args = append(args, *u.DurationMS)
	}
	if len(sets) == 0 {
		return nil
	}
	query := "UPDATE c2_tasks SET " + strings.Join(sets, ", ") + " WHERE id = ?"
	args = append(args, id)
	if u.ExpectedStatus != nil {
		query += " AND status = ?"
		args = append(args, *u.ExpectedStatus)
	}
	res, err := db.Exec(query, args...)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// GetC2Task 单条
func (db *DB) GetC2Task(id string) (*C2Task, error) {
	query := `
		SELECT id, session_id, task_type, COALESCE(payload_json, '{}'),
			status, COALESCE(result_text, ''), COALESCE(result_blob_path, ''),
			COALESCE(error, ''), COALESCE(source, 'manual'),
			COALESCE(conversation_id, ''), COALESCE(approval_status, ''),
			created_at, sent_at, started_at, completed_at, COALESCE(duration_ms, 0)
		FROM c2_tasks WHERE id = ?
	`
	var t C2Task
	var payloadJSON string
	var sentAt, startedAt, completedAt sql.NullTime
	err := db.QueryRow(query, id).Scan(
		&t.ID, &t.SessionID, &t.TaskType, &payloadJSON,
		&t.Status, &t.ResultText, &t.ResultBlobPath,
		&t.Error, &t.Source,
		&t.ConversationID, &t.ApprovalStatus,
		&t.CreatedAt, &sentAt, &startedAt, &completedAt, &t.DurationMS,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if payloadJSON != "" && payloadJSON != "{}" {
		_ = json.Unmarshal([]byte(payloadJSON), &t.Payload)
	}
	if sentAt.Valid {
		x := sentAt.Time
		t.SentAt = &x
	}
	if startedAt.Valid {
		x := startedAt.Time
		t.StartedAt = &x
	}
	if completedAt.Valid {
		x := completedAt.Time
		t.CompletedAt = &x
	}
	return &t, nil
}

// ListC2TasksFilter 任务过滤
type ListC2TasksFilter struct {
	SessionID string
	ProjectID string
	Status    string
	TaskType  string
	Since     *time.Time
	Limit     int
	Offset    int
}

func buildC2TasksWhere(filter ListC2TasksFilter) (where string, args []interface{}) {
	conditions := []string{"1=1"}
	args = []interface{}{}
	if filter.SessionID != "" {
		conditions = append(conditions, "session_id = ?")
		args = append(args, filter.SessionID)
	}
	if strings.TrimSpace(filter.ProjectID) == ProjectFilterUnbound {
		conditions = append(conditions, `EXISTS (
			SELECT 1 FROM c2_sessions s
			JOIN c2_listeners l ON l.id = s.listener_id
			WHERE s.id = c2_tasks.session_id AND COALESCE(l.project_id, '') = ''
		)`)
	} else if strings.TrimSpace(filter.ProjectID) != "" {
		conditions = append(conditions, `EXISTS (
			SELECT 1 FROM c2_sessions s
			JOIN c2_listeners l ON l.id = s.listener_id
			WHERE s.id = c2_tasks.session_id AND COALESCE(l.project_id, '') = ?
		)`)
		args = append(args, strings.TrimSpace(filter.ProjectID))
	}
	if filter.Status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, filter.Status)
	}
	if strings.TrimSpace(filter.TaskType) != "" {
		conditions = append(conditions, "task_type = ?")
		args = append(args, strings.TrimSpace(filter.TaskType))
	}
	if filter.Since != nil {
		conditions = append(conditions, sqliteEpochGE("created_at", ">="))
		args = append(args, formatSQLiteUTC(*filter.Since))
	}
	return strings.Join(conditions, " AND "), args
}

func appendC2TaskAccessFilter(conditions *[]string, args *[]interface{}, access RBACListAccess) {
	if access.Scope == RBACScopeAll {
		return
	}
	if access.UserID == "" {
		*conditions = append(*conditions, "1=0")
		return
	}
	clauses := []string{`EXISTS (
		SELECT 1 FROM c2_sessions s
		JOIN c2_listeners l ON l.id = s.listener_id
		WHERE s.id = c2_tasks.session_id AND l.owner_user_id = ?
	)`}
	*args = append(*args, access.UserID)
	if access.Scope == RBACScopeAssigned {
		clauses = append(clauses, `EXISTS (
			SELECT 1 FROM c2_sessions s
			JOIN rbac_resource_assignments ra ON ra.resource_id = s.listener_id
			WHERE s.id = c2_tasks.session_id
				AND ra.user_id = ? AND ra.resource_type = 'c2_listener'
		)`)
		*args = append(*args, access.UserID)
	}
	*conditions = append(*conditions, "("+strings.Join(clauses, " OR ")+")")
}

func buildC2TasksWhereForAccess(filter ListC2TasksFilter, access RBACListAccess) (string, []interface{}) {
	where, args := buildC2TasksWhere(filter)
	conditions := []string{where}
	appendC2TaskAccessFilter(&conditions, &args, access)
	return strings.Join(conditions, " AND "), args
}

// CountC2Tasks 与 ListC2Tasks 相同过滤条件下的记录总数
func (db *DB) CountC2Tasks(filter ListC2TasksFilter) (int64, error) {
	where, args := buildC2TasksWhere(filter)
	query := `SELECT COUNT(*) FROM c2_tasks WHERE ` + where
	var n int64
	err := db.QueryRow(query, args...).Scan(&n)
	return n, err
}

func (db *DB) CountC2TasksForAccess(filter ListC2TasksFilter, access RBACListAccess) (int64, error) {
	where, args := buildC2TasksWhereForAccess(filter, access)
	query := `SELECT COUNT(*) FROM c2_tasks WHERE ` + where
	var n int64
	err := db.QueryRow(query, args...).Scan(&n)
	return n, err
}

// CountC2TasksByStatusForAccess 与 ListC2Tasks 相同过滤条件下按状态统计
func (db *DB) CountC2TasksByStatusForAccess(filter ListC2TasksFilter, access RBACListAccess) (map[string]int64, error) {
	where, args := buildC2TasksWhereForAccess(filter, access)
	query := `SELECT status, COUNT(*) FROM c2_tasks WHERE ` + where + ` GROUP BY status`
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := map[string]int64{
		"queued":    0,
		"sent":      0,
		"running":   0,
		"success":   0,
		"failed":    0,
		"cancelled": 0,
		"pending":   0,
	}
	var legacyPending int64
	for rows.Next() {
		var status string
		var n int64
		if err := rows.Scan(&status, &n); err != nil {
			continue
		}
		if status == "pending" {
			legacyPending = n
			continue
		}
		if _, ok := counts[status]; ok {
			counts[status] = n
		}
	}
	counts["pending"] = counts["queued"] + counts["sent"] + counts["running"] + legacyPending
	return counts, rows.Err()
}

// CountC2TasksQueuedOrPending 统计 queued/pending 状态任务数（仪表盘「待审任务」）
func (db *DB) CountC2TasksQueuedOrPending(sessionID string) (int64, error) {
	conditions := []string{"status IN ('queued', 'pending')"}
	args := []interface{}{}
	if sessionID != "" {
		conditions = append(conditions, "session_id = ?")
		args = append(args, sessionID)
	}
	query := `SELECT COUNT(*) FROM c2_tasks WHERE ` + strings.Join(conditions, " AND ")
	var n int64
	err := db.QueryRow(query, args...).Scan(&n)
	return n, err
}

func (db *DB) CountC2TasksQueuedOrPendingForAccess(sessionID, projectID string, access RBACListAccess) (int64, error) {
	filter := ListC2TasksFilter{SessionID: sessionID, ProjectID: projectID}
	where, args := buildC2TasksWhereForAccess(filter, access)
	query := `SELECT COUNT(*) FROM c2_tasks WHERE status IN ('queued', 'pending') AND ` + where
	var n int64
	err := db.QueryRow(query, args...).Scan(&n)
	return n, err
}

// ListC2Tasks 任务列表，按创建时间倒序
func (db *DB) ListC2Tasks(filter ListC2TasksFilter) ([]*C2Task, error) {
	where, args := buildC2TasksWhere(filter)
	query := `
		SELECT id, session_id, task_type, COALESCE(payload_json, '{}'),
			status, COALESCE(result_text, ''), COALESCE(result_blob_path, ''),
			COALESCE(error, ''), COALESCE(source, 'manual'),
			COALESCE(conversation_id, ''), COALESCE(approval_status, ''),
			created_at, sent_at, started_at, completed_at, COALESCE(duration_ms, 0)
		FROM c2_tasks
		WHERE ` + where + `
		ORDER BY created_at DESC
	`
	limit := filter.Limit
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}
	if limit > 0 {
		if limit > 1000 {
			limit = 1000
		}
		query += ` LIMIT ? OFFSET ?`
		args = append(args, limit, offset)
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*C2Task
	for rows.Next() {
		var t C2Task
		var payloadJSON string
		var sentAt, startedAt, completedAt sql.NullTime
		if err := rows.Scan(
			&t.ID, &t.SessionID, &t.TaskType, &payloadJSON,
			&t.Status, &t.ResultText, &t.ResultBlobPath,
			&t.Error, &t.Source,
			&t.ConversationID, &t.ApprovalStatus,
			&t.CreatedAt, &sentAt, &startedAt, &completedAt, &t.DurationMS,
		); err != nil {
			db.logger.Warn("failed to scan a c2_tasks row", zap.Error(err))
			continue
		}
		if payloadJSON != "" && payloadJSON != "{}" {
			_ = json.Unmarshal([]byte(payloadJSON), &t.Payload)
		}
		if sentAt.Valid {
			x := sentAt.Time
			t.SentAt = &x
		}
		if startedAt.Valid {
			x := startedAt.Time
			t.StartedAt = &x
		}
		if completedAt.Valid {
			x := completedAt.Time
			t.CompletedAt = &x
		}
		list = append(list, &t)
	}
	return list, rows.Err()
}

func (db *DB) ListC2TasksForAccess(filter ListC2TasksFilter, access RBACListAccess) ([]*C2Task, error) {
	where, args := buildC2TasksWhereForAccess(filter, access)
	query := `
		SELECT id, session_id, task_type, COALESCE(payload_json, '{}'),
			status, COALESCE(result_text, ''), COALESCE(result_blob_path, ''),
			COALESCE(error, ''), COALESCE(source, 'manual'),
			COALESCE(conversation_id, ''), COALESCE(approval_status, ''),
			created_at, sent_at, started_at, completed_at, COALESCE(duration_ms, 0)
		FROM c2_tasks
		WHERE ` + where + `
		ORDER BY created_at DESC
	`
	limit := filter.Limit
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}
	if limit > 0 {
		if limit > 1000 {
			limit = 1000
		}
		query += ` LIMIT ? OFFSET ?`
		args = append(args, limit, offset)
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return db.scanC2TaskRows(rows)
}

func (db *DB) scanC2TaskRows(rows *sql.Rows) ([]*C2Task, error) {
	var list []*C2Task
	for rows.Next() {
		var t C2Task
		var payloadJSON string
		var sentAt, startedAt, completedAt sql.NullTime
		if err := rows.Scan(
			&t.ID, &t.SessionID, &t.TaskType, &payloadJSON,
			&t.Status, &t.ResultText, &t.ResultBlobPath,
			&t.Error, &t.Source,
			&t.ConversationID, &t.ApprovalStatus,
			&t.CreatedAt, &sentAt, &startedAt, &completedAt, &t.DurationMS,
		); err != nil {
			db.logger.Warn("failed to scan a c2_tasks row", zap.Error(err))
			continue
		}
		if payloadJSON != "" && payloadJSON != "{}" {
			_ = json.Unmarshal([]byte(payloadJSON), &t.Payload)
		}
		if sentAt.Valid {
			x := sentAt.Time
			t.SentAt = &x
		}
		if startedAt.Valid {
			x := startedAt.Time
			t.StartedAt = &x
		}
		if completedAt.Valid {
			x := completedAt.Time
			t.CompletedAt = &x
		}
		list = append(list, &t)
	}
	return list, rows.Err()
}

// PopQueuedC2Tasks 取出某会话所有 queued/approved 任务（用于 beacon 拉取），原子置为 sent
func (db *DB) PopQueuedC2Tasks(sessionID string, limit int) ([]*C2Task, error) {
	if limit <= 0 {
		limit = 50
	}
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	query := `
		SELECT id, session_id, task_type, COALESCE(payload_json, '{}'),
			status, COALESCE(source, 'manual'), COALESCE(approval_status, ''),
			created_at
		FROM c2_tasks
		WHERE session_id = ? AND (status = 'queued' AND (approval_status = '' OR approval_status = 'approved'))
		ORDER BY created_at ASC, rowid ASC
		LIMIT ?
	`
	rows, err := tx.Query(query, sessionID, limit)
	if err != nil {
		return nil, err
	}
	var list []*C2Task
	for rows.Next() {
		var t C2Task
		var payloadJSON string
		if err := rows.Scan(&t.ID, &t.SessionID, &t.TaskType, &payloadJSON,
			&t.Status, &t.Source, &t.ApprovalStatus, &t.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		if payloadJSON != "" && payloadJSON != "{}" {
			_ = json.Unmarshal([]byte(payloadJSON), &t.Payload)
		}
		list = append(list, &t)
	}
	rows.Close()

	now := time.Now()
	for _, t := range list {
		if _, err := tx.Exec(
			`UPDATE c2_tasks SET status = 'sent', sent_at = ? WHERE id = ?`, now, t.ID,
		); err != nil {
			return nil, err
		}
		t.Status = "sent"
		t.SentAt = &now
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	committed = true
	return list, nil
}

// DeleteC2Task 删除任务（一般用于 cancel queued）
func (db *DB) DeleteC2Task(id string) error {
	res, err := db.Exec(`DELETE FROM c2_tasks WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteC2TasksByIDs 按主键批量删除任务
func (db *DB) DeleteC2TasksByIDs(ids []string) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	const maxBatch = 500
	if len(ids) > maxBatch {
		ids = ids[:maxBatch]
	}
	clean := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if !validC2TextIDForDelete(id) {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		clean = append(clean, id)
	}
	if len(clean) == 0 {
		return 0, ErrNoValidC2TaskIDs
	}
	placeholders := strings.Repeat("?,", len(clean)-1) + "?"
	args := make([]interface{}, len(clean))
	for i := range clean {
		args[i] = clean[i]
	}
	query := `DELETE FROM c2_tasks WHERE id IN (` + placeholders + `)`
	res, err := db.Exec(query, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (db *DB) DeleteC2TasksByIDsForAccess(ids []string, access RBACListAccess) (int64, error) {
	if access.Scope == RBACScopeAll {
		return db.DeleteC2TasksByIDs(ids)
	}
	clean := cleanC2IDs(ids)
	if len(clean) == 0 {
		return 0, ErrNoValidC2TaskIDs
	}
	placeholders := strings.Repeat("?,", len(clean)-1) + "?"
	args := make([]interface{}, 0, len(clean)+2)
	for _, id := range clean {
		args = append(args, id)
	}
	conditions := []string{"id IN (" + placeholders + ")"}
	appendC2TaskAccessFilter(&conditions, &args, access)
	query := `DELETE FROM c2_tasks WHERE ` + strings.Join(conditions, " AND ")
	res, err := db.Exec(query, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ----------------------------------------------------------------------------
// CRUD：C2 文件
// ----------------------------------------------------------------------------

// CreateC2File 记录上传/下载凭证（实际文件落盘由调用方处理）
func (db *DB) CreateC2File(f *C2File) error {
	if f == nil || strings.TrimSpace(f.ID) == "" {
		return errors.New("file id is required")
	}
	if f.CreatedAt.IsZero() {
		f.CreatedAt = time.Now()
	}
	query := `
		INSERT INTO c2_files (id, session_id, task_id, direction, remote_path,
			local_path, size_bytes, sha256, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := db.Exec(query, f.ID, f.SessionID, f.TaskID, f.Direction,
		f.RemotePath, f.LocalPath, f.SizeBytes, f.SHA256, f.CreatedAt)
	return err
}

// ListC2FilesBySession 列出某会话下所有上传/下载凭证
func (db *DB) ListC2FilesBySession(sessionID string) ([]*C2File, error) {
	query := `
		SELECT id, session_id, COALESCE(task_id, ''), direction, remote_path, local_path,
			COALESCE(size_bytes, 0), COALESCE(sha256, ''), created_at
		FROM c2_files WHERE session_id = ? ORDER BY created_at DESC
	`
	rows, err := db.Query(query, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*C2File
	for rows.Next() {
		var f C2File
		if err := rows.Scan(&f.ID, &f.SessionID, &f.TaskID, &f.Direction,
			&f.RemotePath, &f.LocalPath, &f.SizeBytes, &f.SHA256, &f.CreatedAt); err != nil {
			continue
		}
		list = append(list, &f)
	}
	return list, rows.Err()
}

func cleanC2IDs(ids []string) []string {
	const maxBatch = 500
	if len(ids) > maxBatch {
		ids = ids[:maxBatch]
	}
	clean := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if !validC2TextIDForDelete(id) {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		clean = append(clean, id)
	}
	return clean
}

// ----------------------------------------------------------------------------
// CRUD：C2 事件审计
// ----------------------------------------------------------------------------

// AppendC2Event 写一条审计事件
func (db *DB) AppendC2Event(e *C2Event) error {
	if e == nil {
		return errors.New("event is nil")
	}
	if strings.TrimSpace(e.ID) == "" {
		return errors.New("event id is required")
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	} else {
		e.CreatedAt = e.CreatedAt.UTC()
	}
	if strings.TrimSpace(e.Level) == "" {
		e.Level = "info"
	}
	dataJSON := ""
	if len(e.Data) > 0 {
		if b, err := json.Marshal(e.Data); err == nil {
			dataJSON = string(b)
		}
	}
	query := `
		INSERT INTO c2_events (id, level, category, session_id, task_id, message, data_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := db.Exec(query, e.ID, e.Level, e.Category, e.SessionID, e.TaskID, e.Message, dataJSON, formatSQLiteUTC(e.CreatedAt))
	return err
}

// ListC2EventsFilter 事件查询参数
type ListC2EventsFilter struct {
	Level     string
	Category  string
	ProjectID string
	SessionID string
	TaskID    string
	Since     *time.Time
	Limit     int
	Offset    int
}

func buildC2EventsWhere(filter ListC2EventsFilter) (where string, args []interface{}) {
	conditions := []string{"1=1"}
	args = []interface{}{}
	if filter.Level != "" {
		conditions = append(conditions, "level = ?")
		args = append(args, filter.Level)
	}
	if filter.Category != "" {
		conditions = append(conditions, "category = ?")
		args = append(args, filter.Category)
	}
	if strings.TrimSpace(filter.ProjectID) == ProjectFilterUnbound {
		conditions = append(conditions, `(
			EXISTS (
				SELECT 1 FROM c2_sessions s
				JOIN c2_listeners l ON l.id = s.listener_id
				WHERE s.id = c2_events.session_id AND COALESCE(l.project_id, '') = ''
			)
			OR EXISTS (
				SELECT 1 FROM c2_tasks t
				JOIN c2_sessions s ON s.id = t.session_id
				JOIN c2_listeners l ON l.id = s.listener_id
				WHERE t.id = c2_events.task_id AND COALESCE(l.project_id, '') = ''
			)
			OR EXISTS (
				SELECT 1 FROM c2_listeners l
				WHERE json_valid(c2_events.data_json)
					AND l.id = json_extract(c2_events.data_json, '$.listener_id')
					AND COALESCE(l.project_id, '') = ''
			)
		)`)
	} else if strings.TrimSpace(filter.ProjectID) != "" {
		conditions = append(conditions, `(
			EXISTS (
				SELECT 1 FROM c2_sessions s
				JOIN c2_listeners l ON l.id = s.listener_id
				WHERE s.id = c2_events.session_id AND COALESCE(l.project_id, '') = ?
			)
			OR EXISTS (
				SELECT 1 FROM c2_tasks t
				JOIN c2_sessions s ON s.id = t.session_id
				JOIN c2_listeners l ON l.id = s.listener_id
				WHERE t.id = c2_events.task_id AND COALESCE(l.project_id, '') = ?
			)
			OR EXISTS (
				SELECT 1 FROM c2_listeners l
				WHERE json_valid(c2_events.data_json)
					AND l.id = json_extract(c2_events.data_json, '$.listener_id')
					AND COALESCE(l.project_id, '') = ?
			)
		)`)
		pid := strings.TrimSpace(filter.ProjectID)
		args = append(args, pid, pid, pid)
	}
	if filter.SessionID != "" {
		conditions = append(conditions, "session_id = ?")
		args = append(args, filter.SessionID)
	}
	if filter.TaskID != "" {
		conditions = append(conditions, "task_id = ?")
		args = append(args, filter.TaskID)
	}
	if filter.Since != nil {
		conditions = append(conditions, sqliteEpochGE("created_at", ">="))
		args = append(args, formatSQLiteUTC(*filter.Since))
	}
	return strings.Join(conditions, " AND "), args
}

func appendC2EventAccessFilter(conditions *[]string, args *[]interface{}, access RBACListAccess) {
	if access.Scope == RBACScopeAll {
		return
	}
	if access.UserID == "" {
		*conditions = append(*conditions, "1=0")
		return
	}
	clauses := []string{`EXISTS (
		SELECT 1 FROM c2_sessions s
		JOIN c2_listeners l ON l.id = s.listener_id
		WHERE s.id = c2_events.session_id AND l.owner_user_id = ?
	)`}
	*args = append(*args, access.UserID)
	if access.Scope == RBACScopeAssigned {
		clauses = append(clauses, `EXISTS (
			SELECT 1 FROM c2_sessions s
			JOIN rbac_resource_assignments ra ON ra.resource_id = s.listener_id
			WHERE s.id = c2_events.session_id
				AND ra.user_id = ? AND ra.resource_type = 'c2_listener'
		)`)
		*args = append(*args, access.UserID)
	}
	clauses = append(clauses, `EXISTS (
		SELECT 1 FROM c2_tasks t
		JOIN c2_sessions s ON s.id = t.session_id
		JOIN c2_listeners l ON l.id = s.listener_id
		WHERE t.id = c2_events.task_id AND l.owner_user_id = ?
	)`)
	*args = append(*args, access.UserID)
	if access.Scope == RBACScopeAssigned {
		clauses = append(clauses, `EXISTS (
			SELECT 1 FROM c2_tasks t
			JOIN c2_sessions s ON s.id = t.session_id
			JOIN rbac_resource_assignments ra ON ra.resource_id = s.listener_id
			WHERE t.id = c2_events.task_id
				AND ra.user_id = ? AND ra.resource_type = 'c2_listener'
		)`)
		*args = append(*args, access.UserID)
	}
	*conditions = append(*conditions, "("+strings.Join(clauses, " OR ")+")")
}

func buildC2EventsWhereForAccess(filter ListC2EventsFilter, access RBACListAccess) (string, []interface{}) {
	where, args := buildC2EventsWhere(filter)
	conditions := []string{where}
	appendC2EventAccessFilter(&conditions, &args, access)
	return strings.Join(conditions, " AND "), args
}

// CountC2Events 与 ListC2Events 相同过滤条件下的记录总数
func (db *DB) CountC2Events(filter ListC2EventsFilter) (int64, error) {
	where, args := buildC2EventsWhere(filter)
	query := `SELECT COUNT(*) FROM c2_events WHERE ` + where
	var n int64
	err := db.QueryRow(query, args...).Scan(&n)
	return n, err
}

func (db *DB) CountC2EventsForAccess(filter ListC2EventsFilter, access RBACListAccess) (int64, error) {
	where, args := buildC2EventsWhereForAccess(filter, access)
	query := `SELECT COUNT(*) FROM c2_events WHERE ` + where
	var n int64
	err := db.QueryRow(query, args...).Scan(&n)
	return n, err
}

// CountC2EventsByLevelForAccess 与 ListC2Events 相同过滤条件下按级别统计
func (db *DB) CountC2EventsByLevelForAccess(filter ListC2EventsFilter, access RBACListAccess) (map[string]int64, error) {
	where, args := buildC2EventsWhereForAccess(filter, access)
	query := `SELECT level, COUNT(*) FROM c2_events WHERE ` + where + ` GROUP BY level`
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := map[string]int64{
		"info":     0,
		"warn":     0,
		"critical": 0,
	}
	for rows.Next() {
		var level string
		var n int64
		if err := rows.Scan(&level, &n); err != nil {
			continue
		}
		if _, ok := counts[level]; ok {
			counts[level] = n
		}
	}
	return counts, rows.Err()
}

// ListC2Events 事件查询，按创建时间倒序
func (db *DB) ListC2Events(filter ListC2EventsFilter) ([]*C2Event, error) {
	where, args := buildC2EventsWhere(filter)
	limit := filter.Limit
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}
	query := `
		SELECT id, level, category, COALESCE(session_id, ''), COALESCE(task_id, ''),
			message, COALESCE(data_json, ''), created_at
		FROM c2_events
		WHERE ` + where + `
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?
	`
	args = append(args, limit, offset)
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*C2Event
	for rows.Next() {
		var e C2Event
		var dataJSON string
		if err := rows.Scan(&e.ID, &e.Level, &e.Category, &e.SessionID, &e.TaskID,
			&e.Message, &dataJSON, &e.CreatedAt); err != nil {
			continue
		}
		if dataJSON != "" {
			_ = json.Unmarshal([]byte(dataJSON), &e.Data)
		}
		list = append(list, &e)
	}
	return list, rows.Err()
}

func (db *DB) ListC2EventsForAccess(filter ListC2EventsFilter, access RBACListAccess) ([]*C2Event, error) {
	where, args := buildC2EventsWhereForAccess(filter, access)
	limit := filter.Limit
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}
	query := `
		SELECT id, level, category, COALESCE(session_id, ''), COALESCE(task_id, ''),
			message, COALESCE(data_json, ''), created_at
		FROM c2_events
		WHERE ` + where + `
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?
	`
	args = append(args, limit, offset)
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanC2EventRows(rows)
}

func scanC2EventRows(rows *sql.Rows) ([]*C2Event, error) {
	var list []*C2Event
	for rows.Next() {
		var e C2Event
		var dataJSON string
		if err := rows.Scan(&e.ID, &e.Level, &e.Category, &e.SessionID, &e.TaskID,
			&e.Message, &dataJSON, &e.CreatedAt); err != nil {
			continue
		}
		if dataJSON != "" {
			_ = json.Unmarshal([]byte(dataJSON), &e.Data)
		}
		list = append(list, &e)
	}
	return list, rows.Err()
}

// DeleteC2EventsByIDs 按主键批量删除事件，返回实际删除行数
func (db *DB) DeleteC2EventsByIDs(ids []string) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	const maxBatch = 500
	if len(ids) > maxBatch {
		ids = ids[:maxBatch]
	}
	clean := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if !validC2TextIDForDelete(id) {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		clean = append(clean, id)
	}
	if len(clean) == 0 {
		return 0, ErrNoValidC2EventIDs
	}
	placeholders := strings.Repeat("?,", len(clean)-1) + "?"
	args := make([]interface{}, len(clean))
	for i := range clean {
		args[i] = clean[i]
	}
	query := `DELETE FROM c2_events WHERE id IN (` + placeholders + `)`
	res, err := db.Exec(query, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (db *DB) DeleteC2EventsByIDsForAccess(ids []string, access RBACListAccess) (int64, error) {
	if access.Scope == RBACScopeAll {
		return db.DeleteC2EventsByIDs(ids)
	}
	clean := cleanC2IDs(ids)
	if len(clean) == 0 {
		return 0, ErrNoValidC2EventIDs
	}
	placeholders := strings.Repeat("?,", len(clean)-1) + "?"
	args := make([]interface{}, 0, len(clean)+4)
	for _, id := range clean {
		args = append(args, id)
	}
	conditions := []string{"id IN (" + placeholders + ")"}
	appendC2EventAccessFilter(&conditions, &args, access)
	query := `DELETE FROM c2_events WHERE ` + strings.Join(conditions, " AND ")
	res, err := db.Exec(query, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ----------------------------------------------------------------------------
// CRUD：C2 Malleable Profile
// ----------------------------------------------------------------------------

// CreateC2Profile 创建/覆盖 Profile（按 name 唯一）
func (db *DB) CreateC2Profile(p *C2Profile) error {
	if p == nil || strings.TrimSpace(p.ID) == "" {
		return errors.New("profile id is required")
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Now()
	}
	urisJSON, _ := json.Marshal(p.URIs)
	reqHdrJSON, _ := json.Marshal(p.RequestHeaders)
	resHdrJSON, _ := json.Marshal(p.ResponseHeaders)
	query := `
		INSERT INTO c2_profiles (id, name, user_agent, uris_json, request_headers_json,
			response_headers_json, body_template, jitter_min_ms, jitter_max_ms, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := db.Exec(query, p.ID, p.Name, p.UserAgent, string(urisJSON),
		string(reqHdrJSON), string(resHdrJSON), p.BodyTemplate,
		p.JitterMinMS, p.JitterMaxMS, p.CreatedAt)
	return err
}

// UpdateC2Profile 全量更新 Profile
func (db *DB) UpdateC2Profile(p *C2Profile) error {
	if p == nil || strings.TrimSpace(p.ID) == "" {
		return errors.New("profile id is required")
	}
	urisJSON, _ := json.Marshal(p.URIs)
	reqHdrJSON, _ := json.Marshal(p.RequestHeaders)
	resHdrJSON, _ := json.Marshal(p.ResponseHeaders)
	query := `
		UPDATE c2_profiles SET name = ?, user_agent = ?, uris_json = ?,
			request_headers_json = ?, response_headers_json = ?, body_template = ?,
			jitter_min_ms = ?, jitter_max_ms = ?
		WHERE id = ?
	`
	res, err := db.Exec(query, p.Name, p.UserAgent, string(urisJSON),
		string(reqHdrJSON), string(resHdrJSON), p.BodyTemplate,
		p.JitterMinMS, p.JitterMaxMS, p.ID)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// GetC2Profile 单条
func (db *DB) GetC2Profile(id string) (*C2Profile, error) {
	query := `
		SELECT id, name, COALESCE(user_agent, ''), COALESCE(uris_json, '[]'),
			COALESCE(request_headers_json, '{}'), COALESCE(response_headers_json, '{}'),
			COALESCE(body_template, ''), COALESCE(jitter_min_ms, 0), COALESCE(jitter_max_ms, 0),
			created_at
		FROM c2_profiles WHERE id = ?
	`
	var p C2Profile
	var urisJSON, reqHdrJSON, resHdrJSON string
	err := db.QueryRow(query, id).Scan(&p.ID, &p.Name, &p.UserAgent, &urisJSON,
		&reqHdrJSON, &resHdrJSON, &p.BodyTemplate, &p.JitterMinMS, &p.JitterMaxMS, &p.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(urisJSON), &p.URIs)
	_ = json.Unmarshal([]byte(reqHdrJSON), &p.RequestHeaders)
	_ = json.Unmarshal([]byte(resHdrJSON), &p.ResponseHeaders)
	return &p, nil
}

// ListC2Profiles 全量列表
func (db *DB) ListC2Profiles() ([]*C2Profile, error) {
	query := `
		SELECT id, name, COALESCE(user_agent, ''), COALESCE(uris_json, '[]'),
			COALESCE(request_headers_json, '{}'), COALESCE(response_headers_json, '{}'),
			COALESCE(body_template, ''), COALESCE(jitter_min_ms, 0), COALESCE(jitter_max_ms, 0),
			created_at
		FROM c2_profiles ORDER BY created_at DESC
	`
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*C2Profile
	for rows.Next() {
		var p C2Profile
		var urisJSON, reqHdrJSON, resHdrJSON string
		if err := rows.Scan(&p.ID, &p.Name, &p.UserAgent, &urisJSON,
			&reqHdrJSON, &resHdrJSON, &p.BodyTemplate, &p.JitterMinMS, &p.JitterMaxMS, &p.CreatedAt); err != nil {
			continue
		}
		_ = json.Unmarshal([]byte(urisJSON), &p.URIs)
		_ = json.Unmarshal([]byte(reqHdrJSON), &p.RequestHeaders)
		_ = json.Unmarshal([]byte(resHdrJSON), &p.ResponseHeaders)
		list = append(list, &p)
	}
	return list, rows.Err()
}

// DeleteC2Profile 删除 Profile（不影响已用此 Profile 的 listener，仅断开关联）
func (db *DB) DeleteC2Profile(id string) error {
	if _, err := db.Exec(`UPDATE c2_listeners SET profile_id = '' WHERE profile_id = ?`, id); err != nil {
		return err
	}
	res, err := db.Exec(`DELETE FROM c2_profiles WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}
