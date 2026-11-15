package handler

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"cyberstrike-ai/internal/deepassessment"
)

func TestDeepAssessmentStoppedReviewIsMetadataOnlyAndLegacyClickWorks(t *testing.T) {
	h, task := deepAssessmentFixture(t)
	h.deepAssessments.Store.Change(task.ID, "", "seed", "", func(v *deepassessment.Task) error {
		v.Status = "cancelled"
		v.Steps = []deepassessment.Step{{ID: "one", Status: "uncertain"}, {ID: "two", Status: "cleanup_unconfirmed"}}
		return nil
	})
	router := assessmentRouter(h, task.OwnerID)
	router.GET("/api/deep-assessments/:id/steps/:stepId", h.DeepAssessmentStep)
	post := func(action, step string) *httptest.ResponseRecorder {
		current, _ := h.deepAssessments.Store.Get(task.ID)
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/deep-assessments/task/control/"+action+"?step_limit=50&step_order=queue&step_status=needs_review", strings.NewReader(fmt.Sprintf(`{"revision":%d,"step_id":%q,"reconciled":true,"cleanup_confirmed":true}`, current.Revision, step)))
		req.Header.Set("Authorization", "Bearer fixture")
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)
		return w
	}
	for index, action := range []string{"retry", "reconcile"} {
		step := []string{"one", "two"}[index]
		w := post(action, step)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var result struct {
			Task      deepassessment.Task `json:"task"`
			StepTotal int                 `json:"step_total"`
		}
		json.Unmarshal(w.Body.Bytes(), &result)
		if result.Task.Status != "cancelled" || result.StepTotal != 1-index || result.Task.ReviewCount != 1-index {
			t.Fatal(w.Body.String())
		}
	}
	current, _ := h.deepAssessments.Store.Get(task.ID)
	if h.deepAssessments.Live(task.ID).Active || current.Status != "cancelled" {
		t.Fatal(current)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/api/deep-assessments/task/steps/one", nil))
	if !strings.Contains(w.Body.String(), `"revision":`) {
		t.Fatal("preflight lacks current revision", w.Body.String())
	}
	// A write-only session cannot use the explicit continuation endpoint to execute.
	denied := post("continue-stopped", "")
	if denied.Code != 403 {
		t.Fatal(denied.Code, denied.Body.String())
	}
}
