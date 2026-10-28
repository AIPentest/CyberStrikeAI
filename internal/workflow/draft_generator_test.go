package workflow

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cyberstrike-ai/internal/config"
)

func TestGenerateDraftFromNaturalLanguageHighRiskAddsHITLAndValidGraph(t *testing.T) {
	result, err := GenerateDraftFromNaturalLanguage(context.Background(), DraftRequest{
		Prompt: "对目标资产做端口扫描，如果发现高危端口就执行加固脚本，最后输出报告",
		Options: DraftOptions{
			IncludeObjective: true,
			AllowSchedule:    false,
			AllowHighRisk:    false,
		},
		AvailableTools: []DraftTool{{Key: "nmap", Name: "nmap", Enabled: true}},
	})
	if err != nil {
		t.Fatalf("GenerateDraftFromNaturalLanguage: %v", err)
	}
	raw, _ := json.Marshal(result.Graph)
	if err := ValidateGraphJSON(context.Background(), string(raw)); err != nil {
		t.Fatalf("generated graph should validate: %v\n%s", err, raw)
	}
	if !result.Audit.HighRisk || !result.Audit.NeedsHITL || len(result.Audit.RiskWarnings) == 0 {
		t.Fatalf("audit did not flag high-risk HITL path: %#v", result.Audit)
	}
	var hasTool, hasHITL, hasConfirmation bool
	for _, node := range result.Graph.Nodes {
		if node.Type == "tool" && cfgString(node.Config, "tool_name") == "nmap" {
			hasTool = true
		}
		if node.Type == "hitl" {
			hasHITL = true
		}
		if cfgString(node.Config, "requires_human_confirmation") == "true" {
			hasConfirmation = true
		}
	}
	if !hasTool || !hasHITL || !hasConfirmation {
		t.Fatalf("expected nmap tool, HITL, and confirmation marker; tool=%v hitl=%v confirmation=%v", hasTool, hasHITL, hasConfirmation)
	}
}

func TestGenerateDraftAllowHighRiskStillLabelsConditionBranch(t *testing.T) {
	result, err := GenerateDraftFromNaturalLanguage(context.Background(), DraftRequest{
		Prompt: "如果漏洞扫描发现高危漏洞，允许生成执行修复脚本的草稿并输出报告",
		Options: DraftOptions{
			AllowHighRisk: true,
		},
		AvailableTools: []DraftTool{{Key: "nuclei", Name: "nuclei", Enabled: true}},
	})
	if err != nil {
		t.Fatalf("GenerateDraftFromNaturalLanguage: %v", err)
	}
	raw, _ := json.Marshal(result.Graph)
	if err := ValidateGraphJSON(context.Background(), string(raw)); err != nil {
		t.Fatalf("generated graph should validate: %v\n%s", err, raw)
	}
	branches := map[string]bool{}
	for _, edge := range result.Graph.Edges {
		if branch := cfgString(edge.Config, "branch"); branch != "" {
			branches[branch] = true
		}
	}
	if !branches["true"] || !branches["false"] {
		t.Fatalf("condition branches = %#v, want true and false", branches)
	}
}

func TestGenerateDraftFromLLMUsesOpenAICompatibleEndpoint(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("path = %s, want /chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("authorization = %q", got)
		}
		var payload struct {
			Temperature    float64 `json:"temperature"`
			ResponseFormat struct {
				Type string `json:"type"`
			} `json:"response_format"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if payload.Temperature != 0 || payload.ResponseFormat.Type != "json_object" {
			t.Fatalf("unexpected structured output controls: temperature=%v response_format=%#v", payload.Temperature, payload.ResponseFormat)
		}
		if len(payload.Messages) == 0 || strings.Contains(payload.Messages[0].Content, "start|tool|agent") {
			t.Fatalf("system prompt still contains pipe enum: %q", payload.Messages[0].Content)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"meta\":{\"id\":\"llm-port-scan\",\"name\":\"端口扫描\",\"description\":\"端口扫描\",\"enabled\":true},\"graph\":{\"nodes\":[{\"id\":\"start-1\",\"type\":\"start\",\"label\":\"开始\",\"position\":{\"x\":120,\"y\":150},\"config\":{\"input_keys\":\"message, target\"}},{\"id\":\"tool-2\",\"type\":\"tool\",\"label\":\"端口扫描\",\"position\":{\"x\":330,\"y\":150},\"config\":{\"tool_name\":\"nmap\",\"arguments\":\"{\\\"target\\\":\\\"{{inputs.target}}\\\"}\",\"timeout_seconds\":\"120\",\"join_strategy\":\"all_merge\"}},{\"id\":\"output-3\",\"type\":\"output\",\"label\":\"输出报告\",\"position\":{\"x\":540,\"y\":150},\"config\":{\"source_binding\":{\"from\":\"previous\",\"field\":\"output\"},\"join_strategy\":\"all_merge\"}}],\"edges\":[{\"id\":\"edge-1\",\"source\":\"start-1\",\"target\":\"tool-2\"},{\"id\":\"edge-2\",\"source\":\"tool-2\",\"target\":\"output-3\"}],\"config\":{\"schema_version\":1}},\"capabilities\":[{\"label\":\"端口扫描\",\"tool_name\":\"nmap\",\"tool_candidates\":[\"nmap\"]}],\"audit\":{\"assumptions\":[]}}"}}]}`))
	}))
	defer srv.Close()

	result, err := GenerateDraftFromLLM(context.Background(), DraftRequest{
		Prompt:         "对目标做端口扫描并输出报告",
		AvailableTools: []DraftTool{{Key: "nmap", Name: "nmap", Enabled: true}},
	}, config.OpenAIConfig{APIKey: "test-key", BaseURL: srv.URL, Model: "test-model"}, nil)
	if err != nil {
		t.Fatalf("GenerateDraftFromLLM: %v", err)
	}
	if !called {
		t.Fatal("expected LLM endpoint to be called")
	}
	if result.Generator != "llm" || !result.Audit.Savable || result.Meta.ID != "llm-port-scan" {
		t.Fatalf("unexpected result: %#v", result)
	}
	for _, node := range result.Graph.Nodes {
		if node.Type == "output" && cfgString(node.Config, "output_key") != "result" {
			t.Fatalf("output_key = %q, want result", cfgString(node.Config, "output_key"))
		}
	}
}

