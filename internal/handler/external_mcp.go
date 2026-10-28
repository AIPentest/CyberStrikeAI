package handler

import (
	"fmt"
	"net/http"
	"os"
	"sync"

	"cyberstrike-ai/internal/audit"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/security"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// ExternalMCPHandler 外部MCP处理器
type ExternalMCPHandler struct {
	manager    *mcp.ExternalMCPManager
	config     *config.Config
	configPath string
	logger     *zap.Logger
	audit      *audit.Service
	mu         sync.RWMutex
}

// SetAudit wires platform audit logging.
func (h *ExternalMCPHandler) SetAudit(s *audit.Service) {
	h.audit = s
}

// NewExternalMCPHandler 创建外部MCP处理器
func NewExternalMCPHandler(manager *mcp.ExternalMCPManager, cfg *config.Config, configPath string, logger *zap.Logger) *ExternalMCPHandler {
	return &ExternalMCPHandler{
		manager:    manager,
		config:     cfg,
		configPath: configPath,
		logger:     logger,
	}
}

// GetExternalMCPs 获取所有外部MCP配置
func (h *ExternalMCPHandler) GetExternalMCPs(c *gin.Context) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	configs := h.manager.GetConfigs()

	// 获取所有外部MCP的工具数量
	toolCounts := h.manager.GetToolCounts()

	// 转换为响应格式
	result := make(map[string]ExternalMCPResponse)
	for name, cfg := range configs {
		client, exists := h.manager.GetClient(name)
		status := "disconnected"
		if exists {
			status = client.GetStatus()
		} else if h.isEnabled(cfg) {
			status = "disconnected"
		} else {
			status = "disabled"
		}

		toolCount := toolCounts[name]
		errorMsg := externalMCPStatusError(h.manager, name, status)

		result[name] = ExternalMCPResponse{
			Config:    externalMCPConfigForResponse(c, cfg),
			Status:    status,
			ToolCount: toolCount,
			Error:     errorMsg,
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"servers": result,
		"stats":   h.manager.GetStats(),
	})
}

// GetExternalMCP 获取单个外部MCP配置
func (h *ExternalMCPHandler) GetExternalMCP(c *gin.Context) {
	name := c.Param("name")

	h.mu.RLock()
	defer h.mu.RUnlock()

	configs := h.manager.GetConfigs()
	cfg, exists := configs[name]
	if !exists {
		respondError(c, http.StatusNotFound, "外部MCP配置不存在", "external MCP config not found")
		return
	}

	client, clientExists := h.manager.GetClient(name)
	status := "disconnected"
	if clientExists {
		status = client.GetStatus()
	} else if h.isEnabled(cfg) {
		status = "disconnected"
	} else {
		status = "disabled"
	}

	// 获取工具数量
	toolCount := 0
	if clientExists && client.IsConnected() {
		if count, err := h.manager.GetToolCount(name); err == nil {
			toolCount = count
		}
	}

	c.JSON(http.StatusOK, ExternalMCPResponse{
		Config:    externalMCPConfigForResponse(c, cfg),
		Status:    status,
		ToolCount: toolCount,
		Error:     externalMCPStatusError(h.manager, name, status),
	})
}

func externalMCPConfigForResponse(c *gin.Context, cfg config.ExternalMCPServerConfig) config.ExternalMCPServerConfig {
	if security.SessionHasPermission(c, "mcp:write") {
		return cfg
	}
	copyCfg := cfg
	if len(cfg.Env) > 0 {
		copyCfg.Env = make(map[string]string, len(cfg.Env))
		for key := range cfg.Env {
			copyCfg.Env[key] = "***"
		}
	}
	if len(cfg.Headers) > 0 {
		copyCfg.Headers = make(map[string]string, len(cfg.Headers))
		for key := range cfg.Headers {
			copyCfg.Headers[key] = "***"
		}
	}
	return copyCfg
}

// externalMCPStatusError 在 error/disconnected 状态下返回最近错误（含断连原因）。
func externalMCPStatusError(manager *mcp.ExternalMCPManager, name, status string) string {
	if status != "error" && status != "disconnected" {
		return ""
	}
	return manager.GetError(name)
}

