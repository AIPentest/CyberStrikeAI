package multiagent

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

func TestLiteralInstructionGenModelInput_PreservesLiteralCurlyBraces(t *testing.T) {
	t.Parallel()
	instruction := "- [finding/x] summary {links: discovered_on←target/dev}\n" +
		"如 finding 上 {from:target/*, type:discovered_on}"
	msgs, err := literalInstructionGenModelInput(context.Background(), instruction, &adk.AgentInput{
		Messages: []adk.Message{schema.UserMessage("继续")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Role != schema.System {
		t.Fatalf("first message must be system, got %s", msgs[0].Role)
	}
	for _, want := range []string{"{links:", "{from:target/*, type:discovered_on}"} {
		if !strings.Contains(msgs[0].Content, want) {
			t.Fatalf("system content missing %q: %q", want, msgs[0].Content)
		}
	}
}
