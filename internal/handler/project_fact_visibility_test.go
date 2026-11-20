package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/security"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func TestReadableProjectHidesUnreadableFactSources(t *testing.T) {
	gin.SetMode(gin.TestMode)
	agent, task := deepAssessmentFixture(t)
	db := agent.db
	now := time.Now().UTC()
	if _, err := db.Exec(`INSERT INTO rbac_roles(id,name,description,scope,is_system,created_at,updated_at) VALUES('role-reader','reader','','own',0,?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"project:read", "project:write", "chat:read", "asset:read", "vulnerability:read", "assessments:read"} {
		if _, err := db.Exec(`INSERT OR IGNORE INTO rbac_role_permissions(role_id,permission_key,created_at) VALUES('role-reader',?,?)`, key, now); err != nil {
			t.Fatal(err)
		}
	}
	reader, err := db.CreateRBACUser("reader", "Reader", "hash", true, []string{"role-reader"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := db.CreateRBACUser("other", "Other", "hash", true, []string{"role-reader"})
	if err != nil {
		t.Fatal(err)
	}
	project, err := db.CreateProject(&database.Project{Name: "reader project"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE projects SET owner_user_id=? WHERE id=?`, reader.ID, project.ID); err != nil {
		t.Fatal(err)
	}
	asset := &database.Asset{ProjectID: project.ID, Domain: "visible.example", Status: "active"}
	if _, err = db.UpsertAssets([]*database.Asset{asset}, reader.ID, true); err != nil {
		t.Fatal(err)
	}
	readable, err := db.CreateConversation("readable", database.ConversationCreateMeta{ProjectID: project.ID})
	if err != nil {
		t.Fatal(err)
	}
	secret, err := db.CreateConversation("secret", database.ConversationCreateMeta{})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.SetResourceOwner("conversation", readable.ID, reader.ID); err != nil {
		t.Fatal(err)
	}
	if err = db.SetResourceOwner("conversation", secret.ID, other.ID); err != nil {
		t.Fatal(err)
	}
	const secretMessage = "secret-message-id"
	const secretExecution = "secret-execution-id"
	if _, err = db.Exec(`INSERT INTO tool_executions(id,tool_name,arguments,status,start_time,conversation_id) VALUES('exec-readable','http','{}','completed',?,?)`, now, readable.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO tool_executions(id,tool_name,arguments,status,start_time,conversation_id) VALUES(?,?,?,?,?,?)`, secretExecution, "http", "{}", "completed", now, secret.ID); err != nil {
		t.Fatal(err)
	}
	manual, err := db.CreateVulnerability(&database.Vulnerability{ConversationID: readable.ID, ProjectID: project.ID, Title: "manual fixed", Severity: "low", Status: "fixed", Target: "https://visible.example/manual"})
	if err != nil {
		t.Fatal(err)
	}
	verified, err := db.CreateVulnerability(&database.Vulnerability{ConversationID: readable.ID, ProjectID: project.ID, Title: "verified fix", Severity: "low", Status: "open", Target: "https://visible.example/verified"})
	if err != nil {
		t.Fatal(err)
	}
	unchecked, err := db.CreateVulnerability(&database.Vulnerability{ConversationID: readable.ID, ProjectID: project.ID, Title: "unchecked fix", Severity: "low", Status: "open", Target: "https://visible.example/unchecked"})
	if err != nil {
		t.Fatal(err)
	}
	for _, vuln := range []*database.Vulnerability{manual, verified, unchecked} {
		if _, err = db.Exec(`UPDATE vulnerabilities SET owner_user_id=? WHERE id=?`, reader.ID, vuln.ID); err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(`INSERT INTO vulnerability_assessment_sources(vulnerability_id,assessment_id,assessment_name,step_id,step_title,conversation_id) VALUES(?,?,?,?,?,?)`, vuln.ID, task.ID, task.Name, "inventory", "inventory", readable.ID); err != nil {
			t.Fatal(err)
		}
	}
	visibleFact, err := db.SaveProjectFact(&database.ProjectFact{
		ProjectID: project.ID, FactKey: "note/visible", Summary: "visible source", Confidence: "tentative",
		SourceConversationID: readable.ID, SourceMessageID: "msg-readable", RelatedVulnerabilityID: verified.ID,
	}, []string{asset.ID}, []string{"exec-readable"}, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.SaveProjectFact(&database.ProjectFact{
		ProjectID: project.ID, FactKey: "note/hidden", Summary: "hidden source", Confidence: "tentative",
		SourceConversationID: secret.ID, SourceMessageID: secretMessage, RelatedVulnerabilityID: verified.ID,
	}, []string{asset.ID}, []string{secretExecution}, true, true); err != nil {
		t.Fatal(err)
	}

	projects := NewProjectHandler(db, zap.NewNop())
	assets := NewAssetHandler(db, zap.NewNop())
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(security.ContextSessionKey, security.Session{
			UserID: reader.ID, Scope: database.RBACScopeOwn,
			Permissions: map[string]bool{
				"project:read": true, "project:write": true, "chat:read": true,
				"asset:read": true, "vulnerability:read": true, "assessments:read": true,
			},
			PermissionScopes: map[string]string{
				"project:read": database.RBACScopeOwn, "project:write": database.RBACScopeOwn,
				"chat:read": database.RBACScopeOwn, "asset:read": database.RBACScopeOwn,
				"vulnerability:read": database.RBACScopeOwn, "assessments:read": database.RBACScopeOwn,
			},
		})
	})
	router.GET("/projects/:id/facts", projects.ListFacts)
	router.PUT("/projects/:id/facts/:factId", projects.UpdateFact)
	router.GET("/assets/:id/facts", assets.Facts)
	router.GET("/deep-assessments/:id/report", agent.DeepAssessmentReport)
	request := func(method, path, body string, code int) string {
		t.Helper()
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)
		if w.Code != code {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		return w.Body.String()
	}
	request(http.MethodPut, "/projects/"+project.ID+"/facts/"+visibleFact.ID, `{"confidence":"confirmed","summary":"visible source"}`, http.StatusBadRequest)
	confirmed := request(http.MethodPut, "/projects/"+project.ID+"/facts/"+visibleFact.ID, `{"confidence":"confirmed","confirmation_note":"人工核对来源回执后确认","summary":"visible source"}`, http.StatusOK)
	if !strings.Contains(confirmed, `"confidence":"confirmed"`) || !strings.Contains(confirmed, "人工核对来源回执后确认") || !strings.Contains(confirmed, readable.ID) || strings.Contains(confirmed, secret.ID) {
		t.Fatalf("review response: %s", confirmed)
	}
	for _, path := range []string{
		"/projects/" + project.ID + "/facts",
		"/projects/" + project.ID + "/facts?fact_key=note/hidden",
		"/projects/" + project.ID + "/facts?related_vulnerability_id=" + verified.ID,
		"/assets/" + asset.ID + "/facts",
	} {
		body := request(http.MethodGet, path, "", http.StatusOK)
		if strings.Contains(body, secret.ID) || strings.Contains(body, secretMessage) || strings.Contains(body, secretExecution) {
			t.Fatalf("unauthorized source leaked from %s: %s", path, body)
		}
		if path != "/projects/"+project.ID+"/facts?fact_key=note/hidden" && (!strings.Contains(body, readable.ID) || !strings.Contains(body, "exec-readable") || !strings.Contains(body, "msg-readable")) {
			t.Fatalf("authorized source missing from %s: %s", path, body)
		}
	}
	hidden := request(http.MethodGet, "/projects/"+project.ID+"/facts?fact_key=note/hidden", "", http.StatusOK)
	if strings.Contains(hidden, "source_conversation_id") || strings.Contains(hidden, "source_message_id") || strings.Contains(hidden, "execution_ids") {
		t.Fatalf("hidden fact still exposes source fields: %s", hidden)
	}

	bare, err := db.CreateConversation("no receipt", database.ConversationCreateMeta{ProjectID: project.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.SetResourceOwner("conversation", bare.ID, reader.ID); err != nil {
		t.Fatal(err)
	}
	if err = db.UpdateVulnerabilityRetestResult(verified.ID, "fixed", "response is now denied", readable.ID); err != nil {
		t.Fatal(err)
	}
	if err = db.UpdateVulnerabilityRetestResult(unchecked.ID, "fixed", "claimed fixed without a receipt", bare.ID); err != nil {
		t.Fatal(err)
	}
	report := request(http.MethodGet, "/deep-assessments/"+task.ID+"/report", "", http.StatusOK)
	if strings.Count(report, "复测：修复已验证") != 1 || !strings.Contains(report, "尚无复测记录") || !strings.Contains(report, "证据待核对") || !strings.Contains(report, "其余内容仍导出") || !strings.Contains(report, "已标记修复") {
		t.Fatalf("report conclusions: %s", report)
	}
	if strings.Contains(report, secret.ID) || strings.Contains(report, secretExecution) {
		t.Fatalf("report exposed an unreadable source: %s", report)
	}
}
