package handler

import (
	"context"
	"testing"
	"time"
)

func TestAbortActiveAgentExecute(t *testing.T) {
	m := NewAgentTaskManager()
	conv := "conv-eino-exec-abort"
	ctx, cancel := context.WithCancel(context.Background())
	_, err := m.StartTask(conv, "test", func(error) {})
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	m.RegisterActiveAgentExecute(conv, cancel)

	done := make(chan struct{})
	go func() {
		<-ctx.Done()
		close(done)
	}()

	if !m.AbortActiveAgentExecute(conv, "跳过域名收集") {
		t.Fatal("expected abort to succeed")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("execute cancel did not propagate")
	}
	if got := m.TakeAgentExecuteAbortNote(conv); got != "跳过域名收集" {
		t.Fatalf("abort note = %q, want 跳过域名收集", got)
	}
	m.UnregisterActiveAgentExecute(conv)
	if m.AbortActiveAgentExecute(conv, "") {
		t.Fatal("second abort should fail when no active execute")
	}
}

func TestConversationIDForActiveMCPExecution(t *testing.T) {
	m := NewAgentTaskManager()
	conv := "conv-mcp-exec"
	_, err := m.StartTask(conv, "test", func(error) {})
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	m.RegisterRunningTool(conv, "exec-123")
	if got := m.ConversationIDForActiveMCPExecution("exec-123"); got != conv {
		t.Fatalf("got %q, want %q", got, conv)
	}
	if got := m.ConversationIDForActiveMCPExecution("missing"); got != "" {
		t.Fatalf("missing should be empty, got %q", got)
	}
}
