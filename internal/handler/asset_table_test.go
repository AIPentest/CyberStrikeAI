package handler

import (
	"testing"
	"time"

	"cyberstrike-ai/internal/database"
)

// 资产表格视图（按类型分 tab）的纯逻辑测试。

func tableTestAssets() []*database.Asset {
	ts := time.Date(2026, 10, 29, 10, 0, 0, 0, time.Local)
	return []*database.Asset{
		{ID: "1", Host: "a.example.com", Domain: "example.com", IP: "1.1.1.1", Port: 443, Protocol: "https", Title: "A站", Server: "nginx", Source: "tool:httpx", FirstSeenAt: ts, LastSeenAt: ts},
		{ID: "2", Host: "a.example.com", Domain: "example.com", IP: "1.1.1.1", Port: 80, Protocol: "http", Source: "tool:httpx", FirstSeenAt: ts, LastSeenAt: ts},
		{ID: "3", Host: "b.example.com", Domain: "example.com", IP: "1.1.1.1", Port: 443, Protocol: "https", Title: "B站", Server: "apache", Source: "tool:nmap", FirstSeenAt: ts, LastSeenAt: ts},
		{ID: "4", Host: "9.9.9.9", IP: "9.9.9.9", Port: 3389, Protocol: "rdp", Source: "manual", FirstSeenAt: ts, LastSeenAt: ts},
	}
}

func tableTestHostIPs() map[string][]database.AssetHostIP {
	return map[string][]database.AssetHostIP{
		"a.example.com": {
			{Host: "a.example.com", IP: "1.1.1.1", RecordType: "A"},
			{Host: "a.example.com", IP: "203.0.113.9", RecordType: "A"},
		},
	}
}

func tableTestFingerprints() map[string][]database.AssetFingerprint {
	return map[string][]database.AssetFingerprint{
		"1": {{AssetID: "1", Service: "http", Technology: "nginx", Version: "1.22.0"}},
		"4": {{AssetID: "4", Service: "rdp"}},
	}
}

func TestBuildSubdomainRowsMultiIP(t *testing.T) {
	rows := buildSubdomainRows(tableTestAssets(), tableTestHostIPs())
	if len(rows) != 2 {
		t.Fatalf("子域名行数 = %d, want 2（纯 IP 资产不应出现在子域名 tab）: %+v", len(rows), rows)
	}
	// 按 domain 升序、再按 host：第一行应为 a.example.com
	var a *assetTableRow
	for i := range rows {
		if rows[i].Host == "a.example.com" {
			a = &rows[i]
		}
	}
	if a == nil {
		t.Fatalf("缺少 a.example.com: %+v", rows)
	}
	if len(a.IPs) != 2 {
		t.Fatalf("关联 IP 数 = %d, want 2: %+v", len(a.IPs), a.IPs)
	}
	if a.RecordType != "A" || a.RecordValue == "" {
		t.Fatalf("解析类型/记录值异常: %+v", a)
	}
	// a.example.com 有 443 与 80 两条资产，asset_count 应为 2
	if a.AssetCount != 2 {
		t.Fatalf("asset_count = %d, want 2", a.AssetCount)
	}
	if a.Domain != "example.com" {
		t.Fatalf("domain = %q", a.Domain)
	}
}

func TestBuildSubdomainRowsFallsBackToAssetIP(t *testing.T) {
	// 没有 host_ips 记录时（手工导入），应退回资产自身 IP，保证记录值不为空。
	rows := buildSubdomainRows(tableTestAssets(), nil)
	for _, row := range rows {
		if row.Host == "b.example.com" {
			if len(row.IPs) != 1 || row.IPs[0] != "1.1.1.1" {
				t.Fatalf("退回 IP 失败: %+v", row.IPs)
			}
			return
		}
	}
	t.Fatalf("缺少 b.example.com")
}

func TestBuildSiteRowsOnlyHTTP(t *testing.T) {
	rows := buildSiteRows(tableTestAssets(), tableTestHostIPs())
	if len(rows) != 3 {
		t.Fatalf("站点行数 = %d, want 3（rdp 不应计入站点）: %+v", len(rows), rows)
	}
	for _, row := range rows {
		if row.Protocol != "http" && row.Protocol != "https" {
			t.Fatalf("站点里混入了非 HTTP 资产: %+v", row)
		}
	}
}

func TestBuildIPRowsAggregation(t *testing.T) {
	rows := buildIPRows(tableTestAssets(), tableTestFingerprints())
	if len(rows) != 2 {
		t.Fatalf("IP 行数 = %d, want 2: %+v", len(rows), rows)
	}
	var shared *assetTableRow
	for i := range rows {
		if rows[i].IP == "1.1.1.1" {
			shared = &rows[i]
		}
	}
	if shared == nil {
		t.Fatalf("缺少 1.1.1.1")
	}
	// 1.1.1.1 关联 a/b 两个域名，3 条资产中 443/80 两个端口
	if shared.AssetCount != 2 {
		t.Fatalf("关联域名数 = %d, want 2", shared.AssetCount)
	}
	if len(shared.Ports) != 2 {
		t.Fatalf("端口数 = %d, want 2: %+v", len(shared.Ports), shared.Ports)
	}
}

func TestBuildServiceRows(t *testing.T) {
	rows := buildServiceRows(tableTestAssets(), tableTestFingerprints())
	if len(rows) != 4 {
		t.Fatalf("服务行数 = %d, want 4: %+v", len(rows), rows)
	}
	var nginx *assetTableRow
	for i := range rows {
		if rows[i].Host == "a.example.com" && rows[i].Port == 443 {
			nginx = &rows[i]
		}
	}
	if nginx == nil {
		t.Fatalf("缺少 a.example.com:443")
	}
	if nginx.Service != "http" || nginx.Version != "1.22.0" {
		t.Fatalf("服务/版本 = %q/%q, want http/1.22.0", nginx.Service, nginx.Version)
	}
}

func TestFilterAssetTableRows(t *testing.T) {
	rows := buildSubdomainRows(tableTestAssets(), tableTestHostIPs())

	if got := filterAssetTableRows(rows, "a.example", "", ""); len(got) != 1 {
		t.Fatalf("关键字过滤 = %d, want 1", len(got))
	}
	if got := filterAssetTableRows(rows, "tool:nmap", "", ""); len(got) != 0 {
		// 子域名 tab 的 source 取第一条资产来源，这里 a/b 都是 httpx，nmap 匹配不到
		t.Fatalf("来源过滤 = %d, want 0", len(got))
	}
	if got := filterAssetTableRows(rows, "", "", "CNAME"); len(got) != 0 {
		t.Fatalf("CNAME 过滤 = %d, want 0（当前只采集 A 记录）", len(got))
	}
	if got := filterAssetTableRows(rows, "", "", "A"); len(got) != 2 {
		t.Fatalf("A 记录过滤 = %d, want 2", len(got))
	}
}

func TestAssetTableFacets(t *testing.T) {
	facets := assetTableFacets(tableTestAssets(), tableTestHostIPs())
	if facets["subdomain"] != 2 {
		t.Fatalf("subdomain facet = %d, want 2", facets["subdomain"])
	}
	if facets["site"] != 3 {
		t.Fatalf("site facet = %d, want 3", facets["site"])
	}
	if facets["ip"] != 2 {
		t.Fatalf("ip facet = %d, want 2", facets["ip"])
	}
	if facets["service"] != 4 {
		t.Fatalf("service facet = %d, want 4", facets["service"])
	}
}
