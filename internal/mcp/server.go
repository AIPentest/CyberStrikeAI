package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"cyberstrike-ai/internal/authctx"
	"cyberstrike-ai/internal/mcp/builtin"
	"cyberstrike-ai/internal/toolguard"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// MonitorStorage 监控数据存储接口
type MonitorStorage interface {
	SaveToolExecution(exec *ToolExecution) error
	UpdateToolExecutionResult(id string, result *ToolResult) error
	LoadToolExecutions() ([]*ToolExecution, error)
	GetToolExecution(id string) (*ToolExecution, error)
	SaveToolStats(toolName string, stats *ToolStats) error
	LoadToolStats() (map[string]*ToolStats, error)
	UpdateToolStats(toolName string, totalCalls, successCalls, failedCalls int, lastCallTime *time.Time) error
}

// Server MCP服务器
type Server struct {
	tools                 map[string]ToolHandler
	toolDefs              map[string]Tool // 工具定义
	executions            map[string]*ToolExecution
	stats                 map[string]*ToolStats
	prompts               map[string]*Prompt   // 提示词模板
	resources             map[string]*Resource // 资源
	storage               MonitorStorage       // 可选的持久化存储
	mu                    sync.RWMutex
	logger                *zap.Logger
	maxExecutionsInMemory int // 内存中最大执行记录数
	sseClients            map[string]*sseClient
	runningCancels        map[string]context.CancelFunc
	runningCancelsMu      sync.Mutex
	abortUserNotes        map[string]string // 监控页终止时附带的用户说明，与 executionID 对应
	// httpToolTimeoutMinutes 同步 agent.tool_timeout_minutes，用于 POST /api/mcp 的 tools/call（不经 Agent 包装的路径）。
	// nil 表示未配置，沿用默认 30 分钟；指向 0 表示不限制；>0 为分钟数。
	httpToolTimeoutMinutes *int
	httpToolTimeoutMu      sync.RWMutex
	toolAuthorizer         func(context.Context, string, map[string]interface{}) error
	toolGuard              *toolguard.Manager
	executionService       *ExecutionService
	toolWaitTimeout        time.Duration
	toolResultMaxBytes     int
	spillRootDir           string
}

const defaultPartialOutputMaxBytes = 64 * 1024

// SetToolAuthorizer installs the common policy decision point for every
// user-attributed tool call, whether it originates from HTTP or an Agent.
func (s *Server) SetToolAuthorizer(authorizer func(context.Context, string, map[string]interface{}) error) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.toolAuthorizer = authorizer
	s.mu.Unlock()
}

// SetToolGuard installs the runtime safety rules shared by HTTP and internal calls.
func (s *Server) SetToolGuard(guard *toolguard.Manager) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.toolGuard = guard
	s.mu.Unlock()
}

func (s *Server) checkToolGuard(toolName string, args map[string]interface{}) *ToolResult {
	s.mu.RLock()
	guard := s.toolGuard
	s.mu.RUnlock()
	return toolGuardBlockedResult(guard, toolName, args)
}

type sseClient struct {
	id   string
	send chan []byte
}

// ToolHandler 工具处理函数
type ToolHandler func(ctx context.Context, args map[string]interface{}) (*ToolResult, error)

func executionStatusAndMessage(err error) (status string, errMsg string) {
	if errors.Is(err, context.Canceled) {
		return "cancelled", "已手动终止（MCP 监控）"
	}
	return "failed", err.Error()
}

// NewServer 创建新的MCP服务器
func NewServer(logger *zap.Logger) *Server {
	return NewServerWithStorage(logger, nil)
}

// NewServerWithStorage 创建新的MCP服务器（带持久化存储）
func NewServerWithStorage(logger *zap.Logger, storage MonitorStorage) *Server {
	s := &Server{
		tools:                 make(map[string]ToolHandler),
		toolDefs:              make(map[string]Tool),
		executions:            make(map[string]*ToolExecution),
		stats:                 make(map[string]*ToolStats),
		prompts:               make(map[string]*Prompt),
		resources:             make(map[string]*Resource),
		storage:               storage,
		logger:                logger,
		maxExecutionsInMemory: 1000, // 默认最多在内存中保留1000条执行记录
		sseClients:            make(map[string]*sseClient),
		runningCancels:        make(map[string]context.CancelFunc),
		abortUserNotes:        make(map[string]string),
		toolWaitTimeout:       60 * time.Second,
		toolResultMaxBytes:    DefaultToolResultMaxBytes,
	}
	s.executionService = NewExecutionService(storage, logger)

	// 初始化默认提示词和资源
	s.initDefaultPrompts()
	s.initDefaultResources()

	return s
}

func (s *Server) ConfigureToolResultMaxBytes(maxBytes int) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.toolResultMaxBytes = maxBytes
	s.mu.Unlock()
	if s.executionService != nil {
		s.executionService.ConfigureToolResultMaxBytes(maxBytes)
	}
}

// ConfigureToolResultSpillRoot sets the local directory root used when oversized
// tool results are spilled (aligned with reduction_root_dir; empty → tmp/reduction).
func (s *Server) ConfigureToolResultSpillRoot(rootDir string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.spillRootDir = strings.TrimSpace(rootDir)
	s.mu.Unlock()
	if s.executionService != nil {
		s.executionService.ConfigureToolResultSpillRoot(rootDir)
	}
}

// ConfigureHTTPToolCallTimeoutFromAgentMinutes 将 agent.tool_timeout_minutes 同步到经 HTTP POST /api/mcp 触发的 tools/call。
// minutes<=0 表示不设置硬性截止时间（与配置「0 不限制」一致）；minutes>0 为该次调用的最长等待时间。
// 未调用前对 tools/call 使用默认 30 分钟（与历史硬编码一致）。
func (s *Server) ConfigureHTTPToolCallTimeoutFromAgentMinutes(minutes int) {
	if s == nil {
		return
	}
	v := minutes
	if v < 0 {
		v = 0
	}
	s.httpToolTimeoutMu.Lock()
	defer s.httpToolTimeoutMu.Unlock()
	s.httpToolTimeoutMinutes = &v
}

func (s *Server) ConfigureToolWaitTimeoutSeconds(seconds int) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if seconds <= 0 {
		s.toolWaitTimeout = 0
		return
	}
	s.toolWaitTimeout = time.Duration(seconds) * time.Second
}

