package database

import (
	"path/filepath"
	"testing"

	"go.uber.org/zap"
)

func newCompanyTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := NewDB(filepath.Join(t.TempDir(), "company.db"), zap.NewNop())
	if err != nil {
		t.Fatalf("创建测试库失败: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestCompanyCRUDAndDomainOwnership(t *testing.T) {
	db := newCompanyTestDB(t)

	company, err := db.CreateCompany("示例集团", "备注", "user-1")
	if err != nil {
		t.Fatalf("创建公司失败: %v", err)
	}
	// 重名（大小写不敏感）必须被拒绝
	if _, err := db.CreateCompany("示例集团", "", ""); err == nil {
		t.Fatal("重名公司应被拒绝")
	}
	if _, err := db.CreateCompany("  ", "", ""); err == nil {
		t.Fatal("空名称应被拒绝")
	}

	if _, err := db.AddCompanyDomain(company.ID, "Example.COM.", "manual", ""); err != nil {
		t.Fatalf("归属域名失败: %v", err)
	}
	// 域名归一化：大小写与末尾点应被清理
	if item, _ := db.GetCompanyDomainByDomain("example.com"); item == nil {
		t.Fatal("域名应已归一化并可查询")
	}

	// 同一域名不能再归属到第二家公司
	other, _ := db.CreateCompany("另一家公司", "", "")
	if _, err := db.AddCompanyDomain(other.ID, "example.com", "manual", ""); err == nil {
		t.Fatal("已确认归属的域名不应被第二家公司抢走")
	}

	// 删除公司后，域名回落为「待归属」
	if err := db.DeleteCompany(company.ID); err != nil {
		t.Fatalf("删除公司失败: %v", err)
	}
	item, err := db.GetCompanyDomainByDomain("example.com")
	if err != nil || item == nil {
		t.Fatalf("域名记录应保留: %v", err)
	}
	if item.CompanyID != "" || item.Status != CompanyDomainStatusPending {
		t.Fatalf("删除公司后域名应为待归属，实际: %+v", item)
	}
}

func TestUpsertPendingDomainsDoesNotOverrideUserDecision(t *testing.T) {
	db := newCompanyTestDB(t)
	company, _ := db.CreateCompany("集团A", "", "")

	// 自动采集登记 3 个待确认域名
	inserted, err := db.UpsertPendingDomains([]string{"a.com", "b.com", "A.COM."}, "auto:httpx")
	if err != nil {
		t.Fatalf("登记待确认域名失败: %v", err)
	}
	if inserted != 2 {
		t.Fatalf("入库条数 = %d, want 2（去重 + 归一化）", inserted)
	}

	// 用户把 a.com 确认到集团A，把 b.com 忽略
	pending, err := db.ListCompanyDomains(CompanyDomainStatusPending)
	if err != nil || len(pending) != 2 {
		t.Fatalf("待确认列表 = %d, want 2: %v", len(pending), err)
	}
	for _, item := range pending {
		switch item.Domain {
		case "a.com":
			if err := db.SetCompanyDomainStatus(item.ID, company.ID, CompanyDomainStatusConfirmed); err != nil {
				t.Fatalf("确认失败: %v", err)
			}
		case "b.com":
			if err := db.SetCompanyDomainStatus(item.ID, "", CompanyDomainStatusIgnored); err != nil {
				t.Fatalf("忽略失败: %v", err)
			}
		}
	}

	// 再次自动采集：不能把用户已确认/已忽略的状态覆盖回 pending
	if _, err := db.UpsertPendingDomains([]string{"a.com", "b.com"}, "auto:naabu"); err != nil {
		t.Fatalf("二次登记失败: %v", err)
	}
	if item, _ := db.GetCompanyDomainByDomain("a.com"); item.Status != CompanyDomainStatusConfirmed || item.CompanyID != company.ID {
		t.Fatalf("已确认域名被覆盖: %+v", item)
	}
	if item, _ := db.GetCompanyDomainByDomain("b.com"); item.Status != CompanyDomainStatusIgnored {
		t.Fatalf("已忽略域名被覆盖: %+v", item)
	}
}

func TestDomainCompanyMapAndAssetCounts(t *testing.T) {
	db := newCompanyTestDB(t)
	company, _ := db.CreateCompany("集团B", "", "")
	if _, err := db.AddCompanyDomain(company.ID, "example.com", "manual", ""); err != nil {
		t.Fatal(err)
	}

	// 两条同域资产
	for _, host := range []string{"a.example.com", "b.example.com"} {
		if _, err := db.UpsertAssets([]*Asset{{Host: host, Domain: "example.com", Port: 443, Protocol: "https"}}, ""); err != nil {
			t.Fatal(err)
		}
	}

	mapping, err := db.DomainCompanyMap()
	if err != nil {
		t.Fatalf("查询域名归属映射失败: %v", err)
	}
	if mapping["example.com"].Name != "集团B" {
		t.Fatalf("映射结果异常: %+v", mapping)
	}

	counts, err := db.AssetCountsByDomain([]string{"example.com", "unknown.com"})
	if err != nil {
		t.Fatalf("统计资产数失败: %v", err)
	}
	if counts["example.com"] != 2 {
		t.Fatalf("example.com 资产数 = %d, want 2", counts["example.com"])
	}
	if counts["unknown.com"] != 0 {
		t.Fatalf("unknown.com 资产数 = %d, want 0", counts["unknown.com"])
	}

	companies, err := db.ListCompanies()
	if err != nil || len(companies) != 1 {
		t.Fatalf("公司列表 = %d, want 1: %v", len(companies), err)
	}
	if companies[0].AssetCount != 2 {
		t.Fatalf("公司资产数 = %d, want 2", companies[0].AssetCount)
	}
}

func TestLocalDomainCandidates(t *testing.T) {
	db := newCompanyTestDB(t)
	project, err := db.CreateProject(&Project{Name: "示例集团", Status: "active"})
	if err != nil {
		t.Fatalf("创建项目失败: %v", err)
	}
	otherProject, err := db.CreateProject(&Project{Name: "无关项目", Status: "active"})
	if err != nil {
		t.Fatalf("创建项目失败: %v", err)
	}
	for _, host := range []string{"a.example.com", "b.example.com"} {
		asset := &Asset{Host: host, Domain: rootOf(host), Port: 80, Protocol: "http", ProjectID: project.ID}
		if _, err := db.UpsertAssets([]*Asset{asset}, ""); err != nil {
			t.Fatal(err)
		}
	}
	// 另一个项目下的域名不应成为「示例集团」的候选
	if _, err := db.UpsertAssets([]*Asset{{
		Host: "x.other.com", Domain: "other.com", Port: 80, Protocol: "http", ProjectID: otherProject.ID,
	}}, ""); err != nil {
		t.Fatal(err)
	}
	candidates, err := db.LocalDomainCandidates("示例集团", 10)
	if err != nil {
		t.Fatalf("本地候选查询失败: %v", err)
	}
	if candidates["example.com"] != 2 {
		t.Fatalf("example.com 候选计数 = %d, want 2: %+v", candidates["example.com"], candidates)
	}
	if _, ok := candidates["other.com"]; ok {
		t.Fatalf("不相关域名不应出现: %+v", candidates)
	}
}

func rootOf(host string) string {
	if idx := indexByte(host, '.'); idx >= 0 {
		return host[idx+1:]
	}
	return host
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}
