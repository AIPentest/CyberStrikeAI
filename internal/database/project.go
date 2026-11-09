package database

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

var factKeyPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._/-]*$`)

// ValidateFactKey 校验事实 key（项目内唯一标识）。
func ValidateFactKey(key string) error {
	if key != strings.TrimSpace(key) {
		return fmt.Errorf("fact_key must not have leading or trailing spaces")
	}
	if key == "" {
		return fmt.Errorf("fact_key is required")
	}
	if len(key) > 128 {
		return fmt.Errorf("fact_key is too long (max 128 characters)")
	}
	if !factKeyPattern.MatchString(key) {
		return fmt.Errorf("fact_key is invalid: only letters, digits and . _ / - are allowed and it must start with a letter or digit (camelCase supported)")
	}
	return nil
}

// Project 渗透测试项目（跨对话共享黑板）。
type Project struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	ScopeJSON   string    `json:"scope_json,omitempty"`
	Status      string    `json:"status"` // active | archived
	Pinned      bool      `json:"pinned"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ProjectFact 项目事实（黑板条目）。
type ProjectFact struct {
	ID                     string    `json:"id"`
	ProjectID              string    `json:"project_id"`
	FactKey                string    `json:"fact_key"`
	Category               string    `json:"category"`
	Summary                string    `json:"summary"`
	Body                   string    `json:"body"`
	Confidence             string    `json:"confidence"` // confirmed | tentative | deprecated
	SourceConversationID   string    `json:"source_conversation_id,omitempty"`
	SourceMessageID        string    `json:"source_message_id,omitempty"`
	Pinned                 bool      `json:"pinned"`
	RelatedVulnerabilityID string    `json:"related_vulnerability_id,omitempty"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

// ProjectFactListFilter 事实列表筛选。
type ProjectFactListFilter struct {
	Category                   string
	Confidence                 string
	Search                     string
	RelatedVulnerabilityID     string
	ExcludeDeprecated          bool // 为 true 时排除 confidence=deprecated
	RestrictContextCheckpoints bool // MCP defaults: hide other/unknown conversation checkpoints before pagination.
	ContextConversationID      string
	SourceConversationID       string // Optional provenance filter; it does not widen project or memory scope.
}

// ContextCheckpointKeyPrefix is reserved for system-generated working memory.
const ContextCheckpointKeyPrefix = "note/context-"

// IsContextCheckpointKey distinguishes working memory from ordinary shared facts.
func IsContextCheckpointKey(key string) bool {
	return strings.HasPrefix(strings.TrimSpace(key), ContextCheckpointKeyPrefix)
}

// Conversation scope affects only automatic working memory, never ordinary
// project facts. Unknown origins require an explicit historical read.
func ContextCheckpointVisible(f *ProjectFact, conversationID string, includeHistory bool) bool {
	return f != nil && (!IsContextCheckpointKey(f.FactKey) || includeHistory ||
		(strings.TrimSpace(conversationID) != "" && f.SourceConversationID == strings.TrimSpace(conversationID)))
}

// SaveContextCheckpoint tests project binding in the same SQLite write statement,
// so a concurrent conversation rebind cannot slip between a read and an upsert.
func (db *DB) SaveContextCheckpoint(f *ProjectFact) error {
	if f == nil || !IsContextCheckpointKey(f.FactKey) || strings.TrimSpace(f.SourceConversationID) == "" {
		return fmt.Errorf("invalid context checkpoint")
	}
	if err := ValidateFactKey(f.FactKey); err != nil {
		return err
	}
	now := time.Now()
	result, err := db.Exec(`INSERT INTO project_facts
		(id, project_id, fact_key, category, summary, body, confidence, source_conversation_id, pinned, created_at, updated_at)
		SELECT ?, ?, ?, 'note', ?, ?, 'tentative', ?, 0, ?, ?
		WHERE EXISTS (SELECT 1 FROM conversations WHERE id = ? AND project_id = ?)
		ON CONFLICT(project_id, fact_key) DO UPDATE SET
		category = 'note', summary = excluded.summary, body = excluded.body,
		confidence = 'tentative', source_conversation_id = excluded.source_conversation_id,
		pinned = 0, updated_at = excluded.updated_at`,
		uuid.New().String(), f.ProjectID, f.FactKey, f.Summary, f.Body, f.SourceConversationID,
		now, now, f.SourceConversationID, f.ProjectID)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return fmt.Errorf("checkpoint project does not match current conversation binding")
	}
	return nil
}

// Match strings.TrimSpace for legacy keys without migrating or rewriting them.
const factKeyTrimCharacters = "\t\n\v\f\r \u0085\u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000"

func contextCheckpointScopeSQL(conversationID string) (string, []interface{}) {
	return " AND (substr(trim(fact_key, ?), 1, ?) != ? OR (source_conversation_id = ? AND ? != ''))",
		[]interface{}{factKeyTrimCharacters, len(ContextCheckpointKeyPrefix), ContextCheckpointKeyPrefix, strings.TrimSpace(conversationID), strings.TrimSpace(conversationID)}
}

// CreateProject 创建项目。
func (db *DB) CreateProject(p *Project) (*Project, error) {
	if p.ID == "" {
		p.ID = uuid.New().String()
	}
	if strings.TrimSpace(p.Status) == "" {
		p.Status = "active"
	}
	now := time.Now()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = now

	_, err := db.Exec(
		`INSERT INTO projects (id, name, description, scope_json, status, pinned, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, p.Description, p.ScopeJSON, p.Status, boolToInt(p.Pinned), p.CreatedAt, p.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create the project: %w", err)
	}
	return p, nil
}

