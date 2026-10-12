package multiagent

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

func TestAgenticSystemMessageNormalizerMiddlewareMergesDuplicates(t *testing.T) {
	t.Parallel()
	mw := newAgenticSystemMessageNormalizerMiddleware(nil, "test")
	state := &adk.TypedChatModelAgentState[*schema.AgenticMessage]{
		Messages: []*schema.AgenticMessage{
			schema.SystemAgenticMessage("first"),
			schema.UserAgenticMessage("hello"),
			schema.SystemAgenticMessage("second"),
		},
	}
	_, out, err := mw.BeforeModelRewriteState(context.Background(), state, nil)
	if err != nil {
		t.Fatalf("BeforeModelRewriteState: %v", err)
	}
	if out == state {
		t.Fatal("expected rewritten state")
	}
	if got := countAgenticSystemMessages(out.Messages); got != 1 {
		t.Fatalf("system messages = %d, want 1", got)
	}
	if out.Messages[0].Role != schema.AgenticRoleTypeSystem {
		t.Fatalf("first role = %s, want system", out.Messages[0].Role)
	}
	text := agenticMessageText(out.Messages[0])
	if !strings.Contains(text, "first") || !strings.Contains(text, "second") {
		t.Fatalf("merged system text = %q", text)
	}
	if len(out.Messages) != 2 || agenticMessageText(out.Messages[1]) != "hello" {
		t.Fatalf("normalized messages = %#v", out.Messages)
	}
}

func TestAgenticContinuationUserDedupMiddlewareKeepsLatest(t *testing.T) {
	t.Parallel()
	mw := newAgenticContinuationUserDedupMiddleware(nil, "test")
	state := &adk.TypedChatModelAgentState[*schema.AgenticMessage]{
		Messages: []*schema.AgenticMessage{
			schema.UserAgenticMessage(continuationSessionMarker + "\nold"),
			schema.UserAgenticMessage("real user request"),
			schema.UserAgenticMessage(continuationSessionMarker + "\nnew"),
		},
	}
	_, out, err := mw.BeforeModelRewriteState(context.Background(), state, nil)
	if err != nil {
		t.Fatalf("BeforeModelRewriteState: %v", err)
	}
	if out == state {
		t.Fatal("expected rewritten state")
	}
	if len(out.Messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(out.Messages))
	}
	if strings.Contains(agenticMessageText(out.Messages[0]), continuationSessionMarker) {
		t.Fatalf("old continuation was not dropped: %#v", out.Messages)
	}
	if !strings.Contains(agenticMessageText(out.Messages[1]), "new") {
		t.Fatalf("latest continuation not retained: %#v", out.Messages)
	}
}

func TestAgenticModelFacingTraceMiddlewareStoresClassicTrace(t *testing.T) {
	t.Parallel()
	holder := newModelFacingTraceHolder()
	mw := newAgenticModelFacingTraceMiddleware(holder)
	state := &adk.TypedChatModelAgentState[*schema.AgenticMessage]{
		Messages: []*schema.AgenticMessage{
			schema.SystemAgenticMessage("instruction"),
			{
				Role: schema.AgenticRoleTypeAssistant,
				ContentBlocks: []*schema.ContentBlock{
					schema.NewContentBlock(&schema.AssistantGenText{Text: "answer"}),
				},
			},
		},
	}
	if _, _, err := mw.BeforeModelRewriteState(context.Background(), state, nil); err != nil {
		t.Fatalf("BeforeModelRewriteState: %v", err)
	}
	got := holder.Snapshot()
	if len(got) != 2 {
		t.Fatalf("trace len = %d, want 2", len(got))
	}
	if got[0].Role != schema.System || got[0].Content != "instruction" {
		t.Fatalf("system trace = %#v", got[0])
	}
	if got[1].Role != schema.Assistant || got[1].Content != "answer" {
		t.Fatalf("assistant trace = %#v", got[1])
	}
}

func TestAppendEinoAgenticChatModelTailMiddlewares(t *testing.T) {
	t.Parallel()
	holder := newModelFacingTraceHolder()
	handlers := appendEinoAgenticChatModelTailMiddlewares(nil, einoChatModelTailConfig{
		phase: "agentic",
		trace: holder,
	})
	// system + continuation + two argument sanitizers + reconciler + orphan_pruner + trace
	if len(handlers) != 7 {
		t.Fatalf("handlers = %d, want system + continuation + two argument sanitizers + reconciler + orphan_pruner + trace", len(handlers))
	}
}
