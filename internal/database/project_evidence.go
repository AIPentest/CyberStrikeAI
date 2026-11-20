package database

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// LoadProjectFactEvidence fills asset and tool-execution links for the given facts.
func (db *DB) LoadProjectFactEvidence(facts []*ProjectFact) error {
	if len(facts) == 0 {
		return nil
	}
	ids := make([]string, 0, len(facts))
	byID := map[string]*ProjectFact{}
	for _, fact := range facts {
		if fact == nil || fact.ID == "" {
			continue
		}
		ids = append(ids, fact.ID)
		fact.AssetIDs = []string{}
		fact.ExecutionIDs = []string{}
		byID[fact.ID] = fact
	}
	if len(ids) == 0 {
		return nil
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := db.Query(`SELECT fact_id, asset_id FROM project_fact_assets WHERE fact_id IN (`+placeholders+`) ORDER BY asset_id`, args...)
	if err != nil {
		return err
	}
	for rows.Next() {
		var factID, assetID string
		if err = rows.Scan(&factID, &assetID); err != nil {
			rows.Close()
			return err
		}
		if fact := byID[factID]; fact != nil {
			fact.AssetIDs = append(fact.AssetIDs, assetID)
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	rows, err = db.Query(`SELECT fact_id, execution_id FROM project_fact_receipts WHERE fact_id IN (`+placeholders+`) ORDER BY execution_id`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var factID, executionID string
		if err = rows.Scan(&factID, &executionID); err != nil {
			return err
		}
		if fact := byID[factID]; fact != nil {
			fact.ExecutionIDs = append(fact.ExecutionIDs, executionID)
		}
	}
	return rows.Err()
}

// ReplaceProjectFactEvidence replaces the sides the caller sent. Omitted sides stay as they are.
func (db *DB) ReplaceProjectFactEvidence(factID, projectID string, assetIDs, executionIDs []string, replaceAssets, replaceExecutions bool) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = replaceProjectFactEvidence(tx, factID, projectID, assetIDs, executionIDs, replaceAssets, replaceExecutions); err != nil {
		return err
	}
	return tx.Commit()
}

func replaceProjectFactEvidence(tx *sql.Tx, factID, projectID string, assetIDs, executionIDs []string, replaceAssets, replaceExecutions bool) error {
	if replaceAssets {
		if _, err := tx.Exec(`DELETE FROM project_fact_assets WHERE fact_id=?`, factID); err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, id := range assetIDs {
			id = strings.TrimSpace(id)
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			var assetProject string
			err := tx.QueryRow(`SELECT COALESCE(project_id,'') FROM assets WHERE id=?`, id).Scan(&assetProject)
			if err == sql.ErrNoRows {
				return fmt.Errorf("资产 %s 不存在", id)
			}
			if err != nil {
				return err
			}
			if assetProject != "" && assetProject != projectID {
				return fmt.Errorf("资产 %s 不属于当前项目", id)
			}
			if _, err = tx.Exec(`INSERT INTO project_fact_assets(fact_id,asset_id) VALUES(?,?)`, factID, id); err != nil {
				return err
			}
		}
	}
	if replaceExecutions {
		if _, err := tx.Exec(`DELETE FROM project_fact_receipts WHERE fact_id=?`, factID); err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, id := range executionIDs {
			id = strings.TrimSpace(id)
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			var status, conversationID, executionProject string
			err := tx.QueryRow(`SELECT te.status, COALESCE(te.conversation_id,''), COALESCE(c.project_id,'')
				FROM tool_executions te LEFT JOIN conversations c ON c.id=te.conversation_id WHERE te.id=?`, id).Scan(&status, &conversationID, &executionProject)
			if err == sql.ErrNoRows {
				return fmt.Errorf("工具回执 %s 不存在", id)
			}
			if err != nil {
				return err
			}
			if status != "completed" {
				return fmt.Errorf("工具回执 %s 尚未成功完成", id)
			}
			if executionProject != "" && executionProject != projectID {
				return fmt.Errorf("工具回执 %s 不属于当前项目", id)
			}
			if _, err = tx.Exec(`INSERT INTO project_fact_receipts(fact_id,execution_id) VALUES(?,?)`, factID, id); err != nil {
				return err
			}
		}
	}
	return nil
}

// FactVisibility is the caller's project and conversation read boundary.
type FactVisibility struct {
	UserID               string
	ProjectScope         string
	ConversationScope    string
	CanReadProjects      bool
	CanReadConversations bool
}

func (db *DB) FactVisibilityFor(userID string) (FactVisibility, error) {
	access, err := db.ResolveRBACAccess(userID)
	if err != nil || access == nil {
		return FactVisibility{}, err
	}
	vis := FactVisibility{UserID: access.User.ID, ProjectScope: "none", ConversationScope: "none"}
	if access.Permissions["project:read"] {
		vis.CanReadProjects = true
		vis.ProjectScope = access.Scope
		if scope := strings.TrimSpace(access.PermissionScopes["project:read"]); scope != "" {
			vis.ProjectScope = scope
		}
	}
	if access.Permissions["chat:read"] {
		vis.CanReadConversations = true
		vis.ConversationScope = access.Scope
		if scope := strings.TrimSpace(access.PermissionScopes["chat:read"]); scope != "" {
			vis.ConversationScope = scope
		}
	}
	return vis, nil
}

func (v FactVisibility) projectSQL(alias string) (string, []any) {
	if !v.CanReadProjects || v.ProjectScope == "none" {
		return " AND 1=0", nil
	}
	if v.ProjectScope == RBACScopeAll {
		return "", nil
	}
	if v.UserID == "" {
		return " AND 1=0", nil
	}
	if v.ProjectScope == RBACScopeOwn {
		return fmt.Sprintf(` AND (EXISTS (SELECT 1 FROM projects vp WHERE vp.id=%s.project_id AND vp.owner_user_id=?) OR EXISTS (SELECT 1 FROM rbac_resource_assignments vra WHERE vra.user_id=? AND vra.resource_type='project' AND vra.resource_id=%s.project_id))`, alias, alias), []any{v.UserID, v.UserID}
	}
	return fmt.Sprintf(` AND EXISTS (SELECT 1 FROM rbac_resource_assignments vra WHERE vra.user_id=? AND vra.resource_type='project' AND vra.resource_id=%s.project_id)`, alias), []any{v.UserID}
}

// PresentProjectFacts loads evidence links and removes sources the caller cannot read.
// Project, asset and vulnerability responses all use this filter.
func (db *DB) PresentProjectFacts(facts []*ProjectFact, vis FactVisibility) error {
	if err := db.LoadProjectFactEvidence(facts); err != nil {
		return err
	}
	db.redactFactSources(facts, vis)
	return nil
}

func (db *DB) RedactFactEdges(edges []*ProjectFactEdge, vis FactVisibility) {
	for _, edge := range edges {
		if edge != nil && !db.canReadConversation(vis, edge.SourceConversationID) {
			edge.SourceConversationID = ""
		}
	}
}

func (db *DB) redactFactSources(facts []*ProjectFact, vis FactVisibility) {
	for _, fact := range facts {
		if fact == nil {
			continue
		}
		if !db.canReadConversation(vis, fact.SourceConversationID) {
			fact.SourceConversationID = ""
			fact.SourceMessageID = ""
		}
		kept := []string{}
		for _, id := range fact.ExecutionIDs {
			if db.canReadExecution(vis, id) {
				kept = append(kept, id)
			}
		}
		if len(kept) == 0 {
			fact.ExecutionIDs = nil
		} else {
			fact.ExecutionIDs = kept
		}
	}
}

func (db *DB) canReadConversation(vis FactVisibility, id string) bool {
	if !vis.CanReadConversations || strings.TrimSpace(id) == "" {
		return false
	}
	if vis.ConversationScope == RBACScopeAll {
		return true
	}
	if vis.UserID == "" {
		return false
	}
	return db.UserCanAccessResource(vis.UserID, vis.ConversationScope, "conversation", id)
}

func (db *DB) canReadExecution(vis FactVisibility, id string) bool {
	var conversationID string
	if err := db.QueryRow(`SELECT COALESCE(conversation_id,'') FROM tool_executions WHERE id=?`, id).Scan(&conversationID); err != nil {
		return false
	}
	if conversationID == "" {
		return false
	}
	return db.canReadConversation(vis, conversationID)
}

func (db *DB) ListFactsForAsset(assetID string, limit int) ([]*ProjectFact, error) {
	return db.ListVisibleFactsForAsset(assetID, FactVisibility{CanReadProjects: true, ProjectScope: RBACScopeAll, CanReadConversations: true, ConversationScope: RBACScopeAll}, limit)
}

func (db *DB) ListVisibleFactsForAsset(assetID string, vis FactVisibility, limit int) ([]*ProjectFact, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	clause, clauseArgs := vis.projectSQL("f")
	args := append([]any{strings.TrimSpace(assetID)}, clauseArgs...)
	args = append(args, limit)
	rows, err := db.Query(`SELECT f.id, f.project_id, f.fact_key, f.category, f.summary, f.body, f.confidence,
		COALESCE(f.source_conversation_id,''), COALESCE(f.source_message_id,''), f.pinned, COALESCE(f.related_vulnerability_id,''), COALESCE(f.confirmation_note,''), f.created_at, f.updated_at
		FROM project_facts f JOIN project_fact_assets a ON a.fact_id=f.id
		WHERE a.asset_id=?`+clause+` ORDER BY f.updated_at DESC, f.id LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*ProjectFact{}
	for rows.Next() {
		fact, err := scanProjectFactPointers(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, fact)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = db.PresentProjectFacts(out, vis); err != nil {
		return nil, err
	}
	return out, nil
}

type factScanner interface {
	Scan(dest ...any) error
}

func scanProjectFactPointers(row factScanner) (*ProjectFact, error) {
	var fact ProjectFact
	var pinned int
	var conversationID, messageID, vulnID string
	if err := row.Scan(&fact.ID, &fact.ProjectID, &fact.FactKey, &fact.Category, &fact.Summary, &fact.Body, &fact.Confidence, &conversationID, &messageID, &pinned, &vulnID, &fact.ConfirmationNote, &fact.CreatedAt, &fact.UpdatedAt); err != nil {
		return nil, err
	}
	fact.SourceConversationID = conversationID
	fact.SourceMessageID = messageID
	fact.RelatedVulnerabilityID = vulnID
	fact.Pinned = pinned != 0
	return &fact, nil
}

func (db *DB) ProjectFactSummariesForCollection(collectionID string, limit int) ([]ProjectFact, error) {
	return db.VisibleProjectFactSummaries(collectionID, FactVisibility{CanReadProjects: true, ProjectScope: RBACScopeAll, CanReadConversations: true, ConversationScope: RBACScopeAll}, limit)
}

func (db *DB) VisibleProjectFactSummaries(collectionID string, vis FactVisibility, limit int) ([]ProjectFact, error) {
	if limit <= 0 || limit > 20 {
		limit = 12
	}
	clause, clauseArgs := vis.projectSQL("f")
	args := append([]any{strings.TrimSpace(collectionID)}, clauseArgs...)
	args = append(args, limit)
	rows, err := db.Query(`SELECT f.id, f.fact_key, f.summary, f.confidence, COALESCE(f.source_conversation_id,'')
		FROM project_facts f
		JOIN project_fact_assets a ON a.fact_id=f.id
		JOIN asset_collection_members m ON m.asset_id=a.asset_id
		WHERE m.collection_id=? AND f.confidence<>'deprecated'`+clause+`
		GROUP BY f.id ORDER BY MAX(f.updated_at) DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProjectFact{}
	for rows.Next() {
		var fact ProjectFact
		if err = rows.Scan(&fact.ID, &fact.FactKey, &fact.Summary, &fact.Confidence, &fact.SourceConversationID); err != nil {
			return nil, err
		}
		out = append(out, fact)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	ptrs := make([]*ProjectFact, len(out))
	for i := range out {
		ptrs[i] = &out[i]
	}
	db.redactFactSources(ptrs, vis)
	return out, nil
}

// PublishConfirmedProjectFact upserts one confirmed project fact and replaces its asset and receipt links.
func PublishConfirmedProjectFact(tx *sql.Tx, fact ProjectFact, assetIDs, executionIDs []string) (string, error) {
	fact.FactKey = strings.TrimSpace(fact.FactKey)
	fact.ProjectID = strings.TrimSpace(fact.ProjectID)
	if err := ValidateFactKey(fact.FactKey); err != nil {
		return "", err
	}
	if fact.ProjectID == "" {
		return "", fmt.Errorf("项目事实必须属于项目")
	}
	fact.Summary = clipEvidenceText(fact.Summary, 500)
	fact.Body = clipEvidenceText(fact.Body, 8000)
	if fact.Summary == "" {
		return "", fmt.Errorf("项目事实摘要不能为空")
	}
	if strings.TrimSpace(fact.Category) == "" {
		fact.Category = "note"
	}
	if fact.Confidence == "" {
		fact.Confidence = "tentative"
	}
	if fact.Confidence != "confirmed" && fact.Confidence != "tentative" && fact.Confidence != "deprecated" {
		return "", fmt.Errorf("项目事实可信度无效")
	}
	if fact.Confidence == "confirmed" && strings.TrimSpace(fact.ConfirmationNote) == "" {
		return "", fmt.Errorf("升级为已确认需要审核说明")
	}
	now := time.Now().UTC()
	var id string
	err := tx.QueryRow(`SELECT id FROM project_facts WHERE project_id=? AND fact_key=?`, fact.ProjectID, fact.FactKey).Scan(&id)
	if err == sql.ErrNoRows {
		id = uuid.NewString()
		_, err = tx.Exec(`INSERT INTO project_facts(id,project_id,fact_key,category,summary,body,confidence,source_conversation_id,source_message_id,pinned,related_vulnerability_id,confirmation_note,created_at,updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			id, fact.ProjectID, fact.FactKey, fact.Category, fact.Summary, fact.Body, fact.Confidence,
			nullIfEmpty(fact.SourceConversationID), nullIfEmpty(fact.SourceMessageID), 0, nullIfEmpty(fact.RelatedVulnerabilityID), strings.TrimSpace(fact.ConfirmationNote), now, now)
	} else if err == nil {
		_, err = tx.Exec(`UPDATE project_facts SET category=?, summary=?, body=?, confidence=?,
			source_conversation_id=COALESCE(?, source_conversation_id), related_vulnerability_id=COALESCE(?, related_vulnerability_id),
			confirmation_note=CASE WHEN ?<>'' THEN ? ELSE confirmation_note END, updated_at=?
			WHERE id=?`, fact.Category, fact.Summary, fact.Body, fact.Confidence, nullIfEmpty(fact.SourceConversationID), nullIfEmpty(fact.RelatedVulnerabilityID), strings.TrimSpace(fact.ConfirmationNote), strings.TrimSpace(fact.ConfirmationNote), now, id)
	}
	if err != nil {
		return "", err
	}
	if err = replaceProjectFactEvidence(tx, id, fact.ProjectID, assetIDs, executionIDs, true, true); err != nil {
		return "", err
	}
	return id, nil
}

func clipEvidenceText(value string, max int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max])
}

func completedExecutionIDs(tx *sql.Tx, conversationID string) ([]string, error) {
	rows, err := tx.Query(`SELECT id FROM tool_executions WHERE conversation_id=? AND status='completed' ORDER BY created_at, id LIMIT 32`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func escapeLike(value string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(value)
}

func assetIDsForTarget(tx *sql.Tx, projectID, target string) ([]string, error) {
	host := strings.ToLower(strings.TrimSpace(vulnerabilityTargetHost(target)))
	if projectID == "" || host == "" {
		return nil, nil
	}
	// Match the host exactly, or as the authority of a URL. A suffix such as
	// example.com.evil must not satisfy example.com.
	escaped := escapeLike(host)
	rows, err := tx.Query(`SELECT id FROM assets WHERE project_id=? AND (
		lower(domain)=? OR lower(ip)=? OR lower(host)=?
		OR lower(host) LIKE ? ESCAPE '\'
		OR lower(host) LIKE ? ESCAPE '\'
		OR lower(host) LIKE ? ESCAPE '\'
		OR lower(host) LIKE ? ESCAPE '\'
		OR lower(host) LIKE ? ESCAPE '\'
		OR lower(host) LIKE ? ESCAPE '\'
		OR lower(host) LIKE ? ESCAPE '\'
	) LIMIT 32`, projectID, host, host, host,
		escaped+":%",
		"http://"+escaped, "https://"+escaped,
		"http://"+escaped+":%", "https://"+escaped+":%",
		"http://"+escaped+"/%", "https://"+escaped+"/%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// FilterProjectExecutions keeps completed receipts that belong to this project.
// A receipt from another project is skipped so it cannot roll back the caller's own write.
func FilterProjectExecutions(tx *sql.Tx, projectID string, ids []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		var status, executionProject string
		err := tx.QueryRow(`SELECT te.status, COALESCE(c.project_id,'')
			FROM tool_executions te LEFT JOIN conversations c ON c.id=te.conversation_id WHERE te.id=?`, id).Scan(&status, &executionProject)
		if err != nil || status != "completed" {
			continue
		}
		if projectID != "" && executionProject != "" && executionProject != projectID {
			continue
		}
		out = append(out, id)
	}
	return out
}

// recordRetestEvidence writes a sourced project fact. Vulnerability status remains the disposition record.
func recordRetestEvidence(tx *sql.Tx, vulnerabilityID, status, notes, conversationID string) error {
	var projectID, target, title string
	if err := tx.QueryRow(`SELECT COALESCE(project_id,''), COALESCE(target,''), title FROM vulnerabilities WHERE id=?`, vulnerabilityID).Scan(&projectID, &target, &title); err != nil {
		return err
	}
	if projectID == "" {
		_ = tx.QueryRow(`SELECT COALESCE(project_id,'') FROM conversations WHERE id=?`, conversationID).Scan(&projectID)
	}
	assets, err := assetIDsForTarget(tx, projectID, target)
	if err != nil {
		return err
	}
	executions, err := completedExecutionIDs(tx, conversationID)
	if err != nil {
		return err
	}
	executions = FilterProjectExecutions(tx, projectID, executions)
	if projectID != "" {
		summary := clipEvidenceText(title+" 复测："+status, 500)
		if strings.TrimSpace(notes) != "" {
			summary = clipEvidenceText(notes, 500)
		}
		factKey := "retest/" + vulnerabilityID + "/" + conversationID
		if len(factKey) > 128 || ValidateFactKey(factKey) != nil {
			factKey = fmt.Sprintf("retest/%x", sha256.Sum256([]byte(vulnerabilityID+"\n"+conversationID)))
		}
		if _, err = PublishConfirmedProjectFact(tx, ProjectFact{
			ProjectID: projectID, FactKey: factKey, Confidence: "tentative",
			Category: "note", Summary: summary, Body: notes, SourceConversationID: conversationID, RelatedVulnerabilityID: vulnerabilityID,
		}, assets, executions); err != nil {
			return err
		}
	}
	return nil
}
