package multiagent

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"cyberstrike-ai/internal/config"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	schemaopenai "github.com/cloudwego/eino/schema/openai"
)

func TestNewEinoAgenticSummarizationMiddlewareCompactsWithNativeTypedMiddleware(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	emit := false
	summaryModel := &capturingAgenticChatModel{
		output: &schema.AgenticMessage{
			Role:         schema.AgenticRoleTypeAssistant,
			ResponseMeta: &schema.AgenticResponseMeta{OpenAIExtension: &schemaopenai.ResponseMetaExtension{Status: schemaopenai.ResponseStatusCompleted}},
			ContentBlocks: []*schema.ContentBlock{schema.NewContentBlock(&schema.AssistantGenText{Text: `<analysis>检查历史</analysis>
<summary>
## 1. 授权范围与约束
- 仅测试 example.com

## 7. 当前进度、策略决策与下一步
- 继续验证 SQL 注入路径
</summary>`})},
		},
	}
	appCfg := &config.Config{}
	appCfg.OpenAI.Model = "gpt-4o"
	appCfg.OpenAI.MaxTotalTokens = 5000
	appCfg.Database.Path = filepath.Join(t.TempDir(), "cyberstrike.db")
	mwCfg := &config.MultiAgentEinoMiddlewareConfig{
		SummarizationEmitInternalEvents:  &emit,
		SummarizationOutputReserveTokens: 1024,
	}

	mw, err := newEinoAgenticSummarizationMiddleware(ctx, summaryModel, appCfg, mwCfg, "conv-agentic", nil, "", nil)
	if err != nil {
		t.Fatalf("newEinoAgenticSummarizationMiddleware: %v", err)
	}
	state := &adk.TypedChatModelAgentState[*schema.AgenticMessage]{
		Messages: []*schema.AgenticMessage{
			schema.SystemAgenticMessage("system root"),
			schema.UserAgenticMessage("授权范围 example.com\n" + strings.Repeat("历史扫描输出 ", 12000)),
			agenticAssistantTextMessage("已记录范围"),
			schema.UserAgenticMessage("继续验证 SQL 注入路径"),
		},
	}

	_, after, err := mw.BeforeModelRewriteState(ctx, state, nil)
	if err != nil {
		t.Fatalf("BeforeModelRewriteState: %v", err)
	}
	inputs := summaryModel.snapshotInputs()
	if len(inputs) != 1 || len(inputs[0]) == 0 {
		t.Fatalf("summary model inputs = %#v, want one typed AgenticMessage call", inputs)
	}
	if after == nil {
		t.Fatal("after state is nil")
	}
	classicAfter := AgenticMessagesToEino(after.Messages)
	joined := joinClassicMessageContent(classicAfter)
	if strings.Contains(joined, "<analysis>") {
		t.Fatalf("analysis block leaked into compacted context: %s", joined)
	}
	for _, want := range []string{"继续验证 SQL 注入路径", "原始用户输入与约束账本", "完整的对话记录位于"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("compacted context missing %q:\n%s", want, joined)
		}
	}
}

