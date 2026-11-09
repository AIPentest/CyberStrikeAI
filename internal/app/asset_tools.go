package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"cyberstrike-ai/internal/authctx"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/mcp/builtin"

	"go.uber.org/zap"
)

const agentAssetPageSizeMax = 50

func registerAssetTools(server *mcp.Server, db *database.DB, logger *zap.Logger) {
	if server == nil || db == nil {
		return
	}
	properties := assetMutationProperties()

	server.RegisterTool(mcp.Tool{
		Name: builtin.ToolCreateAsset, ShortDescription: "Create a new asset, deduplicating and updating existing ones",
		Description: "Add an asset to the asset library. Deduplicate by target+port+protocol; if the asset already exists, update non-empty fields. Provide at least one of host, ip, domain.",
		// Bedrock rejects tool schemas with top-level oneOf/allOf/anyOf. The
		// host/ip/domain requirement is enforced by assetFromCreateArgs below.
		InputSchema: map[string]interface{}{"type": "object", "properties": properties},
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		asset, err := assetFromCreateArgs(args)
		if err != nil {
			return textResult("Error: "+err.Error(), true), nil
		}
		access, owner, global := assetAccessFromToolContext(ctx, "asset:write")
		result, err := db.UpsertAssets([]*database.Asset{asset}, owner, global)
		if err != nil {
			logger.Error("Failed to save the asset for the agent", zap.Error(err))
			return textResult("Error: "+err.Error(), true), nil
		}
		if result.Skipped > 0 || asset.ID == "" {
			return textResult("Asset not saved: the same asset already exists but the current user has no permission to update it, or the target fields are empty", true), nil
		}
		saved, err := db.GetAsset(asset.ID, access)
		if err != nil {
			return textResult("Asset saved, but unable to read the result: "+err.Error(), true), nil
		}
		action := "created"
		if result.Updated > 0 {
			action = "updated"
		}
		return assetJSONResult(map[string]interface{}{"action": action, "asset": assetToolDetail(saved)})
	})

	server.RegisterTool(mcp.Tool{
		Name: builtin.ToolGetAsset, ShortDescription: "View asset details by ID", Description: "Return full asset details by asset ID. When querying lists, use query_assets first to avoid fetching too many details at once.",
		InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"id": map[string]interface{}{"type": "string", "description": "Asset ID"}}, "required": []string{"id"}},
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		projectID, projectScoped, err := agentAssetProjectScope(db, ctx)
		if err != nil {
			return textResult("Error: "+err.Error(), true), nil
		}
		asset, err := db.GetAsset(strings.TrimSpace(strArg(args, "id")), assetAccessOnly(ctx, "asset:read"))
		if err != nil {
			if err == sql.ErrNoRows {
				return textResult("Error: asset does not exist or you are not authorized to view it", true), nil
			}
			return textResult("Error: "+err.Error(), true), nil
		}
		if projectScoped && strings.TrimSpace(asset.ProjectID) != projectID {
			return textResult("Error: asset does not exist or does not belong to the project bound to the current conversation", true), nil
		}
		return assetJSONResult(assetToolDetail(asset))
	})

	server.RegisterTool(mcp.Tool{
		Name: builtin.ToolQueryAssets, ShortDescription: "Flexible paginated asset query",
		Description: "Paginated asset query. Supports exact field matches, time ranges, scan state, and whitelisted sorting. To find the least recently scanned assets, use sort_by=last_scan_at and sort_order=asc; never-scanned assets sort first. Defaults to 20 items per page, max 50, returning concise summaries; use get_asset for a single item's details.",
		InputSchema: assetQuerySchema(),
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		filter, page, pageSize, err := assetFilterFromToolArgs(args)
		if err != nil {
			return textResult("Error: "+err.Error(), true), nil
		}
		projectID, projectScoped, err := agentAssetProjectScope(db, ctx)
		if err != nil {
			return textResult("Error: "+err.Error(), true), nil
		}
		if projectScoped {
			// 对话绑定项目后，项目范围是服务端强制边界；不能通过工具参数扩大或切换范围。
			filter.ProjectID = projectID
		}
		items, total, err := db.ListAssets(pageSize, (page-1)*pageSize, filter, assetAccessOnly(ctx, "asset:read"))
		if err != nil {
			return textResult("Error: "+err.Error(), true), nil
		}
		totalPages := (total + pageSize - 1) / pageSize
		if totalPages < 1 {
			totalPages = 1
		}
		var b strings.Builder
		b.WriteString(fmt.Sprintf("Asset query: page %d/%d, %d items on this page, %d total, page_size=%d\n", page, totalPages, len(items), total, pageSize))
		for _, asset := range items {
			b.WriteString(formatAssetListItem(asset))
			b.WriteByte('\n')
		}
		if page < totalPages {
			b.WriteString(fmt.Sprintf("Next page: keep the same filters and set page=%d.", page+1))
		}
		return textResult(b.String(), false), nil
	})

	updateProperties := assetMutationProperties()
	updateProperties["id"] = map[string]interface{}{"type": "string", "description": "Asset ID"}
	server.RegisterTool(mcp.Tool{
		Name: builtin.ToolUpdateAsset, ShortDescription: "Partially update an asset",
		Description: "Partially update an asset by ID, modifying only explicitly passed fields; passing an empty project_id clears the project binding, and passing empty tags clears the tags.",
		InputSchema: map[string]interface{}{"type": "object", "properties": updateProperties, "required": []string{"id"}},
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		id := strings.TrimSpace(strArg(args, "id"))
		access := assetAccessOnly(ctx, "asset:write")
		asset, err := db.GetAsset(id, access)
		if err != nil {
			return textResult("Error: asset does not exist or you are not authorized to update it", true), nil
		}
		if err := applyAssetPatch(asset, args); err != nil {
			return textResult("Error: "+err.Error(), true), nil
		}
		if err := db.UpdateAsset(id, asset, access); err != nil {
			return textResult("Error: "+err.Error(), true), nil
		}
		updated, err := db.GetAsset(id, access)
		if err != nil {
			return textResult("Asset updated, but unable to read the result: "+err.Error(), true), nil
		}
		return assetJSONResult(map[string]interface{}{"action": "updated", "asset": assetToolDetail(updated)})
	})

	server.RegisterTool(mcp.Tool{
		Name: builtin.ToolDeleteAsset, ShortDescription: "Delete an asset", Description: "Permanently delete an asset record by ID. Only call when the user explicitly asks for deletion.",
		InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"id": map[string]interface{}{"type": "string", "description": "Asset ID"}}, "required": []string{"id"}},
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		id := strings.TrimSpace(strArg(args, "id"))
		if err := db.DeleteAsset(id, assetAccessOnly(ctx, "asset:delete")); err != nil {
			return textResult("Error: asset does not exist or you are not authorized to delete it", true), nil
		}
		return textResult("Asset deleted: "+id, false), nil
	})

	server.RegisterTool(mcp.Tool{
		Name:             builtin.ToolCompleteAssetScan,
		ShortDescription: "Mark an asset scan complete and write back its results",
		Description:      "Call after the target scan finishes: update the asset's last scan time to the current time and link the current conversation. The related vulnerability count is not entered by hand; it is automatically counted from the vulnerabilities saved via record_vulnerability in the current scan conversation. Call after all vulnerabilities have been recorded; one scan conversation should ideally map to only one asset.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id": map[string]interface{}{"type": "string", "description": "ID of the asset whose scan is complete"},
			},
			"required": []string{"id"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		id := strings.TrimSpace(strArg(args, "id"))
		conversationID := conversationIDFromToolCtx(ctx)
		if conversationID == "" {
			return textResult("Error: unable to determine the current scan conversation", true), nil
		}
		access := assetAccessOnly(ctx, "asset:write")
		if err := db.CompleteAssetScan(id, conversationID, access); err != nil {
			if err == sql.ErrNoRows {
				return textResult("Error: asset does not exist or you are not authorized to write back scan results", true), nil
			}
			return textResult("Error: "+err.Error(), true), nil
		}
		updated, err := db.GetAsset(id, access)
		if err != nil {
			return textResult("Scan results written back, but unable to read the asset: "+err.Error(), true), nil
		}
		return assetJSONResult(map[string]interface{}{
			"action":  "scan_completed",
			"message": "Last scan time updated; the related vulnerability count is computed automatically from the vulnerabilities saved in the current scan conversation",
			"asset":   assetToolDetail(updated),
		})
	})
}

