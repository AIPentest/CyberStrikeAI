package handler

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"cyberstrike-ai/internal/agentruntime"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/deepassessment"
	"github.com/gin-gonic/gin"
)

func TestDeepAssessmentEvidencePathsHistoryAndEndpointGaps(t *testing.T) {
	h, task := deepAssessmentFixture(t)
	store := h.deepAssessments.Store
	other, err := h.db.CreateRBACUser("evidence-other", "Other", "hash", true, nil)
	if err != nil {
		t.Fatal(err)
	}
	asset := &database.Asset{Host: "https://review.example.test"}
	private := &database.Asset{Host: "https://private.example.test"}
	if _, err = h.db.UpsertAssets([]*database.Asset{asset}, task.OwnerID); err != nil {
		t.Fatal(err)
	}
	if _, err = h.db.UpsertAssets([]*database.Asset{private}, other.ID); err != nil {
		t.Fatal(err)
	}
	allAssets := database.RBACListAccess{UserID: task.OwnerID, Scope: database.RBACScopeAll}
	endpoints := []*database.AssetEndpoint{
		{AssetID: asset.ID, Method: "GET", Path: "/baseline"},
		{AssetID: asset.ID, Method: "GET", Path: "/current"},
		{AssetID: asset.ID, Method: "GET", Path: "/pending"},
		{AssetID: private.ID, Method: "GET", Path: "/private"},
	}
	for _, endpoint := range endpoints {
		if err = h.db.PutAssetEndpoint(endpoint, allAssets); err != nil {
			t.Fatal(err)
		}
	}

	task.Scope = "Inventory fixture"
	task.Criteria = "Inventory snapshot\nReview unresolved item"
	task.Steps = []deepassessment.Step{{ID: "inventory", Title: "Read inventory", Status: "running", AssetIDs: []string{asset.ID}}}
	if err = store.Change(task.ID, "", "fixture", "", func(current *deepassessment.Task) error {
		current.Scope, current.Criteria, current.Steps = task.Scope, task.Criteria, task.Steps
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	historical := func(id, user, scope string, age time.Duration) *deepassessment.Task {
		t.Helper()
		old := *task
		old.ID, old.Name, old.OwnerID, old.Scope, old.Status = id, id, user, scope, "paused"
		old.CreatedAt = task.CreatedAt.Add(-age)
		if err := store.Create(&old); err != nil {
			t.Fatal(err)
		}
		if err := store.BindAssets(id, []string{asset.ID}); err != nil {
			t.Fatal(err)
		}
		return &old
	}
	baseline := historical("baseline", task.OwnerID, task.Scope, time.Hour)
	historical("changed-scope", task.OwnerID, "Different inventory scope", 2*time.Hour)
	historical("private-history", other.ID, task.Scope, time.Minute)
	if err = store.BindAssets(task.ID, []string{asset.ID, private.ID}); err != nil {
		t.Fatal(err)
	}
	receipt := func(id, owner string) {
		t.Helper()
		call := agentruntime.ToolCall{ID: id, Name: "fixture_read"}
		if err := store.BeginCall(owner, "inventory", call); err != nil {
			t.Fatal(err)
		}
		if err := store.FinishCall(call, agentruntime.ToolResult{Content: "saved fixture"}, false); err != nil {
			t.Fatal(err)
		}
	}
	fact := func(owner, id, state, summary, proof string, assets ...string) {
		t.Helper()
		receipt(proof, owner)
		err := store.AddFact(owner, &deepassessment.Fact{ID: id, StepID: "inventory", Confidence: state, Summary: summary, Body: "fixture", Evidence: []string{proof}, AssetIDs: assets})
		if err != nil {
			t.Fatal(err)
		}
	}
	fact(baseline.ID, "fact:old", "observed", "Previously observed inventory", "old-receipt", asset.ID)
	if err = store.SetEndpointEvidence(baseline.ID, asset.ID, endpoints[0].ID, "fact:old", true); err != nil {
		t.Fatal(err)
	}
	fact(task.ID, "fact:current", "observed", "Updated inventory", "current-receipt", asset.ID, private.ID)
	fact(task.ID, "fact:pending", "inferred", "Inventory needs review", "pending-receipt", asset.ID)
	receipt("unattributed-receipt", task.ID)
	if err = store.SaveAcceptanceMapping(task, 1, deepassessment.AcceptanceMapping{FactIDs: []string{"fact:current"}}); err != nil {
		t.Fatal(err)
	}
	if err = store.SaveAcceptanceMapping(task, 2, deepassessment.AcceptanceMapping{ReceiptIDs: []string{"unattributed-receipt"}, NeedsReview: true, FactIDs: []string{"fact:pending"}}); err != nil {
		t.Fatal(err)
	}
	register := func(permissions map[string]bool) *gin.Engine {
		r := explorationRouter(h, task.OwnerID, permissions)
		r.GET("/api/deep-assessments/:id/evidence-path", h.DeepAssessmentEvidencePath)
		r.GET("/api/deep-assessments/:id/evidence-history", h.DeepAssessmentEvidenceHistory)
		r.GET("/api/deep-assessments/:id/endpoint-evidence", h.DeepAssessmentEndpointEvidence)
		r.PUT("/api/deep-assessments/:id/endpoint-evidence", h.DeepAssessmentEndpointEvidence)
		r.GET("/api/deep-assessments/:id/report", h.DeepAssessmentReport)
		return r
	}
	router := register(map[string]bool{"assessments:read": true, "assessments:write": true, "asset:read": true})
	noAssets := register(map[string]bool{"assessments:read": true, "assessments:write": true})
	readOnly := register(map[string]bool{"assessments:read": true, "asset:read": true})
	call := func(r *gin.Engine, verb, path, payload string, status int) string {
		t.Helper()
		w := httptest.NewRecorder()
		req := httptest.NewRequest(verb, "/api/deep-assessments/task/"+path, strings.NewReader(payload))
		r.ServeHTTP(w, req)
		if w.Code != status {
			t.Fatalf("%s %s -> %d: %s", verb, path, w.Code, w.Body.String())
		}
		return w.Body.String()
	}
	decode := func(body string, dest any) {
		t.Helper()
		if err := json.Unmarshal([]byte(body), dest); err != nil {
			t.Fatal(err, body)
		}
	}
	params := "asset_id=" + asset.ID
	before, _ := store.ChangeVersion(task.ID)
	var view deepassessment.EvidenceView
	decode(call(router, "GET", "evidence-path?"+params, "", 200), &view)
	if view.Root != "asset:"+asset.ID || view.RelatedIntentCount != 1 ||
		!slices.Contains(view.Edges, deepassessment.Edge{Source: "criterion:1", Target: "fact:current", Relation: "review_link"}) ||
		!slices.Contains(view.Edges, deepassessment.Edge{Source: "fact:current", Target: "receipt:current-receipt", Relation: "backed_by"}) {
		t.Fatal(view)
	}
	decode(call(router, "GET", "evidence-path?criterion=2", "", 200), &view)
	if view.Unattributed != 1 || view.Root != "criterion:2" {
		t.Fatal(view)
	}
	body := call(router, "GET", "evidence-path?fact_id=fact:current", "", 200)
	if strings.Contains(body, private.ID) || !strings.Contains(body, "criterion:1") || !strings.Contains(body, asset.ID) {
		t.Fatal(body)
	}
	body = call(noAssets, "GET", "evidence-path?criterion=1", "", 200)
	if strings.Contains(body, asset.ID) || !strings.Contains(body, "fact:current") {
		t.Fatal(body)
	}
	call(noAssets, "GET", "evidence-path?"+params, "", 403)
	call(router, "GET", "evidence-path?fact_id=fact:old", "", 404)
	call(router, "GET", "evidence-path?"+params+"&endpoint_id="+endpoints[3].ID, "", 404)

	link := func(r *gin.Engine, endpoint, fact string, linked bool, code int) {
		t.Helper()
		body := fmt.Sprintf(`{"asset_id":%q,"endpoint_id":%q,"fact_id":%q,"linked":%t}`, asset.ID, endpoint, fact, linked)
		call(r, "PUT", "endpoint-evidence", body, code)
	}
	link(readOnly, endpoints[1].ID, "fact:current", true, 403)
	link(router, endpoints[3].ID, "fact:current", true, 404)
	link(router, endpoints[1].ID, "fact:old", true, 404)
	link(router, endpoints[1].ID, "fact:current", true, 200)
	link(router, endpoints[1].ID, "fact:current", true, 200)
	link(router, endpoints[2].ID, "fact:pending", true, 200)
	var coverage struct {
		Endpoints []deepassessment.EndpointEvidence
		Counts    map[string]int
	}
	decode(call(router, "GET", "endpoint-evidence?"+params+"&status=missing", "", 200), &coverage)
	if len(coverage.Endpoints) != 1 || coverage.Endpoints[0].ID != endpoints[0].ID || coverage.Counts["observed"] != 1 || coverage.Counts["needs_review"] != 1 {
		t.Fatal(coverage)
	}
	decode(call(router, "GET", "evidence-path?"+params+"&endpoint_id="+endpoints[1].ID, "", 200), &view)
	if !slices.Contains(view.Edges, deepassessment.Edge{Source: endpoints[1].ID, Target: "fact:current", Relation: "endpoint_annotation"}) {
		t.Fatal(view)
	}
	var history deepassessment.AssetEvidenceComparison
	decode(call(router, "GET", "evidence-history?"+params, "", 200), &history)
	if len(history.Prior) != 2 || history.Prior[0].TaskID != baseline.ID || !history.Prior[0].Comparable || history.Prior[0].NewEndpoints != 1 ||
		history.Prior[0].NewObservations != 1 || history.Prior[0].UnseenNow != 1 || history.Prior[1].Comparable || !slices.Equal(history.Current.PendingCriteria, []int{2}) {
		t.Fatal(history)
	}
	body = call(router, "GET", "report", "", 200)
	for _, expected := range []string{"端点证据与历次测评对照", "新增观察文本 1 条", "新增证据端点 1 个", "待人工复核的验收项 2", "tab=evidence-path&criterion=2", "endpoint_state=missing"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("missing %q in report", expected)
		}
	}
	if strings.Contains(body, "private-history") || strings.Contains(body, "private.example.test") {
		t.Fatal("report leaked restricted history or asset")
	}
	link(router, endpoints[1].ID, "fact:current", false, 200)
	decode(call(router, "GET", "endpoint-evidence?"+params+"&status=missing", "", 200), &coverage)
	if coverage.Counts["observed"] != 0 || coverage.Counts["missing"] != 2 {
		t.Fatal(coverage)
	}
	after, _ := store.ChangeVersion(task.ID)
	if before != after {
		t.Fatalf("review or navigation changed scheduler version from %d to %d", before, after)
	}
}
