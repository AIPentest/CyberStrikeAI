package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/security"
	workflowrunner "cyberstrike-ai/internal/workflow"
	workflowpkg "cyberstrike-ai/internal/workflow/package"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type workflowPackageResolution struct {
	Action        string `json:"action"`
	NewWorkflowID string `json:"new_workflow_id"`
}
type workflowPackageImportRequest struct {
	InspectionID     string                    `json:"inspection_id"`
	Resolution       workflowPackageResolution `json:"resolution"`
	ConfirmOverwrite bool                      `json:"confirm_overwrite"`
}

func (h *WorkflowHandler) ExportPackage(c *gin.Context) {
	wf, err := h.db.GetWorkflowDefinition(c.Param("id"))
	if err != nil {
		writeWorkflowPackageError(c, http.StatusInternalServerError, "WFPKG_EXPORT_FAILED", "failed to export the workflow package", nil)
		return
	}
	if wf == nil {
		writeWorkflowPackageError(c, http.StatusNotFound, "WFPKG_WORKFLOW_NOT_FOUND", "workflow not found", nil)
		return
	}
	pkg, meta, err := workflowpkg.Export(workflowPackageDocument(wf))
	if err != nil {
		writeWorkflowPackageError(c, http.StatusInternalServerError, "WFPKG_EXPORT_FAILED", "failed to export the workflow package", nil)
		return
	}
	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": meta.FileName}))
	c.Header("ETag", `"`+meta.PackageHash+`"`)
	c.Header("X-Workflow-Package-SHA256", meta.PackageHash)
	if h.audit != nil {
		h.audit.RecordOK(c, "workflow_package", "export", "workflow package exported", "workflow", wf.ID, map[string]interface{}{"package_hash": meta.PackageHash})
	}
	c.Data(http.StatusOK, "application/zip", pkg)
}

