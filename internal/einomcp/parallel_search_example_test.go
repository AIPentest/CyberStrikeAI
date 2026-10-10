package einomcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/config"
	appmcp "cyberstrike-ai/internal/mcp"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap"
)

const parallelSource = "https://cheatsheetseries.owasp.org/cheatsheets/SQL_Injection_Prevention_Cheat_Sheet.html"

// Record the actual outbound HTTP requests, including discovery and tool calls.
// Only this test replaces the transport; application clients remain unchanged.
type parallelRequestRecorder struct {
	base       http.RoundTripper
	mu         sync.Mutex
	methods    map[string]int
	badHeaders []string
}

func (r *parallelRequestRecorder) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method == http.MethodPost {
		var body struct {
			Method string `json:"method"`
			Params struct {
				Name string `json:"name"`
			} `json:"params"`
		}
		if req.Body != nil {
			data, err := io.ReadAll(req.Body)
			if err != nil {
				return nil, err
			}
			req.Body = io.NopCloser(strings.NewReader(string(data)))
			if err := json.Unmarshal(data, &body); err != nil {
				return nil, err
			}
		}
		key := body.Method
		if body.Params.Name != "" {
			key += ":" + body.Params.Name
		}
		r.mu.Lock()
		r.methods[key]++
		if req.Header.Get("User-Agent") != "CyberStrikeAI/1.0.0" || req.Header.Get("Authorization") != "" || req.Header.Get("X-API-Key") != "" {
			r.badHeaders = append(r.badHeaders, key)
		}
		r.mu.Unlock()
	}
	return r.base.RoundTrip(req)
}

func loadParallelExample(t *testing.T) *config.Config {
	t.Helper()
	// No saved settings or dotenv loader is involved, and no inherited key is used.
	for _, name := range []string{"PARALLEL_API_KEY", "PARALLEL_API_TOKEN", "PARALLEL_KEY"} {
		t.Setenv(name, "")
	}
	data, err := os.ReadFile("../../docs/examples/parallel-search.yaml")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	srv := cfg.ExternalMCP.Servers["parallel-search"]
	if srv.GetTransportType() != "http" || srv.URL != "https://search.parallel.ai/mcp" || srv.Timeout != 60 || !srv.Disabled || srv.ExternalMCPEnable {
		t.Fatalf("unexpected effective example: %+v", srv)
	}
	if len(srv.Headers) != 1 || srv.Headers["User-Agent"] != "CyberStrikeAI/1.0.0" || len(srv.Env) != 0 || len(srv.AutoApprove) != 0 {
		t.Fatalf("example must identify the client without credentials or automatic approvals: %+v", srv)
	}
	return cfg
}

