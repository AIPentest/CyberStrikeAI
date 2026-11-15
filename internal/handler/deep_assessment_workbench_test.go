package handler

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"cyberstrike-ai/internal/agentruntime"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/deepassessment"
	"cyberstrike-ai/internal/mcp"
)

func TestDeepAssessmentTargetedGuidancePersistsAndUsesOnlyOnePiControl(t *testing.T) {
	h, task := deepAssessmentFixture(t)
	h.deepAssessments.Store.Change(task.ID, "", "seed", "", func(v *deepassessment.Task) error {
		v.Status = "running"
		v.Steps = []deepassessment.Step{{ID: "one", Status: "running"}, {ID: "two", Status: "running"}}
		return nil
	})
	var first, second []string
	h.deepAssessmentRuntime.controls.Store(task.ID+"/one", &assessmentStepControl{native: agentruntime.Control{Steer: func(note string) error { first = append(first, note); return nil }}})
	h.deepAssessmentRuntime.controls.Store(task.ID+"/two", &assessmentStepControl{native: agentruntime.Control{Steer: func(note string) error { second = append(second, note); return nil }}})
	current, _ := h.deepAssessments.Store.Get(task.ID)
	if err := h.deepAssessments.ControlStep(task.ID, "one", "guidance", "Only review this intent", 0, current.Revision); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := h.deepAssessmentRuntime.steerStep(task.ID, "one"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if len(first) != 1 || len(second) != 0 {
		t.Fatal(first, second)
	}
	h.deepAssessmentRuntime.trackStepActivity(task.ID, "one", agentruntime.Event{Type: "compaction_start"})
	h.deepAssessmentRuntime.trackStepActivity(task.ID, "two", agentruntime.Event{Type: "tool_execution_start", Data: json.RawMessage(`{"toolName":"fixture-read","args":{"private":"not exposed"}}`)})
	activity := h.deepAssessmentRuntime.sessionActivity(task.ID)
	if activity["one"].Stage != "compacting" || activity["two"].Tool != "fixture-read" {
		t.Fatal(activity)
	}
	if len(h.deepAssessmentRuntime.sessionActivity("foreign-task")) != 0 {
		t.Fatal("cross-task runtime activity exposed")
	}
	current, _ = h.deepAssessments.Store.Get(task.ID)
	if len(current.Guidance) != 0 || len(current.Step("one").Guidance) != 1 {
		t.Fatal(current)
	}
	h.deepAssessments.Store.Change(task.ID, "", "seed", "", func(v *deepassessment.Task) error {
		v.Step("one").Status = "paused"
		v.Step("one").UserPaused = true
		return nil
	})
	current, _ = h.deepAssessments.Store.Get(task.ID)
	if err := h.deepAssessments.ControlStep(task.ID, "one", "guidance", "Saved for later", 0, current.Revision); err != nil {
		t.Fatal(err)
	}
	delivered, err := h.deepAssessmentRuntime.steerStep(task.ID, "one")
	if err != nil || delivered || len(first) != 1 {
		t.Fatal(delivered, err, first)
	}
	graph, err := h.deepAssessments.Store.Graph(task.ID, "", 80, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, n := range graph.Nodes {
		if n.Kind == "guidance" {
			if n.StepID != "one" {
				t.Fatal(n)
			}
			found++
		}
	}
	if found != 2 {
		t.Fatal("targeted directions lack durable provenance", found)
	}
}

func TestDeepAssessmentRuntimeOverviewUsesTaskScopedRecords(t *testing.T) {
	h, task := deepAssessmentFixture(t)
	conv, _ := h.db.CreateConversation("own step", database.ConversationCreateMeta{})
	h.db.SetResourceOwner("conversation", conv.ID, task.OwnerID)
	h.deepAssessments.Store.Change(task.ID, "one", "checkpoint", "saved progress", func(v *deepassessment.Task) error {
		v.Status = "running"
		v.Steps = []deepassessment.Step{{ID: "one", Title: "Active fixture", Status: "running", ConversationID: conv.ID}}
		return nil
	})
	for _, exec := range []*mcp.ToolExecution{{ID: "own-exec", ToolName: "fixture-read", Status: "running", ConversationID: conv.ID, StartTime: time.Now(), Arguments: map[string]any{}}, {ID: "foreign-exec", ToolName: "private-tool", Status: "running", ConversationID: "foreign", StartTime: time.Now(), Arguments: map[string]any{}}} {
		if err := h.db.SaveToolExecution(exec); err != nil {
			t.Fatal(err)
		}
	}
	router := assessmentRouter(h, task.OwnerID)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/api/deep-assessments/task?runtime=true&step_limit=1", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "own-exec") || strings.Contains(w.Body.String(), "foreign-exec") {
		t.Fatal(w.Code, w.Body.String())
	}
	var data struct {
		Runtime deepassessment.RuntimeOverview `json:"runtime"`
	}
	json.Unmarshal(w.Body.Bytes(), &data)
	if len(data.Runtime.Intents) != 1 || data.Runtime.Counts["running"] != 1 || len(data.Runtime.Recent) == 0 {
		t.Fatal(data)
	}
}

func TestDeepAssessmentReportIncludesAuthorizedFindingsReviewsAndReceipts(t *testing.T) {
	h, task := deepAssessmentFixture(t)
	origin, _ := h.db.CreateConversation("origin", database.ConversationCreateMeta{})
	review, _ := h.db.CreateConversation("review", database.ConversationCreateMeta{})
	h.db.SetResourceOwner("conversation", origin.ID, task.OwnerID)
	h.db.SetResourceOwner("conversation", review.ID, task.OwnerID)
	h.deepAssessments.Store.Change(task.ID, "", "seed", "", func(v *deepassessment.Task) error {
		v.Steps = []deepassessment.Step{{ID: "origin", Status: "running", ConversationID: origin.ID}, {ID: "review", Status: "running", ConversationID: review.ID}}
		return nil
	})
	for _, step := range []string{"origin", "review"} {
		call := agentruntime.ToolCall{ID: step + "-call", Name: "fixture-read"}
		if err := h.deepAssessments.Store.BeginCall(task.ID, step, call); err != nil {
			t.Fatal(err)
		}
		if err := h.deepAssessments.Store.FinishCall(call, agentruntime.ToolResult{Content: step + " preserved evidence\n``` literal fence", ExecutionID: step + "-execution"}, false); err != nil {
			t.Fatal(err)
		}
	}
	v, err := h.db.CreateVulnerability(&database.Vulnerability{ID: "visible-finding", ConversationID: origin.ID, Title: "Visible finding", Severity: "high", ReproSteps: "First reproduce the fixture", Evidence: "Recorded proof", EvidenceReceiptIDs: []string{"origin-call"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = h.db.ReviewAssessmentFinding(task.ID, "review", v.ID, "confirmed", "Independent fixture confirmation", []string{"review-call"}); err != nil {
		t.Fatal(err)
	}
	// Report history is not limited to the inspector's newest 100 review rows.
	for i := 0; i < 101; i++ {
		_, err = h.db.Exec("INSERT INTO deep_assessment_finding_reviews(id,vulnerability_id,task_id,step_id,verdict,reason,evidence_json,created_at) VALUES(?,?,?,'review','inconclusive',?,'[]',?)", fmt.Sprint("history-", i), v.ID, task.ID, fmt.Sprint("history marker ", i), time.Now())
		if err != nil {
			t.Fatal(err)
		}
	}
	other, _ := h.db.CreateRBACUser("private-owner", "Private owner", "hash", true, nil)
	privateConv, _ := h.db.CreateConversation("private origin", database.ConversationCreateMeta{})
	h.db.SetResourceOwner("conversation", privateConv.ID, other.ID)
	h.deepAssessments.Store.Change(task.ID, "", "seed", "", func(v *deepassessment.Task) error {
		v.Steps = append(v.Steps, deepassessment.Step{ID: "private-origin", Status: "completed", ConversationID: privateConv.ID})
		return nil
	})
	private, err := h.db.CreateVulnerability(&database.Vulnerability{ID: "private-finding", ConversationID: privateConv.ID, Title: "DO_NOT_EXPORT", Severity: "low"})
	if err != nil {
		t.Fatal(err)
	}
	h.db.SetResourceOwner("vulnerability", private.ID, other.ID)
	router := assessmentFindingRouter(h, task.OwnerID, map[string]bool{"assessments:read": true, "vulnerability:read": true})
	router.GET("/api/deep-assessments/:id/report", h.DeepAssessmentReport)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/api/deep-assessments/task/report", nil))
	text := w.Body.String()
	if w.Code != 200 {
		t.Fatal(w.Code, text)
	}
	for _, expected := range []string{"Visible finding", "Independent fixture confirmation", "origin preserved evidence", "review preserved evidence", "history marker 0", "history marker 100", "vulnerability_id=visible-finding", "````"} {
		if !strings.Contains(text, expected) {
			t.Fatal("report missing", expected)
		}
	}
	if strings.Contains(text, "DO_NOT_EXPORT") {
		t.Fatal("report leaked a private finding")
	}
	without := assessmentRouter(h, task.OwnerID)
	w = httptest.NewRecorder()
	without.ServeHTTP(w, httptest.NewRequest("GET", "/api/deep-assessments/task/report", nil))
	if w.Code != 200 || strings.Contains(w.Body.String(), "Visible finding") || strings.Contains(w.Body.String(), "Independent fixture confirmation") {
		t.Fatal(w.Code, w.Body.String())
	}
}
