package project

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	"go.uber.org/zap"
)

func TestProjectResumeSurvivesRestartAndStaysProjectScoped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resume.db")
	db, err := database.NewDB(path, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { db.Close() }()
	p, err := db.CreateProject(&database.Project{Name: "resume"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := db.CreateProject(&database.Project{Name: "other"})
	if err != nil {
		t.Fatal(err)
	}
	conv, err := db.CreateConversation("yesterday", database.ConversationCreateMeta{ProjectID: p.ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []*database.ProjectFact{
		{ProjectID: p.ID, FactKey: "artifact/parser", Category: "note", Summary: "已验证解析脚本", Body: "路径 artifacts/parser.py；调用 python parser.py --input capture.txt；验证输出 PASS", Confidence: "confirmed", SourceConversationID: conv.ID},
		{ProjectID: p.ID, FactKey: "test/login-query", Category: "note", Summary: "已测参数未发现异常", Body: "POST /login 参数 q 匿名：重复三次响应相同；仅在该入口有效；版本变化再测", Confidence: "confirmed"},
		{ProjectID: p.ID, FactKey: "artifact/obsolete", Category: "note", Summary: "废弃旧脚本", Body: "obsolete-body", Confidence: "deprecated"},
		{ProjectID: other.ID, FactKey: "artifact/private", Category: "note", Summary: "other-project-secret", Body: "private-body", Confidence: "confirmed"},
	} {
		if _, err := db.UpsertProjectFact(f); err != nil {
			t.Fatal(err)
		}
	}
	for _, v := range []*database.Vulnerability{
		{ProjectID: p.ID, Title: "known finding", Severity: "medium", Status: "fixed", Target: "https://example.test/login"},
		{ProjectID: other.ID, Title: "private-vulnerability", Severity: "high"},
	} {
		if _, err := db.CreateVulnerability(v); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = database.NewDB(path, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	// Both a reopened conversation and a fresh conversation in the same project
	// receive saved results without relying on any chat history.
	for _, title := range []string{"reopened", "today"} {
		if _, err := db.CreateConversation(title, database.ConversationCreateMeta{ProjectID: p.ID}); err != nil {
			t.Fatal(err)
		}
		block, err := BuildProjectBlackboardBlock(db, p.ID, config.ProjectConfig{Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"artifacts/parser.py", "python parser.py", "POST /login", "版本变化再测", "known finding", "fixed", conv.ID, "first restore prior results", "skip them by default"} {
			if !strings.Contains(block, want) {
				t.Errorf("missing %q in %s", want, block)
			}
		}
		for _, secret := range []string{"other-project-secret", "private-body", "private-vulnerability", "obsolete-body"} {
			if strings.Contains(block, secret) {
				t.Errorf("unexpected %q", secret)
			}
		}
	}
}

func TestResumeSupplementBudgetAndIndependentResultQuotas(t *testing.T) {
	db, err := database.NewDB(filepath.Join(t.TempDir(), "budget.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p, err := db.CreateProject(&database.Project{Name: "budget"})
	if err != nil {
		t.Fatal(err)
	}
	var facts []*database.ProjectFact
	for i := 0; i < 25; i++ {
		facts = append(facts, &database.ProjectFact{FactKey: fmt.Sprintf("artifact/%d", i), Summary: strings.Repeat("大", 2000), Body: strings.Repeat("文", 10000), Confidence: "confirmed"})
		if _, err := db.CreateVulnerability(&database.Vulnerability{ProjectID: p.ID, Title: strings.Repeat("漏洞", 500), Severity: "high"}); err != nil {
			t.Fatal(err)
		}
	}
	facts = append(facts, &database.ProjectFact{FactKey: "test/negative", Summary: "bounded-negative", Body: "已测条件下未发现", Confidence: "confirmed"})
	block := buildResumeSection(db, p.ID, facts)
	if n := len([]rune(block)); n > resumeSupplementMaxRunes {
		t.Fatalf("budget exceeded: %d", n)
	}
	if !strings.Contains(block, "test/negative") || (!strings.Contains(block, "/ 25") || strings.Contains(block, "artifact: showing 0")) || !strings.Contains(block, "get_vulnerability") {
		t.Fatalf("missing independent result quota or retrieval: %s", block)
	}
}

func TestProjectResumeWithoutFactsStillListsVulnerabilities(t *testing.T) {
	db, err := database.NewDB(filepath.Join(t.TempDir(), "empty.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p, err := db.CreateProject(&database.Project{Name: "empty"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateVulnerability(&database.Vulnerability{ProjectID: p.ID, Title: "finding-without-facts", Severity: "low"}); err != nil {
		t.Fatal(err)
	}
	block, err := BuildFactIndexBlock(db, p.ID, config.ProjectConfig{Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(block, "finding-without-facts") {
		t.Fatal(block)
	}
	disabled, err := BuildFactIndexBlock(db, p.ID, config.ProjectConfig{})
	if err != nil || disabled != "" {
		t.Fatalf("disabled=%q, err=%v", disabled, err)
	}
}

func TestResumeSupplementRespectsMergedIndexBudget(t *testing.T) {
	db, err := database.NewDB(filepath.Join(t.TempDir(), "resume-cap.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p, err := db.CreateProject(&database.Project{Name: "budget"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.UpsertProjectFact(&database.ProjectFact{ProjectID: p.ID, FactKey: "artifact/saved", Category: "note", Summary: "saved script", Body: "artifacts/saved.py " + FactIndexSectionEndMarker, Confidence: "confirmed"}); err != nil {
		t.Fatal(err)
	}
	for _, cap := range []int{30, 100, 300, 700, 3500, 6500, 12000} {
		block, err := BuildFactIndexBlock(db, p.ID, config.ProjectConfig{Enabled: true, FactIndexMaxRunes: cap})
		if err != nil {
			t.Fatal(err)
		}
		if n := len([]rune(block)); n > cap {
			t.Fatalf("index has %d runes for cap %d", n, cap)
		}
		if block != "" && (strings.Count(block, FactIndexSectionStartMarker) != 1 || strings.Count(block, FactIndexSectionEndMarker) != 1) {
			t.Fatalf("broken host markers: %s", block)
		}
		if cap == 12000 && !strings.Contains(block, "artifacts/saved.py") {
			t.Fatal("selected artifact body was not recovered from the index projection")
		}
	}
}
