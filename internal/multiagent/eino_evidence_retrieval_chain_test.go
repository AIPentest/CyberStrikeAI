package multiagent

import (
	"context"
	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/config"
	"encoding/json"
	"fmt"
	localbk "github.com/cloudwego/eino-ext/adk/backend/local"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/filesystem"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestCookieRecoveryThroughReductionSnapshotAndResume(t *testing.T) {
	if err := InitADK(); err != nil {
		t.Fatal(err)
	}
	defer adk.SetLanguage(adk.LanguageEnglish)
	for _, native := range []bool{false, true} {
		t.Run(fmt.Sprintf("native=%v", native), func(t *testing.T) {
			ctx := context.Background()
			loc, err := localbk.NewBackend(ctx, &localbk.Config{})
			if err != nil {
				t.Fatal(err)
			}
			redCfg := config.MultiAgentEinoMiddlewareConfig{ReductionRootDir: t.TempDir(), ReductionMaxLengthForTrunc: 512}
			const cookie = "AUDIT_SYNTHETIC_COOKIE_1234_NO_REAL_SECRET"
			raw := strings.Repeat("noise line without credentials\n", 1000) + "Set-Cookie: sid=" + cookie + "; Domain=authorized.example\n" + strings.Repeat("noise line without credentials\n", 1000)
			endpoint := func(context.Context, string, ...tool.Option) (string, error) { return raw, nil }
			var wrapped adk.InvokableToolCallEndpoint
			if native {
				mw, err := buildAgenticReductionMiddleware(ctx, redCfg, "", "audit-conversation", loc, nil)
				if err != nil {
					t.Fatal(err)
				}
				wrapped, err = mw.WrapInvokableToolCall(ctx, endpoint, &adk.ToolContext{Name: "execute", CallID: "audit-login"})
				if err != nil {
					t.Fatal(err)
				}
			} else {
				mw, err := buildReductionMiddleware(ctx, redCfg, "", "audit-conversation", loc, nil)
				if err != nil {
					t.Fatal(err)
				}
				wrapped, err = mw.WrapInvokableToolCall(ctx, endpoint, &adk.ToolContext{Name: "execute", CallID: "audit-login"})
				if err != nil {
					t.Fatal(err)
				}
			}
			notice, err := wrapped(ctx, `{"command":"synthetic authorized.example login response"}`)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(notice, "<persisted-output>") || strings.Contains(notice, cookie) {
				t.Fatalf("reduction not isolating middle credential: %s", notice)
			}
			findPath := regexp.MustCompile(`(?m)(?:Full output saved to: |完整输出保存到: )([^\n]+)`)
			match := findPath.FindStringSubmatch(notice)
			if len(match) != 2 {
				t.Fatalf("missing spill reference: %s", notice)
			}
			spill := match[1]
			cfg := &config.Config{}
			cfg.Database.Path = filepath.Join(t.TempDir(), "runtime.db")
			cfg.OpenAI.Model = "gpt-4o"
			cfg.OpenAI.MaxTotalTokens = 5000
			emit := false
			sumCfg := &config.MultiAgentEinoMiddlewareConfig{SummarizationEmitInternalEvents: &emit, SummarizationOutputReserveTokens: 1024}
			original := []adk.Message{schema.SystemMessage("fresh system"), schema.UserMessage("Only authorized.example; find exact Cookie if needed"), &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{ID: "audit-login", Type: "function", Function: schema.FunctionCall{Name: "execute", Arguments: `{"command":"synthetic login"}`}}}}, &schema.Message{Role: schema.Tool, ToolCallID: "audit-login", Content: notice}, schema.AssistantMessage(strings.Repeat("intermediate completed finding ", 15000), nil), schema.UserMessage("continue with missing authentication detail")}
			const summary = "<summary>Login completed on authorized.example.\n## 8. 原始证据检索线索（追加）\n- authorized.example login Set-Cookie response</summary>"
			var after []adk.Message
			if native {
				mw, err := newEinoAgenticSummarizationMiddleware(ctx, &capturingAgenticChatModel{output: agenticAssistantTextMessage(summary)}, cfg, sumCfg, "audit-conversation", nil, "", nil)
				if err != nil {
					t.Fatal(err)
				}
				_, s, err := mw.BeforeModelRewriteState(ctx, &adk.TypedChatModelAgentState[*schema.AgenticMessage]{Messages: EinoMessagesToAgentic(original)}, nil)
				if err != nil {
					t.Fatal(err)
				}
				after = AgenticMessagesToEino(s.Messages)
			} else {
				mw, err := newEinoSummarizationMiddleware(ctx, &capturingClassicChatModel{output: &schema.Message{Role: schema.Assistant, Content: summary, ResponseMeta: &schema.ResponseMeta{FinishReason: "stop"}}}, cfg, sumCfg, "audit-conversation", nil, "", nil)
				if err != nil {
					t.Fatal(err)
				}
				_, s, err := mw.BeforeModelRewriteState(ctx, &adk.ChatModelAgentState{Messages: original}, nil)
				if err != nil {
					t.Fatal(err)
				}
				after = s.Messages
			}
			if strings.Contains(joinClassicMessageContent(after), cookie) {
				t.Fatal("test requires cookie omitted from compacted context")
			}
			encoded, err := json.Marshal(markModelFacingTraceForPersistence(after))
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := agent.ParseTraceMessages(string(encoded))
			if err != nil {
				t.Fatal(err)
			}
			recovered := historyToMessages(agent.TraceMessagesForResume(parsed), nil, nil)
			resumedText := joinClassicMessageContent(recovered)
			snapshotExpr := regexp.MustCompile(`(?m)full conversation record is located at: ([^\n]+)`)
			snapshotMatch := snapshotExpr.FindStringSubmatch(resumedText)
			if len(snapshotMatch) != 2 {
				t.Fatalf("snapshot hint lost after resume: %s", resumedText)
			}
			snapshotContent, err := loc.Read(ctx, &filesystem.ReadRequest{FilePath: snapshotMatch[1], Offset: 1, Limit: 40})
			if err != nil {
				t.Fatal(err)
			}
			recoveredSpill := findPath.FindStringSubmatch(snapshotContent.Content)
			if len(recoveredSpill) != 2 || recoveredSpill[1] != spill {
				t.Fatalf("spill reference lost in snapshot: %s", snapshotContent.Content)
			}
			fullLine, err := loc.Read(ctx, &filesystem.ReadRequest{FilePath: recoveredSpill[1], Offset: 1001, Limit: 1})
			if err != nil {
				t.Fatal(err)
			}
			if fullLine.Content != "Set-Cookie: sid="+cookie+"; Domain=authorized.example" {
				t.Fatalf("exact cookie recovery failed: %q", fullLine.Content)
			}
			t.Log("real reduction -> actual compaction -> persisted resume -> snapshot read -> raw spill read restored exact synthetic Cookie")
		})
	}
}
