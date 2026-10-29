package handler

import (
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"cyberstrike-ai/internal/database"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

//
// 与 /api/assets/tree（层级树）互补：树适合看归属关系，表格适合按类型检索。
// 支持的类型：subdomain（子域名）、site（站点）、ip、service（服务）。

type assetTablePortItem struct {
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
	Service  string `json:"service"`
}

type assetTableRow struct {
	Key         string               `json:"key"`
	Host        string               `json:"host"`
	Domain      string               `json:"domain"`
	IP          string               `json:"ip"`
	Port        int                  `json:"port"`
	Protocol    string               `json:"protocol"`
	Service     string               `json:"service"`
	Version     string               `json:"version"`
	RecordType  string               `json:"record_type"`
	RecordValue string               `json:"record_value"`
	IPs         []string             `json:"ips"`
	Title       string               `json:"title"`
	Server      string               `json:"server"`
	Source      string               `json:"source"`
	Sources     []string             `json:"sources"`
	AssetCount  int                  `json:"asset_count"`
	Ports       []assetTablePortItem `json:"ports,omitempty"`
	FirstSeenAt time.Time            `json:"first_seen_at"`
	LastSeenAt  time.Time            `json:"last_seen_at"`
}

type assetTableResponse struct {
	Type     string          `json:"type"`
	Total    int             `json:"total"`
	Page     int             `json:"page"`
	PageSize int             `json:"page_size"`
	Facets   map[string]int  `json:"facets"`
	Rows     []assetTableRow `json:"rows"`
}

const (
	assetTableDefaultPageSize = 50
	assetTableMaxPageSize     = 200
)

// Table 返回按资产类型聚合的表格数据。
func (h *AssetHandler) Table(c *gin.Context) {
	tableType := strings.ToLower(strings.TrimSpace(c.DefaultQuery("type", "subdomain")))
	switch tableType {
	case "subdomain", "site", "ip", "service":
	default:
		c.JSON(400, gin.H{"error": "type 仅支持 subdomain/site/ip/service"})
		return
	}
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil || page < 1 {
		page = 1
	}
	pageSize, err := strconv.Atoi(c.DefaultQuery("page_size", strconv.Itoa(assetTableDefaultPageSize)))
	if err != nil || pageSize < 1 {
		pageSize = assetTableDefaultPageSize
	}
	if pageSize > assetTableMaxPageSize {
		pageSize = assetTableMaxPageSize
	}
	keyword := strings.ToLower(strings.TrimSpace(c.Query("q")))
	sourceFilter := strings.TrimSpace(c.Query("source"))
	recordTypeFilter := strings.ToUpper(strings.TrimSpace(c.Query("record_type")))

	filter, err := assetListFilterFromQuery(c)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	assets := h.loadAssetsForView(filter, assetAccess(c))

	hosts := make([]string, 0, len(assets))
	ids := make([]string, 0, len(assets))
	for _, asset := range assets {
		if asset == nil {
			continue
		}
		if host := strings.ToLower(strings.TrimSpace(asset.Host)); host != "" && net.ParseIP(host) == nil {
			hosts = append(hosts, host)
		}
		ids = append(ids, asset.ID)
	}
	hostIPs, err := h.db.ListAssetHostIPs(hosts)
	if err != nil {
		h.logger.Warn("资产表格：查询域名解析关系失败，降级为单 IP", zap.Error(err))
		hostIPs = nil
	}
	fingerprints, err := h.db.ListAssetFingerprints(ids)
	if err != nil {
		h.logger.Warn("资产表格：查询指纹失败，降级为无服务信息", zap.Error(err))
		fingerprints = nil
	}

	allRows := buildAssetTableRows(tableType, assets, hostIPs, fingerprints)
	facets := assetTableFacets(assets, hostIPs)

	rows := filterAssetTableRows(allRows, keyword, sourceFilter, recordTypeFilter)
	total := len(rows)
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	paged := make([]assetTableRow, 0, end-start)
	paged = append(paged, rows[start:end]...)

	c.JSON(200, assetTableResponse{
		Type:     tableType,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
		Facets:   facets,
		Rows:     paged,
	})
}

// loadAssetsForView 分页拉全量资产（与层级视图一致的上限策略）。
func (h *AssetHandler) loadAssetsForView(filter database.AssetListFilter, access database.RBACListAccess) []*database.Asset {
	items := make([]*database.Asset, 0, assetTreePageSize)
	for offset := 0; offset < assetTreeMaxRows; offset += assetTreePageSize {
		batch, total, err := h.db.ListAssets(assetTreePageSize, offset, filter, access)
		if err != nil {
			h.logger.Error("资产视图：查询资产失败", zap.Error(err))
			return items
		}
		items = append(items, batch...)
		if len(batch) < assetTreePageSize || len(items) >= total {
			break
		}
	}
	return items
}

func buildAssetTableRows(tableType string, assets []*database.Asset, hostIPs map[string][]database.AssetHostIP, fingerprints map[string][]database.AssetFingerprint) []assetTableRow {
	switch tableType {
	case "subdomain":
		return buildSubdomainRows(assets, hostIPs)
	case "site":
		return buildSiteRows(assets, hostIPs)
	case "ip":
		return buildIPRows(assets, fingerprints)
	case "service":
		return buildServiceRows(assets, fingerprints)
	}
	return nil
}

// subdomainIPs 返回某个域名关联的 IP：优先用解析关系表（多 A 记录），
// 没有记录时退回资产自身的 IP，保证手工导入的数据也能显示。
func subdomainIPs(host string, asset *database.Asset, hostIPs map[string][]database.AssetHostIP) []string {
	ips := make([]string, 0, 2)
	for _, item := range hostIPs[strings.ToLower(host)] {
		if item.IP != "" && !containsString(ips, item.IP) {
			ips = append(ips, item.IP)
		}
	}
	if ip := strings.TrimSpace(asset.IP); ip != "" && !containsString(ips, ip) {
		ips = append(ips, ip)
	}
	return ips
}

func buildSubdomainRows(assets []*database.Asset, hostIPs map[string][]database.AssetHostIP) []assetTableRow {
	type aggregate struct {
		row   assetTableRow
		count int
	}
	byHost := make(map[string]*aggregate)
	for _, asset := range assets {
		if asset == nil {
			continue
		}
		host := strings.ToLower(strings.TrimSpace(asset.Host))
		if host == "" || net.ParseIP(host) != nil {
			continue
		}
		agg, ok := byHost[host]
		if !ok {
			agg = &aggregate{row: assetTableRow{
				Key:    host,
				Host:   host,
				Domain: assetRootDomain(asset),
				IPs:    subdomainIPs(host, asset, hostIPs),
			}}
			byHost[host] = agg
		}
		agg.count++
		if agg.row.Source == "" && asset.Source != "" {
			agg.row.Source = asset.Source
		}
		agg.row.Sources = appendUniqueString(agg.row.Sources, asset.Source)
		if agg.row.FirstSeenAt.IsZero() || asset.FirstSeenAt.Before(agg.row.FirstSeenAt) {
			agg.row.FirstSeenAt = asset.FirstSeenAt
		}
		if asset.LastSeenAt.After(agg.row.LastSeenAt) {
			agg.row.LastSeenAt = asset.LastSeenAt
		}
	}
	rows := make([]assetTableRow, 0, len(byHost))
	for _, agg := range byHost {
		row := agg.row
		row.AssetCount = agg.count
		if len(row.IPs) > 0 {
			// 目前只采集到 A 记录；CNAME 采集接入后此处可扩展。
			row.RecordType = "A"
			row.RecordValue = row.IPs[0]
			row.IP = row.IPs[0]
		}
		sort.Strings(row.Sources)
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Domain != rows[j].Domain {
			return rows[i].Domain < rows[j].Domain
		}
		return rows[i].Host < rows[j].Host
	})
	return rows
}