// GetProject 获取项目。
func (db *DB) GetProject(id string) (*Project, error) {
	var p Project
	var pinned int
	var createdAt, updatedAt string
	err := db.QueryRow(
		`SELECT id, name, COALESCE(description,''), COALESCE(scope_json,''), status, pinned, created_at, updated_at
		 FROM projects WHERE id = ?`, id,
	).Scan(&p.ID, &p.Name, &p.Description, &p.ScopeJSON, &p.Status, &pinned, &createdAt, &updatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("project not found")
		}
		return nil, fmt.Errorf("failed to get the project: %w", err)
	}
	p.Pinned = pinned != 0
	p.CreatedAt = parseDBTime(createdAt)
	p.UpdatedAt = parseDBTime(updatedAt)
	return &p, nil
}

// GetProjectName returns a project display name without loading the full record.
func (db *DB) GetProjectName(id string) (string, error) {
	var name string
	err := db.QueryRow(`SELECT name FROM projects WHERE id = ?`, id).Scan(&name)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("project not found")
		}
		return "", fmt.Errorf("failed to get the project name: %w", err)
	}
	return strings.TrimSpace(name), nil
}

func projectListSearchPattern(q string) string {
	q = strings.TrimSpace(q)
	if q == "" {
		return ""
	}
	var b strings.Builder
	b.WriteByte('%')
	for _, r := range q {
		switch r {
		case '%', '_', '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('%')
	return b.String()
}

func appendProjectListFilters(query string, args []interface{}, status, search string) (string, []interface{}) {
	if s := strings.TrimSpace(status); s != "" {
		query += " AND status = ?"
		args = append(args, s)
	}
	if pattern := projectListSearchPattern(search); pattern != "" {
		query += ` AND (LOWER(name) LIKE LOWER(?) ESCAPE '\' OR LOWER(COALESCE(description,'')) LIKE LOWER(?) ESCAPE '\' OR LOWER(id) LIKE LOWER(?) ESCAPE '\')`
		args = append(args, pattern, pattern, pattern)
	}
	return query, args
}

func appendProjectAccessFilter(query string, args []interface{}, userID, scope string) (string, []interface{}) {
	userID = strings.TrimSpace(userID)
	if userID == "" || scope == RBACScopeAll {
		return query, args
	}
	query += ` AND (owner_user_id = ? OR EXISTS (
		SELECT 1 FROM rbac_resource_assignments ra
		WHERE ra.user_id = ? AND ra.resource_type = 'project' AND ra.resource_id = projects.id
	))`
	args = append(args, userID, userID)
	return query, args
}

// CountProjects 统计项目数量。
func (db *DB) CountProjects(status, search string) (int, error) {
	query := `SELECT COUNT(*) FROM projects WHERE 1=1`
	args := []interface{}{}
	query, args = appendProjectListFilters(query, args, status, search)
	var count int
	if err := db.QueryRow(query, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("failed to count projects: %w", err)
	}
	return count, nil
}

func (db *DB) CountProjectsForAccess(status, search, userID, scope string) (int, error) {
	query := `SELECT COUNT(*) FROM projects WHERE 1=1`
	args := []interface{}{}
	query, args = appendProjectListFilters(query, args, status, search)
	query, args = appendProjectAccessFilter(query, args, userID, scope)
	var count int
	if err := db.QueryRow(query, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("failed to count projects: %w", err)
	}
	return count, nil
}

// ListProjects 列出项目。
func (db *DB) ListProjects(status, search string, limit, offset int) ([]*Project, error) {
	if limit <= 0 {
		limit = 50
	}
	query := `SELECT id, name, COALESCE(description,''), COALESCE(scope_json,''), status, pinned, created_at, updated_at
		FROM projects WHERE 1=1`
	args := []interface{}{}
	query, args = appendProjectListFilters(query, args, status, search)
	query += " ORDER BY pinned DESC, updated_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list projects: %w", err)
	}
	defer rows.Close()

	var out []*Project
	for rows.Next() {
		var p Project
		var pinned int
		var createdAt, updatedAt string
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.ScopeJSON, &p.Status, &pinned, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		p.Pinned = pinned != 0
		p.CreatedAt = parseDBTime(createdAt)
		p.UpdatedAt = parseDBTime(updatedAt)
		out = append(out, &p)
	}
	return out, rows.Err()
}

