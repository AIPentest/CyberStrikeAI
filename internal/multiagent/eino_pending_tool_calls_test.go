package multiagent

import (
	"errors"
	"testing"
)

func TestEinoPendingToolCallsPopNextForAgentSkipsRemovedIDs(t *testing.T) {
	p := newEinoPendingToolCalls("conv", nil)
	p.Mark(toolCallPendingInfo{ToolCallID: "call-1", ToolName: "first", EinoAgent: "agent"})
	p.Mark(toolCallPendingInfo{ToolCallID: "call-2", ToolName: "second", EinoAgent: "agent"})
	p.RemoveByID("call-1")

	got, ok := p.PopNextForAgent("agent")
	if !ok {
		t.Fatal("expected pending tool call")
	}
	if got.ToolCallID != "call-2" {
		t.Fatalf("toolCallID = %q, want call-2", got.ToolCallID)
	}
	if p.Count() != 0 {
		t.Fatalf("pending count = %d, want 0", p.Count())
	}
}

func TestEinoPendingToolCallsPopAny(t *testing.T) {
	p := newEinoPendingToolCalls("conv", nil)
	p.Mark(toolCallPendingInfo{ToolCallID: "call-1", ToolName: "tool"})

	got, ok := p.PopAny()
	if !ok || got.ToolCallID != "call-1" {
		t.Fatalf("PopAny = %#v ok=%v", got, ok)
	}
	if _, ok := p.PopAny(); ok {
		t.Fatal("PopAny should be empty after first pop")
	}
}

func TestEinoPendingToolCallsFlushAsFailedEmitsAndClears(t *testing.T) {
	var events []struct {
		eventType string
		message   string
		data      map[string]interface{}
	}
	p := newEinoPendingToolCalls("conv-1", func(eventType, message string, data interface{}) {
		m, _ := data.(map[string]interface{})
		events = append(events, struct {
			eventType string
			message   string
			data      map[string]interface{}
		}{eventType: eventType, message: message, data: m})
	})
	p.Mark(toolCallPendingInfo{
		ToolCallID: "call-err",
		ToolName:   "",
		EinoAgent:  "agent",
		EinoRole:   "sub",
	})

	p.FlushAsFailed(errors.New("boom"))

	if p.Count() != 0 {
		t.Fatalf("pending count = %d, want 0", p.Count())
	}
	if len(events) != 1 {
		t.Fatalf("events = %#v, want one", events)
	}
	ev := events[0]
	if ev.eventType != "tool_result" || ev.message != "Tool result (unknown)" {
		t.Fatalf("event = %#v", ev)
	}
	if ev.data["toolCallId"] != "call-err" ||
		ev.data["conversationId"] != "conv-1" ||
		ev.data["einoAgent"] != "agent" ||
		ev.data["einoRole"] != "sub" ||
		ev.data["isError"] != true ||
		ev.data["success"] != false ||
		ev.data["result"] != "boom" {
		t.Fatalf("payload = %#v", ev.data)
	}
}
