package multiagent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/project"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

func TestTranscriptSnapshotsPreserveEarlierEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.txt")
	first := []adk.Message{schema.UserMessage("first target"), schema.AssistantMessage("first evidence", nil)}
	before, err := writeSummarizationTranscriptSnapshot(path, first)
	if err != nil {
		t.Fatal(err)
	}
	again, err := writeSummarizationTranscriptSnapshot(path, first)
	if err != nil || before != again {
		t.Fatalf("snapshot is not idempotent: %s %s %v", before, again, err)
	}
	after, err := writeSummarizationTranscriptSnapshot(path, []adk.Message{schema.UserMessage("second target")})
	if err != nil || before == after {
		t.Fatalf("new history reused old snapshot: %v", err)
	}
	raw, err := os.ReadFile(before)
	if err != nil || !strings.Contains(string(raw), "first evidence") {
		t.Fatalf("earlier evidence lost: %s %v", raw, err)
	}
	alias, _ := os.ReadFile(path)
	if !strings.Contains(string(alias), "second target") {
		t.Fatal("latest alias not updated")
	}
	stat, _ := os.Stat(before)
	if runtime.GOOS != "windows" && stat.Mode().Perm() != 0o600 {
		t.Fatalf("snapshot permissions: %v", stat.Mode())
	}
	files, _ := filepath.Glob(filepath.Join(filepath.Dir(before), "transcript-*.txt"))
	if len(files) != 2 {
		t.Fatalf("retries duplicated snapshots: %v", files)
	}
}