func buildSiteRows(assets []*database.Asset, hostIPs map[string][]database.AssetHostIP) []assetTableRow {
	rows := make([]assetTableRow, 0, len(assets))
	for _, asset := range assets {
		if asset == nil {
			continue
		}
		protocol := strings.ToLower(strings.TrimSpace(asset.Protocol))
		if protocol != "http" && protocol != "https" {
			continue
		}
		host := strings.ToLower(strings.TrimSpace(asset.Host))
		row := assetTableRow{
			Key:         host + "\x00" + strconv.Itoa(asset.Port) + "\x00" + protocol,
			Host:        host,
			Domain:      assetRootDomain(asset),
			IP:          assetIPAddress(asset),
			Port:        asset.Port,
			Protocol:    protocol,
			Title:       asset.Title,
			Server:      asset.Server,
			Source:      asset.Source,
			IPs:         subdomainIPs(host, asset, hostIPs),
			AssetCount:  1,
			FirstSeenAt: asset.FirstSeenAt,
			LastSeenAt:  asset.LastSeenAt,
		}
		if row.Source != "" {
			row.Sources = []string{row.Source}
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Host != rows[j].Host {
			return rows[i].Host < rows[j].Host
		}
		return rows[i].Port < rows[j].Port
	})
	return rows
}

