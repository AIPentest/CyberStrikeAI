package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cyberstrike-ai/internal/authctx"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/toolguard"

	"go.uber.org/zap"
)

const (
	// externalToolListCacheTTL 已连接外部 MCP 的工具列表缓存有效期，避免每次 API 请求都打远程 ListTools。
	externalToolListCacheTTL = 60 * time.Second
	// externalToolCountRefreshInterval 后台刷新工具数量的间隔（仅刷新缓存过期或缺失的客户端）。
	externalToolCountRefreshInterval = 60 * time.Second
)

// toolListCacheEntry 外部 MCP 工具列表缓存条目
type toolListCacheEntry struct {
	tools     []Tool
	updatedAt time.Time
}

// listToolsInflight 合并同一 MCP 上并发的 ListTools 请求
type listToolsInflight struct {
	done  chan struct{}
	tools []Tool
	err   error
}

type ExternalMCPResilienceConfig struct {
	MaxConcurrentPerServer  int
	MaxConcurrentTotal      int
	CircuitFailureThreshold int
	CircuitCooldown         time.Duration
}

type externalMCPServerRuntime struct {
	semaphore           chan struct{}
	consecutiveFailures int
	circuitOpenUntil    time.Time
}

// ExternalMCPManager 外部MCP管理器
type ExternalMCPManager struct {
	clients            map[string]ExternalMCPClient
	configs            map[string]config.ExternalMCPServerConfig
	logger             *zap.Logger
	storage            MonitorStorage                // 可选的持久化存储
	executions         map[string]*ToolExecution     // 执行记录
	stats              map[string]*ToolStats         // 工具统计信息
	errors             map[string]string             // 错误信息
	toolCounts         map[string]int                // 工具数量缓存
	toolCountsMu       sync.RWMutex                  // 工具数量缓存的锁
	toolCache          map[string]toolListCacheEntry // 工具列表缓存：MCP名称 -> 工具列表
	toolCacheMu        sync.RWMutex                  // 工具列表缓存的锁
	listToolsMu        sync.Mutex
	listToolsInflight  map[string]*listToolsInflight
	stopRefresh        chan struct{}  // 停止后台刷新的信号
	refreshWg          sync.WaitGroup // 等待后台刷新goroutine完成
	refreshing         atomic.Bool    // 防止 refreshToolCounts 并发堆积
	mu                 sync.RWMutex
	runningCancels     map[string]context.CancelFunc
	abortUserNotes     map[string]string
	reconnectMu        sync.Mutex
	reconnecting       map[string]bool
	reconnectLastTry   map[string]time.Time
	reconnectAttempts  map[string]int
	toolAuthorizer     func(context.Context, string, map[string]interface{}) error
	toolGuard          *toolguard.Manager
	executionService   *ExecutionService
	toolWaitTimeout    time.Duration
	toolResultMaxBytes int
	spillRootDir       string
	resilience         ExternalMCPResilienceConfig
	serverRuntimes     map[string]*externalMCPServerRuntime
	globalSemaphore    chan struct{}
}

// NewExternalMCPManager 创建外部MCP管理器
func NewExternalMCPManager(logger *zap.Logger) *ExternalMCPManager {
	return NewExternalMCPManagerWithStorage(logger, nil)
}

// SetToolAuthorizer installs the policy decision point for all external MCP
// invocations. App wiring configures this before any Agent can call a tool.
func (m *ExternalMCPManager) SetToolAuthorizer(authorizer func(context.Context, string, map[string]interface{}) error) {
	m.mu.Lock()
	m.toolAuthorizer = authorizer
	m.mu.Unlock()
}

// SetToolGuard installs safety rules evaluated before dispatch to external MCPs.
func (m *ExternalMCPManager) SetToolGuard(guard *toolguard.Manager) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.toolGuard = guard
	m.mu.Unlock()
}

func (m *ExternalMCPManager) checkToolGuard(toolName string, args map[string]interface{}) *ToolResult {
	m.mu.RLock()
	guard := m.toolGuard
	m.mu.RUnlock()
	return toolGuardBlockedResult(guard, toolName, args)
}

// NewExternalMCPManagerWithStorage 创建外部MCP管理器（带持久化存储）
func NewExternalMCPManagerWithStorage(logger *zap.Logger, storage MonitorStorage) *ExternalMCPManager {
	manager := &ExternalMCPManager{
		clients:            make(map[string]ExternalMCPClient),
		configs:            make(map[string]config.ExternalMCPServerConfig),
		logger:             logger,
		storage:            storage,
		executions:         make(map[string]*ToolExecution),
		stats:              make(map[string]*ToolStats),
		errors:             make(map[string]string),
		toolCounts:         make(map[string]int),
		toolCache:          make(map[string]toolListCacheEntry),
		listToolsInflight:  make(map[string]*listToolsInflight),
		stopRefresh:        make(chan struct{}),
		runningCancels:     make(map[string]context.CancelFunc),
		abortUserNotes:     make(map[string]string),
		reconnecting:       make(map[string]bool),
		reconnectLastTry:   make(map[string]time.Time),
		reconnectAttempts:  make(map[string]int),
		toolWaitTimeout:    60 * time.Second,
		toolResultMaxBytes: DefaultToolResultMaxBytes,
		resilience: ExternalMCPResilienceConfig{
			MaxConcurrentPerServer:  2,
			MaxConcurrentTotal:      16,
			CircuitFailureThreshold: 3,
			CircuitCooldown:         60 * time.Second,
		},
		serverRuntimes:  make(map[string]*externalMCPServerRuntime),
		globalSemaphore: make(chan struct{}, 16),
	}
	manager.executionService = NewExecutionService(storage, logger)
	// 启动后台刷新工具数量的goroutine
	manager.startToolCountRefresh()
	return manager
}

func (m *ExternalMCPManager) ConfigureToolResultMaxBytes(maxBytes int) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.toolResultMaxBytes = maxBytes
	m.mu.Unlock()
	if m.executionService != nil {
		m.executionService.ConfigureToolResultMaxBytes(maxBytes)
	}
}