func (db *DB) ListProjectsForAccess(status, search string, limit, offset int, userID, scope string) ([]*Project, error) {
	if scope == RBACScopeAll || strings.TrimSpace(userID) == "" {
		return db.ListProjects(status, search, limit, offset)
	}
	if limit <= 0 {
		limit = 50
	}
	query := `SELECT id, name, COALESCE(description,''), COALESCE(scope_json,''), status, pinned, created_at, updated_at
		FROM projects WHERE 1=1`
	args := []interface{}{}
	query, args = appendProjectListFilters(query, args, status, search)
	query, args = appendProjectAccessFilter(query, args, userID, scope)
	query += " ORDER BY pinned DESC, updated_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list projects: %w", err)
	}
	defer rows.Close()
	var out []*Project
	for rows.Next() {
		var p Project
		var pinned int
		var createdAt, updatedAt string
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.ScopeJSON, &p.Status, &pinned, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		p.Pinned = pinned != 0
		p.CreatedAt = parseDBTime(createdAt)
		p.UpdatedAt = parseDBTime(updatedAt)
		out = append(out, &p)
	}
	return out, rows.Err()
}

// UpdateProject 更新项目。
func (db *DB) UpdateProject(p *Project) error {
	p.UpdatedAt = time.Now()
	_, err := db.Exec(
		`UPDATE projects SET name = ?, description = ?, scope_json = ?, status = ?, pinned = ?, updated_at = ? WHERE id = ?`,
		p.Name, p.Description, p.ScopeJSON, p.Status, boolToInt(p.Pinned), p.UpdatedAt, p.ID,
	)
	if err != nil {
		return fmt.Errorf("failed to update the project: %w", err)
	}
	return nil
}

