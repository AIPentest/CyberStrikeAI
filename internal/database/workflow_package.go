package database

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

type WorkflowPackageInspection struct {
	ID, PackageHash, ManifestJSON, WorkflowPayloadJSON, InspectionJSON    string
	SourceWorkflowID, SourceContentHash, SourceGraphHash                  string
	SourceRevision                                                        int
	LocalConflictState, LocalWorkflowID, LocalContentHash, LocalGraphHash string
	CreatedBy, Status                                                     string
	CreatedAt, ExpiresAt                                                  time.Time
	ConsumedAt                                                            *time.Time
}

type WorkflowPackageImport struct {
	ID, InspectionID, RequestHash, IdempotencyKey, ActorUserID      string
	Action, SourceWorkflowID, TargetWorkflowID, ResultingWorkflowID string
	Result, ErrorCode, ErrorMessage                                 string
	CreatedAt                                                       time.Time
	AppliedAt                                                       *time.Time
}

type WorkflowPackageApplyRequest struct {
	InspectionID, RequestHash, IdempotencyKey, ActorUserID, Action, NewWorkflowID string
	ConfirmOverwrite                                                              bool
}

type WorkflowPackageStoreError struct{ Code, Message string }

func (e *WorkflowPackageStoreError) Error() string { return e.Code + ": " + e.Message }
func workflowPackageStoreError(code, message string) error {
	return &WorkflowPackageStoreError{code, message}
}

