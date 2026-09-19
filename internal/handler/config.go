package handler

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"cyberstrike-ai/internal/agents"
	"cyberstrike-ai/internal/audit"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/knowledge"
	"cyberstrike-ai/internal/llm"
	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/mcp/builtin"
	"cyberstrike-ai/internal/openai"
	"cyberstrike-ai/internal/security"
	"cyberstrike-ai/internal/toolguard"

	"github.com/cloudwego/eino/schema"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// KnowledgeToolRegistrar 知识库工具注册器接口
type KnowledgeToolRegistrar func() error

// VulnerabilityToolRegistrar 漏洞工具注册器接口
type VulnerabilityToolRegistrar func() error

// WebshellToolRegistrar WebShell 工具注册器接口（ApplyConfig 时重新注册）
type WebshellToolRegistrar func() error

// SkillsToolRegistrar Skills工具注册器接口
type SkillsToolRegistrar func() error

// BatchTaskToolRegistrar 批量任务 MCP 工具注册器（ApplyConfig 时重新注册）
type BatchTaskToolRegistrar func() error

// C2ToolRegistrar C2 MCP 工具注册器（ApplyConfig 时 ClearTools 之后调用）
type C2ToolRegistrar func() error

// C2Runtime ApplyConfig 时按配置启停 C2 子系统（由 internal/app.App 实现）
type C2Runtime interface {
	ReconcileC2AfterConfigApply() error
}

// RetrieverUpdater 检索器更新接口
type RetrieverUpdater interface {
	UpdateConfig(config *knowledge.RetrievalConfig)
}

// KnowledgeInitializer 知识库初始化器接口
type KnowledgeInitializer func() (*KnowledgeHandler, error)

// AppUpdater App更新接口（用于更新App中的知识库组件）
type AppUpdater interface {
	UpdateKnowledgeComponents(handler *KnowledgeHandler, manager interface{}, retriever interface{}, indexer interface{})
}

// RobotRestarter 机器人连接重启器（用于配置应用后重启钉钉/飞书长连接）
type RobotRestarter interface {
	RestartRobotConnections()
}

// ConfigHandler 配置处理器
type ConfigHandler struct {
	configPath                 string
	config                     *config.Config
	mcpServer                  *mcp.Server
	executor                   *security.Executor
	agent                      AgentUpdater               // Agent接口，用于更新Agent配置
	attackChainHandler         AttackChainUpdater         // 攻击链处理器接口，用于更新配置
	externalMCPMgr             *mcp.ExternalMCPManager    // 外部MCP管理器
	knowledgeToolRegistrar     KnowledgeToolRegistrar     // 知识库工具注册器（可选）
	vulnerabilityToolRegistrar VulnerabilityToolRegistrar // 漏洞工具注册器（可选）
	webshellToolRegistrar      WebshellToolRegistrar      // WebShell 工具注册器（可选）
	skillsToolRegistrar        SkillsToolRegistrar        // Skills工具注册器（可选）
	batchTaskToolRegistrar     BatchTaskToolRegistrar     // 批量任务 MCP 工具（可选）
	c2ToolRegistrar            C2ToolRegistrar            // C2 MCP 工具（可选）
	c2Runtime                  C2Runtime                  // C2 启停（可选）
	retrieverUpdater           RetrieverUpdater           // 检索器更新器（可选）
	knowledgeInitializer       KnowledgeInitializer       // 知识库初始化器（可选）
	appUpdater                 AppUpdater                 // App更新器（可选）
	robotRestarter             RobotRestarter             // 机器人连接重启器（可选），ApplyConfig 时重启钉钉/飞书
	audit                      *audit.Service
	db                         *database.DB
	logger                     *zap.Logger
	mu                         sync.RWMutex
	toolGuard                  *toolguard.Manager
	lastEmbeddingConfig        *config.EmbeddingConfig // 上一次的嵌入模型配置（用于检测变更）
}

func (h *ConfigHandler) SetDB(db *database.DB) {
	h.db = db
}

func (h *ConfigHandler) validateRobotServiceAccounts(robots config.RobotsConfig) error {
	if h.db == nil {
		return fmt.Errorf("RBAC 服务不可用，无法校验机器人服务账号")
	}
	for platform, userID := range robots.ServiceAccountUserIDs() {
		user, err := h.db.GetRBACUserByID(userID)
		if err != nil {
			return fmt.Errorf("robots.%s.auth.service_user_id 对应用户不存在", platform)
		}
		if !user.Enabled {
			return fmt.Errorf("robots.%s.auth.service_user_id 对应用户已禁用", platform)
		}
	}
	return nil
}

// AttackChainUpdater 攻击链处理器更新接口
type AttackChainUpdater interface {
	UpdateConfig(cfg *config.OpenAIConfig)
}

// AgentUpdater Agent更新接口
type AgentUpdater interface {
	UpdateConfig(cfg *config.OpenAIConfig)
	UpdateMaxIterations(maxIterations int)
	UpdateToolDescriptionMode(mode string)
}

// NewConfigHandler 创建新的配置处理器
func NewConfigHandler(configPath string, cfg *config.Config, mcpServer *mcp.Server, executor *security.Executor, agent AgentUpdater, attackChainHandler AttackChainUpdater, externalMCPMgr *mcp.ExternalMCPManager, logger *zap.Logger) *ConfigHandler {
	// 保存初始的嵌入模型配置（如果知识库已启用）
	var lastEmbeddingConfig *config.EmbeddingConfig
	if cfg.Knowledge.Enabled {
		lastEmbeddingConfig = &config.EmbeddingConfig{
			Provider: cfg.Knowledge.Embedding.Provider,
			Model:    cfg.Knowledge.Embedding.Model,
			BaseURL:  cfg.Knowledge.Embedding.BaseURL,
			APIKey:   cfg.Knowledge.Embedding.APIKey,
		}
	}
	return &ConfigHandler{
		configPath:          configPath,
		config:              cfg,
		mcpServer:           mcpServer,
		executor:            executor,
		agent:               agent,
		attackChainHandler:  attackChainHandler,
		externalMCPMgr:      externalMCPMgr,
		logger:              logger,
		lastEmbeddingConfig: lastEmbeddingConfig,
	}
}

// SetKnowledgeToolRegistrar 设置知识库工具注册器
func (h *ConfigHandler) SetKnowledgeToolRegistrar(registrar KnowledgeToolRegistrar) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.knowledgeToolRegistrar = registrar
}

// SetVulnerabilityToolRegistrar 设置漏洞工具注册器
func (h *ConfigHandler) SetVulnerabilityToolRegistrar(registrar VulnerabilityToolRegistrar) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.vulnerabilityToolRegistrar = registrar
}

// SetWebshellToolRegistrar 设置 WebShell 工具注册器
func (h *ConfigHandler) SetWebshellToolRegistrar(registrar WebshellToolRegistrar) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.webshellToolRegistrar = registrar
}

// SetSkillsToolRegistrar 设置Skills工具注册器
func (h *ConfigHandler) SetSkillsToolRegistrar(registrar SkillsToolRegistrar) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.skillsToolRegistrar = registrar
}

// SetBatchTaskToolRegistrar 设置批量任务 MCP 工具注册器
func (h *ConfigHandler) SetBatchTaskToolRegistrar(registrar BatchTaskToolRegistrar) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.batchTaskToolRegistrar = registrar
}

// SetC2ToolRegistrar 设置 C2 MCP 工具注册器
func (h *ConfigHandler) SetC2ToolRegistrar(registrar C2ToolRegistrar) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.c2ToolRegistrar = registrar
}

// SetC2Runtime 设置 C2 运行时（Apply 时启停）
func (h *ConfigHandler) SetC2Runtime(rt C2Runtime) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.c2Runtime = rt
}

// SetRetrieverUpdater 设置检索器更新器
func (h *ConfigHandler) SetRetrieverUpdater(updater RetrieverUpdater) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.retrieverUpdater = updater
}

// SetKnowledgeInitializer 设置知识库初始化器
func (h *ConfigHandler) SetKnowledgeInitializer(initializer KnowledgeInitializer) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.knowledgeInitializer = initializer
}

// SetAppUpdater 设置App更新器
func (h *ConfigHandler) SetAppUpdater(updater AppUpdater) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.appUpdater = updater
}

// SetRobotRestarter 设置机器人连接重启器（ApplyConfig 时用于重启钉钉/飞书长连接）
func (h *ConfigHandler) SetRobotRestarter(restarter RobotRestarter) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.robotRestarter = restarter
}

// SetAudit wires platform audit logging.
func (h *ConfigHandler) SetAudit(s *audit.Service) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.audit = s
}

// ApplyWechatRobotBinding 微信 iLink 扫码绑定成功后写入配置并重启机器人连接
func (h *ConfigHandler) ApplyWechatRobotBinding(wc config.RobotWechatConfig) error {
	h.mu.Lock()
	wc.Enabled = true
	h.config.Robots.Wechat = wc
	h.mu.Unlock()
	if err := h.saveConfig(); err != nil {
		return err
	}
	if h.robotRestarter != nil {
		h.robotRestarter.RestartRobotConnections()
	}
	h.logger.Info("微信机器人绑定已保存",
		zap.String("ilink_bot_id", wc.ILinkBotID),
		zap.Bool("enabled", wc.Enabled),
	)
	return nil
}

// GetConfigResponse 获取配置响应
type GetConfigResponse struct {
	AI         config.AIConfig          `json:"ai"`
	OpenAI     config.OpenAIConfig      `json:"openai"`
	Vision     config.VisionConfig      `json:"vision"`
	FOFA       config.FofaConfig        `json:"fofa"`
	ZoomEye    config.SpaceSearchConfig `json:"zoomeye"`
	Quake      config.SpaceSearchConfig `json:"quake"`
	Shodan     config.SpaceSearchConfig `json:"shodan"`
	MCP        config.MCPConfig         `json:"mcp"`
	Tools      []ToolConfigInfo         `json:"tools"`
	Agent      config.AgentConfig       `json:"agent"`
	Hitl       config.HitlConfig        `json:"hitl,omitempty"`
	Knowledge  config.KnowledgeConfig   `json:"knowledge"`
	Robots     config.RobotsConfig      `json:"robots,omitempty"`
	MultiAgent config.MultiAgentPublic  `json:"multi_agent,omitempty"`
	C2         config.C2Public          `json:"c2"`
}

// ToolConfigInfo 工具配置信息
type ToolConfigInfo struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Enabled     bool                   `json:"enabled"`
	IsExternal  bool                   `json:"is_external,omitempty"`  // 是否为外部MCP工具
	ExternalMCP string                 `json:"external_mcp,omitempty"` // 外部MCP名称（如果是外部工具）
	RoleEnabled *bool                  `json:"role_enabled,omitempty"` // 该工具在当前角色中是否启用（nil表示未指定角色或使用所有工具）
	InputSchema map[string]interface{} `json:"input_schema,omitempty"` // 工具参数 JSON Schema（用于前端展示详情）
}

// GetConfig 获取当前配置
func (h *ConfigHandler) GetConfig(c *gin.Context) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	// 获取工具列表（包含内部和外部工具）
	// 首先从配置文件获取工具
	configToolMap := make(map[string]bool)
	tools := make([]ToolConfigInfo, 0, len(h.config.Security.Tools))

	for _, tool := range h.config.Security.Tools {
		configToolMap[tool.Name] = true
		info := ToolConfigInfo{
			Name:        tool.Name,
			Description: h.pickToolDescription(tool.ShortDescription, tool.Description),
			Enabled:     tool.Enabled,
			IsExternal:  false,
		}
		tools = append(tools, info)
	}

	// 从MCP服务器获取所有已注册的工具（包括直接注册的工具，如知识检索工具）
	if h.mcpServer != nil {
		mcpTools := h.mcpServer.GetAllTools()
		for _, mcpTool := range mcpTools {
			if configToolMap[mcpTool.Name] {
				continue
			}
			description := h.pickToolDescription(mcpTool.ShortDescription, mcpTool.Description)
			tools = append(tools, ToolConfigInfo{
				Name:        mcpTool.Name,
				Description: description,
				Enabled:     true,
				IsExternal:  false,
			})
		}
	}

	// 获取外部MCP工具（走缓存，持锁期间通常不阻塞）
	if h.externalMCPMgr != nil {
		ctx := context.Background()
		externalTools := h.getExternalMCPTools(ctx)
		for _, toolInfo := range externalTools {
			tools = append(tools, toolInfo)
		}
	}

	subAgentCount := len(h.config.MultiAgent.SubAgents)
	agentsDir := strings.TrimSpace(h.config.AgentsDir)
	if agentsDir == "" {
		agentsDir = "agents"
	}
	if !filepath.IsAbs(agentsDir) {
		agentsDir = filepath.Join(filepath.Dir(h.configPath), agentsDir)
	}
	if load, err := agents.LoadMarkdownAgentsDir(agentsDir); err == nil {
		subAgentCount = len(agents.MergeYAMLAndMarkdown(h.config.MultiAgent.SubAgents, load.SubAgents))
	}
	multiPub := config.MultiAgentPublic{
		Enabled:                               h.config.MultiAgent.Enabled,
		RobotDefaultAgentMode:                 config.NormalizeRobotAgentMode(h.config.MultiAgent),
		BatchUseMultiAgent:                    h.config.MultiAgent.BatchUseMultiAgent,
		SubAgentCount:                         subAgentCount,
		Orchestration:                         config.NormalizeMultiAgentOrchestration(h.config.MultiAgent.Orchestration),
		PlanExecuteLoopMaxIterations:          h.config.MultiAgent.PlanExecuteLoopMaxIterations,
		SummarizationUserIntentLedgerMaxRunes: h.config.MultiAgent.EinoMiddleware.SummarizationUserIntentLedgerMaxRunesEffective(),
		SummarizationUserIntentLedgerEntryMaxRunes: h.config.MultiAgent.EinoMiddleware.SummarizationUserIntentLedgerEntryMaxRunesEffective(),
		LatestUserMessageMaxRunes:                  h.config.MultiAgent.EinoMiddleware.LatestUserMessageMaxRunesEffective(),
		LatestUserMessageHeadRunes:                 h.config.MultiAgent.EinoMiddleware.LatestUserMessageHeadRunesEffective(),
		LatestUserMessageTailRunes:                 h.config.MultiAgent.EinoMiddleware.LatestUserMessageTailRunesEffective(),
		ModelRetryMaxRetries:                       h.config.MultiAgent.EinoMiddleware.ModelRetryMaxRetries,
		ModelRetryMaxBackoffSec:                    h.config.MultiAgent.EinoMiddleware.ModelRetryMaxBackoffSec,
		ModelFailoverChannels:                      append([]string(nil), h.config.MultiAgent.EinoMiddleware.ModelFailoverChannels...),
		ModelFailoverMaxRetries:                    h.config.MultiAgent.EinoMiddleware.ModelFailoverMaxRetries,
		ToolSearchAlwaysVisibleTools:               append([]string(nil), h.config.MultiAgent.EinoMiddleware.ToolSearchAlwaysVisibleTools...),
		ToolSearchAlwaysVisibleEffectiveTools: mergeToolNameLists(
			h.config.MultiAgent.EinoMiddleware.ToolSearchAlwaysVisibleTools,
			builtin.GetAllBuiltinTools(),
		),
	}

	c.JSON(http.StatusOK, GetConfigResponse{
		AI:         h.config.AI,
		OpenAI:     h.config.OpenAI,
		Vision:     h.config.Vision,
		FOFA:       h.config.FOFA,
		ZoomEye:    h.config.ZoomEye,
		Quake:      h.config.Quake,
		Shodan:     h.config.Shodan,
		MCP:        h.config.MCP,
		Tools:      tools,
		Agent:      h.config.Agent,
		Hitl:       h.config.Hitl,
		Knowledge:  h.config.Knowledge,
		C2:         h.config.C2.Public(),
		Robots:     h.config.Robots,
		MultiAgent: multiPub,
	})
}