// AddOrUpdateExternalMCP 添加或更新外部MCP配置
func (h *ExternalMCPHandler) AddOrUpdateExternalMCP(c *gin.Context) {
	var req AddOrUpdateExternalMCPRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf(i18nText(requestWantsEnglish(c), "无效的请求参数: %s", "invalid request parameter: %s"), err.Error())})
		return
	}

	name := c.Param("name")
	if name == "" {
		respondError(c, http.StatusBadRequest, "名称不能为空", "name must not be empty")
		return
	}

	// 验证配置
	if err := h.validateConfig(req.Config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	// 更新内存中的配置
	if h.config.ExternalMCP.Servers == nil {
		h.config.ExternalMCP.Servers = make(map[string]config.ExternalMCPServerConfig)
	}

	cfg := req.Config

	// Editing metadata does not change activation unless the request says so.
	previous, exists := h.manager.GetConfigs()[name]
	cfg.ExternalMCPEnable = req.activation(previous.ExternalMCPEnable, exists)
	cfg.Disabled = !cfg.ExternalMCPEnable

	// 展开 ${VAR} 环境变量
	config.ExpandConfigEnv(&cfg)

	// 添加或更新配置
	if err := h.manager.AddOrUpdateConfig(name, cfg); err != nil {
		h.logger.Error("failed to add or update external MCP configuration", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf(i18nText(requestWantsEnglish(c), "添加或更新配置失败: %s", "failed to add or update config: %s"), err.Error())})
		return
	}

	h.config.ExternalMCP.Servers[name] = cfg

	// 保存到配置文件
	if err := h.saveConfig(); err != nil {
		h.logger.Error("failed to save configuration", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf(i18nText(requestWantsEnglish(c), "保存配置失败: %s", "failed to save config: %s"), err.Error())})
		return
	}

	h.logger.Info("external MCP configuration updated", zap.String("name", name))
	if h.audit != nil {
		h.audit.Record(c, audit.Entry{
			Category:     "external_mcp",
			Action:       "upsert",
			Result:       "success",
			ResourceType: "external_mcp",
			ResourceID:   name,
			Message:      "updated external MCP configuration",
		})
	}
	c.JSON(http.StatusOK, gin.H{"message": i18nText(requestWantsEnglish(c), "配置已更新", "config updated")})
}

// DeleteExternalMCP 删除外部MCP配置
func (h *ExternalMCPHandler) DeleteExternalMCP(c *gin.Context) {
	name := c.Param("name")

	h.mu.Lock()
	defer h.mu.Unlock()

	// 移除配置
	if err := h.manager.RemoveConfig(name); err != nil {
		respondError(c, http.StatusNotFound, "配置不存在", "config not found")
		return
	}

	// 从内存配置中删除
	if h.config.ExternalMCP.Servers != nil {
		delete(h.config.ExternalMCP.Servers, name)
	}

	// 保存到配置文件
	if err := h.saveConfig(); err != nil {
		h.logger.Error("failed to save configuration", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf(i18nText(requestWantsEnglish(c), "保存配置失败: %s", "failed to save config: %s"), err.Error())})
		return
	}

	h.logger.Info("external MCP configuration deleted", zap.String("name", name))
	if h.audit != nil {
		h.audit.Record(c, audit.Entry{
			Category:     "external_mcp",
			Action:       "delete",
			Result:       "success",
			ResourceType: "external_mcp",
			ResourceID:   name,
			Message:      "deleted external MCP configuration",
		})
	}
	c.JSON(http.StatusOK, gin.H{"message": i18nText(requestWantsEnglish(c), "配置已删除", "config deleted")})
}

