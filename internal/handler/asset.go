package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/security"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type AssetHandler struct {
	db     *database.DB
	logger *zap.Logger
}

const (
	maxAssetImportBatch    = 100000
	maxAssetOperationBatch = 10000
)

func NewAssetHandler(db *database.DB, logger *zap.Logger) *AssetHandler {
	return &AssetHandler{db: db, logger: logger}
}

func assetAccess(c *gin.Context) database.RBACListAccess {
	if session, ok := security.CurrentSession(c); ok {
		return database.RBACListAccess{UserID: session.UserID, Scope: session.Scope}
	}
	return database.RBACListAccess{}
}

func assetAccessForPermission(c *gin.Context, permission string) database.RBACListAccess {
	if session, ok := security.CurrentSession(c); ok {
		return database.RBACListAccess{UserID: session.UserID, Scope: session.ScopeFor(permission)}
	}
	return database.RBACListAccess{}
}

type importAssetsRequest struct {
	Assets      []*database.Asset `json:"assets" binding:"required"`
	Source      string            `json:"source"`
	SourceQuery string            `json:"source_query"`
}

type assetScanLink struct {
	AssetID        string `json:"asset_id" binding:"required"`
	ConversationID string `json:"conversation_id"`
	QueueID        string `json:"queue_id"`
	TaskID         string `json:"task_id"`
}

type recordAssetScansRequest struct {
	Scans []assetScanLink `json:"scans" binding:"required"`
}

type updateAssetsProjectRequest struct {
	AssetIDs  []string `json:"asset_ids" binding:"required"`
	ProjectID string   `json:"project_id"`
}

type bulkUpdateAssetsRequest struct {
	AssetIDs          []string `json:"asset_ids" binding:"required"`
	Status            *string  `json:"status"`
	ResponsiblePerson *string  `json:"responsible_person"`
	Department        *string  `json:"department"`
	BusinessSystem    *string  `json:"business_system"`
	Environment       *string  `json:"environment"`
	Criticality       *string  `json:"criticality"`
	AddTags           []string `json:"add_tags"`
	RemoveTags        []string `json:"remove_tags"`
}

type assetIDsRequest struct {
	AssetIDs []string `json:"asset_ids" binding:"required"`
}

type mergeAssetsRequest struct {
	AssetIDs  []string `json:"asset_ids" binding:"required"`
	PrimaryID string   `json:"primary_id"`
}

