package multiagent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/project"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/summarization"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// fixedTokenCounter 让 tool 消息按 tokensPerToolMessage 计，其它消息按 1 计。
// 用于验证 tool-round 超预算时整体被跳过的分支。
func fixedTokenCounter(tokensPerToolMessage int) summarization.TokenCounterFunc {
	return func(_ context.Context, in *summarization.TokenCounterInput) (int, error) {
		total := 0
		for _, msg := range in.Messages {
			if msg == nil {
				continue
			}
			switch msg.Role {
			case schema.Tool:
				total += tokensPerToolMessage
			default:
				total++
			}
		}
		return total, nil
	}
}

// variableTokenCounter 让 tool 消息按 len(Content) 计（可区分不同大小的 tool 结果），
// 其它消息按 1 计；assistant 附加 len(ToolCalls) token 近似 tool_calls schema 开销。
func variableTokenCounter() summarization.TokenCounterFunc {
	return func(_ context.Context, in *summarization.TokenCounterInput) (int, error) {
		total := 0
		for _, msg := range in.Messages {
			if msg == nil {
				continue
			}
			if msg.Role == schema.Tool {
				total += len(msg.Content)
				continue
			}
			total++
			total += len(msg.ToolCalls)
		}
		return total, nil
	}
}

func TestBuildBudgetedSummarizationModelInputKeepsRecentCompleteRounds(t *testing.T) {
	msgs := []adk.Message{
		schema.UserMessage("old-user"),
		schema.AssistantMessage("old-answer", nil),
		schema.UserMessage("latest-user"),
		assistantToolCallsMsg("", "call-latest"),
		schema.ToolMessage("latest-tool-result", "call-latest"),
	}
	input, dropped, err := buildBudgetedSummarizationModelInput(
		context.Background(), schema.SystemMessage("summary-system"), schema.UserMessage("summary-instruction"),
		msgs, fixedTokenCounter(2), 7, summarizationInputBudgetOpts{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if dropped == 0 {
		t.Fatal("expected older rounds to be omitted")
	}
	joined := formatSummarizationTranscript(input)
	if strings.Contains(joined, "old-user") || strings.Contains(joined, "old-answer") {
		t.Fatalf("old rounds leaked into bounded input: %s", joined)
	}
	if !strings.Contains(joined, "latest-user") || !strings.Contains(joined, "latest-tool-result") {
		t.Fatalf("latest complete rounds missing: %s", joined)
	}
	for _, msg := range input {
		if msg.Role == schema.Tool || len(msg.ToolCalls) > 0 {
			t.Fatalf("summary input must use inert plaintext history, got role=%s tool_calls=%d", msg.Role, len(msg.ToolCalls))
		}
	}
}

func TestBuildBudgetedSummarizationModelInputNeutralizesMalformedToolCallProtocol(t *testing.T) {
	call := assistantToolCallsMsg("", "broken")
	call.ToolCalls[0].Function.Arguments = `{"command":"unterminated`
	input, _, err := buildBudgetedSummarizationModelInput(
		context.Background(), schema.SystemMessage("summary-system"), schema.UserMessage("summary-instruction"),
		[]adk.Message{schema.UserMessage("run it"), call, schema.ToolMessage("parse failed", "broken")},
		einoSummarizationTokenCounter("gpt-4o"), 4096, summarizationInputBudgetOpts{},
	)
	if err != nil {
		t.Fatal(err)
	}
	joined := formatSummarizationTranscript(input)
	if !strings.Contains(joined, `unterminated`) || !strings.Contains(joined, "parse failed") {
		t.Fatalf("historical evidence missing from plaintext transcript: %s", joined)
	}
	for _, msg := range input {
		if msg.Role == schema.Tool || len(msg.ToolCalls) != 0 {
			t.Fatalf("provider-visible tool protocol leaked into summary input: %+v", msg)
		}
	}
}

func TestSplitMessagesIntoRounds_Complex(t *testing.T) {
	msgs := []adk.Message{
		schema.UserMessage("q1"),
		assistantToolCallsMsg("", "c1", "c2"),
		schema.ToolMessage("r1", "c1"),
		schema.ToolMessage("r2", "c2"),
		schema.AssistantMessage("reply1", nil),
		schema.UserMessage("q2"),
		assistantToolCallsMsg("", "c3"),
		schema.ToolMessage("r3", "c3"),
	}
	rounds := splitMessagesIntoRounds(msgs)
	// 5 rounds: user(q1) | assistant(tc:c1,c2)+tool*2 | assistant(reply1) | user(q2) | assistant(tc:c3)+tool(c3)
	if len(rounds) != 5 {
		t.Fatalf("want 5 rounds, got %d", len(rounds))
	}
	// round 1 应为 tool-round，必须成对
	r1 := rounds[1]
	if len(r1.messages) != 3 {
		t.Fatalf("rounds[1] size: want 3, got %d", len(r1.messages))
	}
	if r1.messages[0].Role != schema.Assistant || len(r1.messages[0].ToolCalls) != 2 {
		t.Fatalf("rounds[1][0] must be assistant(tc=2)")
	}
	for i := 1; i < 3; i++ {
		if r1.messages[i].Role != schema.Tool {
			t.Fatalf("rounds[1][%d] must be tool, got %s", i, r1.messages[i].Role)
		}
	}
	// 最后一个 round 成对
	rLast := rounds[len(rounds)-1]
	if len(rLast.messages) != 2 {
		t.Fatalf("rounds[last] size: want 2, got %d", len(rLast.messages))
	}
	if rLast.messages[0].Role != schema.Assistant || rLast.messages[1].Role != schema.Tool {
		t.Fatalf("last round must be assistant(tc)+tool(c3)")
	}
}

func TestSplitMessagesIntoRounds_DropsOrphanTool(t *testing.T) {
	// 起点直接是 tool 消息（孤儿）—— 应被丢弃，不独立成 round。
	msgs := []adk.Message{
		schema.ToolMessage("orphan", "c_old"),
		schema.UserMessage("continue"),
		assistantToolCallsMsg("", "c_new"),
		schema.ToolMessage("r_new", "c_new"),
	}
	rounds := splitMessagesIntoRounds(msgs)
	// user(continue) | assistant(tc:c_new)+tool(c_new) → 2 rounds
	if len(rounds) != 2 {
		t.Fatalf("want 2 rounds after dropping orphan, got %d", len(rounds))
	}
	for _, r := range rounds {
		for _, m := range r.messages {
			if m.Role == schema.Tool && m.ToolCallID == "c_old" {
				t.Fatalf("orphan tool c_old must not appear in any round")
			}
		}
	}
}

func TestSplitMessagesIntoRounds_ToolBelongsToCurrentAssistantOnly(t *testing.T) {
	// 两个相邻 assistant(tc)，第二个的 tool 不应被归到第一个 assistant。
	msgs := []adk.Message{
		assistantToolCallsMsg("", "c1"),
		schema.ToolMessage("r1", "c1"),
		assistantToolCallsMsg("", "c2"),
		schema.ToolMessage("r2", "c2"),
	}
	rounds := splitMessagesIntoRounds(msgs)
	if len(rounds) != 2 {
		t.Fatalf("want 2 rounds, got %d", len(rounds))
	}
	if len(rounds[0].messages) != 2 || rounds[0].messages[0].ToolCalls[0].ID != "c1" {
		t.Fatalf("round[0] wrong: %+v", rounds[0].messages)
	}
	if len(rounds[1].messages) != 2 || rounds[1].messages[0].ToolCalls[0].ID != "c2" {
		t.Fatalf("round[1] wrong: %+v", rounds[1].messages)
	}
}

func TestSplitMessagesIntoRounds_ToolBelongsToWrongAssistant(t *testing.T) {
	// assistant(tc:c1) 后面跟一个 tool_call_id=c999 的 tool 消息（本不属它）。
	// 切分规则：该 tool 不应拼入第一个 round（配对不完整），round 在此结束。
	// 而 c999 又没有对应 assistant，应被当孤儿丢弃。
	msgs := []adk.Message{
		assistantToolCallsMsg("", "c1"),
		schema.ToolMessage("wrong", "c999"),
		schema.UserMessage("hi"),
	}
	rounds := splitMessagesIntoRounds(msgs)
	// assistant(tc:c1) 没有对应 tool(c1)，但不是孤儿（patchtoolcalls 会兜底补）；
	// 它独立成 round 允许上游后处理。user(hi) 独立成 round。共 2 rounds。
	if len(rounds) != 2 {
		t.Fatalf("want 2 rounds, got %d: %+v", len(rounds), rounds)
	}
	for _, r := range rounds {
		for _, m := range r.messages {
			if m.Role == schema.Tool && m.ToolCallID == "c999" {
				t.Fatalf("wrong-owner tool must be dropped as orphan")
			}
		}
	}
}

func TestSummarizeFinalize_KeepsToolRoundIntact(t *testing.T) {
	// 关键回归测试：一个 tool-round 整体被保留，而不是只保留 tool 消息。
	sys := schema.SystemMessage("sys")
	summary := schema.AssistantMessage("summary_content", nil)
	msgs := []adk.Message{
		sys,
		schema.UserMessage("q1"),
		schema.AssistantMessage("reply_before_tc", nil), // 填料，占预算
		assistantToolCallsMsg("", "c1"),
		schema.ToolMessage("r1", "c1"),
	}

	// token 预算：2 条消息（1 assistant + 1 tool）恰好够用。
	// 若按条数保留，可能先吃 tool(c1) 再吃 assistant(reply) 落入 budget，assistant(tc:c1) 被挤掉，导致孤儿。
	// 按 round 保留时，整个 tool-round 为原子，要么保留 2 条都在，要么都不在。
	out, err := summarizeFinalizeWithRecentAssistantToolTrail(
		context.Background(),
		msgs,
		summary,
		fixedTokenCounter(1),
		2, // 预算：2 tokens
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 必须包含 system + summary
	if len(out) < 2 {
		t.Fatalf("output too short: %d", len(out))
	}
	if out[0].Role != schema.System || out[0].Content != "sys" {
		t.Fatalf("first message must be system sys, got %s: %q", out[0].Role, out[0].Content)
	}
	if out[1] != summary {
		t.Fatalf("second message must be summary")
	}

	// 关键不变量：每个被保留的 tool 消息，必须能在输出中找到提供其 ToolCallID 的 assistant(tc)。
	assertNoOrphanTool(t, out)
}

func TestSummarizeFinalize_SkipsOversizedToolRoundButKeepsSmallerRound(t *testing.T) {
	// 构造两个大小差异显著的 tool-round：
	//   c_big round 的 tool 结果 content="aaaaaaaaaa"（10 bytes），round token ≈ 2 (assistant+tc) + 10 = 12
	//   c_ok  round 的 tool 结果 content="ok"（2 bytes），round token ≈ 2 + 2 = 4
	// 配上 budget=8，使得：
	//   - 最新的 c_ok round（4）能放下；
	//   - 进一步的中间 round（assistant reply + user）也能放下；
	//   - 更早的 c_big round（12）放不下会被跳过（continue），而非 break。
	sys := schema.SystemMessage("sys")
	summary := schema.AssistantMessage("summary_content", nil)
	msgs := []adk.Message{
		sys,
		schema.UserMessage("q1"),
		assistantToolCallsMsg("", "c_big"),
		schema.ToolMessage("aaaaaaaaaa", "c_big"),
		schema.AssistantMessage("s", nil),
		schema.UserMessage("q2"),
		assistantToolCallsMsg("", "c_ok"),
		schema.ToolMessage("ok", "c_ok"),
	}

	out, err := summarizeFinalizeWithRecentAssistantToolTrail(
		context.Background(),
		msgs,
		summary,
		variableTokenCounter(),
		8,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertNoOrphanTool(t, out)

	// c_big 整个 round 必须被丢弃（tool 和 assistant 都不能出现）
	for _, m := range out {
		if m == nil {
			continue
		}
		if m.Role == schema.Tool && m.ToolCallID == "c_big" {
			t.Fatal("oversized tool round must be skipped: tool(c_big) leaked")
		}
		if m.Role == schema.Assistant {
			for _, tc := range m.ToolCalls {
				if tc.ID == "c_big" {
					t.Fatal("oversized tool round must be skipped: assistant(tc:c_big) leaked")
				}
			}
		}
	}

	// 最近 round (c_ok) 作为一个原子单位必须整体保留。
	foundOKTool, foundOKAsst := false, false
	for _, m := range out {
		if m == nil {
			continue
		}
		if m.Role == schema.Tool && m.ToolCallID == "c_ok" {
			foundOKTool = true
		}
		if m.Role == schema.Assistant {
			for _, tc := range m.ToolCalls {
				if tc.ID == "c_ok" {
					foundOKAsst = true
				}
			}
		}
	}
	if !foundOKTool || !foundOKAsst {
		t.Fatalf("recent tool-round (c_ok) must be retained as an atomic pair: assistantKept=%v toolKept=%v", foundOKAsst, foundOKTool)
	}
}

func TestSummarizeFinalize_BudgetZeroFallsBackToSummaryOnly(t *testing.T) {
	sys := schema.SystemMessage("sys")
	summary := schema.AssistantMessage("summary", nil)
	msgs := []adk.Message{
		sys,
		assistantToolCallsMsg("", "c1"),
		schema.ToolMessage("r1", "c1"),
	}
	out, err := summarizeFinalizeWithRecentAssistantToolTrail(
		context.Background(),
		msgs,
		summary,
		fixedTokenCounter(1),
		0,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 2 || out[0].Role != schema.System || out[0].Content != "sys" || out[1] != summary {
		t.Fatalf("budget=0 must yield [system, summary] only, got %+v", out)
	}
}

func TestSummarizeFinalize_MergesSystemMessages(t *testing.T) {
	sys1 := schema.SystemMessage("sys1")
	sys2 := schema.SystemMessage("sys2")
	summary := schema.AssistantMessage("s", nil)
	msgs := []adk.Message{
		sys1,
		schema.UserMessage("q"),
		sys2, // 非典型位置，但应当被 system group 捕获
	}
	out, err := summarizeFinalizeWithRecentAssistantToolTrail(
		context.Background(),
		msgs,
		summary,
		fixedTokenCounter(1),
		100,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	systemCount := 0
	for _, m := range out {
		if m != nil && m.Role == schema.System {
			systemCount++
			if got := m.Content; got != "sys1\n\nsys2" {
				t.Fatalf("unexpected merged system content: %q", got)
			}
		}
	}
	if systemCount != 1 {
		t.Fatalf("want 1 merged system message, got %d", systemCount)
	}
}

func TestEinoSummarizationMiddlewareRetriesWhenSummaryModelReturnsEmpty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	emit := false
	summaryModel := &capturingClassicChatModel{outputs: []*schema.Message{
		schema.AssistantMessage("", nil),
		{Role: schema.Assistant, Content: "<summary>有效摘要：继续验证 SQL 注入路径</summary>", ResponseMeta: &schema.ResponseMeta{FinishReason: "stop"}},
	}}
	appCfg := &config.Config{}
	appCfg.OpenAI.Model = "gpt-4o"
	appCfg.OpenAI.MaxTotalTokens = 5000
	appCfg.Database.Path = filepath.Join(t.TempDir(), "cyberstrike.db")
	mwCfg := &config.MultiAgentEinoMiddlewareConfig{
		SummarizationEmitInternalEvents:  &emit,
		SummarizationOutputReserveTokens: 1024,
	}

	mw, err := newEinoSummarizationMiddleware(ctx, summaryModel, appCfg, mwCfg, "conv-empty-summary", nil, "", nil)
	if err != nil {
		t.Fatalf("newEinoSummarizationMiddleware: %v", err)
	}
	state := &adk.ChatModelAgentState{Messages: []adk.Message{
		schema.SystemMessage("system root"),
		schema.UserMessage("授权范围 example.com\n" + strings.Repeat("历史扫描输出 ", 12000)),
		schema.AssistantMessage("已记录范围", nil),
		schema.UserMessage("继续验证 SQL 注入路径"),
	}}

	_, after, err := mw.BeforeModelRewriteState(ctx, state, nil)
	if err != nil {
		t.Fatalf("BeforeModelRewriteState should retry instead of failing on empty summary: %v", err)
	}
	if after == nil {
		t.Fatal("after state is nil")
	}
	if summaryModel.calls < 2 {
		t.Fatalf("summary model calls=%d, want retry after empty output", summaryModel.calls)
	}
	joined := joinClassicMessageContent(after.Messages)
	for _, want := range []string{"有效摘要", "继续验证 SQL 注入路径", "原始用户输入与约束账本"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("retried compacted context missing %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "本地压缩摘要") {
		t.Fatalf("local fallback should not be used:\n%s", joined)
	}
}

type capturingClassicChatModel struct {
	output  *schema.Message
	outputs []*schema.Message
	calls   int
}

func (m *capturingClassicChatModel) Generate(_ context.Context, _ []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	m.calls++
	if len(m.outputs) > 0 {
		idx := m.calls - 1
		if idx >= len(m.outputs) {
			idx = len(m.outputs) - 1
		}
		return m.outputs[idx], nil
	}
	if m.output != nil {
		return m.output, nil
	}
	return schema.AssistantMessage("classic answer", nil), nil
}

func (m *capturingClassicChatModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	msg, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{msg}), nil
}

// assertNoOrphanTool 断言消息列表里的每个 role=tool 消息都能在更前面找到一个
// assistant(tool_calls) 提供相同 ID，否则说明产生了孤儿（触发 LLM 400 的根因）。
func assertNoOrphanTool(t *testing.T, msgs []adk.Message) {
	t.Helper()
	provided := make(map[string]struct{})
	for _, m := range msgs {
		if m == nil {
			continue
		}
		if m.Role == schema.Assistant {
			for _, tc := range m.ToolCalls {
				if tc.ID != "" {
					provided[tc.ID] = struct{}{}
				}
			}
		}
		if m.Role == schema.Tool && m.ToolCallID != "" {
			if _, ok := provided[m.ToolCallID]; !ok {
				t.Fatalf("orphan tool message found: ToolCallID=%q has no preceding assistant(tool_calls)", m.ToolCallID)
			}
		}
	}
}

func TestWriteSummarizationTranscript(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "summarization", "transcript.txt")
	msgs := []adk.Message{
		schema.UserMessage("scan target"),
		assistantToolCallsMsg("", "tc1"),
		schema.ToolMessage("nmap output", "tc1"),
	}
	if err := writeSummarizationTranscript(path, msgs); err != nil {
		t.Fatalf("writeSummarizationTranscript: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read transcript: %v", err)
	}
	text := string(body)
	if !strings.Contains(text, "Pre-compaction session record") {
		t.Fatalf("missing transcript header: %q", text)
	}
	if !strings.Contains(text, "[user]") || !strings.Contains(text, "scan target") {
		t.Fatalf("missing user section: %q", text)
	}
	if !strings.Contains(text, "tool_calls:") || !strings.Contains(text, "nmap output") {
		t.Fatalf("missing tool round: %q", text)
	}
	if !strings.Contains(text, `"name":"stub_tool"`) || !strings.Contains(text, `"arguments":"{}"`) {
		t.Fatalf("missing tool name/arguments: %q", text)
	}
	if strings.Contains(text, "tool_call_id") || strings.Contains(text, `"id":"tc1"`) {
		t.Fatalf("transcript should omit tool_call_id: %q", text)
	}
}

func TestSanitizeSystemContentForTranscript_BestPractice(t *testing.T) {
	t.Parallel()
	system := strings.Join([]string{
		"以下是当前会话绑定的工具名称索引（仅名称，无参数 JSON Schema）。",
		"- nmap",
		"- nuclei",
		"",
		"使用规则：",
		"1) 上表仅为名称索引",
		"5) 不要臆造不存在的工具名。",
		"",
		"你是CyberStrikeAI，是一个专业的网络安全渗透测试专家。",
		"高强度扫描要求：全力出击",
		"",
		project.FactIndexSectionStartMarker,
		"## 项目黑板索引（project: 123, id: abc）",
		"（暂无事实）",
		"需要写入请使用 upsert_project_fact。",
		project.FactIndexSectionEndMarker,
		"",
		transcriptSkillsSystemMarker,
		"**如何使用 Skill（技能）（渐进式展示）：**",
		"记住：Skill 让你更加强大和稳定",
	}, "\n")

	out := sanitizeSystemContentForTranscript(system)
	if strings.Contains(out, "以下是当前会话绑定的工具名称索引") {
		t.Fatalf("tool index should be stripped: %q", out)
	}
	if strings.Contains(out, "- nmap") || strings.Contains(out, "高强度扫描要求") {
		t.Fatalf("static persona should be stripped: %q", out)
	}
	if strings.Contains(out, transcriptSkillsSystemMarker) || strings.Contains(out, "如何使用 Skill") {
		t.Fatalf("skills boilerplate should be stripped: %q", out)
	}
	if !strings.Contains(out, transcriptStaticSystemOmitNote) {
		t.Fatalf("missing omission note: %q", out)
	}
	if !strings.Contains(out, "## 项目黑板索引（project: 123, id: abc）") {
		t.Fatalf("project blackboard should be kept: %q", out)
	}
}

func TestFormatSummarizationTranscript_OmitsBloatedSystem(t *testing.T) {
	t.Parallel()
	msgs := []adk.Message{
		schema.SystemMessage("以下是当前会话绑定的工具名称索引\n- nmap\n\n你是CyberStrikeAI\n" + project.FactIndexSectionStartMarker + "\n## 项目黑板索引（project: p1, id: x）\n（暂无事实）\n" + project.FactIndexSectionEndMarker + "\n" + transcriptSkillsSystemMarker + "\nboiler"),
		schema.UserMessage("hello"),
		schema.AssistantMessage("reply", nil),
	}
	out := formatSummarizationTranscript(msgs)
	if strings.Contains(out, "- nmap") {
		t.Fatalf("tool list leaked into transcript: %q", out)
	}
	if !strings.Contains(out, "hello") || !strings.Contains(out, "reply") {
		t.Fatalf("conversation turns missing: %q", out)
	}
	if !strings.Contains(out, "## 项目黑板索引（project: p1, id: x）") {
		t.Fatalf("dynamic blackboard missing: %q", out)
	}
}

func TestRefreshFactIndexInMessages(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "summarize-facts.db")
	db, err := database.NewDB(dbPath, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	proj, err := db.CreateProject(&database.Project{Name: "summarize-proj"})
	if err != nil {
		t.Fatal(err)
	}

	cfg := config.ProjectConfig{Enabled: true}
	oldIndex, err := project.BuildFactIndexBlock(db, proj.ID, cfg)
	if err != nil {
		t.Fatal(err)
	}

	_, err = db.UpsertProjectFact(&database.ProjectFact{
		ProjectID: proj.ID,
		FactKey:   "target/host",
		Category:  "target",
		Summary:   "fresh host fact",
	})
	if err != nil {
		t.Fatal(err)
	}

	msgs := []adk.Message{
		schema.SystemMessage("instruction\n\n" + oldIndex),
		schema.UserMessage("hi"),
	}

	out := refreshFactIndexInMessages(msgs, db, proj.ID, cfg, nil)
	sys := out[0].Content
	if strings.Contains(sys, "（暂无事实）") {
		t.Fatalf("expected refreshed index, got: %q", sys)
	}
	if !strings.Contains(sys, "fresh host fact") {
		t.Fatalf("expected new fact in index: %q", sys)
	}
	if !strings.Contains(sys, "instruction") {
		t.Fatalf("non-index system content should be preserved: %q", sys)
	}
}

func TestBuildOriginalUserIntentLedgerUsesOnlyModelFacingMessages(t *testing.T) {
	ledger := buildOriginalUserIntentLedgerMessage(
		[]adk.Message{schema.UserMessage("模型实际看到的裁剪预览")},
		config.DefaultSummarizationUserIntentLedgerMaxRunes,
		config.DefaultSummarizationUserIntentLedgerEntryMaxRunes,
	)
	if ledger == nil {
		t.Fatal("expected ledger message")
	}
	body := ledger.Content
	if !strings.Contains(body, "模型实际看到的裁剪预览") {
		t.Fatalf("ledger should preserve the model-facing user message: %q", body)
	}
}
