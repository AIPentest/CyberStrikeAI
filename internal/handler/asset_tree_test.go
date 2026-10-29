package handler

import (
	"testing"

	"cyberstrike-ai/internal/database"
)

// 这些测试只覆盖 buildAssetTree 的纯逻辑（不碰数据库），
// 用来固定「层级聚合 + 去重 + 兜底文案」的契约，避免后续改动悄悄改变前端拿到的结构。

func TestBuildAssetTreeHierarchy(t *testing.T) {
	assets := []*database.Asset{
		// 项目 A：example.com 下两个子域名，其中一个带两个指纹
		{ID: "a1", ProjectID: "p1", ProjectName: "项目A", Host: "a.example.com", IP: "1.1.1.1", Port: 443, Domain: "example.com", Protocol: "https", Server: "nginx"},
		{ID: "a2", ProjectID: "p1", ProjectName: "项目A", Host: "a.example.com", IP: "1.1.1.1", Port: 80, Domain: "example.com", Protocol: "http"},
		{ID: "a3", ProjectID: "p1", ProjectName: "项目A", Host: "b.example.com", IP: "2.2.2.2", Port: 22, Domain: "example.com", Protocol: "ssh"},
	}
	fingerprints := map[string][]database.AssetFingerprint{
		"a1": {
			{AssetID: "a1", Service: "http", Technology: "nginx", Version: "1.22.0"},
			{AssetID: "a1", Service: "http", Technology: "php", Version: "8.1"},
		},
	}

	resp := buildAssetTree(assets, fingerprints, nil)

	if resp.TotalAssets != 3 {
		t.Fatalf("total_assets = %d, want 3", resp.TotalAssets)
	}
	if len(resp.Companies) != 1 {
		t.Fatalf("companies = %d, want 1", len(resp.Companies))
	}
	company := resp.Companies[0]
	if company.CompanyName != "项目A" || company.AssetCount != 3 {
		t.Fatalf("company = %+v", company)
	}
	if len(company.Domains) != 1 || company.Domains[0].Domain != "example.com" {
		t.Fatalf("domains = %+v", company.Domains)
	}
	domain := company.Domains[0]
	if domain.AssetCount != 3 || len(domain.Subdomains) != 2 {
		t.Fatalf("domain = %+v", domain)
	}

	// 子域名按资产数降序：a.example.com(2) 在 b.example.com(1) 前。
	sub := domain.Subdomains[0]
	if sub.Host != "a.example.com" || sub.AssetCount != 2 {
		t.Fatalf("sub = %+v", sub)
	}
	if len(sub.IPs) != 1 || sub.IPs[0].IP != "1.1.1.1" {
		t.Fatalf("ips = %+v", sub.IPs)
	}
	// 端口按数值升序：80 在 443 前。
	ports := sub.IPs[0].Ports
	if len(ports) != 2 {
		t.Fatalf("ports = %+v", ports)
	}
	if ports[0].Port != 80 || ports[1].Port != 443 {
		t.Fatalf("port order = %d,%d", ports[0].Port, ports[1].Port)
	}
	// 443 端口应从指纹表拿到 service 与两个技术指纹。
	httpsPort := ports[1]
	if httpsPort.Service != "http" {
		t.Fatalf("service = %q, want http", httpsPort.Service)
	}
	if len(httpsPort.Technologies) != 2 {
		t.Fatalf("technologies = %+v", httpsPort.Technologies)
	}
	if len(httpsPort.AssetIDs) != 1 || httpsPort.AssetIDs[0] != "a1" {
		t.Fatalf("asset_ids = %+v", httpsPort.AssetIDs)
	}
	// 80 端口没有指纹，service 应回退为协议名。
	if ports[0].Service != "http" {
		t.Fatalf("fallback service = %q, want http", ports[0].Service)
	}
}

func TestBuildAssetTreeIPOnlyAndUnboundProject(t *testing.T) {
	assets := []*database.Asset{
		// 纯 IP 资产、未绑定项目：company 空、domain 空、subdomain 空，应逐层落到 IP。
		{ID: "i1", Host: "9.9.9.9", IP: "9.9.9.9", Port: 3389, Protocol: "rdp"},
	}
	resp := buildAssetTree(assets, nil, nil)

	if len(resp.Companies) != 1 {
		t.Fatalf("companies = %d, want 1", len(resp.Companies))
	}
	company := resp.Companies[0]
	if company.CompanyID != "" || company.CompanyName != "未绑定项目" {
		t.Fatalf("company = %+v", company)
	}
	if len(company.Domains) != 1 || company.Domains[0].Domain != "" {
		t.Fatalf("domains = %+v", company.Domains)
	}
	subs := company.Domains[0].Subdomains
	if len(subs) != 1 || subs[0].Host != "" {
		t.Fatalf("subdomains = %+v", subs)
	}
	ips := subs[0].IPs
	if len(ips) != 1 || ips[0].IP != "9.9.9.9" {
		t.Fatalf("ips = %+v", ips)
	}
	ports := ips[0].Ports
	if len(ports) != 1 || ports[0].Port != 3389 {
		t.Fatalf("ports = %+v", ports)
	}
	if ports[0].Service != "rdp" {
		t.Fatalf("service = %q, want rdp", ports[0].Service)
	}
}

func TestBuildAssetTreeMergesSamePort(t *testing.T) {
	// 同一 host/ip/port 被两条资产记录覆盖（例如不同来源重复上报），
	// 端口节点必须合并，asset_ids 去重。
	assets := []*database.Asset{
		{ID: "d1", Host: "x.example.com", IP: "3.3.3.3", Port: 8080, Domain: "example.com", Protocol: "http", Server: "tomcat"},
		{ID: "d2", Host: "x.example.com", IP: "3.3.3.3", Port: 8080, Domain: "example.com", Protocol: "http"},
	}
	resp := buildAssetTree(assets, nil, nil)
	ports := resp.Companies[0].Domains[0].Subdomains[0].IPs[0].Ports
	if len(ports) != 1 {
		t.Fatalf("ports = %+v, want 1 merged node", ports)
	}
	if len(ports[0].AssetIDs) != 2 {
		t.Fatalf("asset_ids = %+v, want 2", ports[0].AssetIDs)
	}
	// 无指纹时用 Server 兜底成一条技术指纹。
	if len(ports[0].Technologies) != 1 || ports[0].Technologies[0].Name != "tomcat" {
		t.Fatalf("technologies = %+v", ports[0].Technologies)
	}
}

func TestBuildAssetTreeEmpty(t *testing.T) {
	resp := buildAssetTree(nil, nil, nil)
	if resp.TotalAssets != 0 || len(resp.Companies) != 0 {
		t.Fatalf("resp = %+v", resp)
	}
}
