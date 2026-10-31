package database

import (
	"path/filepath"
	"testing"

	"go.uber.org/zap"
)

// 资产身份必须是「子域名级」的：同一根域下的不同子域名即使端口/协议相同，
// 也应该是不同的资产，否则资产层级视图的「子域名」维度会被合并掉。
//
// 回归背景：assetDedupKey 原先优先用 Domain（eTLD+1），一旦解析器填充了 domain 字段，
// a.example.com:443 与 b.example.com:443 就会算出同一个 dedup_key 而互相覆盖。
func TestUpsertAssetsKeepsSubdomainsSeparate(t *testing.T) {
	db, err := NewDB(filepath.Join(t.TempDir(), "asset-dedup.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	assets := []*Asset{
		{Host: "a.example.com", Domain: "example.com", Port: 443, Protocol: "https"},
		{Host: "b.example.com", Domain: "example.com", Port: 443, Protocol: "https"},
		{Host: "a.example.com", Domain: "example.com", Port: 80, Protocol: "http"},
	}
	result, err := db.UpsertAssets(assets, "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Created != 3 {
		t.Fatalf("created = %d, want 3（子域名/端口不同应各自成条）: %#v", result.Created, result)
	}

	// 同一目标再次上报必须命中已有记录（更新而非新增），保证幂等。
	again := []*Asset{
		{Host: "a.example.com", Domain: "example.com", Port: 443, Protocol: "https", Title: "登录页"},
	}
	result, err = db.UpsertAssets(again, "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Updated != 1 || result.Created != 0 {
		t.Fatalf("重复上报应更新而非新建: %#v", result)
	}

	items, total, err := db.ListAssets(10, 0, AssetListFilter{}, RBACListAccess{Scope: RBACScopeAll})
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(items) != 3 {
		t.Fatalf("资产数 = %d(total)/%d(items), want 3", total, len(items))
	}
	hosts := make(map[string]bool, len(items))
	for _, item := range items {
		hosts[item.Host] = true
	}
	if !hosts["a.example.com"] || !hosts["b.example.com"] {
		t.Fatalf("子域名丢失: %v", hosts)
	}
}