func assetMutationProperties() map[string]interface{} {
	return map[string]interface{}{
		"project_id": map[string]interface{}{"type": "string"}, "host": map[string]interface{}{"type": "string"},
		"ip": map[string]interface{}{"type": "string"}, "port": map[string]interface{}{"type": "integer", "minimum": 0, "maximum": 65535},
		"domain": map[string]interface{}{"type": "string"}, "protocol": map[string]interface{}{"type": "string"},
		"title": map[string]interface{}{"type": "string"}, "server": map[string]interface{}{"type": "string"},
		"country": map[string]interface{}{"type": "string"}, "province": map[string]interface{}{"type": "string"}, "city": map[string]interface{}{"type": "string"},
		"responsible_person": map[string]interface{}{"type": "string"}, "department": map[string]interface{}{"type": "string"},
		"business_system": map[string]interface{}{"type": "string"},
		"environment":     map[string]interface{}{"type": "string", "enum": []string{"production", "staging", "testing", "development", "other"}},
		"criticality":     map[string]interface{}{"type": "string", "enum": []string{"critical", "high", "medium", "low"}},
		"source":          map[string]interface{}{"type": "string"}, "source_query": map[string]interface{}{"type": "string"},
		"status": map[string]interface{}{"type": "string", "enum": []string{"active", "inactive"}},
		"tags":   map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "maxItems": 50},
	}
}