// GetToolsResponse 获取工具列表响应（分页）
type GetToolsResponse struct {
	Tools        []ToolConfigInfo `json:"tools"`
	Total        int              `json:"total"`
	TotalEnabled int              `json:"total_enabled"` // 已启用的工具总数
	Page         int              `json:"page"`
	PageSize     int              `json:"page_size"`
	TotalPages   int              `json:"total_pages"`
}

// GetTools 获取工具列表（支持分页和搜索）
func (h *ConfigHandler) GetTools(c *gin.Context) {
	c.Header("Cache-Control", "no-store, no-cache, must-revalidate")

	// 解析分页参数
	page := 1
	pageSize := 20
	if pageStr := c.Query("page"); pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
		}
	}
	if pageSizeStr := c.Query("page_size"); pageSizeStr != "" {
		if ps, err := strconv.Atoi(pageSizeStr); err == nil && ps > 0 && ps <= 100 {
			pageSize = ps
		}
	}

	// 解析搜索参数
	searchTerm := c.Query("search")
	searchTermLower := ""
	if searchTerm != "" {
		searchTermLower = strings.ToLower(searchTerm)
	}

	// 解析状态筛选: tool_filter=on|off（角色弹窗等优先，避免与网关/代理对 enabled 的特殊处理冲突）
	// 兼容旧参数 enabled=true|false
	var filterEnabled *bool
	toolFilter := strings.TrimSpace(strings.ToLower(c.Query("tool_filter")))
	switch toolFilter {
	case "on", "1", "true", "enabled":
		v := true
		filterEnabled = &v
	case "off", "0", "false", "disabled":
		v := false
		filterEnabled = &v
	default:
		enabledFilter := strings.TrimSpace(c.Query("enabled"))
		if enabledFilter == "true" {
			v := true
			filterEnabled = &v
		} else if enabledFilter == "false" {
			v := false
			filterEnabled = &v
		}
	}

	includeExternal := true
	if v := strings.TrimSpace(strings.ToLower(c.Query("include_external"))); v == "0" || v == "false" || v == "no" {
		includeExternal = false
	}
	refreshExternal := false
	if v := strings.TrimSpace(strings.ToLower(c.Query("refresh_external"))); v == "1" || v == "true" || v == "yes" {
		refreshExternal = true
	}

	// 按外部 MCP 名称筛选（MCP 管理页左侧卡片 → 右侧工具列表联动）
	externalMCPFilter := strings.TrimSpace(c.Query("external_mcp"))

	// 快照配置后立即释放锁，避免外部 MCP 网络 IO 阻塞整个配置子系统
	h.mu.RLock()
	securityTools := append([]config.ToolConfig(nil), h.config.Security.Tools...)
	roles := h.config.Roles
	toolDescriptionMode := h.config.Security.ToolDescriptionMode
	mcpServer := h.mcpServer
	externalMCPMgr := h.externalMCPMgr
	h.mu.RUnlock()

	pickDesc := func(shortDesc, fullDesc string) string {
		return pickToolDescriptionWithMode(toolDescriptionMode, shortDesc, fullDesc)
	}

	// 解析角色参数，用于过滤工具并标注启用状态
	roleName := c.Query("role")
	var roleToolsSet map[string]bool // 角色配置的工具集合
	var roleUsesAllTools bool = true // 角色是否使用所有工具（默认角色）
	if roleName != "" && roleName != "默认" && roles != nil {
		if role, exists := roles[roleName]; exists && role.Enabled {
			if len(role.Tools) > 0 {
				// 角色配置了工具列表，只使用这些工具
				roleToolsSet = make(map[string]bool)
				for _, toolKey := range role.Tools {
					roleToolsSet[toolKey] = true
				}
				roleUsesAllTools = false
			}
		}
	}

	// 获取所有内部工具并应用搜索过滤
	configToolMap := make(map[string]bool)
	allTools := make([]ToolConfigInfo, 0, len(securityTools))
	for _, tool := range securityTools {
		configToolMap[tool.Name] = true
		toolInfo := ToolConfigInfo{
			Name:        tool.Name,
			Description: pickDesc(tool.ShortDescription, tool.Description),
			Enabled:     tool.Enabled,
			IsExternal:  false,
		}

		// 根据角色配置标注工具状态
		if roleName != "" {
			if roleUsesAllTools {
				// 角色使用所有工具，标注启用的工具为role_enabled=true
				if tool.Enabled {
					roleEnabled := true
					toolInfo.RoleEnabled = &roleEnabled
				} else {
					roleEnabled := false
					toolInfo.RoleEnabled = &roleEnabled
				}
			} else {
				// 角色配置了工具列表，检查工具是否在列表中
				// 内部工具使用工具名称作为key
				if roleToolsSet[tool.Name] {
					roleEnabled := tool.Enabled // 工具必须在角色列表中且本身启用
					toolInfo.RoleEnabled = &roleEnabled
				} else {
					// 不在角色列表中，标记为false
					roleEnabled := false
					toolInfo.RoleEnabled = &roleEnabled
				}
			}
		}

		// 如果有关键词，进行搜索过滤
		if searchTermLower != "" {
			nameLower := strings.ToLower(toolInfo.Name)
			descLower := strings.ToLower(toolInfo.Description)
			if !strings.Contains(nameLower, searchTermLower) && !strings.Contains(descLower, searchTermLower) {
				continue // 不匹配，跳过
			}
		}

		// 状态筛选
		if filterEnabled != nil && toolInfo.Enabled != *filterEnabled {
			continue
		}

		allTools = append(allTools, toolInfo)
	}

	// 从MCP服务器获取所有已注册的工具（包括直接注册的工具，如知识检索工具）
	if mcpServer != nil {
		mcpTools := mcpServer.GetAllTools()
		for _, mcpTool := range mcpTools {
			// 跳过已经在配置文件中的工具（避免重复）
			if configToolMap[mcpTool.Name] {
				continue
			}

			description := pickDesc(mcpTool.ShortDescription, mcpTool.Description)

			toolInfo := ToolConfigInfo{
				Name:        mcpTool.Name,
				Description: description,
				Enabled:     true,
				IsExternal:  false,
			}

			// 根据角色配置标注工具状态
			if roleName != "" {
				if roleUsesAllTools {
					// 角色使用所有工具，直接注册的工具默认启用
					roleEnabled := true
					toolInfo.RoleEnabled = &roleEnabled
				} else {
					// 角色配置了工具列表，检查工具是否在列表中
					// 内部工具使用工具名称作为key
					if roleToolsSet[mcpTool.Name] {
						roleEnabled := true // 在角色列表中且工具本身启用
						toolInfo.RoleEnabled = &roleEnabled
					} else {
						// 不在角色列表中，标记为false
						roleEnabled := false
						toolInfo.RoleEnabled = &roleEnabled
					}
				}
			}

			// 如果有关键词，进行搜索过滤
			if searchTermLower != "" {
				nameLower := strings.ToLower(toolInfo.Name)
				descLower := strings.ToLower(toolInfo.Description)
				if !strings.Contains(nameLower, searchTermLower) && !strings.Contains(descLower, searchTermLower) {
					continue // 不匹配，跳过
				}
			}

			// 状态筛选
			if filterEnabled != nil && toolInfo.Enabled != *filterEnabled {
				continue
			}

			allTools = append(allTools, toolInfo)
		}
	}

	// 获取外部MCP工具（可走缓存，不持有 config 锁）
	if includeExternal && externalMCPMgr != nil {
		if refreshExternal {
			externalMCPMgr.InvalidateAllToolCaches()
		}
		ctx := context.Background()
		externalTools := h.getExternalMCPToolsWithManager(ctx, externalMCPMgr, pickDesc)

		// 应用搜索过滤和角色配置
		for _, toolInfo := range externalTools {
			// 搜索过滤
			if searchTermLower != "" {
				nameLower := strings.ToLower(toolInfo.Name)
				descLower := strings.ToLower(toolInfo.Description)
				if !strings.Contains(nameLower, searchTermLower) && !strings.Contains(descLower, searchTermLower) {
					continue // 不匹配，跳过
				}
			}

			// 根据角色配置标注工具状态
			if roleName != "" {
				if roleUsesAllTools {
					// 角色使用所有工具，标注启用的工具为role_enabled=true
					roleEnabled := toolInfo.Enabled
					toolInfo.RoleEnabled = &roleEnabled
				} else {
					// 角色配置了工具列表，检查工具是否在列表中
					// 外部工具使用 "mcpName::toolName" 格式作为key
					externalToolKey := fmt.Sprintf("%s::%s", toolInfo.ExternalMCP, toolInfo.Name)
					if roleToolsSet[externalToolKey] {
						roleEnabled := toolInfo.Enabled // 工具必须在角色列表中且本身启用
						toolInfo.RoleEnabled = &roleEnabled
					} else {
						// 不在角色列表中，标记为false
						roleEnabled := false
						toolInfo.RoleEnabled = &roleEnabled
					}
				}
			}

			// 状态筛选
			if filterEnabled != nil && toolInfo.Enabled != *filterEnabled {
				continue
			}

			allTools = append(allTools, toolInfo)
		}
	}

	// 如果角色配置了工具列表，过滤工具（只保留列表中的工具，但保留其他工具并标记为禁用）
	// 注意：这里我们不直接过滤掉工具，而是保留所有工具，但通过 role_enabled 字段标注状态
	// 这样前端可以显示所有工具，并标注哪些工具在当前角色中可用

	if externalMCPFilter != "" {
		filtered := make([]ToolConfigInfo, 0)
		for _, tool := range allTools {
			if tool.IsExternal && tool.ExternalMCP == externalMCPFilter {
				filtered = append(filtered, tool)
			}
		}
		allTools = filtered
	}

	// 统一按名称排序后再分页，避免配置文件中顺序导致「全部」与「仅已启用」前几页看起来完全一致
	sort.SliceStable(allTools, func(i, j int) bool {
		key := func(t ToolConfigInfo) string {
			if t.IsExternal && t.ExternalMCP != "" {
				return strings.ToLower(t.ExternalMCP + "::" + t.Name)
			}
			return strings.ToLower(t.Name)
		}
		return key(allTools[i]) < key(allTools[j])
	})

	total := len(allTools)
	// 统计已启用的工具数（在角色中的启用工具数）
	totalEnabled := 0
	for _, tool := range allTools {
		if tool.RoleEnabled != nil && *tool.RoleEnabled {
			totalEnabled++
		} else if tool.RoleEnabled == nil && tool.Enabled {
			// 如果未指定角色，统计所有启用的工具
			totalEnabled++
		}
	}

	totalPages := (total + pageSize - 1) / pageSize
	if totalPages == 0 {
		totalPages = 1
	}

	// 计算分页范围
	offset := (page - 1) * pageSize
	end := offset + pageSize
	if end > total {
		end = total
	}

	var tools []ToolConfigInfo
	if offset < total {
		tools = allTools[offset:end]
	} else {
		tools = []ToolConfigInfo{}
	}

	c.JSON(http.StatusOK, GetToolsResponse{
		Tools:        tools,
		Total:        total,
		TotalEnabled: totalEnabled,
		Page:         page,
		PageSize:     pageSize,
		TotalPages:   totalPages,
	})
}

// UpdateConfigRequest 更新配置请求
type UpdateConfigRequest struct {
	AI         *config.AIConfig            `json:"ai,omitempty"`
	OpenAI     *config.OpenAIConfig        `json:"openai,omitempty"`
	Vision     *config.VisionConfig        `json:"vision,omitempty"`
	FOFA       *config.FofaConfig          `json:"fofa,omitempty"`
	ZoomEye    *config.SpaceSearchConfig   `json:"zoomeye,omitempty"`
	Quake      *config.SpaceSearchConfig   `json:"quake,omitempty"`
	Shodan     *config.SpaceSearchConfig   `json:"shodan,omitempty"`
	MCP        *config.MCPConfig           `json:"mcp,omitempty"`
	Tools      []ToolEnableStatus          `json:"tools,omitempty"`
	Agent      *AgentConfigUpdate          `json:"agent,omitempty"`
	Hitl       *config.HitlConfig          `json:"hitl,omitempty"`
	Knowledge  *config.KnowledgeConfig     `json:"knowledge,omitempty"`
	Robots     *config.RobotsConfig        `json:"robots,omitempty"`
	MultiAgent *config.MultiAgentAPIUpdate `json:"multi_agent,omitempty"`
	C2         *config.C2APIUpdate         `json:"c2,omitempty"`
	Storage    *config.StorageConfig       `json:"storage,omitempty"`
}

