package multiagent

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"cyberstrike-ai/internal/config"

	localbk "github.com/cloudwego/eino-ext/adk/backend/local"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

func TestReductionCacheRootDir(t *testing.T) {
	got := reductionCacheRootDir("", "proj-1", "conv-1")
	want := filepath.Join("tmp", "reduction", "projects", "proj-1")
	if got != want {
		t.Fatalf("project scope: got %q want %q", got, want)
	}
	got = reductionCacheRootDir("", "", "conv-abc")
	want = filepath.Join("tmp", "reduction", "conversations", "conv-abc")
	if got != want {
		t.Fatalf("conversation scope: got %q want %q", got, want)
	}
	custom := reductionCacheRootDir("/data/cache", "p1", "c1")
	if !strings.HasSuffix(custom, filepath.Join("projects", "p1")) {
		t.Fatalf("custom base should still scope by project, got %q", custom)
	}
}

func TestSanitizeEinoPathSegmentForWindows(t *testing.T) {
	got := sanitizeEinoPathSegment(`call_2hwSDB7U504wTZ7Y8t1DKjtb|fc_0bf3b3607a76b6c4016a8eed99151c87d2b7ecfa3c97cb75ee`)
	if strings.ContainsAny(got, `<>:"/\|?*`) {
		t.Fatalf("sanitized path segment still contains a Windows-invalid character: %q", got)
	}
	if strings.ContainsRune(got, '\x00') {
		t.Fatalf("sanitized path segment still contains a control character: %q", got)
	}
	if strings.Contains(got, "..") {
		t.Fatalf("sanitized path segment still contains traversal: %q", got)
	}
}

func TestBuildAgenticReductionMiddlewareClearsOldAgenticToolResult(t *testing.T) {
	ctx := context.Background()
	loc, err := localbk.NewBackend(ctx, &localbk.Config{})
	if err != nil {
		t.Fatalf("NewBackend: %v", err)
	}
	root := t.TempDir()
	mw, err := buildAgenticReductionMiddleware(ctx, config.MultiAgentEinoMiddlewareConfig{
		ReductionRootDir:           root,
		ReductionMaxTokensForClear: 1,
	}, "", "conv-1", loc, nil)
	if err != nil {
		t.Fatalf("buildAgenticReductionMiddleware: %v", err)
	}
	oldText := strings.Repeat("old-tool-output-", 20)
	newText := strings.Repeat("new-tool-output-", 20)
	oldCallID := "call_2hwSDB7U504wTZ7Y8t1DKjtb|fc_0bf3b3607a76b6c4016a8eed99151c87d2b7ecfa3c97cb75ee"
	state := &adk.TypedChatModelAgentState[*schema.AgenticMessage]{
		Messages: []*schema.AgenticMessage{
			agenticAssistantToolCall(oldCallID, "execute", `{"command":"old"}`),
			agenticToolResult(oldCallID, "execute", oldText),
			agenticAssistantToolCall("new-call", "execute", `{"command":"new"}`),
			agenticToolResult("new-call", "execute", newText),
		},
	}
	_, out, err := mw.BeforeModelRewriteState(ctx, state, nil)
	if err != nil {
		t.Fatalf("BeforeModelRewriteState: %v", err)
	}
	oldGot := out.Messages[1].ContentBlocks[0].FunctionToolResult.Content[0].Text.Text
	newGot := out.Messages[3].ContentBlocks[0].FunctionToolResult.Content[0].Text.Text
	if oldGot == oldText {
		t.Fatal("agentic reduction did not clear old oversized tool result")
	}
	if !strings.Contains(oldGot, "read_file") {
		t.Fatalf("cleared content should mention read_file, got %q", oldGot)
	}
	if newGot != newText {
		t.Fatalf("latest tool result should be retained, got %q", newGot)
	}
	files, err := filepath.Glob(filepath.Join(root, "conversations", "conv-1", "clear", "*"))
	if err != nil {
		t.Fatalf("glob clear offload files: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("clear offload files = %d, want one sanitized file: %v", len(files), files)
	}
	if strings.ContainsAny(filepath.Base(files[0]), `<>:"/\|?*`) {
		t.Fatalf("clear offload file name is not Windows-safe: %q", filepath.Base(files[0]))
	}
}

func agenticAssistantToolCall(callID, name, arguments string) *schema.AgenticMessage {
	return &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{schema.NewContentBlock(&schema.FunctionToolCall{
			CallID:    callID,
			Name:      name,
			Arguments: arguments,
		})},
	}
}

func agenticToolResult(callID, name, text string) *schema.AgenticMessage {
	return &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeUser,
		ContentBlocks: []*schema.ContentBlock{schema.NewContentBlock(&schema.FunctionToolResult{
			CallID: callID,
			Name:   name,
			Content: []*schema.FunctionToolResultContentBlock{{
				Type: schema.FunctionToolResultContentBlockTypeText,
				Text: &schema.UserInputText{Text: text},
			}},
		})},
	}
}