// StartExternalMCP 启动外部MCP
func (h *ExternalMCPHandler) StartExternalMCP(c *gin.Context) {
	name := c.Param("name")

	h.mu.Lock()
	defer h.mu.Unlock()

	// 更新配置为启用
	if h.config.ExternalMCP.Servers == nil {
		h.config.ExternalMCP.Servers = make(map[string]config.ExternalMCPServerConfig)
	}
	cfg := h.config.ExternalMCP.Servers[name]
	cfg.ExternalMCPEnable = true
	cfg.Disabled = false
	h.config.ExternalMCP.Servers[name] = cfg

	// 保存到配置文件
	if err := h.saveConfig(); err != nil {
		h.logger.Error("failed to save configuration", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf(i18nText(requestWantsEnglish(c), "保存配置失败: %s", "failed to save config: %s"), err.Error())})
		return
	}

	// 启动客户端（立即创建客户端并设置状态为connecting，实际连接在后台进行）
	h.logger.Info("starting external MCP", zap.String("name", name))
	if err := h.manager.StartClient(name); err != nil {
		h.logger.Error("failed to start external MCP", zap.String("name", name), zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{
			"error":  err.Error(),
			"status": "error",
		})
		return
	}

	// 获取客户端状态（应该是connecting）
	client, exists := h.manager.GetClient(name)
	status := "connecting"
	if exists {
		status = client.GetStatus()
	}

	// 立即返回，不等待连接完成
	// 客户端会在后台异步连接，用户可以通过状态查询接口查看连接状态
	c.JSON(http.StatusOK, gin.H{
		"message": i18nText(requestWantsEnglish(c), "外部MCP启动请求已提交，正在后台连接中", "external MCP start request submitted; connecting in the background"),
		"status":  status,
	})
}

// StopExternalMCP 停止外部MCP
func (h *ExternalMCPHandler) StopExternalMCP(c *gin.Context) {
	name := c.Param("name")

	h.mu.Lock()
	defer h.mu.Unlock()

	// 停止客户端
	if err := h.manager.StopClient(name); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 更新配置
	if h.config.ExternalMCP.Servers == nil {
		h.config.ExternalMCP.Servers = make(map[string]config.ExternalMCPServerConfig)
	}
	cfg := h.config.ExternalMCP.Servers[name]
	cfg.ExternalMCPEnable = false
	cfg.Disabled = true
	h.config.ExternalMCP.Servers[name] = cfg

	// 保存到配置文件
	if err := h.saveConfig(); err != nil {
		h.logger.Error("failed to save configuration", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf(i18nText(requestWantsEnglish(c), "保存配置失败: %s", "failed to save config: %s"), err.Error())})
		return
	}

	h.logger.Info("external MCP stopped", zap.String("name", name))
	c.JSON(http.StatusOK, gin.H{"message": i18nText(requestWantsEnglish(c), "外部MCP已停止", "external MCP stopped")})
}

// GetExternalMCPStats 获取统计信息
func (h *ExternalMCPHandler) GetExternalMCPStats(c *gin.Context) {
	stats := h.manager.GetStats()
	c.JSON(http.StatusOK, stats)
}

// validateConfig 验证配置（同时支持官方 type 字段和旧版 transport 字段）
func (h *ExternalMCPHandler) validateConfig(cfg config.ExternalMCPServerConfig) error {
	transport := cfg.GetTransportType()
	if transport == "" {
		return fmt.Errorf("must specify command (stdio mode) or url + type (http/sse mode)")
	}

	switch transport {
	case "http":
		if cfg.URL == "" {
			return fmt.Errorf("HTTP mode requires url")
		}
	case "stdio":
		if cfg.Command == "" {
			return fmt.Errorf("stdio mode requires command")
		}
	case "sse":
		if cfg.URL == "" {
			return fmt.Errorf("SSE mode requires url")
		}
	default:
		return fmt.Errorf("unsupported transport mode: %s; supported modes: http, stdio, sse", transport)
	}

	return nil
}

// isEnabled 检查是否启用
func (h *ExternalMCPHandler) isEnabled(cfg config.ExternalMCPServerConfig) bool {
	return cfg.ExternalMCPEnable
}

// saveConfig 保存配置到文件
func (h *ExternalMCPHandler) saveConfig() error {
	configFileMu.Lock()
	defer configFileMu.Unlock()
	data, err := os.ReadFile(h.configPath)
	if err != nil {
		return fmt.Errorf("failed to read configuration file: %w", err)
	}

	if err := os.WriteFile(h.configPath+".backup", data, 0644); err != nil {
		h.logger.Warn("failed to create configuration backup", zap.Error(err))
	}

	root, err := loadYAMLDocument(h.configPath)
	if err != nil {
		return fmt.Errorf("failed to parse configuration file: %w", err)
	}

	updateExternalMCPConfig(root, h.config.ExternalMCP)

	if err := writeYAMLDocument(h.configPath, root); err != nil {
		return fmt.Errorf("failed to save configuration file: %w", err)
	}

	h.logger.Info("configuration saved", zap.String("path", h.configPath))
	return nil
}