func assetQuerySchema() map[string]interface{} {
	properties := map[string]interface{}{
		"q":          map[string]interface{}{"type": "string", "description": "Fuzzy search across host, IP, domain, title, server, and tags"},
		"project_id": map[string]interface{}{"type": "string"}, "status": map[string]interface{}{"type": "string", "enum": []string{"active", "inactive"}},
		"protocol": map[string]interface{}{"type": "string"}, "source": map[string]interface{}{"type": "string"}, "tag": map[string]interface{}{"type": "string"},
		"host": map[string]interface{}{"type": "string"}, "ip": map[string]interface{}{"type": "string"}, "domain": map[string]interface{}{"type": "string"},
		"port":                map[string]interface{}{"type": "integer", "minimum": 0, "maximum": 65535},
		"risk_level":          map[string]interface{}{"type": "string", "enum": []string{"unassessed", "critical", "high", "medium", "low", "info", "normal"}},
		"min_vulnerabilities": map[string]interface{}{"type": "integer", "minimum": 0},
		"max_vulnerabilities": map[string]interface{}{"type": "integer", "minimum": 0},
		"country":             map[string]interface{}{"type": "string"}, "province": map[string]interface{}{"type": "string"}, "city": map[string]interface{}{"type": "string"},
		"responsible_person": map[string]interface{}{"type": "string"}, "department": map[string]interface{}{"type": "string"},
		"business_system": map[string]interface{}{"type": "string"}, "environment": map[string]interface{}{"type": "string"}, "criticality": map[string]interface{}{"type": "string"},
		"scan_state":        map[string]interface{}{"type": "string", "enum": []string{"never", "scanned"}, "description": "never=never scanned, scanned=has been scanned"},
		"scan_overdue_days": map[string]interface{}{"type": "integer", "minimum": 1},
		"last_scan_before":  map[string]interface{}{"type": "string", "description": "RFC3339 time or YYYY-MM-DD"},
		"last_scan_after":   map[string]interface{}{"type": "string", "description": "RFC3339 time or YYYY-MM-DD"},
		"first_seen_before": map[string]interface{}{"type": "string", "description": "RFC3339 time or YYYY-MM-DD"},
		"first_seen_after":  map[string]interface{}{"type": "string", "description": "RFC3339 time or YYYY-MM-DD"},
		"last_seen_before":  map[string]interface{}{"type": "string", "description": "RFC3339 time or YYYY-MM-DD"},
		"last_seen_after":   map[string]interface{}{"type": "string", "description": "RFC3339 time or YYYY-MM-DD"},
		"sort_by":           map[string]interface{}{"type": "string", "enum": []string{"last_seen_at", "last_scan_at", "first_seen_at", "created_at", "updated_at", "host", "port", "risk_level", "vulnerability_count"}},
		"sort_order":        map[string]interface{}{"type": "string", "enum": []string{"asc", "desc"}},
		"page":              map[string]interface{}{"type": "integer", "minimum": 1},
		"page_size":         map[string]interface{}{"type": "integer", "minimum": 1, "maximum": agentAssetPageSizeMax},
	}
	return map[string]interface{}{"type": "object", "properties": properties}
}

