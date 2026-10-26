package handler

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"cyberstrike-ai/internal/audit"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"

	"gopkg.in/yaml.v3"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// RoleHandler 角色处理器
type RoleHandler struct {
	config     *config.Config
	configPath string
	logger     *zap.Logger
	audit      *audit.Service
	db         *database.DB
}

func (h *RoleHandler) SetDB(db *database.DB) { h.db = db }

// SetAudit wires platform audit logging.
func (h *RoleHandler) SetAudit(s *audit.Service) {
	h.audit = s
}

// NewRoleHandler 创建新的角色处理器
func NewRoleHandler(cfg *config.Config, configPath string, logger *zap.Logger) *RoleHandler {
	return &RoleHandler{
		config:     cfg,
		configPath: configPath,
		logger:     logger,
	}
}

// GetRoles 获取所有角色
func (h *RoleHandler) GetRoles(c *gin.Context) {
	if h.config.Roles == nil {
		h.config.Roles = make(map[string]config.RoleConfig)
	}

	roles := make([]config.RoleConfig, 0, len(h.config.Roles))
	for key, role := range h.config.Roles {
		// 确保角色的key与name一致
		if role.Name == "" {
			role.Name = key
		}
		roles = append(roles, role)
	}

	c.JSON(http.StatusOK, gin.H{
		"roles": roles,
	})
}

// GetRole 获取单个角色
func (h *RoleHandler) GetRole(c *gin.Context) {
	roleName := c.Param("name")
	if roleName == "" {
		respondError(c, http.StatusBadRequest, "角色名称不能为空", "role name must not be empty")
		return
	}

	if h.config.Roles == nil {
		respondError(c, http.StatusNotFound, "角色不存在", "role not found")
		return
	}

	role, exists := h.config.Roles[roleName]
	if !exists {
		respondError(c, http.StatusNotFound, "角色不存在", "role not found")
		return
	}

	// 确保角色的name与key一致
	if role.Name == "" {
		role.Name = roleName
	}

	c.JSON(http.StatusOK, gin.H{
		"role": role,
	})
}

