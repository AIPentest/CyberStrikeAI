package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/net/idna"
)

// Asset is a persistent, deduplicated target discovered manually or by recon providers.
type Asset struct {
	ID                     string     `json:"id"`
	ProjectID              string     `json:"project_id,omitempty"`
	ProjectName            string     `json:"project_name,omitempty"`
	Host                   string     `json:"host"`
	IP                     string     `json:"ip"`
	Port                   int        `json:"port"`
	Domain                 string     `json:"domain"`
	Protocol               string     `json:"protocol"`
	Title                  string     `json:"title"`
	Server                 string     `json:"server"`
	Country                string     `json:"country"`
	Province               string     `json:"province"`
	City                   string     `json:"city"`
	ResponsiblePerson      string     `json:"responsible_person"`
	Department             string     `json:"department"`
	BusinessSystem         string     `json:"business_system"`
	Environment            string     `json:"environment"`
	Criticality            string     `json:"criticality"`
	Source                 string     `json:"source"`
	SourceQuery            string     `json:"source_query"`
	Status                 string     `json:"status"`
	Tags                   []string   `json:"tags"`
	FirstSeenAt            time.Time  `json:"first_seen_at"`
	LastSeenAt             time.Time  `json:"last_seen_at"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
	LastScanAt             *time.Time `json:"last_scan_at,omitempty"`
	LastScanConversationID string     `json:"last_scan_conversation_id,omitempty"`
	LastScanQueueID        string     `json:"last_scan_queue_id,omitempty"`
	LastScanTaskID         string     `json:"last_scan_task_id,omitempty"`
	VulnerabilityCount     int        `json:"vulnerability_count"`
	RiskLevel              string     `json:"risk_level"`
	RiskScore              int        `json:"-"`
	OwnerUserID            string     `json:"-"`
}

type AssetListFilter struct {
	Search             string
	Status             string
	Protocol           string
	ProjectID          string
	Source             string
	Tag                string
	Host               string
	IP                 string
	Domain             string
	Port               *int
	RiskLevel          string
	MinVulnerabilities *int
	MaxVulnerabilities *int
	Country            string
	Province           string
	City               string
	ResponsiblePerson  string
	Department         string
	BusinessSystem     string
	Environment        string
	Criticality        string
	ScanState          string
	ScanOverdueDays    *int
	LastScanBefore     *time.Time
	LastScanAfter      *time.Time
	FirstSeenBefore    *time.Time
	FirstSeenAfter     *time.Time
	LastSeenBefore     *time.Time
	LastSeenAfter      *time.Time
	SortBy             string
	SortOrder          string
}

type AssetImportResult struct {
	Created int `json:"created"`
	Updated int `json:"updated"`
	Skipped int `json:"skipped"`
}

func normalizeAsset(a *Asset) {
	a.Host = strings.TrimSpace(a.Host)
	a.IP = strings.ToLower(strings.TrimSpace(a.IP))
	a.Domain = strings.ToLower(strings.TrimSpace(a.Domain))
	a.Protocol = strings.ToLower(strings.TrimSpace(a.Protocol))
	a.Title = strings.TrimSpace(a.Title)
	a.Server = strings.TrimSpace(a.Server)
	a.Country = strings.TrimSpace(a.Country)
	a.Province = strings.TrimSpace(a.Province)
	a.City = strings.TrimSpace(a.City)
	a.ResponsiblePerson = strings.TrimSpace(a.ResponsiblePerson)
	a.Department = strings.TrimSpace(a.Department)
	a.BusinessSystem = strings.TrimSpace(a.BusinessSystem)
	a.Environment = strings.ToLower(strings.TrimSpace(a.Environment))
	a.Criticality = strings.ToLower(strings.TrimSpace(a.Criticality))
	a.Source = strings.TrimSpace(a.Source)
	a.SourceQuery = strings.TrimSpace(a.SourceQuery)
	a.ProjectID = strings.TrimSpace(a.ProjectID)
	a.Status = strings.ToLower(strings.TrimSpace(a.Status))
	if a.Status == "" {
		a.Status = "active"
	}
	if a.Source == "" {
		a.Source = "manual"
	}
	seen := map[string]bool{}
	tags := make([]string, 0, len(a.Tags))
	for _, tag := range a.Tags {
		tag = strings.TrimSpace(tag)
		if tag != "" && !seen[tag] {
			seen[tag] = true
			tags = append(tags, tag)
		}
	}
	a.Tags = tags
	// URL 型 Host 是常见输入。缺失的结构化字段在服务端同样补齐，确保
	// API、MCP 与 Web 端产生一致的去重键，而不依赖某个客户端正确解析。
	if strings.Contains(a.Host, "://") {
		if parsed, err := url.Parse(a.Host); err == nil && parsed.Hostname() != "" && parsed.User == nil {
			hostname := strings.Trim(strings.ToLower(parsed.Hostname()), "[]")
			if net.ParseIP(hostname) != nil && a.IP == "" {
				a.IP = hostname
			} else if a.Domain == "" {
				if ascii, err := idna.Lookup.ToASCII(hostname); err == nil {
					a.Domain = strings.ToLower(ascii)
				}
			}
			if a.Protocol == "" {
				a.Protocol = strings.ToLower(parsed.Scheme)
			}
			if a.Port == 0 {
				if parsed.Port() != "" {
					a.Port, _ = strconv.Atoi(parsed.Port())
				} else if a.Protocol == "https" {
					a.Port = 443
				} else if a.Protocol == "http" {
					a.Port = 80
				}
			}
		}
	}
	// Recon providers occasionally return placeholders, multiple values, or
	// provider-specific identifiers in structured fields. They are optional
	// enrichment; a valid Host must not make the entire batch fail because one
	// of those fields is dirty.
	if strings.EqualFold(a.Source, "fofa") {
		if a.IP != "" && net.ParseIP(strings.Trim(a.IP, "[]")) == nil {
			a.IP = ""
		}
		if a.Domain != "" {
			ascii, err := idna.Lookup.ToASCII(strings.TrimSuffix(a.Domain, "."))
			if err != nil || !validAssetDomain(ascii) {
				a.Domain = ""
			} else {
				a.Domain = strings.ToLower(ascii)
			}
		}
		if a.Protocol != "" && !assetProtocolPattern.MatchString(a.Protocol) {
			a.Protocol = ""
		}
	}
}

var assetProtocolPattern = regexp.MustCompile(`^[a-z][a-z0-9+.-]{0,31}$`)

// AssetValidationError distinguishes user-correctable asset data from storage failures.
type AssetValidationError struct{ Message string }

func (e *AssetValidationError) Error() string { return e.Message }

func assetValidationErrorf(format string, args ...interface{}) error {
	return &AssetValidationError{Message: fmt.Sprintf(format, args...)}
}

func validateAsset(a *Asset) error {
	if a == nil {
		return assetValidationErrorf("资产不能为空")
	}
	if a.Host == "" && a.IP == "" && a.Domain == "" {
		return assetValidationErrorf("资产目标不能为空")
	}
	if a.Port < 0 || a.Port > 65535 {
		return assetValidationErrorf("端口必须在 0-65535 之间")
	}
	if a.IP != "" && net.ParseIP(strings.Trim(a.IP, "[]")) == nil {
		return assetValidationErrorf("IP 地址格式无效")
	}
	if a.Domain != "" {
		ascii, err := idna.Lookup.ToASCII(strings.TrimSuffix(a.Domain, "."))
		if err != nil || !validAssetDomain(ascii) {
			return assetValidationErrorf("域名格式无效")
		}
		a.Domain = strings.ToLower(ascii)
	}
	if a.Protocol != "" && !assetProtocolPattern.MatchString(a.Protocol) {
		return assetValidationErrorf("协议格式无效")
	}
	if a.Status != "active" && a.Status != "inactive" {
		return assetValidationErrorf("资产状态必须为 active 或 inactive")
	}
	for name, value := range map[string]string{
		"Host": a.Host, "域名": a.Domain, "协议": a.Protocol, "页面标题": a.Title,
		"服务指纹": a.Server, "国家/地区": a.Country, "省份/州": a.Province, "城市": a.City,
		"负责人": a.ResponsiblePerson, "部门": a.Department, "业务系统": a.BusinessSystem,
	} {
		limit := 255
		if name == "Host" || name == "页面标题" {
			limit = 500
		}
		if utf8.RuneCountInString(value) > limit {
			return assetValidationErrorf("%s不能超过 %d 个字符", name, limit)
		}
	}
	if !oneOfAssetValue(a.Environment, "", "production", "staging", "testing", "development", "other") {
		return assetValidationErrorf("环境必须为 production、staging、testing、development 或 other")
	}
	if !oneOfAssetValue(a.Criticality, "", "critical", "high", "medium", "low") {
		return assetValidationErrorf("重要性必须为 critical、high、medium 或 low")
	}
	if len(a.Tags) > 30 {
		return assetValidationErrorf("标签不能超过 30 个")
	}
	for _, tag := range a.Tags {
		if utf8.RuneCountInString(tag) > 64 {
			return assetValidationErrorf("单个标签不能超过 64 个字符")
		}
	}
	return nil
}

func oneOfAssetValue(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func validAssetDomain(domain string) bool {
	domain = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
	if domain == "" || len(domain) > 253 || net.ParseIP(domain) != nil {
		return false
	}
	for _, label := range strings.Split(domain, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
				return false
			}
		}
	}
	return true
}

// assetDedupKey derives asset identity from ip+domain combined: assets with
// different IPs under the same domain (e.g. reconnaissance exports where the
// domain column holds the registrable root domain and host holds subdomains)
// must not be merged. Host only participates as a fallback (pure-URL assets)
// when both ip and domain are missing, keeping free-form host text out of the
// key to avoid format sensitivity.
func assetDedupKey(a *Asset) string {
	host := ""
	if a.IP == "" && a.Domain == "" {
		host = strings.ToLower(a.Host)
	}
	return strings.Join([]string{host, a.IP, a.Domain, strconv.Itoa(a.Port), a.Protocol}, "|")
}

// translateAssetDedupConflict converts a dedup_key UNIQUE constraint conflict
// into a readable validation error. Under the ip+domain combined identity,
// editing an asset's identifiers to exactly match an existing asset triggers it.
func translateAssetDedupConflict(err error) error {
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "unique constraint failed: assets.dedup_key") {
		return assetValidationErrorf("目标标识与已有资产冲突，请先合并重复资产或修改 IP/域名/端口/协议")
	}
	return err
}

func appendAssetAccess(query string, args []interface{}, access RBACListAccess, alias string) (string, []interface{}) {
	if strings.TrimSpace(access.UserID) == "" || access.Scope == RBACScopeAll {
		return query, args
	}
	prefix := ""
	if alias != "" {
		prefix = alias + "."
	}
	query += ` AND (` + prefix + `owner_user_id = ? OR EXISTS (
		SELECT 1 FROM rbac_resource_assignments ra
		WHERE ra.user_id = ? AND ra.resource_type = 'asset' AND ra.resource_id = ` + prefix + `id
	) OR (` + prefix + `project_id IS NOT NULL AND ` + prefix + `project_id <> '' AND (
		EXISTS (SELECT 1 FROM projects ap WHERE ap.id=` + prefix + `project_id AND ap.owner_user_id=?)
		OR EXISTS (SELECT 1 FROM rbac_resource_assignments pra WHERE pra.user_id=? AND pra.resource_type='project' AND pra.resource_id=` + prefix + `project_id)
	)))`
	return query, append(args, access.UserID, access.UserID, access.UserID, access.UserID)
}

func (db *DB) UpsertAssets(assets []*Asset, ownerUserID string, allowGlobal ...bool) (AssetImportResult, error) {
	result := AssetImportResult{}
	tx, err := db.Begin()
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	now := time.Now()
	for _, asset := range assets {
		if asset == nil {
			result.Skipped++
			continue
		}
		normalizeAsset(asset)
		if err := validateAsset(asset); err != nil {
			return result, fmt.Errorf("第 %d 个资产无效: %w", result.Created+result.Updated+result.Skipped+1, err)
		}
		key := assetDedupKey(asset)
		var existingID string
		var existingOwner sql.NullString
		err := tx.QueryRow(`SELECT id,owner_user_id FROM assets WHERE dedup_key = ?`, key).Scan(&existingID, &existingOwner)
		tagsJSON, _ := json.Marshal(asset.Tags)
		if err == sql.ErrNoRows {
			asset.ID = uuid.NewString()
			asset.FirstSeenAt, asset.LastSeenAt, asset.CreatedAt, asset.UpdatedAt = now, now, now, now
			_, err = tx.Exec(`INSERT INTO assets (
				id,dedup_key,project_id,host,ip,port,domain,protocol,title,server,country,province,city,source,source_query,status,tags_json,
				responsible_person,department,business_system,environment,criticality,
				first_seen_at,last_seen_at,created_at,updated_at,owner_user_id
			) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				asset.ID, key, nullIfEmpty(asset.ProjectID), asset.Host, asset.IP, asset.Port, asset.Domain, asset.Protocol, asset.Title, asset.Server,
				asset.Country, asset.Province, asset.City, asset.Source, asset.SourceQuery, asset.Status, string(tagsJSON),
				asset.ResponsiblePerson, asset.Department, asset.BusinessSystem, asset.Environment, asset.Criticality,
				now, now, now, now, nullIfEmpty(ownerUserID))
			if err != nil {
				return result, fmt.Errorf("创建资产失败: %w", err)
			}
			if ownerUserID != "" {
				if _, err := tx.Exec(`INSERT OR IGNORE INTO rbac_resource_assignments (id,user_id,resource_type,resource_id,created_at) SELECT ?,id,?,?,? FROM rbac_users WHERE id=?`, uuid.NewString(), "asset", asset.ID, now, ownerUserID); err != nil {
					return result, fmt.Errorf("授权新资产失败: %w", err)
				}
			}
			result.Created++
			continue
		}
		if err != nil {
			return result, fmt.Errorf("检查资产去重键失败: %w", err)
		}
		asset.ID = existingID
		global := len(allowGlobal) > 0 && allowGlobal[0]
		if !global && existingOwner.Valid && strings.TrimSpace(existingOwner.String) != "" && strings.TrimSpace(existingOwner.String) != strings.TrimSpace(ownerUserID) {
			result.Skipped++
			continue
		}
		_, err = tx.Exec(`UPDATE assets SET
			host=CASE WHEN ?<>'' THEN ? ELSE host END, ip=CASE WHEN ?<>'' THEN ? ELSE ip END,
			domain=CASE WHEN ?<>'' THEN ? ELSE domain END, protocol=CASE WHEN ?<>'' THEN ? ELSE protocol END,
			title=CASE WHEN ?<>'' THEN ? ELSE title END, server=CASE WHEN ?<>'' THEN ? ELSE server END,
			country=CASE WHEN ?<>'' THEN ? ELSE country END, province=CASE WHEN ?<>'' THEN ? ELSE province END,
			city=CASE WHEN ?<>'' THEN ? ELSE city END, source=CASE WHEN ?<>'' THEN ? ELSE source END,
			source_query=CASE WHEN ?<>'' THEN ? ELSE source_query END, project_id=CASE WHEN ?<>'' THEN ? ELSE project_id END,
			responsible_person=CASE WHEN ?<>'' THEN ? ELSE responsible_person END,
			department=CASE WHEN ?<>'' THEN ? ELSE department END,
			business_system=CASE WHEN ?<>'' THEN ? ELSE business_system END,
			environment=CASE WHEN ?<>'' THEN ? ELSE environment END,
			criticality=CASE WHEN ?<>'' THEN ? ELSE criticality END,
			tags_json=CASE WHEN ?<>'[]' THEN ? ELSE tags_json END,
			last_seen_at=?, updated_at=? WHERE id=?`,
			asset.Host, asset.Host, asset.IP, asset.IP, asset.Domain, asset.Domain, asset.Protocol, asset.Protocol,
			asset.Title, asset.Title, asset.Server, asset.Server, asset.Country, asset.Country, asset.Province, asset.Province,
			asset.City, asset.City, asset.Source, asset.Source, asset.SourceQuery, asset.SourceQuery, asset.ProjectID, nullIfEmpty(asset.ProjectID),
			asset.ResponsiblePerson, asset.ResponsiblePerson, asset.Department, asset.Department, asset.BusinessSystem, asset.BusinessSystem,
			asset.Environment, asset.Environment, asset.Criticality, asset.Criticality, string(tagsJSON), string(tagsJSON),
			now, now, existingID)
		if err != nil {
			return result, fmt.Errorf("更新资产失败: %w", err)
		}
		if ownerUserID != "" && (!existingOwner.Valid || strings.TrimSpace(existingOwner.String) == ownerUserID) {
			if _, err := tx.Exec(`INSERT OR IGNORE INTO rbac_resource_assignments (id,user_id,resource_type,resource_id,created_at) SELECT ?,id,?,?,? FROM rbac_users WHERE id=?`, uuid.NewString(), "asset", existingID, now, ownerUserID); err != nil {
				return result, fmt.Errorf("授权资产失败: %w", err)
			}
		}
		result.Updated++
	}
	if err := tx.Commit(); err != nil {
		return result, err
	}
	return result, nil
}