func assetFromCreateArgs(args map[string]interface{}) (*database.Asset, error) {
	asset := &database.Asset{}
	if err := applyAssetPatch(asset, args); err != nil {
		return nil, err
	}
	if strings.TrimSpace(asset.Host) == "" && strings.TrimSpace(asset.IP) == "" && strings.TrimSpace(asset.Domain) == "" {
		return nil, fmt.Errorf("at least one of host, ip, domain is required")
	}
	return asset, nil
}

func applyAssetPatch(asset *database.Asset, args map[string]interface{}) error {
	setString := func(key string, dst *string) {
		if _, ok := args[key]; ok {
			*dst = strings.TrimSpace(strArg(args, key))
		}
	}
	setString("project_id", &asset.ProjectID)
	setString("host", &asset.Host)
	setString("ip", &asset.IP)
	setString("domain", &asset.Domain)
	setString("protocol", &asset.Protocol)
	setString("title", &asset.Title)
	setString("server", &asset.Server)
	setString("country", &asset.Country)
	setString("province", &asset.Province)
	setString("city", &asset.City)
	setString("responsible_person", &asset.ResponsiblePerson)
	setString("department", &asset.Department)
	setString("business_system", &asset.BusinessSystem)
	setString("environment", &asset.Environment)
	setString("criticality", &asset.Criticality)
	setString("source", &asset.Source)
	setString("source_query", &asset.SourceQuery)
	setString("status", &asset.Status)
	if _, ok := args["port"]; ok {
		port := intArg(args, "port", -1)
		if port < 0 || port > 65535 {
			return fmt.Errorf("port must be between 0 and 65535")
		}
		asset.Port = port
	}
	if raw, ok := args["tags"]; ok {
		tags, err := stringSliceArg(raw)
		if err != nil {
			return fmt.Errorf("tags: %w", err)
		}
		asset.Tags = tags
	}
	return nil
}