// UpdateRole 更新角色
func (h *RoleHandler) UpdateRole(c *gin.Context) {
	roleName := c.Param("name")
	if roleName == "" {
		respondError(c, http.StatusBadRequest, "角色名称不能为空", "role name must not be empty")
		return
	}

	var req config.RoleConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf(i18nText(requestWantsEnglish(c), "无效的请求参数: %s", "invalid request parameter: %s"), err.Error())})
		return
	}

	// 确保角色名称与请求中的name一致
	if req.Name == "" {
		req.Name = roleName
	}
	if err := h.validateRole(req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Name != roleName {
		if _, exists := h.config.Roles[req.Name]; exists {
			respondError(c, http.StatusConflict, "角色已存在", "role already exists")
			return
		}
	}

	// 初始化Roles map
	if h.config.Roles == nil {
		h.config.Roles = make(map[string]config.RoleConfig)
	}

	// 删除所有与角色name相同但key不同的旧角色（避免重复）
	// 使用角色name作为key，确保唯一性
	finalKey := req.Name
	keysToDelete := make([]string, 0)
	for key := range h.config.Roles {
		// 如果key与最终的key不同，但name相同，则标记为删除
		if key != finalKey {
			role := h.config.Roles[key]
			// 确保角色的name字段正确设置
			if role.Name == "" {
				role.Name = key
			}
			if role.Name == req.Name {
				keysToDelete = append(keysToDelete, key)
			}
		}
	}
	// 删除旧的角色
	for _, key := range keysToDelete {
		delete(h.config.Roles, key)
		h.logger.Info("删除重复的角色", zap.String("oldKey", key), zap.String("name", req.Name))
	}

	// 如果当前更新的key与最终key不同，也需要删除旧的
	if roleName != finalKey {
		delete(h.config.Roles, roleName)
	}

	// 如果角色名称改变，需要删除旧文件
	if roleName != finalKey {
		configDir := filepath.Dir(h.configPath)
		rolesDir := h.config.RolesDir
		if rolesDir == "" {
			rolesDir = "roles" // 默认目录
		}

		// 如果是相对路径，相对于配置文件所在目录
		if !filepath.IsAbs(rolesDir) {
			rolesDir = filepath.Join(configDir, rolesDir)
		}

		// 删除旧的角色文件
		oldSafeFileName := sanitizeFileName(roleName)
		oldRoleFileYaml := filepath.Join(rolesDir, oldSafeFileName+".yaml")
		oldRoleFileYml := filepath.Join(rolesDir, oldSafeFileName+".yml")

		if _, err := os.Stat(oldRoleFileYaml); err == nil {
			if err := os.Remove(oldRoleFileYaml); err != nil {
				h.logger.Warn("删除旧角色配置文件失败", zap.String("file", oldRoleFileYaml), zap.Error(err))
			}
		}
		if _, err := os.Stat(oldRoleFileYml); err == nil {
			if err := os.Remove(oldRoleFileYml); err != nil {
				h.logger.Warn("删除旧角色配置文件失败", zap.String("file", oldRoleFileYml), zap.Error(err))
			}
		}
	}

	// 使用角色name作为key来保存（确保唯一性）
	h.config.Roles[finalKey] = req

	// 保存配置到文件
	if err := h.saveConfig(); err != nil {
		h.logger.Error("保存配置失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf(i18nText(requestWantsEnglish(c), "保存配置失败: %s", "failed to save config: %s"), err.Error())})
		return
	}

	h.logger.Info("更新角色", zap.String("oldKey", roleName), zap.String("newKey", finalKey), zap.String("name", req.Name))
	if h.audit != nil {
		h.audit.RecordOK(c, "role", "update", "更新角色", "role", finalKey, map[string]interface{}{"name": req.Name})
	}
	c.JSON(http.StatusOK, gin.H{
		"message": i18nText(requestWantsEnglish(c), "角色已更新", "role updated"),
		"role":    req,
	})
}

// CreateRole 创建新角色
func (h *RoleHandler) CreateRole(c *gin.Context) {
	var req config.RoleConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf(i18nText(requestWantsEnglish(c), "无效的请求参数: %s", "invalid request parameter: %s"), err.Error())})
		return
	}

	if err := h.validateRole(req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 初始化Roles map
	if h.config.Roles == nil {
		h.config.Roles = make(map[string]config.RoleConfig)
	}

	// 检查角色是否已存在
	if _, exists := h.config.Roles[req.Name]; exists {
		respondError(c, http.StatusBadRequest, "角色已存在", "role already exists")
		return
	}

	// 创建角色（默认启用）
	if !req.Enabled {
		req.Enabled = true
	}

	h.config.Roles[req.Name] = req

	// 保存配置到文件
	if err := h.saveConfig(); err != nil {
		h.logger.Error("保存配置失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf(i18nText(requestWantsEnglish(c), "保存配置失败: %s", "failed to save config: %s"), err.Error())})
		return
	}

	h.logger.Info("创建角色", zap.String("roleName", req.Name))
	if h.audit != nil {
		h.audit.RecordOK(c, "role", "create", "创建角色", "role", req.Name, nil)
	}
	c.JSON(http.StatusOK, gin.H{
		"message": i18nText(requestWantsEnglish(c), "角色已创建", "role created"),
		"role":    req,
	})
}

// DeleteRole 删除角色
func (h *RoleHandler) DeleteRole(c *gin.Context) {
	roleName := c.Param("name")
	if roleName == "" {
		respondError(c, http.StatusBadRequest, "角色名称不能为空", "role name must not be empty")
		return
	}

	if h.config.Roles == nil {
		respondError(c, http.StatusNotFound, "角色不存在", "role not found")
		return
	}

	if _, exists := h.config.Roles[roleName]; !exists {
		respondError(c, http.StatusNotFound, "角色不存在", "role not found")
		return
	}

	// 不允许删除"默认"角色
	if roleName == "默认" {
		respondError(c, http.StatusBadRequest, "不能删除默认角色", "the default role cannot be deleted")
		return
	}

	delete(h.config.Roles, roleName)

	// 删除对应的角色文件
	configDir := filepath.Dir(h.configPath)
	rolesDir := h.config.RolesDir
	if rolesDir == "" {
		rolesDir = "roles" // 默认目录
	}

	// 如果是相对路径，相对于配置文件所在目录
	if !filepath.IsAbs(rolesDir) {
		rolesDir = filepath.Join(configDir, rolesDir)
	}

	// 尝试删除角色文件（.yaml 和 .yml）
	safeFileName := sanitizeFileName(roleName)
	roleFileYaml := filepath.Join(rolesDir, safeFileName+".yaml")
	roleFileYml := filepath.Join(rolesDir, safeFileName+".yml")

	// 删除 .yaml 文件（如果存在）
	if _, err := os.Stat(roleFileYaml); err == nil {
		if err := os.Remove(roleFileYaml); err != nil {
			h.logger.Warn("删除角色配置文件失败", zap.String("file", roleFileYaml), zap.Error(err))
		} else {
			h.logger.Info("已删除角色配置文件", zap.String("file", roleFileYaml))
		}
	}

	// 删除 .yml 文件（如果存在）
	if _, err := os.Stat(roleFileYml); err == nil {
		if err := os.Remove(roleFileYml); err != nil {
			h.logger.Warn("删除角色配置文件失败", zap.String("file", roleFileYml), zap.Error(err))
		} else {
			h.logger.Info("已删除角色配置文件", zap.String("file", roleFileYml))
		}
	}

	h.logger.Info("删除角色", zap.String("roleName", roleName))
	if h.audit != nil {
		h.audit.RecordOK(c, "role", "delete", "删除角色", "role", roleName, nil)
	}
	c.JSON(http.StatusOK, gin.H{
		"message": i18nText(requestWantsEnglish(c), "角色已删除", "role deleted"),
	})
}

// saveConfig 保存配置到目录中的文件
func (h *RoleHandler) saveConfig() error {
	configDir := filepath.Dir(h.configPath)
	rolesDir := h.config.RolesDir
	if rolesDir == "" {
		rolesDir = "roles" // 默认目录
	}

	// 如果是相对路径，相对于配置文件所在目录
	if !filepath.IsAbs(rolesDir) {
		rolesDir = filepath.Join(configDir, rolesDir)
	}

	// 确保目录存在
	if err := os.MkdirAll(rolesDir, 0755); err != nil {
		return fmt.Errorf("创建角色目录失败: %w", err)
	}

	// 保存每个角色到独立的文件
	if h.config.Roles != nil {
		for roleName, role := range h.config.Roles {
			// 确保角色名称正确设置
			if role.Name == "" {
				role.Name = roleName
			}

			// 使用角色名称作为文件名（安全化文件名，避免特殊字符）
			safeFileName := sanitizeFileName(role.Name)
			roleFile := filepath.Join(rolesDir, safeFileName+".yaml")

			// 将角色配置序列化为YAML
			roleData, err := yaml.Marshal(&role)
			if err != nil {
				h.logger.Error("序列化角色配置失败", zap.String("role", roleName), zap.Error(err))
				continue
			}

			// 处理icon字段：确保包含\U的icon值被引号包围（YAML需要引号才能正确解析Unicode转义）
			roleDataStr := string(roleData)
			if role.Icon != "" && strings.HasPrefix(role.Icon, "\\U") {
				// 匹配 icon: \UXXXXXXXX 格式（没有引号），排除已经有引号的情况
				// 使用负向前瞻确保后面没有引号，或者直接匹配没有引号的情况
				re := regexp.MustCompile(`(?m)^(icon:\s+)(\\U[0-9A-F]{8})(\s*)$`)
				roleDataStr = re.ReplaceAllString(roleDataStr, `${1}"${2}"${3}`)
				roleData = []byte(roleDataStr)
			}

			// 写入文件
			if err := os.WriteFile(roleFile, roleData, 0644); err != nil {
				h.logger.Error("保存角色配置文件失败", zap.String("role", roleName), zap.String("file", roleFile), zap.Error(err))
				continue
			}

			h.logger.Info("角色配置已保存到文件", zap.String("role", roleName), zap.String("file", roleFile))
		}
	}

	return nil
}

// sanitizeFileName 将角色名称转换为安全的文件名
func sanitizeFileName(name string) string {
	// 替换可能不安全的字符
	replacer := map[rune]string{
		'/':  "_",
		'\\': "_",
		':':  "_",
		'*':  "_",
		'?':  "_",
		'"':  "_",
		'<':  "_",
		'>':  "_",
		'|':  "_",
		' ':  "_",
	}

	var result []rune
	for _, r := range name {
		if replacement, ok := replacer[r]; ok {
			result = append(result, []rune(replacement)...)
		} else {
			result = append(result, r)
		}
	}

	fileName := string(result)
	// 如果文件名为空，使用默认名称
	if fileName == "" {
		fileName = "role"
	}

	return fileName
}

// updateRolesConfig 更新角色配置
func updateRolesConfig(doc *yaml.Node, cfg config.RolesConfig) {
	root := doc.Content[0]
	rolesNode := ensureMap(root, "roles")

	// 清空现有角色
	if rolesNode.Kind == yaml.MappingNode {
		rolesNode.Content = nil
	}

	// 添加新角色（使用name作为key，确保唯一性）
	if cfg.Roles != nil {
		// 先建立一个以name为key的map，去重（保留最后一个）
		rolesByName := make(map[string]config.RoleConfig)
		for roleKey, role := range cfg.Roles {
			// 确保角色的name字段正确设置
			if role.Name == "" {
				role.Name = roleKey
			}
			// 使用name作为最终key，如果有多个key对应相同的name，只保留最后一个
			rolesByName[role.Name] = role
		}

		// 将去重后的角色写入YAML
		for roleName, role := range rolesByName {
			roleNode := ensureMap(rolesNode, roleName)
			setStringInMap(roleNode, "name", role.Name)
			setStringInMap(roleNode, "description", role.Description)
			setStringInMap(roleNode, "user_prompt", role.UserPrompt)
			if role.Icon != "" {
				setStringInMap(roleNode, "icon", role.Icon)
			}
			setBoolInMap(roleNode, "enabled", role.Enabled)

			// 添加工具列表（优先使用tools字段）
			if len(role.Tools) > 0 {
				toolsNode := ensureArray(roleNode, "tools")
				toolsNode.Content = nil
				for _, toolKey := range role.Tools {
					toolNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: toolKey}
					toolsNode.Content = append(toolsNode.Content, toolNode)
				}
			} else if len(role.MCPs) > 0 {
				// 向后兼容：如果没有tools但有mcps，保存mcps
				mcpsNode := ensureArray(roleNode, "mcps")
				mcpsNode.Content = nil
				for _, mcpName := range role.MCPs {
					mcpNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: mcpName}
					mcpsNode.Content = append(mcpsNode.Content, mcpNode)
				}
			}
		}
	}
}

// ensureArray 确保数组中存在指定key的数组节点
func ensureArray(parent *yaml.Node, key string) *yaml.Node {
	_, valueNode := ensureKeyValue(parent, key)
	if valueNode.Kind != yaml.SequenceNode {
		valueNode.Kind = yaml.SequenceNode
		valueNode.Tag = "!!seq"
		valueNode.Content = nil
	}
	return valueNode
}