func (db *DB) CreateWorkflowPackageInspection(v *WorkflowPackageInspection) error {
	if v == nil || strings.TrimSpace(v.ID) == "" || strings.TrimSpace(v.CreatedBy) == "" {
		return fmt.Errorf("workflow package inspection is incomplete")
	}
	if v.CreatedAt.IsZero() {
		v.CreatedAt = time.Now().UTC()
	}
	if v.ExpiresAt.IsZero() {
		v.ExpiresAt = v.CreatedAt.Add(30 * time.Minute)
	}
	_, err := db.Exec(`INSERT INTO workflow_package_inspections (id,package_hash,manifest_json,workflow_payload_json,inspection_json,source_workflow_id,source_revision,source_content_hash,source_graph_hash,local_conflict_state,local_workflow_id,local_content_hash,local_graph_hash,created_by,status,created_at,expires_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, v.ID, v.PackageHash, v.ManifestJSON, v.WorkflowPayloadJSON, v.InspectionJSON, v.SourceWorkflowID, v.SourceRevision, v.SourceContentHash, v.SourceGraphHash, v.LocalConflictState, nullString(v.LocalWorkflowID), nullString(v.LocalContentHash), nullString(v.LocalGraphHash), v.CreatedBy, "ready", v.CreatedAt.UTC(), v.ExpiresAt.UTC())
	return err
}

func (db *DB) GetWorkflowPackageInspection(id, actor string) (*WorkflowPackageInspection, error) {
	now := time.Now().UTC()
	_, _ = db.Exec(`UPDATE workflow_package_inspections SET status='expired' WHERE status='ready' AND expires_at <= ?`, now)
	row, err := scanWorkflowPackageInspection(db.QueryRow(`SELECT id,package_hash,manifest_json,workflow_payload_json,inspection_json,source_workflow_id,source_revision,source_content_hash,source_graph_hash,local_conflict_state,COALESCE(local_workflow_id,''),COALESCE(local_content_hash,''),COALESCE(local_graph_hash,''),created_by,status,created_at,expires_at,consumed_at FROM workflow_package_inspections WHERE id=? AND created_by=?`, strings.TrimSpace(id), strings.TrimSpace(actor)))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return row, err
}

func scanWorkflowPackageInspection(s interface{ Scan(...any) error }) (*WorkflowPackageInspection, error) {
	var v WorkflowPackageInspection
	var consumed sql.NullTime
	err := s.Scan(&v.ID, &v.PackageHash, &v.ManifestJSON, &v.WorkflowPayloadJSON, &v.InspectionJSON, &v.SourceWorkflowID, &v.SourceRevision, &v.SourceContentHash, &v.SourceGraphHash, &v.LocalConflictState, &v.LocalWorkflowID, &v.LocalContentHash, &v.LocalGraphHash, &v.CreatedBy, &v.Status, &v.CreatedAt, &v.ExpiresAt, &consumed)
	if consumed.Valid {
		t := consumed.Time
		v.ConsumedAt = &t
	}
	return &v, err
}

func (db *DB) GetWorkflowPackageImport(id, actor string) (*WorkflowPackageImport, error) {
	v, err := scanWorkflowPackageImport(db.QueryRow(`SELECT id,inspection_id,request_hash,idempotency_key,actor_user_id,action,source_workflow_id,target_workflow_id,COALESCE(resulting_workflow_id,''),result,COALESCE(error_code,''),COALESCE(error_message,''),created_at,applied_at FROM workflow_package_imports WHERE id=? AND actor_user_id=?`, strings.TrimSpace(id), strings.TrimSpace(actor)))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return v, err
}
func scanWorkflowPackageImport(s interface{ Scan(...any) error }) (*WorkflowPackageImport, error) {
	var v WorkflowPackageImport
	var applied sql.NullTime
	err := s.Scan(&v.ID, &v.InspectionID, &v.RequestHash, &v.IdempotencyKey, &v.ActorUserID, &v.Action, &v.SourceWorkflowID, &v.TargetWorkflowID, &v.ResultingWorkflowID, &v.Result, &v.ErrorCode, &v.ErrorMessage, &v.CreatedAt, &applied)
	if applied.Valid {
		t := applied.Time
		v.AppliedAt = &t
	}
	return &v, err
}

func (db *DB) ApplyWorkflowPackageImport(ctx context.Context, req WorkflowPackageApplyRequest) (*WorkflowPackageImport, bool, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	var existingHash string
	previous, prevErr := scanWorkflowPackageImport(tx.QueryRowContext(ctx, `SELECT id,inspection_id,request_hash,idempotency_key,actor_user_id,action,source_workflow_id,target_workflow_id,COALESCE(resulting_workflow_id,''),result,COALESCE(error_code,''),COALESCE(error_message,''),created_at,applied_at FROM workflow_package_imports WHERE actor_user_id=? AND idempotency_key=?`, req.ActorUserID, req.IdempotencyKey))
	if prevErr == nil {
		existingHash = previous.RequestHash
		if existingHash != req.RequestHash {
			return nil, false, workflowPackageStoreError("WFPKG_IDEMPOTENCY_KEY_REUSED", "the idempotency key was already used for another request")
		}
		return previous, true, nil
	}
	if prevErr != sql.ErrNoRows {
		return nil, false, prevErr
	}
	inspection, err := scanWorkflowPackageInspection(tx.QueryRowContext(ctx, `SELECT id,package_hash,manifest_json,workflow_payload_json,inspection_json,source_workflow_id,source_revision,source_content_hash,source_graph_hash,local_conflict_state,COALESCE(local_workflow_id,''),COALESCE(local_content_hash,''),COALESCE(local_graph_hash,''),created_by,status,created_at,expires_at,consumed_at FROM workflow_package_inspections WHERE id=? AND created_by=?`, req.InspectionID, req.ActorUserID))
	if err == sql.ErrNoRows {
		return nil, false, workflowPackageStoreError("WFPKG_INSPECTION_NOT_FOUND", "pre-inspection not found")
	}
	if err != nil {
		return nil, false, err
	}
	now := time.Now().UTC()
	if !inspection.ExpiresAt.After(now) || inspection.Status == "expired" {
		_, _ = tx.ExecContext(ctx, `UPDATE workflow_package_inspections SET status='expired' WHERE id=?`, inspection.ID)
		return nil, false, workflowPackageStoreError("WFPKG_INSPECTION_EXPIRED", "the pre-inspection has expired")
	}
	if inspection.Status != "ready" {
		return nil, false, workflowPackageStoreError("WFPKG_INSPECTION_CONSUMED", "the pre-inspection has already been used")
	}
	var payload struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		GraphJSON   string `json:"graph_json"`
		Enabled     bool   `json:"enabled"`
	}
	if err := json.Unmarshal([]byte(inspection.WorkflowPayloadJSON), &payload); err != nil {
		return nil, false, fmt.Errorf("decode inspection payload: %w", err)
	}
	targetID := inspection.SourceWorkflowID
	if req.Action == "rename" {
		targetID = strings.TrimSpace(req.NewWorkflowID)
		if !validWorkflowPackageID(targetID) {
			return nil, false, workflowPackageStoreError("WFPKG_INVALID_RENAME_ID", "the new workflow ID is invalid")
		}
	}
	sourceCurrent, err := scanWorkflowDefinition(tx.QueryRowContext(ctx, "SELECT "+workflowDefinitionColumns+" FROM workflow_definitions WHERE id=?", inspection.SourceWorkflowID))
	if err == sql.ErrNoRows {
		sourceCurrent = nil
	} else if err != nil {
		return nil, false, err
	}
	if err := checkWorkflowPackageSnapshot(inspection, sourceCurrent, inspection.SourceWorkflowID); err != nil {
		return nil, false, err
	}
	current := sourceCurrent
	if targetID != inspection.SourceWorkflowID {
		current, err = scanWorkflowDefinition(tx.QueryRowContext(ctx, "SELECT "+workflowDefinitionColumns+" FROM workflow_definitions WHERE id=?", targetID))
		if err == sql.ErrNoRows {
			current = nil
		} else if err != nil {
			return nil, false, err
		}
	}
	result := ""
	resultingID := ""
	switch req.Action {
	case "create":
		if inspection.LocalConflictState != "none" || current != nil {
			return nil, false, workflowPackageStoreError("WFPKG_ID_CONFLICT", "the target workflow already exists")
		}
		result = "created"
		resultingID = targetID
	case "keep_existing":
		if inspection.LocalConflictState == "none" {
			return nil, false, workflowPackageStoreError("WFPKG_ID_CONFLICT", "the current pre-inspection does not allow keeping the local workflow")
		}
		if inspection.LocalConflictState == "identical" {
			result = "skipped_identical"
		} else {
			result = "kept_existing"
		}
		resultingID = targetID
	case "overwrite":
		if inspection.LocalConflictState != "id_conflict" {
			return nil, false, workflowPackageStoreError("WFPKG_ID_CONFLICT", "the current pre-inspection does not allow overwriting")
		}
		if !req.ConfirmOverwrite {
			return nil, false, workflowPackageStoreError("WFPKG_OVERWRITE_CONFIRMATION_REQUIRED", "the overwrite needs confirmation")
		}
		result = "overwritten"
		resultingID = targetID
	case "rename":
		if inspection.LocalConflictState != "id_conflict" || current != nil {
			return nil, false, workflowPackageStoreError("WFPKG_ID_CONFLICT", "the current pre-inspection does not allow saving under the new ID")
		}
		result = "renamed"
		resultingID = targetID
	default:
		return nil, false, workflowPackageStoreError("WFPKG_INVALID_ACTION", "the import action is invalid")
	}
	if result == "created" || result == "renamed" {
		_, err = tx.ExecContext(ctx, `INSERT INTO workflow_definitions (id,name,description,version,graph_json,enabled,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?)`, resultingID, payload.Name, payload.Description, 1, payload.GraphJSON, boolToInt(payload.Enabled), now, now)
	} else if result == "overwritten" {
		_, err = tx.ExecContext(ctx, `UPDATE workflow_definitions SET name=?,description=?,version=version+1,graph_json=?,enabled=?,updated_at=? WHERE id=?`, payload.Name, payload.Description, payload.GraphJSON, boolToInt(payload.Enabled), now, resultingID)
	}
	if err != nil {
		return nil, false, err
	}
	imp := &WorkflowPackageImport{ID: "wpii_" + strings.ReplaceAll(uuid.NewString(), "-", ""), InspectionID: inspection.ID, RequestHash: req.RequestHash, IdempotencyKey: req.IdempotencyKey, ActorUserID: req.ActorUserID, Action: req.Action, SourceWorkflowID: inspection.SourceWorkflowID, TargetWorkflowID: targetID, ResultingWorkflowID: resultingID, Result: result, CreatedAt: now, AppliedAt: &now}
	_, err = tx.ExecContext(ctx, `INSERT INTO workflow_package_imports (id,inspection_id,request_hash,idempotency_key,actor_user_id,action,source_workflow_id,target_workflow_id,resulting_workflow_id,result,created_at,applied_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`, imp.ID, imp.InspectionID, imp.RequestHash, imp.IdempotencyKey, imp.ActorUserID, imp.Action, imp.SourceWorkflowID, imp.TargetWorkflowID, nullString(imp.ResultingWorkflowID), imp.Result, now, now)
	if err != nil {
		return nil, false, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE workflow_package_inspections SET status='consumed',consumed_at=? WHERE id=? AND status='ready'`, now, inspection.ID); err != nil {
		return nil, false, err
	}
	if err = tx.Commit(); err != nil {
		return nil, false, err
	}
	return imp, false, nil
}

func checkWorkflowPackageSnapshot(i *WorkflowPackageInspection, current *WorkflowDefinition, targetID string) error {
	if i.LocalConflictState == "none" {
		if current != nil {
			return workflowPackageStoreError("WFPKG_CONFLICT_CHANGED", "the local workflow has changed")
		}
		return nil
	}
	if current == nil || current.ID != i.LocalWorkflowID || current.ID != targetID {
		return workflowPackageStoreError("WFPKG_CONFLICT_CHANGED", "the local workflow has changed")
	}
	content, graph := workflowDefinitionPackageHashes(current)
	if content != i.LocalContentHash || graph != i.LocalGraphHash {
		return workflowPackageStoreError("WFPKG_CONFLICT_CHANGED", "the local workflow has changed")
	}
	return nil
}
func workflowDefinitionPackageHashes(w *WorkflowDefinition) (string, string) {
	var g any
	dec := json.NewDecoder(strings.NewReader(w.GraphJSON))
	dec.UseNumber()
	_ = dec.Decode(&g)
	graph, _ := json.Marshal(g)
	payload := struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
		Version     int    `json:"version"`
		GraphJSON   string `json:"graph_json"`
		Enabled     bool   `json:"enabled"`
	}{w.ID, w.Name, w.Description, w.Version, string(graph), w.Enabled}
	b, _ := json.Marshal(payload)
	return workflowPackageHash(b), workflowPackageHash(graph)
}
func workflowPackageHash(b []byte) string {
	s := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(s[:])
}
func validWorkflowPackageID(id string) bool {
	if len(id) < 1 || len(id) > 128 {
		return false
	}
	for _, r := range id {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func (db *DB) PurgeWorkflowPackageLifecycle(now time.Time) error {
	now = now.UTC()
	if _, err := db.Exec(`UPDATE workflow_package_inspections SET status='expired' WHERE status='ready' AND expires_at<=?`, now); err != nil {
		return err
	}
	if _, err := db.Exec(`DELETE FROM workflow_package_inspections WHERE status='expired' AND expires_at<? AND NOT EXISTS (SELECT 1 FROM workflow_package_imports i WHERE i.inspection_id=workflow_package_inspections.id)`, now.Add(-24*time.Hour)); err != nil {
		return err
	}
	_, err := db.Exec(`DELETE FROM workflow_package_imports WHERE created_at<?`, now.AddDate(0, 0, -90))
	return err
}
