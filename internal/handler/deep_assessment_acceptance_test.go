package handler

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"cyberstrike-ai/internal/agentruntime"
	"cyberstrike-ai/internal/deepassessment"
	"github.com/gin-gonic/gin"
)

func TestDeepAssessmentAcceptanceMatrix(t *testing.T) {
	h, task := deepAssessmentFixture(t)
	store := h.deepAssessments.Store
	if err := store.Change(task.ID, "", "fixture", "", func(task *deepassessment.Task) error {
		task.Criteria = "1. Verify inventory\r\n\r\n2. Review the mismatch\r\n3. Reconcile remaining records"
		task.Steps = []deepassessment.Step{{ID: "inventory", Status: "running"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	seed := func(id, tool string) {
		t.Helper()
		call := agentruntime.ToolCall{ID: id, Name: tool}
		if err := store.BeginCall(task.ID, "inventory", call); err != nil {
			t.Fatal(err)
		}
		if err := store.FinishCall(call, agentruntime.ToolResult{Content: "fixture output " + id}, false); err != nil {
			t.Fatal(err)
		}
	}
	seed("inventory-receipt", "fixture_read")
	seed("summary-call", "assessment_facts")
	for _, fact := range []deepassessment.Fact{
		{ID: "fact:observed", Confidence: "observed", Summary: "Inventory observed"},
		{ID: "fact:inferred", Confidence: "inferred", Summary: "Inventory inferred"},
	} {
		fact.StepID = "inventory"
		fact.Evidence = []string{"inventory-receipt"}
		if err := store.AddFact(task.ID, &fact); err != nil {
			t.Fatal(err)
		}
	}
	otherTask := *task
	otherTask.ID = "other"
	if err := store.Create(&otherTask); err != nil {
		t.Fatal(err)
	}
	if err := store.BeginCall(otherTask.ID, "", agentruntime.ToolCall{ID: "foreign-receipt", Name: "fixture_read"}); err != nil {
		t.Fatal(err)
	}

	register := func(perms map[string]bool, user string) *gin.Engine {
		r := explorationRouter(h, user, perms)
		r.GET("/api/deep-assessments/:id/acceptance", h.DeepAssessmentAcceptance)
		r.GET("/api/deep-assessments/:id/acceptance/sources", h.DeepAssessmentAcceptanceSources)
		r.PUT("/api/deep-assessments/:id/acceptance/:criterion", h.SaveDeepAssessmentAcceptance)
		r.GET("/api/deep-assessments/:id/receipts", h.DeepAssessmentReceipts)
		r.GET("/api/deep-assessments/:id/report", h.DeepAssessmentReport)
		return r
	}
	router := register(map[string]bool{"assessments:read": true, "assessments:write": true}, task.OwnerID)
	readOnly := register(map[string]bool{"assessments:read": true}, task.OwnerID)
	client := func(r *gin.Engine, method, path, body string, code int) string {
		t.Helper()
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, "/api/deep-assessments/task/"+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		if w.Code != code {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		return w.Body.String()
	}
	matrix := func() []deepassessment.AcceptanceRow {
		t.Helper()
		body := client(router, "GET", "acceptance", "", 200)
		var response struct {
			Rows []deepassessment.AcceptanceRow
		}
		if err := json.Unmarshal([]byte(body), &response); err != nil || len(response.Rows) != 3 {
			t.Fatal(body, err)
		}
		return response.Rows
	}
	if rows := matrix(); rows[0].Status != "missing" || rows[1].Text != "2. Review the mismatch" || rows[2].Number != 3 {
		t.Fatal(rows)
	}
	body := client(router, "GET", "acceptance/sources?kind=receipt&q=inventory", "", 200)
	if !strings.Contains(body, "inventory-receipt") || strings.Contains(body, "summary-call") || strings.Contains(body, "fixture output") {
		t.Fatal(body)
	}
	client(router, "GET", "acceptance/sources?kind=unknown", "", 400)
	mapping := `{"revision":0,"fact_ids":["fact:observed","fact:observed"],"receipt_ids":["inventory-receipt"]}`
	client(readOnly, "PUT", "acceptance/1", mapping, 403)
	client(router, "PUT", "acceptance/4", mapping, 404)
	for _, id := range []string{"summary-call", "foreign-receipt"} {
		client(router, "PUT", "acceptance/1", fmt.Sprintf(`{"receipt_ids":[%q]}`, id), 400)
	}
	client(router, "PUT", "acceptance/1", mapping, 200)
	client(router, "PUT", "acceptance/1", mapping, 409)
	client(router, "PUT", "acceptance/2", `{"fact_ids":["fact:inferred"],"review_note":"Compare inventory sources"}`, 200)
	rows := matrix()
	if rows[0].Status != "has_evidence" || rows[0].Revision != 1 || len(rows[0].Facts) != 1 || len(rows[0].Receipts) != 1 || len(rows[0].Facts[0].ReceiptIDs) != 1 ||
		rows[1].Status != "needs_review" || rows[1].NeedsReview || rows[2].Status != "missing" {
		t.Fatal(rows)
	}
	client(router, "PUT", "acceptance/1", `{"revision":1,"receipt_ids":["inventory-receipt"],"needs_review":true}`, 200)
	if rows = matrix(); rows[0].Status != "needs_review" || !rows[0].NeedsReview {
		t.Fatal(rows)
	}
	client(router, "PUT", "acceptance/1", `{"revision":2,"fact_ids":["fact:observed"]}`, 200)
	body = client(router, "GET", "report", "", 200)
	for _, expected := range []string{"验收证据矩阵", "已有证据 1 项，缺证据 1 项，待人工复核 1 项", "Compare inventory sources", "tab=acceptance&criterion=2", "node_id=fact%3Aobserved", "receipt_id=inventory-receipt"} {
		if !strings.Contains(body, expected) {
			t.Fatal("report missing", expected)
		}
	}
	body = client(router, "GET", "receipts?receipt_id=inventory-receipt", "", 200)
	if !strings.Contains(body, "fixture output inventory-receipt") || strings.Contains(body, "summary-call") {
		t.Fatal(body)
	}
	client(router, "GET", "receipts?receipt_id=foreign-receipt", "", 404)
	if _, err := h.db.Exec("DELETE FROM deep_assessment_calls WHERE id='inventory-receipt'"); err != nil {
		t.Fatal(err)
	}
	if rows = matrix(); rows[0].Status != "needs_review" || len(rows[0].Receipts) != 0 {
		t.Fatal(rows)
	}
}
