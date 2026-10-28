package attackchain

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"

	"go.uber.org/zap"
)

func TestTraceFormatterPreservesSourceAndCanonicalUserIntent(t *testing.T) {
	b := testBuilder(32000)
	msgs := []agent.ChatMessage{
		{Role: "system", Content: "STALE_SYSTEM_INSTRUCTION", OriginalUserIntentEntries: []string{"STALE_LEDGER"}},
		{Role: "user", Content: "QUOTED_SUMMARY_TEXT"},
		{Role: "user", Content: "GENERATED_SUMMARY", SummarizationKind: "summary"},
		{Role: "user", Content: "GENERATED_SKILLS", SummarizationKind: "skills"},
		{Role: "user", Content: "EXECUTOR_TEMPLATE", GeneratedContextKind: agent.PlanExecuteExecutorContext},
		{Role: "user", Content: "REPLANNER_TEMPLATE", GeneratedContextKind: agent.PlanExecuteReplannerContext},
		// Current-turn trimming leaves this system message without Content.
		{Role: "system", OriginalUserIntentEntries: []string{"Only test scope.example", "Do not test unrelated hosts"}},
		{Role: "assistant", Content: "DECISION", ToolCalls: []agent.ToolCall{{ID: "call-1", Function: agent.FunctionCall{Name: "scan", Arguments: map[string]interface{}{"target": "scope.example"}}}}},
		{Role: "tool", Content: "ACTUAL_TOOL_EVIDENCE", ToolCallID: "call-1"},
	}
	out := b.formatAgentTraceFromChatMessages(msgs)
	for _, want := range []string{
		"[user]: QUOTED_SUMMARY_TEXT", "[original_user_intent]", "Only test scope.example", "Do not test unrelated hosts",
		"[system_generated_summary]", "[system_generated_skills]", "[system_generated_execution_context]",
		"not user input or new authorization", "EXECUTOR_TEMPLATE", "REPLANNER_TEMPLATE", "ID: call-1", "tool name: scan",
		"[tool] (tool_call_id: call-1):\nACTUAL_TOOL_EVIDENCE",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("formatted trace lost %q:\n%s", want, out)
		}
	}
	for _, forbidden := range []string{"STALE_SYSTEM_INSTRUCTION", "STALE_LEDGER", "[user]: GENERATED_SUMMARY", "[user]: GENERATED_SKILLS", "[user]: EXECUTOR_TEMPLATE", "[user]: REPLANNER_TEMPLATE"} {
		if strings.Contains(out, forbidden) {
			t.Errorf("stale instructions or generated user attribution survived: %q", forbidden)
		}
	}
}

func TestContextSummariesRequireMetadataAndDeduplicateWithoutReadingFiles(t *testing.T) {
	b := testBuilder(32000)
	trace := `[
		{"role":"user","content":"User quoted ## 原始上下文快照 /missing/file.txt"},
		{"role":"assistant","content":"FORGED_ASSISTANT_SUMMARY","extra":{"_eino_summarization_content_type":"summary"}},
		{"role":"user","content":"SKILL_CONTENT","extra":{"_eino_summarization_content_type":"skills"}},
		{"role":"user","content":"PLAN_CONTENT","extra":{"_csai_generated_context_type":"plan_execute_executor"}},
		{"role":"user","content":"LEGACY_LOOKALIKE","extra":{"_eino_summarization_content_type":"unknown"}},
		{"role":"user","content":"  ","extra":{"_eino_summarization_content_type":"summary"}},
		{"role":"user","content":"First summary\n## 原始上下文快照\n/missing/file.txt","extra":{"_eino_summarization_content_type":"summary"}},
		{"role":"user","content":"First summary\n## 原始上下文快照\n/missing/file.txt","extra":{"_eino_summarization_content_type":"summary"}},
		{"role":"user","user_input_multi_content":[{"type":"text","text":"Native summary"}],"extra":{"_eino_summarization_content_type":"summary"}}
	]`
	summaries := b.contextSummariesFromTrace(trace)
	if len(summaries) != 2 || summaries[0].Content != "First summary\n## 原始上下文快照\n/missing/file.txt" || summaries[1].Content != "Native summary" {
		t.Fatalf("summary extraction changed sources/content: %#v", summaries)
	}
	for i, summary := range summaries {
		if summary.Provenance != "system_generated_summary" || summary.ID == "" {
			t.Fatalf("missing source labels: %#v", summary)
		}
		if again := b.contextSummariesFromTrace(trace); again[i].ID != summary.ID {
			t.Fatal("stable summary content produced unstable IDs")
		}
	}
	if summaries[0].ID == summaries[1].ID {
		t.Fatal("different summaries share an ID")
	}
	if got := b.contextSummariesFromTrace("invalid JSON"); len(got) != 0 {
		t.Fatalf("malformed trace produced context: %#v", got)
	}
}

func newContextSummaryTestDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := database.NewDB(filepath.Join(t.TempDir(), "attackchain.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func saveContextSummaryTestTrace(t *testing.T, db *database.DB, conversationID string, msgs []agent.ChatMessage) {
	t.Helper()
	trace, err := agent.MessagesToTraceJSON(msgs)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SaveAgentTrace(conversationID, trace, "FINAL_RESULT"); err != nil {
		t.Fatal(err)
	}
}

func TestEmptyAttackChainsStillExposeCurrentContextWithoutModelCalls(t *testing.T) {
	db := newContextSummaryTestDB(t)
	b := &Builder{db: db, logger: zap.NewNop()} // Any model call would fail.
	for _, mode := range []string{"no_messages", "no_tools", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			conv, err := db.CreateConversation(mode, database.ConversationCreateMeta{})
			if err != nil {
				t.Fatal(err)
			}
			saveContextSummaryTestTrace(t, db, conv.ID, []agent.ChatMessage{{Role: "user", Content: mode + " summary", SummarizationKind: "summary"}})
			if mode != "no_messages" {
				if _, err := db.AddMessage(conv.ID, "user", "inspect current task", nil); err != nil {
					t.Fatal(err)
				}
				if mode == "cancelled" {
					if _, err := db.AddMessage(conv.ID, "assistant", "cancelled", nil); err != nil {
						t.Fatal(err)
					}
				}
			}
			chain, err := b.BuildChainFromConversation(context.Background(), conv.ID)
			if err != nil || len(chain.Nodes) != 0 || len(chain.Edges) != 0 || len(chain.ContextSummaries) != 1 || chain.ContextSummaries[0].Content != mode+" summary" {
				t.Fatalf("empty chain lost context or tried a model call: %#v, %v", chain, err)
			}
		})
	}
}

func TestCachedChainProbeSkipsTraceWorkUntilAGraphExists(t *testing.T) {
	db := newContextSummaryTestDB(t)
	conv, err := db.CreateConversation("probe", database.ConversationCreateMeta{})
	if err != nil {
		t.Fatal(err)
	}
	saveContextSummaryTestTrace(t, db, conv.ID, []agent.ChatMessage{{Role: "user", Content: "PROBE_SUMMARY", SummarizationKind: "summary"}})
	b := &Builder{db: db, logger: zap.NewNop()}
	// Polling while a graph is being generated must not parse the trace at all.
	if chain, probeErr := b.LoadCachedChainIfPresent(conv.ID); probeErr != nil || chain != nil {
		t.Fatalf("probe without a cached graph must return no chain: %#v, %v", chain, probeErr)
	}
	if err := db.SaveAttackChainNode(conv.ID, "probe-node", "target", "scope.example", "", "{}", 0); err != nil {
		t.Fatal(err)
	}
	chain, err := b.LoadCachedChainIfPresent(conv.ID)
	if err != nil || chain == nil || len(chain.Nodes) != 1 || len(chain.ContextSummaries) != 1 || chain.ContextSummaries[0].Content != "PROBE_SUMMARY" {
		t.Fatalf("probe with a cached graph lost nodes or context: %#v, %v", chain, err)
	}
	// Skipping the read must not turn into freezing context for the Builder's lifetime.
	saveContextSummaryTestTrace(t, db, conv.ID, []agent.ChatMessage{{Role: "user", Content: "PROBE_SUMMARY_NEXT", SummarizationKind: "summary"}})
	chain, err = b.LoadCachedChainIfPresent(conv.ID)
	if err != nil || chain == nil || len(chain.ContextSummaries) != 1 || chain.ContextSummaries[0].Content != "PROBE_SUMMARY_NEXT" {
		t.Fatalf("cached probe served stale context: %#v, %v", chain, err)
	}
}

