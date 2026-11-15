package handler

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/mcp"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func TestProcessDetailsPageIncludesTerminalToolStatusAcrossPageBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := database.NewDB(filepath.Join(t.TempDir(), "process-details-page.db"), zap.NewNop())
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	conversation, err := db.CreateConversation("page boundary", database.ConversationCreateMeta{})
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	message, err := db.AddMessage(conversation.ID, "assistant", "done", nil)
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}
	for i := 1; i <= 4; i++ {
		id := fmt.Sprintf("call-%d", i)
		if err := db.AddProcessDetail(message.ID, conversation.ID, "tool_call", "call", map[string]interface{}{
			"toolName": "http-framework-test", "toolCallId": id, "index": i, "total": 4,
		}); err != nil {
			t.Fatalf("AddProcessDetail(tool_call): %v", err)
		}
	}
	for i := 1; i <= 4; i++ {
		id := fmt.Sprintf("call-%d", i)
		if err := db.AddProcessDetail(message.ID, conversation.ID, "tool_result", "result", map[string]interface{}{
			"toolName": "http-framework-test", "toolCallId": id, "success": true,
		}); err != nil {
			t.Fatalf("AddProcessDetail(tool_result): %v", err)
		}
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/messages/"+message.ID+"/process-details?limit=6&offset=0", nil)
	c.Params = gin.Params{{Key: "id", Value: message.ID}}
	NewConversationHandler(db, zap.NewNop()).GetMessageProcessDetails(c)
	if w.Code != 200 {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	var response struct {
		HasMore        bool                                   `json:"hasMore"`
		ProcessDetails []map[string]interface{}               `json:"processDetails"`
		ToolExecutions []database.ProcessDetailsToolExecution `json:"toolExecutions"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !response.HasMore || len(response.ProcessDetails) != 6 {
		t.Fatalf("page hasMore=%v details=%d, want true/6", response.HasMore, len(response.ProcessDetails))
	}
	if len(response.ToolExecutions) != 4 {
		t.Fatalf("tool executions = %d, want 4", len(response.ToolExecutions))
	}
	for i, execution := range response.ToolExecutions {
		if execution.Status != "completed" {
			t.Fatalf("execution %d status = %q, want completed", i, execution.Status)
		}
	}
}

func TestProcessDetailsPageUsesPersistedExecutionStatusAfterBackgroundCancel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := database.NewDB(filepath.Join(t.TempDir(), "process-details-cancelled.db"), zap.NewNop())
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	conversation, err := db.CreateConversation("cancelled background", database.ConversationCreateMeta{})
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	message, err := db.AddMessage(conversation.ID, "assistant", "done", nil)
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}
	execID := "exec-cancelled-after-background"
	if err := db.AddProcessDetail(message.ID, conversation.ID, "tool_call", "call", map[string]interface{}{
		"toolName": "exec", "toolCallId": "call-cancelled", "index": 1, "total": 1,
	}); err != nil {
		t.Fatalf("AddProcessDetail(tool_call): %v", err)
	}
	if err := db.AddProcessDetail(message.ID, conversation.ID, "tool_result", "background", map[string]interface{}{
		"toolName": "exec", "toolCallId": "call-cancelled", "executionId": execID, "status": "background_running", "success": true,
	}); err != nil {
		t.Fatalf("AddProcessDetail(tool_result): %v", err)
	}
	now := time.Now()
	if err := db.SaveToolExecution(&mcp.ToolExecution{
		ID:        execID,
		ToolName:  "exec",
		Status:    mcp.ToolExecutionStatusCancelled,
		StartTime: now,
		EndTime:   &now,
	}); err != nil {
		t.Fatalf("SaveToolExecution: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/messages/"+message.ID+"/process-details?limit=10&offset=0", nil)
	c.Params = gin.Params{{Key: "id", Value: message.ID}}
	NewConversationHandler(db, zap.NewNop()).GetMessageProcessDetails(c)
	if w.Code != 200 {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	var response struct {
		ToolExecutions []database.ProcessDetailsToolExecution `json:"toolExecutions"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.ToolExecutions) != 1 {
		t.Fatalf("tool executions = %d, want 1", len(response.ToolExecutions))
	}
	if got := response.ToolExecutions[0].Status; got != mcp.ToolExecutionStatusCancelled {
		t.Fatalf("tool execution status = %q, want cancelled", got)
	}
}

func TestProcessDetailsFullBackfillsEmptyToolCallArgumentsFromExecution(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := database.NewDB(filepath.Join(t.TempDir(), "process-details-args.db"), zap.NewNop())
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	conversation, err := db.CreateConversation("empty args", database.ConversationCreateMeta{})
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	message, err := db.AddMessage(conversation.ID, "assistant", "done", nil)
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}
	if err := db.AddProcessDetail(message.ID, conversation.ID, "tool_call", "calling exec", map[string]interface{}{
		"toolName": "exec", "toolCallId": "call-empty", "arguments": "", "argumentsObj": nil,
	}); err != nil {
		t.Fatalf("AddProcessDetail(tool_call): %v", err)
	}
	if err := db.SaveToolExecution(&mcp.ToolExecution{
		ID:             "exec-whoami",
		ToolName:       "exec",
		Arguments:      map[string]interface{}{"command": "whoami"},
		Status:         "completed",
		StartTime:      time.Now(),
		ConversationID: conversation.ID,
	}); err != nil {
		t.Fatalf("SaveToolExecution: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/messages/"+message.ID+"/process-details?full=1", nil)
	c.Params = gin.Params{{Key: "id", Value: message.ID}}
	NewConversationHandler(db, zap.NewNop()).GetMessageProcessDetails(c)
	if w.Code != 200 {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	var response struct {
		ProcessDetails []map[string]interface{} `json:"processDetails"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.ProcessDetails) != 1 {
		t.Fatalf("process details = %d, want 1", len(response.ProcessDetails))
	}
	data, ok := response.ProcessDetails[0]["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("data = %#v", response.ProcessDetails[0]["data"])
	}
	args, ok := data["argumentsObj"].(map[string]interface{})
	if !ok {
		t.Fatalf("argumentsObj = %#v", data["argumentsObj"])
	}
	if args["command"] != "whoami" {
		t.Fatalf("command = %#v, want whoami", args["command"])
	}
	if data["executionId"] != "exec-whoami" {
		t.Fatalf("executionId = %#v, want exec-whoami", data["executionId"])
	}
}

func TestProcessDetailsPageBackfillsAgentFilesystemArgumentsFromPrefixedExecution(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := database.NewDB(filepath.Join(t.TempDir(), "process-details-eino-fs-args.db"), zap.NewNop())
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	conversation, err := db.CreateConversation("eino fs args", database.ConversationCreateMeta{})
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	message, err := db.AddMessage(conversation.ID, "assistant", "done", nil)
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}
	if err := db.AddProcessDetail(message.ID, conversation.ID, "tool_call", "calling read_file", map[string]interface{}{
		"toolName": "read_file", "toolCallId": "call-read", "arguments": "", "argumentsObj": nil,
	}); err != nil {
		t.Fatalf("AddProcessDetail(tool_call): %v", err)
	}
	if err := db.SaveToolExecution(&mcp.ToolExecution{
		ID:             "exec-read",
		ToolName:       "eino_fs::read_file",
		Arguments:      map[string]interface{}{"file_path": "/tmp/requirements.txt", "limit": float64(2000)},
		Status:         "completed",
		StartTime:      time.Now(),
		ConversationID: conversation.ID,
	}); err != nil {
		t.Fatalf("SaveToolExecution: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/messages/"+message.ID+"/process-details?limit=50&offset=0", nil)
	c.Params = gin.Params{{Key: "id", Value: message.ID}}
	NewConversationHandler(db, zap.NewNop()).GetMessageProcessDetails(c)
	if w.Code != 200 {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	var response struct {
		ProcessDetails []map[string]interface{} `json:"processDetails"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.ProcessDetails) != 1 {
		t.Fatalf("process details = %d, want 1", len(response.ProcessDetails))
	}
	data, ok := response.ProcessDetails[0]["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("data = %#v", response.ProcessDetails[0]["data"])
	}
	args, ok := data["argumentsObj"].(map[string]interface{})
	if !ok {
		t.Fatalf("argumentsObj = %#v", data["argumentsObj"])
	}
	if args["file_path"] != "/tmp/requirements.txt" {
		t.Fatalf("file_path = %#v, want /tmp/requirements.txt", args["file_path"])
	}
	if data["arguments"] == nil {
		t.Fatalf("arguments should be preserved in summarized page data: %#v", data)
	}
}