func (s *Server) effectiveHTTPToolCallDeadline(parent context.Context) (context.Context, context.CancelFunc) {
	const defaultDur = 30 * time.Minute
	if parent == nil {
		parent = context.Background()
	}
	if s == nil {
		return context.WithTimeout(parent, defaultDur)
	}
	s.httpToolTimeoutMu.RLock()
	mPtr := s.httpToolTimeoutMinutes
	s.httpToolTimeoutMu.RUnlock()
	if mPtr == nil {
		return context.WithTimeout(parent, defaultDur)
	}
	if *mPtr <= 0 {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, time.Duration(*mPtr)*time.Minute)
}

// RegisterTool 注册工具
func (s *Server) RegisterTool(tool Tool, handler ToolHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools[tool.Name] = handler
	s.toolDefs[tool.Name] = tool

	// 自动为工具创建资源文档
	resourceURI := fmt.Sprintf("tool://%s", tool.Name)
	s.resources[resourceURI] = &Resource{
		URI:         resourceURI,
		Name:        fmt.Sprintf("%s工具文档", tool.Name),
		Description: tool.Description,
		MimeType:    "text/plain",
	}
}

// ClearTools 清空所有工具（用于重新加载配置）
func (s *Server) ClearTools() {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 清空工具和工具定义
	s.tools = make(map[string]ToolHandler)
	s.toolDefs = make(map[string]Tool)

	// 清空工具相关的资源（保留其他资源）
	newResources := make(map[string]*Resource)
	for uri, resource := range s.resources {
		// 保留非工具资源
		if !strings.HasPrefix(uri, "tool://") {
			newResources[uri] = resource
		}
	}
	s.resources = newResources
}

// HandleHTTP 处理HTTP请求
func (s *Server) HandleHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		s.handleSSE(w, r)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 官方 MCP SSE 规范：带 sessionid 的 POST 表示消息发往该 SSE 会话，响应通过 SSE 流返回
	if sessionID := r.URL.Query().Get("sessionid"); sessionID != "" {
		s.serveSSESessionMessage(w, r, sessionID)
		return
	}

	// 简单 POST：请求体为 JSON-RPC，响应在 body 中返回
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.sendError(w, nil, -32700, "Parse error", err.Error())
		return
	}

	var msg Message
	if err := json.Unmarshal(body, &msg); err != nil {
		s.sendError(w, nil, -32700, "Parse error", err.Error())
		return
	}

	response := s.handleMessage(r.Context(), &msg)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// serveSSESessionMessage 处理发往 SSE 会话的 POST：读取 JSON-RPC 请求，处理后将响应通过该会话的 SSE 流推送
func (s *Server) serveSSESessionMessage(w http.ResponseWriter, r *http.Request, sessionID string) {
	s.mu.RLock()
	client, exists := s.sseClients[sessionID]
	s.mu.RUnlock()
	if !exists || client == nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	var msg Message
	if err := json.Unmarshal(body, &msg); err != nil {
		http.Error(w, "failed to parse body", http.StatusBadRequest)
		return
	}

	response := s.handleMessage(r.Context(), &msg)
	if response == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	respBytes, err := json.Marshal(response)
	if err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
		return
	}

	select {
	case client.send <- respBytes:
		w.WriteHeader(http.StatusAccepted)
	default:
		http.Error(w, "session send buffer full", http.StatusServiceUnavailable)
	}
}

// handleSSE 处理 SSE 连接，兼容官方 MCP 2024-11-05 SSE 规范：
// 1. 首个事件必须为 event: endpoint，data 为客户端 POST 消息的 URL（含 sessionid）
// 2. 后续事件为 event: message，data 为 JSON-RPC 响应
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	sessionID := uuid.New().String()
	client := &sseClient{
		id:   sessionID,
		send: make(chan []byte, 32),
	}

	s.addSSEClient(client)
	defer s.removeSSEClient(client.id)

	// 官方规范：首个事件为 endpoint，data 为消息端点 URL（客户端将向该 URL POST 请求）
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	} else if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	}
	if r.URL.Scheme != "" {
		scheme = r.URL.Scheme
	}
	endpointURL := fmt.Sprintf("%s://%s%s?sessionid=%s", scheme, r.Host, r.URL.Path, sessionID)
	fmt.Fprintf(w, "event: endpoint\ndata: %s\n\n", endpointURL)
	flusher.Flush()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case msg, ok := <-client.send:
			if !ok {
				return
			}
			fmt.Fprintf(w, "event: message\ndata: %s\n\n", msg)
			flusher.Flush()
		case <-ticker.C:
			fmt.Fprintf(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

// addSSEClient 注册SSE客户端
func (s *Server) addSSEClient(client *sseClient) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sseClients[client.id] = client
}

// removeSSEClient 移除SSE客户端
func (s *Server) removeSSEClient(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if client, exists := s.sseClients[id]; exists {
		close(client.send)
		delete(s.sseClients, id)
	}
}

// handleMessage 处理MCP消息
func (s *Server) handleMessage(ctx context.Context, msg *Message) *Message {
	// 检查是否是通知（notification）- 通知没有id字段，不需要响应
	isNotification := msg.ID.Value() == nil || msg.ID.String() == ""

	// 如果不是通知且ID为空，生成新的UUID
	if !isNotification && msg.ID.String() == "" {
		msg.ID = MessageID{value: uuid.New().String()}
	}

	switch msg.Method {
	case "initialize":
		return s.handleInitialize(msg)
	case "tools/list":
		return s.handleListTools(msg)
	case "tools/call":
		return s.handleCallTool(ctx, msg)
	case "prompts/list":
		return s.handleListPrompts(msg)
	case "prompts/get":
		return s.handleGetPrompt(msg)
	case "resources/list":
		return s.handleListResources(msg)
	case "resources/read":
		return s.handleReadResource(msg)
	case "sampling/request":
		return s.handleSamplingRequest(msg)
	case "notifications/initialized":
		// 通知类型，不需要响应
		s.logger.Debug("收到 initialized 通知")
		return nil
	case "":
		// 空方法名，可能是通知，不返回错误
		if isNotification {
			s.logger.Debug("收到无方法名的通知消息")
			return nil
		}
		fallthrough
	default:
		// 如果是通知，不返回错误响应
		if isNotification {
			s.logger.Debug("收到未知通知", zap.String("method", msg.Method))
			return nil
		}
		// 对于请求，返回方法未找到错误
		return &Message{
			ID:      msg.ID,
			Type:    MessageTypeError,
			Version: "2.0",
			Error:   &Error{Code: -32601, Message: "Method not found"},
		}
	}
}

