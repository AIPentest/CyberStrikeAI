package database

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	RBACSystemRoleAdmin    = "admin"
	RBACSystemRoleOperator = "operator"
	RBACSystemRoleAuditor  = "auditor"
	RBACSystemRoleViewer   = "viewer"

	RBACScopeAll      = "all"
	RBACScopeAssigned = "assigned"
	RBACScopeOwn      = "own"

	RBACMaxBatchResourceAssignments = 100
)

var rbacAssignableResourceTables = map[string]string{
	"project":       "projects",
	"conversation":  "conversations",
	"vulnerability": "vulnerabilities",
	"asset":         "assets",
	"webshell":      "webshell_connections",
	"batch_task":    "batch_task_queues",
	"c2_listener":   "c2_listeners",
}

// RBACUser is a local platform account.
type RBACUser struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	DisplayName  string    `json:"displayName,omitempty"`
	PasswordHash string    `json:"-"`
	Enabled      bool      `json:"enabled"`
	IsBuiltin    bool      `json:"isBuiltin"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// RBACRole groups permissions and a resource visibility scope.
type RBACRole struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Scope       string    `json:"scope"`
	IsSystem    bool      `json:"isSystem"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// RBACResourceAssignment grants a user access to one resource.
type RBACResourceAssignment struct {
	ID             string    `json:"id"`
	UserID         string    `json:"userId"`
	ResourceType   string    `json:"resourceType"`
	ResourceID     string    `json:"resourceId"`
	ResourceLabel  string    `json:"resourceLabel,omitempty"`
	ResourceDetail string    `json:"resourceDetail,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
}

// RBACResourceOption is a safe, minimal projection used by the assignment picker.
// It intentionally excludes resource contents and credentials.
type RBACResourceOption struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Detail string `json:"detail,omitempty"`
}

// RBACAccess is the resolved authorization profile for one user.
type RBACAccess struct {
	User             RBACUser          `json:"user"`
	Roles            []RBACRole        `json:"roles"`
	Permissions      map[string]bool   `json:"permissions"`
	PermissionScopes map[string]string `json:"permissionScopes,omitempty"`
	// Scope is retained as the broadest effective scope for UI compatibility.
	// Authorization decisions must use PermissionScopes so a global read role
	// cannot widen an unrelated write permission from another role.
	Scope string `json:"scope"`
}

func (db *DB) initRBACTables() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS rbac_users (
			id TEXT PRIMARY KEY,
			username TEXT NOT NULL UNIQUE,
			display_name TEXT NOT NULL DEFAULT '',
			password_hash TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			is_builtin INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS rbac_roles (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			description TEXT NOT NULL DEFAULT '',
			scope TEXT NOT NULL DEFAULT 'assigned',
			is_system INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS rbac_permissions (
			key TEXT PRIMARY KEY,
			description TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS rbac_role_permissions (
			role_id TEXT NOT NULL,
			permission_key TEXT NOT NULL,
			created_at DATETIME NOT NULL,
			PRIMARY KEY (role_id, permission_key),
			FOREIGN KEY (role_id) REFERENCES rbac_roles(id) ON DELETE CASCADE,
			FOREIGN KEY (permission_key) REFERENCES rbac_permissions(key) ON DELETE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS rbac_user_roles (
			user_id TEXT NOT NULL,
			role_id TEXT NOT NULL,
			created_at DATETIME NOT NULL,
			PRIMARY KEY (user_id, role_id),
			FOREIGN KEY (user_id) REFERENCES rbac_users(id) ON DELETE CASCADE,
			FOREIGN KEY (role_id) REFERENCES rbac_roles(id) ON DELETE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS rbac_resource_assignments (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			resource_type TEXT NOT NULL,
			resource_id TEXT NOT NULL,
			created_at DATETIME NOT NULL,
			FOREIGN KEY (user_id) REFERENCES rbac_users(id) ON DELETE CASCADE,
			UNIQUE(user_id, resource_type, resource_id)
		);`,
		`CREATE TABLE IF NOT EXISTS robot_user_bindings (
			id TEXT PRIMARY KEY,
			platform TEXT NOT NULL,
			external_user_id TEXT NOT NULL,
			rbac_user_id TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			FOREIGN KEY (rbac_user_id) REFERENCES rbac_users(id) ON DELETE CASCADE,
			UNIQUE(platform, external_user_id)
		);`,
		`CREATE TABLE IF NOT EXISTS robot_binding_codes (
			code_hash TEXT PRIMARY KEY,
			rbac_user_id TEXT NOT NULL,
			expires_at DATETIME NOT NULL,
			used_at DATETIME,
			created_at DATETIME NOT NULL,
			FOREIGN KEY (rbac_user_id) REFERENCES rbac_users(id) ON DELETE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS chat_upload_artifacts (
			relative_path TEXT PRIMARY KEY,
			conversation_id TEXT NOT NULL,
			owner_user_id TEXT NOT NULL,
			created_at DATETIME NOT NULL,
			FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS c2_payload_artifacts (
			filename TEXT PRIMARY KEY,
			payload_id TEXT NOT NULL,
			listener_id TEXT NOT NULL,
			owner_user_id TEXT NOT NULL,
			created_at DATETIME NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_rbac_user_roles_user ON rbac_user_roles(user_id);`,
		`CREATE INDEX IF NOT EXISTS idx_rbac_role_permissions_role ON rbac_role_permissions(role_id);`,
		`CREATE INDEX IF NOT EXISTS idx_rbac_assignments_user_resource ON rbac_resource_assignments(user_id, resource_type, resource_id);`,
		`CREATE INDEX IF NOT EXISTS idx_rbac_assignments_resource ON rbac_resource_assignments(resource_type, resource_id);`,
		`CREATE INDEX IF NOT EXISTS idx_robot_user_bindings_user ON robot_user_bindings(rbac_user_id);`,
		`CREATE INDEX IF NOT EXISTS idx_robot_binding_codes_expiry ON robot_binding_codes(expires_at);`,
		`CREATE INDEX IF NOT EXISTS idx_chat_upload_artifacts_conversation ON chat_upload_artifacts(conversation_id);`,
		`CREATE INDEX IF NOT EXISTS idx_chat_upload_artifacts_owner ON chat_upload_artifacts(owner_user_id);`,
		`CREATE INDEX IF NOT EXISTS idx_c2_payload_artifacts_listener ON c2_payload_artifacts(listener_id);`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func (db *DB) migrateRBACOwnershipColumns() error {
	for _, col := range []struct {
		table string
		name  string
		stmt  string
	}{
		{"projects", "owner_user_id", "ALTER TABLE projects ADD COLUMN owner_user_id TEXT"},
		{"conversations", "owner_user_id", "ALTER TABLE conversations ADD COLUMN owner_user_id TEXT"},
		{"vulnerabilities", "owner_user_id", "ALTER TABLE vulnerabilities ADD COLUMN owner_user_id TEXT"},
		{"webshell_connections", "owner_user_id", "ALTER TABLE webshell_connections ADD COLUMN owner_user_id TEXT"},
		{"batch_task_queues", "owner_user_id", "ALTER TABLE batch_task_queues ADD COLUMN owner_user_id TEXT"},
		{"c2_listeners", "owner_user_id", "ALTER TABLE c2_listeners ADD COLUMN owner_user_id TEXT"},
		{"tool_executions", "owner_user_id", "ALTER TABLE tool_executions ADD COLUMN owner_user_id TEXT"},
		{"tool_executions", "conversation_id", "ALTER TABLE tool_executions ADD COLUMN conversation_id TEXT"},
	} {
		if err := db.addColumnIfMissing(col.table, col.name, col.stmt); err != nil {
			return err
		}
	}
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_tool_executions_owner ON tool_executions(owner_user_id)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_tool_executions_conversation ON tool_executions(conversation_id)`)
	return nil
}

func (db *DB) addColumnIfMissing(table, name, stmt string) error {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM pragma_table_info(?) WHERE name=?", table, name).Scan(&count)
	if err != nil || count == 0 {
		if _, addErr := db.Exec(stmt); addErr != nil {
			msg := strings.ToLower(addErr.Error())
			if !strings.Contains(msg, "duplicate column") && !strings.Contains(msg, "already exists") {
				return fmt.Errorf("添加%s.%s字段失败: %w", table, name, addErr)
			}
		}
	}
	return nil
}

// RBACNeedsAdminPassword reports whether the built-in admin account still needs an initial password.
func (db *DB) RBACNeedsAdminPassword() (bool, error) {
	var userCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM rbac_users`).Scan(&userCount); err != nil {
		return false, err
	}
	if userCount == 0 {
		return true, nil
	}
	var hash sql.NullString
	err := db.QueryRow(`
		SELECT password_hash FROM rbac_users
		WHERE username = 'admin' AND is_builtin = 1
		LIMIT 1
	`).Scan(&hash)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return !hash.Valid || strings.TrimSpace(hash.String) == "", nil
}