// ConfigureToolResultSpillRoot sets the local directory root used when oversized
// tool results are spilled (aligned with reduction_root_dir; empty → tmp/reduction).
func (m *ExternalMCPManager) ConfigureToolResultSpillRoot(rootDir string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.spillRootDir = strings.TrimSpace(rootDir)
	m.mu.Unlock()
	if m.executionService != nil {
		m.executionService.ConfigureToolResultSpillRoot(rootDir)
	}
}

// ConfigureToolWaitTimeoutSeconds controls how long an agent-facing tool call
// waits for an external MCP execution before returning an execution_id that can
// be polled with wait_tool_execution. seconds<=0 waits until completion.
func (m *ExternalMCPManager) ConfigureToolWaitTimeoutSeconds(seconds int) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if seconds <= 0 {
		m.toolWaitTimeout = 0
		return
	}
	m.toolWaitTimeout = time.Duration(seconds) * time.Second
}

func (m *ExternalMCPManager) ConfigureResilience(cfg ExternalMCPResilienceConfig) {
	if m == nil {
		return
	}
	normalized := normalizeExternalMCPResilienceConfig(cfg)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resilience = normalized
	m.serverRuntimes = make(map[string]*externalMCPServerRuntime)
	if normalized.MaxConcurrentTotal > 0 {
		m.globalSemaphore = make(chan struct{}, normalized.MaxConcurrentTotal)
	} else {
		m.globalSemaphore = nil
	}
}

func normalizeExternalMCPResilienceConfig(cfg ExternalMCPResilienceConfig) ExternalMCPResilienceConfig {
	if cfg.MaxConcurrentPerServer == 0 {
		cfg.MaxConcurrentPerServer = 2
	}
	if cfg.MaxConcurrentTotal == 0 {
		cfg.MaxConcurrentTotal = 16
	}
	if cfg.CircuitFailureThreshold == 0 {
		cfg.CircuitFailureThreshold = 3
	}
	if cfg.CircuitCooldown <= 0 {
		cfg.CircuitCooldown = 60 * time.Second
	}
	if cfg.MaxConcurrentPerServer < 0 {
		cfg.MaxConcurrentPerServer = 0
	}
	if cfg.MaxConcurrentTotal < 0 {
		cfg.MaxConcurrentTotal = 0
	}
	return cfg
}

// LoadConfigs 加载配置
func (m *ExternalMCPManager) LoadConfigs(cfg *config.ExternalMCPConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if cfg == nil || cfg.Servers == nil {
		return
	}

	m.configs = make(map[string]config.ExternalMCPServerConfig)
	for name, serverCfg := range cfg.Servers {
		m.configs[name] = serverCfg
	}
}

// GetConfigs 获取所有配置
func (m *ExternalMCPManager) GetConfigs() map[string]config.ExternalMCPServerConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]config.ExternalMCPServerConfig)
	for k, v := range m.configs {
		result[k] = v
	}
	return result
}

// AddOrUpdateConfig 添加或更新配置
func (m *ExternalMCPManager) AddOrUpdateConfig(name string, serverCfg config.ExternalMCPServerConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 如果已存在客户端，先关闭
	if client, exists := m.clients[name]; exists {
		client.Close()
		delete(m.clients, name)
	}

	m.configs[name] = serverCfg

	// 如果启用，自动连接
	if m.isEnabled(serverCfg) {
		go m.connectClient(name, serverCfg)
	}

	return nil
}

// RemoveConfig 移除配置
func (m *ExternalMCPManager) RemoveConfig(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 关闭客户端
	if client, exists := m.clients[name]; exists {
		client.Close()
		delete(m.clients, name)
	}

	delete(m.configs, name)
	m.clearReconnectState(name)

	// 清理工具数量缓存
	m.toolCountsMu.Lock()
	delete(m.toolCounts, name)
	m.toolCountsMu.Unlock()

	// 清理工具列表缓存
	m.toolCacheMu.Lock()
	delete(m.toolCache, name)
	m.toolCacheMu.Unlock()

	return nil
}

// StartClient 启动客户端（用户手动启动；连接失败不自动重试）
func (m *ExternalMCPManager) StartClient(name string) error {
	return m.startClient(name, false)
}

// startClient 启动客户端。autoReconnect 为 true 时用于断连自愈：尊重停用状态，失败后按退避继续重试。
func (m *ExternalMCPManager) startClient(name string, autoReconnect bool) error {
	m.mu.Lock()
	serverCfg, exists := m.configs[name]
	m.mu.Unlock()

	if !exists {
		return fmt.Errorf("配置不存在: %s", name)
	}

	if autoReconnect && !m.isEnabled(serverCfg) {
		return nil
	}

	// 检查是否已经有连接的客户端
	m.mu.RLock()
	existingClient, hasClient := m.clients[name]
	m.mu.RUnlock()

	if hasClient {
		// 检查客户端是否已连接
		if existingClient.IsConnected() {
			// 客户端已连接，直接返回成功（目标状态已达成）
			if !autoReconnect {
				m.mu.Lock()
				serverCfg.ExternalMCPEnable = true
				m.configs[name] = serverCfg
				m.mu.Unlock()
			}
			return nil
		}
		// 如果有客户端但未连接，先关闭
		existingClient.Close()
		m.mu.Lock()
		delete(m.clients, name)
		m.mu.Unlock()
	}

	if autoReconnect {
		m.mu.RLock()
		serverCfg, exists = m.configs[name]
		enabled := exists && m.isEnabled(serverCfg)
		m.mu.RUnlock()
		if !enabled {
			return nil
		}
	}

	// 更新配置为启用
	m.mu.Lock()
	serverCfg.ExternalMCPEnable = true
	m.configs[name] = serverCfg
	// 清除之前的错误信息（重新启动时）
	delete(m.errors, name)
	m.mu.Unlock()

	// 立即创建客户端并设置为"connecting"状态，这样前端可以立即看到状态
	client := m.createClient(serverCfg)
	if client == nil {
		return fmt.Errorf("无法创建客户端：不支持的传输模式")
	}

	// 设置状态为connecting
	m.setClientStatus(client, "connecting")

	// 立即保存客户端，这样前端查询时就能看到"connecting"状态
	m.mu.Lock()
	m.clients[name] = client
	m.mu.Unlock()

	// 在后台异步进行实际连接
	go func(reconnect bool) {
		if err := m.doConnect(name, serverCfg, client); err != nil {
			m.logger.Error("连接外部MCP客户端失败",
				zap.String("name", name),
				zap.Bool("auto_reconnect", reconnect),
				zap.Error(err),
			)
			// 连接失败，设置状态为error并保存错误信息
			m.setClientStatus(client, "error")
			m.mu.Lock()
			m.errors[name] = err.Error()
			m.mu.Unlock()
			// 触发工具数量刷新（连接失败，工具数量应为0）
			m.triggerToolCountRefresh()
			if reconnect {
				m.scheduleReconnectAfterFailure(name)
			}
		} else {
			// 连接成功，清除错误信息
			m.mu.Lock()
			delete(m.errors, name)
			m.mu.Unlock()
			m.onClientConnected(name)
			// 异步拉取工具列表（singleflight 去重，结果同时写入 toolCache 与 toolCounts）
			go m.refreshToolCache(name, client)
		}
	}(autoReconnect)

	return nil
}

