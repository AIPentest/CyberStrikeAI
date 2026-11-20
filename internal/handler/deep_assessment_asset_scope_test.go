package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cyberstrike-ai/internal/agentruntime"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/deepassessment"
	"cyberstrike-ai/internal/security"

	"github.com/gin-gonic/gin"
)

func TestDeepAssessmentUsesProjectAssetsFromTheSharedLibrary(t *testing.T) {
	h, task := deepAssessmentFixture(t)
	project, err := h.db.CreateProject(&database.Project{Name: "Shared library"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := h.db.CreateProject(&database.Project{Name: "Other"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.db.Exec(`UPDATE projects SET scope_json=? WHERE id=?`, `{"targets":["https://scope.example.com/health"],"exclude":["https://skip.example.com/secret"]}`, project.ID); err != nil {
		t.Fatal(err)
	}
	owned := &database.Asset{ProjectID: project.ID, Host: "https://owned.example.com"}
	extra := &database.Asset{ProjectID: project.ID, Host: "https://extra.example.com"}
	foreign := &database.Asset{ProjectID: other.ID, Host: "https://foreign.example.net"}
	if _, err = h.db.UpsertAssets([]*database.Asset{owned, extra, foreign}, task.OwnerID, true); err != nil {
		t.Fatal(err)
	}
	router := assessmentAssetRouter(h, task.OwnerID)
	body := `{"name":"shared","objective":"评估 https://owned.example.com/login","project_id":"` + project.ID + `"}`
	request := httptest.NewRequest("POST", "/api/deep-assessments", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatal(response.Code, response.Body.String())
	}
	var created struct {
		Task deepassessment.Task `json:"task"`
	}
	if err = json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	linked := linkedAssetNames(t, h.db, created.Task.ID)
	for _, name := range []string{"owned.example.com", "extra.example.com", "scope.example.com"} {
		if !linked[name] {
			t.Fatalf("shared library asset %s was not linked: %v", name, linked)
		}
	}
	if linked["foreign.example.net"] || linked["skip.example.com"] {
		t.Fatalf("asset outside the project or an excluded target was linked: %v", linked)
	}
	if !assetInProject(t, h.db, project.ID, "scope.example.com") || assetInProject(t, h.db, project.ID, "skip.example.com") {
		t.Fatal("project scope was not written through the shared asset library")
	}

	collection, err := h.db.CreateAssetCollection(project.ID, "only extra", "", "", task.OwnerID, []string{extra.ID})
	if err != nil {
		t.Fatal(err)
	}
	narrow := `{"name":"narrow","objective":"inspect fixture","project_id":"` + project.ID + `","collection_id":"` + collection.ID + `"}`
	request = httptest.NewRequest("POST", "/api/deep-assessments", strings.NewReader(narrow))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatal(response.Code, response.Body.String())
	}
	if err = json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	narrowed := linkedAssetNames(t, h.db, created.Task.ID)
	if !narrowed["extra.example.com"] || narrowed["owned.example.com"] || narrowed["scope.example.com"] {
		t.Fatalf("collection did not narrow the same library: %v", narrowed)
	}
}

func TestDeepAssessmentWritesDiscoveriesBackToTheSharedLibrary(t *testing.T) {
	h, task := deepAssessmentFixture(t)
	project, err := h.db.CreateProject(&database.Project{Name: "Discovery"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.db.Exec(`UPDATE projects SET scope_json=? WHERE id=?`, `{"exclude":["https://skip.example.com"]}`, project.ID); err != nil {
		t.Fatal(err)
	}
	seed := &database.Asset{ProjectID: project.ID, Host: "https://example.com"}
	created := &database.Asset{ProjectID: project.ID, Host: "created.example.net"}
	if _, err = h.db.UpsertAssets([]*database.Asset{seed, created}, task.OwnerID, true); err != nil {
		t.Fatal(err)
	}
	if err = h.deepAssessments.Store.Change(task.ID, "", "fixture", "", func(saved *deepassessment.Task) error {
		saved.ProjectID = project.ID
		saved.Objective = "评估 https://example.com"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	current, err := h.deepAssessments.Store.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	output := "https://api.example.com/admin\nhttp://8.8.8.8/\nhttps://skip.example.com/secret\n"
	if err = h.deepAssessmentRuntime.ingestToolSurface(current, "httpx", agentruntime.ToolResult{Content: output}); err != nil {
		t.Fatal(err)
	}
	if !assetInProject(t, h.db, project.ID, "api.example.com") || assetInProject(t, h.db, project.ID, "8.8.8.8") || assetInProject(t, h.db, project.ID, "skip.example.com") {
		t.Fatal("discovery was not limited to the shared project scope")
	}
	linked := linkedAssetNames(t, h.db, task.ID)
	if !linked["api.example.com"] {
		t.Fatal("new service was not added to the attack surface", linked)
	}
	var endpoint string
	if err = h.db.QueryRow(`SELECT path FROM asset_endpoints WHERE asset_id=(SELECT id FROM assets WHERE domain=? AND project_id=?)`, "api.example.com", project.ID).Scan(&endpoint); err != nil || endpoint != "/admin" {
		t.Fatal(endpoint, err)
	}
	payload, _ := json.Marshal(map[string]any{"action": "created", "asset": map[string]string{"id": created.ID, "domain": "created.example.net"}})
	if err = h.deepAssessmentRuntime.ingestToolSurface(current, "create_asset", agentruntime.ToolResult{Content: string(payload)}); err != nil {
		t.Fatal(err)
	}
	linked = linkedAssetNames(t, h.db, task.ID)
	if !linked["created.example.net"] {
		t.Fatal("asset written to the shared library was not linked", linked)
	}
	preview, err := h.deepAssessmentRuntime.context(current)
	if err != nil || !strings.Contains(preview, "api.example.com") || !strings.Contains(preview, "linked_asset_preview") {
		t.Fatal(err, preview)
	}
}

func assessmentAssetRouter(h *AgentHandler, user string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(security.ContextSessionKey, security.Session{
			UserID: user,
			Scope:  database.RBACScopeAll,
			Permissions: map[string]bool{
				"assessments:read": true, "assessments:write": true,
				"project:read": true, "asset:read": true, "asset:write": true,
			},
		})
	})
	router.Use(security.RBACMiddleware(h.db))
	router.POST("/api/deep-assessments", h.CreateDeepAssessment)
	return router
}

func linkedAssetNames(t *testing.T, db *database.DB, taskID string) map[string]bool {
	t.Helper()
	rows, err := db.Query(`SELECT COALESCE(domain,''), COALESCE(ip,''), COALESCE(host,'') FROM assets JOIN deep_assessment_assets a ON a.asset_id=assets.id WHERE a.task_id=?`, taskID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var domain, ip, host string
		if err = rows.Scan(&domain, &ip, &host); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{domain, ip, host} {
			if name != "" {
				out[name] = true
			}
		}
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func assetInProject(t *testing.T, db *database.DB, projectID, name string) bool {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM assets WHERE project_id=? AND (domain=? OR ip=? OR host=?)`, projectID, name, name, name).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count > 0
}