// DeleteProject 删除项目（级联删除事实；对话 project_id 置空由 FK 处理；其他资源 project_id 置空）。
func (db *DB) DeleteProject(id string) error {
	if _, err := db.Exec(`UPDATE vulnerabilities SET project_id = NULL WHERE project_id = ?`, id); err != nil {
		return fmt.Errorf("failed to detach vulnerabilities from the project: %w", err)
	}
	if _, err := db.Exec(`UPDATE assets SET project_id = NULL WHERE project_id = ?`, id); err != nil {
		return fmt.Errorf("failed to detach assets from the project: %w", err)
	}
	if _, err := db.Exec(`UPDATE webshell_connections SET project_id = NULL WHERE project_id = ?`, id); err != nil {
		return fmt.Errorf("failed to detach WebShell connections from the project: %w", err)
	}
	if _, err := db.Exec(`UPDATE c2_listeners SET project_id = NULL WHERE project_id = ?`, id); err != nil {
		return fmt.Errorf("failed to detach C2 listeners from the project: %w", err)
	}
	_, err := db.Exec(`DELETE FROM projects WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("failed to delete the project: %w", err)
	}
	db.removeProjectScopedDirs(id)
	return nil
}

// GetConversationProjectID 返回对话绑定的项目 ID。
func (db *DB) GetConversationProjectID(conversationID string) (string, error) {
	var pid sql.NullString
	err := db.QueryRow(`SELECT project_id FROM conversations WHERE id = ?`, conversationID).Scan(&pid)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("conversation not found")
		}
		return "", err
	}
	if pid.Valid {
		return strings.TrimSpace(pid.String), nil
	}
	return "", nil
}

// SetConversationProjectID 设置对话所属项目（空字符串表示解除绑定）。
func (db *DB) SetConversationProjectID(conversationID, projectID string) error {
	projectID = strings.TrimSpace(projectID)
	if projectID != "" {
		if _, err := db.GetProject(projectID); err != nil {
			return err
		}
	}
	var val interface{}
	if projectID == "" {
		val = nil
	} else {
		val = projectID
	}
	_, err := db.Exec(`UPDATE conversations SET project_id = ?, updated_at = ? WHERE id = ?`, val, time.Now(), conversationID)
	if err != nil {
		return fmt.Errorf("failed to set the conversation project: %w", err)
	}
	return nil
}

// ListProjectFactsForIndex 列出用于黑板索引注入的事实（不含 deprecated，除非 includeDeprecated）。
func (db *DB) ListProjectFactsForIndex(projectID string, includeDeprecated bool, conversationIDs ...string) ([]*ProjectFact, error) {
	query := `SELECT id, project_id, fact_key, category, summary, '', confidence,
		COALESCE(source_conversation_id,''), COALESCE(source_message_id,''), pinned,
		COALESCE(related_vulnerability_id,''), created_at, updated_at
		FROM project_facts WHERE project_id = ?`
	args := []interface{}{projectID}
	if len(conversationIDs) > 0 {
		clause, values := contextCheckpointScopeSQL(conversationIDs[0])
		query += clause
		args = append(args, values...)
	}
	if !includeDeprecated {
		query += " AND confidence != 'deprecated'"
	}
	query += " ORDER BY pinned DESC, updated_at DESC"
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanProjectFacts(rows)
}

// ListProjectFacts 分页列出项目事实。
func (db *DB) ListProjectFacts(projectID string, filter ProjectFactListFilter, limit, offset int) ([]*ProjectFact, error) {
	if limit <= 0 {
		limit = 100
	}
	query := `SELECT id, project_id, fact_key, category, summary, COALESCE(body,''), confidence,
		COALESCE(source_conversation_id,''), COALESCE(source_message_id,''), pinned,
		COALESCE(related_vulnerability_id,''), created_at, updated_at
		FROM project_facts WHERE project_id = ?`
	args := []interface{}{projectID}
	if filter.RestrictContextCheckpoints {
		clause, values := contextCheckpointScopeSQL(filter.ContextConversationID)
		query += clause
		args = append(args, values...)
	}
	if source := strings.TrimSpace(filter.SourceConversationID); source != "" {
		query += " AND source_conversation_id = ?"
		args = append(args, source)
	}
	if c := strings.TrimSpace(filter.Category); c != "" {
		query += " AND category = ?"
		args = append(args, c)
	}
	if c := strings.TrimSpace(filter.Confidence); c != "" {
		query += " AND confidence = ?"
		args = append(args, c)
	}
	if filter.ExcludeDeprecated {
		query += " AND confidence != 'deprecated'"
	}
	if rid := strings.TrimSpace(filter.RelatedVulnerabilityID); rid != "" {
		query += " AND related_vulnerability_id = ?"
		args = append(args, rid)
	}
	if s := strings.TrimSpace(filter.Search); s != "" {
		pat := "%" + s + "%"
		query += " AND (fact_key LIKE ? OR summary LIKE ? OR body LIKE ?)"
		args = append(args, pat, pat, pat)
	}
	query += " ORDER BY pinned DESC, updated_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanProjectFacts(rows)
}

// GetProjectFactByKey 按 key 获取事实。
func (db *DB) GetProjectFactByKey(projectID, factKey string) (*ProjectFact, error) {
	row := db.QueryRow(
		`SELECT id, project_id, fact_key, category, summary, COALESCE(body,''), confidence,
			COALESCE(source_conversation_id,''), COALESCE(source_message_id,''), pinned,
			COALESCE(related_vulnerability_id,''), created_at, updated_at
		 FROM project_facts WHERE project_id = ? AND fact_key = ?`,
		projectID, factKey,
	)
	return scanProjectFactRow(row)
}

// GetProjectFact 按 ID 获取事实。
func (db *DB) GetProjectFact(id string) (*ProjectFact, error) {
	row := db.QueryRow(
		`SELECT id, project_id, fact_key, category, summary, COALESCE(body,''), confidence,
			COALESCE(source_conversation_id,''), COALESCE(source_message_id,''), pinned,
			COALESCE(related_vulnerability_id,''), created_at, updated_at
		 FROM project_facts WHERE id = ?`, id,
	)
	return scanProjectFactRow(row)
}

// mergeFactBodyOnUpdate 更新时若 incoming body 为空则保留已有内容，避免仅改 summary 时丢失攻击链。
func mergeFactBodyOnUpdate(incoming, existing string) string {
	if strings.TrimSpace(incoming) == "" {
		return existing
	}
	return incoming
}

// UpsertProjectFact 创建或更新事实（按 project_id + fact_key）。
func (db *DB) UpsertProjectFact(f *ProjectFact) (*ProjectFact, error) {
	keyErr := ValidateFactKey(f.FactKey)
	if keyErr != nil && (f.ID == "" || ValidateFactKey(strings.TrimSpace(f.FactKey)) != nil) {
		return nil, keyErr
	}
	existing, err := db.GetProjectFactByKey(f.ProjectID, f.FactKey)
	// Older versions accepted and stored padded keys. Preserve their exact
	// identity when editing a loaded record, without allowing new padded keys
	// or silently renaming historical evidence and its relationship references.
	if keyErr != nil && (err != nil || existing == nil || existing.ID != f.ID) {
		return nil, keyErr
	}
	if strings.TrimSpace(f.Category) == "" {
		f.Category = "note"
	}
	if strings.TrimSpace(f.Confidence) == "" {
		f.Confidence = "tentative"
	}
	now := time.Now()

	if err == nil && existing != nil {
		f.ID = existing.ID
		f.CreatedAt = existing.CreatedAt
		f.UpdatedAt = now
		f.Body = mergeFactBodyOnUpdate(f.Body, existing.Body)
		if strings.TrimSpace(f.Category) == "" {
			f.Category = existing.Category
		}
		if strings.TrimSpace(f.Confidence) == "" {
			f.Confidence = existing.Confidence
		}
		_, err = db.Exec(
			`UPDATE project_facts SET category = ?, summary = ?, body = ?, confidence = ?,
				source_conversation_id = COALESCE(?, source_conversation_id),
				source_message_id = COALESCE(?, source_message_id),
				pinned = ?, related_vulnerability_id = ?, updated_at = ?
			 WHERE id = ?`,
			f.Category, f.Summary, f.Body, f.Confidence,
			nullIfEmpty(f.SourceConversationID), nullIfEmpty(f.SourceMessageID), boolToInt(f.Pinned),
			nullIfEmpty(f.RelatedVulnerabilityID), f.UpdatedAt, f.ID,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to update the fact: %w", err)
		}
		return f, nil
	}

	if f.ID == "" {
		f.ID = uuid.New().String()
	}
	f.CreatedAt = now
	f.UpdatedAt = now
	_, err = db.Exec(
		`INSERT INTO project_facts (
			id, project_id, fact_key, category, summary, body, confidence,
			source_conversation_id, source_message_id, pinned, related_vulnerability_id,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		f.ID, f.ProjectID, f.FactKey, f.Category, f.Summary, f.Body, f.Confidence,
		nullIfEmpty(f.SourceConversationID), nullIfEmpty(f.SourceMessageID), boolToInt(f.Pinned),
		nullIfEmpty(f.RelatedVulnerabilityID),
		f.CreatedAt, f.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create the fact: %w", err)
	}
	return f, nil
}

// DeprecateProjectFact 将事实标记为 deprecated（关联边同步 deprecated）。
func (db *DB) DeprecateProjectFact(projectID, factKey string) error {
	res, err := db.Exec(
		`UPDATE project_facts SET confidence = 'deprecated', updated_at = ? WHERE project_id = ? AND fact_key = ?`,
		time.Now(), projectID, factKey,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("fact not found")
	}
	return db.DeprecateProjectFactEdgesForKey(projectID, factKey)
}

// RestoreProjectFact 将已废弃事实恢复为 tentative 或 confirmed（重新参与黑板索引）。
func (db *DB) RestoreProjectFact(projectID, factKey, confidence string) error {
	confidence = strings.TrimSpace(strings.ToLower(confidence))
	if confidence == "" {
		confidence = "tentative"
	}
	if confidence != "confirmed" && confidence != "tentative" {
		return fmt.Errorf("confidence must be confirmed or tentative")
	}

	existing, err := db.GetProjectFactByKey(projectID, factKey)
	if err != nil {
		return fmt.Errorf("fact not found")
	}
	if strings.ToLower(strings.TrimSpace(existing.Confidence)) != "deprecated" {
		return fmt.Errorf("fact is not deprecated")
	}

	_, err = db.Exec(
		`UPDATE project_facts SET confidence = ?, updated_at = ? WHERE project_id = ? AND fact_key = ?`,
		confidence, time.Now(), projectID, factKey,
	)
	return err
}

// DeleteProjectFact 删除事实（级联删除相关边）。
func (db *DB) DeleteProjectFact(id string) error {
	f, err := db.GetProjectFact(id)
	if err != nil {
		return err
	}
	if err := db.DeleteProjectFactEdgesForKey(f.ProjectID, f.FactKey); err != nil {
		return err
	}
	_, err = db.Exec(`DELETE FROM project_facts WHERE id = ?`, id)
	return err
}

func scanProjectFacts(rows *sql.Rows) ([]*ProjectFact, error) {
	var out []*ProjectFact
	for rows.Next() {
		f, err := scanProjectFactFromRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func scanProjectFactRow(row *sql.Row) (*ProjectFact, error) {
	var f ProjectFact
	var pinned int
	var createdAt, updatedAt string
	err := row.Scan(
		&f.ID, &f.ProjectID, &f.FactKey, &f.Category, &f.Summary, &f.Body, &f.Confidence,
		&f.SourceConversationID, &f.SourceMessageID, &pinned,
		&f.RelatedVulnerabilityID, &createdAt, &updatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("fact not found")
		}
		return nil, err
	}
	f.Pinned = pinned != 0
	f.CreatedAt = parseDBTime(createdAt)
	f.UpdatedAt = parseDBTime(updatedAt)
	return &f, nil
}

func scanProjectFactFromRows(rows *sql.Rows) (*ProjectFact, error) {
	var f ProjectFact
	var pinned int
	var createdAt, updatedAt string
	err := rows.Scan(
		&f.ID, &f.ProjectID, &f.FactKey, &f.Category, &f.Summary, &f.Body, &f.Confidence,
		&f.SourceConversationID, &f.SourceMessageID, &pinned,
		&f.RelatedVulnerabilityID, &createdAt, &updatedAt,
	)
	if err != nil {
		return nil, err
	}
	f.Pinned = pinned != 0
	f.CreatedAt = parseDBTime(createdAt)
	f.UpdatedAt = parseDBTime(updatedAt)
	return &f, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullIfEmpty(s string) interface{} {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

func parseDBTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	// go-sqlite3 读 DATETIME 常返回 RFC3339（含 T），写入时可能是空格分隔格式，需兼容多种形态
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05-07:00",
		"2006-01-02T15:04:05.999999999-07:00",
		"2006-01-02T15:04:05-07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05.999999999",
		"2006-01-02T15:04:05",
	}
	for _, layout := range layouts {
		if t, e := time.Parse(layout, s); e == nil {
			return t
		}
	}
	return time.Time{}
}