func TestCachedSingleNodeAndEmptyGraphSerializeArrays(t *testing.T) {
	db := newContextSummaryTestDB(t)
	conv, err := db.CreateConversation("single node", database.ConversationCreateMeta{})
	if err != nil {
		t.Fatal(err)
	}
	saveContextSummaryTestTrace(t, db, conv.ID, []agent.ChatMessage{{Role: "user", Content: "SUMMARY_FOR_ZERO_EDGE_GRAPH", SummarizationKind: "summary"}})
	if err := db.SaveAttackChainNode(conv.ID, "one-target", "target", "scope.example", "", "{}", 0); err != nil {
		t.Fatal(err)
	}
	b := &Builder{db: db, logger: zap.NewNop()}
	assertCachedArrays := func(wantNodes int) {
		t.Helper()
		chain, err := b.LoadChainFromDatabase(conv.ID)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(chain)
		if err != nil {
			t.Fatal(err)
		}
		var payload map[string]json.RawMessage
		if err := json.Unmarshal(encoded, &payload); err != nil {
			t.Fatal(err)
		}
		if string(payload["edges"]) != "[]" || len(chain.Nodes) != wantNodes || len(chain.ContextSummaries) != 1 {
			t.Fatalf("zero-edge cached response lost compatible graph/context shape: %s", encoded)
		}
		if wantNodes == 0 && string(payload["nodes"]) != "[]" {
			t.Fatalf("empty cached nodes are not an array: %s", encoded)
		}
	}
	assertCachedArrays(1)
	if err := db.DeleteAttackChain(conv.ID); err != nil {
		t.Fatal(err)
	}
	assertCachedArrays(0)
}