// BootstrapRBAC seeds the local admin account and system roles.
func (db *DB) BootstrapRBAC(adminPasswordHash string, permissions map[string]string) error {
	now := time.Now()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	for key, desc := range permissions {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO rbac_permissions (key, description, created_at) VALUES (?, ?, ?)`, key, desc, now); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE rbac_permissions SET description = ? WHERE key = ?`, desc, key); err != nil {
			return err
		}
	}
	// Remove stale/unknown keys so a permission invented by an older build or
	// manual database edit cannot become active automatically if a future route
	// happens to reuse the same name.
	permissionRows, err := tx.Query(`SELECT key FROM rbac_permissions`)
	if err != nil {
		return err
	}
	var stalePermissionKeys []string
	for permissionRows.Next() {
		var key string
		if err := permissionRows.Scan(&key); err != nil {
			_ = permissionRows.Close()
			return err
		}
		if _, known := permissions[key]; !known {
			stalePermissionKeys = append(stalePermissionKeys, key)
		}
	}
	if err := permissionRows.Close(); err != nil {
		return err
	}
	for _, key := range stalePermissionKeys {
		if _, err := tx.Exec(`DELETE FROM rbac_role_permissions WHERE permission_key = ?`, key); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM rbac_permissions WHERE key = ?`, key); err != nil {
			return err
		}
	}

	systemRoles := []RBACRole{
		{ID: RBACSystemRoleAdmin, Name: "管理员", Description: "全局管理权限", Scope: RBACScopeAll, IsSystem: true},
		{ID: RBACSystemRoleOperator, Name: "操作员", Description: "可执行日常安全工作流，不能管理账号与核心配置", Scope: RBACScopeAssigned, IsSystem: true},
		{ID: RBACSystemRoleAuditor, Name: "审计员", Description: "只读查看审计、监控与资产", Scope: RBACScopeAll, IsSystem: true},
		{ID: RBACSystemRoleViewer, Name: "只读用户", Description: "只读查看被授权资源", Scope: RBACScopeAssigned, IsSystem: true},
	}
	for _, role := range systemRoles {
		if _, err := tx.Exec(`
			INSERT INTO rbac_roles (id, name, description, scope, is_system, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET name=excluded.name, description=excluded.description, scope=excluded.scope, is_system=excluded.is_system, updated_at=excluded.updated_at
		`, role.ID, role.Name, role.Description, role.Scope, boolToInt(role.IsSystem), now, now); err != nil {
			return err
		}
	}

	var userCount int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM rbac_users`).Scan(&userCount); err != nil {
		return err
	}
	if userCount == 0 {
		if strings.TrimSpace(adminPasswordHash) == "" {
			return errors.New("admin password hash is required for initial bootstrap")
		}
		if _, err := tx.Exec(`
			INSERT INTO rbac_users (id, username, display_name, password_hash, enabled, is_builtin, created_at, updated_at)
			VALUES (?, 'admin', '管理员', ?, 1, 1, ?, ?)
		`, "admin", adminPasswordHash, now, now); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO rbac_user_roles (user_id, role_id, created_at) VALUES ('admin', ?, ?)`, RBACSystemRoleAdmin, now); err != nil {
			return err
		}
	} else if strings.TrimSpace(adminPasswordHash) != "" {
		if _, err := tx.Exec(`UPDATE rbac_users SET password_hash = ?, updated_at = ? WHERE username = 'admin' AND is_builtin = 1 AND (password_hash = '' OR password_hash IS NULL)`, adminPasswordHash, now); err != nil {
			return err
		}
	}

	if err := grantSystemRolePermissions(tx, permissions); err != nil {
		return err
	}

	return tx.Commit()
}

func grantSystemRolePermissions(tx *serialTx, permissions map[string]string) error {
	now := time.Now()
	// System roles are immutable and owned by the application. Rebuild their
	// grants deterministically so policy tightening also removes permissions
	// seeded by older versions instead of leaving stale INSERT OR IGNORE rows.
	if _, err := tx.Exec(`DELETE FROM rbac_role_permissions WHERE role_id IN (?, ?, ?, ?)`, RBACSystemRoleAdmin, RBACSystemRoleOperator, RBACSystemRoleAuditor, RBACSystemRoleViewer); err != nil {
		return err
	}
	for key := range permissions {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO rbac_role_permissions (role_id, permission_key, created_at) VALUES (?, ?, ?)`, RBACSystemRoleAdmin, key, now); err != nil {
			return err
		}
		switch {
		case key == "auth:self":
			for _, roleID := range []string{RBACSystemRoleOperator, RBACSystemRoleAuditor, RBACSystemRoleViewer} {
				if _, err := tx.Exec(`INSERT OR IGNORE INTO rbac_role_permissions (role_id, permission_key, created_at) VALUES (?, ?, ?)`, roleID, key, now); err != nil {
					return err
				}
			}
		case key == "audit:read":
			if _, err := tx.Exec(`INSERT OR IGNORE INTO rbac_role_permissions (role_id, permission_key, created_at) VALUES (?, ?, ?)`, RBACSystemRoleAuditor, key, now); err != nil {
				return err
			}
		case strings.HasPrefix(key, "rbac:"), strings.HasPrefix(key, "config:"), strings.HasPrefix(key, "terminal:"), strings.HasPrefix(key, "audit:"):
			continue
		case key == "mcp:write" || key == "mcp:external:execute":
			continue
		case key == "roles:write" || key == "roles:delete" ||
			key == "skills:write" || key == "skills:delete" ||
			key == "agents:write" || key == "agents:delete" ||
			key == "knowledge:write" || key == "knowledge:delete" ||
			key == "workflow:write" || key == "workflow:delete" || key == "robot:write":
			continue
		case strings.HasSuffix(key, ":read"):
			for _, roleID := range []string{RBACSystemRoleOperator, RBACSystemRoleAuditor, RBACSystemRoleViewer} {
				if _, err := tx.Exec(`INSERT OR IGNORE INTO rbac_role_permissions (role_id, permission_key, created_at) VALUES (?, ?, ?)`, roleID, key, now); err != nil {
					return err
				}
			}
		default:
			if _, err := tx.Exec(`INSERT OR IGNORE INTO rbac_role_permissions (role_id, permission_key, created_at) VALUES (?, ?, ?)`, RBACSystemRoleOperator, key, now); err != nil {
				return err
			}
		}
	}
	return nil
}

