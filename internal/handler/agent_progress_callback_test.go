package handler

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/openai"

	"go.uber.org/zap"
)

// TestCreateProgressCallback_ConcurrentToolEvents 回归 issue #142：并行 tool 回调不得 concurrent map panic。
func TestCreateProgressCallback_ConcurrentToolEvents(t *testing.T) {
	logger := zap.NewNop()
	h := &AgentHandler{
		logger: logger,
		config: &config.Config{},
	}
	cb := h.createProgressCallback(context.Background(), nil, "conv-race-test", "", nil)

	const workers = 64
	var wg sync.WaitGroup
	wg.Add(workers * 2)
	for i := 0; i < workers; i++ {
		i := i
		go func() {
			defer wg.Done()
			toolCallID := fmt.Sprintf("tc-%d", i)
			cb("tool_call", "calling skill", map[string]interface{}{
				"toolCallId":   toolCallID,
				"toolName":     "skill",
				"argumentsObj": map[string]interface{}{"skill_name": "demo-skill"},
			})
		}()
		go func() {
			defer wg.Done()
			toolCallID := fmt.Sprintf("tc-%d", i)
			cb("tool_result", "skill done", map[string]interface{}{
				"toolCallId": toolCallID,
				"toolName":   "skill",
				"success":    true,
			})
		}()
	}
	wg.Wait()
}

func TestCreateProgressCallback_RecordsSkillStats(t *testing.T) {
	logger := zap.NewNop()
	db, err := database.NewDB(filepath.Join(t.TempDir(), "test.sqlite"), logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	h := &AgentHandler{logger: logger, config: &config.Config{}, db: db}
	cb := h.createProgressCallback(context.Background(), nil, "conv-skill-stats", "", nil)

	cb("tool_call", "", map[string]interface{}{
		"toolName": "skill", "toolCallId": "pi-success",
		"arguments": map[string]interface{}{"name": "demo-skill"},
	})
	piSuccess := map[string]interface{}{
		"toolName": "skill", "toolCallId": "pi-success", "success": true, "isError": false,
	}
	cb("tool_result", "loaded", piSuccess)
	cb("tool_result", "loaded", piSuccess)

	cb("tool_call", "", map[string]interface{}{
		"toolName": "skill", "toolCallId": "pi-failure",
		"arguments": map[string]interface{}{"name": "demo-skill"},
	})
	cb("tool_result", "failed", map[string]interface{}{
		"toolName": "skill", "toolCallId": "pi-failure", "success": false, "isError": true,
	})

	cb("tool_call", "", map[string]interface{}{
		"toolName": "skill", "toolCallId": "legacy-success",
		"argumentsObj": map[string]interface{}{"skill": "legacy-skill"},
	})
	cb("tool_result", "loaded", map[string]interface{}{
		"toolName": "skill", "toolCallId": "legacy-success", "success": true,
	})

	stats, err := db.LoadSkillStats()
	if err != nil {
		t.Fatal(err)
	}
	if got := stats["demo-skill"]; got == nil || got.TotalCalls != 2 || got.SuccessCalls != 1 || got.FailedCalls != 1 {
		t.Errorf("Pi stats = %+v, want two calls with one success and one failure", got)
	}
	if got := stats["legacy-skill"]; got == nil || got.TotalCalls != 1 || got.SuccessCalls != 1 || got.FailedCalls != 0 {
		t.Errorf("legacy stats = %+v, want one successful call", got)
	}
}

// TestCreateProgressCallback_MirrorsWebStreamEvents 页面刷新后 task-events 订阅必须
// 继续收到原 Web SSE 任务的后续事件，不能只等数据库最终结果。
func TestCreateProgressCallback_MirrorsWebStreamEvents(t *testing.T) {
	bus := NewTaskEventBus()
	h := &AgentHandler{logger: zap.NewNop(), config: &config.Config{}, taskEventBus: bus}
	_, events := bus.Subscribe("conv-refresh-stream")
	primaryCalls := 0
	cb := h.createProgressCallback(
		context.Background(), nil, "conv-refresh-stream", "",
		func(eventType, message string, data interface{}) { primaryCalls++ },
	)

	cb("progress", "第 3 轮", map[string]interface{}{"iteration": 3})
	if primaryCalls != 1 {
		t.Fatalf("expected primary SSE callback once, got %d", primaryCalls)
	}
	select {
	case payload := <-events:
		body := string(payload)
		if !strings.Contains(body, `"type":"progress"`) || !strings.Contains(body, `"conversationId":"conv-refresh-stream"`) {
			t.Fatalf("unexpected mirrored event: %s", body)
		}
	case <-time.After(time.Second):
		t.Fatal("expected progress event mirrored to task event bus")
	}
}

func TestCreateProgressCallback_HidesInternalAgentDiagnostics(t *testing.T) {
	tmp := t.TempDir()
	db, err := database.NewDB(filepath.Join(tmp, "test.sqlite"), zap.NewNop())
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	conv, err := db.CreateConversation("diag-hidden", database.ConversationCreateMeta{})
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	asst, err := db.AddMessage(conv.ID, "assistant", "处理中...", nil)
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}
	bus := NewTaskEventBus()
	h := &AgentHandler{logger: zap.NewNop(), db: db, taskEventBus: bus}
	_, events := bus.Subscribe(conv.ID)
	primaryCalls := 0
	cb := h.createProgressCallback(
		context.Background(), nil, conv.ID, asst.ID,
		func(string, string, interface{}) { primaryCalls++ },
	)

	cb("model_output_rejected", "模型工具调用不完整或参数不安全，已阻止执行并要求重写。", map[string]interface{}{
		"reason": "invalid_tool_arguments_json",
	})
	cb("progress", "Agent TurnLoop 常驻多轮 runtime 已接管本轮会话。", map[string]interface{}{
		"kind": "turn_loop_takeover",
	})

	if primaryCalls != 0 {
		t.Fatalf("primary SSE calls = %d, want hidden diagnostics", primaryCalls)
	}
	select {
	case payload := <-events:
		t.Fatalf("unexpected mirrored diagnostic event: %s", string(payload))
	default:
	}
	details, err := db.GetProcessDetails(asst.ID)
	if err != nil {
		t.Fatalf("GetProcessDetails: %v", err)
	}
	if len(details) != 0 {
		t.Fatalf("process details = %+v, want no diagnostics persisted", details)
	}
}