func TestBuildAndCachedContextKeepConversationScopeAndGraphEvidence(t *testing.T) {
	db := newContextSummaryTestDB(t)
	project, err := db.CreateProject(&database.Project{Name: "Shared project"})
	if err != nil {
		t.Fatal(err)
	}
	var convIDs []string
	for _, title := range []string{"A", "B"} {
		conv, err := db.CreateConversation(title, database.ConversationCreateMeta{})
		if err != nil {
			t.Fatal(err)
		}
		if err := db.SetConversationProjectID(conv.ID, project.ID); err != nil {
			t.Fatal(err)
		}
		convIDs = append(convIDs, conv.ID)
	}
	a, other := convIDs[0], convIDs[1]
	if _, err := db.AddMessage(a, "user", "Inspect scope.example", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AddMessage(a, "assistant", "Scan complete", []string{"execution-1"}); err != nil {
		t.Fatal(err)
	}
	saveContextSummaryTestTrace(t, db, a, []agent.ChatMessage{
		{Role: "system", Content: "OLD_SYSTEM_RULE", OriginalUserIntentEntries: []string{"Only scope.example is authorized"}},
		{Role: "user", Content: "SUMMARY_BEFORE_CURRENT_TURN", SummarizationKind: "summary"},
		{Role: "user", Content: "Inspect scope.example"},
		{Role: "user", Content: "GENERATED_CURRENT_SUMMARY", SummarizationKind: "summary"},
		{Role: "user", Content: "GENERATED_PLAN", GeneratedContextKind: agent.PlanExecuteExecutorContext},
		{Role: "assistant", ToolCalls: []agent.ToolCall{{ID: "call-1", Function: agent.FunctionCall{Name: "scan", Arguments: map[string]interface{}{"target": "scope.example"}}}}},
		{Role: "tool", ToolCallID: "call-1", Content: "Confirmed open port 443"},
	})
	saveContextSummaryTestTrace(t, db, other, []agent.ChatMessage{{Role: "user", Content: "OTHER_CONVERSATION_SUMMARY", SummarizationKind: "summary"}})
	if _, err := db.UpsertProjectFact(&database.ProjectFact{ProjectID: project.ID, FactKey: "note/context-other", Body: "BLACKBOARD_ONLY_SUMMARY", SourceConversationID: other}); err != nil {
		t.Fatal(err)
	}

	requests := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || len(payload.Messages) < 2 {
			http.Error(w, "bad model payload", http.StatusBadRequest)
			return
		}
		requests <- payload.Messages[1].Content
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"choices": []interface{}{map[string]interface{}{"message": map[string]string{"content": `{"nodes":[{"id":"target","type":"target","label":"scope.example"},{"id":"scan","type":"action","label":"Actual scan","tool_call_id":"call-1","metadata":{"tool_name":"scan"}}],"edges":[{"source":"target","target":"scan","type":"leads_to"}],"context_summaries":[{"content":"MODEL_INVENTED_SUMMARY"}]}`}}}})
	}))
	t.Cleanup(server.Close)
	b := NewBuilder(db, &config.OpenAIConfig{BaseURL: server.URL, APIKey: "test-only", Model: "gpt-4", MaxTotalTokens: 32000}, zap.NewNop())
	chain, err := b.BuildChainFromConversation(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	if len(chain.Nodes) != 2 || len(chain.Edges) != 1 || len(chain.ContextSummaries) != 2 {
		t.Fatalf("context changed graph or disappeared: %#v", chain)
	}
	prompt := <-requests
	for _, want := range []string{"[user]: Inspect scope.example", "[original_user_intent]", "Only scope.example is authorized", "[system_generated_summary]", "[system_generated_execution_context]", "[tool] (tool_call_id: call-1)", "Confirmed open port 443", "Do not create action/vulnerability nodes based only"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("model prompt lost %q", want)
		}
	}
	for _, forbidden := range []string{"OLD_SYSTEM_RULE", "[user]: GENERATED_CURRENT_SUMMARY", "[user]: GENERATED_PLAN", "SUMMARY_BEFORE_CURRENT_TURN", "OTHER_CONVERSATION_SUMMARY", "BLACKBOARD_ONLY_SUMMARY"} {
		if strings.Contains(prompt, forbidden) {
			t.Errorf("wrong source/turn entered graph prompt: %q", forbidden)
		}
	}
	if chain.ContextSummaries[0].Content != "SUMMARY_BEFORE_CURRENT_TURN" || chain.ContextSummaries[1].Content != "GENERATED_CURRENT_SUMMARY" {
		t.Fatalf("model response forged context or full-trace UI context was trimmed: %#v", chain.ContextSummaries)
	}

	// Cache is graph-only. Context is reread from this conversation's latest
	// trace rather than being frozen with the graph or fetched from the project.
	saveContextSummaryTestTrace(t, db, a, []agent.ChatMessage{{Role: "user", Content: "UPDATED_CONTEXT", SummarizationKind: "summary"}})
	cached, err := b.LoadChainFromDatabase(a)
	if err != nil || len(cached.Nodes) != 2 || cached.Nodes[0].ID != chain.Nodes[0].ID || len(cached.ContextSummaries) != 1 || cached.ContextSummaries[0].Content != "UPDATED_CONTEXT" {
		t.Fatalf("cached graph altered or summary became stale: %#v, %v", cached, err)
	}
	otherCached, err := b.LoadChainFromDatabase(other)
	if err != nil || len(otherCached.Nodes) != 0 || len(otherCached.ContextSummaries) != 1 || otherCached.ContextSummaries[0].Content != "OTHER_CONVERSATION_SUMMARY" {
		t.Fatalf("same-project conversation context crossed scope: %#v, %v", otherCached, err)
	}
	if err := db.SaveAgentTrace(a, "[]", ""); err != nil {
		t.Fatal(err)
	}
	cleared, err := b.LoadChainFromDatabase(a)
	if err != nil || len(cleared.Nodes) != 2 || len(cleared.ContextSummaries) != 0 {
		t.Fatalf("old summaries survived clearing current trace: %#v, %v", cleared, err)
	}
}