func (h *WorkflowHandler) CreatePackageInspection(c *gin.Context) {
	session, ok := security.CurrentSession(c)
	if !ok || strings.TrimSpace(session.UserID) == "" {
		writeWorkflowPackageError(c, http.StatusUnauthorized, "WFPKG_INSPECTION_NOT_FOUND", "not authorized to access", nil)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, workflowpkg.MaxArchiveBytes+1)
	file, _, err := c.Request.FormFile("file")
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeWorkflowPackageError(c, http.StatusUnprocessableEntity, "WFPKG_FILE_TOO_LARGE", "the workflow package file exceeds the size limit", nil)
			return
		}
		writeWorkflowPackageError(c, http.StatusUnprocessableEntity, "WFPKG_FILE_REQUIRED", "a workflow package file must be uploaded", nil)
		return
	}
	defer file.Close()
	archive, err := io.ReadAll(io.LimitReader(file, workflowpkg.MaxArchiveBytes+1))
	if err != nil || len(archive) > workflowpkg.MaxArchiveBytes {
		writeWorkflowPackageError(c, http.StatusUnprocessableEntity, "WFPKG_FILE_TOO_LARGE", "the workflow package file exceeds the size limit", nil)
		return
	}
	inspected, err := workflowpkg.InspectArchive(c.Request.Context(), archive, workflowrunner.ValidateGraphJSON)
	if err != nil {
		code := workflowpkg.ErrorCode(err)
		if code == "" {
			code = "WFPKG_INVALID_ARCHIVE"
		}
		if h.audit != nil {
			h.audit.RecordFail(c, "workflow_package", "inspect", "workflow package pre-inspection failed", map[string]interface{}{"code": code, "package_hash": workflowPackageHash(archive)})
		}
		writeWorkflowPackageError(c, http.StatusUnprocessableEntity, code, "workflow package pre-inspection failed", nil)
		return
	}
	state, local, err := h.workflowPackageConflict(inspected.Document.ID, inspected.ContentHash)
	if err != nil {
		writeWorkflowPackageError(c, http.StatusInternalServerError, "WFPKG_IMPORT_FAILED", "failed to read the local workflow", nil)
		return
	}
	summary := workflowPackageInspectionSummary{ID: "wpi_" + strings.ReplaceAll(uuid.NewString(), "-", ""), Status: "ready", ExpiresAt: time.Now().UTC().Add(30 * time.Minute), Package: workflowPackagePackageSummary{PackageFormat: inspected.Manifest.PackageFormat, FormatVersion: inspected.Manifest.FormatVersion, PackageID: inspected.Manifest.PackageID, PackageHash: inspected.PackageHash}, Workflow: workflowPackageWorkflowSummary{SourceID: inspected.Document.ID, Name: inspected.Document.Name, Description: inspected.Document.Description, SourceRevision: inspected.Document.Version, Enabled: inspected.Document.Enabled, ContentHash: inspected.ContentHash, GraphHash: inspected.GraphHash, NodeCount: inspected.NodeCount, EdgeCount: inspected.EdgeCount}, Conflict: workflowPackageConflictSummary{State: state}, Warnings: []string{}}
	if local != nil {
		content, graph, _, _ := workflowpkg.DocumentHashes(workflowPackageDocument(local))
		summary.Conflict.LocalWorkflow = &workflowPackageLocalWorkflow{ID: local.ID, Version: local.Version, ContentHash: content, GraphHash: graph}
	}
	manifestJSON, _ := json.Marshal(inspected.Manifest)
	payloadJSON, err := json.Marshal(inspected.Document)
	if err != nil {
		writeWorkflowPackageError(c, http.StatusInternalServerError, "WFPKG_IMPORT_FAILED", "failed to save the pre-inspection", nil)
		return
	}
	inspectionJSON, _ := json.Marshal(summary)
	record := &database.WorkflowPackageInspection{ID: summary.ID, PackageHash: inspected.PackageHash, ManifestJSON: string(manifestJSON), WorkflowPayloadJSON: string(payloadJSON), InspectionJSON: string(inspectionJSON), SourceWorkflowID: inspected.Document.ID, SourceRevision: inspected.Document.Version, SourceContentHash: inspected.ContentHash, SourceGraphHash: inspected.GraphHash, LocalConflictState: state, CreatedBy: session.UserID, CreatedAt: time.Now().UTC(), ExpiresAt: summary.ExpiresAt}
	if local != nil {
		content, graph, _, _ := workflowpkg.DocumentHashes(workflowPackageDocument(local))
		record.LocalWorkflowID = local.ID
		record.LocalContentHash = content
		record.LocalGraphHash = graph
	}
	if err := h.db.CreateWorkflowPackageInspection(record); err != nil {
		writeWorkflowPackageError(c, http.StatusInternalServerError, "WFPKG_IMPORT_FAILED", "failed to save the pre-inspection", nil)
		return
	}
	if h.audit != nil {
		h.audit.RecordOK(c, "workflow_package", "inspect", "workflow package pre-inspection succeeded", "inspection", record.ID, map[string]interface{}{"package_hash": record.PackageHash, "workflow_id": record.SourceWorkflowID})
	}
	c.JSON(http.StatusCreated, gin.H{"inspection": summary})
}

func (h *WorkflowHandler) GetPackageInspection(c *gin.Context) {
	session, ok := security.CurrentSession(c)
	if !ok {
		writeWorkflowPackageError(c, http.StatusUnauthorized, "WFPKG_INSPECTION_NOT_FOUND", "not authorized to access", nil)
		return
	}
	v, err := h.db.GetWorkflowPackageInspection(c.Param("inspectionId"), session.UserID)
	if err != nil {
		writeWorkflowPackageError(c, http.StatusInternalServerError, "WFPKG_IMPORT_FAILED", "failed to read the pre-inspection", nil)
		return
	}
	if v == nil {
		writeWorkflowPackageError(c, http.StatusNotFound, "WFPKG_INSPECTION_NOT_FOUND", "pre-inspection not found", nil)
		return
	}
	if v.Status == "expired" {
		writeWorkflowPackageError(c, http.StatusConflict, "WFPKG_INSPECTION_EXPIRED", "the pre-inspection has expired", nil)
		return
	}
	var summary any
	_ = json.Unmarshal([]byte(v.InspectionJSON), &summary)
	c.JSON(http.StatusOK, gin.H{"inspection": summary})
}