func (db *DB) GetRBACUserByUsername(username string) (*RBACUser, error) {
	username = strings.TrimSpace(strings.ToLower(username))
	if username == "" {
		return nil, sql.ErrNoRows
	}
	return db.scanRBACUser(db.QueryRow(`
		SELECT id, username, display_name, password_hash, enabled, is_builtin, created_at, updated_at
		FROM rbac_users WHERE username = ?
	`, username))
}

func (db *DB) GetRBACUserByID(id string) (*RBACUser, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, sql.ErrNoRows
	}
	return db.scanRBACUser(db.QueryRow(`
		SELECT id, username, display_name, password_hash, enabled, is_builtin, created_at, updated_at
		FROM rbac_users WHERE id = ?
	`, id))
}

func (db *DB) scanRBACUser(row *sql.Row) (*RBACUser, error) {
	var u RBACUser
	var enabled, builtin int
	var createdAt, updatedAt string
	if err := row.Scan(&u.ID, &u.Username, &u.DisplayName, &u.PasswordHash, &enabled, &builtin, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	u.Enabled = enabled != 0
	u.IsBuiltin = builtin != 0
	u.CreatedAt = parseDBTime(createdAt)
	u.UpdatedAt = parseDBTime(updatedAt)
	return &u, nil
}

func (db *DB) ResolveRBACAccess(userID string) (*RBACAccess, error) {
	u, err := db.GetRBACUserByID(userID)
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(`
		SELECT r.id, r.name, r.description, r.scope, r.is_system, r.created_at, r.updated_at
		FROM rbac_roles r
		JOIN rbac_user_roles ur ON ur.role_id = r.id
		WHERE ur.user_id = ?
		ORDER BY r.is_system DESC, r.name ASC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	access := &RBACAccess{
		User: *u, Permissions: map[string]bool{}, PermissionScopes: map[string]string{}, Scope: RBACScopeOwn,
	}
	for rows.Next() {
		var role RBACRole
		var isSystem int
		var createdAt, updatedAt string
		if err := rows.Scan(&role.ID, &role.Name, &role.Description, &role.Scope, &isSystem, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		role.IsSystem = isSystem != 0
		role.CreatedAt = parseDBTime(createdAt)
		role.UpdatedAt = parseDBTime(updatedAt)
		access.Roles = append(access.Roles, role)
		access.Scope = mergeRBACScope(access.Scope, role.Scope)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	prows, err := db.Query(`
		SELECT rp.permission_key, r.scope
		FROM rbac_role_permissions rp
		JOIN rbac_user_roles ur ON ur.role_id = rp.role_id
		JOIN rbac_roles r ON r.id = rp.role_id
		WHERE ur.user_id = ?
	`, userID)
	if err != nil {
		return nil, err
	}
	defer prows.Close()
	for prows.Next() {
		var key, scope string
		if err := prows.Scan(&key, &scope); err != nil {
			return nil, err
		}
		access.Permissions[key] = true
		if existing, ok := access.PermissionScopes[key]; ok {
			access.PermissionScopes[key] = mergeRBACScope(existing, scope)
		} else {
			access.PermissionScopes[key] = scope
		}
	}
	return access, prows.Err()
}

func mergeRBACScope(a, b string) string {
	if a == RBACScopeAll || b == RBACScopeAll {
		return RBACScopeAll
	}
	if a == RBACScopeAssigned || b == RBACScopeAssigned {
		return RBACScopeAssigned
	}
	return RBACScopeOwn
}

func (db *DB) UserCanAccessResource(userID, scope, resourceType, resourceID string) bool {
	userID = strings.TrimSpace(userID)
	resourceType = strings.TrimSpace(resourceType)
	resourceID = strings.TrimSpace(resourceID)
	if userID == "" || resourceType == "" || resourceID == "" {
		return false
	}
	if scope == RBACScopeAll {
		return true
	}
	if scope == RBACScopeOwn {
		if db.userOwnsResource(userID, resourceType, resourceID) {
			return true
		}
	}
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM rbac_resource_assignments WHERE user_id = ? AND resource_type = ? AND resource_id = ?`, userID, resourceType, resourceID).Scan(&n)
	if err == nil && n > 0 {
		return true
	}
	if resourceType == "vulnerability" {
		return db.userCanAccessVulnerabilityViaParent(userID, scope, resourceID)
	}
	if resourceType == "asset" {
		return db.userCanAccessAssetViaParent(userID, scope, resourceID)
	}
	if resourceType == "conversation" {
		return db.userCanAccessConversationViaParent(userID, scope, resourceID)
	}
	if strings.HasPrefix(resourceType, "c2_") {
		return db.userCanAccessC2ViaParent(userID, scope, resourceType, resourceID)
	}
	return false
}

func (db *DB) userCanAccessAssetViaParent(userID, scope, assetID string) bool {
	var projectID sql.NullString
	if err := db.QueryRow(`SELECT project_id FROM assets WHERE id = ?`, assetID).Scan(&projectID); err != nil {
		return false
	}
	return projectID.Valid && strings.TrimSpace(projectID.String) != "" &&
		db.UserCanAccessResource(userID, scope, "project", strings.TrimSpace(projectID.String))
}

func (db *DB) userCanAccessConversationViaParent(userID, scope, conversationID string) bool {
	var projectID sql.NullString
	if err := db.QueryRow(`SELECT project_id FROM conversations WHERE id = ?`, conversationID).Scan(&projectID); err != nil {
		return false
	}
	return projectID.Valid && strings.TrimSpace(projectID.String) != "" &&
		db.UserCanAccessResource(userID, scope, "project", strings.TrimSpace(projectID.String))
}

func (db *DB) userCanAccessVulnerabilityViaParent(userID, scope, vulnerabilityID string) bool {
	var projectID, conversationID sql.NullString
	err := db.QueryRow(`SELECT project_id, conversation_id FROM vulnerabilities WHERE id = ?`, vulnerabilityID).Scan(&projectID, &conversationID)
	if err != nil {
		return false
	}
	if projectID.Valid && strings.TrimSpace(projectID.String) != "" && db.UserCanAccessResource(userID, scope, "project", strings.TrimSpace(projectID.String)) {
		return true
	}
	if conversationID.Valid && strings.TrimSpace(conversationID.String) != "" && db.UserCanAccessResource(userID, scope, "conversation", strings.TrimSpace(conversationID.String)) {
		return true
	}
	return false
}

func (db *DB) UserCanAccessMessage(userID, scope, messageID string) bool {
	var conversationID string
	err := db.QueryRow(`SELECT conversation_id FROM messages WHERE id = ?`, strings.TrimSpace(messageID)).Scan(&conversationID)
	if err != nil {
		return false
	}
	return db.UserCanAccessResource(userID, scope, "conversation", conversationID)
}

func (db *DB) UserCanAccessProcessDetail(userID, scope, processDetailID string) bool {
	var conversationID string
	err := db.QueryRow(`SELECT conversation_id FROM process_details WHERE id = ?`, strings.TrimSpace(processDetailID)).Scan(&conversationID)
	if err != nil {
		return false
	}
	return db.UserCanAccessResource(userID, scope, "conversation", conversationID)
}

func (db *DB) userCanAccessC2ViaParent(userID, scope, resourceType, resourceID string) bool {
	switch resourceType {
	case "c2_session":
		var listenerID string
		if err := db.QueryRow(`SELECT listener_id FROM c2_sessions WHERE id = ?`, resourceID).Scan(&listenerID); err != nil {
			return false
		}
		return db.UserCanAccessResource(userID, scope, "c2_listener", listenerID)
	case "c2_task":
		var sessionID string
		if err := db.QueryRow(`SELECT session_id FROM c2_tasks WHERE id = ?`, resourceID).Scan(&sessionID); err != nil {
			return false
		}
		return db.UserCanAccessResource(userID, scope, "c2_session", sessionID)
	case "c2_file":
		var sessionID string
		if err := db.QueryRow(`SELECT session_id FROM c2_files WHERE id = ?`, resourceID).Scan(&sessionID); err != nil {
			return false
		}
		return db.UserCanAccessResource(userID, scope, "c2_session", sessionID)
	case "c2_event":
		var sessionID, taskID sql.NullString
		if err := db.QueryRow(`SELECT session_id, task_id FROM c2_events WHERE id = ?`, resourceID).Scan(&sessionID, &taskID); err != nil {
			return false
		}
		if sessionID.Valid && strings.TrimSpace(sessionID.String) != "" {
			return db.UserCanAccessResource(userID, scope, "c2_session", strings.TrimSpace(sessionID.String))
		}
		if taskID.Valid && strings.TrimSpace(taskID.String) != "" {
			return db.UserCanAccessResource(userID, scope, "c2_task", strings.TrimSpace(taskID.String))
		}
	}
	return false
}

func (db *DB) userOwnsResource(userID, resourceType, resourceID string) bool {
	table := ""
	switch resourceType {
	case "project":
		table = "projects"
	case "conversation":
		table = "conversations"
	case "vulnerability":
		table = "vulnerabilities"
	case "asset":
		table = "assets"
	case "webshell":
		table = "webshell_connections"
	case "batch_task":
		table = "batch_task_queues"
	case "c2_listener":
		table = "c2_listeners"
	default:
		return false
	}
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM `+table+` WHERE id = ? AND owner_user_id = ?`, resourceID, userID).Scan(&n)
	return err == nil && n > 0
}

func (db *DB) SetResourceOwner(resourceType, resourceID, userID string) error {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil
	}
	table := ""
	switch resourceType {
	case "project":
		table = "projects"
	case "conversation":
		table = "conversations"
	case "vulnerability":
		table = "vulnerabilities"
	case "asset":
		table = "assets"
	case "webshell":
		table = "webshell_connections"
	case "batch_task":
		table = "batch_task_queues"
	case "c2_listener":
		table = "c2_listeners"
	default:
		return nil
	}
	_, err := db.Exec(`UPDATE `+table+` SET owner_user_id = COALESCE(NULLIF(owner_user_id, ''), ?) WHERE id = ?`, userID, resourceID)
	return err
}

func (db *DB) GetResourceOwner(resourceType, resourceID string) string {
	table := ""
	switch strings.TrimSpace(resourceType) {
	case "project":
		table = "projects"
	case "conversation":
		table = "conversations"
	case "vulnerability":
		table = "vulnerabilities"
	case "asset":
		table = "assets"
	case "webshell":
		table = "webshell_connections"
	case "batch_task":
		table = "batch_task_queues"
	case "c2_listener":
		table = "c2_listeners"
	default:
		return ""
	}
	var owner sql.NullString
	if err := db.QueryRow(`SELECT owner_user_id FROM `+table+` WHERE id = ?`, strings.TrimSpace(resourceID)).Scan(&owner); err != nil {
		return ""
	}
	return strings.TrimSpace(owner.String)
}

func (db *DB) AssignResourceToUser(userID, resourceType, resourceID string) error {
	_, err := db.AssignResourcesToUser(userID, resourceType, []string{resourceID})
	return err
}

// ListAssignableRBACResources returns real resources for the admin assignment
// picker without exposing full records or secret-bearing fields.
func (db *DB) ListAssignableRBACResources(resourceType, search string, limit int) ([]RBACResourceOption, error) {
	return db.ListAssignableRBACResourcesPage(resourceType, search, limit, 0)
}

// ListAssignableRBACResourcesPage returns one stable page for the assignment
// picker. Callers can request limit+1 rows to determine whether another page
// exists without running a separate COUNT query.
func (db *DB) ListAssignableRBACResourcesPage(resourceType, search string, limit, offset int) ([]RBACResourceOption, error) {
	resourceType = strings.TrimSpace(resourceType)
	if _, ok := rbacAssignableResourceTables[resourceType]; !ok {
		return nil, fmt.Errorf("不支持的资源类型: %s", resourceType)
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	pattern := "%" + strings.ToLower(strings.NewReplacer(
		`\`, `\\`,
		`%`, `\%`,
		`_`, `\_`,
	).Replace(strings.TrimSpace(search))) + "%"

	var query string
	switch resourceType {
	case "project":
		query = `SELECT id, name, status FROM projects
			WHERE LOWER(name) LIKE ? ESCAPE '\' OR LOWER(id) LIKE ? ESCAPE '\'
			ORDER BY updated_at DESC LIMIT ? OFFSET ?`
	case "conversation":
		query = `SELECT id, COALESCE(NULLIF(TRIM(title), ''), '未命名对话'), COALESCE(project_id, '') FROM conversations
			WHERE LOWER(COALESCE(NULLIF(TRIM(title), ''), id)) LIKE ? ESCAPE '\' OR LOWER(id) LIKE ? ESCAPE '\'
			ORDER BY updated_at DESC LIMIT ? OFFSET ?`
	case "vulnerability":
		query = `SELECT id, title, severity FROM vulnerabilities
			WHERE LOWER(title) LIKE ? ESCAPE '\' OR LOWER(id) LIKE ? ESCAPE '\'
			ORDER BY updated_at DESC LIMIT ? OFFSET ?`
	case "asset":
		query = `SELECT id, COALESCE(NULLIF(host,''),NULLIF(domain,''),NULLIF(ip,''),id), protocol || CASE WHEN port>0 THEN ':' || port ELSE '' END FROM assets
			WHERE LOWER(host) LIKE ? ESCAPE '\' OR LOWER(domain) LIKE ? ESCAPE '\' OR LOWER(ip) LIKE ? ESCAPE '\'
			ORDER BY updated_at DESC LIMIT ? OFFSET ?`
	case "webshell":
		query = `SELECT id, COALESCE(NULLIF(remark, ''), url), type FROM webshell_connections
			WHERE LOWER(COALESCE(NULLIF(remark, ''), url)) LIKE ? ESCAPE '\' OR LOWER(id) LIKE ? ESCAPE '\'
			ORDER BY created_at DESC LIMIT ? OFFSET ?`
	case "batch_task":
		query = `SELECT id, COALESCE(NULLIF(title, ''), id), status FROM batch_task_queues
			WHERE LOWER(COALESCE(NULLIF(title, ''), id)) LIKE ? ESCAPE '\' OR LOWER(id) LIKE ? ESCAPE '\'
			ORDER BY created_at DESC LIMIT ? OFFSET ?`
	case "c2_listener":
		query = `SELECT id, name, type || ' · ' || status FROM c2_listeners
			WHERE LOWER(name) LIKE ? ESCAPE '\' OR LOWER(id) LIKE ? ESCAPE '\'
			ORDER BY created_at DESC LIMIT ? OFFSET ?`
	}

	queryArgs := []interface{}{pattern, pattern}
	if resourceType == "asset" {
		queryArgs = append(queryArgs, pattern)
	}
	queryArgs = append(queryArgs, limit, offset)
	rows, err := db.Query(query, queryArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	options := make([]RBACResourceOption, 0)
	for rows.Next() {
		var option RBACResourceOption
		if err := rows.Scan(&option.ID, &option.Label, &option.Detail); err != nil {
			return nil, err
		}
		option.Label = normalizeRBACResourceLabel(option.Label, option.ID)
		options = append(options, option)
	}
	return options, rows.Err()
}

// CountAssignableRBACResources returns the total rows matching the resource picker filter.
func (db *DB) CountAssignableRBACResources(resourceType, search string) (int, error) {
	resourceType = strings.TrimSpace(resourceType)
	if _, ok := rbacAssignableResourceTables[resourceType]; !ok {
		return 0, fmt.Errorf("不支持的资源类型: %s", resourceType)
	}
	pattern := "%" + strings.ToLower(strings.NewReplacer(
		`\`, `\\`, `%`, `\%`, `_`, `\_`,
	).Replace(strings.TrimSpace(search))) + "%"
	var query string
	switch resourceType {
	case "project":
		query = `SELECT COUNT(*) FROM projects WHERE LOWER(name) LIKE ? ESCAPE '\' OR LOWER(id) LIKE ? ESCAPE '\'`
	case "conversation":
		query = `SELECT COUNT(*) FROM conversations WHERE LOWER(COALESCE(NULLIF(TRIM(title), ''), id)) LIKE ? ESCAPE '\' OR LOWER(id) LIKE ? ESCAPE '\'`
	case "vulnerability":
		query = `SELECT COUNT(*) FROM vulnerabilities WHERE LOWER(title) LIKE ? ESCAPE '\' OR LOWER(id) LIKE ? ESCAPE '\'`
	case "asset":
		query = `SELECT COUNT(*) FROM assets WHERE LOWER(host) LIKE ? ESCAPE '\' OR LOWER(domain) LIKE ? ESCAPE '\' OR LOWER(ip) LIKE ? ESCAPE '\'`
	case "webshell":
		query = `SELECT COUNT(*) FROM webshell_connections WHERE LOWER(COALESCE(NULLIF(remark, ''), url)) LIKE ? ESCAPE '\' OR LOWER(id) LIKE ? ESCAPE '\'`
	case "batch_task":
		query = `SELECT COUNT(*) FROM batch_task_queues WHERE LOWER(COALESCE(NULLIF(title, ''), id)) LIKE ? ESCAPE '\' OR LOWER(id) LIKE ? ESCAPE '\'`
	case "c2_listener":
		query = `SELECT COUNT(*) FROM c2_listeners WHERE LOWER(name) LIKE ? ESCAPE '\' OR LOWER(id) LIKE ? ESCAPE '\'`
	}
	var total int
	queryArgs := []interface{}{pattern, pattern}
	if resourceType == "asset" {
		queryArgs = append(queryArgs, pattern)
	}
	if err := db.QueryRow(query, queryArgs...).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func normalizeRBACResourceLabel(label, id string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return "资源 " + shortRBACResourceID(id)
	}
	if isWeakRBACResourceLabel(label) {
		return label + " · " + shortRBACResourceID(id)
	}
	return label
}

