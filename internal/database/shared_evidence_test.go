package database

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestSharedCollectionCoverageAndRetestFact(t *testing.T) {
	db, err := NewDB(filepath.Join(t.TempDir(), "shared-evidence.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	user, err := db.CreateRBACUser("owner", "Owner", "hash", true, nil)
	if err != nil {
		t.Fatal(err)
	}
	project, err := db.CreateProject(&Project{Name: "shared"})
	if err != nil {
		t.Fatal(err)
	}
	asset := &Asset{ProjectID: project.ID, Domain: "example.com", Protocol: "https", Port: 443, Status: "active"}
	evil := &Asset{ProjectID: project.ID, Host: "https://example.com.evil/a", Status: "active"}
	if result, err := db.UpsertAssets([]*Asset{asset, evil}, user.ID, true); err != nil || result.Created != 2 || asset.ID == "" {
		t.Fatal(result, err)
	}
	otherProject, err := db.CreateProject(&Project{Name: "other"})
	if err != nil {
		t.Fatal(err)
	}
	other := &Asset{ProjectID: otherProject.ID, Domain: "other.example", Status: "active"}
	if _, err = db.UpsertAssets([]*Asset{other}, user.ID, true); err != nil {
		t.Fatal(err)
	}
	collection, err := db.CreateAssetCollection(project.ID, "example", "fofa", "domain=\"example.com\"", user.ID, []string{asset.ID, other.ID, "missing"})
	if err != nil || collection.MemberCount != 1 {
		t.Fatalf("collection=%#v err=%v", collection, err)
	}
	now := time.Now().UTC()
	source, err := db.CreateConversation("source", ConversationCreateMeta{ProjectID: project.ID})
	if err != nil {
		t.Fatal(err)
	}
	retestConv, err := db.CreateConversation("retest", ConversationCreateMeta{ProjectID: project.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO tool_executions(id,tool_name,arguments,status,start_time,conversation_id) VALUES(?,?,?,?,?,?)`,
		"exec-retest", "http_request", "{}", "completed", now, retestConv.ID); err != nil {
		t.Fatal(err)
	}
	vuln, err := db.CreateVulnerability(&Vulnerability{ConversationID: source.ID, ProjectID: project.ID, Title: "login", Severity: "high", Status: "open", Target: "https://example.com/login"})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.UpdateVulnerabilityRetestResult(vuln.ID, "fixed", "patched in this build", retestConv.ID); err != nil {
		t.Fatal(err)
	}
	facts, err := db.ListFactsForAsset(asset.ID, 20)
	if err != nil || len(facts) != 1 || facts[0].Confidence != "tentative" || len(facts[0].ExecutionIDs) != 1 || facts[0].RelatedVulnerabilityID != vuln.ID {
		t.Fatalf("facts=%#v confidence=%q execs=%v vuln=%q err=%v", facts, facts[0].Confidence, facts[0].ExecutionIDs, facts[0].RelatedVulnerabilityID, err)
	}
	if evilFacts, err := db.ListFactsForAsset(evil.ID, 20); err != nil || len(evilFacts) != 0 {
		t.Fatalf("suffix host was linked: %#v %v", evilFacts, err)
	}
	total, observed, err := db.ObservationCoverage(RBACListAccess{Scope: RBACScopeAll}, collection.ID)
	if err != nil || total != 1 || observed != 0 {
		t.Fatalf("unreviewed retest counted as confirmed coverage: total=%d observed=%d err=%v", total, observed, err)
	}
	if _, err = db.SaveProjectFact(&ProjectFact{ProjectID: project.ID, FactKey: "note/reviewed", Summary: "reviewed", Confidence: "confirmed", ConfirmationNote: "人工复核通过", SourceConversationID: retestConv.ID}, []string{asset.ID}, []string{"exec-retest"}, true, true); err != nil {
		t.Fatal(err)
	}
	if _, err = db.SaveProjectFact(&ProjectFact{ProjectID: project.ID, FactKey: "note/reviewed", Summary: "should not replace", Body: "CHANGED", Confidence: "confirmed"}, []string{asset.ID}, []string{"missing-exec"}, true, true); err == nil {
		t.Fatal("invalid receipt replaced the confirmed fact")
	}
	kept, err := db.GetProjectFactByKey(project.ID, "note/reviewed")
	if err != nil || kept.Summary != "reviewed" || strings.Contains(kept.Body, "CHANGED") {
		t.Fatalf("failed save changed the fact: %#v %v", kept, err)
	}
	_, observed, err = db.ObservationCoverage(RBACListAccess{Scope: RBACScopeAll}, collection.ID)
	if err != nil || observed != 1 {
		t.Fatalf("reviewed fact was not counted: %d %v", observed, err)
	}
	stats, err := db.GetAssetStats(RBACListAccess{Scope: RBACScopeAll}, 30)
	if err != nil {
		t.Fatal(err)
	}
	coverage := stats["coverage"].(map[string]interface{})
	if coverage["scanned"] != 0 {
		t.Fatalf("scan coverage changed: %#v", coverage)
	}
	observation := stats["observation"].(map[string]interface{})
	if observation["observed"] != 1 {
		t.Fatalf("observation=%#v", observation)
	}
}

func TestSharedAssetFactsStayInsideReadableProjects(t *testing.T) {
	db, err := NewDB(filepath.Join(t.TempDir(), "shared-boundary.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now().UTC()
	if _, err = db.Exec(`INSERT INTO rbac_roles(id,name,description,scope,is_system,created_at,updated_at) VALUES('role-own','own','','own',0,?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"project:read", "project:write", "chat:read", "asset:read"} {
		if _, err = db.Exec(`INSERT OR IGNORE INTO rbac_permissions(key,description,created_at) VALUES(?,?,?)`, key, key, now); err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(`INSERT OR IGNORE INTO rbac_role_permissions(role_id,permission_key,created_at) VALUES('role-own',?,?)`, key, now); err != nil {
			t.Fatal(err)
		}
	}
	userA, err := db.CreateRBACUser("usera", "A", "hash", true, []string{"role-own"})
	if err != nil {
		t.Fatal(err)
	}
	userB, err := db.CreateRBACUser("userb", "B", "hash", true, []string{"role-own"})
	if err != nil {
		t.Fatal(err)
	}
	projectA, err := db.CreateProject(&Project{Name: "A"})
	if err != nil {
		t.Fatal(err)
	}
	projectB, err := db.CreateProject(&Project{Name: "B"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE projects SET owner_user_id=? WHERE id=?`, userA.ID, projectA.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE projects SET owner_user_id=? WHERE id=?`, userB.ID, projectB.ID); err != nil {
		t.Fatal(err)
	}
	asset := &Asset{Domain: "shared.example", Status: "active"}
	if _, err = db.UpsertAssets([]*Asset{asset}, userA.ID, true); err != nil {
		t.Fatal(err)
	}
	convA, err := db.CreateConversation("a", ConversationCreateMeta{ProjectID: projectA.ID})
	if err != nil {
		t.Fatal(err)
	}
	convB, err := db.CreateConversation("b", ConversationCreateMeta{ProjectID: projectB.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE conversations SET owner_user_id=? WHERE id=?`, userA.ID, convA.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE conversations SET owner_user_id=? WHERE id=?`, userB.ID, convB.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO tool_executions(id,tool_name,arguments,status,start_time,conversation_id) VALUES('exec-a','http','{}','completed',?,?)`, now, convA.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO tool_executions(id,tool_name,arguments,status,start_time,conversation_id) VALUES('exec-b','http','{}','completed',?,?)`, now, convB.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.SaveProjectFact(&ProjectFact{ProjectID: projectA.ID, FactKey: "note/a", Summary: "from A", Confidence: "tentative", SourceConversationID: convA.ID}, []string{asset.ID}, []string{"exec-a"}, true, true); err != nil {
		t.Fatal(err)
	}
	if _, err = db.SaveProjectFact(&ProjectFact{ProjectID: projectB.ID, FactKey: "note/b", Summary: "from B", Confidence: "tentative", SourceConversationID: convB.ID}, []string{asset.ID}, []string{"exec-b"}, true, true); err != nil {
		t.Fatal(err)
	}
	visA, err := db.FactVisibilityFor(userA.ID)
	if err != nil {
		t.Fatal(err)
	}
	visible, err := db.ListVisibleFactsForAsset(asset.ID, visA, 20)
	if err != nil || len(visible) != 1 || visible[0].ProjectID != projectA.ID || visible[0].SourceConversationID != convA.ID || len(visible[0].ExecutionIDs) != 1 {
		t.Fatalf("boundary leak: %#v %v", visible, err)
	}
}