func assetWhere(filter AssetListFilter, access RBACListAccess) (string, []interface{}) {
	query := " WHERE 1=1"
	args := []interface{}{}
	if q := strings.TrimSpace(filter.Search); q != "" {
		pattern := "%" + escapeAssetLike(strings.ToLower(q)) + "%"
		query += ` AND (LOWER(assets.host) LIKE ? ESCAPE '\' OR LOWER(assets.ip) LIKE ? ESCAPE '\' OR LOWER(assets.domain) LIKE ? ESCAPE '\'
			OR LOWER(assets.title) LIKE ? ESCAPE '\' OR LOWER(assets.server) LIKE ? ESCAPE '\' OR LOWER(assets.tags_json) LIKE ? ESCAPE '\'
			OR LOWER(assets.responsible_person) LIKE ? ESCAPE '\' OR LOWER(assets.department) LIKE ? ESCAPE '\' OR LOWER(assets.business_system) LIKE ? ESCAPE '\')`
		for i := 0; i < 9; i++ {
			args = append(args, pattern)
		}
	}
	if filter.Status != "" {
		query += " AND assets.status = ?"
		args = append(args, filter.Status)
	}
	if filter.Protocol != "" {
		query += " AND assets.protocol = ?"
		args = append(args, filter.Protocol)
	}
	if filter.ProjectID != "" {
		query += " AND assets.project_id = ?"
		args = append(args, filter.ProjectID)
	}
	if filter.Source != "" {
		query += " AND LOWER(assets.source) = LOWER(?)"
		args = append(args, strings.TrimSpace(filter.Source))
	}
	if tag := strings.TrimSpace(filter.Tag); tag != "" {
		pattern := "%\"" + escapeAssetLike(strings.ToLower(tag)) + "\"%"
		query += ` AND LOWER(assets.tags_json) LIKE ? ESCAPE '\'`
		args = append(args, pattern)
	}
	if filter.Host != "" {
		query += " AND LOWER(assets.host) = LOWER(?)"
		args = append(args, strings.TrimSpace(filter.Host))
	}
	if filter.IP != "" {
		query += " AND LOWER(assets.ip) = LOWER(?)"
		args = append(args, strings.TrimSpace(filter.IP))
	}
	if filter.Domain != "" {
		query += " AND LOWER(assets.domain) = LOWER(?)"
		args = append(args, strings.TrimSpace(filter.Domain))
	}
	if filter.Port != nil {
		query += " AND assets.port = ?"
		args = append(args, *filter.Port)
	}
	if filter.RiskLevel != "" {
		query += " AND " + assetRiskLevelCachedExpr + " = ?"
		args = append(args, strings.ToLower(strings.TrimSpace(filter.RiskLevel)))
	}
	if filter.MinVulnerabilities != nil {
		query += " AND " + assetVulnerabilityCountCachedExpr + " >= ?"
		args = append(args, *filter.MinVulnerabilities)
	}
	if filter.MaxVulnerabilities != nil {
		query += " AND " + assetVulnerabilityCountCachedExpr + " <= ?"
		args = append(args, *filter.MaxVulnerabilities)
	}
	for _, item := range []struct {
		column string
		value  string
	}{
		{"assets.country", filter.Country}, {"assets.province", filter.Province}, {"assets.city", filter.City},
		{"assets.responsible_person", filter.ResponsiblePerson}, {"assets.department", filter.Department},
		{"assets.business_system", filter.BusinessSystem}, {"assets.environment", filter.Environment}, {"assets.criticality", filter.Criticality},
	} {
		if strings.TrimSpace(item.value) != "" {
			query += " AND LOWER(" + item.column + ") = LOWER(?)"
			args = append(args, strings.TrimSpace(item.value))
		}
	}
	switch strings.ToLower(strings.TrimSpace(filter.ScanState)) {
	case "never":
		query += " AND " + assetEffectiveLastScanExpr + " IS NULL"
	case "scanned":
		query += " AND " + assetEffectiveLastScanExpr + " IS NOT NULL"
	}
	if filter.ScanOverdueDays != nil {
		query += " AND (" + assetEffectiveLastScanExpr + " IS NULL OR datetime(" + assetEffectiveLastScanExpr + ") < datetime('now', ?))"
		args = append(args, fmt.Sprintf("-%d days", *filter.ScanOverdueDays))
	}
	if filter.LastScanBefore != nil {
		query += " AND " + assetEffectiveLastScanExpr + " < ?"
		args = append(args, *filter.LastScanBefore)
	}
	if filter.LastScanAfter != nil {
		query += " AND " + assetEffectiveLastScanExpr + " > ?"
		args = append(args, *filter.LastScanAfter)
	}
	if filter.FirstSeenBefore != nil {
		query += " AND assets.first_seen_at < ?"
		args = append(args, *filter.FirstSeenBefore)
	}
	if filter.FirstSeenAfter != nil {
		query += " AND assets.first_seen_at > ?"
		args = append(args, *filter.FirstSeenAfter)
	}
	if filter.LastSeenBefore != nil {
		query += " AND assets.last_seen_at < ?"
		args = append(args, *filter.LastSeenBefore)
	}
	if filter.LastSeenAfter != nil {
		query += " AND assets.last_seen_at > ?"
		args = append(args, *filter.LastSeenAfter)
	}
	return appendAssetAccess(query, args, access, "assets")
}

func escapeAssetLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	return strings.ReplaceAll(value, `_`, `\_`)
}

func scanAsset(scanner interface{ Scan(...interface{}) error }) (*Asset, error) {
	var a Asset
	var tags string
	var lastScanAt interface{}
	err := scanner.Scan(&a.ID, &a.ProjectID, &a.ProjectName, &a.Host, &a.IP, &a.Port, &a.Domain, &a.Protocol, &a.Title, &a.Server, &a.Country,
		&a.Province, &a.City, &a.ResponsiblePerson, &a.Department, &a.BusinessSystem, &a.Environment, &a.Criticality,
		&a.Source, &a.SourceQuery, &a.Status, &tags, &a.FirstSeenAt, &a.LastSeenAt, &a.CreatedAt, &a.UpdatedAt,
		&lastScanAt, &a.LastScanConversationID, &a.LastScanQueueID, &a.LastScanTaskID, &a.VulnerabilityCount, &a.RiskLevel)
	if err != nil {
		return nil, err
	}
	if parsed, ok := parseAssetScanTime(lastScanAt); ok {
		a.LastScanAt = &parsed
	}
	_ = json.Unmarshal([]byte(tags), &a.Tags)
	return &a, nil
}

func parseAssetScanTime(value interface{}) (time.Time, bool) {
	if value == nil {
		return time.Time{}, false
	}
	if parsed, ok := value.(time.Time); ok {
		return parsed, true
	}
	var raw string
	switch typed := value.(type) {
	case string:
		raw = typed
	case []byte:
		raw = string(typed)
	default:
		raw = fmt.Sprint(typed)
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999Z07:00",
		"2006-01-02 15:04:05-07:00",
		"2006-01-02 15:04:05",
	} {
		if parsed, err := time.Parse(layout, strings.TrimSpace(raw)); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

const assetEffectiveLastScanExpr = `COALESCE(
		(SELECT bt.completed_at FROM batch_tasks bt WHERE bt.id=assets.last_scan_task_id AND bt.completed_at IS NOT NULL LIMIT 1),
		(SELECT MAX(m.updated_at) FROM messages m WHERE m.conversation_id=assets.last_scan_conversation_id AND m.role='assistant'),
		assets.last_scan_at
	)`

const assetVulnerabilityMatchExpr = `(
	(COALESCE(assets.last_scan_conversation_id,'')<>'' AND v.conversation_id=assets.last_scan_conversation_id)
	OR (COALESCE(assets.last_scan_task_id,'')<>'' AND EXISTS (
		SELECT 1 FROM batch_tasks bt WHERE bt.id=assets.last_scan_task_id AND bt.conversation_id=v.conversation_id
	))
)`

const assetVulnerabilityCountExpr = `(SELECT COUNT(DISTINCT v.id) FROM vulnerabilities v WHERE ` + assetVulnerabilityMatchExpr + `)`

const assetRiskScoreQueryExpr = `COALESCE((
	SELECT MAX(CASE LOWER(COALESCE(v.severity,'')) WHEN 'critical' THEN 5 WHEN 'high' THEN 4 WHEN 'medium' THEN 3 WHEN 'low' THEN 2 WHEN 'info' THEN 1 ELSE 0 END)
	FROM vulnerabilities v
	WHERE LOWER(COALESCE(v.status,'open')) NOT IN ('fixed','false_positive','ignored') AND ` + assetVulnerabilityMatchExpr + `
),0)`

const assetRiskLevelQueryExpr = `(CASE WHEN ` + assetEffectiveLastScanExpr + ` IS NULL THEN 'unassessed' ELSE CASE ` + assetRiskScoreQueryExpr + `
	WHEN 5 THEN 'critical' WHEN 4 THEN 'high' WHEN 3 THEN 'medium' WHEN 2 THEN 'low' WHEN 1 THEN 'info' ELSE 'normal' END END)`

const assetVulnerabilityCountCachedExpr = `COALESCE(assets.vulnerability_count,0)`
const assetRiskScoreCachedExpr = `COALESCE(assets.risk_score,0)`
const assetRiskLevelCachedExpr = `COALESCE(NULLIF(assets.risk_level,''),'unassessed')`

const assetSelectColumns = `assets.id,COALESCE(assets.project_id,''),COALESCE(p.name,''),assets.host,assets.ip,assets.port,assets.domain,assets.protocol,assets.title,assets.server,assets.country,
	assets.province,assets.city,assets.responsible_person,assets.department,assets.business_system,assets.environment,assets.criticality,
	assets.source,assets.source_query,assets.status,assets.tags_json,assets.first_seen_at,assets.last_seen_at,assets.created_at,assets.updated_at,
	` + assetEffectiveLastScanExpr + `,COALESCE(assets.last_scan_conversation_id,''),COALESCE(assets.last_scan_queue_id,''),COALESCE(assets.last_scan_task_id,''),
	` + assetVulnerabilityCountCachedExpr + `,` + assetRiskLevelCachedExpr

// MarkAssetScanned links an asset to the conversation or batch subtask created from it.
// The link lets the asset list show the latest scan time and vulnerabilities produced by that scan.
func (db *DB) MarkAssetScanned(id, conversationID, queueID, taskID string, access RBACListAccess) error {
	where, args := appendAssetAccess(" WHERE id = ?", []interface{}{strings.TrimSpace(id)}, access, "assets")
	res, err := db.Exec(`UPDATE assets SET last_scan_at=?,last_scan_conversation_id=?,last_scan_queue_id=?,last_scan_task_id=?,updated_at=?`+where,
		append([]interface{}{time.Now(), strings.TrimSpace(conversationID), strings.TrimSpace(queueID), strings.TrimSpace(taskID), time.Now()}, args...)...)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	if err := db.RefreshAssetRiskCache(id); err != nil {
		return err
	}
	return nil
}

// CompleteAssetScan records completion from inside an Agent conversation. If
// the asset was launched as a batch task, keep its task/queue link only when
// that task belongs to the current conversation; a later ad-hoc chat scan must
// not retain stale task associations.
func (db *DB) CompleteAssetScan(id, conversationID string, access RBACListAccess) error {
	id = strings.TrimSpace(id)
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" {
		return fmt.Errorf("扫描对话不能为空")
	}
	where, args := appendAssetAccess(" WHERE id = ?", []interface{}{id}, access, "assets")
	now := time.Now()
	res, err := db.Exec(`UPDATE assets SET
		last_scan_at=?,last_scan_conversation_id=?,
		last_scan_queue_id=CASE WHEN EXISTS (SELECT 1 FROM batch_tasks bt WHERE bt.id=assets.last_scan_task_id AND bt.conversation_id=?) THEN last_scan_queue_id ELSE '' END,
		last_scan_task_id=CASE WHEN EXISTS (SELECT 1 FROM batch_tasks bt WHERE bt.id=assets.last_scan_task_id AND bt.conversation_id=?) THEN last_scan_task_id ELSE '' END,
		updated_at=?`+where,
		append([]interface{}{now, conversationID, conversationID, conversationID, now}, args...)...)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	if err := db.RefreshAssetRiskCache(id); err != nil {
		return err
	}
	return nil
}

func (db *DB) BatchTaskBelongsToQueue(taskID, queueID string) bool {
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM batch_tasks WHERE id=? AND queue_id=?`, strings.TrimSpace(taskID), strings.TrimSpace(queueID)).Scan(&count)
	return err == nil && count > 0
}

func assetRiskLevelFromScore(score int, scanned bool) string {
	if !scanned {
		return "unassessed"
	}
	switch score {
	case 5:
		return "critical"
	case 4:
		return "high"
	case 3:
		return "medium"
	case 2:
		return "low"
	case 1:
		return "info"
	default:
		return "normal"
	}
}

// RefreshAssetRiskCache recalculates the denormalized fields used by the asset
// list. Keeping this in the database layer makes Web API and MCP writes share
// one consistency path.
func (db *DB) RefreshAssetRiskCache(assetID string) error {
	assetID = strings.TrimSpace(assetID)
	if assetID == "" {
		return nil
	}
	var count int
	if err := db.QueryRow("SELECT "+assetVulnerabilityCountExpr+" FROM assets WHERE assets.id=?", assetID).Scan(&count); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return fmt.Errorf("刷新资产漏洞数量失败: %w", err)
	}
	var score int
	if err := db.QueryRow("SELECT "+assetRiskScoreQueryExpr+" FROM assets WHERE assets.id=?", assetID).Scan(&score); err != nil {
		return fmt.Errorf("刷新资产风险分数失败: %w", err)
	}
	var lastScan interface{}
	if err := db.QueryRow("SELECT "+assetEffectiveLastScanExpr+" FROM assets WHERE assets.id=?", assetID).Scan(&lastScan); err != nil {
		return fmt.Errorf("刷新资产扫描状态失败: %w", err)
	}
	level := assetRiskLevelFromScore(score, lastScan != nil)
	if _, err := db.Exec(`UPDATE assets SET vulnerability_count=?, risk_score=?, risk_level=? WHERE id=?`, count, score, level, assetID); err != nil {
		return fmt.Errorf("更新资产风险缓存失败: %w", err)
	}
	return nil
}

func (db *DB) RefreshAllAssetRiskCache() error {
	rows, err := db.Query(`SELECT id FROM assets`)
	if err != nil {
		return fmt.Errorf("查询资产列表失败: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		if err := db.RefreshAssetRiskCache(id); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (db *DB) AssetIDsForVulnerabilityConversations(conversationIDs []string) ([]string, error) {
	seen := map[string]struct{}{}
	cleaned := make([]string, 0, len(conversationIDs))
	for _, id := range conversationIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		cleaned = append(cleaned, id)
	}
	if len(cleaned) == 0 {
		return nil, nil
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(cleaned)), ",")
	args := make([]interface{}, 0, len(cleaned)*2)
	for _, id := range cleaned {
		args = append(args, id)
	}
	for _, id := range cleaned {
		args = append(args, id)
	}
	rows, err := db.Query(`SELECT DISTINCT assets.id FROM assets
		WHERE assets.last_scan_conversation_id IN (`+placeholders+`)
		OR assets.last_scan_task_id IN (SELECT bt.id FROM batch_tasks bt WHERE bt.conversation_id IN (`+placeholders+`))`, args...)
	if err != nil {
		return nil, fmt.Errorf("查询受影响资产失败: %w", err)
	}
	defer rows.Close()
	assetIDs := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		assetIDs = append(assetIDs, id)
	}
	return assetIDs, rows.Err()
}

func (db *DB) RefreshAssetRiskCacheForConversations(conversationIDs ...string) error {
	assetIDs, err := db.AssetIDsForVulnerabilityConversations(conversationIDs)
	if err != nil {
		return err
	}
	for _, id := range assetIDs {
		if err := db.RefreshAssetRiskCache(id); err != nil {
			return err
		}
	}
	return nil
}

func (db *DB) refreshAssetRiskCacheForConversationsBestEffort(conversationIDs ...string) {
	if err := db.RefreshAssetRiskCacheForConversations(conversationIDs...); err != nil && db.logger != nil {
		db.logger.Warn("刷新资产风险缓存失败", zap.Error(err))
	}
}

func (db *DB) ListAssets(limit, offset int, filter AssetListFilter, access RBACListAccess) ([]*Asset, int, error) {
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	where, args := assetWhere(filter, access)
	var total int
	if err := db.QueryRow("SELECT COUNT(*) FROM assets"+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	orderBy := assetOrderBy(filter.SortBy, filter.SortOrder)
	rows, err := db.Query("SELECT "+assetSelectColumns+" FROM assets LEFT JOIN projects p ON p.id=assets.project_id"+where+" ORDER BY "+orderBy+" LIMIT ? OFFSET ?", append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []*Asset{}
	for rows.Next() {
		a, err := scanAsset(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, a)
	}
	return items, total, rows.Err()
}

// ListAssetsForOperation resolves the complete filtered selection used by
// cross-page bulk actions. The caller supplies a strict upper bound.
func (db *DB) ListAssetsForOperation(limit int, filter AssetListFilter, access RBACListAccess) ([]*Asset, int, error) {
	if limit < 1 || limit > 10000 {
		limit = 10000
	}
	where, args := assetWhere(filter, access)
	var total int
	if err := db.QueryRow("SELECT COUNT(*) FROM assets"+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if total > limit {
		return nil, total, fmt.Errorf("匹配资产超过 %d 条，请缩小筛选范围", limit)
	}
	rows, err := db.Query("SELECT "+assetSelectColumns+" FROM assets LEFT JOIN projects p ON p.id=assets.project_id"+where+" ORDER BY "+assetOrderBy(filter.SortBy, filter.SortOrder), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]*Asset, 0, total)
	for rows.Next() {
		item, err := scanAsset(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func assetOrderBy(sortBy, sortOrder string) string {
	direction := "DESC"
	if strings.EqualFold(strings.TrimSpace(sortOrder), "asc") {
		direction = "ASC"
	}
	var expression string
	switch strings.ToLower(strings.TrimSpace(sortBy)) {
	case "last_scan_at":
		expression = assetEffectiveLastScanExpr
		// For oldest-first queries, assets that have never been scanned are the
		// most overdue and intentionally appear first. NULLs stay last for DESC.
		if direction == "ASC" {
			return "CASE WHEN " + expression + " IS NULL THEN 0 ELSE 1 END ASC, " + expression + " ASC, assets.id ASC"
		}
		return "CASE WHEN " + expression + " IS NULL THEN 1 ELSE 0 END ASC, " + expression + " DESC, assets.id ASC"
	case "first_seen_at":
		expression = "assets.first_seen_at"
	case "created_at":
		expression = "assets.created_at"
	case "updated_at":
		expression = "assets.updated_at"
	case "host":
		expression = "LOWER(assets.host)"
	case "port":
		expression = "assets.port"
	case "vulnerability_count":
		expression = assetVulnerabilityCountCachedExpr
	case "risk_level":
		expression = assetRiskScoreCachedExpr
	default:
		expression = "assets.last_seen_at"
	}
	return expression + " " + direction + ", assets.id ASC"
}

func (db *DB) GetAsset(id string, access RBACListAccess) (*Asset, error) {
	query, args := appendAssetAccess("SELECT "+assetSelectColumns+" FROM assets LEFT JOIN projects p ON p.id=assets.project_id WHERE assets.id = ?", []interface{}{id}, access, "assets")
	return scanAsset(db.QueryRow(query, args...))
}

func (db *DB) UpdateAsset(id string, a *Asset, access RBACListAccess) error {
	normalizeAsset(a)
	if err := validateAsset(a); err != nil {
		return err
	}
	key := assetDedupKey(a)
	tags, _ := json.Marshal(a.Tags)
	where, args := appendAssetAccess(" WHERE id = ?", []interface{}{id}, access, "assets")
	res, err := db.Exec(`UPDATE assets SET dedup_key=?,project_id=?,host=?,ip=?,port=?,domain=?,protocol=?,title=?,server=?,country=?,province=?,city=?,
		responsible_person=?,department=?,business_system=?,environment=?,criticality=?,source=?,source_query=?,status=?,tags_json=?,updated_at=?`+where,
		append([]interface{}{key, nullIfEmpty(a.ProjectID), a.Host, a.IP, a.Port, a.Domain, a.Protocol, a.Title, a.Server, a.Country, a.Province, a.City,
			a.ResponsiblePerson, a.Department, a.BusinessSystem, a.Environment, a.Criticality, a.Source, a.SourceQuery, a.Status, string(tags), time.Now()}, args...)...)
	if err != nil {
		return translateAssetDedupConflict(err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

type AssetBulkPatch struct {
	Status            *string
	ResponsiblePerson *string
	Department        *string
	BusinessSystem    *string
	Environment       *string
	Criticality       *string
	AddTags           []string
	RemoveTags        []string
}

func normalizeAssetIDs(ids []string) []string {
	unique := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	return unique
}

func normalizeBulkTags(tags []string) ([]string, error) {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if utf8.RuneCountInString(tag) > 64 {
			return nil, assetValidationErrorf("单个标签不能超过 64 个字符")
		}
		if _, exists := seen[tag]; exists {
			continue
		}
		seen[tag] = struct{}{}
		result = append(result, tag)
	}
	return result, nil
}

// UpdateAssetsBulk atomically applies operational metadata to a selected set.
func (db *DB) UpdateAssetsBulk(ids []string, patch AssetBulkPatch, access RBACListAccess) (int, error) {
	unique := normalizeAssetIDs(ids)
	if len(unique) == 0 {
		return 0, fmt.Errorf("资产列表不能为空")
	}
	if patch.Status != nil {
		value := strings.ToLower(strings.TrimSpace(*patch.Status))
		if value != "active" && value != "inactive" {
			return 0, assetValidationErrorf("资产状态必须为 active 或 inactive")
		}
		patch.Status = &value
	}
	if patch.Environment != nil {
		value := strings.ToLower(strings.TrimSpace(*patch.Environment))
		if !oneOfAssetValue(value, "", "production", "staging", "testing", "development", "other") {
			return 0, assetValidationErrorf("环境值无效")
		}
		patch.Environment = &value
	}
	if patch.Criticality != nil {
		value := strings.ToLower(strings.TrimSpace(*patch.Criticality))
		if !oneOfAssetValue(value, "", "critical", "high", "medium", "low") {
			return 0, assetValidationErrorf("重要性值无效")
		}
		patch.Criticality = &value
	}
	var err error
	if patch.AddTags, err = normalizeBulkTags(patch.AddTags); err != nil {
		return 0, err
	}
	if patch.RemoveTags, err = normalizeBulkTags(patch.RemoveTags); err != nil {
		return 0, err
	}

	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(unique)), ",")
	idArgs := make([]interface{}, len(unique))
	for i, id := range unique {
		idArgs[i] = id
	}
	countQuery, countArgs := appendAssetAccess("SELECT COUNT(*) FROM assets WHERE id IN ("+placeholders+")", idArgs, access, "assets")
	var accessible int
	if err := tx.QueryRow(countQuery, countArgs...).Scan(&accessible); err != nil {
		return 0, err
	}
	if accessible != len(unique) {
		return 0, fmt.Errorf("部分资产不存在或无权更新")
	}

	for _, id := range unique {
		var rawTags string
		if err := tx.QueryRow("SELECT tags_json FROM assets WHERE id=?", id).Scan(&rawTags); err != nil {
			return 0, err
		}
		tags := []string{}
		_ = json.Unmarshal([]byte(rawTags), &tags)
		remove := map[string]struct{}{}
		for _, tag := range patch.RemoveTags {
			remove[tag] = struct{}{}
		}
		merged := make([]string, 0, len(tags)+len(patch.AddTags))
		seen := map[string]struct{}{}
		for _, tag := range append(tags, patch.AddTags...) {
			if _, removed := remove[tag]; removed {
				continue
			}
			if _, exists := seen[tag]; exists {
				continue
			}
			seen[tag] = struct{}{}
			merged = append(merged, tag)
		}
		if len(merged) > 30 {
			return 0, assetValidationErrorf("批量修改后标签不能超过 30 个")
		}
		tagsJSON, _ := json.Marshal(merged)
		_, err := tx.Exec(`UPDATE assets SET
			status=CASE WHEN ? THEN ? ELSE status END,
			responsible_person=CASE WHEN ? THEN ? ELSE responsible_person END,
			department=CASE WHEN ? THEN ? ELSE department END,
			business_system=CASE WHEN ? THEN ? ELSE business_system END,
			environment=CASE WHEN ? THEN ? ELSE environment END,
			criticality=CASE WHEN ? THEN ? ELSE criticality END,
			tags_json=?,updated_at=? WHERE id=?`,
			patch.Status != nil, valueOrEmpty(patch.Status),
			patch.ResponsiblePerson != nil, valueOrEmpty(patch.ResponsiblePerson),
			patch.Department != nil, valueOrEmpty(patch.Department),
			patch.BusinessSystem != nil, valueOrEmpty(patch.BusinessSystem),
			patch.Environment != nil, valueOrEmpty(patch.Environment),
			patch.Criticality != nil, valueOrEmpty(patch.Criticality),
			string(tagsJSON), time.Now(), id)
		if err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(unique), nil
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func (db *DB) DeleteAssets(ids []string, access RBACListAccess) (int, error) {
	unique := normalizeAssetIDs(ids)
	if len(unique) == 0 {
		return 0, fmt.Errorf("资产列表不能为空")
	}
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(unique)), ",")
	args := make([]interface{}, len(unique))
	for i, id := range unique {
		args[i] = id
	}
	countQuery, countArgs := appendAssetAccess("SELECT COUNT(*) FROM assets WHERE id IN ("+placeholders+")", args, access, "assets")
	var accessible int
	if err := tx.QueryRow(countQuery, countArgs...).Scan(&accessible); err != nil {
		return 0, err
	}
	if accessible != len(unique) {
		return 0, fmt.Errorf("部分资产不存在或无权删除")
	}
	deleteQuery, deleteArgs := appendAssetAccess("DELETE FROM assets WHERE id IN ("+placeholders+")", args, access, "assets")
	result, err := tx.Exec(deleteQuery, deleteArgs...)
	if err != nil {
		return 0, err
	}
	deleted, err := result.RowsAffected()
	if err != nil || int(deleted) != len(unique) {
		return 0, fmt.Errorf("批量删除资产失败")
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return int(deleted), nil
}

// MergeAssets atomically updates the surviving asset and removes duplicates.
// Separate access scopes preserve permission-specific RBAC boundaries.
func (db *DB) MergeAssets(primary *Asset, duplicateIDs []string, writeAccess, deleteAccess RBACListAccess) (int, error) {
	if primary == nil || strings.TrimSpace(primary.ID) == "" {
		return 0, fmt.Errorf("主资产不能为空")
	}
	normalizeAsset(primary)
	if err := validateAsset(primary); err != nil {
		return 0, err
	}
	duplicates := normalizeAssetIDs(duplicateIDs)
	filtered := duplicates[:0]
	for _, id := range duplicates {
		if id != primary.ID {
			filtered = append(filtered, id)
		}
	}
	duplicates = filtered
	if len(duplicates) == 0 {
		return 0, fmt.Errorf("重复资产列表不能为空")
	}
	key := assetDedupKey(primary)
	tagsJSON, _ := json.Marshal(primary.Tags)

	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	primaryQuery, primaryArgs := appendAssetAccess("SELECT COUNT(*) FROM assets WHERE id=?", []interface{}{primary.ID}, writeAccess, "assets")
	var primaryCount int
	if err := tx.QueryRow(primaryQuery, primaryArgs...).Scan(&primaryCount); err != nil || primaryCount != 1 {
		return 0, fmt.Errorf("主资产不存在或无权更新")
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(duplicates)), ",")
	deleteArgs := make([]interface{}, len(duplicates))
	for i, id := range duplicates {
		deleteArgs[i] = id
	}
	countQuery, countArgs := appendAssetAccess("SELECT COUNT(*) FROM assets WHERE id IN ("+placeholders+")", deleteArgs, deleteAccess, "assets")
	var accessible int
	if err := tx.QueryRow(countQuery, countArgs...).Scan(&accessible); err != nil || accessible != len(duplicates) {
		return 0, fmt.Errorf("部分重复资产不存在或无权删除")
	}
	deleteQuery, scopedDeleteArgs := appendAssetAccess("DELETE FROM assets WHERE id IN ("+placeholders+")", deleteArgs, deleteAccess, "assets")
	if result, err := tx.Exec(deleteQuery, scopedDeleteArgs...); err != nil {
		return 0, err
	} else if deleted, _ := result.RowsAffected(); int(deleted) != len(duplicates) {
		return 0, fmt.Errorf("删除重复资产失败")
	}
	updateQuery, updateScopeArgs := appendAssetAccess(`UPDATE assets SET dedup_key=?,project_id=?,host=?,ip=?,port=?,domain=?,protocol=?,title=?,server=?,country=?,province=?,city=?,
		responsible_person=?,department=?,business_system=?,environment=?,criticality=?,source=?,source_query=?,status=?,tags_json=?,updated_at=? WHERE id=?`,
		[]interface{}{key, nullIfEmpty(primary.ProjectID), primary.Host, primary.IP, primary.Port, primary.Domain, primary.Protocol, primary.Title, primary.Server,
			primary.Country, primary.Province, primary.City, primary.ResponsiblePerson, primary.Department, primary.BusinessSystem, primary.Environment,
			primary.Criticality, primary.Source, primary.SourceQuery, primary.Status, string(tagsJSON), time.Now(), primary.ID}, writeAccess, "assets")
	result, err := tx.Exec(updateQuery, updateScopeArgs...)
	if err != nil {
		return 0, translateAssetDedupConflict(err)
	}
	if updated, _ := result.RowsAffected(); updated != 1 {
		return 0, fmt.Errorf("更新主资产失败")
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(duplicates), nil
}

// UpdateAssetsProject atomically replaces the project binding for every asset.
// It refuses the whole update when any requested asset is missing or outside
// the caller's access scope, so a bulk action can never partially succeed.
func (db *DB) UpdateAssetsProject(ids []string, projectID string, access RBACListAccess) (int, error) {
	unique := normalizeAssetIDs(ids)
	if len(unique) == 0 {
		return 0, fmt.Errorf("资产列表不能为空")
	}

	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(unique)), ",")
	idArgs := make([]interface{}, len(unique))
	for i, id := range unique {
		idArgs[i] = id
	}
	countQuery, countArgs := appendAssetAccess("SELECT COUNT(*) FROM assets WHERE id IN ("+placeholders+")", idArgs, access, "assets")
	var accessible int
	if err := tx.QueryRow(countQuery, countArgs...).Scan(&accessible); err != nil {
		return 0, err
	}
	if accessible != len(unique) {
		return 0, fmt.Errorf("部分资产不存在或无权更新")
	}

	updateArgs := []interface{}{nullIfEmpty(strings.TrimSpace(projectID)), time.Now()}
	updateArgs = append(updateArgs, idArgs...)
	updateQuery, updateArgs := appendAssetAccess("UPDATE assets SET project_id=?,updated_at=? WHERE id IN ("+placeholders+")", updateArgs, access, "assets")
	result, err := tx.Exec(updateQuery, updateArgs...)
	if err != nil {
		return 0, err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if int(updated) != len(unique) {
		return 0, fmt.Errorf("批量更新资产失败")
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return int(updated), nil
}

func (db *DB) DeleteAsset(id string, access RBACListAccess) error {
	where, args := appendAssetAccess(" WHERE id = ?", []interface{}{id}, access, "assets")
	res, err := db.Exec("DELETE FROM assets"+where, args...)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (db *DB) GetAssetStats(access RBACListAccess, requestedDays ...int) (map[string]interface{}, error) {
	days := 30
	if len(requestedDays) > 0 && (requestedDays[0] == 7 || requestedDays[0] == 30 || requestedDays[0] == 90) {
		days = requestedDays[0]
	}
	where, args := appendAssetAccess(" WHERE 1=1", nil, access, "assets")
	stats := map[string]interface{}{}
	row := db.QueryRow(`SELECT COUNT(*),COUNT(DISTINCT NULLIF(ip,'')),COUNT(DISTINCT NULLIF(domain,'')),
		COUNT(DISTINCT CASE WHEN port>0 THEN CAST(port AS TEXT) END),
		COALESCE(SUM(CASE WHEN datetime(last_seen_at)>=datetime('now','-7 days') THEN 1 ELSE 0 END),0) FROM assets`+where, args...)
	var total, ips, domains, ports, recent int
	if err := row.Scan(&total, &ips, &domains, &ports, &recent); err != nil {
		return nil, err
	}
	stats["total"], stats["ips"], stats["domains"], stats["ports"], stats["recent"] = total, ips, domains, ports, recent
	rows, err := db.Query(`SELECT CASE WHEN protocol='' THEN 'unknown' ELSE protocol END,COUNT(*) FROM assets`+where+` GROUP BY protocol ORDER BY COUNT(*) DESC LIMIT 8`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	dist := []map[string]interface{}{}
	for rows.Next() {
		var name string
		var count int
		if err := rows.Scan(&name, &count); err != nil {
			return nil, err
		}
		dist = append(dist, map[string]interface{}{"name": name, "count": count})
	}
	stats["protocols"] = dist
	stats["period_days"] = days

	coverage := map[string]interface{}{}
	coverageRow := db.QueryRow(`SELECT
		COALESCE(SUM(CASE WHEN last_scan_at IS NOT NULL THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN datetime(last_scan_at)>=datetime('now','-7 days') THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN datetime(last_scan_at)>=datetime('now','-30 days') THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN last_scan_at IS NULL THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN last_scan_at IS NOT NULL AND datetime(last_scan_at)<datetime('now','-30 days') THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN status='active' THEN 1 ELSE 0 END),0)
		FROM assets`+where, args...)
	var scanned, scanned7, scanned30, neverScanned, stale, active int
	if err := coverageRow.Scan(&scanned, &scanned7, &scanned30, &neverScanned, &stale, &active); err != nil {
		return nil, err
	}
	coverage["scanned"], coverage["scanned_7d"], coverage["scanned_30d"] = scanned, scanned7, scanned30
	coverage["never_scanned"], coverage["stale"], coverage["active"] = neverScanned, stale, active
	if total > 0 {
		coverage["rate"] = int(float64(scanned) / float64(total) * 100)
		coverage["recent_rate"] = int(float64(scanned30) / float64(total) * 100)
	} else {
		coverage["rate"], coverage["recent_rate"] = 0, 0
	}
	stats["coverage"] = coverage

	assetDaily := map[string]map[string]int{}
	trendWhere, trendArgs := appendAssetAccess(" WHERE datetime(first_seen_at)>=datetime('now',?)", []interface{}{fmt.Sprintf("-%d days", days-1)}, access, "assets")
	trendRows, err := db.Query(`SELECT date(first_seen_at), COUNT(*)
		FROM assets`+trendWhere+` GROUP BY date(first_seen_at) ORDER BY date(first_seen_at)`, trendArgs...)
	if err != nil {
		return nil, err
	}
	for trendRows.Next() {
		var day string
		var added int
		if err := trendRows.Scan(&day, &added); err != nil {
			trendRows.Close()
			return nil, err
		}
		assetDaily[day] = map[string]int{"added": added, "inactive": 0}
	}
	if err := trendRows.Close(); err != nil {
		return nil, err
	}
	inactiveWhere, inactiveArgs := appendAssetAccess(" WHERE status='inactive' AND datetime(updated_at)>=datetime('now',?)", []interface{}{fmt.Sprintf("-%d days", days-1)}, access, "assets")
	inactiveRows, err := db.Query(`SELECT date(updated_at), COUNT(*) FROM assets`+inactiveWhere+` GROUP BY date(updated_at) ORDER BY date(updated_at)`, inactiveArgs...)
	if err != nil {
		return nil, err
	}
	for inactiveRows.Next() {
		var day string
		var inactive int
		if err := inactiveRows.Scan(&day, &inactive); err != nil {
			inactiveRows.Close()
			return nil, err
		}
		if _, ok := assetDaily[day]; !ok {
			assetDaily[day] = map[string]int{"added": 0, "inactive": 0}
		}
		assetDaily[day]["inactive"] = inactive
	}
	if err := inactiveRows.Close(); err != nil {
		return nil, err
	}

	riskDaily := map[string]map[string]int{}
	riskWhere, riskArgs := appendVulnerabilityAccessFilter(" WHERE datetime(created_at)>=datetime('now',?)", []interface{}{fmt.Sprintf("-%d days", days-1)}, access)
	riskRows, err := db.Query(`SELECT date(created_at), COUNT(*),
		COALESCE(SUM(CASE WHEN LOWER(severity) IN ('critical','high') THEN 1 ELSE 0 END),0)
		FROM vulnerabilities`+riskWhere+` GROUP BY date(created_at) ORDER BY date(created_at)`, riskArgs...)
	if err != nil {
		return nil, err
	}
	for riskRows.Next() {
		var day string
		var discovered, highRisk int
		if err := riskRows.Scan(&day, &discovered, &highRisk); err != nil {
			riskRows.Close()
			return nil, err
		}
		riskDaily[day] = map[string]int{"discovered": discovered, "high_risk": highRisk}
	}
	if err := riskRows.Close(); err != nil {
		return nil, err
	}

	assetTrend := make([]map[string]interface{}, 0, days)
	riskTrend := make([]map[string]interface{}, 0, days)
	start := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -(days - 1))
	for i := 0; i < days; i++ {
		day := start.AddDate(0, 0, i).Format("2006-01-02")
		assetPoint := map[string]interface{}{"date": day, "added": 0, "inactive": 0}
		if values, ok := assetDaily[day]; ok {
			assetPoint["added"], assetPoint["inactive"] = values["added"], values["inactive"]
		}
		assetTrend = append(assetTrend, assetPoint)
		riskPoint := map[string]interface{}{"date": day, "discovered": 0, "high_risk": 0}
		if values, ok := riskDaily[day]; ok {
			riskPoint["discovered"], riskPoint["high_risk"] = values["discovered"], values["high_risk"]
		}
		riskTrend = append(riskTrend, riskPoint)
	}
	stats["asset_trend"], stats["risk_trend"] = assetTrend, riskTrend
	return stats, rows.Err()
}
