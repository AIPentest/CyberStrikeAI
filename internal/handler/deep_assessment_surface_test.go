package handler

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/deepassessment"
	"cyberstrike-ai/internal/security"
	"github.com/gin-gonic/gin"
)

func TestDeepAssessmentSurfaceItems(t *testing.T) {
	h, task := deepAssessmentFixture(t)
	other := *task
	other.ID = "another-assessment"
	if err := h.deepAssessments.Store.Create(&other); err != nil {
		t.Fatal(err)
	}
	for _, ref := range []struct{ taskID, id string }{{task.ID, "fact:local"}, {other.ID, "fact:foreign"}} {
		if _, err := h.db.Exec(`INSERT INTO deep_assessment_facts(id,task_id,step_id,confidence,summary,body,evidence_json,assets_json,sources_json,created_at)
			VALUES(?,?,'step','inferred','fixture','','[]','[]','[]',?)`, ref.id, ref.taskID, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
	}
	router := func(user string, write, assetRead bool) *gin.Engine {
		r := gin.New()
		r.Use(func(c *gin.Context) {
			c.Set(security.ContextSessionKey, security.Session{UserID: user, Scope: database.RBACScopeOwn,
				Permissions: map[string]bool{"assessments:read": true, "assessments:write": write, "asset:read": assetRead}})
		})
		r.Use(security.RBACMiddleware(h.db))
		r.GET("/api/deep-assessments/:id/surface-items", h.DeepAssessmentSurfaceItems)
		r.POST("/api/deep-assessments/:id/surface-items", h.DeepAssessmentSurfaceItems)
		r.PUT("/api/deep-assessments/:id/surface-items/:itemId", h.DeepAssessmentSurfaceItems)
		return r
	}
	request := func(r *gin.Engine, method, path, body string, status int) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(method, "/api/deep-assessments/"+path, strings.NewReader(body)))
		if w.Code != status {
			t.Fatalf("%s %s: got %d, want %d: %s", method, path, w.Code, status, w.Body)
		}
		return w
	}
	owner := router(task.OwnerID, true, true)
	reader := router(task.OwnerID, false, true)
	const path = "task/surface-items"
	const note = `{"kind":"cloud_access_key","summary":"  Fixture key exposure  ","location":"fixture settings","fact_id":"fact:local"}`
	request(reader, "POST", path, note, 403)
	request(router("unrelated", true, true), "GET", path, "", 403)
	request(router(task.OwnerID, true, false), "GET", path, "", 403)
	request(owner, "POST", path, `{"kind":"unknown","summary":"Example"}`, 400)
	request(owner, "POST", path, `{"kind":"credential","summary":"   "}`, 400)
	request(owner, "POST", path, `{"kind":"credential","summary":"Example","secret":"fixture-only"}`, 400)
	request(owner, "POST", path, strings.Replace(note, "fact:local", "fact:foreign", 1), 400)
	w := request(owner, "POST", path, note, 200)
	var saved deepassessment.SurfaceItem
	if err := json.Unmarshal(w.Body.Bytes(), &saved); err != nil || saved.ID == "" || saved.Summary != "Fixture key exposure" || saved.FactID != "fact:local" || saved.Revision != 1 {
		t.Fatalf("unexpected saved record: %s (%v)", w.Body, err)
	}
	w = request(reader, "GET", path, "", 200)
	if !strings.Contains(w.Body.String(), saved.ID) {
		t.Fatal(w.Body)
	}
	const updated = `{"kind":"credential","summary":"Updated finding","revision":1}`
	itemPath := path + "/" + saved.ID
	request(owner, "PUT", "another-assessment/surface-items/"+saved.ID, updated, 404)
	request(owner, "PUT", itemPath, updated, 200)
	request(owner, "PUT", itemPath, updated, 409)
	w = request(reader, "GET", path, "", 200)
	if !strings.Contains(w.Body.String(), `"revision":2`) || !strings.Contains(w.Body.String(), `"fact_id":""`) {
		t.Fatal(w.Body)
	}
	if err := h.deepAssessments.Store.Change(task.ID, "", "", "", func(task *deepassessment.Task) error {
		task.Archived = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	request(owner, "POST", path, note, 409)
	request(owner, "PUT", itemPath, `{"kind":"other","summary":"Example","revision":2}`, 409)
	request(reader, "GET", path, "", 200)
}