// AgentConfigUpdate 用于 PATCH /api/config 的 agent 段：仅 JSON 中出现的字段（指针非 nil）覆盖内存配置。
// 避免旧版「整包替换 *AgentConfig」时，未传的整型字段被反序列化为 0 误覆盖（例如 tool_timeout_minutes 变成 0）。
type AgentConfigUpdate struct {
	MaxIterations                      *int    `json:"max_iterations,omitempty"`
	ToolTimeoutMinutes                 *int    `json:"tool_timeout_minutes,omitempty"`
	ToolWaitTimeoutSeconds             *int    `json:"tool_wait_timeout_seconds,omitempty"`
	ExternalMCPMaxConcurrentPerServer  *int    `json:"external_mcp_max_concurrent_per_server,omitempty"`
	ExternalMCPMaxConcurrentTotal      *int    `json:"external_mcp_max_concurrent_total,omitempty"`
	ExternalMCPCircuitFailureThreshold *int    `json:"external_mcp_circuit_failure_threshold,omitempty"`
	ExternalMCPCircuitCooldownSeconds  *int    `json:"external_mcp_circuit_cooldown_seconds,omitempty"`
	SystemPromptPath                   *string `json:"system_prompt_path,omitempty"`
}

func applyAgentConfigUpdate(dst *config.AgentConfig, src *AgentConfigUpdate) {
	if dst == nil || src == nil {
		return
	}
	if src.MaxIterations != nil {
		dst.MaxIterations = *src.MaxIterations
	}
	if src.ToolTimeoutMinutes != nil {
		dst.ToolTimeoutMinutes = *src.ToolTimeoutMinutes
	}
	if src.ToolWaitTimeoutSeconds != nil {
		dst.ToolWaitTimeoutSeconds = *src.ToolWaitTimeoutSeconds
	}
	if src.ExternalMCPMaxConcurrentPerServer != nil {
		dst.ExternalMCPMaxConcurrentPerServer = *src.ExternalMCPMaxConcurrentPerServer
	}
	if src.ExternalMCPMaxConcurrentTotal != nil {
		dst.ExternalMCPMaxConcurrentTotal = *src.ExternalMCPMaxConcurrentTotal
	}
	if src.ExternalMCPCircuitFailureThreshold != nil {
		dst.ExternalMCPCircuitFailureThreshold = *src.ExternalMCPCircuitFailureThreshold
	}
	if src.ExternalMCPCircuitCooldownSeconds != nil {
		dst.ExternalMCPCircuitCooldownSeconds = *src.ExternalMCPCircuitCooldownSeconds
	}
	if src.SystemPromptPath != nil {
		dst.SystemPromptPath = *src.SystemPromptPath
	}
}

// ToolEnableStatus 工具启用状态
type ToolEnableStatus struct {
	Name        string `json:"name"`
	Enabled     bool   `json:"enabled"`
	IsExternal  bool   `json:"is_external,omitempty"`  // 是否为外部MCP工具
	ExternalMCP string `json:"external_mcp,omitempty"` // 外部MCP名称（如果是外部工具）
}

// UpdateConfig 更新配置
func (h *ConfigHandler) UpdateConfig(c *gin.Context) {
	var req UpdateConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的请求参数: " + err.Error()})
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	// 更新OpenAI配置
	if req.AI != nil {
		h.config.AI = *req.AI
		h.config.ApplyDefaultAIChannel()
		h.logger.Info("更新 AI 通道配置",
			zap.String("default_channel", h.config.AI.DefaultChannel),
			zap.Int("channels", len(h.config.AI.Channels)),
		)
	}
	if req.OpenAI != nil {
		h.config.OpenAI = *req.OpenAI
		h.config.AI.EnsureDefaultFromOpenAI(h.config.OpenAI)
		if def := config.NormalizeAIChannelID(h.config.AI.DefaultChannel); def != "" {
			h.config.AI.Channels[def] = config.AIChannelFromOpenAI(def, "Default", h.config.OpenAI)
		}
		h.logger.Info("更新OpenAI配置",
			zap.String("base_url", h.config.OpenAI.BaseURL),
			zap.String("model", h.config.OpenAI.Model),
		)
	}

	if req.Vision != nil {
		h.config.Vision = *req.Vision
		h.logger.Info("更新 Vision 配置",
			zap.Bool("enabled", h.config.Vision.Enabled),
			zap.String("model", h.config.Vision.Model),
		)
	}

	// 更新FOFA配置
	if req.FOFA != nil {
		h.config.FOFA = *req.FOFA
		h.logger.Info("更新FOFA配置", zap.String("base_url", h.config.FOFA.BaseURL))
	}
	if req.ZoomEye != nil {
		h.config.ZoomEye = *req.ZoomEye
		h.logger.Info("更新ZoomEye配置", zap.String("base_url", h.config.ZoomEye.BaseURL))
	}
	if req.Quake != nil {
		h.config.Quake = *req.Quake
		h.logger.Info("更新Quake配置", zap.String("base_url", h.config.Quake.BaseURL))
	}
	if req.Shodan != nil {
		h.config.Shodan = *req.Shodan
		h.logger.Info("更新Shodan配置", zap.String("base_url", h.config.Shodan.BaseURL))
	}

	// 更新MCP配置
	if req.MCP != nil {
		h.config.MCP = *req.MCP
		h.logger.Info("更新MCP配置",
			zap.Bool("enabled", h.config.MCP.Enabled),
			zap.String("host", h.config.MCP.Host),
			zap.Int("port", h.config.MCP.Port),
		)
	}

	// 更新Agent配置（按字段合并，避免部分 JSON 把未出现的字段写成 0）
	if req.Agent != nil {
		applyAgentConfigUpdate(&h.config.Agent, req.Agent)
		h.logger.Info("更新Agent配置",
			zap.Int("max_iterations", h.config.Agent.MaxIterations),
			zap.Int("tool_timeout_minutes", h.config.Agent.ToolTimeoutMinutes),
			zap.Int("tool_wait_timeout_seconds", h.config.Agent.ToolWaitTimeoutSeconds),
			zap.Int("external_mcp_max_concurrent_per_server", h.config.Agent.ExternalMCPMaxConcurrentPerServer),
			zap.Int("external_mcp_max_concurrent_total", h.config.Agent.ExternalMCPMaxConcurrentTotal),
			zap.Int("external_mcp_circuit_failure_threshold", h.config.Agent.ExternalMCPCircuitFailureThreshold),
			zap.Int("external_mcp_circuit_cooldown_seconds", h.config.Agent.ExternalMCPCircuitCooldownSeconds),
		)
		if h.agent != nil && req.Agent.MaxIterations != nil {
			h.agent.UpdateMaxIterations(h.config.Agent.MaxIterations)
		}
		if h.executor != nil {
			h.executor.SetToolOutputMaxBytes(h.config.MultiAgent.EinoMiddleware.ReductionMaxLengthForTruncEffective())
			h.executor.SetToolOutputSpillRoot(h.config.MultiAgent.EinoMiddleware.ReductionRootDir)
		}
		if h.mcpServer != nil {
			h.mcpServer.ConfigureHTTPToolCallTimeoutFromAgentMinutes(h.config.Agent.ToolTimeoutMinutes)
			h.mcpServer.ConfigureToolWaitTimeoutSeconds(h.config.Agent.ToolWaitTimeoutSeconds)
			h.mcpServer.ConfigureToolResultMaxBytes(h.config.MultiAgent.EinoMiddleware.ReductionMaxLengthForTruncEffective())
			h.mcpServer.ConfigureToolResultSpillRoot(h.config.MultiAgent.EinoMiddleware.ReductionRootDir)
		}
		if h.externalMCPMgr != nil {
			h.externalMCPMgr.ConfigureToolWaitTimeoutSeconds(h.config.Agent.ToolWaitTimeoutSeconds)
			h.externalMCPMgr.ConfigureToolResultMaxBytes(h.config.MultiAgent.EinoMiddleware.ReductionMaxLengthForTruncEffective())
			h.externalMCPMgr.ConfigureToolResultSpillRoot(h.config.MultiAgent.EinoMiddleware.ReductionRootDir)
			h.externalMCPMgr.ConfigureResilience(mcp.ExternalMCPResilienceConfig{
				MaxConcurrentPerServer:  h.config.Agent.ExternalMCPMaxConcurrentPerServer,
				MaxConcurrentTotal:      h.config.Agent.ExternalMCPMaxConcurrentTotal,
				CircuitFailureThreshold: h.config.Agent.ExternalMCPCircuitFailureThreshold,
				CircuitCooldown:         time.Duration(h.config.Agent.ExternalMCPCircuitCooldownSeconds) * time.Second,
			})
		}
	}

	if req.Hitl != nil {
		h.config.Hitl.AuditModel = req.Hitl.AuditModel
		h.config.Hitl.ToolWhitelist = mergeHitlToolWhitelistSlice(nil, req.Hitl.ToolWhitelist)
		if strings.TrimSpace(req.Hitl.DefaultMode) != "" {
			h.config.Hitl.DefaultMode = req.Hitl.EffectiveDefaultMode()
		}
		h.config.Hitl.DefaultReviewer = req.Hitl.EffectiveDefaultReviewer()
		if req.Hitl.DefaultTimeoutSeconds != nil {
			v := req.Hitl.EffectiveDefaultTimeoutSeconds()
			h.config.Hitl.DefaultTimeoutSeconds = &v
		}
		h.config.Hitl.AuditAgentPrompt = strings.TrimSpace(req.Hitl.AuditAgentPrompt)
		h.config.Hitl.AuditAgentPromptReviewEdit = strings.TrimSpace(req.Hitl.AuditAgentPromptReviewEdit)
		if req.Hitl.RetentionDays != nil {
			v := *req.Hitl.RetentionDays
			if v < 0 {
				v = 0
			}
			h.config.Hitl.RetentionDays = &v
		}
		h.logger.Info("更新HITL配置",
			zap.String("default_reviewer", h.config.Hitl.DefaultReviewer),
			zap.Int("tool_whitelist", len(h.config.Hitl.ToolWhitelist)),
		)
	}

	if req.Storage != nil {
		st := &h.config.Storage
		if req.Storage.AutoClean != nil {
			v := *req.Storage.AutoClean
			st.AutoClean = &v
		}
		if req.Storage.IntervalMinutes != nil {
			v := *req.Storage.IntervalMinutes
			if v < 5 {
				v = 5
			}
			st.IntervalMinutes = &v
		}
		if req.Storage.OrphanGraceDays != nil {
			v := *req.Storage.OrphanGraceDays
			if v < 0 {
				v = 0
			}
			st.OrphanGraceDays = &v
		}
		if req.Storage.ActiveGraceHours != nil {
			v := *req.Storage.ActiveGraceHours
			if v < 1 {
				v = 1
			}
			st.ActiveGraceHours = &v
		}
		if req.Storage.Categories != nil {
			if st.Categories == nil {
				st.Categories = make(map[string]config.StorageCategoryConfig, len(req.Storage.Categories))
			}
			// 只接受注册表内的类别键，未注册的键直接忽略，避免被写进 config.yaml。
			for _, key := range config.StorageCategoryOrder {
				patch, ok := req.Storage.Categories[key]
				if !ok {
					continue
				}
				cur := st.Categories[key]
				if patch.Enabled != nil {
					v := *patch.Enabled
					cur.Enabled = &v
				}
				if patch.RetentionDays != nil {
					v := *patch.RetentionDays
					if v < 0 {
						v = 0
					}
					cur.RetentionDays = &v
				}
				st.Categories[key] = cur
			}
		}
		h.logger.Info("更新运行空间清理配置",
			zap.Bool("auto_clean", st.AutoCleanEffective()),
			zap.Int("interval_minutes", st.IntervalMinutesEffective()),
			zap.Int("orphan_grace_days", st.OrphanGraceDaysEffective()),
			zap.Int("active_grace_hours", st.ActiveGraceHoursEffective()),
		)
	}

	// 更新Knowledge配置
	if req.Knowledge != nil {
		// 保存旧的嵌入模型配置（用于检测变更）
		if h.config.Knowledge.Enabled {
			h.lastEmbeddingConfig = &config.EmbeddingConfig{
				Provider: h.config.Knowledge.Embedding.Provider,
				Model:    h.config.Knowledge.Embedding.Model,
				BaseURL:  h.config.Knowledge.Embedding.BaseURL,
				APIKey:   h.config.Knowledge.Embedding.APIKey,
			}
		}
		h.config.Knowledge = *req.Knowledge
		h.logger.Info("更新Knowledge配置",
			zap.Bool("enabled", h.config.Knowledge.Enabled),
			zap.String("base_path", h.config.Knowledge.BasePath),
			zap.String("embedding_model", h.config.Knowledge.Embedding.Model),
			zap.Int("retrieval_top_k", h.config.Knowledge.Retrieval.TopK),
			zap.Float64("similarity_threshold", h.config.Knowledge.Retrieval.SimilarityThreshold),
		)
	}

	// 更新机器人配置
	if req.Robots != nil {
		if err := config.ValidateWecomConfig(req.Robots.Wecom); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if err := config.ValidateRobotsAuthorization(*req.Robots); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if err := h.validateRobotServiceAccounts(*req.Robots); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		h.config.Robots = *req.Robots
		h.logger.Info("更新机器人配置",
			zap.Bool("wechat_enabled", h.config.Robots.Wechat.Enabled),
			zap.Bool("wecom_enabled", h.config.Robots.Wecom.Enabled),
			zap.Bool("dingtalk_enabled", h.config.Robots.Dingtalk.Enabled),
			zap.Bool("lark_enabled", h.config.Robots.Lark.Enabled),
			zap.Bool("telegram_enabled", h.config.Robots.Telegram.Enabled),
			zap.Bool("slack_enabled", h.config.Robots.Slack.Enabled),
			zap.Bool("discord_enabled", h.config.Robots.Discord.Enabled),
			zap.Bool("qq_enabled", h.config.Robots.QQ.Enabled),
		)
	}

	if req.C2 != nil {
		v := req.C2.Enabled
		h.config.C2.Enabled = &v
		h.logger.Info("更新C2配置", zap.Bool("enabled", v))
	}

	// 多代理标量（sub_agents 等仍由 config.yaml 维护）
	if req.MultiAgent != nil {
		h.config.MultiAgent.Enabled = req.MultiAgent.Enabled
		h.config.MultiAgent.BatchUseMultiAgent = req.MultiAgent.BatchUseMultiAgent
		if mode := strings.TrimSpace(req.MultiAgent.RobotDefaultAgentMode); mode != "" {
			h.config.MultiAgent.RobotDefaultAgentMode = mode
		} else {
			h.config.MultiAgent.RobotDefaultAgentMode = "eino_single"
		}
		if req.MultiAgent.PlanExecuteLoopMaxIterations != nil {
			h.config.MultiAgent.PlanExecuteLoopMaxIterations = *req.MultiAgent.PlanExecuteLoopMaxIterations
		}
		if req.MultiAgent.SummarizationUserIntentLedgerMaxRunes != nil {
			v := *req.MultiAgent.SummarizationUserIntentLedgerMaxRunes
			if v < 0 {
				v = 0
			}
			h.config.MultiAgent.EinoMiddleware.SummarizationUserIntentLedgerMaxRunes = v
		}
		if req.MultiAgent.SummarizationUserIntentLedgerEntryMaxRunes != nil {
			v := *req.MultiAgent.SummarizationUserIntentLedgerEntryMaxRunes
			if v < 0 {
				v = 0
			}
			h.config.MultiAgent.EinoMiddleware.SummarizationUserIntentLedgerEntryMaxRunes = v
		}
		if req.MultiAgent.LatestUserMessageMaxRunes != nil {
			v := *req.MultiAgent.LatestUserMessageMaxRunes
			if v < 0 {
				v = 0
			}
			h.config.MultiAgent.EinoMiddleware.LatestUserMessageMaxRunes = v
		}
		if req.MultiAgent.LatestUserMessageHeadRunes != nil {
			v := *req.MultiAgent.LatestUserMessageHeadRunes
			if v < 0 {
				v = 0
			}
			h.config.MultiAgent.EinoMiddleware.LatestUserMessageHeadRunes = v
		}
		if req.MultiAgent.LatestUserMessageTailRunes != nil {
			v := *req.MultiAgent.LatestUserMessageTailRunes
			if v < 0 {
				v = 0
			}
			h.config.MultiAgent.EinoMiddleware.LatestUserMessageTailRunes = v
		}
		if req.MultiAgent.ModelRetryMaxRetries != nil {
			v := *req.MultiAgent.ModelRetryMaxRetries
			if v < 0 {
				v = 0
			}
			h.config.MultiAgent.EinoMiddleware.ModelRetryMaxRetries = v
		}
		if req.MultiAgent.ModelRetryMaxBackoffSec != nil {
			v := *req.MultiAgent.ModelRetryMaxBackoffSec
			if v < 0 {
				v = 0
			}
			h.config.MultiAgent.EinoMiddleware.ModelRetryMaxBackoffSec = v
		}
		if req.MultiAgent.ModelFailoverChannels != nil {
			h.config.MultiAgent.EinoMiddleware.ModelFailoverChannels = dedupeTrimmedStringList(*req.MultiAgent.ModelFailoverChannels)
		}
		if req.MultiAgent.ModelFailoverMaxRetries != nil {
			v := *req.MultiAgent.ModelFailoverMaxRetries
			if v < 0 {
				v = 0
			}
			h.config.MultiAgent.EinoMiddleware.ModelFailoverMaxRetries = v
		}
		if req.MultiAgent.ToolSearchAlwaysVisibleTools != nil {
			h.config.MultiAgent.EinoMiddleware.ToolSearchAlwaysVisibleTools = dedupeToolNameList(*req.MultiAgent.ToolSearchAlwaysVisibleTools)
		}
		h.logger.Info("更新多代理配置",
			zap.Bool("enabled", h.config.MultiAgent.Enabled),
			zap.String("robot_default_agent_mode", config.NormalizeRobotAgentMode(h.config.MultiAgent)),
			zap.Bool("batch_use_multi_agent", h.config.MultiAgent.BatchUseMultiAgent),
			zap.Int("plan_execute_loop_max_iterations", h.config.MultiAgent.PlanExecuteLoopMaxIterations),
			zap.Int("summarization_user_intent_ledger_max_runes", h.config.MultiAgent.EinoMiddleware.SummarizationUserIntentLedgerMaxRunesEffective()),
			zap.Int("summarization_user_intent_ledger_entry_max_runes", h.config.MultiAgent.EinoMiddleware.SummarizationUserIntentLedgerEntryMaxRunesEffective()),
			zap.Int("latest_user_message_max_runes", h.config.MultiAgent.EinoMiddleware.LatestUserMessageMaxRunesEffective()),
			zap.Int("latest_user_message_head_runes", h.config.MultiAgent.EinoMiddleware.LatestUserMessageHeadRunesEffective()),
			zap.Int("latest_user_message_tail_runes", h.config.MultiAgent.EinoMiddleware.LatestUserMessageTailRunesEffective()),
			zap.Int("model_retry_max_retries", h.config.MultiAgent.EinoMiddleware.ModelRetryMaxRetries),
			zap.Int("model_retry_max_backoff_sec", h.config.MultiAgent.EinoMiddleware.ModelRetryMaxBackoffSec),
			zap.Int("model_failover_channels", len(h.config.MultiAgent.EinoMiddleware.ModelFailoverChannels)),
			zap.Int("model_failover_max_retries", h.config.MultiAgent.EinoMiddleware.ModelFailoverMaxRetries),
			zap.Int("tool_search_always_visible_tools", len(h.config.MultiAgent.EinoMiddleware.ToolSearchAlwaysVisibleTools)),
		)
	}

	// 更新工具启用状态
	if req.Tools != nil {
		// 分离内部工具和外部工具
		internalToolMap := make(map[string]bool)
		// 外部工具状态：MCP名称 -> 工具名称 -> 启用状态
		externalMCPToolMap := make(map[string]map[string]bool)

		for _, toolStatus := range req.Tools {
			if toolStatus.IsExternal && toolStatus.ExternalMCP != "" {
				// 外部工具：保存每个工具的独立状态
				mcpName := toolStatus.ExternalMCP
				if externalMCPToolMap[mcpName] == nil {
					externalMCPToolMap[mcpName] = make(map[string]bool)
				}
				externalMCPToolMap[mcpName][toolStatus.Name] = toolStatus.Enabled
			} else {
				// 内部工具
				internalToolMap[toolStatus.Name] = toolStatus.Enabled
			}
		}

		// 更新内部工具状态
		for i := range h.config.Security.Tools {
			if enabled, ok := internalToolMap[h.config.Security.Tools[i].Name]; ok {
				h.config.Security.Tools[i].Enabled = enabled
				h.logger.Info("更新工具启用状态",
					zap.String("tool", h.config.Security.Tools[i].Name),
					zap.Bool("enabled", enabled),
				)
			}
		}

		// 更新外部MCP工具状态
		if h.externalMCPMgr != nil {
			for mcpName, toolStates := range externalMCPToolMap {
				// 更新配置中的工具启用状态
				if h.config.ExternalMCP.Servers == nil {
					h.config.ExternalMCP.Servers = make(map[string]config.ExternalMCPServerConfig)
				}
				cfg, exists := h.config.ExternalMCP.Servers[mcpName]
				if !exists {
					h.logger.Warn("外部MCP配置不存在", zap.String("mcp", mcpName))
					continue
				}

				// 初始化ToolEnabled map
				if cfg.ToolEnabled == nil {
					cfg.ToolEnabled = make(map[string]bool)
				}

				// 更新每个工具的启用状态
				for toolName, enabled := range toolStates {
					cfg.ToolEnabled[toolName] = enabled
					h.logger.Info("更新外部工具启用状态",
						zap.String("mcp", mcpName),
						zap.String("tool", toolName),
						zap.Bool("enabled", enabled),
					)
				}

				// 检查是否有任何工具启用，如果有则启用MCP
				hasEnabledTool := false
				for _, enabled := range cfg.ToolEnabled {
					if enabled {
						hasEnabledTool = true
						break
					}
				}

				// 如果MCP之前未启用，但现在有工具启用，则启用MCP
				// 如果MCP之前已启用，保持启用状态（允许部分工具禁用）
				if !cfg.ExternalMCPEnable && hasEnabledTool {
					cfg.ExternalMCPEnable = true
					h.logger.Info("自动启用外部MCP（因为有工具启用）", zap.String("mcp", mcpName))
				}

				h.config.ExternalMCP.Servers[mcpName] = cfg
			}

			// 同步更新 externalMCPMgr 中的配置，确保 GetConfigs() 返回最新配置
			// 在循环外部统一更新，避免重复调用
			h.externalMCPMgr.LoadConfigs(&h.config.ExternalMCP)

			// 处理MCP连接状态（异步启动，避免阻塞）
			for mcpName := range externalMCPToolMap {
				cfg := h.config.ExternalMCP.Servers[mcpName]
				// 如果MCP需要启用，确保客户端已启动
				if cfg.ExternalMCPEnable {
					// 启动外部MCP（如果未启动）- 异步执行，避免阻塞
					client, exists := h.externalMCPMgr.GetClient(mcpName)
					if !exists || !client.IsConnected() {
						go func(name string) {
							if err := h.externalMCPMgr.StartClient(name); err != nil {
								h.logger.Warn("启动外部MCP失败",
									zap.String("mcp", name),
									zap.Error(err),
								)
							} else {
								h.logger.Info("启动外部MCP",
									zap.String("mcp", name),
								)
							}
						}(mcpName)
					}
				}
			}
		}
	}

	h.config.NormalizeAIProviderProfiles()

	// 保存配置到文件
	if err := h.saveConfig(); err != nil {
		h.logger.Error("保存配置失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存配置失败: " + err.Error()})
		return
	}

	if h.audit != nil {
		h.audit.RecordOK(c, "config", "update", "更新内存配置", "config", "", nil)
	}
	c.JSON(http.StatusOK, gin.H{"message": "配置已更新"})
}