// handleInitialize 处理初始化请求
func (s *Server) handleInitialize(msg *Message) *Message {
	var req InitializeRequest
	if err := json.Unmarshal(msg.Params, &req); err != nil {
		return &Message{
			ID:      msg.ID,
			Type:    MessageTypeError,
			Version: "2.0",
			Error:   &Error{Code: -32602, Message: "Invalid params"},
		}
	}

	response := InitializeResponse{
		ProtocolVersion: ProtocolVersion,
		Capabilities: ServerCapabilities{
			Tools: map[string]interface{}{
				"listChanged": true,
			},
			Prompts: map[string]interface{}{
				"listChanged": true,
			},
			Resources: map[string]interface{}{
				"subscribe":   true,
				"listChanged": true,
			},
			Sampling: map[string]interface{}{},
		},
		ServerInfo: ServerInfo{
			Name:    "CyberStrikeAI",
			Version: "1.0.0",
		},
	}

	result, _ := json.Marshal(response)
	return &Message{
		ID:      msg.ID,
		Type:    MessageTypeResponse,
		Version: "2.0",
		Result:  result,
	}
}

// handleListTools 处理列出工具请求
func (s *Server) handleListTools(msg *Message) *Message {
	s.mu.RLock()
	tools := make([]Tool, 0, len(s.toolDefs))
	for _, tool := range s.toolDefs {
		tools = append(tools, tool)
	}
	s.mu.RUnlock()
	s.logger.Debug("tools/list 请求", zap.Int("返回工具数", len(tools)))

	response := ListToolsResponse{Tools: tools}
	result, _ := json.Marshal(response)
	return &Message{
		ID:      msg.ID,
		Type:    MessageTypeResponse,
		Version: "2.0",
		Result:  result,
	}
}

// handleCallTool 处理工具调用请求
func (s *Server) handleCallTool(requestCtx context.Context, msg *Message) *Message {
	var req CallToolRequest
	if err := json.Unmarshal(msg.Params, &req); err != nil {
		return &Message{
			ID:      msg.ID,
			Type:    MessageTypeError,
			Version: "2.0",
			Error:   &Error{Code: -32602, Message: "Invalid params"},
		}
	}
	_, authenticated := authctx.PrincipalFromContext(requestCtx)
	s.mu.RLock()
	authorizer := s.toolAuthorizer
	s.mu.RUnlock()
	if authorizer != nil {
		if err := authorizer(requestCtx, req.Name, req.Arguments); err != nil {
			return &Message{ID: msg.ID, Type: MessageTypeError, Version: "2.0", Error: &Error{Code: -32003, Message: "Forbidden", Data: err.Error()}}
		}
	} else if authenticated {
		return &Message{ID: msg.ID, Type: MessageTypeError, Version: "2.0", Error: &Error{Code: -32003, Message: "Tool authorization policy is not configured"}}
	}

	executionID := uuid.New().String()
	execution := &ToolExecution{
		ID:        executionID,
		ToolName:  req.Name,
		Arguments: req.Arguments,
		Status:    "running",
		StartTime: time.Now(),
	}
	if principal, ok := authctx.PrincipalFromContext(requestCtx); ok {
		execution.OwnerUserID = principal.UserID
	}
	execution.ConversationID = MCPConversationIDFromContext(requestCtx)

	s.mu.Lock()
	s.executions[executionID] = execution
	// 如果内存中的执行记录超过限制，清理最旧的记录
	s.cleanupOldExecutions()
	s.mu.Unlock()

	if s.storage != nil {
		if err := s.storage.SaveToolExecution(execution); err != nil {
			s.logger.Warn("保存执行记录到数据库失败", zap.Error(err))
		}
	}

	s.mu.RLock()
	handler, exists := s.tools[req.Name]
	s.mu.RUnlock()

	if !exists {
		execution.Status = "failed"
		execution.Error = "Tool not found"
		now := time.Now()
		execution.EndTime = &now
		execution.Duration = now.Sub(execution.StartTime)

		if s.storage != nil {
			if err := s.storage.SaveToolExecution(execution); err != nil {
				s.logger.Warn("保存执行记录到数据库失败", zap.Error(err))
			}
			s.mu.Lock()
			delete(s.executions, executionID)
			s.mu.Unlock()
		}

		s.updateStats(req.Name, ToolExecutionStatusFailed)

		return &Message{
			ID:      msg.ID,
			Type:    MessageTypeError,
			Version: "2.0",
			Error:   &Error{Code: -32601, Message: "Tool not found"},
		}
	}

	baseCtx, timeoutCancel := s.effectiveHTTPToolCallDeadline(requestCtx)
	defer timeoutCancel()
	execCtx, runCancel := context.WithCancel(baseCtx)
	s.registerRunningCancel(executionID, runCancel)
	defer func() {
		runCancel()
		s.unregisterRunningCancel(executionID)
	}()

	s.logger.Info("开始执行工具",
		zap.String("toolName", req.Name),
		zap.Any("arguments", req.Arguments),
	)

	result := s.checkToolGuard(req.Name, req.Arguments)
	var err error
	if result == nil {
		result, err = handler(execCtx, req.Arguments)
	}
	cancelledWithUserNote := s.applyAbortUserNoteToCancelledToolResult(executionID, &result, &err)
	now := time.Now()
	var finalResult *ToolResult

	s.mu.Lock()
	execution.EndTime = &now
	execution.Duration = now.Sub(execution.StartTime)

	if err != nil {
		st, msg := executionStatusAndMessage(err)
		execution.Status = st
		execution.Error = msg
	} else if result != nil && result.Blocked {
		execution.Status = ToolExecutionStatusBlocked
		execution.Error = firstToolResultText(result, toolGuardBlockedPrefix)
		execution.Result = result
	} else if result != nil && result.IsError {
		if cancelledWithUserNote {
			execution.Status = "cancelled"
			execution.Error = ""
			execution.Result = result
		} else {
			execution.Status = "failed"
			if len(result.Content) > 0 {
				execution.Error = result.Content[0].Text
			} else {
				execution.Error = "工具执行返回错误结果"
			}
			execution.Result = result
		}
	} else {
		execution.Status = "completed"
		if result == nil {
			result = &ToolResult{
				Content: []Content{
					{Type: "text", Text: "工具执行完成，但未返回结果"},
				},
			}
		}
		execution.Result = result
	}

	finalResult = execution.Result
	s.mu.Unlock()

	if s.storage != nil {
		if err := s.storage.SaveToolExecution(execution); err != nil {
			s.logger.Warn("保存执行记录到数据库失败", zap.Error(err))
		}
	}

	s.updateStats(req.Name, execution.Status)

	if s.storage != nil {
		s.mu.Lock()
		delete(s.executions, executionID)
		s.mu.Unlock()
	}

	if err != nil {
		s.logger.Error("工具执行失败",
			zap.String("toolName", req.Name),
			zap.Error(err),
		)

		errText := fmt.Sprintf("工具执行失败: %v", err)
		if errors.Is(err, context.Canceled) {
			errText = "工具执行已手动终止（MCP 监控）。后续编排步骤可继续。"
		}
		errorResult, _ := json.Marshal(CallToolResponse{
			Content: []Content{
				{Type: "text", Text: errText},
			},
			IsError: true,
		})
		return &Message{
			ID:      msg.ID,
			Type:    MessageTypeResponse,
			Version: "2.0",
			Result:  errorResult,
		}
	}

	if finalResult != nil && finalResult.IsError {
		s.logger.Warn("工具执行返回错误结果",
			zap.String("toolName", req.Name),
		)

		errorResult, _ := json.Marshal(CallToolResponse{
			Content: finalResult.Content,
			IsError: true,
			Blocked: finalResult.Blocked,
			Meta:    toolResultProtocolMeta(finalResult),
		})
		return &Message{
			ID:      msg.ID,
			Type:    MessageTypeResponse,
			Version: "2.0",
			Result:  errorResult,
		}
	}

	if finalResult == nil {
		finalResult = &ToolResult{
			Content: []Content{
				{Type: "text", Text: "工具执行完成，但未返回结果"},
			},
		}
	}

	resultJSON, _ := json.Marshal(CallToolResponse{
		Content: finalResult.Content,
		IsError: false,
	})

	s.logger.Info("工具执行完成",
		zap.String("toolName", req.Name),
		zap.Bool("isError", finalResult.IsError),
	)

	return &Message{
		ID:      msg.ID,
		Type:    MessageTypeResponse,
		Version: "2.0",
		Result:  resultJSON,
	}
}

