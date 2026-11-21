package mcp

import (
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"testing"
)

func TestSDKToolMetadataAndStructuredResultsSurviveTheBridge(t *testing.T) {
	no := false
	defs := sdkToolsToOur([]*sdk.Tool{{Name: "fixture_read", Description: "Read fixture", InputSchema: map[string]any{"type": "object"}, OutputSchema: map[string]any{"type": "object", "required": []string{"value"}}, Annotations: &sdk.ToolAnnotations{Title: "Fixture", ReadOnlyHint: true, IdempotentHint: true, DestructiveHint: &no}}})
	if len(defs) != 1 || defs[0].OutputSchema["type"] != "object" || !defs[0].Annotations["readOnlyHint"] || defs[0].Annotations["destructiveHint"] || defs[0].ShortDescription != "Fixture" {
		t.Fatalf("lost tool metadata: %+v", defs)
	}
	result := sdkCallToolResultToOurs(&sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "observed"}}, StructuredContent: map[string]any{"value": "observed"}})
	if result.StructuredContent["value"] != "observed" || result.Content[0].Text != "observed" {
		t.Fatalf("lost structured output: %+v", result)
	}
}