func buildIPRows(assets []*database.Asset, fingerprints map[string][]database.AssetFingerprint) []assetTableRow {
	type aggregate struct {
		row   assetTableRow
		hosts map[string]bool
		ports map[string]assetTablePortItem
	}
	byIP := make(map[string]*aggregate)
	for _, asset := range assets {
		if asset == nil {
			continue
		}
		ip := assetIPAddress(asset)
		if ip == "" {
			continue
		}
		agg, ok := byIP[ip]
		if !ok {
			agg = &aggregate{
				row:   assetTableRow{Key: ip, IP: ip, Source: asset.Source},
				hosts: make(map[string]bool),
				ports: make(map[string]assetTablePortItem),
			}
			byIP[ip] = agg
		}
		host := strings.ToLower(strings.TrimSpace(asset.Host))
		if host != "" && net.ParseIP(host) == nil {
			agg.hosts[host] = true
		}
		agg.row.Sources = appendUniqueString(agg.row.Sources, asset.Source)
		if agg.row.FirstSeenAt.IsZero() || asset.FirstSeenAt.Before(agg.row.FirstSeenAt) {
			agg.row.FirstSeenAt = asset.FirstSeenAt
		}
		if asset.LastSeenAt.After(agg.row.LastSeenAt) {
			agg.row.LastSeenAt = asset.LastSeenAt
		}
		if asset.Port > 0 {
			service := assetTableServiceFor(asset, fingerprints)
			portKey := strconv.Itoa(asset.Port) + "|" + strings.ToLower(strings.TrimSpace(asset.Protocol))
			agg.ports[portKey] = assetTablePortItem{
				Port:     asset.Port,
				Protocol: strings.ToLower(strings.TrimSpace(asset.Protocol)),
				Service:  service,
			}
		}
	}
	rows := make([]assetTableRow, 0, len(byIP))
	for _, agg := range byIP {
		row := agg.row
		// asset_count 表示该 IP 关联的域名数；纯 IP 资产按 1 计。
		row.AssetCount = len(agg.hosts)
		if row.AssetCount == 0 {
			row.AssetCount = 1
		}
		for _, item := range agg.ports {
			row.Ports = append(row.Ports, item)
		}
		sort.Slice(row.Ports, func(i, j int) bool {
			if row.Ports[i].Port != row.Ports[j].Port {
				return row.Ports[i].Port < row.Ports[j].Port
			}
			return row.Ports[i].Protocol < row.Ports[j].Protocol
		})
		sort.Strings(row.Sources)
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].IP < rows[j].IP })
	return rows
}