// updateStats 更新统计信息
func (s *Server) updateStats(toolName string, status string) {
	now := time.Now()
	if s.storage != nil {
		totalCalls := 1
		successCalls := 0
		failedCalls := 0
		if executionStatusCountsAsFailed(status) {
			failedCalls = 1
		} else if status == ToolExecutionStatusCompleted {
			successCalls = 1
		}
		if err := s.storage.UpdateToolStats(toolName, totalCalls, successCalls, failedCalls, &now); err != nil {
			s.logger.Warn("保存统计信息到数据库失败", zap.Error(err))
		}
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stats[toolName] == nil {
		s.stats[toolName] = &ToolStats{
			ToolName: toolName,
		}
	}

	stats := s.stats[toolName]
	stats.TotalCalls++
	stats.LastCallTime = &now

	if executionStatusCountsAsFailed(status) {
		stats.FailedCalls++
	} else if status == ToolExecutionStatusCompleted {
		stats.SuccessCalls++
	} else if status == ToolExecutionStatusBlocked {
		stats.BlockedCalls++
	}
}

// GetExecution 获取执行记录（先从内存查找，再从数据库查找）
func (s *Server) GetExecution(id string) (*ToolExecution, bool) {
	if s.executionService != nil {
		if snap, err := s.executionService.Get(id); err == nil && snap != nil && snap.Execution != nil {
			return snap.Execution, true
		}
	}
	s.mu.RLock()
	exec, exists := s.executions[id]
	s.mu.RUnlock()

	if exists {
		return exec, true
	}

	if s.storage != nil {
		exec, err := s.storage.GetToolExecution(id)
		if err == nil {
			return exec, true
		}
	}

	return nil, false
}

// loadHistoricalData 从数据库加载历史数据
func (s *Server) loadHistoricalData() {
	if s.storage == nil {
		return
	}

	// 加载历史执行记录（最近1000条）
	executions, err := s.storage.LoadToolExecutions()
	if err != nil {
		s.logger.Warn("加载历史执行记录失败", zap.Error(err))
	} else {
		s.mu.Lock()
		for _, exec := range executions {
			// 只加载最近 maxExecutionsInMemory 条，避免内存占用过大
			if len(s.executions) < s.maxExecutionsInMemory {
				s.executions[exec.ID] = exec
			} else {
				break
			}
		}
		s.mu.Unlock()
		s.logger.Info("加载历史执行记录", zap.Int("count", len(executions)))
	}

	// 加载历史统计信息
	stats, err := s.storage.LoadToolStats()
	if err != nil {
		s.logger.Warn("加载历史统计信息失败", zap.Error(err))
	} else {
		s.mu.Lock()
		for k, v := range stats {
			s.stats[k] = v
		}
		s.mu.Unlock()
		s.logger.Info("加载历史统计信息", zap.Int("count", len(stats)))
	}
}

// GetAllExecutions 获取所有执行记录（合并内存和数据库）
func (s *Server) GetAllExecutions() []*ToolExecution {
	if s.storage != nil {
		dbExecutions, err := s.storage.LoadToolExecutions()
		if err == nil {
			execMap := make(map[string]*ToolExecution)
			for _, exec := range dbExecutions {
				if _, exists := execMap[exec.ID]; !exists {
					execMap[exec.ID] = exec
				}
			}

			s.mu.RLock()
			for id, exec := range s.executions {
				if _, exists := execMap[id]; !exists {
					execMap[id] = exec
				}
			}
			s.mu.RUnlock()

			result := make([]*ToolExecution, 0, len(execMap))
			for _, exec := range execMap {
				result = append(result, exec)
			}
			return result
		} else {
			s.logger.Warn("从数据库加载执行记录失败", zap.Error(err))
		}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	memExecutions := make([]*ToolExecution, 0, len(s.executions))
	for _, exec := range s.executions {
		memExecutions = append(memExecutions, exec)
	}
	return memExecutions
}

// GetStats 获取统计信息（合并内存和数据库）
func (s *Server) GetStats() map[string]*ToolStats {
	if s.storage != nil {
		dbStats, err := s.storage.LoadToolStats()
		if err == nil {
			return dbStats
		}
		s.logger.Warn("从数据库加载统计信息失败", zap.Error(err))
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	memStats := make(map[string]*ToolStats)
	for k, v := range s.stats {
		statCopy := *v
		memStats[k] = &statCopy
	}

	return memStats
}

// GetAllTools 获取所有已注册的工具（用于Agent动态获取工具列表）
func (s *Server) GetAllTools() []Tool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tools := make([]Tool, 0, len(s.toolDefs))
	for _, tool := range s.toolDefs {
		tools = append(tools, tool)
	}
	return tools
}

// CallTool 直接调用工具（用于内部调用）
func (s *Server) CallTool(ctx context.Context, toolName string, args map[string]interface{}) (*ToolResult, string, error) {
	if s.executionService == nil {
		s.executionService = NewExecutionService(s.storage, s.logger)
		s.executionService.ConfigureToolResultMaxBytes(s.toolResultMaxBytes)
		s.executionService.ConfigureToolResultSpillRoot(s.spillRootDir)
	}
	var ownerUserID string
	if principal, ok := authctx.PrincipalFromContext(ctx); ok {
		ownerUserID = principal.UserID
	}
	handle, err := s.executionService.Submit(ctx, ExecutionRequest{
		ToolName:       toolName,
		Arguments:      args,
		ConversationID: MCPConversationIDFromContext(ctx),
		OwnerUserID:    ownerUserID,
		Run: func(runCtx context.Context) (*ToolResult, error) {
			_, authenticated := authctx.PrincipalFromContext(runCtx)
			s.mu.RLock()
			authorizer := s.toolAuthorizer
			handler, exists := s.tools[toolName]
			s.mu.RUnlock()
			if authorizer != nil {
				if err := authorizer(runCtx, toolName, args); err != nil {
					return nil, fmt.Errorf("tool authorization denied: %w", err)
				}
			} else if authenticated {
				return nil, errors.New("tool authorization policy is not configured")
			}
			if !exists {
				return nil, fmt.Errorf("工具 %s 未找到", toolName)
			}
			if blocked := s.checkToolGuard(toolName, args); blocked != nil {
				return blocked, nil
			}
			return handler(runCtx, args)
		},
		OnDone: func(exec *ToolExecution) {
			if exec != nil {
				s.updateStats(toolName, exec.Status)
			}
		},
	})
	if err != nil {
		return nil, "", err
	}

	s.mu.RLock()
	waitTimeout := s.toolWaitTimeout
	s.mu.RUnlock()
	if isExecutionControlTool(toolName) {
		waitTimeout = 0
	}
	snapshot, waitErr := s.executionService.Wait(ctx, handle.ID, waitTimeout)
	if errors.Is(waitErr, ErrExecutionWaitTimeout) {
		return internalMCPWaitTimeoutResult(snapshot, waitTimeout), handle.ID, nil
	}
	if waitErr != nil {
		return nil, handle.ID, waitErr
	}
	if snapshot == nil || snapshot.Execution == nil {
		return &ToolResult{Content: []Content{{Type: "text", Text: "工具执行完成，但未返回执行快照"}}, IsError: true}, handle.ID, nil
	}
	if snapshot.Execution.Result != nil {
		return snapshot.Execution.Result, handle.ID, nil
	}
	if snapshot.Execution.Error != "" {
		return nil, handle.ID, errors.New(snapshot.Execution.Error)
	}
	return &ToolResult{Content: []Content{{Type: "text", Text: "工具执行完成，但未返回结果"}}, IsError: false}, handle.ID, nil
}

func internalMCPWaitTimeoutResult(snapshot *ExecutionSnapshot, waitTimeout time.Duration) *ToolResult {
	execID := ""
	status := ToolExecutionStatusRunning
	toolName := ""
	elapsed := time.Duration(0)
	if snapshot != nil && snapshot.Execution != nil {
		execID = snapshot.Execution.ID
		status = snapshot.Execution.Status
		toolName = snapshot.Execution.ToolName
		elapsed = time.Since(snapshot.Execution.StartTime).Round(time.Second)
	}
	waitText := "unbounded"
	if waitTimeout > 0 {
		waitText = waitTimeout.Round(time.Second).String()
	}
	msg := fmt.Sprintf(`工具已提交到后台执行，但本次等待已到达上限。

execution_id: %s
tool: %s
status: %s
wait_timeout: %s
elapsed: %s

你可以继续推理、改用其他工具，或调用 wait_tool_execution 继续等待该 execution_id；也可以调用 cancel_tool_execution 取消。`, execID, toolName, status, waitText, elapsed)
	return &ToolResult{Content: []Content{{Type: "text", Text: msg}}, IsError: true}
}

func isExecutionControlTool(toolName string) bool {
	switch strings.TrimSpace(toolName) {
	case builtin.ToolGetToolExecution, builtin.ToolWaitToolExecution, builtin.ToolCancelToolExecution:
		return true
	default:
		return false
	}
}

// BeginToolExecution 创建 running 状态的执行记录，供 Eino 等非 CallTool 路径在工具开始时落库。
func (s *Server) BeginToolExecution(ctx context.Context, toolName string, args map[string]interface{}) string {
	if s == nil {
		return ""
	}
	if args == nil {
		args = map[string]interface{}{}
	}
	executionID := uuid.New().String()
	execution := &ToolExecution{
		ID:        executionID,
		ToolName:  toolName,
		Arguments: args,
		Status:    "running",
		StartTime: time.Now(),
	}
	if principal, ok := authctx.PrincipalFromContext(ctx); ok {
		execution.OwnerUserID = principal.UserID
	}
	execution.ConversationID = MCPConversationIDFromContext(ctx)

	s.mu.Lock()
	s.executions[executionID] = execution
	s.cleanupOldExecutions()
	s.mu.Unlock()

	if s.storage != nil {
		if err := s.storage.SaveToolExecution(execution); err != nil {
			s.logger.Warn("保存执行记录到数据库失败", zap.Error(err))
		}
	}
	return executionID
}

// FinishToolExecution 完成先前 BeginToolExecution 创建的记录；executionID 为空时等同 RecordCompletedToolInvocation。
func (s *Server) FinishToolExecution(ctx context.Context, executionID, toolName string, args map[string]interface{}, resultText string, invokeErr error) string {
	if s == nil {
		return ""
	}
	if args == nil {
		args = map[string]interface{}{}
	}
	id := strings.TrimSpace(executionID)
	if id == "" {
		id = uuid.New().String()
	}

	now := time.Now()
	failed := invokeErr != nil
	var finalResult *ToolResult

	s.mu.Lock()
	maxBytes := s.toolResultMaxBytes
	spillRoot := s.spillRootDir
	exec, inMem := s.executions[id]
	if !inMem || exec == nil {
		exec = &ToolExecution{
			ID:        id,
			ToolName:  toolName,
			Arguments: args,
			StartTime: now,
		}
		s.executions[id] = exec
	} else if toolName != "" {
		exec.ToolName = toolName
	}
	if len(args) > 0 {
		exec.Arguments = args
	}
	if principal, ok := authctx.PrincipalFromContext(ctx); ok {
		exec.OwnerUserID = principal.UserID
	}
	if conversationID := MCPConversationIDFromContext(ctx); conversationID != "" {
		exec.ConversationID = conversationID
	}
	exec.EndTime = &now
	if exec.StartTime.IsZero() {
		exec.StartTime = now
	}
	exec.Duration = now.Sub(exec.StartTime)

	spill := ToolResultSpillConfig{
		RootDir:        spillRoot,
		ProjectID:      MCPProjectIDFromContext(ctx),
		ConversationID: exec.ConversationID,
		ExecutionID:    id,
	}
	if failed {
		st, msg := executionStatusAndMessage(invokeErr)
		exec.Status = st
		exec.Error = msg
		if strings.TrimSpace(resultText) != "" {
			finalResult = &ToolResult{Content: []Content{{Type: "text", Text: resultText}}}
			finalResult = NormalizeToolResultForStorageWithSpill(finalResult, maxBytes, spill)
			exec.Result = finalResult
		}
	} else {
		exec.Status = "completed"
		text := resultText
		if strings.TrimSpace(text) == "" {
			text = "（无输出）"
		}
		finalResult = &ToolResult{Content: []Content{{Type: "text", Text: text}}}
		finalResult = NormalizeToolResultForStorageWithSpill(finalResult, maxBytes, spill)
		exec.Result = finalResult
	}
	s.mu.Unlock()

	if s.storage != nil {
		if err := s.storage.SaveToolExecution(exec); err != nil {
			s.logger.Warn("保存执行记录到数据库失败", zap.Error(err))
		}
	}

	s.updateStats(exec.ToolName, exec.Status)

	if s.storage != nil {
		s.mu.Lock()
		delete(s.executions, id)
		s.mu.Unlock()
	}
	return id
}

// AppendToolExecutionPartialOutput records a bounded tail preview for a running local execution.
// The final Result remains authoritative and is written only when the tool finishes.
func (s *Server) AppendToolExecutionPartialOutput(executionID, chunk string) {
	if s == nil || strings.TrimSpace(executionID) == "" || chunk == "" {
		return
	}
	id := strings.TrimSpace(executionID)
	if s.executionService != nil && s.executionService.AppendPartialOutput(id, chunk) {
		return
	}
	now := time.Now()
	s.mu.Lock()
	exec := s.executions[id]
	if exec != nil {
		appendPartialOutput(exec, chunk, defaultPartialOutputMaxBytes, now)
	}
	s.mu.Unlock()
}

// RecordCompletedToolInvocation 将已在其它路径完成的工具调用写入监控存储（格式与 CallTool 结束后一致），
// 用于 Eino ADK filesystem execute 等未经过 CallTool 的场景；返回 executionId 供助手消息 mcpExecutionIds 关联。
func (s *Server) RecordCompletedToolInvocation(ctx context.Context, toolName string, args map[string]interface{}, resultText string, invokeErr error) string {
	return s.FinishToolExecution(ctx, "", toolName, args, resultText, invokeErr)
}

// UpdateToolExecutionResult 将监控库中的工具结果更新为送入模型的展示正文（如 reduction 后的 persisted-output）。
func (s *Server) UpdateToolExecutionResult(executionID string, result *ToolResult) error {
	if s == nil {
		return nil
	}
	executionID = strings.TrimSpace(executionID)
	if executionID == "" || result == nil {
		return nil
	}
	if previous, ok := s.GetExecution(executionID); ok && previous != nil &&
		(previous.Status == ToolExecutionStatusBlocked || previous.Result != nil && previous.Result.Blocked) {
		result = cloneToolResult(result)
		result.Blocked, result.IsError = true, true
	}
	s.mu.Lock()
	spill := ToolResultSpillConfig{
		RootDir:     s.spillRootDir,
		ExecutionID: executionID,
	}
	if exec, ok := s.executions[executionID]; ok && exec != nil {
		spill.ConversationID = exec.ConversationID
		result = NormalizeToolResultForStorageWithSpill(result, s.toolResultMaxBytes, spill)
		exec.Result = result
	} else {
		result = NormalizeToolResultForStorageWithSpill(result, s.toolResultMaxBytes, spill)
	}
	s.mu.Unlock()
	if s.storage != nil {
		return s.storage.UpdateToolExecutionResult(executionID, result)
	}
	return nil
}

// cleanupOldExecutions 清理旧的执行记录，防止内存无限增长
func (s *Server) cleanupOldExecutions() {
	if len(s.executions) <= s.maxExecutionsInMemory {
		return
	}

	// 按开始时间排序，找出最旧的记录
	type execWithTime struct {
		id        string
		startTime time.Time
	}
	execs := make([]execWithTime, 0, len(s.executions))
	for id, exec := range s.executions {
		execs = append(execs, execWithTime{
			id:        id,
			startTime: exec.StartTime,
		})
	}

	// 使用 sort 包进行高效排序（最旧的在前）
	sort.Slice(execs, func(i, j int) bool {
		return execs[i].startTime.Before(execs[j].startTime)
	})

	// 删除最旧的记录，保留 maxExecutionsInMemory 条
	toDelete := len(s.executions) - s.maxExecutionsInMemory
	for i := 0; i < toDelete; i++ {
		delete(s.executions, execs[i].id)
	}

	s.logger.Debug("清理旧的执行记录",
		zap.Int("before", len(execs)),
		zap.Int("after", len(s.executions)),
		zap.Int("deleted", toDelete),
	)
}

func (s *Server) registerRunningCancel(id string, cancel context.CancelFunc) {
	s.runningCancelsMu.Lock()
	s.runningCancels[id] = cancel
	s.runningCancelsMu.Unlock()
}

func (s *Server) unregisterRunningCancel(id string) {
	s.runningCancelsMu.Lock()
	delete(s.runningCancels, id)
	s.runningCancelsMu.Unlock()
}

// RegisterToolExecutionCancel lets non-ExecutionService tool paths, such as Eino
// filesystem execute, participate in cancel_tool_execution by execution_id.
func (s *Server) RegisterToolExecutionCancel(id string, cancel context.CancelFunc) {
	id = strings.TrimSpace(id)
	if s == nil || id == "" || cancel == nil {
		return
	}
	s.registerRunningCancel(id, cancel)
}

func (s *Server) UnregisterToolExecutionCancel(id string) {
	id = strings.TrimSpace(id)
	if s == nil || id == "" {
		return
	}
	s.unregisterRunningCancel(id)
}

func (s *Server) readAbortUserNote(id string) string {
	s.runningCancelsMu.Lock()
	defer s.runningCancelsMu.Unlock()
	if s.abortUserNotes == nil {
		return ""
	}
	return s.abortUserNotes[id]
}

func (s *Server) takeAbortUserNote(id string) string {
	s.runningCancelsMu.Lock()
	defer s.runningCancelsMu.Unlock()
	if s.abortUserNotes == nil {
		return ""
	}
	n := s.abortUserNotes[id]
	delete(s.abortUserNotes, id)
	return n
}

// applyAbortUserNoteToCancelledToolResult 监控页「终止并填写说明」时合并「工具已输出 + 用户说明」交给模型。
// exec 等工具会把失败写在 *ToolResult 里并返回 err==nil，若仅在 err!=nil 时合并会漏掉说明，甚至误 clear 掉 note。
func (s *Server) applyAbortUserNoteToCancelledToolResult(executionID string, result **ToolResult, err *error) (cancelledWithUserNote bool) {
	note := strings.TrimSpace(s.readAbortUserNote(executionID))
	if note == "" {
		return false
	}
	hasErr := err != nil && *err != nil
	hasRes := result != nil && *result != nil
	if hasRes && (*result).Blocked {
		return false
	}
	if !hasErr && !hasRes {
		return false
	}
	_ = s.takeAbortUserNote(executionID)
	partial := ""
	if hasRes {
		partial = ToolResultPlainText(*result)
	}
	if partial == "" && hasErr {
		partial = (*err).Error()
	}
	merged := MergePartialToolOutputAndAbortNote(partial, note)
	*err = nil
	*result = &ToolResult{Content: []Content{{Type: "text", Text: merged}}, IsError: true}
	return true
}

// CancelToolExecutionWithNote 取消内部工具；note 非空时与工具已返回文本合并后交给上层模型。
func (s *Server) CancelToolExecutionWithNote(id string, note string) bool {
	if s.executionService != nil && s.executionService.Cancel(id, note) {
		return true
	}
	s.runningCancelsMu.Lock()
	cancel, ok := s.runningCancels[id]
	if !ok || cancel == nil {
		s.runningCancelsMu.Unlock()
		return false
	}
	if strings.TrimSpace(note) != "" {
		if s.abortUserNotes == nil {
			s.abortUserNotes = make(map[string]string)
		}
		s.abortUserNotes[id] = strings.TrimSpace(note)
	}
	s.runningCancelsMu.Unlock()
	cancel()
	return true
}

// CancelToolExecution 取消正在执行的内部工具调用（无用户说明）。
func (s *Server) CancelToolExecution(id string) bool {
	return s.CancelToolExecutionWithNote(id, "")
}

// ActiveRunningExecutionIDs 返回当前进程内仍登记 cancel 的 executionId 快照。
func (s *Server) ActiveRunningExecutionIDs() map[string]struct{} {
	if s == nil {
		return nil
	}
	out := make(map[string]struct{})
	if s.executionService != nil {
		for id := range s.executionService.ActiveRunningExecutionIDs() {
			out[id] = struct{}{}
		}
	}
	s.runningCancelsMu.Lock()
	defer s.runningCancelsMu.Unlock()
	if len(s.runningCancels) == 0 && len(out) == 0 {
		return nil
	}
	for id := range s.runningCancels {
		out[id] = struct{}{}
	}
	return out
}

// initDefaultPrompts 初始化默认提示词模板
func (s *Server) initDefaultPrompts() {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 网络安全测试提示词
	s.prompts["security_scan"] = &Prompt{
		Name:        "security_scan",
		Description: "生成网络安全扫描任务的提示词",
		Arguments: []PromptArgument{
			{Name: "target", Description: "扫描目标（IP地址或域名）", Required: true},
			{Name: "scan_type", Description: "扫描类型（port, vuln, web等）", Required: false},
		},
	}

	// 渗透测试提示词
	s.prompts["penetration_test"] = &Prompt{
		Name:        "penetration_test",
		Description: "生成渗透测试任务的提示词",
		Arguments: []PromptArgument{
			{Name: "target", Description: "测试目标", Required: true},
			{Name: "scope", Description: "测试范围", Required: false},
		},
	}
}

// initDefaultResources 初始化默认资源
// 注意：工具资源现在在 RegisterTool 时自动创建，此函数保留用于其他非工具资源
func (s *Server) initDefaultResources() {
	// 工具资源已改为在 RegisterTool 时自动创建，无需在此硬编码
}

// handleListPrompts 处理列出提示词请求
func (s *Server) handleListPrompts(msg *Message) *Message {
	s.mu.RLock()
	prompts := make([]Prompt, 0, len(s.prompts))
	for _, prompt := range s.prompts {
		prompts = append(prompts, *prompt)
	}
	s.mu.RUnlock()

	response := ListPromptsResponse{
		Prompts: prompts,
	}
	result, _ := json.Marshal(response)
	return &Message{
		ID:      msg.ID,
		Type:    MessageTypeResponse,
		Version: "2.0",
		Result:  result,
	}
}

// handleGetPrompt 处理获取提示词请求
func (s *Server) handleGetPrompt(msg *Message) *Message {
	var req GetPromptRequest
	if err := json.Unmarshal(msg.Params, &req); err != nil {
		return &Message{
			ID:      msg.ID,
			Type:    MessageTypeError,
			Version: "2.0",
			Error:   &Error{Code: -32602, Message: "Invalid params"},
		}
	}

	s.mu.RLock()
	prompt, exists := s.prompts[req.Name]
	s.mu.RUnlock()

	if !exists {
		return &Message{
			ID:      msg.ID,
			Type:    MessageTypeError,
			Version: "2.0",
			Error:   &Error{Code: -32601, Message: "Prompt not found"},
		}
	}

	// 根据提示词名称生成消息
	messages := s.generatePromptMessages(prompt, req.Arguments)

	response := GetPromptResponse{
		Messages: messages,
	}
	result, _ := json.Marshal(response)
	return &Message{
		ID:      msg.ID,
		Type:    MessageTypeResponse,
		Version: "2.0",
		Result:  result,
	}
}

// generatePromptMessages 生成提示词消息
func (s *Server) generatePromptMessages(prompt *Prompt, args map[string]interface{}) []PromptMessage {
	messages := []PromptMessage{}

	switch prompt.Name {
	case "security_scan":
		target, _ := args["target"].(string)
		scanType, _ := args["scan_type"].(string)
		if scanType == "" {
			scanType = "comprehensive"
		}

		content := fmt.Sprintf(`请对目标 %s 执行%s安全扫描。包括：
1. 端口扫描和服务识别
2. 漏洞检测
3. Web应用安全测试
4. 生成详细的安全报告`, target, scanType)

		messages = append(messages, PromptMessage{
			Role:    "user",
			Content: content,
		})

	case "penetration_test":
		target, _ := args["target"].(string)
		scope, _ := args["scope"].(string)

		content := fmt.Sprintf(`请对目标 %s 执行渗透测试。`, target)
		if scope != "" {
			content += fmt.Sprintf("测试范围：%s", scope)
		}
		content += "\n请按照OWASP Top 10进行全面的安全测试。"

		messages = append(messages, PromptMessage{
			Role:    "user",
			Content: content,
		})

	default:
		messages = append(messages, PromptMessage{
			Role:    "user",
			Content: "请执行安全测试任务",
		})
	}

	return messages
}

// handleListResources 处理列出资源请求
func (s *Server) handleListResources(msg *Message) *Message {
	s.mu.RLock()
	resources := make([]Resource, 0, len(s.resources))
	for _, resource := range s.resources {
		resources = append(resources, *resource)
	}
	s.mu.RUnlock()

	response := ListResourcesResponse{
		Resources: resources,
	}
	result, _ := json.Marshal(response)
	return &Message{
		ID:      msg.ID,
		Type:    MessageTypeResponse,
		Version: "2.0",
		Result:  result,
	}
}

// handleReadResource 处理读取资源请求
func (s *Server) handleReadResource(msg *Message) *Message {
	var req ReadResourceRequest
	if err := json.Unmarshal(msg.Params, &req); err != nil {
		return &Message{
			ID:      msg.ID,
			Type:    MessageTypeError,
			Version: "2.0",
			Error:   &Error{Code: -32602, Message: "Invalid params"},
		}
	}

	s.mu.RLock()
	resource, exists := s.resources[req.URI]
	s.mu.RUnlock()

	if !exists {
		return &Message{
			ID:      msg.ID,
			Type:    MessageTypeError,
			Version: "2.0",
			Error:   &Error{Code: -32601, Message: "Resource not found"},
		}
	}

	// 生成资源内容
	content := s.generateResourceContent(resource)

	response := ReadResourceResponse{
		Contents: []ResourceContent{content},
	}
	result, _ := json.Marshal(response)
	return &Message{
		ID:      msg.ID,
		Type:    MessageTypeResponse,
		Version: "2.0",
		Result:  result,
	}
}

// generateResourceContent 生成资源内容
func (s *Server) generateResourceContent(resource *Resource) ResourceContent {
	content := ResourceContent{
		URI:      resource.URI,
		MimeType: resource.MimeType,
	}

	// 如果是工具资源，生成详细文档
	if strings.HasPrefix(resource.URI, "tool://") {
		toolName := strings.TrimPrefix(resource.URI, "tool://")
		content.Text = s.generateToolDocumentation(toolName, resource)
	} else {
		// 其他资源使用描述或默认内容
		content.Text = resource.Description
	}

	return content
}

// generateToolDocumentation 生成工具文档
// 注意：硬编码的工具文档已移除，现在只使用工具定义中的信息
func (s *Server) generateToolDocumentation(toolName string, resource *Resource) string {
	// 获取工具定义以获取更详细的信息
	s.mu.RLock()
	tool, hasTool := s.toolDefs[toolName]
	s.mu.RUnlock()

	// 使用工具定义中的描述信息
	if hasTool {
		doc := fmt.Sprintf("%s\n\n", resource.Description)
		if tool.InputSchema != nil {
			if props, ok := tool.InputSchema["properties"].(map[string]interface{}); ok {
				doc += "参数说明：\n"
				for paramName, paramInfo := range props {
					if paramMap, ok := paramInfo.(map[string]interface{}); ok {
						if desc, ok := paramMap["description"].(string); ok {
							doc += fmt.Sprintf("- %s: %s\n", paramName, desc)
						}
					}
				}
			}
		}
		return doc
	}
	return resource.Description
}

// handleSamplingRequest 处理采样请求
func (s *Server) handleSamplingRequest(msg *Message) *Message {
	var req SamplingRequest
	if err := json.Unmarshal(msg.Params, &req); err != nil {
		return &Message{
			ID:      msg.ID,
			Type:    MessageTypeError,
			Version: "2.0",
			Error:   &Error{Code: -32602, Message: "Invalid params"},
		}
	}

	// 注意：采样功能通常需要连接到实际的LLM服务
	// 这里返回一个占位符响应，实际实现需要集成LLM API
	s.logger.Warn("Sampling request received but not fully implemented",
		zap.Any("request", req),
	)

	response := SamplingResponse{
		Content: []SamplingContent{
			{
				Type: "text",
				Text: "采样功能需要配置LLM服务。请使用Agent Loop API进行AI对话。",
			},
		},
		StopReason: "length",
	}
	result, _ := json.Marshal(response)
	return &Message{
		ID:      msg.ID,
		Type:    MessageTypeResponse,
		Version: "2.0",
		Result:  result,
	}
}

// RegisterPrompt 注册提示词模板
func (s *Server) RegisterPrompt(prompt *Prompt) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prompts[prompt.Name] = prompt
}

// RegisterResource 注册资源
func (s *Server) RegisterResource(resource *Resource) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resources[resource.URI] = resource
}