func isWeakRBACResourceLabel(label string) bool {
	runes := []rune(strings.TrimSpace(label))
	if len(runes) <= 1 {
		return true
	}
	if len(runes) <= 3 {
		numeric := true
		for _, r := range runes {
			if r < '0' || r > '9' {
				numeric = false
				break
			}
		}
		return numeric
	}
	return false
}

func shortRBACResourceID(id string) string {
	id = strings.TrimSpace(id)
	if len(id) <= 12 {
		return id
	}
	return id[:8] + "…"
}

func (db *DB) lookupRBACResourceOptionsByIDs(resourceType string, ids []string) (map[string]RBACResourceOption, error) {
	resourceType = strings.TrimSpace(resourceType)
	if _, ok := rbacAssignableResourceTables[resourceType]; !ok {
		return nil, fmt.Errorf("不支持的资源类型: %s", resourceType)
	}
	unique := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, rawID := range ids {
		id := strings.TrimSpace(rawID)
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	out := make(map[string]RBACResourceOption, len(unique))
	if len(unique) == 0 {
		return out, nil
	}

	placeholders := strings.TrimRight(strings.Repeat("?,", len(unique)), ",")
	args := make([]interface{}, 0, len(unique))
	for _, id := range unique {
		args = append(args, id)
	}

	var query string
	switch resourceType {
	case "project":
		query = `SELECT id, name, status FROM projects WHERE id IN (` + placeholders + `)`
	case "conversation":
		query = `SELECT id, COALESCE(NULLIF(TRIM(title), ''), '未命名对话'), COALESCE(project_id, '') FROM conversations WHERE id IN (` + placeholders + `)`
	case "vulnerability":
		query = `SELECT id, title, severity FROM vulnerabilities WHERE id IN (` + placeholders + `)`
	case "asset":
		query = `SELECT id, COALESCE(NULLIF(host,''),NULLIF(domain,''),NULLIF(ip,''),id), protocol || CASE WHEN port>0 THEN ':' || port ELSE '' END FROM assets WHERE id IN (` + placeholders + `)`
	case "webshell":
		query = `SELECT id, COALESCE(NULLIF(remark, ''), url), type FROM webshell_connections WHERE id IN (` + placeholders + `)`
	case "batch_task":
		query = `SELECT id, COALESCE(NULLIF(title, ''), id), status FROM batch_task_queues WHERE id IN (` + placeholders + `)`
	case "c2_listener":
		query = `SELECT id, name, type || ' · ' || status FROM c2_listeners WHERE id IN (` + placeholders + `)`
	default:
		return out, nil
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var option RBACResourceOption
		if err := rows.Scan(&option.ID, &option.Label, &option.Detail); err != nil {
			return nil, err
		}
		option.Label = normalizeRBACResourceLabel(option.Label, option.ID)
		out[option.ID] = option
	}
	return out, rows.Err()
}

func enrichRBACAssignmentLabels(rows []RBACResourceAssignment, lookup func(resourceType string, ids []string) (map[string]RBACResourceOption, error)) error {
	if lookup == nil || len(rows) == 0 {
		return nil
	}
	idsByType := make(map[string][]string)
	for _, row := range rows {
		idsByType[row.ResourceType] = append(idsByType[row.ResourceType], row.ResourceID)
	}
	labelsByType := make(map[string]map[string]RBACResourceOption, len(idsByType))
	for resourceType, ids := range idsByType {
		options, err := lookup(resourceType, ids)
		if err != nil {
			return err
		}
		labelsByType[resourceType] = options
	}
	for i := range rows {
		options := labelsByType[rows[i].ResourceType]
		if options == nil {
			continue
		}
		if option, ok := options[rows[i].ResourceID]; ok {
			rows[i].ResourceLabel = option.Label
			rows[i].ResourceDetail = option.Detail
		}
	}
	return nil
}

// AssignResourcesToUser validates the complete request before writing anything,
// then inserts all grants in one transaction. Existing grants are idempotent.
func (db *DB) AssignResourcesToUser(userID, resourceType string, resourceIDs []string) (int64, error) {
	userID = strings.TrimSpace(userID)
	resourceType = strings.TrimSpace(resourceType)
	if userID == "" || resourceType == "" || len(resourceIDs) == 0 {
		return 0, errors.New("user_id, resource_type and resource_ids are required")
	}
	if len(resourceIDs) > RBACMaxBatchResourceAssignments {
		return 0, fmt.Errorf("一次最多授权 %d 个资源", RBACMaxBatchResourceAssignments)
	}
	table, ok := rbacAssignableResourceTables[resourceType]
	if !ok {
		return 0, fmt.Errorf("不支持的资源类型: %s", resourceType)
	}

	uniqueIDs := make([]string, 0, len(resourceIDs))
	seen := make(map[string]struct{}, len(resourceIDs))
	for _, rawID := range resourceIDs {
		id := strings.TrimSpace(rawID)
		if id == "" {
			return 0, errors.New("资源 ID 不能为空")
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		uniqueIDs = append(uniqueIDs, id)
	}
	if len(uniqueIDs) == 0 {
		return 0, errors.New("资源 ID 不能为空")
	}

	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	var userExists int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM rbac_users WHERE id = ?`, userID).Scan(&userExists); err != nil {
		return 0, err
	}
	if userExists == 0 {
		return 0, errors.New("用户不存在")
	}
	for _, resourceID := range uniqueIDs {
		var exists int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM `+table+` WHERE id = ?`, resourceID).Scan(&exists); err != nil {
			return 0, err
		}
		if exists == 0 {
			return 0, fmt.Errorf("资源不存在: %s/%s", resourceType, resourceID)
		}
	}

	var created int64
	for _, resourceID := range uniqueIDs {
		result, err := tx.Exec(`
			INSERT OR IGNORE INTO rbac_resource_assignments (id, user_id, resource_type, resource_id, created_at)
			VALUES (?, ?, ?, ?, ?)
		`, uuid.NewString(), userID, resourceType, resourceID, time.Now())
		if err != nil {
			return 0, err
		}
		if n, err := result.RowsAffected(); err == nil {
			created += n
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return created, nil
}

// AssignResourcesToUserAuto detects each resource's actual type before writing.
// The whole batch is validated first and committed atomically.
func (db *DB) AssignResourcesToUserAuto(userID string, resourceIDs []string) (int64, map[string]string, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" || len(resourceIDs) == 0 {
		return 0, nil, errors.New("user_id and resource_ids are required")
	}
	if len(resourceIDs) > RBACMaxBatchResourceAssignments {
		return 0, nil, fmt.Errorf("一次最多授权 %d 个资源", RBACMaxBatchResourceAssignments)
	}
	uniqueIDs := make([]string, 0, len(resourceIDs))
	seen := make(map[string]struct{}, len(resourceIDs))
	for _, rawID := range resourceIDs {
		id := strings.TrimSpace(rawID)
		if id == "" {
			return 0, nil, errors.New("资源 ID 不能为空")
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		uniqueIDs = append(uniqueIDs, id)
	}

	tx, err := db.Begin()
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = tx.Rollback() }()
	var userExists int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM rbac_users WHERE id = ?`, userID).Scan(&userExists); err != nil {
		return 0, nil, err
	}
	if userExists == 0 {
		return 0, nil, errors.New("用户不存在")
	}

	typeTablePairs := []struct{ resourceType, table string }{
		{"project", "projects"}, {"conversation", "conversations"},
		{"vulnerability", "vulnerabilities"}, {"webshell", "webshell_connections"},
		{"asset", "assets"},
		{"batch_task", "batch_task_queues"}, {"c2_listener", "c2_listeners"},
	}
	detected := make(map[string]string, len(uniqueIDs))
	for _, resourceID := range uniqueIDs {
		for _, pair := range typeTablePairs {
			var exists int
			if err := tx.QueryRow(`SELECT COUNT(*) FROM `+pair.table+` WHERE id = ?`, resourceID).Scan(&exists); err != nil {
				return 0, nil, err
			}
			if exists > 0 {
				if previous := detected[resourceID]; previous != "" {
					return 0, nil, fmt.Errorf("资源 ID 同时匹配多个类型: %s (%s, %s)", resourceID, previous, pair.resourceType)
				}
				detected[resourceID] = pair.resourceType
			}
		}
		if detected[resourceID] == "" {
			return 0, nil, fmt.Errorf("资源不存在: %s", resourceID)
		}
	}

	var created int64
	for _, resourceID := range uniqueIDs {
		result, err := tx.Exec(`
			INSERT OR IGNORE INTO rbac_resource_assignments (id, user_id, resource_type, resource_id, created_at)
			VALUES (?, ?, ?, ?, ?)
		`, uuid.NewString(), userID, detected[resourceID], resourceID, time.Now())
		if err != nil {
			return 0, nil, err
		}
		if n, err := result.RowsAffected(); err == nil {
			created += n
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, nil, err
	}
	return created, detected, nil
}

func (db *DB) ListRBACUsers() ([]RBACUser, error) {
	rows, err := db.Query(`SELECT id, username, display_name, password_hash, enabled, is_builtin, created_at, updated_at FROM rbac_users ORDER BY username ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RBACUser
	for rows.Next() {
		var u RBACUser
		var enabled, builtin int
		var createdAt, updatedAt string
		if err := rows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.PasswordHash, &enabled, &builtin, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		u.Enabled = enabled != 0
		u.IsBuiltin = builtin != 0
		u.CreatedAt = parseDBTime(createdAt)
		u.UpdatedAt = parseDBTime(updatedAt)
		out = append(out, u)
	}
	return out, rows.Err()
}

func (db *DB) ListRBACRoles() ([]RBACRole, error) {
	rows, err := db.Query(`SELECT id, name, description, scope, is_system, created_at, updated_at FROM rbac_roles ORDER BY is_system DESC, name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RBACRole
	for rows.Next() {
		var r RBACRole
		var system int
		var createdAt, updatedAt string
		if err := rows.Scan(&r.ID, &r.Name, &r.Description, &r.Scope, &system, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		r.IsSystem = system != 0
		r.CreatedAt = parseDBTime(createdAt)
		r.UpdatedAt = parseDBTime(updatedAt)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (db *DB) GetRBACRoleByID(id string) (*RBACRole, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, sql.ErrNoRows
	}
	var r RBACRole
	var system int
	var createdAt, updatedAt string
	err := db.QueryRow(`SELECT id, name, description, scope, is_system, created_at, updated_at FROM rbac_roles WHERE id = ?`, id).
		Scan(&r.ID, &r.Name, &r.Description, &r.Scope, &system, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	r.IsSystem = system != 0
	r.CreatedAt = parseDBTime(createdAt)
	r.UpdatedAt = parseDBTime(updatedAt)
	return &r, nil
}

func (db *DB) UpsertRBACRole(id, name, description, scope string, permissionKeys []string) (*RBACRole, error) {
	id = strings.TrimSpace(id)
	name = strings.TrimSpace(name)
	scope = strings.TrimSpace(scope)
	if name == "" {
		return nil, errors.New("role name is required")
	}
	if scope != RBACScopeAll && scope != RBACScopeAssigned && scope != RBACScopeOwn {
		scope = RBACScopeAssigned
	}
	if id == "" {
		id = uuid.NewString()
	}
	now := time.Now()
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	var isSystem int
	_ = tx.QueryRow(`SELECT is_system FROM rbac_roles WHERE id = ?`, id).Scan(&isSystem)
	if _, err := tx.Exec(`
		INSERT INTO rbac_roles (id, name, description, scope, is_system, created_at, updated_at)
		VALUES (?, ?, ?, ?, 0, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			description = excluded.description,
			scope = excluded.scope,
			updated_at = excluded.updated_at
	`, id, name, strings.TrimSpace(description), scope, now, now); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`DELETE FROM rbac_role_permissions WHERE role_id = ?`, id); err != nil {
		return nil, err
	}
	for _, key := range permissionKeys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		var permissionExists int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM rbac_permissions WHERE key = ?`, key).Scan(&permissionExists); err != nil {
			return nil, err
		}
		if permissionExists == 0 {
			return nil, fmt.Errorf("unknown permission: %s", key)
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO rbac_role_permissions (role_id, permission_key, created_at) VALUES (?, ?, ?)`, id, key, now); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return db.GetRBACRoleByID(id)
}

func (db *DB) DeleteRBACRole(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("role id is required")
	}
	if id == RBACSystemRoleAdmin || id == RBACSystemRoleOperator || id == RBACSystemRoleAuditor || id == RBACSystemRoleViewer {
		return errors.New("system role cannot be deleted")
	}
	_, err := db.Exec(`DELETE FROM rbac_roles WHERE id = ? AND is_system = 0`, id)
	return err
}

func (db *DB) UpdateRBACUserPassword(userID, passwordHash string) error {
	userID = strings.TrimSpace(userID)
	passwordHash = strings.TrimSpace(passwordHash)
	if userID == "" || passwordHash == "" {
		return errors.New("user_id and password_hash are required")
	}
	_, err := db.Exec(`UPDATE rbac_users SET password_hash = ?, updated_at = ? WHERE id = ?`, passwordHash, time.Now(), userID)
	return err
}

func (db *DB) UpdateRBACAdminPassword(passwordHash string) error {
	return db.UpdateRBACUserPassword("admin", passwordHash)
}

func (db *DB) CreateRBACUser(username, displayName, passwordHash string, enabled bool, roleIDs []string) (*RBACUser, error) {
	username = strings.TrimSpace(strings.ToLower(username))
	if username == "" || passwordHash == "" {
		return nil, errors.New("username and password are required")
	}
	id := uuid.NewString()
	now := time.Now()
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`
		INSERT INTO rbac_users (id, username, display_name, password_hash, enabled, is_builtin, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 0, ?, ?)
	`, id, username, strings.TrimSpace(displayName), passwordHash, boolToInt(enabled), now, now); err != nil {
		return nil, err
	}
	for _, roleID := range roleIDs {
		roleID = strings.TrimSpace(roleID)
		if roleID == "" {
			continue
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO rbac_user_roles (user_id, role_id, created_at) VALUES (?, ?, ?)`, id, roleID, now); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return db.GetRBACUserByID(id)
}

func (db *DB) UpdateRBACUser(userID, displayName string, enabled *bool, roleIDs *[]string) error {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return errors.New("user_id is required")
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if enabled != nil {
		if _, err := tx.Exec(`UPDATE rbac_users SET display_name = ?, enabled = ?, updated_at = ? WHERE id = ?`, strings.TrimSpace(displayName), boolToInt(*enabled), time.Now(), userID); err != nil {
			return err
		}
	} else {
		if _, err := tx.Exec(`UPDATE rbac_users SET display_name = ?, updated_at = ? WHERE id = ?`, strings.TrimSpace(displayName), time.Now(), userID); err != nil {
			return err
		}
	}
	if roleIDs != nil {
		if _, err := tx.Exec(`DELETE FROM rbac_user_roles WHERE user_id = ?`, userID); err != nil {
			return err
		}
		for _, roleID := range *roleIDs {
			roleID = strings.TrimSpace(roleID)
			if roleID == "" {
				continue
			}
			if _, err := tx.Exec(`INSERT OR IGNORE INTO rbac_user_roles (user_id, role_id, created_at) VALUES (?, ?, ?)`, userID, roleID, time.Now()); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func (db *DB) DeleteRBACUser(userID string) error {
	userID = strings.TrimSpace(userID)
	if userID == "" || userID == "admin" {
		return errors.New("cannot delete this user")
	}
	_, err := db.Exec(`DELETE FROM rbac_users WHERE id = ? AND is_builtin = 0`, userID)
	return err
}

func (db *DB) ListRBACUserRoleIDs(userID string) ([]string, error) {
	rows, err := db.Query(`SELECT role_id FROM rbac_user_roles WHERE user_id = ? ORDER BY role_id ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (db *DB) ListRBACRolePermissionKeys(roleID string) ([]string, error) {
	rows, err := db.Query(`SELECT permission_key FROM rbac_role_permissions WHERE role_id = ? ORDER BY permission_key ASC`, roleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		out = append(out, key)
	}
	return out, rows.Err()
}

func (db *DB) ListRBACResourceAssignments(userID string) ([]RBACResourceAssignment, error) {
	query := `SELECT id, user_id, resource_type, resource_id, created_at FROM rbac_resource_assignments WHERE 1=1`
	args := []interface{}{}
	if strings.TrimSpace(userID) != "" {
		query += ` AND user_id = ?`
		args = append(args, strings.TrimSpace(userID))
	}
	query += ` ORDER BY created_at DESC`
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RBACResourceAssignment
	for rows.Next() {
		var row RBACResourceAssignment
		var createdAt string
		if err := rows.Scan(&row.ID, &row.UserID, &row.ResourceType, &row.ResourceID, &createdAt); err != nil {
			return nil, err
		}
		row.CreatedAt = parseDBTime(createdAt)
		out = append(out, row)
	}
	if err := enrichRBACAssignmentLabels(out, db.lookupRBACResourceOptionsByIDs); err != nil {
		return nil, err
	}
	return out, rows.Err()
}

func (db *DB) DeleteRBACResourceAssignment(id string) error {
	_, err := db.DeleteRBACResourceAssignmentWithDetails(id)
	return err
}

// DeleteRBACResourceAssignmentWithDetails atomically removes an assignment and
// returns the deleted row so callers can write a complete, attributable audit
// event without racing a separate lookup against another delete.
func (db *DB) DeleteRBACResourceAssignmentWithDetails(id string) (*RBACResourceAssignment, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, errors.New("assignment id is required")
	}
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var row RBACResourceAssignment
	var createdAt string
	err = tx.QueryRow(`
		SELECT id, user_id, resource_type, resource_id, created_at
		FROM rbac_resource_assignments
		WHERE id = ?
	`, id).Scan(&row.ID, &row.UserID, &row.ResourceType, &row.ResourceID, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("资源授权不存在或已撤销")
	}
	if err != nil {
		return nil, err
	}
	row.CreatedAt = parseDBTime(createdAt)

	result, err := tx.Exec(`DELETE FROM rbac_resource_assignments WHERE id = ?`, id)
	if err != nil {
		return nil, err
	}
	if affected, rowsErr := result.RowsAffected(); rowsErr != nil {
		return nil, rowsErr
	} else if affected != 1 {
		return nil, errors.New("资源授权不存在或已撤销")
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &row, nil
}