// TestOpenAIRequest 测试OpenAI连接请求
type TestOpenAIRequest struct {
	Provider string `json:"provider"`
	BaseURL  string `json:"base_url"`
	APIKey   string `json:"api_key"`
	Model    string `json:"model"`
}

// TestOpenAI 测试OpenAI API连接是否可用
func (h *ConfigHandler) TestOpenAI(c *gin.Context) {
	var req TestOpenAIRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的请求参数: " + err.Error()})
		return
	}

	if strings.TrimSpace(req.APIKey) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "API Key 不能为空"})
		return
	}
	if strings.TrimSpace(req.Model) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "模型不能为空"})
		return
	}

	baseURL := strings.TrimSuffix(strings.TrimSpace(req.BaseURL), "/")
	if baseURL == "" {
		if strings.EqualFold(strings.TrimSpace(req.Provider), "claude") {
			baseURL = "https://api.anthropic.com"
		} else {
			baseURL = "https://api.openai.com/v1"
		}
	}

	// 构造一个最小的 chat completion 请求
	payload := map[string]interface{}{
		"model": req.Model,
		"messages": []map[string]string{
			{"role": "user", "content": "Hi"},
		},
		"max_completion_tokens": 5,
	}

	// OpenAI-compatible 通道使用内部客户端；Claude 通道在下方直接使用 Eino agenticclaude。
	tmpCfg := &config.OpenAIConfig{
		Provider: req.Provider,
		BaseURL:  baseURL,
		APIKey:   strings.TrimSpace(req.APIKey),
		Model:    req.Model,
	}
	client := openai.NewClient(tmpCfg, nil, h.logger)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	start := time.Now()
	if llm.IsClaudeProvider(req.Provider) {
		nativeModel, err := llm.NewClaudeAgenticModel(ctx, *tmpCfg, nil, 5, nil)
		if err == nil {
			_, err = nativeModel.Generate(ctx, []*schema.AgenticMessage{
				schema.UserAgenticMessage("Hi"),
			})
		}
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"error":   "连接失败: " + err.Error(),
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"success":    true,
			"model":      tmpCfg.Model,
			"latency_ms": time.Since(start).Milliseconds(),
		})
		return
	}

	var chatResp struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	err := client.ChatCompletion(ctx, payload, &chatResp)
	latency := time.Since(start)

	if err != nil {
		if apiErr, ok := err.(*openai.APIError); ok {
			c.JSON(http.StatusOK, gin.H{
				"success":     false,
				"error":       fmt.Sprintf("API 返回错误 (HTTP %d): %s", apiErr.StatusCode, apiErr.Body),
				"status_code": apiErr.StatusCode,
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"error":   "连接失败: " + err.Error(),
		})
		return
	}

	// 严格校验：必须包含 choices 且有 assistant 回复
	if len(chatResp.Choices) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"error":   "API 响应缺少 choices 字段，请检查 Base URL 路径是否正确",
		})
		return
	}
	if chatResp.ID == "" && chatResp.Model == "" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"error":   "API 响应格式不符合预期，请检查 Base URL 是否正确",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"model":      chatResp.Model,
		"latency_ms": latency.Milliseconds(),
	})
}

// ListModelsRequest 获取模型列表请求（OpenAI 兼容 GET /models）。
type ListModelsRequest struct {
	Provider string `json:"provider"`
	BaseURL  string `json:"base_url"`
	APIKey   string `json:"api_key"`
}