func runParallelExample(t *testing.T, endpoint string, live bool) {
	t.Helper()
	cfg := loadParallelExample(t)
	srv := cfg.ExternalMCP.Servers["parallel-search"]
	// Match the documented file-edit opt-in, rather than only changing a struct.
	data, err := os.ReadFile("../../docs/examples/parallel-search.yaml")
	if err != nil {
		t.Fatal(err)
	}
	enabled := strings.Replace(string(data), "disabled: true", "disabled: false", 1)
	if !live {
		enabled = strings.Replace(enabled, srv.URL, endpoint, 1)
	}
	configPath := filepath.Join(t.TempDir(), "enabled.yaml")
	if err := os.WriteFile(configPath, []byte(enabled), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err = config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ExternalMCP.Servers["parallel-search"].ExternalMCPEnable {
		t.Fatal("documented opt-in did not enable server")
	}
	recorder := &parallelRequestRecorder{base: http.DefaultTransport, methods: map[string]int{}}
	oldTransport := http.DefaultTransport
	http.DefaultTransport = recorder
	t.Cleanup(func() { http.DefaultTransport = oldTransport })
	log := zap.NewNop()
	manager := appmcp.NewExternalMCPManager(log)
	t.Cleanup(manager.StopAll)
	disabledCfg := loadParallelExample(t)
	manager.LoadConfigs(&disabledCfg.ExternalMCP)
	disabledAgent := agent.NewAgent(&config.OpenAIConfig{}, nil, appmcp.NewServer(log), manager, log, 2)
	if tools := disabledAgent.ToolsForRole(nil); len(tools) != 0 {
		t.Fatal("disabled example exposed agent tools")
	}
	manager.LoadConfigs(&cfg.ExternalMCP)
	manager.ConfigureToolWaitTimeoutSeconds(0)
	manager.ConfigureToolResultSpillRoot(t.TempDir())
	if err := manager.StartClient("parallel-search"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(60 * time.Second)
	for {
		client, ok := manager.GetClient("parallel-search")
		if ok && client.IsConnected() {
			break
		}
		if ok && client.GetStatus() == "error" {
			t.Fatalf("connection failed: %s", manager.GetError("parallel-search"))
		}
		if time.Now().After(deadline) {
			t.Fatal("connection did not become ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	ag := agent.NewAgent(&config.OpenAIConfig{}, &config.AgentConfig{ToolTimeoutMinutes: 1}, appmcp.NewServer(log), manager, log, 2)
	defs := ag.ToolsForRole([]string{"parallel-search::web_search", "parallel-search::web_fetch"})
	if len(defs) != 2 {
		t.Fatalf("want two discovered role-selected tools, got %d", len(defs))
	}
	if restricted := ag.ToolsForRole([]string{"parallel-search::web_search"}); len(restricted) != 1 || !strings.HasSuffix(restricted[0].Function.Name, "web_search") {
		t.Fatal("role restriction did not preserve selected tool")
	}
	// Rebuild both definitions and their name mapping for the runner.
	defs = ag.ToolsForRole([]string{"parallel-search::web_search", "parallel-search::web_fetch"})
	holder := &ConversationHolder{}
	holder.Set("parallel-example-validation")
	var executionIDs []string
	tools, err := ToolsFromDefinitions(ag, holder, defs, func(id, _ string) { executionIDs = append(executionIDs, id) }, nil, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	scripted := &parallelAnswerFixture{}
	for _, def := range defs {
		if strings.HasSuffix(def.Function.Name, "web_search") {
			scripted.search = def.Function.Name
		}
		if strings.HasSuffix(def.Function.Name, "web_fetch") {
			scripted.fetch = def.Function.Name
		}
	}
	chatAgent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name: "public-research-example", Model: scripted,
		ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{Tools: tools}},
	})
	if err != nil {
		t.Fatal(err)
	}
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: chatAgent})
	events := runner.Query(ctx, "Explain OWASP parameterized query guidance and cite the source.")
	finalAnswer := ""
	for {
		event, ok := events.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			t.Fatal(event.Err)
		}
		if event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}
		message, err := event.Output.MessageOutput.GetMessage()
		if err != nil {
			t.Fatal(err)
		}
		if message.Role == schema.Tool {
			if strings.HasPrefix(message.Content, ToolErrorPrefix) || !strings.Contains(message.Content, "https://") || !strings.Contains(strings.ToLower(message.Content), "paramet") {
				t.Fatalf("tool did not produce useful agent-facing output: %s", message.Content)
			}
			t.Logf("agent-facing tool result: %s", message.Content)
		}
		if message.Role == schema.Assistant && len(message.ToolCalls) == 0 {
			finalAnswer = message.Content
		}
	}
	if !strings.Contains(finalAnswer, parallelSource) || !strings.Contains(strings.ToLower(finalAnswer), "paramet") {
		t.Fatalf("runner did not return useful sourced final answer: %s", finalAnswer)
	}
	t.Logf("scripted runner final answer: %s", finalAnswer)
	if len(executionIDs) != 2 {
		t.Fatalf("missing execution monitoring IDs: %v", executionIDs)
	}
	for _, id := range executionIDs {
		execution, ok := manager.GetExecution(id)
		if !ok || execution.ConversationID != holder.Get() || execution.Status != appmcp.ToolExecutionStatusCompleted {
			t.Fatalf("execution did not retain conversation and successful status: %+v", execution)
		}
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	for _, method := range []string{"initialize", "tools/list", "tools/call:web_search", "tools/call:web_fetch"} {
		if recorder.methods[method] == 0 {
			t.Fatalf("missing observed HTTP request %s", method)
		}
	}
	if len(recorder.badHeaders) != 0 {
		t.Fatalf("incorrect attribution or inherited authentication on %v", recorder.badHeaders)
	}
	t.Logf("observed keyless CyberStrikeAI requests: %v", recorder.methods)
}