// StopClient 停止客户端
func (m *ExternalMCPManager) StopClient(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	serverCfg, exists := m.configs[name]
	if !exists {
		return fmt.Errorf("配置不存在: %s", name)
	}

	// 关闭客户端
	if client, exists := m.clients[name]; exists {
		client.Close()
		delete(m.clients, name)
	}

	// 清除错误信息
	delete(m.errors, name)

	// 更新工具数量缓存（停止后工具数量为0）
	m.toolCountsMu.Lock()
	m.toolCounts[name] = 0
	m.toolCountsMu.Unlock()

	m.toolCacheMu.Lock()
	delete(m.toolCache, name)
	m.toolCacheMu.Unlock()

	// 更新配置为禁用
	serverCfg.ExternalMCPEnable = false
	m.configs[name] = serverCfg

	m.clearReconnectState(name)

	return nil
}

// GetClient 获取客户端
func (m *ExternalMCPManager) GetClient(name string) (ExternalMCPClient, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	client, exists := m.clients[name]
	return client, exists
}

// GetError 获取错误信息
func (m *ExternalMCPManager) GetError(name string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.errors[name]
}

// GetAllTools 获取所有外部MCP的工具
// 优先从已连接的客户端获取，如果连接断开则返回缓存的工具列表
// 策略：
//   - error 状态：不使用缓存，直接跳过（配置错误或服务不可用）
//   - disconnected/connecting 状态：使用缓存（临时断开）
//   - connected 状态：正常获取，失败时降级使用缓存
func (m *ExternalMCPManager) GetAllTools(ctx context.Context) ([]Tool, error) {
	m.mu.RLock()
	clients := make(map[string]ExternalMCPClient)
	for k, v := range m.clients {
		clients[k] = v
	}
	m.mu.RUnlock()

	var allTools []Tool
	var hasError bool
	var lastError error

	// 使用较短的超时时间进行快速检查（3秒），避免阻塞
	quickCtx, quickCancel := context.WithTimeout(ctx, 3*time.Second)
	defer quickCancel()

	for name, client := range clients {
		tools, err := m.getToolsForClient(name, client, quickCtx)
		if err != nil {
			// 记录错误，但继续处理其他客户端
			hasError = true
			if lastError == nil {
				lastError = err
			}
			continue
		}

		// 为工具添加前缀，避免冲突
		for _, tool := range tools {
			tool.Name = fmt.Sprintf("%s::%s", name, tool.Name)
			allTools = append(allTools, tool)
		}
	}

	// 如果有错误但至少返回了一些工具，不返回错误（部分成功）
	if hasError && len(allTools) == 0 {
		return nil, fmt.Errorf("获取外部MCP工具失败: %w", lastError)
	}

	return allTools, nil
}

// getToolsForClient 获取指定客户端的工具列表
// 返回工具列表和错误（如果完全无法获取）
func (m *ExternalMCPManager) getToolsForClient(name string, client ExternalMCPClient, ctx context.Context) ([]Tool, error) {
	status := client.GetStatus()

	// error 状态：不使用缓存，直接返回错误
	if status == "error" {
		m.logger.Debug("跳过连接失败的外部MCP（不使用缓存）",
			zap.String("name", name),
			zap.String("status", status),
		)
		return nil, fmt.Errorf("外部MCP连接失败: %s", name)
	}

	// 已连接：缓存优先，仅在缺失或过期时打远程 ListTools
	if client.IsConnected() {
		if tools, ok := m.getFreshCachedTools(name); ok {
			return tools, nil
		}
		if tools, ok := m.getAnyCachedTools(name); ok {
			m.triggerToolListRefresh(name, client)
			return tools, nil
		}
		tools, err := m.listToolsDeduped(ctx, name, client)
		if err != nil {
			return m.getCachedTools(name, "连接正常但获取失败", err)
		}
		return tools, nil
	}

	// 未连接：根据状态决定是否使用缓存
	if status == "disconnected" || status == "connecting" {
		return m.getCachedTools(name, fmt.Sprintf("客户端临时断开（状态: %s）", status), nil)
	}

	// 其他未知状态，不使用缓存
	m.logger.Debug("跳过外部MCP（未知状态）",
		zap.String("name", name),
		zap.String("status", status),
	)
	return nil, fmt.Errorf("外部MCP状态未知: %s (状态: %s)", name, status)
}

// getCachedTools 获取缓存的工具列表（含空列表缓存）
func (m *ExternalMCPManager) getCachedTools(name, reason string, originalErr error) ([]Tool, error) {
	if tools, ok := m.getAnyCachedTools(name); ok {
		m.logger.Debug("使用缓存的工具列表",
			zap.String("name", name),
			zap.String("reason", reason),
			zap.Int("count", len(tools)),
			zap.Error(originalErr),
		)
		return tools, nil
	}

	if originalErr != nil {
		return nil, fmt.Errorf("获取外部MCP工具失败且无缓存: %w", originalErr)
	}
	return nil, fmt.Errorf("外部MCP无缓存工具: %s", name)
}

func (m *ExternalMCPManager) isToolCacheFresh(updatedAt time.Time) bool {
	return !updatedAt.IsZero() && time.Since(updatedAt) < externalToolListCacheTTL
}