func assetFilterFromToolArgs(args map[string]interface{}) (database.AssetListFilter, int, int, error) {
	filter := database.AssetListFilter{
		Search: strings.TrimSpace(strArg(args, "q")), ProjectID: strings.TrimSpace(strArg(args, "project_id")), Status: strings.ToLower(strings.TrimSpace(strArg(args, "status"))),
		Protocol: strings.ToLower(strings.TrimSpace(strArg(args, "protocol"))), Source: strings.TrimSpace(strArg(args, "source")), Tag: strings.TrimSpace(strArg(args, "tag")),
		Host: strings.TrimSpace(strArg(args, "host")), IP: strings.TrimSpace(strArg(args, "ip")), Domain: strings.TrimSpace(strArg(args, "domain")),
		ScanState: strings.ToLower(strings.TrimSpace(strArg(args, "scan_state"))), SortBy: strings.ToLower(strings.TrimSpace(strArg(args, "sort_by"))),
		SortOrder: strings.ToLower(strings.TrimSpace(strArg(args, "sort_order"))),
		RiskLevel: strings.ToLower(strings.TrimSpace(strArg(args, "risk_level"))),
		Country:   strings.TrimSpace(strArg(args, "country")), Province: strings.TrimSpace(strArg(args, "province")), City: strings.TrimSpace(strArg(args, "city")),
		ResponsiblePerson: strings.TrimSpace(strArg(args, "responsible_person")), Department: strings.TrimSpace(strArg(args, "department")),
		BusinessSystem: strings.TrimSpace(strArg(args, "business_system")), Environment: strings.ToLower(strings.TrimSpace(strArg(args, "environment"))),
		Criticality: strings.ToLower(strings.TrimSpace(strArg(args, "criticality"))),
	}
	if !oneOfOrEmpty(filter.Status, "active", "inactive") {
		return filter, 0, 0, fmt.Errorf("status only supports active or inactive")
	}
	if !oneOfOrEmpty(filter.ScanState, "never", "scanned") {
		return filter, 0, 0, fmt.Errorf("scan_state only supports never or scanned")
	}
	if !oneOfOrEmpty(filter.SortBy, "last_seen_at", "last_scan_at", "first_seen_at", "created_at", "updated_at", "host", "port", "risk_level", "vulnerability_count") {
		return filter, 0, 0, fmt.Errorf("sort_by is not supported")
	}
	if !oneOfOrEmpty(filter.SortOrder, "asc", "desc") {
		return filter, 0, 0, fmt.Errorf("sort_order only supports asc or desc")
	}
	if _, ok := args["port"]; ok {
		port := intArg(args, "port", -1)
		if port < 0 || port > 65535 {
			return filter, 0, 0, fmt.Errorf("port must be between 0 and 65535")
		}
		filter.Port = &port
	}
	if _, ok := args["min_vulnerabilities"]; ok {
		value := intArg(args, "min_vulnerabilities", -1)
		if value < 0 {
			return filter, 0, 0, fmt.Errorf("min_vulnerabilities cannot be less than 0")
		}
		filter.MinVulnerabilities = &value
	}
	if _, ok := args["max_vulnerabilities"]; ok {
		value := intArg(args, "max_vulnerabilities", -1)
		if value < 0 {
			return filter, 0, 0, fmt.Errorf("max_vulnerabilities cannot be less than 0")
		}
		filter.MaxVulnerabilities = &value
	}
	if _, ok := args["scan_overdue_days"]; ok {
		value := intArg(args, "scan_overdue_days", 0)
		if value < 1 {
			return filter, 0, 0, fmt.Errorf("scan_overdue_days must be greater than 0")
		}
		filter.ScanOverdueDays = &value
	}
	var err error
	if filter.LastScanBefore, err = parseAssetToolTime("last_scan_before", strArg(args, "last_scan_before")); err != nil {
		return filter, 0, 0, err
	}
	if filter.LastScanAfter, err = parseAssetToolTime("last_scan_after", strArg(args, "last_scan_after")); err != nil {
		return filter, 0, 0, err
	}
	if filter.FirstSeenBefore, err = parseAssetToolTime("first_seen_before", strArg(args, "first_seen_before")); err != nil {
		return filter, 0, 0, err
	}
	if filter.FirstSeenAfter, err = parseAssetToolTime("first_seen_after", strArg(args, "first_seen_after")); err != nil {
		return filter, 0, 0, err
	}
	if filter.LastSeenBefore, err = parseAssetToolTime("last_seen_before", strArg(args, "last_seen_before")); err != nil {
		return filter, 0, 0, err
	}
	if filter.LastSeenAfter, err = parseAssetToolTime("last_seen_after", strArg(args, "last_seen_after")); err != nil {
		return filter, 0, 0, err
	}
	page := intArg(args, "page", 1)
	pageSize := intArg(args, "page_size", 20)
	if page < 1 || page > 1_000_000 {
		return filter, 0, 0, fmt.Errorf("page must be between 1 and 1000000")
	}
	if pageSize < 1 || pageSize > agentAssetPageSizeMax {
		return filter, 0, 0, fmt.Errorf("page_size must be between 1 and %d", agentAssetPageSizeMax)
	}
	return filter, page, pageSize, nil
}

func oneOfOrEmpty(value string, allowed ...string) bool {
	if value == "" {
		return true
	}
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func parseAssetToolTime(field, value string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return &parsed, nil
		}
	}
	return nil, fmt.Errorf("%s must be an RFC3339 time or YYYY-MM-DD", field)
}

func stringSliceArg(raw interface{}) ([]string, error) {
	values := []string{}
	switch typed := raw.(type) {
	case []string:
		values = append(values, typed...)
	case []interface{}:
		for _, item := range typed {
			value, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("must be an array of strings")
			}
			values = append(values, value)
		}
	default:
		return nil, fmt.Errorf("must be an array of strings")
	}
	if len(values) > 50 {
		return nil, fmt.Errorf("at most 50 tags")
	}
	return values, nil
}

func assetAccessOnly(ctx context.Context, permission string) database.RBACListAccess {
	principal, ok := authctx.PrincipalFromContext(ctx)
	if !ok {
		return database.RBACListAccess{}
	}
	return database.RBACListAccess{UserID: principal.UserID, Scope: principal.ScopeFor(permission)}
}

