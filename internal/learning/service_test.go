package learning

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	"go.uber.org/zap"
)

type extractorFunc func(context.Context, string, []database.LearningEvidence) (Extraction, error)

func (f extractorFunc) Extract(ctx context.Context, s string, e []database.LearningEvidence) (Extraction, error) {
	return f(ctx, s, e)
}
func fixtureCandidate(ids []string) Candidate {
	return Candidate{Decision: "new", Name: "option-check", Description: "When a tool explicitly reports an unsupported option, inspect its help before retrying.", EvidenceIDs: ids, Reason: "Reusable option compatibility procedure", Body: "## 适用条件\n明确的选项错误。\n## 前置条件\n当前任务授权。\n## 操作步骤\n查帮助，确认语义再重试。\n## 验收与未知项\n检查输出；未证明对其他工具适用。\n## 停止条件\n无法确认等效选项就停止。"}
}

func TestLearningWorkerDraftAndSkip(t *testing.T) {
	db, err := database.NewDB(filepath.Join(t.TempDir(), "learning.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.BootstrapRBAC("test-hash", map[string]string{"chat:read": "read"}); err != nil {
		t.Fatal(err)
	}
	u, err := db.CreateRBACUser("learner", "learner", "test-hash", true, []string{database.RBACSystemRoleAdmin})
	if err != nil {
		t.Fatal(err)
	}
	c, err := db.CreateConversation("synthetic", database.ConversationCreateMeta{})
	if err != nil {
		t.Fatal(err)
	}
	_ = db.SetResourceOwner("conversation", c.ID, u.ID)
	addRun := func(id string, tool bool) {
		t.Helper()
		_, _ = db.AddMessage(c.ID, "user", "check fixture tool", nil)
		m, _ := db.AddMessage(c.ID, "assistant", "checked", nil)
		if err := db.BeginLearningRun(id, c.ID); err != nil {
			t.Fatal(err)
		}
		if tool {
			if err := db.AddProcessDetail(m.ID, c.ID, "tool_result", "", map[string]any{"result": "unsupported option; help lists replacement"}); err != nil {
				t.Fatal(err)
			}
		}
		if err := db.FinishLearningRun(id, "completed"); err != nil {
			t.Fatal(err)
		}
	}
	calls := 0
	s := New(db, config.LearningConfig{}, extractorFunc(func(ctx context.Context, status string, e []database.LearningEvidence) (Extraction, error) {
		calls++
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("unbounded model context")
		}
		ids := []string{}
		for _, v := range e {
			if v.Kind == "tool_result" {
				ids = append(ids, v.ID)
			}
		}
		return Extraction{Candidate: fixtureCandidate(ids), PromptTokens: 12, CompletionTokens: 7}, nil
	}), "", zap.NewNop())
	addRun("one", true)
	if err := s.ProcessOne(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	addRun("two", true)
	if err := s.ProcessOne(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	addRun("chat", false)
	if err := s.ProcessOne(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	jobs, err := db.ListLearningJobs(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	drafts, skips := 0, 0
	for _, j := range jobs {
		if j.Status == "drafted" {
			drafts++
			if j.PromptTokens != 12 || j.Outcome != "unknown" {
				t.Fatalf("wrong accounting: %+v", j)
			}
			var candidate Candidate
			_ = json.Unmarshal([]byte(j.CandidateJSON), &candidate)
			if !strings.Contains(candidate.SkillMarkdown, "origin: learned-draft") {
				t.Fatal("missing manifest")
			}
		}
		if j.Status == "skipped" {
			skips++
		}
	}
	if drafts != 1 || skips != 2 || calls != 2 {
		t.Fatalf("drafts=%d skips=%d calls=%d", drafts, skips, calls)
	}
}

func TestCandidateGuardrailsAndSanitization(t *testing.T) {
	e := []database.LearningEvidence{{ID: "tool", Kind: "tool_result", Text: `{"cookie":"private-cookie","result":"Bearer private-token https://target.invalid/test 192.0.2.1","api_key":"synthetic-key"}`}}
	clean := SanitizeEvidence(e, "synthetic-key")
	for _, secret := range []string{"private-cookie", "private-token", "target.invalid", "192.0.2.1", "synthetic-key"} {
		if strings.Contains(clean[0].Text, secret) {
			t.Fatal("secret survived minimization")
		}
	}
	for _, tc := range []struct {
		name   string
		change func(*Candidate)
	}{
		{"invented source", func(c *Candidate) { c.EvidenceIDs = []string{"made-up"} }},
		{"target data", func(c *Candidate) { c.Body += "\nhttps://target.invalid" }},
		{"script", func(c *Candidate) { c.Body += "\n```sh\nrun\n```" }},
		{"missing stop condition", func(c *Candidate) { c.Body = strings.ReplaceAll(c.Body, "## 停止条件", "") }},
		{"manifest from model", func(c *Candidate) { c.SkillMarkdown = "arbitrary" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := fixtureCandidate([]string{"tool"})
			tc.change(&c)
			if _, err := ValidateCandidate(&c, "completed", e); err == nil {
				t.Fatal("invalid draft accepted")
			}
		})
	}
	c := fixtureCandidate([]string{"tool"})
	if _, err := ValidateCandidate(&c, "failed", e); err == nil {
		t.Fatal("failure became successful procedure")
	}
	c.Decision = "negative_case"
	if _, err := ValidateCandidate(&c, "failed", e); err != nil {
		t.Fatal(err)
	}
	c = Candidate{Decision: "skip", Body: "unwanted raw content", Reason: "no new procedure"}
	_, err := ValidateCandidate(&c, "completed", e)
	if err != nil || c.Body != "" {
		t.Fatal("skip retained unwanted content")
	}
}

func TestModelExtractorNoToolsAndStrictJSON(t *testing.T) {
	for _, suffix := range []string{"", " trailing garbage"} {
		t.Run(suffix, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request map[string]any
				_ = json.NewDecoder(r.Body).Decode(&request)
				if _, ok := request["tools"]; ok {
					t.Fatal("learner received tool access")
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": `{"decision":"skip","reason":"no evidence"}` + suffix}}}, "usage": map[string]any{"prompt_tokens": 2, "completion_tokens": 3}})
			}))
			defer server.Close()
			m := NewModelExtractor(config.OpenAIConfig{APIKey: "synthetic-test-key", Model: "test-model", BaseURL: server.URL})
			r, err := m.Extract(context.Background(), "completed", nil)
			if (suffix == "") != (err == nil) || r.PromptTokens != 2 {
				t.Fatalf("result=%+v err=%v", r, err)
			}
		})
	}
}

func TestLearningLateModelResultCannotRestoreDeletedSource(t *testing.T) {
	db, err := database.NewDB(filepath.Join(t.TempDir(), "late.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.BootstrapRBAC("test-hash", map[string]string{"chat:read": "read"}); err != nil {
		t.Fatal(err)
	}
	u, err := db.CreateRBACUser("learner", "learner", "test-hash", true, []string{database.RBACSystemRoleAdmin})
	if err != nil {
		t.Fatal(err)
	}
	c, err := db.CreateConversation("synthetic", database.ConversationCreateMeta{})
	if err != nil {
		t.Fatal(err)
	}
	_ = db.SetResourceOwner("conversation", c.ID, u.ID)
	_, _ = db.AddMessage(c.ID, "user", "check tool", nil)
	m, _ := db.AddMessage(c.ID, "assistant", "checked", nil)
	_ = db.AddProcessDetail(m.ID, c.ID, "tool_result", "", map[string]any{"result": "synthetic tool error and help"})
	_ = db.BeginLearningRun("late", c.ID)
	_ = db.FinishLearningRun("late", "completed")
	called := false
	s := New(db, config.LearningConfig{}, extractorFunc(func(ctx context.Context, status string, e []database.LearningEvidence) (Extraction, error) {
		called = true
		ids := []string{}
		for _, v := range e {
			if v.Kind == "tool_result" {
				ids = append(ids, v.ID)
			}
		}
		if err := db.DeleteConversation(c.ID); err != nil {
			t.Fatal(err)
		}
		return Extraction{Candidate: fixtureCandidate(ids)}, nil
	}), "", zap.NewNop())
	if err := s.ProcessOne(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	var count int
	_ = db.QueryRow("SELECT COUNT(*) FROM learning_jobs").Scan(&count)
	if !called || count != 0 {
		t.Fatal("late model restored deleted source")
	}
}