func cloneTools(tools []Tool) []Tool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]Tool, len(tools))
	copy(out, tools)
	return out
}

func (m *ExternalMCPManager) getFreshCachedTools(name string) ([]Tool, bool) {
	m.toolCacheMu.RLock()
	entry, ok := m.toolCache[name]
	m.toolCacheMu.RUnlock()
	if !ok || !m.isToolCacheFresh(entry.updatedAt) {
		return nil, false
	}
	return cloneTools(entry.tools), true
}

func (m *ExternalMCPManager) getAnyCachedTools(name string) ([]Tool, bool) {
	m.toolCacheMu.RLock()
	entry, ok := m.toolCache[name]
	m.toolCacheMu.RUnlock()
	if !ok {
		return nil, false
	}
	return cloneTools(entry.tools), true
}

// listToolsDeduped 对同一 MCP 合并并发 ListTools，并更新 toolCache / toolCounts。
func (m *ExternalMCPManager) listToolsDeduped(ctx context.Context, name string, client ExternalMCPClient) ([]Tool, error) {
	m.listToolsMu.Lock()
	if inflight, exists := m.listToolsInflight[name]; exists {
		m.listToolsMu.Unlock()
		select {
		case <-inflight.done:
			if inflight.err != nil {
				return nil, inflight.err
			}
			return cloneTools(inflight.tools), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	inflight := &listToolsInflight{done: make(chan struct{})}
	m.listToolsInflight[name] = inflight
	m.listToolsMu.Unlock()

	inflight.tools, inflight.err = client.ListTools(ctx)
	if inflight.err == nil {
		m.updateToolCache(name, inflight.tools)
	}

	m.listToolsMu.Lock()
	delete(m.listToolsInflight, name)
	close(inflight.done)
	m.listToolsMu.Unlock()

	if inflight.err != nil {
		m.handleConnectionDead(name, client, inflight.err)
		return nil, inflight.err
	}
	return cloneTools(inflight.tools), nil
}

// InvalidateToolCache 清除指定外部 MCP 的工具列表缓存（手动刷新时使用）
func (m *ExternalMCPManager) InvalidateToolCache(name string) {
	m.toolCacheMu.Lock()
	delete(m.toolCache, name)
	m.toolCacheMu.Unlock()
}

// InvalidateAllToolCaches 清除所有外部 MCP 工具列表缓存
func (m *ExternalMCPManager) InvalidateAllToolCaches() {
	m.toolCacheMu.Lock()
	m.toolCache = make(map[string]toolListCacheEntry)
	m.toolCacheMu.Unlock()
}

func (m *ExternalMCPManager) triggerToolListRefresh(name string, client ExternalMCPClient) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, _ = m.listToolsDeduped(ctx, name, client)
	}()
}

// updateToolCache 更新工具列表缓存与工具数量
func (m *ExternalMCPManager) updateToolCache(name string, tools []Tool) {
	stored := cloneTools(tools)
	m.toolCacheMu.Lock()
	m.toolCache[name] = toolListCacheEntry{tools: stored, updatedAt: time.Now()}
	m.toolCacheMu.Unlock()

	m.toolCountsMu.Lock()
	m.toolCounts[name] = len(stored)
	m.toolCountsMu.Unlock()

	if len(stored) == 0 {
		m.logger.Warn("外部MCP返回空工具列表",
			zap.String("name", name),
			zap.String("hint", "服务可能暂时不可用，工具列表为空"),
		)
	} else {
		m.logger.Debug("工具列表缓存已更新",
			zap.String("name", name),
			zap.Int("count", len(stored)),
		)
	}
}

