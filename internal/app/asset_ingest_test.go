package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"cyberstrike-ai/internal/authctx"
	"cyberstrike-ai/internal/database"

	"go.uber.org/zap"
)

// 集成测试：工具输出 -> 清洗 -> 资产入库 -> 指纹落库。
// 这是「对话里跑扫描，资产页就能看到」这条链路的端到端验证。

func newIngestTestDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := database.NewDB(filepath.Join(t.TempDir(), "asset-ingest.db"), zap.NewNop())
	if err != nil {
		t.Fatalf("创建测试数据库失败: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func ingestTestContext() context.Context {
	principal := authctx.NewPrincipal("admin", "admin", database.RBACScopeAll, map[string]bool{
		"asset:read": true, "asset:write": true,
	})
	return authctx.WithPrincipal(context.Background(), principal)
}

func listIngestedAssets(t *testing.T, db *database.DB) []*database.Asset {
	t.Helper()
	access := database.RBACListAccess{UserID: "admin", Scope: database.RBACScopeAll}
	items, _, err := db.ListAssets(200, 0, database.AssetListFilter{}, access)
	if err != nil {
		t.Fatalf("查询资产失败: %v", err)
	}
	return items
}

func TestAssetIngestorFromHttpxJSON(t *testing.T) {
	db := newIngestTestDB(t)
	ingestor := newAssetIngestor(db, zap.NewNop())
	ctx := ingestTestContext()

	// 两行 httpx -json 输出 + 一行日志噪音。
	output := `{"url":"https://a.example.com","host":"a.example.com","port":"443","scheme":"https","input":"a.example.com","title":"Login Page","webserver":"nginx","tech":["nginx:1.22.0","PHP:8.1"]}
{"url":"http://b.example.com","host":"b.example.com","port":"80","scheme":"http","input":"b.example.com","title":"B Site","webserver":"Apache"}
[INF] some log noise that must be ignored
`

	ingestor.Ingest(ctx, "httpx", nil, output)

	items := listIngestedAssets(t, db)
	if len(items) != 2 {
		t.Fatalf("入库资产数 = %d, want 2", len(items))
	}

	byHost := make(map[string]*database.Asset, len(items))
	for _, item := range items {
		byHost[item.Host] = item
		if item.Source != "tool:httpx" {
			t.Errorf("source = %q, want tool:httpx", item.Source)
		}
		if item.Domain != "example.com" {
			t.Errorf("%s domain = %q, want example.com", item.Host, item.Domain)
		}
	}

	a := byHost["a.example.com"]
	if a == nil {
		t.Fatalf("未找到 a.example.com，实际: %v", keysOf(byHost))
	}
	if a.Port != 443 || a.Protocol != "https" {
		t.Errorf("a.example.com port/protocol = %d/%q, want 443/https", a.Port, a.Protocol)
	}
	if a.Title != "Login Page" {
		t.Errorf("title = %q", a.Title)
	}

	// 指纹：a 应有 nginx 1.22.0 与 php 8.1（大小写不敏感，按技术名小写存）。
	fps, err := db.ListAssetFingerprints([]string{a.ID})
	if err != nil {
		t.Fatalf("查询指纹失败: %v", err)
	}
	if len(fps[a.ID]) < 2 {
		t.Fatalf("a.example.com 指纹数 = %d, want >=2: %+v", len(fps[a.ID]), fps[a.ID])
	}
	versions := make(map[string]string, len(fps[a.ID]))
	for _, fp := range fps[a.ID] {
		if fp.Technology != "" {
			versions[fp.Technology] = fp.Version
		}
	}
	if versions["nginx"] != "1.22.0" {
		t.Errorf("nginx 版本 = %q, want 1.22.0（实际: %+v）", versions["nginx"], versions)
	}
}

func TestAssetIngestorIsIdempotent(t *testing.T) {
	db := newIngestTestDB(t)
	ingestor := newAssetIngestor(db, zap.NewNop())
	ctx := ingestTestContext()

	output := `{"url":"https://c.example.com","host":"c.example.com","port":"443","scheme":"https","title":"C","webserver":"nginx","tech":["nginx:1.22.0"]}`

	ingestor.Ingest(ctx, "httpx", nil, output)
	ingestor.Ingest(ctx, "httpx", nil, output)

	items := listIngestedAssets(t, db)
	if len(items) != 1 {
		t.Fatalf("重复采集后资产数 = %d, want 1（去重失效）", len(items))
	}
	fps, err := db.ListAssetFingerprints([]string{items[0].ID})
	if err != nil {
		t.Fatalf("查询指纹失败: %v", err)
	}
	if len(fps[items[0].ID]) != 1 {
		t.Fatalf("重复采集后指纹数 = %d, want 1（幂等失效）", len(fps[items[0].ID]))
	}
}

func TestAssetIngestorFromNmapXML(t *testing.T) {
	db := newIngestTestDB(t)
	ingestor := newAssetIngestor(db, zap.NewNop())
	ctx := ingestTestContext()

	output := `<?xml version="1.0" encoding="UTF-8"?>
<nmaprun scanner="nmap">
<host><status state="up"/>
<address addr="10.0.0.5" addrtype="ipv4"/>
<hostnames><hostname name="db.example.com" type="user"/></hostnames>
<ports>
<port protocol="tcp" portid="3306"><state state="open"/><service name="mysql" product="MySQL" version="8.0.32"/></port>
<port protocol="tcp" portid="22"><state state="open"/><service name="ssh" product="OpenSSH" version="9.0"/></port>
<port protocol="tcp" portid="9999"><state state="closed"/><service name="unknown"/></port>
</ports></host>
</nmaprun>`

	ingestor.Ingest(ctx, "nmap", nil, output)

	items := listIngestedAssets(t, db)
	if len(items) != 2 {
		t.Fatalf("开放端口数 = %d, want 2（closed 端口不应入库）: %+v", len(items), items)
	}
	ports := make(map[int]*database.Asset, len(items))
	for _, item := range items {
		ports[item.Port] = item
		if item.IP != "10.0.0.5" {
			t.Errorf("ip = %q, want 10.0.0.5", item.IP)
		}
	}
	mysql := ports[3306]
	if mysql == nil {
		t.Fatalf("未找到 3306 端口，实际: %v", keysOfPort(ports))
	}
	fps, err := db.ListAssetFingerprints([]string{mysql.ID})
	if err != nil {
		t.Fatalf("查询指纹失败: %v", err)
	}
	if len(fps[mysql.ID]) == 0 {
		t.Fatalf("3306 应有服务/版本指纹")
	}
	found := false
	for _, fp := range fps[mysql.ID] {
		if fp.Service == "mysql" && fp.Version == "8.0.32" {
			found = true
		}
	}
	if !found {
		t.Fatalf("未找到 mysql/8.0.32 指纹: %+v", fps[mysql.ID])
	}
}

// 第二批工具：fscan 的端口/Web/漏洞混合输出。
func TestAssetIngestorFromFscan(t *testing.T) {
	db := newIngestTestDB(t)
	ingestor := newAssetIngestor(db, zap.NewNop())
	ctx := ingestTestContext()

	output := "[*] 1.2.3.4:80 open\n" +
		"[+] 1.2.3.4:445 open ms17-010\n" +
		"[+] http://1.2.3.4 200\n" +
		"[+] 1.2.3.5 MS17-010\n"

	ingestor.Ingest(ctx, "fscan", nil, output)

	items := listIngestedAssets(t, db)
	// 1.2.3.4:80 与 1.2.3.4:445 协议不同，属于两条资产；1.2.3.5 只有主机（端口 0）。
	if len(items) != 3 {
		t.Fatalf("fscan 入库资产数 = %d, want 3: %+v", len(items), items)
	}
	byPort := make(map[int]*database.Asset, len(items))
	for _, item := range items {
		byPort[item.Port] = item
		if item.Source != "tool:fscan" {
			t.Errorf("source = %q, want tool:fscan", item.Source)
		}
	}
	if byPort[80] == nil || byPort[445] == nil {
		t.Fatalf("缺少 80/445 端口资产: %v", keysOfPort(byPort))
	}
	// 445 端口应被 fscan 解析器识别为 smb 服务并落成指纹。
	fps, err := db.ListAssetFingerprints([]string{byPort[445].ID})
	if err != nil {
		t.Fatalf("查询指纹失败: %v", err)
	}
	found := false
	for _, fp := range fps[byPort[445].ID] {
		if fp.Service == "smb" {
			found = true
		}
	}
	if !found {
		t.Fatalf("445 端口未记录 smb 服务指纹: %+v", fps[byPort[445].ID])
	}
}

// 第二批工具：katana 抓到的 URL 只保留 host/port/protocol，且同一主机多 URL 合并。
func TestAssetIngestorFromKatanaURLs(t *testing.T) {
	db := newIngestTestDB(t)
	ingestor := newAssetIngestor(db, zap.NewNop())
	ctx := ingestTestContext()

	output := "http://shop.example.com/\n" +
		"https://shop.example.com:8443/login?next=/admin\n" +
		"https://shop.example.com/login\n" +
		"https://shop.example.com/login?user=1\n" +
		"http://api.example.com/v1/users?id=1\n" +
		"[INF] crawled 5 URLs\n"

	ingestor.Ingest(ctx, "katana", nil, output)

	items := listIngestedAssets(t, db)
	// shop.example.com 的 https:8443 与 https:443 是不同端口，各算一条；http 无端口按 80 补。
	if len(items) != 4 {
		t.Fatalf("katana 入库资产数 = %d, want 4: %+v", len(items), items)
	}
	portsByHost := make(map[string]map[int]bool)
	for _, item := range items {
		if strings.ContainsAny(item.Host, "/?") || strings.Contains(item.Host, "://") {
			t.Errorf("host 里混入了路径/协议: %q", item.Host)
		}
		if item.Domain != "example.com" {
			t.Errorf("domain = %q, want example.com", item.Domain)
		}
		if portsByHost[item.Host] == nil {
			portsByHost[item.Host] = make(map[int]bool)
		}
		portsByHost[item.Host][item.Port] = true
	}
	for _, want := range []int{80, 443, 8443} {
		if !portsByHost["shop.example.com"][want] {
			t.Errorf("shop.example.com 缺少端口 %d，实际: %v", want, portsByHost["shop.example.com"])
		}
	}
	if !portsByHost["api.example.com"][80] {
		t.Errorf("api.example.com 应补默认端口 80，实际: %v", portsByHost["api.example.com"])
	}
}

func keysOf(m map[string]*database.Asset) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func keysOfPort(m map[int]*database.Asset) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
