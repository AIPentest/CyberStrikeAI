package multiagent

import (
	"encoding/json"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestEinoExtractFallbackAssistantFromMsgs_exitToolMessage(t *testing.T) {
	u := schema.UserMessage("hi")
	tm := schema.ToolMessage("answer for user", "call-exit-1")
	tm.ToolName = "exit"
	if got := einoExtractFallbackAssistantFromMsgs([]*schema.Message{u, tm}); got != "answer for user" {
		t.Fatalf("got %q", got)
	}
}

func TestEinoExtractFallbackAssistantFromMsgs_lastExitWins(t *testing.T) {
	msgs := []*schema.Message{
		schema.UserMessage("hi"),
		toolExitMsg("first", "c1"),
		toolExitMsg("second", "c2"),
	}
	if got := einoExtractFallbackAssistantFromMsgs(msgs); got != "second" {
		t.Fatalf("got %q", got)
	}
}

func TestEinoExtractFallbackAssistantFromMsgs_fromAssistantToolCalls(t *testing.T) {
	m := schema.AssistantMessage("", []schema.ToolCall{{
		ID:   "x",
		Type: "function",
		Function: schema.FunctionCall{
			Name:      "exit",
			Arguments: `{"final_result":"from args"}`,
		},
	}})
	if got := einoExtractFallbackAssistantFromMsgs([]*schema.Message{m}); got != "from args" {
		t.Fatalf("got %q", got)
	}
}

func TestEinoExtractFallbackAssistantFromMsgs_prefersToolOverEarlierAssistant(t *testing.T) {
	asst := schema.AssistantMessage("", []schema.ToolCall{{
		ID:   "x",
		Type: "function",
		Function: schema.FunctionCall{
			Name:      "exit",
			Arguments: `{"final_result":"from args"}`,
		},
	}})
	tool := toolExitMsg("from tool", "c1")
	if got := einoExtractFallbackAssistantFromMsgs([]*schema.Message{asst, tool}); got != "from tool" {
		t.Fatalf("got %q", got)
	}
}

func TestEinoExtractFallbackAssistantFromMsgs_plainAssistant(t *testing.T) {
	msgs := []*schema.Message{
		schema.UserMessage("hi"),
		schema.AssistantMessage("plain answer", nil),
	}
	if got := einoExtractFallbackAssistantFromMsgs(msgs); got != "plain answer" {
		t.Fatalf("got %q", got)
	}
}

func TestEinoExtractFallbackAssistantFromMsgs_finalAssistantAfterToolResult(t *testing.T) {
	msgs := []*schema.Message{
		schema.UserMessage("hi"),
		schema.AssistantMessage("", []schema.ToolCall{{
			ID:   "call-1",
			Type: "function",
			Function: schema.FunctionCall{
				Name:      "execute",
				Arguments: `{"command":"pwd"}`,
			},
		}}),
		schema.ToolMessage("/tmp", "call-1", schema.WithToolName("execute")),
		schema.AssistantMessage("final after tool", nil),
	}
	if got := einoExtractFallbackAssistantFromMsgs(msgs); got != "final after tool" {
		t.Fatalf("got %q", got)
	}
}

func TestEinoExtractFallbackAssistantFromMsgs_doesNotUseAssistantBeforeUnfinishedToolResult(t *testing.T) {
	msgs := []*schema.Message{
		schema.UserMessage("hi"),
		schema.AssistantMessage("I will inspect that.", nil),
		schema.AssistantMessage("", []schema.ToolCall{{
			ID:   "call-1",
			Type: "function",
			Function: schema.FunctionCall{
				Name:      "execute",
				Arguments: `{"command":"pwd"}`,
			},
		}}),
		schema.ToolMessage("/tmp", "call-1", schema.WithToolName("execute")),
	}
	if got := einoExtractFallbackAssistantFromMsgs(msgs); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestEinoRunResultBuilderFinalFallsBackToPlainAssistantTrace(t *testing.T) {
	runMessages := newEinoRunMessageAccumulator(nil)
	runMessages.Append(schema.UserMessage("hi"))
	runMessages.Append(schema.AssistantMessage("plain answer", nil))

	got := newEinoRunResultBuilder(einoRunResultBuilderConfig{
		OrchMode:    "deep",
		EmptyHint:   "empty",
		RunMessages: runMessages,
	}).BuildFinal()

	if got.Response != "plain answer" {
		t.Fatalf("response = %q, want plain answer", got.Response)
	}
}

func TestEinoRunResultBuilderPrefersExitFinalOverAssistantIntro(t *testing.T) {
	intro := "本轮渗透收束完成。交付终审报告："
	report := "## 终审报告\n目标 alvin-whn.top 已完成 getshell 验证。"
	asst := schema.AssistantMessage(intro, []schema.ToolCall{{
		ID:   "exit-1",
		Type: "function",
		Function: schema.FunctionCall{
			Name:      "exit",
			Arguments: `{"final_result":` + mustJSONString(report) + `}`,
		},
	}})
	runMessages := newEinoRunMessageAccumulator(nil)
	runMessages.Append(schema.UserMessage("继续"))
	runMessages.Append(asst)
	runMessages.Append(toolExitMsg(report, "exit-1"))

	assistantOutput := newEinoAssistantOutputAccumulator("supervisor")
	assistantOutput.RecordMainAssistant("cyberstrike-supervisor", intro)

	got := newEinoRunResultBuilder(einoRunResultBuilderConfig{
		OrchMode:        "supervisor",
		EmptyHint:       "empty",
		RunMessages:     runMessages,
		AssistantOutput: assistantOutput,
	}).BuildFinal()

	want := intro + "\n\n" + report
	if got.Response != want {
		t.Fatalf("response = %q, want %q", got.Response, want)
	}
}

func TestEinoRunResultBuilderPrefersExitFinalFromArgsWhenToolContentMissing(t *testing.T) {
	intro := "交付终审报告："
	report := "full report body"
	asst := schema.AssistantMessage(intro, []schema.ToolCall{{
		ID:   "exit-1",
		Type: "function",
		Function: schema.FunctionCall{
			Name:      "exit",
			Arguments: `{"final_result":` + mustJSONString(report) + `}`,
		},
	}})
	runMessages := newEinoRunMessageAccumulator(nil)
	runMessages.Append(asst)
	runMessages.Append(toolExitMsg("", "exit-1"))

	assistantOutput := newEinoAssistantOutputAccumulator("supervisor")
	assistantOutput.RecordMainAssistant("cyberstrike-supervisor", intro)

	got := newEinoRunResultBuilder(einoRunResultBuilderConfig{
		OrchMode:        "supervisor",
		EmptyHint:       "empty",
		RunMessages:     runMessages,
		AssistantOutput: assistantOutput,
	}).BuildFinal()

	want := intro + "\n\n" + report
	if got.Response != want {
		t.Fatalf("response = %q, want %q", got.Response, want)
	}
}

func TestEinoMergeAssistantIntroWithExitFinal(t *testing.T) {
	if got := einoMergeAssistantIntroWithExitFinal("", "final"); got != "final" {
		t.Fatalf("empty intro: %q", got)
	}
	if got := einoMergeAssistantIntroWithExitFinal("same", "same"); got != "same" {
		t.Fatalf("identical: %q", got)
	}
	if got := einoMergeAssistantIntroWithExitFinal("intro", "intro\n\nbody"); got != "intro\n\nbody" {
		t.Fatalf("contained intro: %q", got)
	}
}

func mustJSONString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func toolExitMsg(content, callID string) *schema.Message {
	m := schema.ToolMessage(content, callID)
	m.ToolName = "exit"
	return m
}
