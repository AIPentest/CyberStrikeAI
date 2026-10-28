package handler

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"cyberstrike-ai/internal/agents"
	"cyberstrike-ai/internal/audit"
	"cyberstrike-ai/internal/config"

	"github.com/gin-gonic/gin"
)

var markdownAgentFilenameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*\.md$`)

// MarkdownAgentsHandler 管理 agents 目录下子代理 Markdown（增删改查）。
type MarkdownAgentsHandler struct {
	dir     string
	audit   *audit.Service
	writeMu sync.Mutex
}

// NewMarkdownAgentsHandler dir 须为已解析的绝对路径。
func NewMarkdownAgentsHandler(dir string) *MarkdownAgentsHandler {
	return &MarkdownAgentsHandler{dir: strings.TrimSpace(dir)}
}

// SetAudit wires platform audit logging.
func (h *MarkdownAgentsHandler) SetAudit(s *audit.Service) {
	h.audit = s
}

func (h *MarkdownAgentsHandler) safeJoin(filename string) (string, error) {
	filename = strings.TrimSpace(filename)
	if filename == "" || !markdownAgentFilenameRe.MatchString(filename) {
		return "", fmt.Errorf("非法文件名")
	}
	clean := filepath.Clean(filename)
	if clean != filename || strings.Contains(clean, "..") {
		return "", fmt.Errorf("非法文件名")
	}
	return filepath.Join(h.dir, clean), nil
}

// existingOtherOrchestrator 若目录中已有同槽位的其他主代理文件，返回其文件名；writingBasename 为当前正在写入的文件名时不冲突。
func existingOtherOrchestrator(dir, writingBasename string) (other string, err error) {
	load, err := agents.LoadMarkdownAgentsDir(dir)
	if err != nil {
		return "", err
	}
	wb := filepath.Base(strings.TrimSpace(writingBasename))
	switch agents.OrchestratorMarkdownKind(wb) {
	case "plan_execute":
		if load.OrchestratorPlanExecute != nil && !strings.EqualFold(load.OrchestratorPlanExecute.Filename, wb) {
			return load.OrchestratorPlanExecute.Filename, nil
		}
	case "supervisor":
		if load.OrchestratorSupervisor != nil && !strings.EqualFold(load.OrchestratorSupervisor.Filename, wb) {
			return load.OrchestratorSupervisor.Filename, nil
		}
	case "deep":
		if load.Orchestrator != nil && !strings.EqualFold(load.Orchestrator.Filename, wb) {
			return load.Orchestrator.Filename, nil
		}
	default:
		if load.Orchestrator != nil && !strings.EqualFold(load.Orchestrator.Filename, wb) {
			return load.Orchestrator.Filename, nil
		}
	}
	return "", nil
}

// ListMarkdownAgents GET /api/multi-agent/markdown-agents
func (h *MarkdownAgentsHandler) ListMarkdownAgents(c *gin.Context) {
	if h.dir == "" {
		c.JSON(http.StatusOK, gin.H{"agents": []any{}, "dir": "", "error": i18nText(requestWantsEnglish(c), "未配置 agents 目录", "agents directory not configured")})
		return
	}
	files, err := agents.LoadMarkdownAgentFiles(h.dir)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	out := make([]gin.H, 0, len(files))
	for _, fa := range files {
		sub := fa.Config
		out = append(out, gin.H{
			"filename":        fa.Filename,
			"id":              sub.ID,
			"name":            sub.Name,
			"description":     sub.Description,
			"is_orchestrator": fa.IsOrchestrator,
			"kind":            sub.Kind,
		})
	}
	c.JSON(http.StatusOK, gin.H{"agents": out, "dir": h.dir})
}

// GetMarkdownAgent GET /api/multi-agent/markdown-agents/:filename
func (h *MarkdownAgentsHandler) GetMarkdownAgent(c *gin.Context) {
	filename := c.Param("filename")
	path, err := h.safeJoin(filename)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			respondError(c, http.StatusNotFound, "文件不存在", "file not found")
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	sub, err := agents.ParseMarkdownSubAgent(filename, string(b))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	isOrch := agents.IsOrchestratorLikeMarkdown(filename, sub.Kind)
	c.JSON(http.StatusOK, gin.H{
		"filename":        filename,
		"raw":             string(b),
		"id":              sub.ID,
		"name":            sub.Name,
		"description":     sub.Description,
		"tools":           sub.RoleTools,
		"instruction":     sub.Instruction,
		"bind_role":       sub.BindRole,
		"max_iterations":  sub.MaxIterations,
		"kind":            sub.Kind,
		"is_orchestrator": isOrch,
	})
}

type markdownAgentBody struct {
	Filename      string   `json:"filename"`
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Tools         []string `json:"tools"`
	Instruction   string   `json:"instruction"`
	BindRole      string   `json:"bind_role"`
	MaxIterations int      `json:"max_iterations"`
	Kind          string   `json:"kind"`
	Raw           string   `json:"raw"`
}

// CreateMarkdownAgent POST /api/multi-agent/markdown-agents
func (h *MarkdownAgentsHandler) CreateMarkdownAgent(c *gin.Context) {
	h.writeMu.Lock()
	defer h.writeMu.Unlock()
	if h.dir == "" {
		respondError(c, http.StatusBadRequest, "未配置 agents 目录", "agents directory not configured")
		return
	}
	var body markdownAgentBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	filename := strings.TrimSpace(body.Filename)
	if filename == "" {
		if strings.EqualFold(strings.TrimSpace(body.Kind), "orchestrator") {
			filename = agents.OrchestratorMarkdownFilename
		} else {
			base := agents.SlugID(body.Name)
			if base == "" {
				base = "agent"
			}
			filename = base + ".md"
		}
	}
	path, err := h.safeJoin(filename)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if _, err := os.Stat(path); err == nil {
		respondError(c, http.StatusConflict, "文件已存在", "file already exists")
		return
	}
	sub := config.MultiAgentSubConfig{
		ID:            strings.TrimSpace(body.ID),
		Name:          strings.TrimSpace(body.Name),
		Description:   strings.TrimSpace(body.Description),
		Instruction:   strings.TrimSpace(body.Instruction),
		RoleTools:     body.Tools,
		BindRole:      strings.TrimSpace(body.BindRole),
		MaxIterations: body.MaxIterations,
		Kind:          strings.TrimSpace(body.Kind),
	}
	base := filepath.Base(path)
	if (strings.EqualFold(base, agents.OrchestratorMarkdownFilename) ||
		strings.EqualFold(base, agents.OrchestratorPlanExecuteMarkdownFilename) ||
		strings.EqualFold(base, agents.OrchestratorSupervisorMarkdownFilename)) && sub.Kind == "" {
		sub.Kind = "orchestrator"
	}
	if sub.ID == "" {
		sub.ID = agents.SlugID(sub.Name)
	}
	if sub.Name == "" {
		respondError(c, http.StatusBadRequest, "name 必填", "name is required")
		return
	}
	var out []byte
	if strings.TrimSpace(body.Raw) != "" {
		out = []byte(body.Raw)
	} else {
		out, err = agents.BuildMarkdownFile(sub)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	if err := h.validateMarkdownWrite(filepath.Base(path), out); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if want := agents.WantsMarkdownOrchestrator(filepath.Base(path), body.Kind, string(out)); want {
		other, oerr := existingOtherOrchestrator(h.dir, filepath.Base(path))
		if oerr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": oerr.Error()})
			return
		}
		if other != "" {
			c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf(i18nText(requestWantsEnglish(c), "已存在主代理定义：%s，请先删除或取消其主代理标记", "a primary agent definition already exists: %s; delete it or unset its primary-agent mark first"), other)})
			return
		}
	}
	if err := os.MkdirAll(h.dir, 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		if os.IsExist(err) {
			respondError(c, http.StatusConflict, "文件已存在", "file already exists")
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	_, writeErr := f.Write(out)
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(path)
		respondError(c, http.StatusInternalServerError, "写入智能体文件失败", "failed to write agent file")
		return
	}
	if h.audit != nil {
		h.audit.RecordOK(c, "agent", "markdown_create", "创建 Markdown 子代理", "markdown_agent", filepath.Base(path), nil)
	}
	c.JSON(http.StatusOK, gin.H{"filename": filepath.Base(path), "message": i18nText(requestWantsEnglish(c), "已创建", "created")})
}

// UpdateMarkdownAgent PUT /api/multi-agent/markdown-agents/:filename
func (h *MarkdownAgentsHandler) UpdateMarkdownAgent(c *gin.Context) {
	h.writeMu.Lock()
	defer h.writeMu.Unlock()
	filename := c.Param("filename")
	path, err := h.safeJoin(filename)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if _, err := os.Stat(path); err != nil {
		respondError(c, http.StatusNotFound, "文件不存在", "file not found")
		return
	}
	var body markdownAgentBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	sub := config.MultiAgentSubConfig{
		ID:            strings.TrimSpace(body.ID),
		Name:          strings.TrimSpace(body.Name),
		Description:   strings.TrimSpace(body.Description),
		Instruction:   strings.TrimSpace(body.Instruction),
		RoleTools:     body.Tools,
		BindRole:      strings.TrimSpace(body.BindRole),
		MaxIterations: body.MaxIterations,
		Kind:          strings.TrimSpace(body.Kind),
	}
	if (strings.EqualFold(filename, agents.OrchestratorMarkdownFilename) ||
		strings.EqualFold(filename, agents.OrchestratorPlanExecuteMarkdownFilename) ||
		strings.EqualFold(filename, agents.OrchestratorSupervisorMarkdownFilename)) && sub.Kind == "" {
		sub.Kind = "orchestrator"
	}
	if sub.Name == "" {
		respondError(c, http.StatusBadRequest, "name 必填", "name is required")
		return
	}
	if sub.ID == "" {
		sub.ID = agents.SlugID(sub.Name)
	}
	var out []byte
	if strings.TrimSpace(body.Raw) != "" {
		out = []byte(body.Raw)
	} else {
		out, err = agents.BuildMarkdownFile(sub)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	if err := h.validateMarkdownWrite(filename, out); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if want := agents.WantsMarkdownOrchestrator(filename, body.Kind, string(out)); want {
		other, oerr := existingOtherOrchestrator(h.dir, filename)
		if oerr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": oerr.Error()})
			return
		}
		if other != "" {
			c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf(i18nText(requestWantsEnglish(c), "已存在主代理定义：%s，请先删除或取消其主代理标记", "a primary agent definition already exists: %s; delete it or unset its primary-agent mark first"), other)})
			return
		}
	}
	if err := os.WriteFile(path, out, 0644); err != nil {
		if os.IsNotExist(err) {
			respondError(c, http.StatusNotFound, "文件不存在", "file not found")
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if h.audit != nil {
		h.audit.RecordOK(c, "agent", "markdown_update", "更新 Markdown 子代理", "markdown_agent", filename, nil)
	}
	c.JSON(http.StatusOK, gin.H{"message": i18nText(requestWantsEnglish(c), "已保存", "saved")})
}

// DeleteMarkdownAgent DELETE /api/multi-agent/markdown-agents/:filename
func (h *MarkdownAgentsHandler) DeleteMarkdownAgent(c *gin.Context) {
	filename := c.Param("filename")
	path, err := h.safeJoin(filename)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			respondError(c, http.StatusNotFound, "文件不存在", "file not found")
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if h.audit != nil {
		h.audit.RecordOK(c, "agent", "markdown_delete", "删除 Markdown 子代理", "markdown_agent", filename, nil)
	}
	c.JSON(http.StatusOK, gin.H{"message": i18nText(requestWantsEnglish(c), "已删除", "deleted")})
}
