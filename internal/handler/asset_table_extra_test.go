package handler

import (
	"testing"
	"time"

	"cyberstrike-ai/internal/database"
)

// 「指纹」tab 的聚合逻辑：同一技术/版本跨多个资产合并，并标出是否 web。

func TestBuildFingerprintRowsAggregates(t *testing.T) {
	ts := time.Date(2026, 10, 29, 12, 0, 0, 0, time.Local)
	assets := []*database.Asset{
		{ID: "a1", Host: "a.example.com", Port: 443, Protocol: "https", Source: "tool:httpx", FirstSeenAt: ts, LastSeenAt: ts},
		{ID: "a2", Host: "b.example.com", Port: 443, Protocol: "https", Source: "tool:httpx", FirstSeenAt: ts, LastSeenAt: ts},
		{ID: "a3", Host: "10.0.0.5", IP: "10.0.0.5", Port: 3306, Protocol: "mysql", Source: "tool:nmap", FirstSeenAt: ts, LastSeenAt: ts},
	}
	fingerprints := map[string][]database.AssetFingerprint{
		"a1": {
			{AssetID: "a1", Service: "http", Technology: "nginx", Version: "1.22.0"},
			{AssetID: "a1", Service: "http", Technology: "php", Version: "8.1"},
		},
		"a2": {
			{AssetID: "a2", Service: "http", Technology: "nginx", Version: "1.22.0"},
		},
		"a3": {
			{AssetID: "a3", Service: "mysql", Technology: "mysql", Version: "8.0.32"},
		},
	}

	rows := buildFingerprintRows(assets, fingerprints)
	if len(rows) != 3 {
		t.Fatalf("指纹行数 = %d, want 3: %+v", len(rows), rows)
	}
	byKey := make(map[string]assetTableRow, len(rows))
	for _, row := range rows {
		byKey[row.Technology] = row
	}
	nginx := byKey["nginx"]
	if nginx.AssetCount != 2 {
		t.Fatalf("nginx 关联资产数 = %d, want 2", nginx.AssetCount)
	}
	if !nginx.IsWeb {
		t.Fatalf("nginx 应标记为 web 指纹: %+v", nginx)
	}
	if nginx.Version != "1.22.0" || nginx.Service != "http" {
		t.Fatalf("nginx 行字段异常: %+v", nginx)
	}
	mysql := byKey["mysql"]
	if mysql.IsWeb {
		t.Fatalf("mysql 不应标记为 web: %+v", mysql)
	}
	// 排序：资产数多的在前（nginx 2 条，其余 1 条）
	if rows[0].Technology != "nginx" {
		t.Fatalf("排序异常: %+v", rows)
	}
}

func TestBuildFingerprintRowsSkipsEmpty(t *testing.T) {
	assets := []*database.Asset{{ID: "a1", Protocol: "http"}}
	fingerprints := map[string][]database.AssetFingerprint{
		"a1": {{AssetID: "a1"}}, // 服务与技术都为空，应跳过
	}
	if rows := buildFingerprintRows(assets, fingerprints); len(rows) != 0 {
		t.Fatalf("空指纹不应产生行: %+v", rows)
	}
}