// ListModels 代理调用上游 GET /models，返回可用模型 id 列表。
func (h *ConfigHandler) ListModels(c *gin.Context) {
	var req ListModelsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的请求参数: " + err.Error()})
		return
	}

	provider := strings.TrimSpace(req.Provider)
	if provider == "" {
		provider = "openai"
	}
	if strings.EqualFold(provider, "claude") {
		c.JSON(http.StatusOK, gin.H{
			"success":   false,
			"supported": false,
			"error":     "Claude (Anthropic Messages API) 不支持自动获取模型列表，请手动填写",
		})
		return
	}

	if strings.TrimSpace(req.APIKey) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "API Key 不能为空"})
		return
	}

	baseURL := strings.TrimSuffix(strings.TrimSpace(req.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	tmpCfg := &config.OpenAIConfig{
		Provider: provider,
		BaseURL:  baseURL,
		APIKey:   strings.TrimSpace(req.APIKey),
	}
	client := openai.NewClient(tmpCfg, nil, h.logger)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	models, err := client.ListModels(ctx)
	if err != nil {
		if apiErr, ok := err.(*openai.APIError); ok {
			c.JSON(http.StatusOK, gin.H{
				"success":   false,
				"supported": true,
				"error":     fmt.Sprintf("API 返回错误 (HTTP %d): %s", apiErr.StatusCode, apiErr.Body),
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"success":   false,
			"supported": true,
			"error":     err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":   true,
		"supported": true,
		"models":    models,
		"count":     len(models),
	})
}

// TestVisionRequest 测试 Vision 模型连接；vision.api_key/base_url 留空时可传 openai 段作回退。
type TestVisionRequest struct {
	Vision config.VisionConfig `json:"vision"`
	OpenAI config.OpenAIConfig `json:"openai,omitempty"`
}

// TestVision 测试视觉模型 API 连接（最小 chat completion）。
func (h *ConfigHandler) TestVision(c *gin.Context) {
	var req TestVisionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的请求参数: " + err.Error()})
		return
	}
	oa := req.Vision.OpenAICfgEffective(req.OpenAI)
	if strings.TrimSpace(oa.APIKey) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "API Key 不能为空（可填写 vision.api_key 或 openai.api_key）"})
		return
	}
	if strings.TrimSpace(oa.Model) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "视觉模型不能为空"})
		return
	}

	baseURL := strings.TrimSuffix(strings.TrimSpace(oa.BaseURL), "/")
	if baseURL == "" {
		if strings.EqualFold(strings.TrimSpace(oa.Provider), "claude") {
			baseURL = "https://api.anthropic.com"
		} else {
			baseURL = "https://api.openai.com/v1"
		}
	}

	payload := map[string]interface{}{
		"model": oa.Model,
		"messages": []map[string]string{
			{"role": "user", "content": "Hi"},
		},
		"max_completion_tokens": 5,
	}

	tmpCfg := &config.OpenAIConfig{
		Provider: oa.Provider,
		BaseURL:  baseURL,
		APIKey:   strings.TrimSpace(oa.APIKey),
		Model:    oa.Model,
	}
	client := openai.NewClient(tmpCfg, nil, h.logger)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	start := time.Now()
	var chatResp struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	err := client.ChatCompletion(ctx, payload, &chatResp)
	latency := time.Since(start)

	if err != nil {
		if apiErr, ok := err.(*openai.APIError); ok {
			c.JSON(http.StatusOK, gin.H{
				"success":     false,
				"error":       fmt.Sprintf("API 返回错误 (HTTP %d): %s", apiErr.StatusCode, apiErr.Body),
				"status_code": apiErr.StatusCode,
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"error":   "连接失败: " + err.Error(),
		})
		return
	}
	if len(chatResp.Choices) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"error":   "API 响应缺少 choices 字段，请检查 Base URL 与视觉模型名称",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"model":      chatResp.Model,
		"latency_ms": latency.Milliseconds(),
	})
}

// ApplyConfig 应用配置（重新加载并重启相关服务）
func (h *ConfigHandler) ApplyConfig(c *gin.Context) {
	// 先检查是否需要动态初始化知识库（在锁外执行，避免阻塞其他请求）
	var needInitKnowledge bool
	var knowledgeInitializer KnowledgeInitializer

	h.mu.RLock()
	needInitKnowledge = h.config.Knowledge.Enabled && h.knowledgeToolRegistrar == nil && h.knowledgeInitializer != nil
	if needInitKnowledge {
		knowledgeInitializer = h.knowledgeInitializer
	}
	h.mu.RUnlock()

	// 如果需要动态初始化知识库，在锁外执行（这是耗时操作）
	if needInitKnowledge {
		h.logger.Info("检测到知识库从禁用变为启用，开始动态初始化知识库组件")
		if _, err := knowledgeInitializer(); err != nil {
			h.logger.Error("动态初始化知识库失败", zap.Error(err))
			if h.audit != nil {
				h.audit.RecordFail(c, "config", "apply", "应用配置失败：初始化知识库", map[string]interface{}{"error": err.Error()})
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "初始化知识库失败: " + err.Error()})
			return
		}
		h.logger.Debug("知识库动态初始化完成，工具已注册")
	}

	// 检查嵌入模型配置是否变更（需要在锁外执行，避免阻塞）
	var needReinitKnowledge bool
	var reinitKnowledgeInitializer KnowledgeInitializer
	h.mu.RLock()
	if h.config.Knowledge.Enabled && h.knowledgeInitializer != nil && h.lastEmbeddingConfig != nil {
		// 检查嵌入模型配置是否变更
		currentEmbedding := h.config.Knowledge.Embedding
		if currentEmbedding.Provider != h.lastEmbeddingConfig.Provider ||
			currentEmbedding.Model != h.lastEmbeddingConfig.Model ||
			currentEmbedding.BaseURL != h.lastEmbeddingConfig.BaseURL ||
			currentEmbedding.APIKey != h.lastEmbeddingConfig.APIKey {
			needReinitKnowledge = true
			reinitKnowledgeInitializer = h.knowledgeInitializer
			h.logger.Info("检测到嵌入模型配置变更，需要重新初始化知识库组件",
				zap.String("old_model", h.lastEmbeddingConfig.Model),
				zap.String("new_model", currentEmbedding.Model),
				zap.String("old_base_url", h.lastEmbeddingConfig.BaseURL),
				zap.String("new_base_url", currentEmbedding.BaseURL),
			)
		}
	}
	h.mu.RUnlock()

	// 如果需要重新初始化知识库（嵌入模型配置变更），在锁外执行
	if needReinitKnowledge {
		h.logger.Info("开始重新初始化知识库组件（嵌入模型配置已变更）")
		if _, err := reinitKnowledgeInitializer(); err != nil {
			h.logger.Error("重新初始化知识库失败", zap.Error(err))
			if h.audit != nil {
				h.audit.RecordFail(c, "config", "apply", "应用配置失败：重新初始化知识库", map[string]interface{}{"error": err.Error()})
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "重新初始化知识库失败: " + err.Error()})
			return
		}
		h.logger.Info("知识库组件重新初始化完成")
	}

	// C2：在 ClearTools 之前按配置启停（随后由 c2ToolRegistrar 注册 MCP 工具）
	h.mu.RLock()
	c2Rt := h.c2Runtime
	h.mu.RUnlock()
	if c2Rt != nil {
		if err := c2Rt.ReconcileC2AfterConfigApply(); err != nil {
			h.logger.Error("C2 配置应用失败", zap.Error(err))
			if h.audit != nil {
				h.audit.RecordFail(c, "config", "apply", "应用配置失败：C2", map[string]interface{}{"error": err.Error()})
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "C2 启动失败: " + err.Error()})
			return
		}
	}

	// 现在获取写锁，执行快速的操作
	h.mu.Lock()
	defer h.mu.Unlock()

	// 如果重新初始化了知识库，更新嵌入模型配置记录
	if needReinitKnowledge && h.config.Knowledge.Enabled {
		h.lastEmbeddingConfig = &config.EmbeddingConfig{
			Provider: h.config.Knowledge.Embedding.Provider,
			Model:    h.config.Knowledge.Embedding.Model,
			BaseURL:  h.config.Knowledge.Embedding.BaseURL,
			APIKey:   h.config.Knowledge.Embedding.APIKey,
		}
		h.logger.Info("已更新嵌入模型配置记录")
	}

	// 从 tools 目录重新加载工具配置（新增/修改/删除 yaml 后无需重启）
	if err := config.ReloadSecurityToolsFromDir(h.config, h.configPath); err != nil {
		h.logger.Error("重新加载工具配置失败", zap.Error(err))
		if h.audit != nil {
			h.audit.RecordFail(c, "config", "apply", "应用配置失败：重新加载工具", map[string]interface{}{"error": err.Error()})
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "重新加载工具配置失败: " + err.Error()})
		return
	}
	h.logger.Debug("已从 tools 目录重新加载工具配置", zap.Int("tools_count", len(h.config.Security.Tools)))

	// 重新注册工具（根据新的启用状态）
	h.logger.Debug("重新注册工具")

	// 清空MCP服务器中的工具
	h.mcpServer.ClearTools()

	// 重新注册安全工具
	h.executor.SetToolOutputMaxBytes(h.config.MultiAgent.EinoMiddleware.ReductionMaxLengthForTruncEffective())
	h.executor.SetToolOutputSpillRoot(h.config.MultiAgent.EinoMiddleware.ReductionRootDir)
	h.executor.RegisterTools(h.mcpServer)
	mcp.RegisterExecutionControlTools(h.mcpServer, h.externalMCPMgr)

	// 重新注册漏洞记录工具（内置工具，必须注册）
	if h.vulnerabilityToolRegistrar != nil {
		h.logger.Info("重新注册漏洞记录工具")
		if err := h.vulnerabilityToolRegistrar(); err != nil {
			h.logger.Error("重新注册漏洞记录工具失败", zap.Error(err))
		} else {
			h.logger.Info("漏洞记录工具已重新注册")
		}
	}

	// 重新注册 WebShell 工具（内置工具，必须注册）
	if h.webshellToolRegistrar != nil {
		h.logger.Info("重新注册 WebShell 工具")
		if err := h.webshellToolRegistrar(); err != nil {
			h.logger.Error("重新注册 WebShell 工具失败", zap.Error(err))
		} else {
			h.logger.Info("WebShell 工具已重新注册")
		}
	}

	// 重新注册Skills工具（内置工具，必须注册）
	if h.skillsToolRegistrar != nil {
		h.logger.Info("重新注册Skills工具")
		if err := h.skillsToolRegistrar(); err != nil {
			h.logger.Error("重新注册Skills工具失败", zap.Error(err))
		} else {
			h.logger.Info("Skills工具已重新注册")
		}
	}

	// 重新注册批量任务 MCP 工具
	if h.batchTaskToolRegistrar != nil {
		h.logger.Info("重新注册批量任务 MCP 工具")
		if err := h.batchTaskToolRegistrar(); err != nil {
			h.logger.Error("重新注册批量任务 MCP 工具失败", zap.Error(err))
		} else {
			h.logger.Info("批量任务 MCP 工具已重新注册")
		}
	}

	// 重新注册 C2 MCP 工具（仅当 C2 已启动）
	if h.c2ToolRegistrar != nil {
		h.logger.Info("重新注册 C2 MCP 工具")
		if err := h.c2ToolRegistrar(); err != nil {
			h.logger.Error("重新注册 C2 MCP 工具失败", zap.Error(err))
		} else {
			h.logger.Info("C2 MCP 工具已处理")
		}
	}

	// 如果知识库启用，重新注册知识库工具
	if h.config.Knowledge.Enabled && h.knowledgeToolRegistrar != nil {
		h.logger.Info("重新注册知识库工具")
		if err := h.knowledgeToolRegistrar(); err != nil {
			h.logger.Error("重新注册知识库工具失败", zap.Error(err))
		} else {
			h.logger.Info("知识库工具已重新注册")
		}
	}

	// 更新Agent的OpenAI配置
	if h.agent != nil {
		h.agent.UpdateConfig(&h.config.OpenAI)
		h.agent.UpdateMaxIterations(h.config.Agent.MaxIterations)
		h.agent.UpdateToolDescriptionMode(h.config.Security.ToolDescriptionMode)
		h.logger.Info("Agent配置已更新")
	}
	if h.mcpServer != nil {
		h.mcpServer.ConfigureHTTPToolCallTimeoutFromAgentMinutes(h.config.Agent.ToolTimeoutMinutes)
		h.mcpServer.ConfigureToolWaitTimeoutSeconds(h.config.Agent.ToolWaitTimeoutSeconds)
		h.mcpServer.ConfigureToolResultMaxBytes(h.config.MultiAgent.EinoMiddleware.ReductionMaxLengthForTruncEffective())
		h.mcpServer.ConfigureToolResultSpillRoot(h.config.MultiAgent.EinoMiddleware.ReductionRootDir)
	}
	if h.executor != nil {
		h.executor.SetToolOutputMaxBytes(h.config.MultiAgent.EinoMiddleware.ReductionMaxLengthForTruncEffective())
		h.executor.SetToolOutputSpillRoot(h.config.MultiAgent.EinoMiddleware.ReductionRootDir)
	}
	if h.externalMCPMgr != nil {
		h.externalMCPMgr.ConfigureToolWaitTimeoutSeconds(h.config.Agent.ToolWaitTimeoutSeconds)
		h.externalMCPMgr.ConfigureToolResultMaxBytes(h.config.MultiAgent.EinoMiddleware.ReductionMaxLengthForTruncEffective())
		h.externalMCPMgr.ConfigureToolResultSpillRoot(h.config.MultiAgent.EinoMiddleware.ReductionRootDir)
		h.externalMCPMgr.ConfigureResilience(mcp.ExternalMCPResilienceConfig{
			MaxConcurrentPerServer:  h.config.Agent.ExternalMCPMaxConcurrentPerServer,
			MaxConcurrentTotal:      h.config.Agent.ExternalMCPMaxConcurrentTotal,
			CircuitFailureThreshold: h.config.Agent.ExternalMCPCircuitFailureThreshold,
			CircuitCooldown:         time.Duration(h.config.Agent.ExternalMCPCircuitCooldownSeconds) * time.Second,
		})
	}

	// 更新AttackChainHandler的OpenAI配置
	if h.attackChainHandler != nil {
		h.attackChainHandler.UpdateConfig(&h.config.OpenAI)
		h.logger.Info("AttackChainHandler配置已更新")
	}

	// 更新检索器配置（如果知识库启用）
	if h.config.Knowledge.Enabled && h.retrieverUpdater != nil {
		retrievalConfig := knowledge.RetrievalConfigFromYAML(h.config.Knowledge.Retrieval)
		h.retrieverUpdater.UpdateConfig(retrievalConfig)
		h.logger.Info("检索器配置已更新",
			zap.Int("top_k", retrievalConfig.TopK),
			zap.Float64("similarity_threshold", retrievalConfig.SimilarityThreshold),
		)
	}

	// 更新嵌入模型配置记录（如果知识库启用）
	if h.config.Knowledge.Enabled {
		h.lastEmbeddingConfig = &config.EmbeddingConfig{
			Provider: h.config.Knowledge.Embedding.Provider,
			Model:    h.config.Knowledge.Embedding.Model,
			BaseURL:  h.config.Knowledge.Embedding.BaseURL,
			APIKey:   h.config.Knowledge.Embedding.APIKey,
		}
	}

	// 重启钉钉/飞书长连接，使前端修改的机器人配置立即生效（无需重启服务）
	if h.robotRestarter != nil {
		h.robotRestarter.RestartRobotConnections()
		h.logger.Info("已触发机器人连接重启（钉钉/飞书）")
	}

	h.logger.Info("配置已应用",
		zap.Int("tools_count", len(h.config.Security.Tools)),
	)

	if h.audit != nil {
		h.audit.Record(c, audit.Entry{
			Category: "config",
			Action:   "apply",
			Result:   "success",
			Message:  "配置已应用",
			Detail: map[string]interface{}{
				"tools_count":       len(h.config.Security.Tools),
				"knowledge_enabled": h.config.Knowledge.Enabled,
				"c2_enabled":        h.config.C2.EnabledEffective(),
			},
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"message":     "配置已应用",
		"tools_count": len(h.config.Security.Tools),
	})
}

