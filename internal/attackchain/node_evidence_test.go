package attackchain

import (
	"strings"
	"testing"

	"cyberstrike-ai/internal/agent"

	"go.uber.org/zap"
)

func traceWithToolCalls(ids ...string) []agent.ChatMessage {
	var msgs []agent.ChatMessage
	for _, id := range ids {
		msgs = append(msgs,
			agent.ChatMessage{Role: "assistant", ToolCalls: []agent.ToolCall{{ID: id, Function: agent.FunctionCall{Name: "scan"}}}},
			agent.ChatMessage{Role: "tool", ToolCallID: id, Content: "result " + id},
		)
	}
	return msgs
}

func TestNewToolEvidenceSkipsSourcesItCannotEnumerate(t *testing.T) {
	b := &Builder{logger: zap.NewNop()}
	msgs := traceWithToolCalls("call-1")
	prompt := b.formatAgentTraceFromChatMessages(msgs)
	if ev := newToolEvidence(msgs, prompt, true); ev != nil {
		t.Fatalf("process-detail supplement must disable judging: %#v", ev)
	}
	if ev := newToolEvidence(nil, prompt, false); ev != nil {
		t.Fatalf("messages-table fallback must disable judging: %#v", ev)
	}
	if ev := newToolEvidence([]agent.ChatMessage{{Role: "user", Content: "hi"}}, prompt, false); ev != nil {
		t.Fatalf("a trace without tool calls must disable judging: %#v", ev)
	}
	ev := newToolEvidence(msgs, prompt, false)
	if ev == nil || len(ev.real) != 1 || len(ev.shown) != 1 {
		t.Fatalf("host-issued IDs not enumerated: %#v", ev)
	}
}

func TestToolEvidenceKeepRules(t *testing.T) {
	b := &Builder{logger: zap.NewNop()}
	msgs := traceWithToolCalls("call-1", "call-2")
	full := newToolEvidence(msgs, b.formatAgentTraceFromChatMessages(msgs), false)
	// The token budget cut one call away, so a missing citation is ambiguous.
	partial := newToolEvidence(msgs, "only call-1 survived", false)
	var none *toolEvidence
	for _, tc := range []struct {
		name     string
		ev       *toolEvidence
		cited    string
		wantKeep bool
	}{
		{"no evidence never judges an uncited node", none, "", true},
		{"no evidence never judges a citation", none, "call-9", true},
		{"citation present in the trajectory", full, "call-1", true},
		{"citation absent from the trajectory", full, "call-9", false},
		{"uncited while every ID was still visible", full, "", false},
		{"uncited while an ID was cut by the budget", partial, "", true},
		{"fabricated citation is dropped even when cut", partial, "call-9", false},
	} {
		if got := tc.ev.keep(tc.cited); got != tc.wantKeep {
			t.Errorf("%s: keep=%v want %v", tc.name, got, tc.wantKeep)
		}
	}
}

func TestParseChainJSONDropsNodesWithoutRealToolEvidence(t *testing.T) {
	b := &Builder{logger: zap.NewNop()}
	msgs := traceWithToolCalls("call-1")
	ev := newToolEvidence(msgs, b.formatAgentTraceFromChatMessages(msgs), false)
	raw := `{"nodes":[
		{"id":"t","type":"target","label":"scope.example"},
		{"id":"ok","type":"action","label":"Real scan","tool_call_id":"call-1"},
		{"id":"nested","type":"action","label":"Nested citation","metadata":{"tool_call_id":"call-1"}},
		{"id":"forged","type":"action","label":"Invented scan","tool_call_id":"call-9"},
		{"id":"nestedforged","type":"action","label":"Nested invented","metadata":{"tool_call_id":"call-9"}},
		{"id":"uncited","type":"vulnerability","label":"Invented SQLi"},
		{"id":"again","type":"action","label":"Second real scan","tool_call_id":"call-1"}
	],"edges":[
		{"source":"t","target":"ok","type":"leads_to"},
		{"source":"ok","target":"forged","type":"leads_to"},
		{"source":"ok","target":"uncited","type":"discovers"}
	]}`
	chain, err := b.parseChainJSON(raw, ev)
	if err != nil {
		t.Fatal(err)
	}
	labels := make([]string, 0, len(chain.Nodes))
	for _, n := range chain.Nodes {
		labels = append(labels, n.Label)
		switch n.Label {
		case "Real scan", "Nested citation", "Second real scan":
			if n.Metadata["tool_call_id"] != "call-1" {
				t.Fatalf("verified citation not recorded on %q: %#v", n.Label, n.Metadata)
			}
		case "scope.example":
			if _, ok := n.Metadata["tool_call_id"]; ok {
				t.Fatalf("target node gained a citation: %#v", n.Metadata)
			}
		}
	}
	if strings.Join(labels, "|") != "scope.example|Real scan|Nested citation|Second real scan" {
		t.Fatalf("evidence gate kept the wrong nodes: %v", labels)
	}
	if len(chain.Edges) != 1 {
		t.Fatalf("edges pointing at dropped nodes survived: %#v", chain.Edges)
	}
}