func (h *WorkflowHandler) ApplyPackageImport(c *gin.Context) {
	session, ok := security.CurrentSession(c)
	if !ok {
		writeWorkflowPackageError(c, http.StatusUnauthorized, "WFPKG_INSPECTION_NOT_FOUND", "not authorized to access", nil)
		return
	}
	key := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if _, err := uuid.Parse(key); err != nil {
		writeWorkflowPackageError(c, http.StatusBadRequest, "WFPKG_IDEMPOTENCY_KEY_REQUIRED", "a UUID idempotency key must be provided", nil)
		return
	}
	var req workflowPackageImportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeWorkflowPackageError(c, http.StatusUnprocessableEntity, "WFPKG_INVALID_ACTION", "invalid import request", nil)
		return
	}
	req.InspectionID = strings.TrimSpace(req.InspectionID)
	req.Resolution.Action = strings.TrimSpace(req.Resolution.Action)
	req.Resolution.NewWorkflowID = strings.TrimSpace(req.Resolution.NewWorkflowID)
	if req.Resolution.Action != "rename" && req.Resolution.NewWorkflowID != "" {
		writeWorkflowPackageError(c, http.StatusUnprocessableEntity, "WFPKG_INVALID_ACTION", "this import action does not accept a new workflow ID", nil)
		return
	}
	requestHash := workflowPackageRequestHash(req)
	imp, replayed, err := h.db.ApplyWorkflowPackageImport(c.Request.Context(), database.WorkflowPackageApplyRequest{InspectionID: req.InspectionID, RequestHash: requestHash, IdempotencyKey: key, ActorUserID: session.UserID, Action: req.Resolution.Action, NewWorkflowID: req.Resolution.NewWorkflowID, ConfirmOverwrite: req.ConfirmOverwrite})
	if err != nil {
		h.writeWorkflowPackageImportError(c, req.InspectionID, err)
		return
	}
	wf, _ := h.db.GetWorkflowDefinition(imp.ResultingWorkflowID)
	if !replayed && (imp.Result == "created" || imp.Result == "overwritten" || imp.Result == "renamed") {
		workflowrunner.InvalidateCompiledCache(imp.ResultingWorkflowID)
	}
	response := h.workflowPackageImportResponse(imp, wf)
	if !replayed && h.audit != nil {
		h.audit.RecordOK(c, "workflow_package", "import", "workflow package imported", "workflow", imp.ResultingWorkflowID, map[string]interface{}{"inspection_id": imp.InspectionID, "action": imp.Action, "result": imp.Result})
	}
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	c.JSON(status, gin.H{"import": response})
}

func (h *WorkflowHandler) GetPackageImport(c *gin.Context) {
	session, ok := security.CurrentSession(c)
	if !ok {
		writeWorkflowPackageError(c, http.StatusUnauthorized, "WFPKG_INSPECTION_NOT_FOUND", "not authorized to access", nil)
		return
	}
	imp, err := h.db.GetWorkflowPackageImport(c.Param("importId"), session.UserID)
	if err != nil {
		writeWorkflowPackageError(c, http.StatusInternalServerError, "WFPKG_IMPORT_FAILED", "failed to read the import result", nil)
		return
	}
	if imp == nil {
		writeWorkflowPackageError(c, http.StatusNotFound, "WFPKG_INSPECTION_NOT_FOUND", "import result not found", nil)
		return
	}
	wf, _ := h.db.GetWorkflowDefinition(imp.ResultingWorkflowID)
	c.JSON(http.StatusOK, gin.H{"import": h.workflowPackageImportResponse(imp, wf)})
}

