package database

import (
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestAssetURLNormalizationAndValidation(t *testing.T) {
	db, err := NewDB(filepath.Join(t.TempDir(), "asset-validation.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	asset := &Asset{Host: "https://例子.测试/path", Tags: []string{" prod ", "prod"}}
	result, err := db.UpsertAssets([]*Asset{asset}, "")
	if err != nil || result.Created != 1 {
		t.Fatalf("URL asset was not created: result=%#v err=%v", result, err)
	}
	if asset.Domain != "xn--fsqu00a.xn--0zwm56d" || asset.Protocol != "https" || asset.Port != 443 {
		t.Fatalf("URL fields were not normalized: %#v", asset)
	}
	if len(asset.Tags) != 1 || asset.Tags[0] != "prod" {
		t.Fatalf("tags were not normalized: %#v", asset.Tags)
	}

	invalid := []*Asset{
		{IP: "999.1.1.1", Status: "active"},
		{Domain: "bad_domain.example", Status: "active"},
		{Domain: "example.com", Port: 70000, Status: "active"},
		{Domain: "example.com", Protocol: "HTTP 1.1", Status: "active"},
		{Domain: "example.com", Status: "deleted"},
	}
	for _, candidate := range invalid {
		if _, err := db.UpsertAssets([]*Asset{candidate}, ""); err == nil {
			t.Fatalf("invalid asset unexpectedly accepted: %#v", candidate)
		}
	}

	for _, host := range []string{"123", "not a formal target", "https://", "https://user:password@example.com"} {
		result, err := db.UpsertAssets([]*Asset{{Host: host}}, "")
		if err != nil || result.Created != 1 {
			t.Fatalf("opaque asset address %q was not accepted: result=%#v err=%v", host, result, err)
		}
	}
}

func TestAssetValidationRejectsOversizedTags(t *testing.T) {
	db, err := NewDB(filepath.Join(t.TempDir(), "asset-tag-validation.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.UpsertAssets([]*Asset{{Domain: "example.com", Tags: []string{strings.Repeat("x", 65)}}}, "")
	if err == nil || !strings.Contains(err.Error(), "标签") {
		t.Fatalf("expected tag validation error, got %v", err)
	}
}

func TestFofaAssetIgnoresInvalidOptionalStructuredFields(t *testing.T) {
	db, err := NewDB(filepath.Join(t.TempDir(), "fofa-asset-validation.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	asset := &Asset{
		Host:     "https://203.0.113.59:8443",
		IP:       "203.0.113.59",
		Domain:   "provider_specific_invalid_domain_59",
		Port:     8443,
		Protocol: "https",
		Source:   "fofa",
	}
	result, err := db.UpsertAssets([]*Asset{asset}, "")
	if err != nil || result.Created != 1 {
		t.Fatalf("FOFA asset with dirty optional domain was not created: result=%#v err=%v", result, err)
	}
	if asset.Domain != "" || asset.IP != "203.0.113.59" {
		t.Fatalf("FOFA structured fields were not sanitized: %#v", asset)
	}
}

func TestAssetUpsertDeduplicatesAndUpdates(t *testing.T) {
	db, err := NewDB(filepath.Join(t.TempDir(), "assets.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	first := &Asset{Host: "https://example.com", Domain: "Example.COM", Port: 443, Protocol: "HTTPS", Title: "Old", Source: "fofa"}
	result, err := db.UpsertAssets([]*Asset{first}, "user-a")
	if err != nil || result.Created != 1 || result.Updated != 0 {
		t.Fatalf("first upsert = %#v, %v", result, err)
	}
	second := &Asset{Domain: "example.com", Port: 443, Protocol: "https", Title: "New", Server: "nginx", Source: "fofa"}
	result, err = db.UpsertAssets([]*Asset{second}, "user-a")
	if err != nil || result.Created != 0 || result.Updated != 1 {
		t.Fatalf("second upsert = %#v, %v", result, err)
	}
	assets, total, err := db.ListAssets(20, 0, AssetListFilter{}, RBACListAccess{Scope: RBACScopeAll})
	if err != nil || total != 1 || len(assets) != 1 {
		t.Fatalf("list assets total=%d len=%d err=%v", total, len(assets), err)
	}
	if assets[0].Title != "New" || assets[0].Server != "nginx" || assets[0].Protocol != "https" {
		t.Fatalf("asset not refreshed: %#v", assets[0])
	}
	stats, err := db.GetAssetStats(RBACListAccess{Scope: RBACScopeAll})
	if err != nil || stats["total"] != 1 {
		t.Fatalf("stats=%#v err=%v", stats, err)
	}
	coverage, ok := stats["coverage"].(map[string]interface{})
	if !ok || coverage["never_scanned"] != 1 || coverage["rate"] != 0 {
		t.Fatalf("coverage=%#v", stats["coverage"])
	}
	assetTrend, ok := stats["asset_trend"].([]map[string]interface{})
	if !ok || len(assetTrend) != 30 {
		t.Fatalf("asset trend=%#v", stats["asset_trend"])
	}
	riskTrend, ok := stats["risk_trend"].([]map[string]interface{})
	if !ok || len(riskTrend) != 30 {
		t.Fatalf("risk trend=%#v", stats["risk_trend"])
	}
}

func TestAssetAccessFiltersOwners(t *testing.T) {
	db, err := NewDB(filepath.Join(t.TempDir(), "assets-access.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now()
	if _, err := db.Exec(`INSERT INTO rbac_users (id,username,display_name,password_hash,enabled,is_builtin,created_at,updated_at) VALUES ('user-a','user-a','User A','hash',1,0,?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.UpsertAssets([]*Asset{{IP: "10.0.0.1", Port: 80, Protocol: "http"}}, "user-a"); err != nil {
		t.Fatal(err)
	}
	_, total, err := db.ListAssets(20, 0, AssetListFilter{}, RBACListAccess{UserID: "user-b", Scope: RBACScopeAssigned})
	if err != nil || total != 0 {
		t.Fatalf("unexpected cross-user assets: total=%d err=%v", total, err)
	}
	_, total, err = db.ListAssets(20, 0, AssetListFilter{}, RBACListAccess{UserID: "user-a", Scope: RBACScopeOwn})
	if err != nil || total != 1 {
		t.Fatalf("owner cannot list asset: total=%d err=%v", total, err)
	}
	assets, _, err := db.ListAssets(1, 0, AssetListFilter{}, RBACListAccess{UserID: "user-a", Scope: RBACScopeAssigned})
	if err != nil || len(assets) != 1 || !db.UserCanAccessResource("user-a", RBACScopeAssigned, "asset", assets[0].ID) {
		t.Fatalf("creator assignment missing: assets=%d err=%v", len(assets), err)
	}
	options, err := db.ListAssignableRBACResources("asset", "10.0.0.1", 10)
	if err != nil || len(options) != 1 {
		t.Fatalf("asset resource picker: options=%#v err=%v", options, err)
	}
	project, err := db.CreateProject(&Project{Name: "Alpha", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetResourceOwner("project", project.ID, "user-b"); err != nil {
		t.Fatal(err)
	}
	asset := assets[0]
	asset.ProjectID = project.ID
	if err := db.UpdateAsset(asset.ID, asset, RBACListAccess{Scope: RBACScopeAll}); err != nil {
		t.Fatal(err)
	}
	projectAssets, total, err := db.ListAssets(20, 0, AssetListFilter{ProjectID: project.ID}, RBACListAccess{UserID: "user-b", Scope: RBACScopeOwn})
	if err != nil || total != 1 || len(projectAssets) != 1 || projectAssets[0].ProjectName != "Alpha" {
		t.Fatalf("project-bound asset access failed: total=%d assets=%#v err=%v", total, projectAssets, err)
	}
	if !db.UserCanAccessResource("user-b", RBACScopeOwn, "asset", asset.ID) {
		t.Fatal("project owner cannot access bound asset")
	}
}

func TestUpdateAssetsProjectIsAtomicAndScoped(t *testing.T) {
	db, err := NewDB(filepath.Join(t.TempDir(), "asset-batch-project.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	project, err := db.CreateProject(&Project{Name: "Batch Project", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.UpsertAssets([]*Asset{
		{IP: "192.0.2.1", Port: 80, Protocol: "http"},
		{IP: "192.0.2.2", Port: 443, Protocol: "https"},
	}, "owner-a"); err != nil {
		t.Fatal(err)
	}
	assets, _, err := db.ListAssets(10, 0, AssetListFilter{}, RBACListAccess{Scope: RBACScopeAll})
	if err != nil || len(assets) != 2 {
		t.Fatalf("list assets: len=%d err=%v", len(assets), err)
	}
	ids := []string{assets[0].ID, assets[1].ID}
	updated, err := db.UpdateAssetsProject(ids, project.ID, RBACListAccess{UserID: "owner-a", Scope: RBACScopeOwn})
	if err != nil || updated != 2 {
		t.Fatalf("batch bind: updated=%d err=%v", updated, err)
	}
	for _, id := range ids {
		asset, err := db.GetAsset(id, RBACListAccess{Scope: RBACScopeAll})
		if err != nil || asset.ProjectID != project.ID {
			t.Fatalf("asset %s was not bound: asset=%#v err=%v", id, asset, err)
		}
	}

	if _, err := db.UpdateAssetsProject([]string{ids[0], "missing"}, "", RBACListAccess{Scope: RBACScopeAll}); err == nil {
		t.Fatal("partial batch update unexpectedly succeeded")
	}
	asset, err := db.GetAsset(ids[0], RBACListAccess{Scope: RBACScopeAll})
	if err != nil || asset.ProjectID != project.ID {
		t.Fatalf("failed batch changed an asset: asset=%#v err=%v", asset, err)
	}

	updated, err = db.UpdateAssetsProject(ids, "", RBACListAccess{Scope: RBACScopeAll})
	if err != nil || updated != 2 {
		t.Fatalf("batch unbind: updated=%d err=%v", updated, err)
	}
}

func TestAssetAdvancedFiltersAndBulkMetadata(t *testing.T) {
	db, err := NewDB(filepath.Join(t.TempDir(), "asset-advanced.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	project, err := db.CreateProject(&Project{Name: "Production", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	input := []*Asset{
		{ProjectID: project.ID, Domain: "critical.example.com", Port: 443, Protocol: "https", Country: "CN", ResponsiblePerson: "Alice", Department: "Security", BusinessSystem: "Portal", Environment: "production", Criticality: "critical", Tags: []string{"internet"}},
		{ProjectID: project.ID, Domain: "dev.example.com", Port: 8080, Protocol: "http", Country: "US", Environment: "development", Criticality: "low"},
	}
	if result, err := db.UpsertAssets(input, "", true); err != nil || result.Created != 2 {
		t.Fatalf("create assets: result=%#v err=%v", result, err)
	}
	conversation, err := db.CreateConversation("critical scan", ConversationCreateMeta{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.MarkAssetScanned(input[0].ID, conversation.ID, "", "", RBACListAccess{Scope: RBACScopeAll}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateVulnerability(&Vulnerability{ConversationID: conversation.ID, Title: "critical finding", Severity: "critical", Target: input[0].Domain}); err != nil {
		t.Fatal(err)
	}

	minVulns := 1
	items, total, err := db.ListAssets(20, 0, AssetListFilter{
		Status: "active", RiskLevel: "critical", MinVulnerabilities: &minVulns,
		Country: "cn", Environment: "production", Criticality: "critical",
		SortBy: "vulnerability_count", SortOrder: "desc",
	}, RBACListAccess{Scope: RBACScopeAll})
	if err != nil || total != 1 || len(items) != 1 {
		t.Fatalf("advanced query: total=%d items=%#v err=%v", total, items, err)
	}
	if items[0].ResponsiblePerson != "Alice" || items[0].BusinessSystem != "Portal" || items[0].VulnerabilityCount != 1 {
		t.Fatalf("metadata did not round-trip: %#v", items[0])
	}

	status := "inactive"
	owner := "Bob"
	environment := "staging"
	updated, err := db.UpdateAssetsBulk([]string{input[0].ID, input[1].ID}, AssetBulkPatch{
		Status: &status, ResponsiblePerson: &owner, Environment: &environment,
		AddTags: []string{"review"}, RemoveTags: []string{"internet"},
	}, RBACListAccess{Scope: RBACScopeAll})
	if err != nil || updated != 2 {
		t.Fatalf("bulk update: updated=%d err=%v", updated, err)
	}
	for _, id := range []string{input[0].ID, input[1].ID} {
		item, err := db.GetAsset(id, RBACListAccess{Scope: RBACScopeAll})
		if err != nil {
			t.Fatal(err)
		}
		if item.Status != "inactive" || item.ResponsiblePerson != "Bob" || item.Environment != "staging" || len(item.Tags) != 1 || item.Tags[0] != "review" {
			t.Fatalf("unexpected bulk metadata: %#v", item)
		}
	}
}

func TestListAssetsForOperationAndBatchDelete(t *testing.T) {
	db, err := NewDB(filepath.Join(t.TempDir(), "asset-selection.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for i := 1; i <= 3; i++ {
		if _, err := db.UpsertAssets([]*Asset{{IP: "198.51.100." + strconv.Itoa(i), Port: 443, Protocol: "https", Tags: []string{"selected"}}}, "", true); err != nil {
			t.Fatal(err)
		}
	}
	items, total, err := db.ListAssetsForOperation(10, AssetListFilter{Tag: "selected"}, RBACListAccess{Scope: RBACScopeAll})
	if err != nil || total != 3 || len(items) != 3 {
		t.Fatalf("selection: total=%d len=%d err=%v", total, len(items), err)
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	deleted, err := db.DeleteAssets(ids, RBACListAccess{Scope: RBACScopeAll})
	if err != nil || deleted != 3 {
		t.Fatalf("batch delete: deleted=%d err=%v", deleted, err)
	}
}

func TestMergeAssetsIsAtomic(t *testing.T) {
	db, err := NewDB(filepath.Join(t.TempDir(), "asset-merge.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	input := []*Asset{
		{Domain: "merge.example.com", Port: 80, Protocol: "http", Title: "Primary", Tags: []string{"one"}},
		{Domain: "merge.example.com", Port: 443, Protocol: "https", ResponsiblePerson: "Alice", Tags: []string{"two"}},
	}
	if _, err := db.UpsertAssets(input, "", true); err != nil {
		t.Fatal(err)
	}
	primary, err := db.GetAsset(input[0].ID, RBACListAccess{Scope: RBACScopeAll})
	if err != nil {
		t.Fatal(err)
	}
	primary.ResponsiblePerson = "Alice"
	primary.Tags = []string{"one", "two"}
	merged, err := db.MergeAssets(primary, []string{input[1].ID}, RBACListAccess{Scope: RBACScopeAll}, RBACListAccess{Scope: RBACScopeAll})
	if err != nil || merged != 1 {
		t.Fatalf("merge: merged=%d err=%v", merged, err)
	}
	items, total, err := db.ListAssets(10, 0, AssetListFilter{}, RBACListAccess{Scope: RBACScopeAll})
	if err != nil || total != 1 || len(items) != 1 || items[0].ResponsiblePerson != "Alice" || len(items[0].Tags) != 2 {
		t.Fatalf("unexpected merged asset: total=%d items=%#v err=%v", total, items, err)
	}

	before := items[0].Title
	items[0].Title = "Must roll back"
	if _, err := db.MergeAssets(items[0], []string{"missing"}, RBACListAccess{Scope: RBACScopeAll}, RBACListAccess{Scope: RBACScopeAll}); err == nil {
		t.Fatal("merge with missing duplicate unexpectedly succeeded")
	}
	after, err := db.GetAsset(items[0].ID, RBACListAccess{Scope: RBACScopeAll})
	if err != nil || after.Title != before {
		t.Fatalf("failed merge was not atomic: asset=%#v err=%v", after, err)
	}
}

func TestAssetScanLinkReturnsTimeAndRelatedVulnerabilities(t *testing.T) {
	db, err := NewDB(filepath.Join(t.TempDir(), "asset-scan.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.UpsertAssets([]*Asset{{IP: "192.0.2.10", Port: 443, Protocol: "https"}}, ""); err != nil {
		t.Fatal(err)
	}
	assets, _, err := db.ListAssets(10, 0, AssetListFilter{}, RBACListAccess{Scope: RBACScopeAll})
	if err != nil || len(assets) != 1 {
		t.Fatalf("list assets: len=%d err=%v", len(assets), err)
	}
	conv, err := db.CreateConversation("asset scan", ConversationCreateMeta{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.MarkAssetScanned(assets[0].ID, conv.ID, "", "", RBACListAccess{Scope: RBACScopeAll}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateVulnerability(&Vulnerability{ConversationID: conv.ID, Title: "finding", Severity: "high", Target: "192.0.2.10"}); err != nil {
		t.Fatal(err)
	}
	linked, err := db.GetAsset(assets[0].ID, RBACListAccess{Scope: RBACScopeAll})
	if err != nil {
		t.Fatal(err)
	}
	if linked.LastScanAt == nil || linked.LastScanConversationID != conv.ID || linked.VulnerabilityCount != 1 || linked.RiskLevel != "high" {
		t.Fatalf("unexpected scan metadata: %#v", linked)
	}
	vulns, err := db.ListVulnerabilities(10, 0, VulnerabilityListFilter{ConversationID: conv.ID})
	if err != nil || len(vulns) != 1 {
		t.Fatalf("list linked vulnerabilities: len=%d err=%v", len(vulns), err)
	}
	vulns[0].Status = "fixed"
	if err := db.UpdateVulnerability(vulns[0].ID, vulns[0]); err != nil {
		t.Fatal(err)
	}
	resolved, err := db.GetAsset(assets[0].ID, RBACListAccess{Scope: RBACScopeAll})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.VulnerabilityCount != 1 || resolved.RiskLevel != "normal" {
		t.Fatalf("resolved finding should remain in history without raising current risk: %#v", resolved)
	}
}

func TestAssetListFlexibleFiltersAndOldestScanPagination(t *testing.T) {
	db, err := NewDB(filepath.Join(t.TempDir(), "asset-query.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	assets := []*Asset{
		{IP: "192.0.2.1", Port: 443, Protocol: "https", Source: "fofa", Tags: []string{"prod"}},
		{IP: "192.0.2.2", Port: 80, Protocol: "http", Source: "manual", Tags: []string{"prod", "legacy"}},
		{Domain: "never.example.com", Port: 443, Protocol: "https", Source: "manual", Tags: []string{"prod"}},
	}
	if _, err := db.UpsertAssets(assets, ""); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-90 * 24 * time.Hour).UTC()
	recent := time.Now().Add(-24 * time.Hour).UTC()
	if _, err := db.Exec(`UPDATE assets SET last_scan_at=? WHERE id=?`, old, assets[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE assets SET last_scan_at=? WHERE id=?`, recent, assets[1].ID); err != nil {
		t.Fatal(err)
	}

	access := RBACListAccess{Scope: RBACScopeAll}
	firstPage, total, err := db.ListAssets(2, 0, AssetListFilter{Tag: "prod", SortBy: "last_scan_at", SortOrder: "asc"}, access)
	if err != nil || total != 3 || len(firstPage) != 2 {
		t.Fatalf("oldest scan page: total=%d len=%d err=%v", total, len(firstPage), err)
	}
	if firstPage[0].ID != assets[2].ID || firstPage[0].LastScanAt != nil || firstPage[1].ID != assets[0].ID {
		t.Fatalf("expected never-scanned then oldest scanned asset, got %#v", firstPage)
	}
	secondPage, _, err := db.ListAssets(2, 2, AssetListFilter{Tag: "prod", SortBy: "last_scan_at", SortOrder: "asc"}, access)
	if err != nil || len(secondPage) != 1 || secondPage[0].ID != assets[1].ID {
		t.Fatalf("unexpected second page: %#v err=%v", secondPage, err)
	}

	never, total, err := db.ListAssets(20, 0, AssetListFilter{ScanState: "never"}, access)
	if err != nil || total != 1 || len(never) != 1 || never[0].ID != assets[2].ID {
		t.Fatalf("never-scanned filter: total=%d assets=%#v err=%v", total, never, err)
	}
	port := 443
	filtered, total, err := db.ListAssets(20, 0, AssetListFilter{Source: "fofa", Port: &port, LastScanBefore: &recent}, access)
	if err != nil || total != 1 || len(filtered) != 1 || filtered[0].ID != assets[0].ID {
		t.Fatalf("structured filters: total=%d assets=%#v err=%v", total, filtered, err)
	}
}

func TestUpsertAssetsConcurrentCreatesDoNotLock(t *testing.T) {
	db, err := NewDB(filepath.Join(t.TempDir(), "assets-concurrent.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	const goroutines = 24
	errs := make([]error, goroutines)
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			asset := &Asset{Host: "https://198.51.100." + strconv.Itoa(i+1) + ":443", Source: "concurrent"}
			_, errs[i] = db.UpsertAssets([]*Asset{asset}, "")
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent upsert %d failed: %v", i, err)
		}
	}
	_, total, err := db.ListAssets(100, 0, AssetListFilter{Source: "concurrent"}, RBACListAccess{Scope: RBACScopeAll})
	if err != nil {
		t.Fatalf("list assets: %v", err)
	}
	if total != goroutines {
		t.Fatalf("total=%d, want %d", total, goroutines)
	}
}