func TestCreateProgressCallback_PersistsRunningResponseBeforeDone(t *testing.T) {
	tmp := t.TempDir()
	db, err := database.NewDB(filepath.Join(tmp, "test.sqlite"), zap.NewNop())
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	conv, err := db.CreateConversation("refresh-running", database.ConversationCreateMeta{})
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	asst, err := db.AddMessage(conv.ID, "assistant", "处理中...", nil)
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	h := &AgentHandler{logger: zap.NewNop(), db: db}
	cb := h.createProgressCallback(context.Background(), nil, conv.ID, asst.ID, nil)
	meta := map[string]interface{}{
		"streamId":      "response-refresh-1",
		"agentName":     "cyberstrike-eino-single",
		"orchestration": "single",
	}
	cb("response_start", "", meta)
	cb("response_delta", "刷新前已生成的第一部分", openai.WithSSEAccumulated(meta, "刷新前已生成的第一部分"))

	details, err := db.GetProcessDetails(asst.ID)
	if err != nil {
		t.Fatalf("GetProcessDetails: %v", err)
	}
	if len(details) != 1 || details[0].EventType != "planning" || details[0].Message != "刷新前已生成的第一部分" {
		t.Fatalf("expected one running planning snapshot, got %+v", details)
	}

	longer := "刷新前已生成的第一部分" + strings.Repeat("继续迭代", 300)
	cb("response_delta", "继续迭代", openai.WithSSEAccumulated(meta, longer))
	details, err = db.GetProcessDetails(asst.ID)
	if err != nil {
		t.Fatalf("GetProcessDetails after update: %v", err)
	}
	if len(details) != 1 || details[0].Message != longer {
		t.Fatalf("running snapshot should update in-place, rows=%d len=%d", len(details), len(details[0].Message))
	}
}

// TestCreateProgressCallback_FlushesReasoningOnDone 流式推理聚合须在 done/response 时落库，刷新后可回放。
func TestCreateProgressCallback_FlushesReasoningOnDone(t *testing.T) {
	tmp := t.TempDir()
	db, err := database.NewDB(filepath.Join(tmp, "test.sqlite"), zap.NewNop())
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })
	conv, err := db.CreateConversation("test", database.ConversationCreateMeta{})
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	asst, err := db.AddMessage(conv.ID, "assistant", "处理中...", nil)
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	h := &AgentHandler{logger: zap.NewNop(), db: db}
	cb := h.createProgressCallback(context.Background(), nil, conv.ID, asst.ID, nil)

	streamID := "eino-reasoning-test-1"
	cb("reasoning_chain_stream_start", " ", map[string]interface{}{
		"streamId": streamID,
		"source":   "eino",
	})
	cb("reasoning_chain_stream_delta", "step one", openai.WithSSEAccumulated(map[string]interface{}{
		"streamId": streamID,
	}, "step one"))
	cb("done", "", map[string]interface{}{"conversationId": conv.ID})

	details, err := db.GetProcessDetails(asst.ID)
	if err != nil {
		t.Fatalf("GetProcessDetails: %v", err)
	}
	found := false
	for _, d := range details {
		if d.EventType == "reasoning_chain" && d.Message == "step one" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected reasoning_chain persisted on done, got %+v", details)
	}
}

func TestEnrichProgressEventData(t *testing.T) {
	t.Run("fills ids", func(t *testing.T) {
		out := enrichProgressEventData(map[string]interface{}{"source": "eino"}, "conv-1", "msg-1")
		m, ok := out.(map[string]interface{})
		if !ok {
			t.Fatalf("expected map, got %T", out)
		}
		if m["conversationId"] != "conv-1" || m["messageId"] != "msg-1" {
			t.Fatalf("unexpected enrichment: %+v", m)
		}
	})
	t.Run("preserves existing ids", func(t *testing.T) {
		out := enrichProgressEventData(map[string]interface{}{
			"conversationId": "keep-conv",
			"messageId":      "keep-msg",
		}, "conv-1", "msg-1")
		m := out.(map[string]interface{})
		if m["conversationId"] != "keep-conv" || m["messageId"] != "keep-msg" {
			t.Fatalf("should not overwrite existing ids: %+v", m)
		}
	})
}