// saveConfig 保存配置到文件
func (h *ConfigHandler) saveConfig() error {
	configFileMu.Lock()
	defer configFileMu.Unlock()
	h.config.NormalizeAIProviderProfiles()

	// 读取现有配置文件并创建备份
	data, err := os.ReadFile(h.configPath)
	if err != nil {
		return fmt.Errorf("读取配置文件失败: %w", err)
	}

	if err := os.WriteFile(h.configPath+".backup", data, 0644); err != nil {
		h.logger.Warn("创建配置备份失败", zap.Error(err))
	}

	root, err := loadYAMLDocument(h.configPath)
	if err != nil {
		return fmt.Errorf("解析配置文件失败: %w", err)
	}

	updateAgentConfig(root, h.config.Agent)
	updateMCPConfig(root, h.config.MCP)
	updateAIConfig(root, h.config.AI)
	removeKeyFromMap(root.Content[0], "openai")
	updateVisionConfig(root, h.config.Vision)
	updateFOFAConfig(root, h.config.FOFA)
	updateSpaceSearchConfig(root, "zoomeye", h.config.ZoomEye)
	updateSpaceSearchConfig(root, "quake", h.config.Quake)
	updateSpaceSearchConfig(root, "shodan", h.config.Shodan)
	updateKnowledgeConfig(root, h.config.Knowledge)
	updateC2Config(root, h.config.C2)
	updateRobotsConfig(root, h.config.Robots)
	updateHitlConfig(root, h.config.Hitl)
	updateStorageConfig(root, h.config.Storage)
	updateMultiAgentConfig(root, h.config.MultiAgent)
	// 更新外部MCP配置（使用external_mcp.go中的函数，同一包中可直接调用）
	updateExternalMCPConfig(root, h.config.ExternalMCP)

	if err := writeYAMLDocument(h.configPath, root); err != nil {
		return fmt.Errorf("保存配置文件失败: %w", err)
	}

	// 更新工具配置文件中的enabled状态
	if h.config.Security.ToolsDir != "" {
		configDir := filepath.Dir(h.configPath)
		toolsDir := h.config.Security.ToolsDir
		if !filepath.IsAbs(toolsDir) {
			toolsDir = filepath.Join(configDir, toolsDir)
		}

		for _, tool := range h.config.Security.Tools {
			toolFile := filepath.Join(toolsDir, tool.Name+".yaml")
			// 检查文件是否存在
			if _, err := os.Stat(toolFile); os.IsNotExist(err) {
				// 尝试.yml扩展名
				toolFile = filepath.Join(toolsDir, tool.Name+".yml")
				if _, err := os.Stat(toolFile); os.IsNotExist(err) {
					h.logger.Warn("工具配置文件不存在", zap.String("tool", tool.Name))
					continue
				}
			}

			toolDoc, err := loadYAMLDocument(toolFile)
			if err != nil {
				h.logger.Warn("解析工具配置失败", zap.String("tool", tool.Name), zap.Error(err))
				continue
			}

			setBoolInMap(toolDoc.Content[0], "enabled", tool.Enabled)

			if err := writeYAMLDocument(toolFile, toolDoc); err != nil {
				h.logger.Warn("保存工具配置文件失败", zap.String("tool", tool.Name), zap.Error(err))
				continue
			}

			h.logger.Info("更新工具配置", zap.String("tool", tool.Name), zap.Bool("enabled", tool.Enabled))
		}
	}

	h.logger.Info("配置已保存", zap.String("path", h.configPath))
	return nil
}

func loadYAMLDocument(path string) (*yaml.Node, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	if len(bytes.TrimSpace(data)) == 0 {
		return newEmptyYAMLDocument(), nil
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}

	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return newEmptyYAMLDocument(), nil
	}

	if doc.Content[0].Kind != yaml.MappingNode {
		root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		doc.Content = []*yaml.Node{root}
	}

	return &doc, nil
}

func newEmptyYAMLDocument() *yaml.Node {
	root := &yaml.Node{
		Kind:    yaml.DocumentNode,
		Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}},
	}
	return root
}

func writeYAMLDocument(path string, doc *yaml.Node) error {
	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(doc); err != nil {
		return err
	}
	if err := encoder.Close(); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0644)
}

func updateAgentConfig(doc *yaml.Node, agent config.AgentConfig) {
	root := doc.Content[0]
	agentNode := ensureMap(root, "agent")
	setIntInMap(agentNode, "max_iterations", agent.MaxIterations)
	setIntInMap(agentNode, "tool_timeout_minutes", agent.ToolTimeoutMinutes)
	setIntInMap(agentNode, "tool_wait_timeout_seconds", agent.ToolWaitTimeoutSeconds)
	setIntInMap(agentNode, "external_mcp_max_concurrent_per_server", agent.ExternalMCPMaxConcurrentPerServer)
	setIntInMap(agentNode, "external_mcp_max_concurrent_total", agent.ExternalMCPMaxConcurrentTotal)
	setIntInMap(agentNode, "external_mcp_circuit_failure_threshold", agent.ExternalMCPCircuitFailureThreshold)
	setIntInMap(agentNode, "external_mcp_circuit_cooldown_seconds", agent.ExternalMCPCircuitCooldownSeconds)
	setStringInMap(agentNode, "system_prompt_path", agent.SystemPromptPath)
}

func updateMCPConfig(doc *yaml.Node, cfg config.MCPConfig) {
	root := doc.Content[0]
	mcpNode := ensureMap(root, "mcp")
	setBoolInMap(mcpNode, "enabled", cfg.Enabled)
	setStringInMap(mcpNode, "host", cfg.Host)
	setIntInMap(mcpNode, "port", cfg.Port)
}

func updateVisionConfig(doc *yaml.Node, cfg config.VisionConfig) {
	root := doc.Content[0]
	visionNode := ensureMap(root, "vision")
	setBoolInMap(visionNode, "enabled", cfg.Enabled)
	if strings.TrimSpace(cfg.APIKey) != "" {
		setStringInMap(visionNode, "api_key", cfg.APIKey)
	} else {
		setStringInMap(visionNode, "api_key", "")
	}
	if strings.TrimSpace(cfg.BaseURL) != "" {
		setStringInMap(visionNode, "base_url", cfg.BaseURL)
	} else {
		setStringInMap(visionNode, "base_url", "")
	}
	setStringInMap(visionNode, "model", cfg.Model)
	if strings.TrimSpace(cfg.Provider) != "" {
		setStringInMap(visionNode, "provider", cfg.Provider)
	}
	if cfg.TimeoutSeconds > 0 {
		setIntInMap(visionNode, "timeout_seconds", cfg.TimeoutSeconds)
	}
	if cfg.MaxImageBytes > 0 {
		setIntInMap(visionNode, "max_image_bytes", int(cfg.MaxImageBytes))
	}
	if cfg.MaxDimension > 0 {
		setIntInMap(visionNode, "max_dimension", cfg.MaxDimension)
	}
	if cfg.JPEGQuality > 0 {
		setIntInMap(visionNode, "jpeg_quality", cfg.JPEGQuality)
	}
	if cfg.MaxPayloadBytes > 0 {
		setIntInMap(visionNode, "max_payload_bytes", int(cfg.MaxPayloadBytes))
	}
	setIntInMap(visionNode, "skip_preprocess_below_bytes", int(cfg.SkipPreprocessBelowBytes))
	if strings.TrimSpace(cfg.Detail) != "" {
		setStringInMap(visionNode, "detail", cfg.Detail)
	}
}

func updateOpenAIConfig(doc *yaml.Node, cfg config.OpenAIConfig) {
	root := doc.Content[0]
	openaiNode := ensureMap(root, "openai")
	if cfg.Provider != "" {
		setStringInMap(openaiNode, "provider", cfg.Provider)
	}
	setStringInMap(openaiNode, "api_key", cfg.APIKey)
	setStringInMap(openaiNode, "base_url", cfg.BaseURL)
	setStringInMap(openaiNode, "model", cfg.Model)
	if cfg.MaxTotalTokens > 0 {
		setIntInMap(openaiNode, "max_total_tokens", cfg.MaxTotalTokens)
	}
	rn := ensureMap(openaiNode, "reasoning")
	if strings.TrimSpace(cfg.Reasoning.Mode) != "" {
		setStringInMap(rn, "mode", cfg.Reasoning.Mode)
	}
	if strings.TrimSpace(cfg.Reasoning.Effort) != "" {
		setStringInMap(rn, "effort", cfg.Reasoning.Effort)
	}
	if cfg.Reasoning.AllowClientReasoning != nil {
		setBoolInMap(rn, "allow_client_reasoning", *cfg.Reasoning.AllowClientReasoning)
	}
	if strings.TrimSpace(cfg.Reasoning.Profile) != "" {
		setStringInMap(rn, "profile", cfg.Reasoning.Profile)
	}
}

func updateAIConfig(doc *yaml.Node, cfg config.AIConfig) {
	root := doc.Content[0]
	aiNode := ensureMap(root, "ai")
	if strings.TrimSpace(cfg.DefaultChannel) != "" {
		setStringInMap(aiNode, "default_channel", config.NormalizeAIChannelID(cfg.DefaultChannel))
	}
	channelsNode := ensureMap(aiNode, "channels")
	channelsNode.Content = nil
	normalized := make(map[string]config.AIChannelConfig, len(cfg.Channels))
	ids := make([]string, 0, len(cfg.Channels))
	for id, ch := range cfg.Channels {
		nid := config.NormalizeAIChannelID(id)
		if nid == "" {
			continue
		}
		if _, exists := normalized[nid]; !exists {
			ids = append(ids, nid)
		}
		normalized[nid] = ch
	}
	sort.Strings(ids)
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		ch := normalized[id]
		keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: id}
		channelNode := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		channelsNode.Content = append(channelsNode.Content, keyNode, channelNode)
		setStringInMap(channelNode, "name", ch.Name)
		if strings.TrimSpace(ch.Provider) != "" {
			setStringInMap(channelNode, "provider", ch.Provider)
		}
		setStringInMap(channelNode, "api_key", ch.APIKey)
		setStringInMap(channelNode, "base_url", ch.BaseURL)
		setStringInMap(channelNode, "model", ch.Model)
		if ch.MaxTotalTokens > 0 {
			setIntInMap(channelNode, "max_total_tokens", ch.MaxTotalTokens)
		}
		if ch.MaxCompletionTokens > 0 {
			setIntInMap(channelNode, "max_completion_tokens", ch.MaxCompletionTokens)
		}
		rn := ensureMap(channelNode, "reasoning")
		if strings.TrimSpace(ch.Reasoning.Mode) != "" {
			setStringInMap(rn, "mode", ch.Reasoning.Mode)
		}
		if strings.TrimSpace(ch.Reasoning.Effort) != "" {
			setStringInMap(rn, "effort", ch.Reasoning.Effort)
		}
		if ch.Reasoning.AllowClientReasoning != nil {
			setBoolInMap(rn, "allow_client_reasoning", *ch.Reasoning.AllowClientReasoning)
		}
		if strings.TrimSpace(ch.Reasoning.Profile) != "" {
			setStringInMap(rn, "profile", ch.Reasoning.Profile)
		}
		if len(rn.Content) == 0 {
			removeKeyFromMap(channelNode, "reasoning")
		}
	}
}

func updateFOFAConfig(doc *yaml.Node, cfg config.FofaConfig) {
	root := doc.Content[0]
	fofaNode := ensureMap(root, "fofa")
	setStringInMap(fofaNode, "base_url", cfg.BaseURL)
	removeKeyFromMap(fofaNode, "email")
	setStringInMap(fofaNode, "api_key", cfg.APIKey)
}

func updateSpaceSearchConfig(doc *yaml.Node, key string, cfg config.SpaceSearchConfig) {
	root := doc.Content[0]
	node := ensureMap(root, key)
	setStringInMap(node, "base_url", cfg.BaseURL)
	setStringInMap(node, "api_key", cfg.APIKey)
}

