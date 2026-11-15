package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/deepassessment"
	"cyberstrike-ai/internal/mcp"
	"go.uber.org/zap"
)

func TestDeepAssessmentNativePiPlanSegmentsEvidenceAndReview(t *testing.T) {
	h, task := deepAssessmentFixture(t)
	worker, err := filepath.Abs("../../runtime/pi/dist/worker.js")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(worker); err != nil {
		t.Skip("build Pi runtime to run SDK integration")
	}
	var toolRuns atomic.Int32
	var plannerRuns atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var body struct {
			Messages []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
			Tools []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		names := map[string]bool{}
		for _, tool := range body.Tools {
			names[tool.Function.Name] = true
		}
		current, _ := h.deepAssessments.Store.Get(task.ID)
		if names["submit_plan"] {
			plannerRuns.Add(1)
			for _, m := range body.Messages {
				if m.Role == "assistant" || m.Role == "tool" {
					t.Error("fresh planner inherited an earlier transcript")
				}
			}
			steps := []map[string]any{}
			if len(current.Steps) == 0 {
				steps = append(steps, map[string]any{"id": "one", "title": "Inspect fixture", "instruction": "Read the isolated fixture and save the execution receipt", "depends_on": []string{}})
			}
			deepAssessmentReply(w, "submit_plan", "plan", map[string]any{"strategy": "Read, retain, verify", "summary": "Fixture plan", "steps": steps, "retire": []string{}, "question": ""}, "")
			return
		}
		if names["submit_review"] {
			deepAssessmentReply(w, "submit_review", "review", map[string]any{"complete": true, "summary": "Fixture evidence verified", "missing": "", "evidence": []string{"one"}}, "")
			return
		}
		combined := ""
		for _, m := range body.Messages {
			if m.Role == "tool" {
				var text string
				if json.Unmarshal(m.Content, &text) == nil {
					combined += text + "\n"
				}
			}
		}
		if !strings.Contains(combined, "fixture-evidence") {
			deepAssessmentReply(w, "fixture_echo", "echo", map[string]any{}, "")
			return
		}
		receipt := regexp.MustCompile(`\[Execution receipt: ([^\]]+)\]`).FindStringSubmatch(combined)
		if len(receipt) < 2 {
			t.Errorf("missing receipt: %s", combined)
			w.WriteHeader(500)
			return
		}
		if !strings.Contains(combined, "saved-checkpoint") {
			deepAssessmentReply(w, "assessment_checkpoint", "checkpoint", map[string]any{"summary": "saved-checkpoint"}, "")
			return
		}
		if !strings.Contains(combined, "original-fact") {
			deepAssessmentReply(w, "assessment_fact", "fact", map[string]any{"confidence": "observed", "summary": "original-fact", "body": "fixture-evidence", "evidence": []string{receipt[1]}, "source_ids": []string{"goal"}}, "")
			return
		}
		deepAssessmentReply(w, "assessment_result", "result", map[string]any{"status": "completed", "summary": "Verified fixture-evidence; no remaining gaps.", "evidence": []string{receipt[1]}}, "")
	}))
	defer model.Close()
	cfg := h.config
	cfg.AgentRuntime = config.AgentRuntimeConfig{Engine: "pi", WorkerPath: worker, SessionDir: t.TempDir()}
	cfg.OpenAI = config.OpenAIConfig{Provider: "openai", APIKey: "fixture", Model: "fixture", BaseURL: model.URL + "/v1", MaxTotalTokens: 32000, MaxCompletionTokens: 2048}
	cfg.Agent.MaxIterations = 1
	cfg.MultiAgent.Skills.Disable = true
	srv := mcp.NewServer(zap.NewNop())
	srv.SetToolAuthorizer(func(context.Context, string, map[string]interface{}) error { return nil })
	srv.RegisterTool(mcp.Tool{Name: "fixture_echo", Description: "Read isolated fixture", InputSchema: map[string]any{"type": "object", "properties": map[string]any{}}}, func(context.Context, map[string]interface{}) (*mcp.ToolResult, error) {
		toolRuns.Add(1)
		return &mcp.ToolResult{Content: []mcp.Content{{Type: "text", Text: "fixture-evidence"}}}, nil
	})
	h.agent = agent.NewAgent(&cfg.OpenAI, &cfg.Agent, srv, nil, zap.NewNop(), 10)
	h.tasks.SetToolCanceler(h.cancelRunningMCPToolsForConversation)
	if err = h.deepAssessments.Start(task.ID, task.OwnerID, task.Revision); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(25 * time.Second)
	var got *deepassessment.Task
	for time.Now().Before(deadline) {
		got, err = h.deepAssessments.Store.Get(task.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !deepassessment.Active(got.Status) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got.Status != "completed" || toolRuns.Load() != 1 || len(got.Steps) != 1 || got.Steps[0].Attempts != 4 || len(got.Steps[0].Conversations) != 1 || len(got.Steps[0].Evidence) != 1 {
		if len(got.Steps) > 0 {
			groups, _ := h.db.GetProcessDetailsByConversation(got.Steps[0].ConversationID)
			for _, group := range groups {
				for _, d := range group {
					if d.EventType == "tool_result" {
						t.Logf("tool %s %s", d.Message, d.Data)
					}
				}
			}
		}
		events, _ := h.deepAssessments.Store.Events(task.ID, 0)
		for _, event := range events {
			t.Logf("%s: %s", event.Kind, event.Message)
		}
		t.Fatalf("native assessment lifecycle failed: %+v steps=%+v tools=%d", got, got.Steps, toolRuns.Load())
	}
	facts, factErr := h.deepAssessments.Store.Facts(task.ID, "observed", 0)
	if factErr != nil || len(facts) != 1 || plannerRuns.Load() < 2 {
		t.Fatalf("missing durable fact/fresh replanning: facts=%+v err=%v planners=%d", facts, factErr, plannerRuns.Load())
	}
	if len(got.Metrics) < 3 {
		t.Fatal(fmt.Sprintf("missing planner/worker/reviewer telemetry: %+v", got.Metrics))
	}
}