// CallTool 调用外部MCP工具（返回执行ID）
func (m *ExternalMCPManager) CallTool(ctx context.Context, toolName string, args map[string]interface{}) (*ToolResult, string, error) {
	if m.executionService == nil {
		m.executionService = NewExecutionService(m.storage, m.logger)
		m.executionService.ConfigureToolResultMaxBytes(m.toolResultMaxBytes)
		m.executionService.ConfigureToolResultSpillRoot(m.spillRootDir)
	}
	var ownerUserID string
	if principal, ok := authctx.PrincipalFromContext(ctx); ok {
		ownerUserID = principal.UserID
	}
	var mcpName, actualToolName string
	var client ExternalMCPClient
	var blockedByGuard bool
	handle, err := m.executionService.Submit(ctx, ExecutionRequest{
		ConfirmCancellation: func(confirmCtx context.Context) error {
			if confirmer, ok := client.(ExternalCancellationConfirmer); ok {
				return confirmer.ConfirmToolCancellation(confirmCtx, actualToolName, args)
			}
			return fmt.Errorf("external MCP client has no cancellation acknowledgement")
		},
		Remote:         true,
		ToolName:       toolName,
		Arguments:      args,
		ConversationID: MCPConversationIDFromContext(ctx),
		OwnerUserID:    ownerUserID,
		PreRun: func(runCtx context.Context, exec *ToolExecution) (func(), error) {
			_, authenticated := authctx.PrincipalFromContext(runCtx)
			m.mu.RLock()
			authorizer := m.toolAuthorizer
			m.mu.RUnlock()
			if authorizer != nil {
				if err := authorizer(runCtx, toolName, args); err != nil {
					return nil, fmt.Errorf("external tool authorization denied: %w", err)
				}
			} else if authenticated {
				return nil, fmt.Errorf("external tool authorization policy is not configured")
			}
			if blocked := m.checkToolGuard(toolName, args); blocked != nil {
				blockedByGuard = true
				return nil, &toolGuardBlockError{result: blocked}
			}

			// 解析工具名称：name::toolName
			if idx := findSubstring(toolName, "::"); idx > 0 {
				mcpName = toolName[:idx]
				actualToolName = toolName[idx+2:]
			} else {
				return nil, fmt.Errorf("无效的工具名称格式: %s", toolName)
			}

			var exists bool
			client, exists = m.GetClient(mcpName)
			if !exists {
				return nil, fmt.Errorf("外部MCP客户端不存在: %s", mcpName)
			}
			if err := m.checkExternalMCPCircuit(mcpName); err != nil {
				return nil, err
			}

			// 检查连接状态，如果未连接或状态为error，不允许调用
			if !client.IsConnected() {
				status := client.GetStatus()
				if status == "error" {
					// 获取错误信息（如果有）
					errorMsg := m.GetError(mcpName)
					if errorMsg != "" {
						return nil, fmt.Errorf("外部MCP连接失败: %s (错误: %s)", mcpName, errorMsg)
					}
					return nil, fmt.Errorf("外部MCP连接失败: %s", mcpName)
				}
				return nil, fmt.Errorf("外部MCP客户端未连接: %s (状态: %s)", mcpName, status)
			}

			release, acquireErr := m.acquireExternalMCPCallSlot(runCtx, mcpName)
			if acquireErr != nil {
				return nil, acquireErr
			}
			return release, nil
		},
		Run: func(runCtx context.Context) (*ToolResult, error) {
			// Rules may have changed while this execution waited for a slot.
			if blocked := m.checkToolGuard(toolName, args); blocked != nil {
				blockedByGuard = true
				return blocked, nil
			}
			result, callErr := client.CallTool(runCtx, actualToolName, args)
			if callErr != nil {
				m.handleConnectionDead(mcpName, client, callErr)
			}
			return result, callErr
		},
		OnDone: func(exec *ToolExecution) {
			failed := exec != nil && executionStatusCountsAsFailed(exec.Status)
			if mcpName != "" && !blockedByGuard && (exec == nil || exec.Status != ToolExecutionStatusBlocked) {
				m.recordExternalMCPResult(mcpName, failed)
			}
			if exec != nil {
				m.updateStats(toolName, exec.Status)
			}
		},
	})
	if err != nil {
		return nil, "", err
	}

	m.mu.RLock()
	waitTimeout := m.toolWaitTimeout
	m.mu.RUnlock()
	snapshot, waitErr := m.executionService.Wait(ctx, handle.ID, waitTimeout)
	if errors.Is(waitErr, ErrExecutionWaitTimeout) {
		return externalMCPWaitTimeoutResult(snapshot, waitTimeout), handle.ID, nil
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

func externalMCPWaitTimeoutResult(snapshot *ExecutionSnapshot, waitTimeout time.Duration) *ToolResult {
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

func (m *ExternalMCPManager) checkExternalMCPCircuit(mcpName string) error {
	if m == nil {
		return nil
	}
	name := strings.TrimSpace(mcpName)
	if name == "" {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.resilience.CircuitFailureThreshold < 0 {
		return nil
	}
	rt := m.externalMCPRuntimeLocked(name)
	if rt == nil || rt.circuitOpenUntil.IsZero() {
		return nil
	}
	now := time.Now()
	if now.Before(rt.circuitOpenUntil) {
		return fmt.Errorf("外部MCP服务 %s 已临时熔断，预计 %s 后重试", name, time.Until(rt.circuitOpenUntil).Round(time.Second))
	}
	rt.circuitOpenUntil = time.Time{}
	return nil
}

func (m *ExternalMCPManager) acquireExternalMCPCallSlot(ctx context.Context, mcpName string) (func(), error) {
	if m == nil {
		return func() {}, nil
	}
	name := strings.TrimSpace(mcpName)
	m.mu.Lock()
	rt := m.externalMCPRuntimeLocked(name)
	serverSem := chan struct{}(nil)
	if rt != nil {
		serverSem = rt.semaphore
	}
	globalSem := m.globalSemaphore
	m.mu.Unlock()

	releaseGlobal := false
	if globalSem != nil {
		select {
		case globalSem <- struct{}{}:
			releaseGlobal = true
		case <-ctxDone(ctx):
			return func() {}, contextErr(ctx)
		}
	}
	releaseServer := false
	if serverSem != nil {
		select {
		case serverSem <- struct{}{}:
			releaseServer = true
		case <-ctxDone(ctx):
			if releaseGlobal {
				<-globalSem
			}
			return func() {}, contextErr(ctx)
		}
	}
	return func() {
		if releaseServer {
			<-serverSem
		}
		if releaseGlobal {
			<-globalSem
		}
	}, nil
}

func contextErr(ctx context.Context) error {
	if ctx == nil || ctx.Err() == nil {
		return context.Canceled
	}
	return ctx.Err()
}

func (m *ExternalMCPManager) recordExternalMCPResult(mcpName string, failed bool) {
	if m == nil || strings.TrimSpace(mcpName) == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	rt := m.externalMCPRuntimeLocked(mcpName)
	if rt == nil {
		return
	}
	if !failed {
		rt.consecutiveFailures = 0
		rt.circuitOpenUntil = time.Time{}
		return
	}
	if m.resilience.CircuitFailureThreshold < 0 {
		return
	}
	rt.consecutiveFailures++
	if rt.consecutiveFailures >= m.resilience.CircuitFailureThreshold {
		rt.circuitOpenUntil = time.Now().Add(m.resilience.CircuitCooldown)
		m.logger.Warn("外部MCP服务触发熔断",
			zap.String("name", mcpName),
			zap.Int("consecutiveFailures", rt.consecutiveFailures),
			zap.Duration("cooldown", m.resilience.CircuitCooldown),
		)
	}
}

func (m *ExternalMCPManager) externalMCPRuntimeLocked(mcpName string) *externalMCPServerRuntime {
	if m.serverRuntimes == nil {
		m.serverRuntimes = make(map[string]*externalMCPServerRuntime)
	}
	name := strings.TrimSpace(mcpName)
	if name == "" {
		return nil
	}
	if rt := m.serverRuntimes[name]; rt != nil {
		return rt
	}
	var sem chan struct{}
	if m.resilience.MaxConcurrentPerServer > 0 {
		sem = make(chan struct{}, m.resilience.MaxConcurrentPerServer)
	}
	rt := &externalMCPServerRuntime{semaphore: sem}
	m.serverRuntimes[name] = rt
	return rt
}

func (m *ExternalMCPManager) applyAbortUserNoteToCancelledToolResult(executionID string, result **ToolResult, err *error) (cancelledWithUserNote bool) {
	note := strings.TrimSpace(m.readAbortUserNote(executionID))
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
	_ = m.takeAbortUserNote(executionID)
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

func (m *ExternalMCPManager) readAbortUserNote(id string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.abortUserNotes == nil {
		return ""
	}
	return m.abortUserNotes[id]
}

func (m *ExternalMCPManager) takeAbortUserNote(id string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.abortUserNotes == nil {
		return ""
	}
	n := m.abortUserNotes[id]
	delete(m.abortUserNotes, id)
	return n
}

// cleanupOldExecutions 清理旧的执行记录（保持内存中的记录数量在限制内）
func (m *ExternalMCPManager) cleanupOldExecutions() {
	const maxExecutionsInMemory = 1000
	if len(m.executions) <= maxExecutionsInMemory {
		return
	}

	// 按开始时间排序，删除最旧的记录
	type execTime struct {
		id        string
		startTime time.Time
	}
	var execs []execTime
	for id, exec := range m.executions {
		execs = append(execs, execTime{id: id, startTime: exec.StartTime})
	}

	// 按时间排序
	for i := 0; i < len(execs)-1; i++ {
		for j := i + 1; j < len(execs); j++ {
			if execs[i].startTime.After(execs[j].startTime) {
				execs[i], execs[j] = execs[j], execs[i]
			}
		}
	}

	// 删除最旧的记录
	toDelete := len(m.executions) - maxExecutionsInMemory
	for i := 0; i < toDelete && i < len(execs); i++ {
		delete(m.executions, execs[i].id)
	}
}

// GetExecution 获取执行记录（先从内存查找，再从数据库查找）
func (m *ExternalMCPManager) GetExecution(id string) (*ToolExecution, bool) {
	if m.executionService != nil {
		if snap, err := m.executionService.Get(id); err == nil && snap != nil && snap.Execution != nil {
			return snap.Execution, true
		}
	}
	m.mu.RLock()
	exec, exists := m.executions[id]
	m.mu.RUnlock()

	if exists {
		return exec, true
	}

	if m.storage != nil {
		exec, err := m.storage.GetToolExecution(id)
		if err == nil {
			return exec, true
		}
	}

	return nil, false
}

func (m *ExternalMCPManager) registerRunningCancel(id string, cancel context.CancelFunc) {
	m.mu.Lock()
	m.runningCancels[id] = cancel
	m.mu.Unlock()
}

func (m *ExternalMCPManager) unregisterRunningCancel(id string) {
	m.mu.Lock()
	delete(m.runningCancels, id)
	m.mu.Unlock()
}

// CancelToolExecutionWithNote 取消外部 MCP 工具；note 非空时与已返回输出合并后交给模型。
func (m *ExternalMCPManager) CancelToolExecutionWithNote(id string, note string) bool {
	if m.executionService != nil && m.executionService.Cancel(id, note) {
		return true
	}
	m.mu.Lock()
	cancel, ok := m.runningCancels[id]
	if !ok || cancel == nil {
		m.mu.Unlock()
		return false
	}
	if strings.TrimSpace(note) != "" {
		if m.abortUserNotes == nil {
			m.abortUserNotes = make(map[string]string)
		}
		m.abortUserNotes[id] = strings.TrimSpace(note)
	}
	m.mu.Unlock()
	cancel()
	return true
}

// CancelToolExecution 取消正在执行的外部 MCP 工具（无用户说明）。
func (m *ExternalMCPManager) CancelToolExecution(id string) bool {
	return m.CancelToolExecutionWithNote(id, "")
}

// ActiveRunningExecutionIDs 返回当前进程内仍登记 cancel 的外部 MCP executionId 快照。
func (m *ExternalMCPManager) ActiveRunningExecutionIDs() map[string]struct{} {
	if m == nil {
		return nil
	}
	if m.executionService != nil {
		if ids := m.executionService.ActiveRunningExecutionIDs(); len(ids) > 0 {
			return ids
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.runningCancels) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(m.runningCancels))
	for id := range m.runningCancels {
		out[id] = struct{}{}
	}
	return out
}

// updateStats 更新统计信息
func (m *ExternalMCPManager) updateStats(toolName string, status string) {
	now := time.Now()
	if m.storage != nil {
		totalCalls := 1
		successCalls := 0
		failedCalls := 0
		if executionStatusCountsAsFailed(status) {
			failedCalls = 1
		} else if status == ToolExecutionStatusCompleted {
			successCalls = 1
		}
		if err := m.storage.UpdateToolStats(toolName, totalCalls, successCalls, failedCalls, &now); err != nil {
			m.logger.Warn("保存统计信息到数据库失败", zap.Error(err))
		}
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.stats[toolName] == nil {
		m.stats[toolName] = &ToolStats{
			ToolName: toolName,
		}
	}

	stats := m.stats[toolName]
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

// GetStats 获取MCP服务器统计信息
func (m *ExternalMCPManager) GetStats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	total := len(m.configs)
	enabled := 0
	disabled := 0
	connected := 0

	for name, cfg := range m.configs {
		if m.isEnabled(cfg) {
			enabled++
			if client, exists := m.clients[name]; exists && client.IsConnected() {
				connected++
			}
		} else {
			disabled++
		}
	}

	return map[string]interface{}{
		"total":     total,
		"enabled":   enabled,
		"disabled":  disabled,
		"connected": connected,
	}
}

// GetToolStats 获取工具统计信息（合并内存和数据库）
// 只返回外部MCP工具的统计信息（工具名称包含 "::"）
func (m *ExternalMCPManager) GetToolStats() map[string]*ToolStats {
	result := make(map[string]*ToolStats)

	// 从数据库加载统计信息（如果使用数据库存储）
	if m.storage != nil {
		dbStats, err := m.storage.LoadToolStats()
		if err == nil {
			// 只保留外部MCP工具的统计信息（工具名称包含 "::"）
			for k, v := range dbStats {
				if findSubstring(k, "::") > 0 {
					result[k] = v
				}
			}
		} else {
			m.logger.Warn("从数据库加载统计信息失败", zap.Error(err))
		}
	}

	// 合并内存中的统计信息
	m.mu.RLock()
	for k, v := range m.stats {
		// 如果数据库中已有该工具的统计信息，合并它们
		if existing, exists := result[k]; exists {
			// 创建新的统计信息对象，避免修改共享对象
			merged := &ToolStats{
				ToolName:     k,
				TotalCalls:   existing.TotalCalls + v.TotalCalls,
				SuccessCalls: existing.SuccessCalls + v.SuccessCalls,
				FailedCalls:  existing.FailedCalls + v.FailedCalls,
			}
			// 使用最新的调用时间
			if v.LastCallTime != nil && (existing.LastCallTime == nil || v.LastCallTime.After(*existing.LastCallTime)) {
				merged.LastCallTime = v.LastCallTime
			} else if existing.LastCallTime != nil {
				timeCopy := *existing.LastCallTime
				merged.LastCallTime = &timeCopy
			}
			result[k] = merged
		} else {
			// 如果数据库中没有，直接使用内存中的统计信息
			statCopy := *v
			result[k] = &statCopy
		}
	}
	m.mu.RUnlock()

	return result
}

// GetToolCount 获取指定外部MCP的工具数量（从缓存读取，不阻塞）
func (m *ExternalMCPManager) GetToolCount(name string) (int, error) {
	// 先从缓存读取
	m.toolCountsMu.RLock()
	if count, exists := m.toolCounts[name]; exists {
		m.toolCountsMu.RUnlock()
		return count, nil
	}
	m.toolCountsMu.RUnlock()

	// 如果缓存中没有，检查客户端状态
	client, exists := m.GetClient(name)
	if !exists {
		return 0, fmt.Errorf("客户端不存在: %s", name)
	}

	if !client.IsConnected() {
		// 未连接，缓存为0
		m.toolCountsMu.Lock()
		m.toolCounts[name] = 0
		m.toolCountsMu.Unlock()
		return 0, nil
	}

	// 如果已连接但缓存中没有，触发异步刷新并返回0（避免阻塞）
	m.triggerToolCountRefresh()
	return 0, nil
}

// GetToolCounts 获取所有外部MCP的工具数量（从缓存读取，不阻塞）
func (m *ExternalMCPManager) GetToolCounts() map[string]int {
	m.toolCountsMu.RLock()
	defer m.toolCountsMu.RUnlock()

	// 返回缓存的副本，避免外部修改
	result := make(map[string]int)
	for k, v := range m.toolCounts {
		result[k] = v
	}
	return result
}

// refreshToolCounts 刷新工具数量缓存（后台异步执行）
// 使用 atomic flag 防止并发堆积：如果上一次刷新尚未完成，本次触发直接跳过。
func (m *ExternalMCPManager) refreshToolCounts() {
	if !m.refreshing.CompareAndSwap(false, true) {
		return // 上一次刷新尚未完成，跳过
	}
	defer m.refreshing.Store(false)

	m.mu.RLock()
	clients := make(map[string]ExternalMCPClient)
	for k, v := range m.clients {
		clients[k] = v
	}
	m.mu.RUnlock()

	newCounts := make(map[string]int)

	// 使用goroutine并发获取每个客户端的工具数量，避免串行阻塞
	type countResult struct {
		name  string
		count int
	}
	resultChan := make(chan countResult, len(clients))

	for name, client := range clients {
		go func(n string, c ExternalMCPClient) {
			if !c.IsConnected() {
				resultChan <- countResult{name: n, count: 0}
				return
			}

			// 缓存仍新鲜时直接复用，避免与 GetAllTools 重复打远程
			if _, fresh := m.getFreshCachedTools(n); fresh {
				m.toolCountsMu.RLock()
				count := m.toolCounts[n]
				m.toolCountsMu.RUnlock()
				resultChan <- countResult{name: n, count: count}
				return
			}

			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			tools, err := m.listToolsDeduped(ctx, n, c)
			cancel()

			if err != nil {
				if !isConnectionDeadError(err) {
					m.logger.Warn("获取外部MCP工具数量失败，请检查连接或服务端 tools/list",
						zap.String("name", n),
						zap.Error(err),
					)
				}
				resultChan <- countResult{name: n, count: -1}
				return
			}

			resultChan <- countResult{name: n, count: len(tools)}
		}(name, client)
	}

	// 收集结果
	m.toolCountsMu.RLock()
	oldCounts := make(map[string]int)
	for k, v := range m.toolCounts {
		oldCounts[k] = v
	}
	m.toolCountsMu.RUnlock()

	for i := 0; i < len(clients); i++ {
		result := <-resultChan
		if result.count >= 0 {
			newCounts[result.name] = result.count
		} else {
			// 获取失败，保留旧值
			if oldCount, exists := oldCounts[result.name]; exists {
				newCounts[result.name] = oldCount
			} else {
				newCounts[result.name] = 0
			}
		}
	}

	// 更新缓存
	m.toolCountsMu.Lock()
	// 更新所有获取到的值
	for name, count := range newCounts {
		m.toolCounts[name] = count
	}
	// 对于未连接的客户端，设置为0
	for name, client := range clients {
		if !client.IsConnected() {
			m.toolCounts[name] = 0
		}
	}
	m.toolCountsMu.Unlock()
}

// refreshToolCache 刷新指定MCP的工具列表缓存
func (m *ExternalMCPManager) refreshToolCache(name string, client ExternalMCPClient) {
	if !client.IsConnected() {
		return
	}
	if client.GetStatus() == "error" {
		m.logger.Debug("跳过刷新工具列表缓存（连接失败）",
			zap.String("name", name),
		)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := m.listToolsDeduped(ctx, name, client); err != nil {
		m.logger.Debug("刷新工具列表缓存失败",
			zap.String("name", name),
			zap.Error(err),
		)
	}
}

// startToolCountRefresh 启动后台刷新工具数量的goroutine
func (m *ExternalMCPManager) startToolCountRefresh() {
	m.refreshWg.Add(1)
	go func() {
		defer m.refreshWg.Done()
		ticker := time.NewTicker(externalToolCountRefreshInterval)
		defer ticker.Stop()

		// 立即执行一次刷新
		m.refreshToolCounts()

		for {
			select {
			case <-ticker.C:
				m.refreshToolCounts()
			case <-m.stopRefresh:
				return
			}
		}
	}()
}

// triggerToolCountRefresh 触发立即刷新工具数量（异步）
func (m *ExternalMCPManager) triggerToolCountRefresh() {
	go m.refreshToolCounts()
}

// createClient 创建客户端（不连接）。统一使用官方 MCP Go SDK 的 lazy 客户端，连接在 Initialize 时完成。
func (m *ExternalMCPManager) createClient(serverCfg config.ExternalMCPServerConfig) ExternalMCPClient {
	transport := serverCfg.GetTransportType()

	switch transport {
	case "http":
		if serverCfg.URL == "" {
			return nil
		}
		return newLazySDKClient(serverCfg, m.logger)
	case "stdio":
		if serverCfg.Command == "" {
			return nil
		}
		return newLazySDKClient(serverCfg, m.logger)
	case "sse":
		if serverCfg.URL == "" {
			return nil
		}
		return newLazySDKClient(serverCfg, m.logger)
	default:
		if transport == "" {
			return nil
		}
		// 未知传输类型也尝试使用 lazy client
		return newLazySDKClient(serverCfg, m.logger)
	}
}

// doConnect 执行实际连接
func (m *ExternalMCPManager) doConnect(name string, serverCfg config.ExternalMCPServerConfig, client ExternalMCPClient) error {
	timeout := time.Duration(serverCfg.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	// 初始化连接
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := client.Initialize(ctx); err != nil {
		return err
	}

	m.logger.Info("外部MCP客户端已连接",
		zap.String("name", name),
	)

	return nil
}

// setClientStatus 设置客户端状态（通过类型断言）
func (m *ExternalMCPManager) setClientStatus(client ExternalMCPClient, status string) {
	if c, ok := client.(*lazySDKClient); ok {
		c.setStatus(status)
	}
}

// connectClient 连接客户端（异步）- 保留用于向后兼容
func (m *ExternalMCPManager) connectClient(name string, serverCfg config.ExternalMCPServerConfig) error {
	client := m.createClient(serverCfg)
	if client == nil {
		return fmt.Errorf("无法创建客户端：不支持的传输模式")
	}

	// 设置状态为connecting
	m.setClientStatus(client, "connecting")

	// 初始化连接
	timeout := time.Duration(serverCfg.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := client.Initialize(ctx); err != nil {
		m.logger.Error("初始化外部MCP客户端失败",
			zap.String("name", name),
			zap.Error(err),
		)
		return err
	}

	// 保存客户端
	m.mu.Lock()
	m.clients[name] = client
	m.mu.Unlock()

	m.logger.Info("外部MCP客户端已连接",
		zap.String("name", name),
	)

	m.onClientConnected(name)

	// 连接成功，触发工具数量刷新和工具列表缓存刷新
	m.triggerToolCountRefresh()
	m.mu.RLock()
	if client, exists := m.clients[name]; exists {
		m.refreshToolCache(name, client)
	}
	m.mu.RUnlock()

	return nil
}

// isEnabled 检查是否启用
func (m *ExternalMCPManager) isEnabled(cfg config.ExternalMCPServerConfig) bool {
	return cfg.ExternalMCPEnable
}

// findSubstring 查找子字符串（简单实现）
func findSubstring(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// StartAllEnabled 启动所有启用的客户端
func (m *ExternalMCPManager) StartAllEnabled() {
	m.mu.RLock()
	configs := make(map[string]config.ExternalMCPServerConfig)
	for k, v := range m.configs {
		configs[k] = v
	}
	m.mu.RUnlock()

	for name, cfg := range configs {
		if m.isEnabled(cfg) {
			go func(n string, c config.ExternalMCPServerConfig) {
				if err := m.connectClient(n, c); err != nil {
					// 检查是否是连接被拒绝的错误（服务可能还没启动）
					errStr := strings.ToLower(err.Error())
					isConnectionRefused := strings.Contains(errStr, "connection refused") ||
						strings.Contains(errStr, "dial tcp") ||
						strings.Contains(errStr, "connect: connection refused")

					if isConnectionRefused {
						// 连接被拒绝，说明目标服务可能还没启动，这是正常的
						// 使用 Warn 级别，提示用户这是正常的，可以通过手动启动或等待服务启动后自动连接
						fields := []zap.Field{
							zap.String("name", n),
							zap.String("message", "目标服务可能尚未启动，这是正常的。服务启动后可通过界面手动连接，或等待自动重试"),
							zap.Error(err),
						}

						transport := c.GetTransportType()

						if transport == "http" && c.URL != "" {
							fields = append(fields, zap.String("url", c.URL))
						} else if transport == "stdio" && c.Command != "" {
							fields = append(fields, zap.String("command", c.Command))
						}

						m.logger.Warn("外部MCP服务暂未就绪", fields...)
					} else {
						// 其他错误，使用 Error 级别
						m.logger.Error("启动外部MCP客户端失败",
							zap.String("name", n),
							zap.Error(err),
						)
					}
				}
			}(name, cfg)
		}
	}
}

// StopAll 停止所有客户端
func (m *ExternalMCPManager) StopAll() {
	if m.executionService != nil {
		m.executionService.CancelAll("外部 MCP 管理器正在停止")
	}
	clients := make(map[string]ExternalMCPClient)
	m.mu.Lock()
	for name, client := range m.clients {
		clients[name] = client
		delete(m.clients, name)
	}
	m.mu.Unlock()

	for name, client := range clients {
		if client != nil {
			_ = client.Close()
		}
		m.clearReconnectState(name)
	}

	// 清理所有工具数量缓存
	m.toolCountsMu.Lock()
	m.toolCounts = make(map[string]int)
	m.toolCountsMu.Unlock()

	// 清理所有工具列表缓存
	m.toolCacheMu.Lock()
	m.toolCache = make(map[string]toolListCacheEntry)
	m.toolCacheMu.Unlock()

	// 停止后台刷新（使用 select 避免重复关闭 channel）
	select {
	case <-m.stopRefresh:
		// 已经关闭，不需要再次关闭
	default:
		close(m.stopRefresh)
	}
	m.refreshWg.Wait()
}

// ExternalCancellationConfirmer is an optional adapter contract for MCP
// servers with server-side cancellation receipts or lease/task status APIs.
// Ordinary notifications/cancelled must never be treated as confirmation.
type ExternalCancellationConfirmer interface {
	ConfirmToolCancellation(context.Context, string, map[string]interface{}) error
}
