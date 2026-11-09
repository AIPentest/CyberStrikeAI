package multiagent

import (
	"context"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// literalInstructionGenModelInput passes Instruction through as a system message without
// FString template formatting. Eino defaultGenModelInput formats instruction whenever
// SessionValues exist; prompts with literal curly braces (project blackboard "{links: ...}",
// JSON examples, link syntax) then fail with "could not find key".
//
// Matches eino/adk/prebuilt/deep genModelInput — the supported fix per Eino docs.
func literalInstructionGenModelInput(ctx context.Context, instruction string, input *adk.AgentInput) ([]adk.Message, error) {
	msgs := make([]adk.Message, 0, len(input.Messages)+1)
	if instruction != "" {
		msgs = append(msgs, schema.SystemMessage(instruction))
	}
	msgs = append(msgs, input.Messages...)
	return msgs, nil
}

func literalAgenticInstructionGenModelInput(ctx context.Context, instruction string, input *adk.TypedAgentInput[*schema.AgenticMessage]) ([]*schema.AgenticMessage, error) {
	msgs := make([]*schema.AgenticMessage, 0, len(input.Messages)+1)
	if instruction != "" {
		msgs = append(msgs, schema.SystemAgenticMessage(instruction))
	}
	msgs = append(msgs, input.Messages...)
	return msgs, nil
}