func (h *AssetHandler) Import(c *gin.Context) {
	var req importAssetsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(req.Assets) == 0 || len(req.Assets) > maxAssetImportBatch {
		respondError(c, http.StatusBadRequest, "assets 数量必须在 1-100000 之间", "assets count must be between 1 and 100000")
		return
	}
	owner := ""
	allowGlobal := false
	if session, ok := security.CurrentSession(c); ok {
		owner = session.UserID
		allowGlobal = session.Scope == database.RBACScopeAll
	}
	for _, asset := range req.Assets {
		if asset == nil {
			continue
		}
		if strings.TrimSpace(asset.ProjectID) != "" {
			if session, ok := security.CurrentSession(c); ok && !h.db.UserCanAccessResource(session.UserID, session.Scope, "project", strings.TrimSpace(asset.ProjectID)) {
				respondError(c, http.StatusForbidden, "无权绑定该项目", "no permission to bind the project")
				return
			}
		}
		if strings.TrimSpace(asset.Source) == "" {
			asset.Source = strings.TrimSpace(req.Source)
		}
		if strings.TrimSpace(asset.SourceQuery) == "" {
			asset.SourceQuery = strings.TrimSpace(req.SourceQuery)
		}
	}
	result, err := h.db.UpsertAssets(req.Assets, owner, allowGlobal)
	if err != nil {
		var validationErr *database.AssetValidationError
		if errors.As(err, &validationErr) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		h.logger.Error("failed to import assets", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *AssetHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	filter, err := assetListFilterFromQuery(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	assets, total, err := h.db.ListAssets(pageSize, (page-1)*pageSize, filter, assetAccess(c))
	if err != nil {
		h.logger.Error("failed to load assets", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	totalPages := (total + pageSize - 1) / pageSize
	if totalPages < 1 {
		totalPages = 1
	}
	c.JSON(http.StatusOK, gin.H{"assets": assets, "total": total, "page": page, "page_size": pageSize, "total_pages": totalPages})
}

func assetListFilterFromQuery(c *gin.Context) (database.AssetListFilter, error) {
	filter := database.AssetListFilter{
		Search: strings.TrimSpace(c.Query("q")), Status: strings.ToLower(strings.TrimSpace(c.Query("status"))),
		Protocol: strings.ToLower(strings.TrimSpace(c.Query("protocol"))), ProjectID: strings.TrimSpace(c.Query("project_id")),
		Source: strings.TrimSpace(c.Query("source")), Tag: strings.TrimSpace(c.Query("tag")), Host: strings.TrimSpace(c.Query("host")),
		IP: strings.TrimSpace(c.Query("ip")), Domain: strings.TrimSpace(c.Query("domain")), ScanState: strings.ToLower(strings.TrimSpace(c.Query("scan_state"))),
		SortBy: strings.ToLower(strings.TrimSpace(c.Query("sort_by"))), SortOrder: strings.ToLower(strings.TrimSpace(c.Query("sort_order"))),
		RiskLevel: strings.ToLower(strings.TrimSpace(c.Query("risk_level"))),
		Country:   strings.TrimSpace(c.Query("country")), Province: strings.TrimSpace(c.Query("province")), City: strings.TrimSpace(c.Query("city")),
		ResponsiblePerson: strings.TrimSpace(c.Query("responsible_person")), Department: strings.TrimSpace(c.Query("department")),
		BusinessSystem: strings.TrimSpace(c.Query("business_system")), Environment: strings.ToLower(strings.TrimSpace(c.Query("environment"))),
		Criticality: strings.ToLower(strings.TrimSpace(c.Query("criticality"))),
	}
	if raw := strings.TrimSpace(c.Query("port")); raw != "" {
		port, err := strconv.Atoi(raw)
		if err != nil || port < 0 || port > 65535 {
			return filter, &assetQueryError{field: "port", value: raw}
		}
		filter.Port = &port
	}
	for field, target := range map[string]**int{
		"min_vulnerabilities": &filter.MinVulnerabilities,
		"max_vulnerabilities": &filter.MaxVulnerabilities,
		"scan_overdue_days":   &filter.ScanOverdueDays,
	} {
		raw := strings.TrimSpace(c.Query(field))
		if raw == "" {
			continue
		}
		value, err := strconv.Atoi(raw)
		if err != nil || value < 0 || (field == "scan_overdue_days" && value == 0) {
			return filter, &assetQueryError{field: field, value: raw}
		}
		*target = &value
	}
	var err error
	if filter.LastScanBefore, err = parseAssetQueryTime("last_scan_before", c.Query("last_scan_before")); err != nil {
		return filter, err
	}
	if filter.LastScanAfter, err = parseAssetQueryTime("last_scan_after", c.Query("last_scan_after")); err != nil {
		return filter, err
	}
	if filter.FirstSeenBefore, err = parseAssetQueryTime("first_seen_before", c.Query("first_seen_before")); err != nil {
		return filter, err
	}
	if filter.FirstSeenAfter, err = parseAssetQueryTime("first_seen_after", c.Query("first_seen_after")); err != nil {
		return filter, err
	}
	if filter.LastSeenBefore, err = parseAssetQueryTime("last_seen_before", c.Query("last_seen_before")); err != nil {
		return filter, err
	}
	if filter.LastSeenAfter, err = parseAssetQueryTime("last_seen_after", c.Query("last_seen_after")); err != nil {
		return filter, err
	}
	return filter, nil
}

// Selection resolves all assets matching the current filter for cross-page actions.
func (h *AssetHandler) Selection(c *gin.Context) {
	filter, err := assetListFilterFromQuery(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	assets, total, err := h.db.ListAssetsForOperation(maxAssetOperationBatch, filter, assetAccess(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "total": total})
		return
	}
	c.JSON(http.StatusOK, gin.H{"assets": assets, "total": total})
}

type assetQueryError struct{ field, value string }

func (e *assetQueryError) Error() string {
	return e.field + " parameter is invalid: " + e.value
}

func parseAssetQueryTime(field, value string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return &parsed, nil
		}
	}
	return nil, &assetQueryError{field: field, value: value}
}

func (h *AssetHandler) Stats(c *gin.Context) {
	days := 30
	if raw := strings.TrimSpace(c.Query("days")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || (parsed != 7 && parsed != 30 && parsed != 90) {
			respondError(c, http.StatusBadRequest, "days 仅支持 7、30 或 90", "days only supports 7, 30, or 90")
			return
		}
		days = parsed
	}
	stats, err := h.db.GetAssetStats(assetAccess(c), days)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, stats)
}

// RecordScans stores the execution link created by the asset-library scan action.
func (h *AssetHandler) RecordScans(c *gin.Context) {
	var req recordAssetScansRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(req.Scans) == 0 || len(req.Scans) > maxAssetOperationBatch {
		respondError(c, http.StatusBadRequest, "scans 数量必须在 1-10000 之间", "scans count must be between 1 and 10000")
		return
	}
	access := assetAccess(c)
	for _, scan := range req.Scans {
		conversationID := strings.TrimSpace(scan.ConversationID)
		queueID := strings.TrimSpace(scan.QueueID)
		taskID := strings.TrimSpace(scan.TaskID)
		if conversationID == "" && taskID == "" {
			respondError(c, http.StatusBadRequest, "conversation_id 或 task_id 至少需要一个", "conversation_id or task_id is required (at least one)")
			return
		}
		if taskID != "" && (queueID == "" || !h.db.BatchTaskBelongsToQueue(taskID, queueID)) {
			respondError(c, http.StatusBadRequest, "任务不属于指定队列", "task does not belong to the specified queue")
			return
		}
		if _, err := h.db.GetAsset(strings.TrimSpace(scan.AssetID), access); err != nil {
			respondError(c, http.StatusForbidden, "资产不存在或无权扫描", "asset not found or no permission to scan")
			return
		}
		if session, ok := security.CurrentSession(c); ok {
			if id := conversationID; id != "" && !h.db.UserCanAccessResource(session.UserID, session.Scope, "conversation", id) {
				respondError(c, http.StatusForbidden, "无权关联该对话", "no permission to link the conversation")
				return
			}
			if id := queueID; id != "" && !h.db.UserCanAccessResource(session.UserID, session.Scope, "batch_task", id) {
				respondError(c, http.StatusForbidden, "无权关联该任务队列", "no permission to link the task queue")
				return
			}
		}
	}
	for _, scan := range req.Scans {
		if err := h.db.MarkAssetScanned(scan.AssetID, scan.ConversationID, scan.QueueID, scan.TaskID, access); err != nil {
			h.logger.Error("failed to record the asset scan", zap.String("asset_id", scan.AssetID), zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"updated": len(req.Scans)})
}

func (h *AssetHandler) Update(c *gin.Context) {
	var asset database.Asset
	if err := c.ShouldBindJSON(&asset); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if asset.ProjectID != "" {
		if session, ok := security.CurrentSession(c); ok && !h.db.UserCanAccessResource(session.UserID, session.Scope, "project", asset.ProjectID) {
			respondError(c, http.StatusForbidden, "无权绑定该项目", "no permission to bind the project")
			return
		}
	}
	if err := h.db.UpdateAsset(c.Param("id"), &asset, assetAccess(c)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	updated, err := h.db.GetAsset(c.Param("id"), assetAccess(c))
	if err != nil {
		respondError(c, http.StatusNotFound, "资产不存在", "asset not found")
		return
	}
	c.JSON(http.StatusOK, updated)
}

// UpdateProjectBinding replaces the project binding for a selected asset set.
func (h *AssetHandler) UpdateProjectBinding(c *gin.Context) {
	var req updateAssetsProjectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(req.AssetIDs) == 0 || len(req.AssetIDs) > maxAssetOperationBatch {
		respondError(c, http.StatusBadRequest, "asset_ids 数量必须在 1-10000 之间", "asset_ids count must be between 1 and 10000")
		return
	}
	req.ProjectID = strings.TrimSpace(req.ProjectID)
	if req.ProjectID != "" {
		if _, err := h.db.GetProject(req.ProjectID); err != nil {
			respondError(c, http.StatusBadRequest, "项目不存在", "project not found")
			return
		}
		if session, ok := security.CurrentSession(c); ok && !h.db.UserCanAccessResource(session.UserID, session.Scope, "project", req.ProjectID) {
			respondError(c, http.StatusForbidden, "无权绑定该项目", "no permission to bind the project")
			return
		}
	}
	updated, err := h.db.UpdateAssetsProject(req.AssetIDs, req.ProjectID, assetAccess(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"updated": updated, "project_id": req.ProjectID})
}

func (h *AssetHandler) BulkUpdate(c *gin.Context) {
	var req bulkUpdateAssetsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(req.AssetIDs) == 0 || len(req.AssetIDs) > maxAssetOperationBatch {
		respondError(c, http.StatusBadRequest, "asset_ids 数量必须在 1-10000 之间", "asset_ids count must be between 1 and 10000")
		return
	}
	updated, err := h.db.UpdateAssetsBulk(req.AssetIDs, database.AssetBulkPatch{
		Status: req.Status, ResponsiblePerson: req.ResponsiblePerson, Department: req.Department,
		BusinessSystem: req.BusinessSystem, Environment: req.Environment, Criticality: req.Criticality,
		AddTags: req.AddTags, RemoveTags: req.RemoveTags,
	}, assetAccess(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"updated": updated})
}

func (h *AssetHandler) BatchDelete(c *gin.Context) {
	var req assetIDsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(req.AssetIDs) == 0 || len(req.AssetIDs) > maxAssetOperationBatch {
		respondError(c, http.StatusBadRequest, "asset_ids 数量必须在 1-10000 之间", "asset_ids count must be between 1 and 10000")
		return
	}
	deleted, err := h.db.DeleteAssets(req.AssetIDs, assetAccess(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": deleted})
}

func assetIdentityKeys(asset *database.Asset) map[string]struct{} {
	keys := map[string]struct{}{}
	if value := strings.ToLower(strings.TrimSpace(asset.Domain)); value != "" {
		keys["domain:"+value] = struct{}{}
	}
	if value := strings.ToLower(strings.Trim(strings.TrimSpace(asset.IP), "[]")); value != "" {
		keys["ip:"+value] = struct{}{}
	}
	if value := strings.ToLower(strings.TrimSpace(asset.Host)); value != "" {
		keys["host:"+value] = struct{}{}
	}
	return keys
}

func shareAssetIdentity(left, right *database.Asset) bool {
	for key := range assetIdentityKeys(left) {
		if _, ok := assetIdentityKeys(right)[key]; ok {
			return true
		}
	}
	return false
}

// Merge keeps the selected primary asset and safely combines compatible duplicate metadata.
func (h *AssetHandler) Merge(c *gin.Context) {
	var req mergeAssetsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(req.AssetIDs) < 2 || len(req.AssetIDs) > 100 {
		respondError(c, http.StatusBadRequest, "合并资产数量必须在 2-100 之间", "merge asset count must be between 2 and 100")
		return
	}
	writeAccess := assetAccessForPermission(c, "asset:write")
	deleteAccess := assetAccessForPermission(c, "asset:delete")
	primaryID := strings.TrimSpace(req.PrimaryID)
	if primaryID == "" {
		primaryID = strings.TrimSpace(req.AssetIDs[0])
	}
	primary, err := h.db.GetAsset(primaryID, writeAccess)
	if err != nil {
		respondError(c, http.StatusBadRequest, "主资产不存在或无权访问", "primary asset not found or no access")
		return
	}
	others := make([]*database.Asset, 0, len(req.AssetIDs)-1)
	seen := map[string]struct{}{primaryID: {}}
	for _, id := range req.AssetIDs {
		id = strings.TrimSpace(id)
		if id == "" || id == primaryID {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		item, err := h.db.GetAsset(id, writeAccess)
		if err != nil {
			respondError(c, http.StatusBadRequest, "部分资产不存在或无权访问", "some assets not found or no access")
			return
		}
		if !shareAssetIdentity(primary, item) {
			respondError(c, http.StatusBadRequest, "所选资产没有共同域名、IP 或 Host，不能判定为重复资产", "selected assets share no common domain, IP, or Host; cannot judge them duplicates")
			return
		}
		others = append(others, item)
	}
	if len(others) == 0 {
		respondError(c, http.StatusBadRequest, "至少需要两个不同资产", "at least two distinct assets are required")
		return
	}
	mergeText := func(dst *string, src string) {
		if strings.TrimSpace(*dst) == "" && strings.TrimSpace(src) != "" {
			*dst = src
		}
	}
	tagSet := map[string]struct{}{}
	for _, tag := range primary.Tags {
		tagSet[tag] = struct{}{}
	}
	for _, item := range others {
		mergeText(&primary.ProjectID, item.ProjectID)
		mergeText(&primary.Host, item.Host)
		mergeText(&primary.IP, item.IP)
		mergeText(&primary.Domain, item.Domain)
		mergeText(&primary.Protocol, item.Protocol)
		mergeText(&primary.Title, item.Title)
		mergeText(&primary.Server, item.Server)
		mergeText(&primary.Country, item.Country)
		mergeText(&primary.Province, item.Province)
		mergeText(&primary.City, item.City)
		mergeText(&primary.ResponsiblePerson, item.ResponsiblePerson)
		mergeText(&primary.Department, item.Department)
		mergeText(&primary.BusinessSystem, item.BusinessSystem)
		mergeText(&primary.Environment, item.Environment)
		mergeText(&primary.Criticality, item.Criticality)
		for _, tag := range item.Tags {
			tagSet[tag] = struct{}{}
		}
	}
	primary.Tags = primary.Tags[:0]
	for tag := range tagSet {
		primary.Tags = append(primary.Tags, tag)
	}
	if len(primary.Tags) > 30 {
		respondError(c, http.StatusBadRequest, "合并后标签超过 30 个", "merged tags exceed 30")
		return
	}
	ids := make([]string, 0, len(others))
	for _, item := range others {
		ids = append(ids, item.ID)
	}
	merged, err := h.db.MergeAssets(primary, ids, writeAccess, deleteAccess)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	updated, _ := h.db.GetAsset(primary.ID, writeAccess)
	c.JSON(http.StatusOK, gin.H{"merged": merged, "asset": updated})
}

func (h *AssetHandler) Delete(c *gin.Context) {
	if err := h.db.DeleteAsset(c.Param("id"), assetAccess(c)); err != nil {
		respondError(c, http.StatusNotFound, "资产不存在或无权删除", "asset not found or no permission to delete")
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}