func TestGenerateDraftFromLLMReturnsErrorOnMalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"meta\":{\"id\":\"bad\"},|\"graph\":{\"nodes\":[]}}"}}]}`))
	}))
	defer srv.Close()

	_, err := GenerateDraftFromLLM(context.Background(), DraftRequest{
		Prompt: "随便生成一个工作流，要求所有节点都用到输出变量",
		Options: DraftOptions{
			IncludeObjective: true,
		},
	}, config.OpenAIConfig{APIKey: "test-key", BaseURL: srv.URL, Model: "test-model"}, nil)
	if err == nil {
		t.Fatal("expected malformed JSON error")
	}
	if !strings.Contains(err.Error(), "failed to parse the LLM workflow JSON") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNormalizeLLMDraftRepairsMissingRequiredConfig(t *testing.T) {
	result := normalizeLLMDraft("随便生成一个工作流", DraftRequest{}, llmDraftEnvelope{
		Graph: graphDef{
			Nodes: []graphNode{
				{ID: "start-1", Type: "start", Label: "开始", Config: map[string]any{}},
				{ID: "agent-1", Type: "agent", Label: "分析", Config: map[string]any{}},
				{ID: "out-1", Type: "output", Label: "输出结果", Config: map[string]any{}},
			},
			Edges: []graphEdge{
				{ID: "e1", Source: "start-1", Target: "agent-1"},
				{ID: "e2", Source: "agent-1", Target: "out-1"},
			},
		},
	})
	raw, _ := json.Marshal(result.Graph)
	if err := ValidateGraphJSON(context.Background(), string(raw)); err != nil {
		t.Fatalf("normalized graph should validate: %v\n%s", err, raw)
	}
	var agentKey, outputKey string
	for _, node := range result.Graph.Nodes {
		switch node.Type {
		case "agent":
			agentKey = cfgString(node.Config, "output_key")
		case "output":
			outputKey = cfgString(node.Config, "output_key")
		}
	}
	if agentKey == "" || outputKey != "result" {
		t.Fatalf("agentKey=%q outputKey=%q", agentKey, outputKey)
	}
}

func TestNormalizeLLMDraftRepairsConditionBranches(t *testing.T) {
	result := normalizeLLMDraft("如果发现异常则输出详情，否则输出正常", DraftRequest{}, llmDraftEnvelope{
		Graph: graphDef{
			Nodes: []graphNode{
				{ID: "start-1", Type: "start", Label: "开始", Config: map[string]any{}},
				{ID: "cond-1", Type: "condition", Label: "判断", Config: map[string]any{"expression": `{{inputs.message}} != ""`}},
				{ID: "out-yes", Type: "output", Label: "异常", Config: map[string]any{}},
				{ID: "out-no", Type: "output", Label: "正常", Config: map[string]any{}},
			},
			Edges: []graphEdge{
				{ID: "e1", Source: "start-1", Target: "cond-1"},
				{ID: "e2", Source: "cond-1", Target: "out-yes"},
				{ID: "e3", Source: "cond-1", Target: "out-no"},
			},
		},
	})
	raw, _ := json.Marshal(result.Graph)
	if err := ValidateGraphJSON(context.Background(), string(raw)); err != nil {
		t.Fatalf("normalized graph should validate: %v\n%s", err, raw)
	}
	branches := map[string]bool{}
	for _, edge := range result.Graph.Edges {
		if edge.Source == "cond-1" {
			branches[cfgString(edge.Config, "branch")] = true
		}
	}
	if !branches["true"] || !branches["false"] {
		t.Fatalf("branches = %#v, want true and false", branches)
	}
}