func TestEinoAgenticChatModelAgentCompactsContextBeforeBusinessModel(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	emit := false
	summaryModel := &capturingAgenticChatModel{
		output: agenticAssistantTextMessage(`<analysis>internal scratchpad</analysis>
<summary>
## 1. 授权范围与约束
- 仅测试 example.com

## 7. 当前进度、策略决策与下一步
- 继续验证 SQL 注入路径
</summary>`),
	}
	businessModel := &capturingAgenticChatModel{
		output: agenticAssistantTextMessage("business answer after compaction"),
	}
	appCfg := &config.Config{}
	appCfg.OpenAI.Model = "gpt-4o"
	appCfg.OpenAI.MaxTotalTokens = 5000
	appCfg.Database.Path = filepath.Join(t.TempDir(), "cyberstrike.db")
	mwCfg := &config.MultiAgentEinoMiddlewareConfig{
		SummarizationEmitInternalEvents:  &emit,
		SummarizationOutputReserveTokens: 1024,
	}
	sumMw, err := newEinoAgenticSummarizationMiddleware(ctx, summaryModel, appCfg, mwCfg, "conv-agentic-e2e", nil, "", nil)
	if err != nil {
		t.Fatalf("newEinoAgenticSummarizationMiddleware: %v", err)
	}
	trace := newModelFacingTraceHolder()
	agent, err := newEinoAgenticChatModelAgentAdapter(ctx, einoAgenticChatModelAgentConfig{
		Name:        "agentic",
		Description: "agentic compaction e2e test",
		Instruction: "system root",
		Model:       businessModel,
		Handlers: appendEinoAgenticChatModelTailMiddlewares(nil, einoChatModelTailConfig{
			phase:                "agentic",
			agenticSummarization: sumMw,
			trace:                trace,
		}),
	})
	if err != nil {
		t.Fatalf("newEinoAgenticChatModelAgentAdapter: %v", err)
	}

	rawHistory := "授权范围 example.com\n" + strings.Repeat("原始扫描输出SHOULD_NOT_REACH_BUSINESS_MODEL ", 12000)
	iter := agent.Run(ctx, &adk.AgentInput{
		Messages: []*schema.Message{
			schema.UserMessage(rawHistory),
			schema.AssistantMessage("已记录范围", nil),
			schema.UserMessage("继续验证 SQL 注入路径"),
		},
	})
	var last *adk.AgentEvent
	for {
		ev, ok := iter.Next()
		if !ok {
			break
		}
		if ev.Err != nil {
			t.Fatalf("agent event error: %v", ev.Err)
		}
		last = ev
	}
	if last == nil || last.Output == nil || last.Output.MessageOutput == nil {
		t.Fatalf("last event = %#v, want message output", last)
	}
	if got := last.Output.MessageOutput.Message.Content; got != "business answer after compaction" {
		t.Fatalf("business output = %q", got)
	}

	if inputs := summaryModel.snapshotInputs(); len(inputs) != 1 {
		t.Fatalf("summary model calls = %d, want 1", len(inputs))
	}
	businessInputs := businessModel.snapshotInputs()
	if len(businessInputs) != 1 {
		t.Fatalf("business model calls = %d, want 1", len(businessInputs))
	}
	finalClassicInput := AgenticMessagesToEino(businessInputs[0])
	joined := joinClassicMessageContent(finalClassicInput)
	for _, want := range []string{"继续验证 SQL 注入路径", "原始用户输入与约束账本", "完整的对话记录位于"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("business model input missing %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "<analysis>") {
		t.Fatalf("analysis leaked to business model input:\n%s", joined)
	}
	if strings.Count(joined, "原始扫描输出SHOULD_NOT_REACH_BUSINESS_MODEL") > 3 {
		t.Fatalf("raw oversized history leaked to business model input, count=%d", strings.Count(joined, "原始扫描输出SHOULD_NOT_REACH_BUSINESS_MODEL"))
	}
	traceJoined := joinClassicMessageContent(trace.Snapshot())
	if !strings.Contains(traceJoined, "继续验证 SQL 注入路径") || strings.Count(traceJoined, "原始扫描输出SHOULD_NOT_REACH_BUSINESS_MODEL") > 3 {
		t.Fatalf("model-facing trace not compacted:\n%s", traceJoined)
	}
}

func TestEinoAgenticSummarizationMiddlewareRetriesWhenSummaryModelReturnsEmpty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	emit := false
	summaryModel := &capturingAgenticChatModel{
		outputs: []*schema.AgenticMessage{
			agenticAssistantTextMessage(""),
			agenticAssistantTextMessage("<summary>有效摘要：继续验证 SQL 注入路径</summary>"),
		},
	}
	appCfg := &config.Config{}
	appCfg.OpenAI.Model = "gpt-4o"
	appCfg.OpenAI.MaxTotalTokens = 5000
	appCfg.Database.Path = filepath.Join(t.TempDir(), "cyberstrike.db")
	mwCfg := &config.MultiAgentEinoMiddlewareConfig{
		SummarizationEmitInternalEvents:  &emit,
		SummarizationOutputReserveTokens: 1024,
	}

	mw, err := newEinoAgenticSummarizationMiddleware(ctx, summaryModel, appCfg, mwCfg, "conv-agentic-empty-summary", nil, "", nil)
	if err != nil {
		t.Fatalf("newEinoAgenticSummarizationMiddleware: %v", err)
	}
	state := &adk.TypedChatModelAgentState[*schema.AgenticMessage]{
		Messages: []*schema.AgenticMessage{
			schema.SystemAgenticMessage("system root"),
			schema.UserAgenticMessage("授权范围 example.com\n" + strings.Repeat("历史扫描输出 ", 12000)),
			agenticAssistantTextMessage("已记录范围"),
			schema.UserAgenticMessage("继续验证 SQL 注入路径"),
		},
	}

	_, after, err := mw.BeforeModelRewriteState(ctx, state, nil)
	if err != nil {
		t.Fatalf("BeforeModelRewriteState should retry instead of failing on empty summary: %v", err)
	}
	if after == nil {
		t.Fatal("after state is nil")
	}
	if inputs := summaryModel.snapshotInputs(); len(inputs) < 2 {
		t.Fatalf("summary model calls=%d, want retry after empty output", len(inputs))
	}
	joined := joinClassicMessageContent(AgenticMessagesToEino(after.Messages))
	for _, want := range []string{"有效摘要", "继续验证 SQL 注入路径", "原始用户输入与约束账本"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("retried compacted context missing %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "本地压缩摘要") {
		t.Fatalf("local fallback should not be used:\n%s", joined)
	}
}

func TestAppendEinoAgenticChatModelTailMiddlewaresIncludesTypedSummarization(t *testing.T) {
	t.Parallel()
	mw := newAgenticSystemMessageNormalizerMiddleware(nil, "summary")
	handlers := appendEinoAgenticChatModelTailMiddlewares(nil, einoChatModelTailConfig{
		agenticSummarization: mw,
		skipTrace:            true,
	})
	found := false
	for _, h := range handlers {
		if h == mw {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("agentic summarization middleware was not appended")
	}
}

func agenticAssistantTextMessage(text string) *schema.AgenticMessage {
	return &schema.AgenticMessage{
		Role:          schema.AgenticRoleTypeAssistant,
		ResponseMeta:  &schema.AgenticResponseMeta{OpenAIExtension: &schemaopenai.ResponseMetaExtension{Status: schemaopenai.ResponseStatusCompleted}},
		ContentBlocks: []*schema.ContentBlock{schema.NewContentBlock(&schema.AssistantGenText{Text: text})},
	}
}

func joinClassicMessageContent(msgs []*schema.Message) string {
	var b strings.Builder
	for _, msg := range msgs {
		if msg == nil {
			continue
		}
		b.WriteString(msg.Content)
		b.WriteByte('\n')
	}
	return b.String()
}