// updateExternalMCPConfig 更新外部MCP配置
func updateExternalMCPConfig(doc *yaml.Node, cfg config.ExternalMCPConfig) {
	root := doc.Content[0]
	externalMCPNode := ensureMap(root, "external_mcp")
	serversNode := ensureMap(externalMCPNode, "servers")

	// 清空现有服务器配置
	serversNode.Content = nil

	// 添加新的服务器配置
	for name, serverCfg := range cfg.Servers {
		nameNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name}
		serverNode := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		serversNode.Content = append(serversNode.Content, nameNode, serverNode)

		// type（官方 MCP 传输类型）
		effectiveType := serverCfg.GetTransportType()
		if effectiveType != "" && effectiveType != "stdio" {
			// stdio 可省略（有 command 时自动推断）
			setStringInMap(serverNode, "type", effectiveType)
		}
		if serverCfg.Command != "" {
			setStringInMap(serverNode, "command", serverCfg.Command)
		}
		if len(serverCfg.Args) > 0 {
			setStringArrayInMap(serverNode, "args", serverCfg.Args)
		}
		if serverCfg.Env != nil && len(serverCfg.Env) > 0 {
			envNode := ensureMap(serverNode, "env")
			for envKey, envValue := range serverCfg.Env {
				setStringInMap(envNode, envKey, envValue)
			}
		}
		if serverCfg.URL != "" {
			setStringInMap(serverNode, "url", serverCfg.URL)
		}
		if serverCfg.Headers != nil && len(serverCfg.Headers) > 0 {
			headersNode := ensureMap(serverNode, "headers")
			for k, v := range serverCfg.Headers {
				setStringInMap(headersNode, k, v)
			}
		}
		if serverCfg.Description != "" {
			setStringInMap(serverNode, "description", serverCfg.Description)
		}
		if serverCfg.Timeout > 0 {
			setIntInMap(serverNode, "timeout", serverCfg.Timeout)
		}
		// 官方标准字段
		if serverCfg.Disabled {
			setBoolInMap(serverNode, "disabled", true)
		}
		if len(serverCfg.AutoApprove) > 0 {
			setStringArrayInMap(serverNode, "autoApprove", serverCfg.AutoApprove)
		}

		// SDK 高级配置
		if serverCfg.MaxRetries > 0 {
			setIntInMap(serverNode, "max_retries", serverCfg.MaxRetries)
		}
		if serverCfg.TerminateDuration > 0 {
			setIntInMap(serverNode, "terminate_duration", serverCfg.TerminateDuration)
		}
		if serverCfg.KeepAlive > 0 {
			setIntInMap(serverNode, "keep_alive", serverCfg.KeepAlive)
		}

		setBoolInMap(serverNode, "external_mcp_enable", serverCfg.ExternalMCPEnable)
		if serverCfg.ToolEnabled != nil && len(serverCfg.ToolEnabled) > 0 {
			toolEnabledNode := ensureMap(serverNode, "tool_enabled")
			for toolName, enabled := range serverCfg.ToolEnabled {
				setBoolInMap(toolEnabledNode, toolName, enabled)
			}
		}
	}
}

// setStringArrayInMap 设置字符串数组
func setStringArrayInMap(mapNode *yaml.Node, key string, values []string) {
	_, valueNode := ensureKeyValue(mapNode, key)
	valueNode.Kind = yaml.SequenceNode
	valueNode.Tag = "!!seq"
	valueNode.Content = nil
	for _, v := range values {
		itemNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}
		valueNode.Content = append(valueNode.Content, itemNode)
	}
}

// AddOrUpdateExternalMCPRequest 添加或更新外部MCP请求
type AddOrUpdateExternalMCPRequest struct {
	Config   config.ExternalMCPServerConfig `json:"config"`
	enable   *bool
	disabled *bool
}

// ExternalMCPResponse 外部MCP响应
type ExternalMCPResponse struct {
	Config    config.ExternalMCPServerConfig `json:"config"`
	Status    string                         `json:"status"`          // "connected", "disconnected", "disabled", "error", "connecting"
	ToolCount int                            `json:"tool_count"`      // 工具数量
	Error     string                         `json:"error,omitempty"` // 错误信息（仅在status为error时存在）
}