// A deterministic model fixture drives the real ADK runner without an AI key.
// Its final answer is built only from the tool result delivered by the bridge.
type parallelAnswerFixture struct{ search, fetch string }

func (f *parallelAnswerFixture) WithTools(_ []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return f, nil
}
func (f *parallelAnswerFixture) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, fmt.Errorf("fixture uses nonstreaming execution")
}
func (f *parallelAnswerFixture) Generate(_ context.Context, messages []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	var results []string
	for _, message := range messages {
		if message.Role == schema.Tool {
			results = append(results, message.Content)
		}
	}
	if len(results) < 2 {
		name := f.search
		args := `{"objective":"Find official OWASP guidance explaining parameterized queries for SQL injection prevention","search_queries":["OWASP SQL injection parameterized queries"]}`
		if len(results) == 1 {
			name = f.fetch
			args = fmt.Sprintf(`{"urls":[%q],"objective":"Explain parameterized queries for SQL injection prevention"}`, parallelSource)
		}
		return &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{ID: fmt.Sprintf("public-research-%d", len(results)), Type: "function", Function: schema.FunctionCall{Name: name, Arguments: args}}}}, nil
	}
	var response struct {
		Results []struct {
			URL      string   `json:"url"`
			Excerpts []string `json:"excerpts"`
		} `json:"results"`
	}
	last := results[len(results)-1]
	if strings.HasPrefix(last, "<persisted-output>") {
		// Preserve the same bounded preview the application gives the model.
		// Do not read the spilled file behind the model's back.
		_, preview, ok := strings.Cut(last, "Preview (first 2000):\n")
		if !ok {
			return nil, fmt.Errorf("persisted output has no visible preview")
		}
		preview = strings.TrimSuffix(strings.TrimSpace(preview), "</persisted-output>")
		return &schema.Message{Role: schema.Assistant, Content: "Source excerpts:\n" + strings.TrimSpace(preview)}, nil
	}
	if err := json.Unmarshal([]byte(last), &response); err != nil {
		return nil, err
	}
	if len(response.Results) == 0 || len(response.Results[0].Excerpts) == 0 {
		return nil, fmt.Errorf("tool returned no source excerpts")
	}
	source := response.Results[0]
	return &schema.Message{Role: schema.Assistant, Content: strings.Join(source.Excerpts, "\n") + "\nSource: " + source.URL}, nil
}

func TestParallelSearchExample(t *testing.T) {
	srv := sdk.NewServer(&sdk.Implementation{Name: "public-research-fixture", Version: "1.0.0"}, nil)
	schema := map[string]any{"type": "object", "properties": map[string]any{"objective": map[string]any{"type": "string"}, "search_queries": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}}, "required": []string{"objective", "search_queries"}}
	for _, name := range []string{"web_search", "web_fetch"} {
		inputSchema := schema
		if name == "web_fetch" {
			inputSchema = map[string]any{"type": "object", "properties": map[string]any{"urls": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "objective": map[string]any{"type": "string"}}, "required": []string{"urls"}}
		}
		srv.AddTool(&sdk.Tool{Name: name, Description: "Public documentation research", InputSchema: inputSchema}, func(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: `{"results":[{"url":"` + parallelSource + `","excerpts":["Use prepared statements with parameterized queries so the database distinguishes code from data."]}]}`}}}, nil
		})
	}
	server := httptest.NewServer(sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return srv }, &sdk.StreamableHTTPOptions{Stateless: true}))
	t.Cleanup(server.Close)
	runParallelExample(t, server.URL, false)
}

func TestParallelSearchLive(t *testing.T) {
	if os.Getenv("CYBERSTRIKE_PARALLEL_LIVE") != "1" {
		t.Skip("set CYBERSTRIKE_PARALLEL_LIVE=1 for public network validation")
	}
	runParallelExample(t, "", true)
}
