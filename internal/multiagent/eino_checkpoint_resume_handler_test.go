package multiagent

import (
	"context"
	"errors"
	"testing"

	"github.com/cloudwego/eino/adk"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestEinoCheckpointResumeHandlerSkipsWithoutCheckpoint(t *testing.T) {
	called := false
	handler := newEinoCheckpointResumeHandler(einoCheckpointResumeHandlerConfig{
		Resume: func(string) (*adk.AsyncIterator[*adk.AgentEvent], error) {
			called = true
			return nil, nil
		},
	})
	if iter := handler.TryResume(); iter != nil {
		t.Fatalf("iter = %#v, want nil", iter)
	}
	if called {
		t.Fatal("resume should not be called without checkpoint state")
	}
}

func TestEinoCheckpointResumeHandlerResumesExistingCheckpoint(t *testing.T) {
	store, err := newFileCheckPointStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set(context.Background(), "cp-1", []byte("checkpoint")); err != nil {
		t.Fatal(err)
	}
	var progressMessages []string
	var resumedID string
	core, logs := observer.New(zap.InfoLevel)
	wantIter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	defer gen.Close()
	handler := newEinoCheckpointResumeHandler(einoCheckpointResumeHandlerConfig{
		Context:        context.Background(),
		ConversationID: "conv-1",
		OrchMode:       "deep",
		Store:          store,
		CheckPointID:   "cp-1",
		Logger:         zap.New(core),
		Progress: func(eventType, message string, data interface{}) {
			if eventType != "progress" {
				return
			}
			progressMessages = append(progressMessages, message)
			m, _ := data.(map[string]interface{})
			if m["conversationId"] != "conv-1" || m["orchestration"] != "deep" || m["checkPointID"] != "cp-1" {
				t.Fatalf("progress data = %#v", m)
			}
		},
		Resume: func(checkPointID string) (*adk.AsyncIterator[*adk.AgentEvent], error) {
			resumedID = checkPointID
			return wantIter, nil
		},
	})

	got := handler.TryResume()
	if got != wantIter {
		t.Fatalf("iter = %#v, want resume iterator", got)
	}
	if resumedID != "cp-1" {
		t.Fatalf("resumed id = %q", resumedID)
	}
	if len(progressMessages) != 1 || progressMessages[0] != "Checkpoint detected; resuming execution from the interrupted node..." {
		t.Fatalf("progress messages = %#v", progressMessages)
	}
	if logs.FilterMessage("eino runner: resume from checkpoint").Len() != 1 {
		t.Fatalf("expected resume log, got %d", logs.Len())
	}
}

func TestEinoCheckpointResumeHandlerFallsBackOnResumeError(t *testing.T) {
	store, err := newFileCheckPointStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set(context.Background(), "cp-1", []byte("checkpoint")); err != nil {
		t.Fatal(err)
	}
	var progressMessages []string
	core, logs := observer.New(zap.WarnLevel)
	handler := newEinoCheckpointResumeHandler(einoCheckpointResumeHandlerConfig{
		Context:      context.Background(),
		Store:        store,
		CheckPointID: "cp-1",
		Logger:       zap.New(core),
		Progress: func(eventType, message string, _ interface{}) {
			if eventType == "progress" {
				progressMessages = append(progressMessages, message)
			}
		},
		Resume: func(string) (*adk.AsyncIterator[*adk.AgentEvent], error) {
			return nil, errors.New("resume failed")
		},
	})

	if iter := handler.TryResume(); iter != nil {
		t.Fatalf("iter = %#v, want nil fallback", iter)
	}
	if len(progressMessages) != 2 || progressMessages[1] != "Checkpoint resume failed; fell back to a fresh run." {
		t.Fatalf("progress messages = %#v", progressMessages)
	}
	if logs.FilterMessage("eino runner: resume failed, fallback to fresh run").Len() != 1 {
		t.Fatalf("expected fallback log, got %d", logs.Len())
	}
}

func TestEinoCheckpointResumeHandlerLogsPreflightError(t *testing.T) {
	store, err := newFileCheckPointStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	core, logs := observer.New(zap.WarnLevel)
	handler := newEinoCheckpointResumeHandler(einoCheckpointResumeHandlerConfig{
		Context:      context.Background(),
		Store:        store,
		CheckPointID: "bad/id",
		Logger:       zap.New(core),
		Resume: func(string) (*adk.AsyncIterator[*adk.AgentEvent], error) {
			t.Fatal("resume should not be called after preflight error")
			return nil, nil
		},
	})
	if iter := handler.TryResume(); iter != nil {
		t.Fatalf("iter = %#v, want nil", iter)
	}
	if logs.FilterMessage("eino checkpoint preflight get failed").Len() != 1 {
		t.Fatalf("expected preflight warning, got %d", logs.Len())
	}
}