func updateKnowledgeConfig(doc *yaml.Node, cfg config.KnowledgeConfig) {
	root := doc.Content[0]
	knowledgeNode := ensureMap(root, "knowledge")
	setBoolInMap(knowledgeNode, "enabled", cfg.Enabled)
	setStringInMap(knowledgeNode, "base_path", cfg.BasePath)

	// 更新嵌入配置
	embeddingNode := ensureMap(knowledgeNode, "embedding")
	setStringInMap(embeddingNode, "provider", cfg.Embedding.Provider)
	setStringInMap(embeddingNode, "model", cfg.Embedding.Model)
	if cfg.Embedding.BaseURL != "" {
		setStringInMap(embeddingNode, "base_url", cfg.Embedding.BaseURL)
	}
	if cfg.Embedding.APIKey != "" {
		setStringInMap(embeddingNode, "api_key", cfg.Embedding.APIKey)
	}

	// 更新检索配置
	retrievalNode := ensureMap(knowledgeNode, "retrieval")
	setIntInMap(retrievalNode, "top_k", cfg.Retrieval.TopK)
	setFloatInMap(retrievalNode, "similarity_threshold", cfg.Retrieval.SimilarityThreshold)
	setStringInMap(retrievalNode, "sub_index_filter", cfg.Retrieval.SubIndexFilter)
	mqNode := ensureMap(retrievalNode, "multi_query")
	setIntInMap(mqNode, "max_queries", cfg.Retrieval.MultiQuery.MaxQueries)
	rerankNode := ensureMap(retrievalNode, "rerank")
	setStringInMap(rerankNode, "provider", cfg.Retrieval.Rerank.Provider)
	setStringInMap(rerankNode, "model", cfg.Retrieval.Rerank.Model)
	setStringInMap(rerankNode, "base_url", cfg.Retrieval.Rerank.BaseURL)
	setStringInMap(rerankNode, "api_key", cfg.Retrieval.Rerank.APIKey)
	postNode := ensureMap(retrievalNode, "post_retrieve")
	setIntInMap(postNode, "prefetch_top_k", cfg.Retrieval.PostRetrieve.PrefetchTopK)
	setIntInMap(postNode, "max_context_chars", cfg.Retrieval.PostRetrieve.MaxContextChars)
	setIntInMap(postNode, "max_context_tokens", cfg.Retrieval.PostRetrieve.MaxContextTokens)

	// 更新索引配置
	indexingNode := ensureMap(knowledgeNode, "indexing")
	setStringInMap(indexingNode, "chunk_strategy", cfg.Indexing.ChunkStrategy)
	setIntInMap(indexingNode, "request_timeout_seconds", cfg.Indexing.RequestTimeoutSeconds)
	setIntInMap(indexingNode, "chunk_size", cfg.Indexing.ChunkSize)
	setIntInMap(indexingNode, "chunk_overlap", cfg.Indexing.ChunkOverlap)
	setIntInMap(indexingNode, "max_chunks_per_item", cfg.Indexing.MaxChunksPerItem)
	setBoolInMap(indexingNode, "prefer_source_file", cfg.Indexing.PreferSourceFile)
	setIntInMap(indexingNode, "batch_size", cfg.Indexing.BatchSize)
	setStringSliceInMap(indexingNode, "sub_indexes", cfg.Indexing.SubIndexes)
	setIntInMap(indexingNode, "max_rpm", cfg.Indexing.MaxRPM)
	setIntInMap(indexingNode, "rate_limit_delay_ms", cfg.Indexing.RateLimitDelayMs)
	setIntInMap(indexingNode, "max_retries", cfg.Indexing.MaxRetries)
	setIntInMap(indexingNode, "retry_delay_ms", cfg.Indexing.RetryDelayMs)
}

func updateC2Config(doc *yaml.Node, cfg config.C2Config) {
	root := doc.Content[0]
	c2Node := ensureMap(root, "c2")
	setBoolInMap(c2Node, "enabled", cfg.EnabledEffective())
}

func mergeHitlToolWhitelistSlice(existing, add []string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(existing)+len(add))
	for _, list := range [][]string{existing, add} {
		for _, t := range list {
			n := strings.ToLower(strings.TrimSpace(t))
			if n == "" {
				continue
			}
			if _, ok := seen[n]; ok {
				continue
			}
			seen[n] = struct{}{}
			out = append(out, strings.TrimSpace(t))
		}
	}
	return out
}

// SetHitlToolWhitelist 将全局免审批工具白名单整表写入 config.yaml（替换，非合并）。
func (h *ConfigHandler) SetHitlToolWhitelist(tools []string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.config.Hitl.ToolWhitelist = mergeHitlToolWhitelistSlice(nil, tools)
	if err := h.saveConfig(); err != nil {
		return err
	}
	h.logger.Info("HITL 全局工具白名单已写入配置文件",
		zap.Int("count", len(h.config.Hitl.ToolWhitelist)),
	)
	return nil
}

// MergeHitlToolWhitelistIntoConfig 将会话侧栏提交的免审批工具名合并进内存配置并写入 config.yaml（与全局白名单去重规则一致：小写键、保留首次出现的原始大小写）。
func (h *ConfigHandler) MergeHitlToolWhitelistIntoConfig(add []string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	merged := mergeHitlToolWhitelistSlice(h.config.Hitl.ToolWhitelist, add)
	h.config.Hitl.ToolWhitelist = merged
	if err := h.saveConfig(); err != nil {
		return err
	}
	h.logger.Info("HITL 全局工具白名单已合并写入配置文件",
		zap.Int("count", len(merged)),
	)
	return nil
}

func updateHitlConfig(doc *yaml.Node, cfg config.HitlConfig) {
	root := doc.Content[0]
	hitlNode := ensureMap(root, "hitl")
	auditModelNode := ensureMap(hitlNode, "audit_model")
	setStringInMap(auditModelNode, "provider", cfg.AuditModel.Provider)
	setStringInMap(auditModelNode, "base_url", cfg.AuditModel.BaseURL)
	setStringInMap(auditModelNode, "api_key", cfg.AuditModel.APIKey)
	setStringInMap(auditModelNode, "model", cfg.AuditModel.Model)
	// flow 样式 [a, b, c] 单行展示，工具多时比块序列省行数
	setFlowStringSliceInMap(hitlNode, "tool_whitelist", cfg.ToolWhitelist)
	setStringInMap(hitlNode, "default_mode", cfg.EffectiveDefaultMode())
	setStringInMap(hitlNode, "default_reviewer", cfg.EffectiveDefaultReviewer())
	setIntInMap(hitlNode, "default_timeout_seconds", cfg.EffectiveDefaultTimeoutSeconds())
	setIntInMap(hitlNode, "retention_days", cfg.RetentionDaysEffective())
	setStringInMap(hitlNode, "audit_agent_prompt", cfg.AuditAgentPrompt)
	setStringInMap(hitlNode, "audit_agent_prompt_review_edit", cfg.AuditAgentPromptReviewEdit)
}

// updateStorageConfig 把运行空间清理策略写回 config.yaml，保留文件其余内容与注释。
func updateStorageConfig(doc *yaml.Node, cfg config.StorageConfig) {
	root := doc.Content[0]
	storageNode := ensureMap(root, "storage")
	setBoolInMap(storageNode, "auto_clean", cfg.AutoCleanEffective())
	setIntInMap(storageNode, "interval_minutes", cfg.IntervalMinutesEffective())
	setIntInMap(storageNode, "orphan_grace_days", cfg.OrphanGraceDaysEffective())
	setIntInMap(storageNode, "active_grace_hours", cfg.ActiveGraceHoursEffective())

	// 按固定顺序输出，避免每次保存都因 map 迭代顺序不同而重排整个文件。
	categoriesNode := ensureMap(storageNode, "categories")
	for _, key := range config.StorageCategoryOrder {
		categoryNode := ensureMap(categoriesNode, key)
		setBoolInMap(categoryNode, "enabled", cfg.CategoryEnabled(key))
		setIntInMap(categoryNode, "retention_days", cfg.CategoryRetentionDays(key))
	}
}

// UpdateHitlDefaultConfig 更新全局默认人机协同配置并写入 config.yaml。
func (h *ConfigHandler) UpdateHitlDefaultConfig(mode, reviewer string, timeoutSeconds int) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.config.Hitl.DefaultMode = config.HitlConfig{DefaultMode: mode}.EffectiveDefaultMode()
	h.config.Hitl.DefaultReviewer = config.HitlConfig{DefaultReviewer: reviewer}.EffectiveDefaultReviewer()
	if timeoutSeconds < 0 {
		timeoutSeconds = 0
	}
	h.config.Hitl.DefaultTimeoutSeconds = &timeoutSeconds
	if err := h.saveConfig(); err != nil {
		return err
	}
	h.logger.Info("HITL 全局默认配置已写入配置文件",
		zap.String("default_mode", h.config.Hitl.DefaultMode),
		zap.String("default_reviewer", h.config.Hitl.DefaultReviewer),
		zap.Int("default_timeout_seconds", timeoutSeconds),
	)
	return nil
}

// UpdateHitlDefaultReviewer 更新全局默认审批方并写入 config.yaml。
func (h *ConfigHandler) UpdateHitlDefaultReviewer(reviewer string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.config.Hitl.DefaultReviewer = config.HitlConfig{DefaultReviewer: reviewer}.EffectiveDefaultReviewer()
	if err := h.saveConfig(); err != nil {
		return err
	}
	h.logger.Info("HITL 全局默认审批方已写入配置文件", zap.String("default_reviewer", h.config.Hitl.DefaultReviewer))
	return nil
}

// UpdateHitlAuditAgentStrategy 更新审批/审查编辑两套审计 Agent 提示词并写入 config.yaml。
func (h *ConfigHandler) UpdateHitlAuditAgentStrategy(approvalPrompt, reviewEditPrompt string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.config.Hitl.AuditAgentPrompt = strings.TrimSpace(approvalPrompt)
	h.config.Hitl.AuditAgentPromptReviewEdit = strings.TrimSpace(reviewEditPrompt)
	if err := h.saveConfig(); err != nil {
		return err
	}
	h.logger.Info("HITL 审计 Agent 提示词已写入配置文件")
	return nil
}

func updateRobotsConfig(doc *yaml.Node, cfg config.RobotsConfig) {
	root := doc.Content[0]
	robotsNode := ensureMap(root, "robots")

	if cfg.Session.StrictUserIdentity != nil {
		sessionNode := ensureMap(robotsNode, "session")
		setBoolInMap(sessionNode, "strict_user_identity", *cfg.Session.StrictUserIdentity)
	}

	wechatNode := ensureMap(robotsNode, "wechat")
	setBoolInMap(wechatNode, "enabled", cfg.Wechat.Enabled)
	setStringInMap(wechatNode, "bot_token", cfg.Wechat.BotToken)
	setStringInMap(wechatNode, "ilink_bot_id", cfg.Wechat.ILinkBotID)
	setStringInMap(wechatNode, "ilink_user_id", cfg.Wechat.ILinkUserID)
	setStringInMap(wechatNode, "base_url", cfg.Wechat.BaseURL)
	setStringInMap(wechatNode, "bot_type", cfg.Wechat.BotType)
	setStringInMap(wechatNode, "bot_agent", cfg.Wechat.BotAgent)

	wecomNode := ensureMap(robotsNode, "wecom")
	setBoolInMap(wecomNode, "enabled", cfg.Wecom.Enabled)
	setStringInMap(wecomNode, "token", cfg.Wecom.Token)
	setStringInMap(wecomNode, "encoding_aes_key", cfg.Wecom.EncodingAESKey)
	setStringInMap(wecomNode, "corp_id", cfg.Wecom.CorpID)
	setStringInMap(wecomNode, "secret", cfg.Wecom.Secret)
	setIntInMap(wecomNode, "agent_id", int(cfg.Wecom.AgentID))

	dingtalkNode := ensureMap(robotsNode, "dingtalk")
	setBoolInMap(dingtalkNode, "enabled", cfg.Dingtalk.Enabled)
	setStringInMap(dingtalkNode, "client_id", cfg.Dingtalk.ClientID)
	setStringInMap(dingtalkNode, "client_secret", cfg.Dingtalk.ClientSecret)
	setBoolInMap(dingtalkNode, "allow_conversation_id_fallback", cfg.Dingtalk.AllowConversationIDFallback)

	larkNode := ensureMap(robotsNode, "lark")
	setBoolInMap(larkNode, "enabled", cfg.Lark.Enabled)
	setStringInMap(larkNode, "app_id", cfg.Lark.AppID)
	setStringInMap(larkNode, "app_secret", cfg.Lark.AppSecret)
	setStringInMap(larkNode, "verify_token", cfg.Lark.VerifyToken)
	setBoolInMap(larkNode, "allow_chat_id_fallback", cfg.Lark.AllowChatIDFallback)

	telegramNode := ensureMap(robotsNode, "telegram")
	setBoolInMap(telegramNode, "enabled", cfg.Telegram.Enabled)
	setStringInMap(telegramNode, "bot_token", cfg.Telegram.BotToken)
	setStringInMap(telegramNode, "bot_username", cfg.Telegram.BotUsername)
	setBoolInMap(telegramNode, "allow_group_messages", cfg.Telegram.AllowGroupMessages)

	slackNode := ensureMap(robotsNode, "slack")
	setBoolInMap(slackNode, "enabled", cfg.Slack.Enabled)
	setStringInMap(slackNode, "bot_token", cfg.Slack.BotToken)
	setStringInMap(slackNode, "app_token", cfg.Slack.AppToken)

	discordNode := ensureMap(robotsNode, "discord")
	setBoolInMap(discordNode, "enabled", cfg.Discord.Enabled)
	setStringInMap(discordNode, "bot_token", cfg.Discord.BotToken)
	setBoolInMap(discordNode, "allow_guild_messages", cfg.Discord.AllowGuildMessages)

	qqNode := ensureMap(robotsNode, "qq")
	setBoolInMap(qqNode, "enabled", cfg.QQ.Enabled)
	setStringInMap(qqNode, "app_id", cfg.QQ.AppID)
	setStringInMap(qqNode, "client_secret", cfg.QQ.ClientSecret)
	setBoolInMap(qqNode, "sandbox", cfg.QQ.Sandbox)
}