func buildServiceRows(assets []*database.Asset, fingerprints map[string][]database.AssetFingerprint) []assetTableRow {
	rows := make([]assetTableRow, 0, len(assets))
	for _, asset := range assets {
		if asset == nil || asset.Port <= 0 {
			continue
		}
		host := strings.ToLower(strings.TrimSpace(asset.Host))
		row := assetTableRow{
			Key:         host + "\x00" + strconv.Itoa(asset.Port) + "\x00" + strings.ToLower(asset.Protocol),
			Host:        host,
			Domain:      assetRootDomain(asset),
			IP:          assetIPAddress(asset),
			Port:        asset.Port,
			Protocol:    strings.ToLower(strings.TrimSpace(asset.Protocol)),
			Service:     assetTableServiceFor(asset, fingerprints),
			Version:     assetTableVersionFor(asset, fingerprints),
			Source:      asset.Source,
			AssetCount:  1,
			FirstSeenAt: asset.FirstSeenAt,
			LastSeenAt:  asset.LastSeenAt,
		}
		if row.Source != "" {
			row.Sources = []string{row.Source}
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Host != rows[j].Host {
			return rows[i].Host < rows[j].Host
		}
		if rows[i].Port != rows[j].Port {
			return rows[i].Port < rows[j].Port
		}
		return rows[i].Protocol < rows[j].Protocol
	})
	return rows
}

// assetTableServiceFor 取资产的服务名：优先指纹表，其次资产 Server 头，最后协议。
func assetTableServiceFor(asset *database.Asset, fingerprints map[string][]database.AssetFingerprint) string {
	for _, fp := range fingerprints[asset.ID] {
		if fp.Service != "" {
			return fp.Service
		}
	}
	if server := strings.ToLower(strings.TrimSpace(asset.Server)); server != "" {
		return server
	}
	return strings.ToLower(strings.TrimSpace(asset.Protocol))
}

// assetTableVersionFor 取资产的版本：指纹表里第一个带版本的记录。
func assetTableVersionFor(asset *database.Asset, fingerprints map[string][]database.AssetFingerprint) string {
	for _, fp := range fingerprints[asset.ID] {
		if fp.Version != "" {
			return fp.Version
		}
	}
	return ""
}

func assetTableFacets(assets []*database.Asset, hostIPs map[string][]database.AssetHostIP) map[string]int {
	hosts := make(map[string]bool)
	ips := make(map[string]bool)
	facets := map[string]int{"subdomain": 0, "site": 0, "ip": 0, "service": 0}
	for _, asset := range assets {
		if asset == nil {
			continue
		}
		host := strings.ToLower(strings.TrimSpace(asset.Host))
		if host != "" && net.ParseIP(host) == nil {
			hosts[host] = true
		}
		protocol := strings.ToLower(strings.TrimSpace(asset.Protocol))
		if protocol == "http" || protocol == "https" {
			facets["site"]++
		}
		if asset.Port > 0 {
			facets["service"]++
		}
		if ip := assetIPAddress(asset); ip != "" {
			ips[ip] = true
		}
	}
	// 解析关系表里出现、但资产尚未落库的域名也算进子域名总数。
	for host := range hostIPs {
		if host != "" {
			hosts[host] = true
		}
	}
	facets["subdomain"] = len(hosts)
	facets["ip"] = len(ips)
	return facets
}

func filterAssetTableRows(rows []assetTableRow, keyword, source, recordType string) []assetTableRow {
	if keyword == "" && source == "" && recordType == "" {
		return rows
	}
	filtered := make([]assetTableRow, 0, len(rows))
	for _, row := range rows {
		if source != "" && !strings.EqualFold(row.Source, source) && !containsStringFold(row.Sources, source) {
			continue
		}
		if recordType != "" && !strings.EqualFold(row.RecordType, recordType) {
			continue
		}
		if keyword != "" && !assetTableRowMatches(row, keyword) {
			continue
		}
		filtered = append(filtered, row)
	}
	return filtered
}

func assetTableRowMatches(row assetTableRow, keyword string) bool {
	for _, field := range []string{row.Host, row.Domain, row.IP, row.Title, row.Server, row.Service} {
		if strings.Contains(strings.ToLower(field), keyword) {
			return true
		}
	}
	for _, ip := range row.IPs {
		if strings.Contains(strings.ToLower(ip), keyword) {
			return true
		}
	}
	return false
}

func containsStringFold(items []string, want string) bool {
	for _, item := range items {
		if strings.EqualFold(item, want) {
			return true
		}
	}
	return false
}