func assetAccessFromToolContext(ctx context.Context, permission string) (database.RBACListAccess, string, bool) {
	principal, ok := authctx.PrincipalFromContext(ctx)
	if !ok {
		return database.RBACListAccess{}, "", false
	}
	access := database.RBACListAccess{UserID: principal.UserID, Scope: principal.ScopeFor(permission)}
	return access, principal.UserID, access.Scope == database.RBACScopeAll
}

// agentAssetProjectScope returns the hard asset-read boundary implied by the
// current conversation. An unbound conversation (or a tool call outside a
// conversation) keeps the existing all-accessible-assets behavior. A bound
// conversation can only read assets assigned to that exact project.
func agentAssetProjectScope(db *database.DB, ctx context.Context) (projectID string, scoped bool, err error) {
	conversationID := conversationIDFromToolCtx(ctx)
	if conversationID == "" {
		return "", false, nil
	}
	projectID, err = db.GetConversationProjectID(conversationID)
	if err != nil {
		return "", false, fmt.Errorf("unable to determine the project scope of the current conversation")
	}
	projectID = strings.TrimSpace(projectID)
	return projectID, projectID != "", nil
}

func formatAssetListItem(asset *database.Asset) string {
	target := asset.Domain
	if target == "" {
		target = asset.IP
	}
	if target == "" {
		target = asset.Host
	}
	if asset.Port > 0 {
		target = fmt.Sprintf("%s:%d", target, asset.Port)
	}
	lastScan := "never"
	if asset.LastScanAt != nil {
		lastScan = asset.LastScanAt.Format(time.RFC3339)
	}
	return fmt.Sprintf("- id=%s | target=%s | protocol=%s | status=%s | last_scan_at=%s | risk=%s | vulnerabilities=%d", asset.ID, truncateRunes(target, 120), truncateRunes(asset.Protocol, 30), truncateRunes(asset.Status, 30), lastScan, asset.RiskLevel, asset.VulnerabilityCount)
}

// assetToolDetail keeps even a single unusually large imported record from
// consuming the model context. The database and HTTP API retain full values.
func assetToolDetail(asset *database.Asset) map[string]interface{} {
	if asset == nil {
		return nil
	}
	tags := make([]string, 0, len(asset.Tags))
	for i, tag := range asset.Tags {
		if i >= 50 {
			break
		}
		tags = append(tags, truncateRunes(tag, 100))
	}
	detail := map[string]interface{}{
		"id": asset.ID, "project_id": asset.ProjectID, "project_name": truncateRunes(asset.ProjectName, 200),
		"host": truncateRunes(asset.Host, 500), "ip": truncateRunes(asset.IP, 100), "port": asset.Port,
		"domain": truncateRunes(asset.Domain, 255), "protocol": truncateRunes(asset.Protocol, 50),
		"title": truncateRunes(asset.Title, 500), "server": truncateRunes(asset.Server, 500),
		"country": truncateRunes(asset.Country, 100), "province": truncateRunes(asset.Province, 100), "city": truncateRunes(asset.City, 100),
		"responsible_person": truncateRunes(asset.ResponsiblePerson, 255), "department": truncateRunes(asset.Department, 255),
		"business_system": truncateRunes(asset.BusinessSystem, 255), "environment": asset.Environment, "criticality": asset.Criticality,
		"source": truncateRunes(asset.Source, 100), "source_query": truncateRunes(asset.SourceQuery, 2000),
		"status": truncateRunes(asset.Status, 50), "tags": tags,
		"first_seen_at": asset.FirstSeenAt, "last_seen_at": asset.LastSeenAt, "created_at": asset.CreatedAt, "updated_at": asset.UpdatedAt,
		"last_scan_conversation_id": asset.LastScanConversationID, "last_scan_queue_id": asset.LastScanQueueID, "last_scan_task_id": asset.LastScanTaskID,
		"vulnerability_count": asset.VulnerabilityCount, "risk_level": asset.RiskLevel,
	}
	if asset.LastScanAt != nil {
		detail["last_scan_at"] = asset.LastScanAt
	}
	if len(asset.Tags) > len(tags) {
		detail["tags_truncated"] = true
	}
	return detail
}

func assetJSONResult(value interface{}) (*mcp.ToolResult, error) {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return textResult("Error: "+err.Error(), true), nil
	}
	return textResult(string(encoded), false), nil
}
