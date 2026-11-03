package assessment

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/mcp"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap"
)

func TestExternalModelToolNamesAndConcurrentRoleMapping(t *testing.T) {
	backend := sdk.NewServer(&sdk.Implementation{Name: "fixture", Version: "1"}, nil)
	for _, name := range []string{"one", "two"} {
		backend.AddTool(&sdk.Tool{Name: name, InputSchema: map[string]any{"type": "object"}}, func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "fixture success"}}}, nil
		})
	}
	server := httptest.NewServer(sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return backend }, &sdk.StreamableHTTPOptions{Stateless: true}))
	defer server.Close()
	manager := mcp.NewExternalMCPManager(zap.NewNop())
	defer manager.StopAll()
	manager.SetToolAuthorizer(func(context.Context, string, map[string]interface{}) error { return nil })
	if err := manager.AddOrUpdateConfig("fixture", config.ExternalMCPServerConfig{Type: "http", URL: server.URL, ExternalMCPEnable: true, Timeout: 5}); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	ag := agent.NewAgent(&cfg.OpenAI, &cfg.Agent, mcp.NewServer(zap.NewNop()), manager, zap.NewNop(), 5)
	rt := &CSAIRuntime{Agent: ag}
	var tools []agent.Tool
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		tools, _ = rt.Tools(&Task{Tools: []string{"fixture__two"}})
		if len(tools) == 1 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(tools) != 1 || tools[0].Function.Name != "fixture__two" {
		t.Fatal("external tool selection rejected normalized model name", tools)
	}
	// A different role discovery must not erase another run's transport mapping.
	ag.ToolsForRole([]string{"fixture::one"})
	result, err := ag.ExecuteMCPToolForConversation(context.Background(), "fixture-run", "fixture__two", map[string]interface{}{})
	if err != nil || result == nil || result.IsError {
		t.Fatal("role lookup erased external mapping", result, err)
	}
}
