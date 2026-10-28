package multiagent

import (
	"testing"

	"github.com/cloudwego/eino/adk"
)

func TestEinoInitialIteratorStartHandlerKeepsExistingIterator(t *testing.T) {
	existing, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	defer gen.Close()

	var started bool
	got := newEinoInitialIteratorStartHandler(einoInitialIteratorStartHandlerConfig{
		UseTurnLoop: true,
		StartTurnLoop: func([]adk.Message) *adk.AsyncIterator[*adk.AgentEvent] {
			started = true
			iter, iterGen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
			iterGen.Close()
			return iter
		},
		Progress: func(string, string, interface{}) {
			t.Fatal("progress should not be emitted when an iterator already exists")
		},
	}).StartIfNeeded(existing, nil)

	if got != existing {
		t.Fatal("existing iterator should be preserved")
	}
	if started {
		t.Fatal("start function should not be called when an iterator already exists")
	}
}

func TestEinoInitialIteratorStartHandlerStartsRunner(t *testing.T) {
	wantIter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	defer gen.Close()

	var runnerStarted bool
	got := newEinoInitialIteratorStartHandler(einoInitialIteratorStartHandlerConfig{
		StartRunner: func(msgs []adk.Message) *adk.AsyncIterator[*adk.AgentEvent] {
			runnerStarted = true
			if msgs == nil {
				t.Fatal("msgs should be forwarded")
			}
			return wantIter
		},
		StartTurnLoop: func([]adk.Message) *adk.AsyncIterator[*adk.AgentEvent] {
			t.Fatal("turn loop should not start when UseTurnLoop is false")
			return nil
		},
		Progress: func(string, string, interface{}) {
			t.Fatal("runner start should not emit TurnLoop takeover progress")
		},
	}).StartIfNeeded(nil, []adk.Message{})

	if !runnerStarted {
		t.Fatal("runner start was not called")
	}
	if got != wantIter {
		t.Fatal("runner iterator should be returned")
	}
}

func TestEinoInitialIteratorStartHandlerStartsTurnLoopWithTakeoverProgress(t *testing.T) {
	wantIter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	defer gen.Close()

	var turnLoopStarted bool
	var gotType, gotMessage string
	var gotData map[string]interface{}
	got := newEinoInitialIteratorStartHandler(einoInitialIteratorStartHandlerConfig{
		ConversationID: "conv-1",
		OrchMode:       "deep",
		UseTurnLoop:    true,
		StartRunner: func([]adk.Message) *adk.AsyncIterator[*adk.AgentEvent] {
			t.Fatal("runner should not start when UseTurnLoop is true")
			return nil
		},
		StartTurnLoop: func(msgs []adk.Message) *adk.AsyncIterator[*adk.AgentEvent] {
			turnLoopStarted = true
			if msgs == nil {
				t.Fatal("msgs should be forwarded")
			}
			return wantIter
		},
		Progress: func(eventType, message string, data interface{}) {
			gotType = eventType
			gotMessage = message
			if m, ok := data.(map[string]interface{}); ok {
				gotData = m
			}
		},
	}).StartIfNeeded(nil, []adk.Message{})

	if !turnLoopStarted {
		t.Fatal("turn loop start was not called")
	}
	if got != wantIter {
		t.Fatal("turn loop iterator should be returned")
	}
	if gotType != "progress" {
		t.Fatalf("progress type = %q, want progress", gotType)
	}
	if gotMessage != "Eino TurnLoop resident multi-turn runtime has taken over this conversation turn." {
		t.Fatalf("progress message = %q", gotMessage)
	}
	if gotData["conversationId"] != "conv-1" || gotData["source"] != "eino" || gotData["orchestration"] != "deep" {
		t.Fatalf("progress data = %#v", gotData)
	}
}