func (h *WorkflowHandler) workflowPackageConflict(id, sourceHash string) (string, *database.WorkflowDefinition, error) {
	local, err := h.db.GetWorkflowDefinition(id)
	if err != nil {
		return "", nil, err
	}
	if local == nil {
		return "none", nil, nil
	}
	localHash, _, _, err := workflowpkg.DocumentHashes(workflowPackageDocument(local))
	if err != nil {
		return "", nil, err
	}
	if localHash == sourceHash {
		return "identical", local, nil
	}
	return "id_conflict", local, nil
}
func workflowPackageDocument(w *database.WorkflowDefinition) workflowpkg.Document {
	return workflowpkg.Document{ID: w.ID, Name: w.Name, Description: w.Description, Version: w.Version, GraphJSON: w.GraphJSON, Enabled: w.Enabled, UpdatedAt: w.UpdatedAt}
}
func workflowPackageRequestHash(req workflowPackageImportRequest) string {
	value := struct {
		ConfirmOverwrite bool                      `json:"confirm_overwrite"`
		InspectionID     string                    `json:"inspection_id"`
		Resolution       workflowPackageResolution `json:"resolution"`
	}{req.ConfirmOverwrite, req.InspectionID, req.Resolution}
	b, _ := json.Marshal(value)
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}
func workflowPackageHash(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (h *WorkflowHandler) writeWorkflowPackageImportError(c *gin.Context, inspectionID string, err error) {
	var e *database.WorkflowPackageStoreError
	if errors.As(err, &e) {
		status := http.StatusConflict
		if e.Code == "WFPKG_INVALID_ACTION" || e.Code == "WFPKG_INVALID_RENAME_ID" {
			status = http.StatusUnprocessableEntity
		}
		if h.audit != nil {
			h.audit.RecordFail(c, "workflow_package", "import", "workflow package import failed", map[string]interface{}{"code": e.Code, "inspection_id": inspectionID})
		}
		writeWorkflowPackageError(c, status, e.Code, e.Message, nil)
		return
	}
	if h.audit != nil {
		h.audit.RecordFail(c, "workflow_package", "import", "workflow package import failed", map[string]interface{}{"code": "WFPKG_IMPORT_FAILED", "inspection_id": inspectionID})
	}
	writeWorkflowPackageError(c, http.StatusInternalServerError, "WFPKG_IMPORT_FAILED", "failed to import the workflow package", nil)
}
func writeWorkflowPackageError(c *gin.Context, status int, code, message string, details map[string]any) {
	body := gin.H{"code": code, "message": message}
	if len(details) > 0 {
		body["details"] = details
	}
	c.JSON(status, gin.H{"error": body})
}

type workflowPackageInspectionSummary struct {
	ID        string                         `json:"id"`
	Status    string                         `json:"status"`
	ExpiresAt time.Time                      `json:"expires_at"`
	Package   workflowPackagePackageSummary  `json:"package"`
	Workflow  workflowPackageWorkflowSummary `json:"workflow"`
	Conflict  workflowPackageConflictSummary `json:"conflict"`
	Warnings  []string                       `json:"warnings"`
}
type workflowPackagePackageSummary struct {
	PackageFormat string `json:"package_format"`
	FormatVersion string `json:"format_version"`
	PackageID     string `json:"package_id"`
	PackageHash   string `json:"package_hash"`
}
type workflowPackageWorkflowSummary struct {
	SourceID       string `json:"source_id"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	SourceRevision int    `json:"source_revision"`
	Enabled        bool   `json:"enabled"`
	ContentHash    string `json:"content_hash"`
	GraphHash      string `json:"graph_hash"`
	NodeCount      int    `json:"node_count"`
	EdgeCount      int    `json:"edge_count"`
}
type workflowPackageConflictSummary struct {
	State         string                        `json:"state"`
	LocalWorkflow *workflowPackageLocalWorkflow `json:"local_workflow,omitempty"`
}
type workflowPackageLocalWorkflow struct {
	ID          string `json:"id"`
	Version     int    `json:"version"`
	ContentHash string `json:"content_hash"`
	GraphHash   string `json:"graph_hash"`
}

func (h *WorkflowHandler) workflowPackageImportResponse(imp *database.WorkflowPackageImport, wf *database.WorkflowDefinition) gin.H {
	out := gin.H{"id": imp.ID, "inspection_id": imp.InspectionID, "status": "succeeded", "result": imp.Result, "action": imp.Action, "source_workflow_id": imp.SourceWorkflowID, "target_workflow_id": imp.TargetWorkflowID, "applied_at": imp.AppliedAt}
	if wf != nil {
		content, graph, _, _ := workflowpkg.DocumentHashes(workflowPackageDocument(wf))
		out["workflow"] = gin.H{"id": wf.ID, "version": wf.Version, "content_hash": content, "graph_hash": graph}
	}
	return out
}