func checkpointTestProject(t *testing.T) (*database.DB, string, string) {
	t.Helper()
	db, err := database.NewDB(filepath.Join(t.TempDir(), "test.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	proj, err := db.CreateProject(&database.Project{Name: "memory"})
	if err != nil {
		t.Fatal(err)
	}
	conv, err := db.CreateConversation("test", database.ConversationCreateMeta{ProjectID: proj.ID})
	if err != nil {
		t.Fatal(err)
	}
	return db, proj.ID, conv.ID
}

func TestContextCheckpointsSeparateAgentsAndPreserveConfirmedFacts(t *testing.T) {
	db, pid, cid := checkpointTestProject(t)
	cfg := config.ProjectConfig{Enabled: true}
	db.UpsertProjectFact(&database.ProjectFact{ProjectID: pid, FactKey: "finding/original", Category: "finding", Confidence: "confirmed", Body: "original evidence", Summary: "confirmed result", Pinned: true})
	main, err := persistSummarizationCheckpoint(db, pid, cid, "main", "first summary", "/old/snapshot", cfg)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := persistSummarizationCheckpoint(db, pid, cid, "main", "new summary", "/new/snapshot", cfg)
	if err != nil || updated != main {
		t.Fatalf("unstable key: %s %s %v", main, updated, err)
	}
	sub, err := persistSummarizationCheckpoint(db, pid, cid, "sub/scanner", "sub findings", "/sub/snapshot", cfg)
	if err != nil || sub == main {
		t.Fatalf("subagent overwrites coordinator: %v", err)
	}
	fact, _ := db.GetProjectFactByKey(pid, main)
	if fact.Confidence != "tentative" || fact.Pinned || fact.SourceConversationID != cid || !strings.Contains(fact.Body, "new summary") || !strings.Contains(fact.Body, "/new/snapshot") {
		t.Fatalf("invalid checkpoint: %#v", fact)
	}
	existing, _ := db.GetProjectFactByKey(pid, "finding/original")
	if existing.Confidence != "confirmed" || existing.Body != "original evidence" || !existing.Pinned {
		t.Fatalf("confirmed fact altered: %#v", existing)
	}
	facts, err := db.ListProjectFacts(pid, database.ProjectFactListFilter{}, 100, 0)
	if err != nil || len(facts) != 3 {
		t.Fatalf("checkpoint updates grow fact list: %d %v", len(facts), err)
	}
}

func TestContextCheckpointDisabledAndProjectMismatch(t *testing.T) {
	db, pid, cid := checkpointTestProject(t)
	off := false
	for _, cfg := range []config.ProjectConfig{{Enabled: false}, {Enabled: true, ContextCheckpoints: &off}} {
		key, err := persistSummarizationCheckpoint(db, pid, cid, "main", "summary", "", cfg)
		if err != nil || key != "" {
			t.Fatalf("disabled checkpoint wrote data: %s %v", key, err)
		}
	}
	if key, err := persistSummarizationCheckpoint(db, "other-project", cid, "main", "summary", "", config.ProjectConfig{Enabled: true}); err == nil || key != "" {
		t.Fatalf("wrong-project write: %s %v", key, err)
	}
}

func TestFinalizeMemorySupportsMultipartSummaryAndReportsOnlySuccessfulWrites(t *testing.T) {
	db, pid, cid := checkpointTestProject(t)
	original := []adk.Message{schema.UserMessage("exact evidence from history")}
	summary := &schema.Message{Role: schema.User, UserInputMultiContent: []schema.MessageInputPart{{Type: schema.ChatMessagePartTypeText, Text: "<analysis>internal</analysis><summary>keep important finding</summary>"}}}
	out := finalizeSummarizationMemory(summary, original, filepath.Join(t.TempDir(), "transcript.txt"), db, pid, cid, "main", config.ProjectConfig{Enabled: true}, nil)
	text := out.UserInputMultiContent[0].Text
	if strings.Contains(text, "internal") || !strings.Contains(text, filepath.Join("snapshots", "transcript-")) || !strings.Contains(text, "get_project_fact(note/context-") {
		t.Fatalf("continuation missing durable references: %s", text)
	}
	if !strings.Contains(summary.UserInputMultiContent[0].Text, "internal") {
		t.Fatal("input summary was mutated")
	}
	bad := finalizeSummarizationMemory(schema.UserMessage("still usable summary"), original, "", db, "wrong-project", cid, "main", config.ProjectConfig{Enabled: true}, nil)
	if strings.Contains(bad.Content, "已保存") || !strings.Contains(bad.Content, "still usable summary") {
		t.Fatalf("failed write claimed success or lost summary: %s", bad.Content)
	}
}

func TestSummarizationMiddlewaresAutomaticallySaveCheckpoint(t *testing.T) {
	for _, native := range []bool{false, true} {
		t.Run(fmt.Sprintf("native=%v", native), func(t *testing.T) {
			db, pid, cid := checkpointTestProject(t)
			_, err := db.UpsertProjectFact(&database.ProjectFact{ProjectID: pid, FactKey: "note/context-other", SourceConversationID: "different-conversation", Summary: "FOREIGN_PROGRESS", Body: "FOREIGN_COOKIE", Pinned: true})
			if err != nil {
				t.Fatal(err)
			}
			cfg := &config.Config{Project: config.ProjectConfig{Enabled: true}}
			cfg.Database.Path = filepath.Join(t.TempDir(), "runtime.db")
			cfg.OpenAI.Model = "gpt-4o"
			cfg.OpenAI.MaxTotalTokens = 5000
			emit := false
			mwCfg := &config.MultiAgentEinoMiddlewareConfig{SummarizationEmitInternalEvents: &emit, SummarizationOutputReserveTokens: 1024}
			index, err := project.BuildFactIndexBlock(db, pid, cfg.Project)
			if err != nil {
				t.Fatal(err)
			}
			originals := []adk.Message{
				schema.SystemMessage("system root\n" + index),
				schema.UserMessage("precise original evidence\n" + strings.Repeat("old scan output ", 12000)),
				schema.AssistantMessage("found endpoint", nil),
				schema.UserMessage("continue from confirmed endpoint"),
			}
			const summary = "<analysis>do not persist scratchpad</analysis><summary>Important endpoint /api/check; next validate response</summary>"
			var after []adk.Message
			ctx := context.Background()
			if native {
				m := &capturingAgenticChatModel{output: agenticAssistantTextMessage(summary)}
				mw, err := newEinoAgenticSummarizationMiddleware(ctx, m, cfg, mwCfg, cid, db, pid, nil)
				if err != nil {
					t.Fatal(err)
				}
				_, state, err := mw.BeforeModelRewriteState(ctx, &adk.TypedChatModelAgentState[*schema.AgenticMessage]{Messages: EinoMessagesToAgentic(originals)}, nil)
				if err != nil {
					t.Fatal(err)
				}
				after = AgenticMessagesToEino(state.Messages)
			} else {
				m := &capturingClassicChatModel{output: &schema.Message{Role: schema.Assistant, Content: summary, ResponseMeta: &schema.ResponseMeta{FinishReason: "stop"}}}
				mw, err := newEinoSummarizationMiddleware(ctx, m, cfg, mwCfg, cid, db, pid, nil)
				if err != nil {
					t.Fatal(err)
				}
				_, state, err := mw.BeforeModelRewriteState(ctx, &adk.ChatModelAgentState{Messages: originals}, nil)
				if err != nil {
					t.Fatal(err)
				}
				after = state.Messages
			}
			facts, err := db.ListProjectFacts(pid, database.ProjectFactListFilter{}, 100, 0)
			if err != nil || len(facts) != 2 {
				t.Fatalf("automatic checkpoint missing: %v %#v", err, facts)
			}
			var checkpoint *database.ProjectFact
			for _, f := range facts {
				if f.SourceConversationID == cid {
					checkpoint = f
				}
			}
			if checkpoint == nil || !strings.Contains(checkpoint.Body, "/api/check") || strings.Contains(checkpoint.Body, "scratchpad") {
				t.Fatalf("wrong working memory: %#v", checkpoint)
			}
			joined := joinClassicMessageContent(after)
			if !strings.Contains(joined, checkpoint.FactKey) || strings.Contains(joined, "（暂无事实）") || strings.Contains(joined, "FOREIGN_PROGRESS") || strings.Contains(joined, "note/context-other") {
				t.Fatalf("live index did not refresh after save: %s", joined)
			}
			snapshots, err := filepath.Glob(filepath.Join(filepath.Dir(cfg.Database.Path), "conversation_artifacts", cid, "summarization", "snapshots", "*.txt"))
			if err != nil || len(snapshots) != 1 {
				t.Fatalf("one compaction should reuse one original transcript across preflight/finalize/callback: %v %v", snapshots, err)
			}
		})
	}
}

func TestCheckpointIndexLabelStaysShortWithLargeSummarySetting(t *testing.T) {
	label := checkpointSummaryLabel("main", "## findings\n- Important endpoint /api/check\n- "+strings.Repeat("raw output ", 1000), 24000)
	if len([]rune(label)) > 200 || !strings.Contains(label, "/api/check") {
		t.Fatalf("checkpoint reinjects long history or loses key hint: %s", label)
	}
}

func TestSuccessfulEmptyFactIndexRemovesOldBlockButRetainsLedger(t *testing.T) {
	db, pid, cid := checkpointTestProject(t)
	if _, err := db.UpsertProjectFact(&database.ProjectFact{ProjectID: pid, FactKey: "finding/test", Summary: "OLD_INDEX_CONTENT"}); err != nil {
		t.Fatal(err)
	}
	index, err := project.BuildFactIndexBlock(db, pid, config.ProjectConfig{Enabled: true}, cid)
	if err != nil {
		t.Fatal(err)
	}
	ledger := buildOriginalUserIntentLedgerMessage([]adk.Message{schema.UserMessage("ONLY_AUTHORIZED_TARGET")}, 96000, 16000)
	msgs := mergeMessageIntoLeadingSystem([]adk.Message{schema.SystemMessage("KEEP_CURRENT_SYSTEM\n" + index + "\nKEEP_SUFFIX")}, ledger)
	out := refreshFactIndexInMessages(msgs, db, pid, config.ProjectConfig{Enabled: true, FactIndexMaxRunes: 30}, nil, cid)
	if strings.Contains(out[0].Content, project.FactIndexSectionStartMarker) || strings.Contains(out[0].Content, "OLD_INDEX_CONTENT") || !strings.Contains(out[0].Content, "KEEP_CURRENT_SYSTEM") || !strings.Contains(out[0].Content, "KEEP_SUFFIX") {
		t.Fatalf("empty build retained index or removed other context: %s", out[0].Content)
	}
	got := buildOriginalUserIntentLedgerMessage(out, 96000, 16000)
	if got == nil || !strings.Contains(got.Content, "ONLY_AUTHORIZED_TARGET") {
		t.Fatal("ledger lost on index removal")
	}
	// A failed build is different: retain the valid prior block.
	failed := refreshFactIndexInMessages(msgs, db, "missing-project", config.ProjectConfig{Enabled: true, FactIndexMaxRunes: 30}, nil, cid)
	if failed[0].Content != msgs[0].Content {
		t.Fatal("database error discarded old context")
	}
	if !strings.Contains(msgs[0].Content, "OLD_INDEX_CONTENT") {
		t.Fatal("input message mutated")
	}
}

func TestCheckpointLabelUsesExistingEvidenceCluesWithoutChangingSummary(t *testing.T) {
	const summary = "## 1. 授权范围与约束\n- authorized.example\n- COOKIE_SECRET\n- user method\n- user todo\n## 7. 当前进度、策略决策与下一步\n- progress\n## 8. 原始证据检索线索（追加）\n- authorized.example /login request Cookie clue\n- response 403 exact error\n"
	label := checkpointSummaryLabel("main", summary, 10000)
	if !strings.Contains(label, "/login") || !strings.Contains(label, "403") || strings.Contains(label, "COOKIE_SECRET") || strings.Contains(label, "user method") || len([]rune(label)) > 200 {
		t.Fatalf("unhelpful or excessive index label: %s", label)
	}
	db, pid, cid := checkpointTestProject(t)
	key, err := persistSummarizationCheckpoint(db, pid, cid, "main", summary, "", config.ProjectConfig{Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	fact, err := db.GetProjectFactByKey(pid, key)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(fact.Body, summary) {
		t.Fatal("index label changed original summary body")
	}
	fallback := checkpointSummaryLabel("main", "## 1. 授权范围\n- authorized.example\n## 8. 原始证据检索线索（追加）\n- 无", 1000)
	if !strings.Contains(fallback, "authorized.example") {
		t.Fatalf("empty-clue legacy fallback lost: %s", fallback)
	}
}
