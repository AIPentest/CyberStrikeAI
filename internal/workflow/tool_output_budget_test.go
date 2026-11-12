package workflow

import (
	"strings"
	"testing"
)

func TestTruncateWorkflowToolOutputBoundsBytesAndKeepsExecutionReference(t *testing.T) {
	out := truncateWorkflowToolOutput(strings.Repeat("Response body", 1000), 256, "exec-123")
	if len(out) > 256 {
		t.Fatalf("workflow output bytes=%d, want <=256", len(out))
	}
	if !strings.Contains(out, "exec-123") || !strings.Contains(out, "truncated") {
		t.Fatalf("missing truncation reference: %q", out)
	}
}

func TestTruncateWorkflowToolOutputLeavesBoundedContentUntouched(t *testing.T) {
	const want = "small-result"
	if got := truncateWorkflowToolOutput(want, 256, "exec-123"); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
