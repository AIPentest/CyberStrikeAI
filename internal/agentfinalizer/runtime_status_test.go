package agentfinalizer

import (
	"cyberstrike-ai/internal/multiagent"
	"testing"
)

func TestRuntimeIterationLimitCannotBecomeCompleted(t *testing.T) {
	result := &multiagent.RunResult{Response: "Partial report", RuntimeStatus: "blocked", RuntimeCompletionReason: "iteration_limit"}
	for i := 0; i < 2; i++ {
		decision := FromRunResult(nil, result, Input{})
		if decision.Finalizable || decision.Status != "blocked" || decision.CompletionReason != "iteration_limit" {
			t.Fatalf("unexpected delivery: %+v", decision)
		}
	}
}
