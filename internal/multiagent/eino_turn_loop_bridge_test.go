package multiagent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

func TestRunEinoADKAgentLoopUsesTurnLoopInterruptPush(t *testing.T) {
	baseCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	pushCh := make(chan func(string) bool, 1)
	ctx := WithAgentTurnLoopInterruptRegistrar(baseCtx, func(push func(string) bool) func() {
		pushCh <- push
		return func() {}
	})

	mockModel := newTurnLoopBlockingModel()
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:  "turn-loop-agent",
		Model: mockModel,
	})
	if err != nil {
		t.Fatalf("NewChatModelAgent: %v", err)
	}

	var mu sync.Mutex
	var eventTypes []string
	var rawInterruptReason string
	var rawInterruptRunID string
	progress := func(eventType, _ string, data interface{}) {
		mu.Lock()
		defer mu.Unlock()
		eventTypes = append(eventTypes, eventType)
		if eventType == "user_interrupt_continue" {
			if m, ok := data.(map[string]interface{}); ok {
				rawInterruptReason, _ = m["rawReason"].(string)
				rawInterruptRunID, _ = m["runId"].(string)
			}
		}
	}

	done := make(chan struct{})
	var result *RunResult
	var runErr error
	go func() {
		defer close(done)
		result, runErr = runEinoADKAgentLoop(ctx, &einoADKRunLoopArgs{
			OrchMode:                 "eino_single",
			OrchestratorName:         "turn-loop-agent",
			ConversationID:           "conv-turn-loop",
			Progress:                 progress,
			DA:                       agent,
			EmptyResponseMessage:     "empty",
			TurnLoopInterruptTimeout: 20 * time.Millisecond,
		}, []*schema.Message{schema.UserMessage("initial task")})
	}()

	select {
	case <-mockModel.started:
	case <-ctx.Done():
		t.Fatal("first model call did not start")
	}
	var push func(string) bool
	select {
	case push = <-pushCh:
	case <-ctx.Done():
		t.Fatal("turn loop interrupt hook was not registered")
	}
	if !push("focus ssh") {
		t.Fatal("turn loop interrupt push was rejected")
	}

	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("run loop did not finish")
	}
	if runErr != nil {
		t.Fatalf("runErr = %v", runErr)
	}
	if result == nil || result.Response != "done" {
		t.Fatalf("result = %#v, err=%v", result, runErr)
	}
	if rawInterruptReason != "focus ssh" {
		t.Fatalf("raw interrupt reason = %q, want focus ssh", rawInterruptReason)
	}
	if rawInterruptRunID == "" {
		t.Fatal("interrupt progress should include runId")
	}
	if !containsString(eventTypes, "user_interrupt_continue") {
		t.Fatalf("events = %#v, want user_interrupt_continue", eventTypes)
	}

	inputs := mockModel.snapshotInputs()
	if len(inputs) < 2 {
		t.Fatalf("model calls = %d, want at least 2", len(inputs))
	}
	last := inputs[len(inputs)-1]
	if len(last) < 2 || last[0].Content != "initial task" {
		t.Fatalf("initial task lost: %#v", last)
	}
	if len(last) == 0 || last[len(last)-1].Role != schema.User || last[len(last)-1].Content == "initial task" {
		t.Fatalf("last model input = %#v, want interrupt supplement turn", last)
	}
}

func TestRunEinoADKAgentLoopInterruptContinueSurvivesStreamCanceled(t *testing.T) {
	baseCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	pushCh := make(chan func(string) bool, 1)
	ctx := WithAgentTurnLoopInterruptRegistrar(baseCtx, func(push func(string) bool) func() {
		pushCh <- push
		return func() {}
	})

	mockModel := newTurnLoopHangingStreamModel()
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:  "turn-loop-agent",
		Model: mockModel,
	})
	if err != nil {
		t.Fatalf("NewChatModelAgent: %v", err)
	}

	done := make(chan struct{})
	var result *RunResult
	var runErr error
	go func() {
		defer close(done)
		result, runErr = runEinoADKAgentLoop(ctx, &einoADKRunLoopArgs{
			OrchMode:                 "deep",
			OrchestratorName:         "turn-loop-agent",
			ConversationID:           "conv-stream-cancel",
			DA:                       agent,
			EmptyResponseMessage:     "empty",
			TurnLoopInterruptTimeout: 20 * time.Millisecond,
		}, []*schema.Message{schema.UserMessage("initial task")})
	}()

	select {
	case <-mockModel.started:
	case <-ctx.Done():
		t.Fatal("first stream did not start")
	}
	var push func(string) bool
	select {
	case push = <-pushCh:
	case <-ctx.Done():
		t.Fatal("turn loop interrupt hook was not registered")
	}
	if !push("focus emobile") {
		t.Fatal("turn loop interrupt push was rejected")
	}

	select {
	case <-mockModel.started:
	case <-ctx.Done():
		t.Fatal("second stream did not start after interrupt")
	}
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("run loop did not finish")
	}
	if runErr != nil {
		t.Fatalf("runErr = %v, want interrupt-continue to survive stream canceled", runErr)
	}
	if result == nil || result.Response != "done" {
		t.Fatalf("result = %#v, want continued turn output", result)
	}
	if isEinoStreamCanceled(runErr) {
		t.Fatal("stream canceled leaked as the run error")
	}
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