// HandleStdio 处理标准输入输出（用于 stdio 传输模式）
// MCP 协议使用换行分隔的 JSON-RPC 消息；管道下需每次写入后 Flush，否则客户端会读不到响应
func (s *Server) HandleStdio() error {
	decoder := json.NewDecoder(os.Stdin)
	stdout := bufio.NewWriter(os.Stdout)
	encoder := json.NewEncoder(stdout)
	// 注意：不设置缩进，MCP 协议期望紧凑的 JSON 格式

	for {
		var msg Message
		if err := decoder.Decode(&msg); err != nil {
			if err == io.EOF {
				break
			}
			// 日志输出到 stderr，避免干扰 stdout 的 JSON-RPC 通信
			s.logger.Error("读取消息失败", zap.Error(err))
			// 发送错误响应
			errorMsg := Message{
				ID:      msg.ID,
				Type:    MessageTypeError,
				Version: "2.0",
				Error:   &Error{Code: -32700, Message: "Parse error", Data: err.Error()},
			}
			if err := encoder.Encode(errorMsg); err != nil {
				return fmt.Errorf("发送错误响应失败: %w", err)
			}
			if err := stdout.Flush(); err != nil {
				return fmt.Errorf("刷新 stdout 失败: %w", err)
			}
			continue
		}

		// 处理消息
		response := s.handleMessage(context.Background(), &msg)

		// 如果是通知（response 为 nil），不需要发送响应
		if response == nil {
			continue
		}

		// 发送响应
		if err := encoder.Encode(response); err != nil {
			return fmt.Errorf("发送响应失败: %w", err)
		}
		if err := stdout.Flush(); err != nil {
			return fmt.Errorf("刷新 stdout 失败: %w", err)
		}
	}

	return nil
}

// sendError 发送错误响应
func (s *Server) sendError(w http.ResponseWriter, id interface{}, code int, message, data string) {
	var msgID MessageID
	if id != nil {
		msgID = MessageID{value: id}
	}
	response := Message{
		ID:      msgID,
		Type:    MessageTypeError,
		Version: "2.0",
		Error:   &Error{Code: code, Message: message, Data: data},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
