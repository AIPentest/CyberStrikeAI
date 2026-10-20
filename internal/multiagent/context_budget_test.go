package multiagent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"cyberstrike-ai/internal/config"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/summarization"
	"github.com/cloudwego/eino/schema"
)

func TestIsEinoContextOverflowError(t *testing.T) {
	t.Parallel()
	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{errors.New("context length exceeded"), true},
		{errors.New("maximum context length"), true},
		{errors.New("input is too long for model"), true},
		{errors.New("the request exceeds the available context size, try increasing it"), true},
		{errors.New("input length exceeds the context length"), true},
		{errors.New("prompt too long"), true},
		{errors.New("The input length of the prompt is 9000, exceeding max_model_len 8192"), true},
		{errors.New("HTTP 429 Too Many Requests"), false},
		{errors.New("invalid api key"), false},
	}
	for _, tc := range cases {
		if got := isEinoContextOverflowError(tc.err); got != tc.want {
			t.Fatalf("isEinoContextOverflowError(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}

func TestTruncateRoundMessagesToTokenBudget(t *testing.T) {
	huge := strings.Repeat("x", 8000)
	round := messageRound{messages: []adk.Message{
		assistantToolCallsMsg("", "c1"),
		schema.ToolMessage(huge, "c1"),
	}}
	out, err := truncateRoundMessagesToTokenBudget(
		context.Background(), round, 256, einoSummarizationTokenCounter("gpt-4o"), 512, "",
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, msg := range out {
		if msg != nil && msg.Role == schema.Tool && len(msg.Content) >= len(huge) {
			t.Fatalf("expected truncated tool output, got len=%d", len(msg.Content))
		}
	}
}

func TestBuildBudgetedSummarizationModelInputTruncatesOversizedLatestRound(t *testing.T) {
	huge := strings.Repeat("x", 8000)
	msgs := []adk.Message{
		assistantToolCallsMsg("", "call-latest"),
		schema.ToolMessage(huge, "call-latest"),
	}
	counter := einoSummarizationTokenCounter("gpt-4o")
	input, dropped, err := buildBudgetedSummarizationModelInput(
		context.Background(),
		schema.SystemMessage("sys"),
		schema.UserMessage("instr"),
		msgs,
		counter,
		512,
		summarizationInputBudgetOpts{toolMaxBytes: 256},
	)
	if err != nil {
		t.Fatal(err)
	}
	if dropped != 0 {
		t.Fatalf("expected no dropped rounds, got %d", dropped)
	}
	toolContent := ""
	for _, msg := range input {
		if msg != nil && msg.Role == schema.Tool {
			toolContent = msg.Content
		}
	}
	if len(toolContent) >= len(huge) {
		t.Fatalf("expected oversized tool output to be compacted, got len=%d", len(toolContent))
	}
}

func TestLocalTokenCounterSafetyMargin(t *testing.T) {
	t.Parallel()
	input := &summarization.TokenCounterInput{Messages: []adk.Message{
		schema.UserMessage(strings.Repeat("目标主机存在漏洞，需要进一步验证。", 40)),
	}}
	baseline := einoSummarizationTokenCounterWithSafety("qwen3-8b", 100)
	local := einoSummarizationTokenCounterWithSafety("qwen3-8b", config.LocalTokenEstimateSafetyPercent)
	baseCount, err := baseline(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	localCount, err := local(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	want := (baseCount*config.LocalTokenEstimateSafetyPercent + 99) / 100
	if localCount != want {
		t.Fatalf("local count = %d, want %d (baseline %d)", localCount, want, baseCount)
	}
	if localCount <= baseCount {
		t.Fatalf("local count %d must exceed baseline %d", localCount, baseCount)
	}
}

func TestCompactMessagesCountsToolSchemas(t *testing.T) {
	t.Parallel()
	counter := func(_ context.Context, in *summarization.TokenCounterInput) (int, error) {
		n := 100 * len(in.Tools)
		for _, msg := range in.Messages {
			if msg != nil {
				n += len(msg.Content)
			}
		}
		return n, nil
	}
	msgs := []adk.Message{
		schema.UserMessage(strings.Repeat("a", 25)),
		schema.AssistantMessage(strings.Repeat("b", 25), nil),
		schema.UserMessage(strings.Repeat("c", 25)),
		schema.AssistantMessage(strings.Repeat("d", 25), nil),
	}
	out, changed := compactMessagesByDroppingRounds(context.Background(), msgs, compactMessagesOpts{
		maxTokens: 150,
		counter:   counter,
		tools:     []*schema.ToolInfo{{Name: "t"}},
		phase:     "test",
	})
	if !changed {
		t.Fatal("expected compaction once tool schema tokens are included")
	}
	total := 100
	for _, msg := range out {
		if msg != nil {
			total += len(msg.Content)
		}
	}
	if total > 150 {
		t.Fatalf("compacted input = %d tokens, want <= 150", total)
	}
}

func TestCompactMessagesLeavesInputWhenToolsExceedBudget(t *testing.T) {
	t.Parallel()
	counter := func(_ context.Context, in *summarization.TokenCounterInput) (int, error) {
		n := 100 * len(in.Tools)
		for _, msg := range in.Messages {
			if msg != nil {
				n += len(msg.Content)
			}
		}
		return n, nil
	}
	msgs := []adk.Message{
		schema.UserMessage(strings.Repeat("a", 25)),
		schema.AssistantMessage(strings.Repeat("b", 25), nil),
	}
	out, changed := compactMessagesByDroppingRounds(context.Background(), msgs, compactMessagesOpts{
		maxTokens: 50,
		counter:   counter,
		tools:     []*schema.ToolInfo{{Name: "t"}},
		phase:     "test",
	})
	if changed {
		t.Fatal("message compaction cannot fix a tool-schema budget overrun")
	}
	if len(out) != len(msgs) {
		t.Fatalf("messages changed: %d -> %d", len(msgs), len(out))
	}
}

func TestModelInputSoftBudgetNeverErrors(t *testing.T) {
	mw := &modelInputSoftBudgetMiddleware{
		maxTokens:    4,
		toolMaxBytes: 16,
		counter:      fixedTokenCounter(4),
		phase:        "test",
	}
	state := &adk.ChatModelAgentState{Messages: []adk.Message{
		schema.UserMessage("u"),
		assistantToolCallsMsg("", "c1"),
		schema.ToolMessage(strings.Repeat("t", 200), "c1"),
	}}
	_, out, err := mw.BeforeModelRewriteState(context.Background(), state, nil)
	if err != nil {
		t.Fatalf("soft budget must not error: %v", err)
	}
	if out == nil || len(out.Messages) == 0 {
		t.Fatal("expected compacted messages")
	}
}