func updateMultiAgentConfig(doc *yaml.Node, cfg config.MultiAgentConfig) {
	root := doc.Content[0]
	maNode := ensureMap(root, "multi_agent")
	setBoolInMap(maNode, "enabled", cfg.Enabled)
	setStringInMap(maNode, "robot_default_agent_mode", config.NormalizeRobotAgentMode(cfg))
	setBoolInMap(maNode, "batch_use_multi_agent", cfg.BatchUseMultiAgent)
	setIntInMap(maNode, "plan_execute_loop_max_iterations", cfg.PlanExecuteLoopMaxIterations)
	mwNode := ensureMap(maNode, "eino_middleware")
	setIntInMap(mwNode, "summarization_user_intent_ledger_max_runes", cfg.EinoMiddleware.SummarizationUserIntentLedgerMaxRunesEffective())
	setIntInMap(mwNode, "summarization_user_intent_ledger_entry_max_runes", cfg.EinoMiddleware.SummarizationUserIntentLedgerEntryMaxRunesEffective())
	setIntInMap(mwNode, "latest_user_message_max_runes", cfg.EinoMiddleware.LatestUserMessageMaxRunesEffective())
	setIntInMap(mwNode, "latest_user_message_head_runes", cfg.EinoMiddleware.LatestUserMessageHeadRunesEffective())
	setIntInMap(mwNode, "latest_user_message_tail_runes", cfg.EinoMiddleware.LatestUserMessageTailRunesEffective())
	setIntInMap(mwNode, "model_retry_max_retries", cfg.EinoMiddleware.ModelRetryMaxRetries)
	setIntInMap(mwNode, "model_retry_max_backoff_sec", cfg.EinoMiddleware.ModelRetryMaxBackoffSec)
	setFlowStringSliceInMap(mwNode, "model_failover_channels", dedupeTrimmedStringList(cfg.EinoMiddleware.ModelFailoverChannels))
	setIntInMap(mwNode, "model_failover_max_retries", cfg.EinoMiddleware.ModelFailoverMaxRetries)
	setFlowStringSliceInMap(mwNode, "tool_search_always_visible_tools", dedupeToolNameList(cfg.EinoMiddleware.ToolSearchAlwaysVisibleTools))
}

func dedupeToolNameList(in []string) []string {
	return dedupeTrimmedStringList(in)
}

func dedupeTrimmedStringList(in []string) []string {
	if len(in) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, name := range in {
		n := strings.TrimSpace(name)
		if n == "" {
			continue
		}
		key := strings.ToLower(n)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, n)
	}
	return out
}

func mergeToolNameLists(a, b []string) []string {
	return dedupeToolNameList(append(append([]string{}, a...), b...))
}

func ensureMap(parent *yaml.Node, path ...string) *yaml.Node {
	current := parent
	for _, key := range path {
		value := findMapValue(current, key)
		if value == nil {
			keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
			mapNode := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			current.Content = append(current.Content, keyNode, mapNode)
			value = mapNode
		}

		if value.Kind != yaml.MappingNode {
			value.Kind = yaml.MappingNode
			value.Tag = "!!map"
			value.Style = 0
			value.Content = nil
		}

		current = value
	}

	return current
}

func findMapValue(mapNode *yaml.Node, key string) *yaml.Node {
	if mapNode == nil || mapNode.Kind != yaml.MappingNode {
		return nil
	}

	for i := 0; i < len(mapNode.Content); i += 2 {
		if mapNode.Content[i].Value == key {
			return mapNode.Content[i+1]
		}
	}
	return nil
}

func ensureKeyValue(mapNode *yaml.Node, key string) (*yaml.Node, *yaml.Node) {
	if mapNode == nil || mapNode.Kind != yaml.MappingNode {
		return nil, nil
	}

	for i := 0; i < len(mapNode.Content); i += 2 {
		if mapNode.Content[i].Value == key {
			return mapNode.Content[i], mapNode.Content[i+1]
		}
	}

	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	valueNode := &yaml.Node{}
	mapNode.Content = append(mapNode.Content, keyNode, valueNode)
	return keyNode, valueNode
}

func setStringInMap(mapNode *yaml.Node, key, value string) {
	_, valueNode := ensureKeyValue(mapNode, key)
	valueNode.Kind = yaml.ScalarNode
	valueNode.Tag = "!!str"
	valueNode.Style = 0
	valueNode.Value = value
}

func removeKeyFromMap(mapNode *yaml.Node, key string) {
	if mapNode == nil || mapNode.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(mapNode.Content); i += 2 {
		if mapNode.Content[i].Value == key {
			mapNode.Content = append(mapNode.Content[:i], mapNode.Content[i+2:]...)
			return
		}
	}
}

func setStringSliceInMap(mapNode *yaml.Node, key string, values []string) {
	_, valueNode := ensureKeyValue(mapNode, key)
	valueNode.Kind = yaml.SequenceNode
	valueNode.Tag = "!!seq"
	valueNode.Style = 0
	valueNode.Content = nil
	for _, v := range values {
		valueNode.Content = append(valueNode.Content, &yaml.Node{
			Kind:  yaml.ScalarNode,
			Tag:   "!!str",
			Value: v,
		})
	}
}

func setFlowStringSliceInMap(mapNode *yaml.Node, key string, values []string) {
	_, valueNode := ensureKeyValue(mapNode, key)
	valueNode.Kind = yaml.SequenceNode
	valueNode.Tag = "!!seq"
	valueNode.Style = yaml.FlowStyle
	valueNode.Content = nil
	for _, v := range values {
		valueNode.Content = append(valueNode.Content, &yaml.Node{
			Kind:  yaml.ScalarNode,
			Tag:   "!!str",
			Value: v,
		})
	}
}

func setIntInMap(mapNode *yaml.Node, key string, value int) {
	_, valueNode := ensureKeyValue(mapNode, key)
	valueNode.Kind = yaml.ScalarNode
	valueNode.Tag = "!!int"
	valueNode.Style = 0
	valueNode.Value = fmt.Sprintf("%d", value)
}

func findBoolInMap(mapNode *yaml.Node, key string) *bool {
	if mapNode == nil || mapNode.Kind != yaml.MappingNode {
		return nil
	}

	for i := 0; i < len(mapNode.Content); i += 2 {
		if i+1 >= len(mapNode.Content) {
			break
		}
		keyNode := mapNode.Content[i]
		valueNode := mapNode.Content[i+1]

		if keyNode.Kind == yaml.ScalarNode && keyNode.Value == key {
			if valueNode.Kind == yaml.ScalarNode {
				if valueNode.Value == "true" {
					result := true
					return &result
				} else if valueNode.Value == "false" {
					result := false
					return &result
				}
			}
			return nil
		}
	}
	return nil
}

func setBoolInMap(mapNode *yaml.Node, key string, value bool) {
	_, valueNode := ensureKeyValue(mapNode, key)
	valueNode.Kind = yaml.ScalarNode
	valueNode.Tag = "!!bool"
	valueNode.Style = 0
	if value {
		valueNode.Value = "true"
	} else {
		valueNode.Value = "false"
	}
}

func setFloatInMap(mapNode *yaml.Node, key string, value float64) {
	_, valueNode := ensureKeyValue(mapNode, key)
	valueNode.Kind = yaml.ScalarNode
	valueNode.Tag = "!!float"
	valueNode.Style = 0
	// 对于0.0到1.0之间的值（如 similarity_threshold），使用%.1f确保0.0被明确序列化为"0.0"
	// 对于其他值，使用%g自动选择最合适的格式
	if value >= 0.0 && value <= 1.0 {
		valueNode.Value = fmt.Sprintf("%.1f", value)
	} else {
		valueNode.Value = fmt.Sprintf("%g", value)
	}
}

// getExternalMCPTools 获取外部MCP工具列表（公共方法）
func (h *ConfigHandler) getExternalMCPTools(ctx context.Context) []ToolConfigInfo {
	if h.externalMCPMgr == nil {
		return nil
	}
	return h.getExternalMCPToolsWithManager(ctx, h.externalMCPMgr, h.pickToolDescription)
}

// getExternalMCPToolsWithManager 获取外部 MCP 工具（不持有 config 锁，供 GetTools 等热路径使用）
func (h *ConfigHandler) getExternalMCPToolsWithManager(
	ctx context.Context,
	mgr *mcp.ExternalMCPManager,
	pickDesc func(shortDesc, fullDesc string) string,
) []ToolConfigInfo {
	var result []ToolConfigInfo
	if mgr == nil {
		return result
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	externalTools, err := mgr.GetAllTools(timeoutCtx)
	if err != nil {
		h.logger.Warn("获取外部MCP工具失败（可能连接断开），尝试返回缓存的工具",
			zap.Error(err),
			zap.String("hint", "如果外部MCP工具未显示，请检查连接状态或点击刷新按钮"),
		)
	}

	if len(externalTools) == 0 {
		return result
	}

	externalMCPConfigs := mgr.GetConfigs()

	for _, externalTool := range externalTools {
		mcpName, actualToolName := h.parseExternalToolName(externalTool.Name)
		if mcpName == "" || actualToolName == "" {
			continue
		}

		enabled := h.calculateExternalToolEnabledWithManager(mcpName, actualToolName, externalMCPConfigs, mgr)

		result = append(result, ToolConfigInfo{
			Name:        actualToolName,
			Description: pickDesc(externalTool.ShortDescription, externalTool.Description),
			Enabled:     enabled,
			IsExternal:  true,
			ExternalMCP: mcpName,
		})
	}

	return result
}

// parseExternalToolName 解析外部工具名称（格式：mcpName::toolName）
func (h *ConfigHandler) parseExternalToolName(fullName string) (mcpName, toolName string) {
	idx := strings.Index(fullName, "::")
	if idx > 0 {
		return fullName[:idx], fullName[idx+2:]
	}
	return "", ""
}

// calculateExternalToolEnabled 计算外部工具的启用状态
func (h *ConfigHandler) calculateExternalToolEnabled(mcpName, toolName string, configs map[string]config.ExternalMCPServerConfig) bool {
	return h.calculateExternalToolEnabledWithManager(mcpName, toolName, configs, h.externalMCPMgr)
}

func (h *ConfigHandler) calculateExternalToolEnabledWithManager(
	mcpName, toolName string,
	configs map[string]config.ExternalMCPServerConfig,
	mgr *mcp.ExternalMCPManager,
) bool {
	cfg, exists := configs[mcpName]
	if !exists {
		return false
	}

	if !cfg.ExternalMCPEnable {
		return false
	}

	if cfg.ToolEnabled != nil {
		if toolEnabled, exists := cfg.ToolEnabled[toolName]; exists && !toolEnabled {
			return false
		}
	}

	if mgr == nil {
		return false
	}
	client, exists := mgr.GetClient(mcpName)
	if !exists || !client.IsConnected() {
		return false
	}

	return true
}

// pickToolDescription 根据 security.tool_description_mode 选择 short 或 full 描述并限制长度。
// 调用方若已持有 h.mu 读锁，须直接读 mode 并调用 pickToolDescriptionWithMode，避免嵌套 RLock 死锁。
func (h *ConfigHandler) pickToolDescription(shortDesc, fullDesc string) string {
	return pickToolDescriptionWithMode(h.config.Security.ToolDescriptionMode, shortDesc, fullDesc)
}

func pickToolDescriptionWithMode(mode, shortDesc, fullDesc string) string {
	useFull := strings.TrimSpace(strings.ToLower(mode)) == "full"
	description := shortDesc
	if useFull {
		description = fullDesc
	} else if description == "" {
		description = fullDesc
	}
	if len(description) > 10000 {
		description = description[:10000] + "..."
	}
	return description
}

// GetToolSchema 获取单个工具的 inputSchema（按需加载，避免列表接口返回大量 schema 数据）
func (h *ConfigHandler) GetToolSchema(c *gin.Context) {
	toolName := c.Param("name")
	if toolName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "工具名称不能为空"})
		return
	}

	externalMCP := c.Query("external_mcp")
	if externalMCP != "" {
		h.mu.RLock()
		externalMCPMgr := h.externalMCPMgr
		h.mu.RUnlock()

		if externalMCPMgr != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			externalTools, _ := externalMCPMgr.GetAllTools(ctx)
			fullName := externalMCP + "::" + toolName
			for _, t := range externalTools {
				if t.Name == fullName {
					c.JSON(http.StatusOK, gin.H{"input_schema": t.InputSchema})
					return
				}
			}
		}
		c.JSON(http.StatusNotFound, gin.H{"error": "外部工具未找到"})
		return
	}

	h.mu.RLock()
	securityTools := append([]config.ToolConfig(nil), h.config.Security.Tools...)
	mcpServer := h.mcpServer
	h.mu.RUnlock()

	for _, tool := range securityTools {
		if tool.Name == toolName {
			c.JSON(http.StatusOK, gin.H{"input_schema": buildInputSchemaFromParams(tool.Parameters)})
			return
		}
	}

	// MCP 注册工具（如知识检索）
	if mcpServer != nil {
		for _, mt := range mcpServer.GetAllTools() {
			if mt.Name == toolName {
				c.JSON(http.StatusOK, gin.H{"input_schema": mt.InputSchema})
				return
			}
		}
	}

	c.JSON(http.StatusNotFound, gin.H{"error": "工具未找到"})
}

// buildInputSchemaFromParams 从 YAML 工具的 ParameterConfig 构建 JSON Schema（用于前端展示）。
// 不依赖 MCP 服务器注册状态，所有工具（包括未启用的）都能返回参数定义。
func buildInputSchemaFromParams(params []config.ParameterConfig) map[string]interface{} {
	if len(params) == 0 {
		return nil
	}

	properties := make(map[string]interface{})
	required := make([]string, 0)

	for _, p := range params {
		name := strings.TrimSpace(p.Name)
		if name == "" {
			continue
		}
		prop := map[string]interface{}{
			"type":        convertParamType(p.Type),
			"description": p.Description,
		}
		if p.Default != nil {
			prop["default"] = p.Default
		}
		if len(p.Options) > 0 {
			prop["enum"] = p.Options
		}
		properties[name] = prop
		if p.Required {
			required = append(required, name)
		}
	}

	schema := map[string]interface{}{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func convertParamType(t string) string {
	switch strings.TrimSpace(strings.ToLower(t)) {
	case "int", "integer", "number":
		return "number"
	case "bool", "boolean":
		return "boolean"
	case "array", "list":
		return "array"
	default:
		return "string"
	}
}
