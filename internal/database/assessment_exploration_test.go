package database

import (
	"encoding/json"
	"testing"
	"time"
)

func TestSharedAssetGraphKeepsIdentityAndPermissionBoundaries(t *testing.T) {
	db := newRBACTestDB(t)
	owner, _ := db.CreateRBACUser("graph-owner", "owner", "hash", true, nil)
	other, _ := db.CreateRBACUser("graph-other", "other", "hash", true, nil)
	a := &Asset{Host: "https://app.example.com", IP: "192.0.2.10", BusinessSystem: "Fixture app"}
	secret := &Asset{Host: "https://secret.example.net"}
	if _, err := db.UpsertAssets([]*Asset{a}, owner.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.UpsertAssets([]*Asset{secret}, other.ID); err != nil {
		t.Fatal(err)
	}
	assessmentSourceFixture(t, db, "a", "A", owner.ID)
	assessmentSourceFixture(t, db, "b", "B", owner.ID)
	for _, task := range []string{"a", "b"} {
		if _, err := db.Exec("INSERT INTO deep_assessment_assets VALUES(?,?)", task, a.ID); err != nil {
			t.Fatal(err)
		}
	}
	access := RBACListAccess{UserID: owner.ID, Scope: RBACScopeOwn}
	ep := &AssetEndpoint{AssetID: a.ID, Method: "GET", Path: "/api/items", Title: "items"}
	if err := db.PutAssetEndpoint(ep, access); err != nil {
		t.Fatal(err)
	}
	firstID := ep.ID
	if err := db.PutAssetEndpoint(ep, access); err != nil || ep.ID != firstID {
		t.Fatal("endpoint not idempotent", err)
	}
	if err := db.PutAssetEndpoint(&AssetEndpoint{AssetID: secret.ID, Method: "GET", Path: "/admin"}, access); err == nil {
		t.Fatal("foreign endpoint write allowed")
	}
	if err := db.PutAssetEndpoint(&AssetEndpoint{AssetID: a.ID, Method: "GET", Path: "/api?token=secret"}, access); err == nil {
		t.Fatal("query values persisted")
	}
	ga, err := db.SharedAssetGraph(AssetListFilter{AssessmentID: "a"}, access, 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	gb, err := db.SharedAssetGraph(AssetListFilter{AssessmentID: "b"}, access, 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	if ga.Total != 1 || gb.Total != 1 {
		t.Fatal(ga, gb)
	}
	kinds := map[string]bool{}
	ids := map[string]bool{}
	for _, n := range ga.Nodes {
		kinds[n.Kind] = true
		ids[n.ID] = true
	}
	for _, n := range gb.Nodes {
		if !ids[n.ID] {
			t.Fatal("shared identity drift", n)
		}
	}
	for _, kind := range []string{"domain", "subdomain", "ip", "service", "application", "endpoint"} {
		if !kinds[kind] {
			t.Fatal("missing kind", kind)
		}
	}
	all, err := db.SharedAssetGraph(AssetListFilter{}, access, 20, 0)
	if err != nil || all.Total != 1 {
		t.Fatal("foreign asset leak", all, err)
	}
	var before, after int
	db.QueryRow("SELECT version FROM deep_assessment_changes WHERE task_id='a'").Scan(&before)
	db.Exec("UPDATE assets SET title='new' WHERE id=?", a.ID)
	db.QueryRow("SELECT version FROM deep_assessment_changes WHERE task_id='a'").Scan(&after)
	if after != before+1 {
		t.Fatal("asset change did not signal", before, after)
	}
	db.Exec("UPDATE assets SET title='new' WHERE id=?", a.ID)
	db.QueryRow("SELECT version FROM deep_assessment_changes WHERE task_id='a'").Scan(&before)
	if before != after {
		t.Fatal("unchanged write caused wake")
	}
}
func TestAssessmentFindingRequiresOriginalReceiptsAndIndependentReview(t *testing.T) {
	db := newRBACTestDB(t)
	original, _ := db.CreateConversation("original", ConversationCreateMeta{})
	reviewer, _ := db.CreateConversation("reviewer", ConversationCreateMeta{})
	assessmentSourceFixture(t, db, "assessment", "Fixture", "", original.ID, reviewer.ID)
	for _, c := range []struct{ id, step, tool string }{{"proof", "one", "fixture_check"}, {"review-proof", "two", "fixture_recheck"}, {"summary", "two", "assessment_facts"}} {
		if _, err := db.Exec("INSERT INTO deep_assessment_calls(id,task_id,step_id,tool,arguments,status,result,updated_at) VALUES(?,'assessment',?,?,'{}','completed','fixture',?)", c.id, c.step, c.tool, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.ValidateAssessmentFinding(original.ID, nil); err == nil {
		t.Fatal("empty evidence accepted")
	}
	if err := db.ValidateAssessmentFinding(original.ID, []string{"review-proof"}); err == nil {
		t.Fatal("foreign intent evidence accepted")
	}
	if err := db.ValidateAssessmentFinding(reviewer.ID, []string{"summary"}); err == nil {
		t.Fatal("summary accepted as original evidence")
	}
	if err := db.ValidateAssessmentFinding(original.ID, []string{"proof"}); err != nil {
		t.Fatal(err)
	}
	v, err := db.CreateVulnerability(&Vulnerability{ConversationID: original.ID, Title: "Fixture finding", Severity: "low", ReproSteps: "read fixture", Evidence: "bytes", EvidenceReceiptIDs: []string{"proof"}})
	if err != nil || len(v.AssessmentReviews) != 1 || v.AssessmentReviews[0].Verdict != "reported" {
		t.Fatal(v, err)
	}
	if err := db.ReviewAssessmentFinding("assessment", "one", v.ID, "confirmed", "self proof", []string{"proof"}); err == nil {
		t.Fatal("self review allowed")
	}
	var raw string
	db.QueryRow("SELECT payload FROM deep_assessments WHERE id='assessment'").Scan(&raw)
	var payload map[string]any
	json.Unmarshal([]byte(raw), &payload)
	for _, st := range payload["steps"].([]any) {
		st.(map[string]any)["status"] = "running"
	}
	b, _ := json.Marshal(payload)
	db.Exec("UPDATE deep_assessments SET payload=? WHERE id='assessment'", string(b))
	if err := db.ReviewAssessmentFinding("assessment", "two", v.ID, "confirmed", "independent fixture reproduction", []string{"proof"}); err == nil {
		t.Fatal("original proof reused")
	}
	if err := db.ReviewAssessmentFinding("assessment", "two", v.ID, "confirmed", "independent fixture reproduction", []string{"review-proof"}, "same-call"); err != nil {
		t.Fatal(err)
	}
	if err := db.ReviewAssessmentFinding("assessment", "two", v.ID, "confirmed", "independent fixture reproduction", []string{"review-proof"}, "same-call"); err != nil {
		t.Fatal(err)
	}
	v, err = db.GetVulnerability(v.ID)
	if err != nil || len(v.AssessmentReviews) != 2 || v.Status != "confirmed" {
		t.Fatal(v, err)
	}
	db.Exec("DELETE FROM deep_assessments WHERE id='assessment'")
	v, err = db.GetVulnerability(v.ID)
	if err != nil || len(v.AssessmentReviews) != 2 || !v.AssessmentDeleted {
		t.Fatal("review provenance lost", v, err)
	}
}