func TestBuildAgenticReductionMiddlewareHandlesSingleAgenticToolResult(t *testing.T) {
	ctx := context.Background()
	loc, err := localbk.NewBackend(ctx, &localbk.Config{})
	if err != nil {
		t.Fatalf("NewBackend: %v", err)
	}
	mw, err := buildAgenticReductionMiddleware(ctx, config.MultiAgentEinoMiddlewareConfig{
		ReductionRootDir:           t.TempDir(),
		ReductionMaxTokensForClear: 1,
	}, "", "conv-1", loc, nil)
	if err != nil {
		t.Fatalf("buildAgenticReductionMiddleware: %v", err)
	}
	state := &adk.TypedChatModelAgentState[*schema.AgenticMessage]{
		Messages: []*schema.AgenticMessage{
			{
				Role: schema.AgenticRoleTypeUser,
				ContentBlocks: []*schema.ContentBlock{schema.NewContentBlock(&schema.FunctionToolResult{
					CallID: "call-1",
					Name:   "execute",
					Content: []*schema.FunctionToolResultContentBlock{{
						Type: schema.FunctionToolResultContentBlockTypeText,
						Text: &schema.UserInputText{Text: strings.Repeat("tool-output-", 20)},
					}},
				})},
			},
		},
	}
	_, out, err := mw.BeforeModelRewriteState(ctx, state, nil)
	if err != nil {
		t.Fatalf("BeforeModelRewriteState: %v", err)
	}
	got := out.Messages[0].ContentBlocks[0].FunctionToolResult.Content[0].Text.Text
	if got != strings.Repeat("tool-output-", 20) {
		t.Fatalf("single retained tool result should not be cleared, got %q", got)
	}
}

func TestPrependEinoAgenticMiddlewaresRespectsReductionPlacement(t *testing.T) {
	ctx := context.Background()
	loc, err := localbk.NewBackend(ctx, &localbk.Config{})
	if err != nil {
		t.Fatalf("NewBackend: %v", err)
	}
	patchToolCalls := false
	mw := &config.MultiAgentEinoMiddlewareConfig{
		ReductionEnable:            true,
		ReductionRootDir:           t.TempDir(),
		ReductionMaxTokensForClear: 100,
		PatchToolCalls:             &patchToolCalls,
	}
	_, mainHandlers, _, err := prependEinoAgenticMiddlewares(ctx, mw, einoMWMain, nil, loc, "", "conv-1", "", nil)
	if err != nil {
		t.Fatalf("prepend main: %v", err)
	}
	if len(mainHandlers) != 1 {
		t.Fatalf("main handlers = %d, want reduction", len(mainHandlers))
	}
	_, subHandlers, _, err := prependEinoAgenticMiddlewares(ctx, mw, einoMWSub, nil, loc, "", "conv-1", "", nil)
	if err != nil {
		t.Fatalf("prepend sub: %v", err)
	}
	if len(subHandlers) != 0 {
		t.Fatalf("sub handlers = %d, want skipped when reduction_sub_agents=false", len(subHandlers))
	}
	mw.ReductionSubAgents = true
	_, subHandlers, _, err = prependEinoAgenticMiddlewares(ctx, mw, einoMWSub, nil, loc, "", "conv-1", "", nil)
	if err != nil {
		t.Fatalf("prepend sub enabled: %v", err)
	}
	if len(subHandlers) != 1 {
		t.Fatalf("sub handlers = %d, want reduction when reduction_sub_agents=true", len(subHandlers))
	}
}

func TestPrependEinoAgenticMiddlewaresMountsToolSearchAndPatchToolCalls(t *testing.T) {
	ctx := context.Background()
	mw := &config.MultiAgentEinoMiddlewareConfig{
		ToolSearchEnable:        true,
		ToolSearchMinTools:      20,
		ToolSearchAlwaysVisible: 5,
	}
	outTools, handlers, toolSearchActive, err := prependEinoAgenticMiddlewares(ctx, mw, einoMWMain, stubTools(25), nil, "", "conv-test", "", nil)
	if err != nil {
		t.Fatalf("prependEinoAgenticMiddlewares: %v", err)
	}
	if !toolSearchActive {
		t.Fatal("agentic tool_search should be active")
	}
	if len(outTools) != 5 {
		t.Fatalf("mounted tools = %d, want static visible tools only", len(outTools))
	}
	if len(handlers) != 2 {
		t.Fatalf("handlers = %d, want patchtoolcalls + toolsearch", len(handlers))
	}
}

type stubTool struct{ name string }

func (s stubTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: s.name}, nil
}

func TestSplitToolsForToolSearch(t *testing.T) {
	mk := func(n int) []tool.BaseTool {
		out := make([]tool.BaseTool, n)
		for i := 0; i < n; i++ {
			out[i] = stubTool{name: fmt.Sprintf("t%d", i)}
		}
		return out
	}
	static, dynamic, ok := splitToolsForToolSearch(mk(4), 3)
	if ok || len(static) != 4 || dynamic != nil {
		t.Fatalf("expected no split when len<=alwaysVisible+1, got ok=%v static=%d dynamic=%v", ok, len(static), dynamic)
	}
	static, dynamic, ok = splitToolsForToolSearch(mk(20), 5)
	if !ok || len(static) != 5 || len(dynamic) != 15 {
		t.Fatalf("expected split 5+15, got ok=%v static=%d dynamic=%d", ok, len(static), len(dynamic))
	}
}
