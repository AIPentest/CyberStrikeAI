package handler

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/audit"
	"cyberstrike-ai/internal/authctx"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/mcp/builtin"
	"cyberstrike-ai/internal/multiagent"
	"cyberstrike-ai/internal/openai"
	"cyberstrike-ai/internal/reasoning"
	"cyberstrike-ai/internal/security"

	"github.com/gin-gonic/gin"
	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
)

// safeTruncateString 安全截断字符串，避免在 UTF-8 字符中间截断
func safeTruncateString(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= maxLen {
		return s
	}

	// 将字符串转换为 rune 切片以正确计算字符数
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}

	// 截断到最大长度
	truncated := string(runes[:maxLen])

	// 尝试在标点符号或空格处截断，使截断更自然
	// 在截断点往前查找合适的断点（不超过20%的长度）
	searchRange := maxLen / 5
	if searchRange > maxLen {
		searchRange = maxLen
	}
	breakChars := []rune("，。、 ,.;:!?！？/\\-_")
	bestBreakPos := len(runes[:maxLen])

	for i := bestBreakPos - 1; i >= bestBreakPos-searchRange && i >= 0; i-- {
		for _, breakChar := range breakChars {
			if runes[i] == breakChar {
				bestBreakPos = i + 1 // 在标点符号后断开
				goto found
			}
		}
	}

found:
	truncated = string(runes[:bestBreakPos])
	return truncated + "..."
}

// responsePlanAgg buffers main-assistant response_stream chunks for one "planning" process_detail row.
type responsePlanAgg struct {
	meta            map[string]interface{}
	b               strings.Builder
	detailID        string
	lastPersistAt   time.Time
	lastPersistSize int
}

// thinkingBuf aggregates thinking_stream_* / reasoning_chain_stream_* before flush to process_details.
type thinkingBuf struct {
	b         strings.Builder
	meta      map[string]interface{}
	persistAs string // "thinking" | "reasoning_chain"
}

func normalizeProcessDetailText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.TrimSpace(s)
}

// discardPlanningIfEchoesToolResult drops buffered planning text when it only repeats the
// upcoming tool_result body. Streaming models often echo tool stdout in chunk.Content; flushing
// that into "planning" before persisting tool_result duplicates the output after page refresh.
// sameResponseStreamMeta 判断是否为同一段主通道流（Eino ADK 可能对同一 MessageStream 重复发 response_start）。
func sameResponseStreamMeta(a, b map[string]interface{}) bool {
	if a == nil || b == nil {
		return false
	}
	agentA, _ := a["einoAgent"].(string)
	agentB, _ := b["einoAgent"].(string)
	agentA = strings.TrimSpace(agentA)
	agentB = strings.TrimSpace(agentB)
	if agentA == "" || !strings.EqualFold(agentA, agentB) {
		return false
	}
	orchA, _ := a["orchestration"].(string)
	orchB, _ := b["orchestration"].(string)
	if strings.TrimSpace(orchA) != strings.TrimSpace(orchB) {
		return false
	}
	iterA := responseStreamIterationFromMeta(a)
	iterB := responseStreamIterationFromMeta(b)
	if iterA != 0 && iterB != 0 && iterA != iterB {
		return false
	}
	streamA, _ := a["streamId"].(string)
	streamB, _ := b["streamId"].(string)
	streamA = strings.TrimSpace(streamA)
	streamB = strings.TrimSpace(streamB)
	if streamA != "" && streamB != "" && streamA != streamB {
		return false
	}
	return true
}

func responseStreamIterationFromMeta(m map[string]interface{}) int {
	if m == nil {
		return 0
	}
	switch v := m["iteration"].(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}

func discardPlanningIfEchoesToolResult(respPlan *responsePlanAgg, toolData interface{}) string {
	if respPlan == nil {
		return ""
	}
	plan := normalizeProcessDetailText(respPlan.b.String())
	if plan == "" {
		return ""
	}
	dataMap, ok := toolData.(map[string]interface{})
	if !ok {
		return ""
	}
	res, ok := dataMap["result"].(string)
	if !ok {
		return ""
	}
	r := normalizeProcessDetailText(res)
	if r == "" {
		return ""
	}
	if plan == r || strings.HasSuffix(plan, r) {
		detailID := respPlan.detailID
		respPlan.meta = nil
		respPlan.b.Reset()
		respPlan.detailID = ""
		respPlan.lastPersistAt = time.Time{}
		respPlan.lastPersistSize = 0
		return detailID
	}
	return ""
}

// AgentHandler Agent处理器
type AgentHandler struct {
	agent            *agent.Agent
	db               *database.DB
	logger           *zap.Logger
	tasks            *AgentTaskManager
	taskEventBus     *TaskEventBus // 镜像 SSE 事件，供刷新后订阅同一运行中任务
	batchTaskManager *BatchTaskManager
	hitlManager      *HITLManager
	config           *config.Config // 配置引用，用于获取角色信息
	knowledgeManager interface {    // 知识库管理器接口
		LogRetrieval(conversationID, messageID, query, riskType string, retrievedItems []string) error
	}
	agentsMarkdownDir string // 多代理：Markdown 子 Agent 目录（绝对路径，空则不从磁盘合并）
	batchCronParser   cron.Parser
	// hitlWhitelistSaver 侧栏「应用」HITL 时将会话增量白名单合并写入 config.yaml（可选）
	hitlWhitelistSaver       HitlToolWhitelistSaver
	hitlStrategySaver        HitlAuditStrategySaver
	hitlDefaultReviewerSaver HitlDefaultReviewerSaver
	auditLLM                 *openai.Client
	audit                    *audit.Service
}

// SetAudit wires platform audit logging.
func (h *AgentHandler) SetAudit(s *audit.Service) {
	h.audit = s
}

// TaskManager 返回 Agent 任务管理器（供 MCP 监控页终止 Eino execute 等）。
func (h *AgentHandler) TaskManager() *AgentTaskManager {
	if h == nil {
		return nil
	}
	return h.tasks
}

// CancelRunningTaskForConversation stops any in-flight agent work for the conversation (idempotent).
func (h *AgentHandler) CancelRunningTaskForConversation(conversationID string) {
	if h == nil || conversationID == "" || h.tasks == nil {
		return
	}
	h.cancelRunningMCPToolsForConversation(conversationID)
	h.tasks.AbortActiveEinoExecute(conversationID, "")
	if ok, err := h.tasks.CancelTask(conversationID, ErrTaskCancelled); ok {
		h.logger.Info("已取消会话运行中任务", zap.String("conversationId", conversationID))
	} else if err != nil {
		h.logger.Warn("取消会话运行中任务失败", zap.String("conversationId", conversationID), zap.Error(err))
	}
}

// ConversationTaskRuntimeState exposes the authoritative live state and start
// time used to scope persisted TaskCreate files to the current run. A task
// already entering cancellation must stop driving progress UI immediately.
func (h *AgentHandler) ConversationTaskRuntimeState(conversationID string) (bool, time.Time) {
	if h == nil || h.tasks == nil || strings.TrimSpace(conversationID) == "" {
		return false, time.Time{}
	}
	task := h.tasks.GetTaskSnapshot(strings.TrimSpace(conversationID))
	if task == nil || !strings.EqualFold(strings.TrimSpace(task.Status), "running") {
		return false, time.Time{}
	}
	return true, task.StartedAt
}

func (h *AgentHandler) cancelRunningMCPToolsForConversation(conversationID string) {
	if h == nil || h.agent == nil {
		return
	}
	n := h.agent.CancelRunningMCPToolsForConversation(conversationID, "会话已结束，自动终止仍在运行的工具")
	if n > 0 && h.logger != nil {
		h.logger.Info("已终止会话仍在运行的 MCP 工具", zap.String("conversationId", conversationID), zap.Int("count", n))
	}
}

// HitlToolWhitelistSaver 合并/设置 HITL 免审批工具到全局配置并落盘
type HitlToolWhitelistSaver interface {
	MergeHitlToolWhitelistIntoConfig(add []string) error
	SetHitlToolWhitelist(tools []string) error
}

// NewAgentHandler 创建新的Agent处理器
func NewAgentHandler(agent *agent.Agent, db *database.DB, cfg *config.Config, logger *zap.Logger) *AgentHandler {
	batchTaskManager := NewBatchTaskManager(logger)
	batchTaskManager.SetDB(db)

	// 从数据库加载所有批量任务队列
	if err := batchTaskManager.LoadFromDB(); err != nil {
		logger.Warn("从数据库加载批量任务队列失败", zap.Error(err))
	}

	bus := NewTaskEventBus()
	tm := NewAgentTaskManager()
	tm.SetTaskEventBus(bus)
	llmHTTP := &http.Client{Timeout: 2 * time.Minute}
	var llmCfg *config.OpenAIConfig
	if cfg != nil {
		llmCfg = &cfg.OpenAI
	}
	handler := &AgentHandler{
		agent:            agent,
		db:               db,
		logger:           logger,
		tasks:            tm,
		taskEventBus:     bus,
		batchTaskManager: batchTaskManager,
		config:           cfg,
		hitlManager:      NewHITLManager(db, logger),
		batchCronParser:  cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor),
		auditLLM:         openai.NewClient(llmCfg, llmHTTP, logger),
	}
	tm.SetToolCanceler(handler.cancelRunningMCPToolsForConversation)
	if err := handler.hitlManager.EnsureSchema(); err != nil {
		logger.Warn("初始化 HITL 表失败", zap.Error(err))
	}
	go handler.batchQueueSchedulerLoop()
	return handler
}

// SetKnowledgeManager 设置知识库管理器（用于记录检索日志）
func (h *AgentHandler) SetKnowledgeManager(manager interface {
	LogRetrieval(conversationID, messageID, query, riskType string, retrievedItems []string) error
}) {
	h.knowledgeManager = manager
}

// SetAgentsMarkdownDir 设置 agents/*.md 子代理目录（绝对路径）；空表示仅使用 config.yaml 中的 sub_agents。
func (h *AgentHandler) SetAgentsMarkdownDir(absDir string) {
	h.agentsMarkdownDir = strings.TrimSpace(absDir)
}

// SetHitlToolWhitelistSaver 设置 HITL 白名单落盘（与 ConfigHandler 配合，避免循环引用用接口）
func (h *AgentHandler) SetHitlToolWhitelistSaver(s HitlToolWhitelistSaver) {
	h.hitlWhitelistSaver = s
}

// HitlDefaultReviewerSaver 持久化全局默认人机协同配置到 config.yaml。
type HitlDefaultReviewerSaver interface {
	UpdateHitlDefaultConfig(mode, reviewer string, timeoutSeconds int) error
	UpdateHitlDefaultReviewer(reviewer string) error
}

// SetHitlDefaultReviewerSaver 设置 HITL 默认配置落盘。
func (h *AgentHandler) SetHitlDefaultReviewerSaver(s HitlDefaultReviewerSaver) {
	h.hitlDefaultReviewerSaver = s
}

func (h *AgentHandler) hitlEffectiveDefaultReviewer() string {
	if h != nil && h.config != nil {
		return normalizeHitlReviewer(h.config.Hitl.EffectiveDefaultReviewer())
	}
	return "human"
}

func (h *AgentHandler) hitlEffectiveDefaultMode() string {
	if h != nil && h.config != nil {
		return normalizeHitlDefaultMode(h.config.Hitl.EffectiveDefaultMode())
	}
	return "off"
}

func (h *AgentHandler) hitlEffectiveDefaultTimeoutSeconds() int {
	if h != nil && h.config != nil {
		timeout := h.config.Hitl.EffectiveDefaultTimeoutSeconds()
		if timeout < 0 {
			return 0
		}
		return timeout
	}
	return 300
}

func (h *AgentHandler) hitlEffectiveDefaultRequest() *HITLRequest {
	mode := h.hitlEffectiveDefaultMode()
	return &HITLRequest{
		Enabled:        mode != "off",
		Mode:           mode,
		Reviewer:       h.hitlEffectiveDefaultReviewer(),
		SensitiveTools: []string{},
		TimeoutSeconds: h.hitlEffectiveDefaultTimeoutSeconds(),
	}
}

// HITLNeedsToolApproval 供 C2 危险任务门控：与会话侧人机协同及免审批白名单判定一致。
func (h *AgentHandler) HITLNeedsToolApproval(conversationID, toolName string) bool {
	if h == nil || h.hitlManager == nil {
		return false
	}
	return h.hitlManager.NeedsToolApproval(conversationID, toolName)
}

// ChatAttachment 聊天附件（用户上传的文件）
type ChatAttachment struct {
	FileName   string `json:"fileName"`          // 展示用文件名
	Content    string `json:"content,omitempty"` // 文本或 base64；若已预先上传到服务器可留空
	MimeType   string `json:"mimeType,omitempty"`
	ServerPath string `json:"serverPath,omitempty"` // 已保存在 chat_uploads 下的绝对路径（由 POST /api/chat-uploads 返回）
}

// ChatReasoningRequest 对话页「模型推理」意图（Eino 单/多代理路径消费）。
type ChatReasoningRequest struct {
	// Mode: default（跟随系统）| off | on | auto
	Mode string `json:"mode,omitempty"`
	// Effort: low | medium | high | max | xhigh（原样下发；不同网关最高档命名不同）。空表示不指定。
	Effort string `json:"effort,omitempty"`
}

// ChatFinalizationRequest is a caller-provided delivery policy. The server does
// not infer execution intent from natural-language user text.
type ChatFinalizationRequest struct {
	RequireExecutionEvidence *bool `json:"requireExecutionEvidence,omitempty"`
}

// ChatRequest 聊天请求
type ChatRequest struct {
	Message              string                  `json:"message" binding:"required"`
	ConversationID       string                  `json:"conversationId,omitempty"`
	ProjectID            string                  `json:"projectId,omitempty"` // 新对话绑定的项目（可选；未指定时可用 config.project.default_project_id）
	Role                 string                  `json:"role,omitempty"`      // 角色名称
	Attachments          []ChatAttachment        `json:"attachments,omitempty"`
	WebShellConnectionID string                  `json:"webshellConnectionId,omitempty"` // WebShell 管理 - AI 助手：当前选中的连接 ID，仅使用 webshell_* 工具
	AIChannelID          string                  `json:"aiChannelId,omitempty"`          // 会话级 AI 通道；空则使用 ai.default_channel
	Hitl                 *HITLRequest            `json:"hitl,omitempty"`
	Reasoning            *ChatReasoningRequest   `json:"reasoning,omitempty"`
	Finalization         ChatFinalizationRequest `json:"finalization,omitempty"`
	// Orchestration 仅对 /api/multi-agent、/api/multi-agent/stream：deep | plan_execute | supervisor；空则等同 deep。机器人/批量等无请求体时由服务端默认 deep。/api/eino-agent* 不使用此字段。
	Orchestration string `json:"orchestration,omitempty"`
}

func (h *AgentHandler) configForAIChannel(channelID string) (*config.Config, string, error) {
	if h == nil || h.config == nil {
		return nil, "", fmt.Errorf("服务器配置未加载")
	}
	oa, resolvedID, ok := h.config.ResolveAIChannel(channelID)
	if !ok {
		return nil, resolvedID, fmt.Errorf("AI 通道不存在: %s", resolvedID)
	}
	cfgCopy := *h.config
	cfgCopy.OpenAI = oa
	return &cfgCopy, resolvedID, nil
}

func chatReasoningToClientIntent(r *ChatReasoningRequest) *reasoning.ClientIntent {
	if r == nil {
		return nil
	}
	return &reasoning.ClientIntent{Mode: r.Mode, Effort: r.Effort}
}

type HITLRequest struct {
	Enabled        bool     `json:"enabled"`
	Mode           string   `json:"mode,omitempty"`
	Reviewer       string   `json:"reviewer,omitempty"` // human | audit_agent
	SensitiveTools []string `json:"sensitiveTools,omitempty"`
	TimeoutSeconds int      `json:"timeoutSeconds,omitempty"`
}

const (
	maxAttachments     = 10
	chatUploadsDirName = "chat_uploads" // 对话附件保存的根目录（相对当前工作目录）
)

// validateChatAttachmentServerPath 校验绝对路径落在工作目录 chat_uploads 下且为普通文件（防路径穿越）
func validateChatAttachmentServerPath(abs string) (string, error) {
	p := strings.TrimSpace(abs)
	if p == "" {
		return "", fmt.Errorf("empty path")
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("获取当前工作目录失败: %w", err)
	}
	root := filepath.Join(cwd, chatUploadsDirName)
	rootAbs, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return "", err
	}
	pathAbs, err := filepath.Abs(filepath.Clean(p))
	if err != nil {
		return "", err
	}
	sep := string(filepath.Separator)
	if pathAbs != rootAbs && !strings.HasPrefix(pathAbs, rootAbs+sep) {
		return "", fmt.Errorf("path outside chat_uploads")
	}
	st, err := os.Stat(pathAbs)
	if err != nil {
		return "", err
	}
	if st.IsDir() {
		return "", fmt.Errorf("not a regular file")
	}
	return pathAbs, nil
}

// avoidChatUploadDestCollision 若 path 已存在则生成带时间戳+随机后缀的新文件名（与上传接口命名风格一致）
func avoidChatUploadDestCollision(path string) string {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return path
	}
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	nameNoExt := strings.TrimSuffix(base, ext)
	suffix := fmt.Sprintf("_%s_%s", time.Now().Format("150405"), shortRand(6))
	var unique string
	if ext != "" {
		unique = nameNoExt + suffix + ext
	} else {
		unique = base + suffix
	}
	return filepath.Join(dir, unique)
}

// relocateManualOrNewUploadToConversation 无会话 ID 时前端会上传到 …/日期/_manual；首条消息创建会话后，将文件移入 …/日期/{conversationId}/ 以便按对话隔离。
func relocateManualOrNewUploadToConversation(absPath, conversationID string, logger *zap.Logger) (string, error) {
	conv := strings.TrimSpace(conversationID)
	if conv == "" {
		return absPath, nil
	}
	convSan := strings.ReplaceAll(conv, string(filepath.Separator), "_")
	if convSan == "" || convSan == "_manual" || convSan == "_new" {
		return absPath, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return absPath, err
	}
	rootAbs, err := filepath.Abs(filepath.Join(cwd, chatUploadsDirName))
	if err != nil {
		return absPath, err
	}
	rel, err := filepath.Rel(rootAbs, absPath)
	if err != nil {
		return absPath, nil
	}
	rel = filepath.ToSlash(filepath.Clean(rel))
	var segs []string
	for _, p := range strings.Split(rel, "/") {
		if p != "" && p != "." {
			segs = append(segs, p)
		}
	}
	// 仅处理扁平结构：日期/_manual|_new/文件名
	if len(segs) != 3 {
		return absPath, nil
	}
	datePart, placeFolder, baseName := segs[0], segs[1], segs[2]
	if placeFolder != "_manual" && placeFolder != "_new" {
		return absPath, nil
	}
	targetDir := filepath.Join(rootAbs, datePart, convSan)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return "", fmt.Errorf("创建会话附件目录失败: %w", err)
	}
	dest := filepath.Join(targetDir, baseName)
	dest = avoidChatUploadDestCollision(dest)
	if err := os.Rename(absPath, dest); err != nil {
		return "", fmt.Errorf("将附件移入会话目录失败: %w", err)
	}
	out, _ := filepath.Abs(dest)
	if logger != nil {
		logger.Info("对话附件已从占位目录移入会话目录",
			zap.String("from", absPath),
			zap.String("to", out),
			zap.String("conversationId", conv))
	}
	return out, nil
}

// saveAttachmentsToDateAndConversationDir 处理附件：若带 serverPath 则仅校验已存在文件；否则将 content 写入 chat_uploads/YYYY-MM-DD/{conversationID}/。
// conversationID 为空时使用 "_new" 作为目录名（新对话尚未有 ID）
func saveAttachmentsToDateAndConversationDir(attachments []ChatAttachment, conversationID string, logger *zap.Logger) (savedPaths []string, err error) {
	if len(attachments) == 0 {
		return nil, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("获取当前工作目录失败: %w", err)
	}
	dateDir := filepath.Join(cwd, chatUploadsDirName, time.Now().Format("2006-01-02"))
	convDirName := strings.TrimSpace(conversationID)
	if convDirName == "" {
		convDirName = "_new"
	} else {
		convDirName = strings.ReplaceAll(convDirName, string(filepath.Separator), "_")
	}
	targetDir := filepath.Join(dateDir, convDirName)
	if err = os.MkdirAll(targetDir, 0755); err != nil {
		return nil, fmt.Errorf("创建上传目录失败: %w", err)
	}
	savedPaths = make([]string, 0, len(attachments))
	for i, a := range attachments {
		if sp := strings.TrimSpace(a.ServerPath); sp != "" {
			valid, verr := validateChatAttachmentServerPath(sp)
			if verr != nil {
				return nil, fmt.Errorf("附件 %s: %w", a.FileName, verr)
			}
			finalPath, rerr := relocateManualOrNewUploadToConversation(valid, conversationID, logger)
			if rerr != nil {
				return nil, fmt.Errorf("附件 %s: %w", a.FileName, rerr)
			}
			savedPaths = append(savedPaths, finalPath)
			if logger != nil {
				logger.Debug("对话附件使用已上传路径", zap.Int("index", i+1), zap.String("fileName", a.FileName), zap.String("path", finalPath))
			}
			continue
		}
		if strings.TrimSpace(a.Content) == "" {
			return nil, fmt.Errorf("附件 %s 缺少内容或未提供 serverPath", a.FileName)
		}
		raw, decErr := attachmentContentToBytes(a)
		if decErr != nil {
			return nil, fmt.Errorf("附件 %s 解码失败: %w", a.FileName, decErr)
		}
		baseName := filepath.Base(a.FileName)
		if baseName == "" || baseName == "." {
			baseName = "file"
		}
		baseName = strings.ReplaceAll(baseName, string(filepath.Separator), "_")
		ext := filepath.Ext(baseName)
		nameNoExt := strings.TrimSuffix(baseName, ext)
		suffix := fmt.Sprintf("_%s_%s", time.Now().Format("150405"), shortRand(6))
		var unique string
		if ext != "" {
			unique = nameNoExt + suffix + ext
		} else {
			unique = baseName + suffix
		}
		fullPath := filepath.Join(targetDir, unique)
		if err = os.WriteFile(fullPath, raw, 0644); err != nil {
			return nil, fmt.Errorf("写入文件 %s 失败: %w", a.FileName, err)
		}
		absPath, _ := filepath.Abs(fullPath)
		savedPaths = append(savedPaths, absPath)
		if logger != nil {
			logger.Debug("对话附件已保存", zap.Int("index", i+1), zap.String("fileName", a.FileName), zap.String("path", absPath))
		}
	}
	return savedPaths, nil
}

func shortRand(n int) string {
	const letters = "0123456789abcdef"
	b := make([]byte, n)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = letters[int(b[i])%len(letters)]
	}
	return string(b)
}

func attachmentContentToBytes(a ChatAttachment) ([]byte, error) {
	content := a.Content
	if decoded, err := base64.StdEncoding.DecodeString(content); err == nil && len(decoded) > 0 {
		return decoded, nil
	}
	return []byte(content), nil
}

// userMessageContentForStorage 返回要存入数据库的用户消息内容：有附件时在正文后追加附件名（及路径），刷新后仍能显示，继续对话时大模型也能从历史中拿到路径
func userMessageContentForStorage(message string, attachments []ChatAttachment, savedPaths []string) string {
	if len(attachments) == 0 {
		return message
	}
	var b strings.Builder
	b.WriteString(message)
	for i, a := range attachments {
		b.WriteString("\n📎 ")
		b.WriteString(a.FileName)
		if i < len(savedPaths) && savedPaths[i] != "" {
			b.WriteString(": ")
			b.WriteString(savedPaths[i])
		}
	}
	return b.String()
}

// appendAttachmentsToMessage 仅将附件的保存路径追加到用户消息末尾，不再内联附件内容，避免上下文过长
func appendAttachmentsToMessage(msg string, attachments []ChatAttachment, savedPaths []string) string {
	if len(attachments) == 0 {
		return msg
	}
	var b strings.Builder
	b.WriteString(msg)
	b.WriteString("\n\n[用户上传的文件]\n")
	for i, a := range attachments {
		if i < len(savedPaths) && savedPaths[i] != "" {
			b.WriteString(fmt.Sprintf("- %s: %s\n", a.FileName, savedPaths[i]))
		} else {
			b.WriteString(fmt.Sprintf("- %s: （路径未知，可能保存失败）\n", a.FileName))
		}
	}
	return b.String()
}

// appendAssistantMessageNotice 在助手消息末尾追加提示，避免覆盖已生成内容。
// 若消息为空则直接写入提示；若已包含相同提示则保持不变。
func (h *AgentHandler) appendAssistantMessageNotice(messageID, notice string) error {
	trimmedNotice := strings.TrimSpace(notice)
	if strings.TrimSpace(messageID) == "" || trimmedNotice == "" {
		return nil
	}
	_, err := h.db.Exec(
		`UPDATE messages
		 SET content = CASE
			WHEN content IS NULL OR TRIM(content) = '' THEN ?
			WHEN INSTR(content, ?) > 0 THEN content
			ELSE content || '\n\n' || ?
		 END,
		     updated_at = ?
		 WHERE id = ?`,
		trimmedNotice,
		trimmedNotice,
		trimmedNotice,
		time.Now(),
		messageID,
	)
	return err
}

// mergeAssistantMessagePartialOnCancel 将取消前已生成的部分回复尽量合并进消息：
// - content 为空或仅占位（处理中...）时，直接替换为 partial；
// - 已有正文时，仅在尚未包含 partial 时追加，避免丢失与重复。
func (h *AgentHandler) mergeAssistantMessagePartialOnCancel(messageID, partial string) error {
	trimmedPartial := strings.TrimSpace(partial)
	if strings.TrimSpace(messageID) == "" || trimmedPartial == "" {
		return nil
	}
	_, err := h.db.Exec(
		`UPDATE messages
		 SET content = CASE
			WHEN content IS NULL OR TRIM(content) = '' OR TRIM(content) = '处理中...' THEN ?
			WHEN INSTR(content, ?) > 0 THEN content
			ELSE content || '\n\n' || ?
		 END,
		     updated_at = ?
		 WHERE id = ?`,
		trimmedPartial,
		trimmedPartial,
		trimmedPartial,
		time.Now(),
		messageID,
	)
	return err
}

// ChatResponse 聊天响应
type ChatResponse struct {
	Response                         string    `json:"response"`
	MCPExecutionIDs                  []string  `json:"mcpExecutionIds,omitempty"` // 本次对话中执行的MCP调用ID列表
	ConversationID                   string    `json:"conversationId"`            // 对话ID
	Time                             time.Time `json:"time"`
	Finalizable                      bool      `json:"finalizable"`
	Finalized                        bool      `json:"finalized"`
	Status                           string    `json:"status,omitempty"`
	CompletionReason                 string    `json:"completionReason,omitempty"`
	EvidenceVerified                 bool      `json:"evidenceVerified"`
	EvidenceRefs                     []string  `json:"evidenceRefs,omitempty"`
	PendingExecutionIDs              []string  `json:"pendingExecutionIds,omitempty"`
	MissingChecks                    []string  `json:"missingChecks,omitempty"`
	AutoCancelledPendingExecutionIDs []string  `json:"autoCancelledPendingExecutionIds,omitempty"`
}

func (h *AgentHandler) finalizeRobotAgentError(ctx context.Context, assistantMessageID, conversationID string, resultMA *multiagent.RunResult, errMA error) (string, string, error) {
	if shouldPersistEinoAgentTraceAfterRunError(ctx) {
		h.persistEinoAgentTraceForResume(conversationID, resultMA)
	}
	errMsg := "执行失败: " + multiagent.EinoClientRunErrorMessage(errMA)
	if assistantMessageID != "" {
		_, _ = h.db.Exec("UPDATE messages SET content = ?, updated_at = ? WHERE id = ?", errMsg, time.Now(), assistantMessageID)
		_ = h.db.AddProcessDetail(assistantMessageID, conversationID, "error", errMsg, nil)
	}
	return "", conversationID, errMA
}

func (h *AgentHandler) finalizeRobotAgentSuccess(taskCtx context.Context, assistantMessageID, conversationID string, resultMA *multiagent.RunResult) (string, string, error) {
	reasoningContent := multiagent.AggregatedReasoningFromTraceJSON(resultMA.LastAgentTraceInput)
	decision := h.decideAgentRunForDeliveryWithPolicy(conversationID, assistantMessageID, "robot", resultMA, resultMA.MCPExecutionIDs, true)
	if cancelled := h.cleanupPendingToolExecutionsAfterIteration(taskCtx, conversationID, decision, nil); len(cancelled) > 0 {
		decision = h.decideAgentRunForDeliveryWithPolicy(conversationID, assistantMessageID, "robot", resultMA, resultMA.MCPExecutionIDs, true)
	}
	h.persistFinalizationDecision(conversationID, assistantMessageID, "robot", resultMA.MCPExecutionIDs, reasoningContent, decision)
	responseText := decision.FinalText
	if !decision.Finalizable {
		responseText = finalizationBlockedMessage(decision)
	}
	if assistantMessageID == "" {
		if _, err := h.db.AddMessage(conversationID, "assistant", responseText, resultMA.MCPExecutionIDs); err != nil {
			h.logger.Warn("机器人：保存助手消息失败", zap.Error(err))
		}
	}
	if resultMA.LastAgentTraceInput != "" || resultMA.LastAgentTraceOutput != "" {
		_ = h.db.SaveAgentTrace(conversationID, resultMA.LastAgentTraceInput, resultMA.LastAgentTraceOutput)
	}
	return responseText, conversationID, nil
}

func (h *AgentHandler) runRobotEinoSingleWithRetry(
	taskCtx context.Context,
	conversationID, finalMessage string,
	history []agent.ChatMessage,
	roleTools []string,
	progressCallback agent.ProgressCallback,
	assistantMessageID string,
	taskStatus *string,
) (string, string, error) {
	resultMA, errMA := multiagent.RunEinoSingleChatModelAgent(
		taskCtx, h.config, &h.config.MultiAgent, h.agent, h.db, h.logger,
		conversationID, h.conversationProjectID(conversationID), finalMessage, history, roleTools, progressCallback, nil, h.agentSessionContextBlock(conversationID),
	)
	if errMA != nil {
		*taskStatus = "failed"
		return h.finalizeRobotAgentError(taskCtx, assistantMessageID, conversationID, resultMA, errMA)
	}
	return h.finalizeRobotAgentSuccess(taskCtx, assistantMessageID, conversationID, resultMA)
}

func (h *AgentHandler) runRobotMultiAgentWithRetry(
	taskCtx context.Context,
	conversationID, finalMessage, orchestration string,
	history []agent.ChatMessage,
	roleTools []string,
	progressCallback agent.ProgressCallback,
	assistantMessageID string,
	taskStatus *string,
) (string, string, error) {
	resultMA, errMA := multiagent.RunDeepAgent(
		taskCtx, h.config, &h.config.MultiAgent, h.agent, h.db, h.logger,
		conversationID, h.conversationProjectID(conversationID), finalMessage, history, roleTools, progressCallback,
		h.agentsMarkdownDir, orchestration, nil, h.agentSessionContextBlock(conversationID),
	)
	if errMA != nil {
		*taskStatus = "failed"
		return h.finalizeRobotAgentError(taskCtx, assistantMessageID, conversationID, resultMA, errMA)
	}
	return h.finalizeRobotAgentSuccess(taskCtx, assistantMessageID, conversationID, resultMA)
}

// ProcessMessageForRobot 供机器人（企业微信/钉钉/飞书）调用：Eino 单/多代理执行路径（含 progressCallback、过程详情），仅不发送 SSE，最后返回完整回复
func (h *AgentHandler) ProcessMessageForRobot(ctx context.Context, platform string, principal authctx.Principal, conversationID, message, role, agentMode string) (response string, convID string, err error) {
	ownerUserID := strings.TrimSpace(principal.UserID)
	if ownerUserID == "" {
		return "", "", fmt.Errorf("authenticated robot principal is required")
	}
	if !principal.HasPermission("agent:execute") || !principal.HasPermission("chat:read") || !principal.HasPermission("chat:write") {
		return "", "", fmt.Errorf("机器人账号缺少 agent:execute、chat:read 或 chat:write 权限")
	}
	ctx = authctx.WithPrincipal(ctx, principal)
	if conversationID == "" {
		title := safeTruncateString(message, 50)
		src := "robot"
		if strings.TrimSpace(platform) != "" {
			src = "robot:" + strings.TrimSpace(platform)
		}
		meta := audit.ConversationCreateMeta(src)
		meta.ProjectID = effectiveProjectID(h.config, "")
		if meta.ProjectID != "" && (!principal.HasPermission("project:read") || !h.db.UserCanAccessResource(ownerUserID, principal.ScopeFor("project:read"), "project", meta.ProjectID)) {
			meta.ProjectID = ""
		}
		conv, createErr := h.db.CreateConversation(title, meta)
		if createErr != nil {
			return "", "", fmt.Errorf("创建对话失败: %w", createErr)
		}
		conversationID = conv.ID
		_ = h.db.SetResourceOwner("conversation", conversationID, ownerUserID)
	} else {
		if _, getErr := h.db.GetConversation(conversationID); getErr != nil || !h.db.UserCanAccessResource(ownerUserID, principal.ScopeFor("chat:write"), "conversation", conversationID) {
			return "", "", fmt.Errorf("对话不存在")
		}
	}

	agentHistoryMessages, err := h.loadHistoryFromAgentTrace(conversationID)
	if err != nil {
		historyMessages, getErr := h.db.GetMessages(conversationID)
		if getErr != nil {
			agentHistoryMessages = []agent.ChatMessage{}
		} else {
			agentHistoryMessages = make([]agent.ChatMessage, 0, len(historyMessages))
			for _, msg := range historyMessages {
				agentHistoryMessages = append(agentHistoryMessages, agent.ChatMessage{Role: msg.Role, Content: msg.Content})
			}
		}
	}

	finalMessage := message
	var roleTools []string
	if role != "" && role != "默认" && h.config.Roles != nil {
		if r, exists := h.config.Roles[role]; exists && r.Enabled {
			if r.UserPrompt != "" {
				finalMessage = r.UserPrompt + "\n\n" + message
			}
			roleTools = r.Tools
		}
	}

	if _, err = h.db.AddMessage(conversationID, "user", message, nil); err != nil {
		return "", "", fmt.Errorf("保存用户消息失败: %w", err)
	}

	// 与 Eino 流式对话一致：先创建助手消息占位，用 progressCallback 写过程详情（不发送 SSE）
	assistantMsg, err := h.db.AddMessage(conversationID, "assistant", "处理中...", nil)
	if err != nil {
		h.logger.Warn("机器人：创建助手消息占位失败", zap.Error(err))
	}
	var assistantMessageID string
	if assistantMsg != nil {
		assistantMessageID = assistantMsg.ID
	}

	// 注册运行中任务并向 taskEventBus 镜像进度事件，供 Web 端 task-events 补流。
	taskCtx, cancelWithCause := context.WithCancelCause(ctx)
	defer cancelWithCause(nil)
	taskStatus := "completed"
	defer func() {
		h.tasks.FinishTask(conversationID, taskStatus)
	}()
	if _, err := h.tasks.StartTask(conversationID, message, cancelWithCause); err != nil {
		if errors.Is(err, ErrTaskAlreadyRunning) {
			return "", conversationID, fmt.Errorf("当前会话已有任务正在执行中，请稍后再试")
		}
		return "", conversationID, fmt.Errorf("无法启动任务: %w", err)
	}
	progressCallback := h.createProgressCallback(taskCtx, cancelWithCause, conversationID, assistantMessageID, nil)

	robotMode := config.NormalizeAgentMode(agentMode)
	if err := h.db.SetConversationAgentMode(conversationID, robotMode); err != nil {
		h.logger.Warn("机器人：更新对话模式失败", zap.String("conversationId", conversationID), zap.String("agentMode", robotMode), zap.Error(err))
	}
	switch robotMode {
	case "eino_single":
		return h.runRobotEinoSingleWithRetry(taskCtx, conversationID, finalMessage, agentHistoryMessages, roleTools, progressCallback, assistantMessageID, &taskStatus)
	case "deep", "plan_execute", "supervisor":
		if h.config == nil || !h.config.MultiAgent.Enabled {
			taskStatus = "failed"
			return "", conversationID, fmt.Errorf("机器人对话模式 %s 需要启用 Eino 多代理", robotMode)
		}
		return h.runRobotMultiAgentWithRetry(taskCtx, conversationID, finalMessage, robotMode, agentHistoryMessages, roleTools, progressCallback, assistantMessageID, &taskStatus)
	}

	taskStatus = "failed"
	return "", conversationID, fmt.Errorf("不支持的机器人代理模式: %s", robotMode)
}

// StreamEvent 流式事件
type StreamEvent struct {
	Type    string      `json:"type"`    // conversation, progress, tool_call, tool_result, response, error, cancelled, done
	Message string      `json:"message"` // 显示消息
	Data    interface{} `json:"data,omitempty"`
}

// publishProgressToTaskEventBus 将进度事件镜像到 taskEventBus（机器人/无 HTTP SSE 客户端时供 Web task-events 订阅）。
func (h *AgentHandler) publishProgressToTaskEventBus(conversationID, eventType, message string, data interface{}) {
	if h == nil || h.taskEventBus == nil || strings.TrimSpace(conversationID) == "" {
		return
	}
	event := StreamEvent{Type: eventType, Message: message, Data: data}
	eventJSON, err := json.Marshal(event)
	if err != nil {
		return
	}
	sseLine := make([]byte, 0, len(eventJSON)+8)
	sseLine = append(sseLine, []byte("data: ")...)
	sseLine = append(sseLine, eventJSON...)
	sseLine = append(sseLine, '\n', '\n')
	h.taskEventBus.Publish(conversationID, sseLine)
}

func isInternalEinoDiagnosticProgress(eventType, message string, data interface{}) bool {
	switch eventType {
	case "model_output_rejected":
		return true
	case "progress":
		msg := strings.TrimSpace(message)
		if msg == "Eino TurnLoop 常驻多轮 runtime 已接管本轮会话。" ||
			msg == "Eino TurnLoop 已在安全点切换到用户补充后的下一轮。" ||
			msg == "已将用户补充推入 Eino TurnLoop，正在等待安全点切换…" {
			return true
		}
		m, ok := data.(map[string]interface{})
		if !ok {
			return false
		}
		switch strings.TrimSpace(fmt.Sprint(m["kind"])) {
		case "turn_loop_takeover", "turn_loop_preempted":
			return true
		default:
			return false
		}
	default:
		return false
	}
}

// enrichProgressEventData 为 SSE / taskEventBus 事件补齐 conversationId、messageId，便于前端懒加载过程详情。
func enrichProgressEventData(data interface{}, conversationID, assistantMessageID string) interface{} {
	if strings.TrimSpace(conversationID) == "" && strings.TrimSpace(assistantMessageID) == "" {
		return data
	}
	var m map[string]interface{}
	switch v := data.(type) {
	case map[string]interface{}:
		m = make(map[string]interface{}, len(v)+2)
		for k, val := range v {
			m[k] = val
		}
	case nil:
		m = make(map[string]interface{}, 2)
	default:
		m = map[string]interface{}{"payload": data}
	}
	if id := strings.TrimSpace(assistantMessageID); id != "" {
		if existing, ok := m["messageId"]; !ok || strings.TrimSpace(fmt.Sprint(existing)) == "" {
			m["messageId"] = id
		}
	}
	if id := strings.TrimSpace(conversationID); id != "" {
		if existing, ok := m["conversationId"]; !ok || strings.TrimSpace(fmt.Sprint(existing)) == "" {
			m["conversationId"] = id
		}
	}
	return m
}

// createProgressCallback 创建进度回调函数，用于保存processDetails
// sendEventFunc: 可选的流式事件发送函数，如果为nil则不发送流式事件
func (h *AgentHandler) createProgressCallback(runCtx context.Context, cancelRun context.CancelCauseFunc, conversationID, assistantMessageID string, sendEventFunc func(eventType, message string, data interface{})) agent.ProgressCallback {
	// 用于保存tool_call事件中的参数，以便在tool_result时使用
	toolCallCache := make(map[string]map[string]interface{}) // toolCallId -> arguments
	skillCallCache := make(map[string]string)                // toolCallId -> skillName
	skillToolName := "skill"
	if h.config != nil {
		if customName := strings.TrimSpace(h.config.MultiAgent.EinoSkills.SkillToolName); customName != "" {
			skillToolName = customName
		}
	}

	extractSkillName := func(args map[string]interface{}) string {
		if len(args) == 0 {
			return ""
		}
		for _, key := range []string{"skill_name", "skillName", "name", "skill", "id", "skill_id", "skillId"} {
			if v, ok := args[key]; ok {
				switch vv := v.(type) {
				case string:
					if s := strings.TrimSpace(vv); s != "" {
						return s
					}
				case map[string]interface{}:
					for _, nestedKey := range []string{"name", "id", "skill_name", "skillId"} {
						if nestedV, nestedOK := vv[nestedKey].(string); nestedOK {
							if s := strings.TrimSpace(nestedV); s != "" {
								return s
							}
						}
					}
				}
			}
		}
		return ""
	}

	// thinking_stream_*（ReAct 等助手正文流）与 reasoning_chain_stream_*（Eino ReasoningContent）：
	// 不逐条落库，按 streamId 聚合，flush 时分别落 thinking / reasoning_chain。
	thinkingStreams := make(map[string]*thinkingBuf) // streamId -> buf
	flushedThinking := make(map[string]bool)         // streamId -> flushed
	seenToolCallSigs := make(map[string]string)      // toolCallId -> payload signature
	seenToolResultSigs := make(map[string]string)    // toolCallId -> payload signature

	// progressMu 保护闭包内 map 与聚合状态。Eino parallelRunToolCall 会在多 goroutine 中并发回调
	// progress（ToolInvokeNotifyHolder.Fire → createProgressCallback），未加锁的 map 会触发 fatal panic。
	var progressMu sync.Mutex

	// response_start + response_delta：前端时间线显示为「📝 规划中」（monitor.js），不落逐条 delta；
	// 聚合为一条 planning 写入 process_details，刷新后与线上一致。
	var respPlan responsePlanAgg
	if assistantMessageID != "" {
		h.tasks.SetHitlAssistantMessageID(conversationID, assistantMessageID)
	}
	syncHitlCognition := func() {
		h.syncHitlCognitionFromProgress(conversationID, assistantMessageID, thinkingStreams, &respPlan)
	}
	persistResponsePlan := func(reset bool) {
		if assistantMessageID == "" {
			return
		}
		content := strings.TrimSpace(respPlan.b.String())
		if content == "" {
			if reset {
				respPlan = responsePlanAgg{}
			}
			return
		}
		data := map[string]interface{}{
			"source": "response_stream",
		}
		for k, v := range respPlan.meta {
			data[k] = v
		}
		var err error
		if respPlan.detailID == "" {
			respPlan.detailID, err = h.db.AddProcessDetailWithID(
				assistantMessageID, conversationID, "planning", content, data,
			)
		} else {
			err = h.db.UpdateProcessDetailContent(respPlan.detailID, content, data)
		}
		if err != nil {
			h.logger.Warn("保存过程详情失败", zap.Error(err), zap.String("eventType", "planning"))
		} else {
			respPlan.lastPersistAt = time.Now()
			respPlan.lastPersistSize = respPlan.b.Len()
		}
		syncHitlCognition()
		if reset {
			respPlan = responsePlanAgg{}
		}
	}
	flushResponsePlan := func() { persistResponsePlan(true) }

	flushThinkingStreams := func() {
		if assistantMessageID == "" {
			return
		}
		for sid, tb := range thinkingStreams {
			if sid == "" || flushedThinking[sid] || tb == nil {
				continue
			}
			content := strings.TrimSpace(tb.b.String())
			if content == "" {
				flushedThinking[sid] = true
				continue
			}
			data := map[string]interface{}{
				"streamId": sid,
			}
			for k, v := range tb.meta {
				// 避免覆盖 streamId
				if k == "streamId" {
					continue
				}
				data[k] = v
			}
			persist := tb.persistAs
			if persist != "reasoning_chain" {
				persist = "thinking"
			}
			if err := h.db.AddProcessDetail(assistantMessageID, conversationID, persist, content, data); err != nil {
				h.logger.Warn("保存过程详情失败", zap.Error(err), zap.String("eventType", persist))
			}
			flushedThinking[sid] = true
		}
		syncHitlCognition()
	}

	return func(eventType, message string, data interface{}) {
		progressMu.Lock()
		defer progressMu.Unlock()

		if isInternalEinoDiagnosticProgress(eventType, message, data) {
			return
		}

		// 上游在重试/补偿时可能重复回调相同 tool_call/tool_result。
		// 这里做幂等过滤，保证前端展示和 process_details 都以唯一事件为准。
		if (eventType == "tool_call" || eventType == "tool_result") && data != nil {
			if dataMap, ok := data.(map[string]interface{}); ok {
				toolCallID := strings.TrimSpace(fmt.Sprint(dataMap["toolCallId"]))
				if toolCallID != "" && toolCallID != "<nil>" {
					payloadJSON, _ := json.Marshal(dataMap)
					sig := eventType + "|" + message + "|" + string(payloadJSON)
					seen := seenToolCallSigs
					if eventType == "tool_result" {
						seen = seenToolResultSigs
					}
					if prev, exists := seen[toolCallID]; exists && prev == sig {
						h.logger.Debug("跳过重复工具进度事件",
							zap.String("eventType", eventType),
							zap.String("toolCallId", toolCallID))
						return
					}
					seen[toolCallID] = sig
				}
			}
		}

		// 工具输出片段不在详情区实时展示；完整结果由 tool_result 落库后按需拉取。
		if eventType == "tool_result_delta" {
			return
		}

		deferToolProgressSend := eventType == "tool_call" || eventType == "tool_result"
		// 主 HTTP SSE 与 taskEventBus 必须同时写入：页面刷新会切断原连接，刷新后的
		// GET task-events 订阅依赖 eventBus 才能继续收到后续迭代。机器人等无主 SSE
		// 的来源同样只写 eventBus。工具事件需先落库拿 processDetailId，再发送摘要。
		if !deferToolProgressSend {
			clientData := enrichProgressEventData(data, conversationID, assistantMessageID)
			if sendEventFunc != nil {
				sendEventFunc(eventType, message, clientData)
			}
			h.publishProgressToTaskEventBus(conversationID, eventType, message, clientData)
		}

		// 保存tool_call事件中的参数
		if eventType == "tool_call" {
			if dataMap, ok := data.(map[string]interface{}); ok {
				toolName, _ := dataMap["toolName"].(string)
				if toolName == builtin.ToolSearchKnowledgeBase {
					if toolCallId, ok := dataMap["toolCallId"].(string); ok && toolCallId != "" {
						if argumentsObj, ok := dataMap["argumentsObj"].(map[string]interface{}); ok {
							toolCallCache[toolCallId] = argumentsObj
						}
					}
				}
				if strings.EqualFold(strings.TrimSpace(toolName), skillToolName) {
					toolCallID, _ := dataMap["toolCallId"].(string)
					if toolCallID != "" {
						if argumentsObj, ok := dataMap["argumentsObj"].(map[string]interface{}); ok {
							if skillName := extractSkillName(argumentsObj); skillName != "" {
								skillCallCache[toolCallID] = skillName
							}
						}
					}
				}
			}
		}

		if eventType == "tool_result" {
			if dataMap, ok := data.(map[string]interface{}); ok {
				toolName, _ := dataMap["toolName"].(string)
				toolCallID, _ := dataMap["toolCallId"].(string)
				success := true
				if v, ok := dataMap["success"].(bool); ok {
					success = v
				}
				resultText := ""
				if r, ok := dataMap["result"].(string); ok {
					resultText = r
				}
				if strings.TrimSpace(resultText) == "" {
					resultText = message
				}
				h.recordHitlToolExecutionResult(conversationID, toolCallID, toolName, success, resultText)
			}
		}

		// 处理知识检索日志记录
		if eventType == "tool_result" && h.knowledgeManager != nil {
			if dataMap, ok := data.(map[string]interface{}); ok {
				toolName, _ := dataMap["toolName"].(string)
				if toolName == builtin.ToolSearchKnowledgeBase {
					// 提取检索信息
					query := ""
					riskType := ""
					var retrievedItems []string

					// 首先尝试从tool_call缓存中获取参数
					if toolCallId, ok := dataMap["toolCallId"].(string); ok && toolCallId != "" {
						if cachedArgs, exists := toolCallCache[toolCallId]; exists {
							if q, ok := cachedArgs["query"].(string); ok && q != "" {
								query = q
							}
							if rt, ok := cachedArgs["risk_type"].(string); ok && rt != "" {
								riskType = rt
							}
							// 使用后清理缓存
							delete(toolCallCache, toolCallId)
						}
					}

					// 如果缓存中没有，尝试从argumentsObj中提取
					if query == "" {
						if arguments, ok := dataMap["argumentsObj"].(map[string]interface{}); ok {
							if q, ok := arguments["query"].(string); ok && q != "" {
								query = q
							}
							if rt, ok := arguments["risk_type"].(string); ok && rt != "" {
								riskType = rt
							}
						}
					}

					// 如果query仍然为空，尝试从result中提取（从结果文本的第一行）
					if query == "" {
						if result, ok := dataMap["result"].(string); ok && result != "" {
							// 尝试从结果中提取查询内容（如果结果包含"未找到与查询 'xxx' 相关的知识"）
							if strings.Contains(result, "未找到与查询 '") {
								start := strings.Index(result, "未找到与查询 '") + len("未找到与查询 '")
								end := strings.Index(result[start:], "'")
								if end > 0 {
									query = result[start : start+end]
								}
							}
						}
						// 如果还是为空，使用默认值
						if query == "" {
							query = "未知查询"
						}
					}

					// 从工具结果中提取检索到的知识项ID
					// 结果格式："找到 X 条相关知识：\n\n--- 结果 1 (相似度: XX.XX%) ---\n来源: [分类] 标题\n...\n<!-- METADATA: {...} -->"
					if result, ok := dataMap["result"].(string); ok && result != "" {
						// 尝试从元数据中提取知识项ID
						metadataMatch := strings.Index(result, "<!-- METADATA:")
						if metadataMatch > 0 {
							// 提取元数据JSON
							metadataStart := metadataMatch + len("<!-- METADATA: ")
							metadataEnd := strings.Index(result[metadataStart:], " -->")
							if metadataEnd > 0 {
								metadataJSON := result[metadataStart : metadataStart+metadataEnd]
								var metadata map[string]interface{}
								if err := json.Unmarshal([]byte(metadataJSON), &metadata); err == nil {
									if meta, ok := metadata["_metadata"].(map[string]interface{}); ok {
										if ids, ok := meta["retrievedItemIDs"].([]interface{}); ok {
											retrievedItems = make([]string, 0, len(ids))
											for _, id := range ids {
												if idStr, ok := id.(string); ok {
													retrievedItems = append(retrievedItems, idStr)
												}
											}
										}
									}
								}
							}
						}

						// 如果没有从元数据中提取到，但结果包含"找到 X 条"，至少标记为有结果
						if len(retrievedItems) == 0 && strings.Contains(result, "找到") && !strings.Contains(result, "未找到") {
							// 有结果，但无法准确提取ID，使用特殊标记
							retrievedItems = []string{"_has_results"}
						}
					}

					// 记录检索日志（异步，不阻塞）
					go func() {
						if err := h.knowledgeManager.LogRetrieval(conversationID, assistantMessageID, query, riskType, retrievedItems); err != nil {
							h.logger.Warn("记录知识检索日志失败", zap.Error(err))
						}
					}()

					// 添加知识检索事件到processDetails
					if assistantMessageID != "" {
						retrievalData := map[string]interface{}{
							"query":    query,
							"riskType": riskType,
							"toolName": toolName,
						}
						if err := h.db.AddProcessDetail(assistantMessageID, conversationID, "knowledge_retrieval", fmt.Sprintf("检索知识: %s", query), retrievalData); err != nil {
							h.logger.Warn("保存知识检索详情失败", zap.Error(err))
						}
					}
				}
			}
		}

		// 记录 skills 调用统计（tool_call + tool_result 关联）
		if eventType == "tool_result" && h.db != nil {
			if dataMap, ok := data.(map[string]interface{}); ok {
				toolName, _ := dataMap["toolName"].(string)
				if strings.EqualFold(strings.TrimSpace(toolName), skillToolName) {
					toolCallID, _ := dataMap["toolCallId"].(string)
					skillName := ""
					if toolCallID != "" {
						skillName = strings.TrimSpace(skillCallCache[toolCallID])
						delete(skillCallCache, toolCallID)
					}
					if skillName == "" {
						if argumentsObj, ok := dataMap["argumentsObj"].(map[string]interface{}); ok {
							skillName = strings.TrimSpace(extractSkillName(argumentsObj))
						}
					}
					if skillName != "" {
						success, ok := dataMap["success"].(bool)
						if !ok {
							if isError, okErr := dataMap["isError"].(bool); okErr {
								success = !isError
							}
						}
						successCalls := 0
						failedCalls := 0
						if success {
							successCalls = 1
						} else {
							failedCalls = 1
						}
						now := time.Now()
						if err := h.db.UpdateSkillStats(skillName, 1, successCalls, failedCalls, &now); err != nil {
							h.logger.Warn("更新Skills调用统计失败", zap.Error(err), zap.String("skill", skillName))
						}
					}
				}
			}
		}

		// 子代理回复流式增量不落库；结束时合并为一条 eino_agent_reply
		if assistantMessageID != "" && eventType == "eino_agent_reply_stream_end" {
			flushResponsePlan()
			// 确保思考流在子代理回复前能持久化（刷新后可读）
			flushThinkingStreams()
			if err := h.db.AddProcessDetail(assistantMessageID, conversationID, "eino_agent_reply", message, data); err != nil {
				h.logger.Warn("保存过程详情失败", zap.Error(err), zap.String("eventType", eventType))
			}
			return
		}

		// 多代理主代理「规划中」：response_start / response_delta 仅用于 SSE，聚合落一条 planning
		if eventType == "response_start" {
			if dataMap, ok := data.(map[string]interface{}); ok {
				if sameResponseStreamMeta(respPlan.meta, dataMap) {
					if respPlan.meta == nil {
						respPlan.meta = make(map[string]interface{}, len(dataMap))
					}
					for k, v := range dataMap {
						respPlan.meta[k] = v
					}
					return
				}
			}
			flushResponsePlan()
			// 助手正文开始前，推理流通常已结束；落库以便刷新后「渗透测试详情」可回放
			flushThinkingStreams()
			respPlan.meta = nil
			if dataMap, ok := data.(map[string]interface{}); ok {
				respPlan.meta = make(map[string]interface{}, len(dataMap))
				for k, v := range dataMap {
					respPlan.meta[k] = v
				}
			}
			respPlan.b.Reset()
			return
		}
		if eventType == "response_delta" {
			if dataMap, ok := data.(map[string]interface{}); ok {
				if acc, okAcc := dataMap[openai.SSEAccumulatedKey].(string); okAcc {
					respPlan.b.Reset()
					respPlan.b.WriteString(acc)
				} else {
					respPlan.b.WriteString(message)
				}
			} else {
				respPlan.b.WriteString(message)
			}
			if dataMap, ok := data.(map[string]interface{}); ok && respPlan.meta == nil {
				respPlan.meta = make(map[string]interface{}, len(dataMap))
				for k, v := range dataMap {
					respPlan.meta[k] = v
				}
			} else if dataMap, ok := data.(map[string]interface{}); ok {
				for k, v := range dataMap {
					respPlan.meta[k] = v
				}
			}
			// 运行中的主回复不能只保存在内存：刷新会销毁旧页面，新的 task-events
			// 订阅只能收到未来增量。按时间或增量大小节流更新同一条 planning 记录，
			// 这样刷新时能从数据库恢复刷新前已经展示的全部文本。
			if respPlan.lastPersistAt.IsZero() ||
				time.Since(respPlan.lastPersistAt) >= 300*time.Millisecond ||
				respPlan.b.Len()-respPlan.lastPersistSize >= 1024 {
				persistResponsePlan(false)
			}
			syncHitlCognition()
			return
		}
		if eventType == "response" {
			flushResponsePlan()
			flushThinkingStreams()
			return
		}
		if eventType == "done" {
			flushResponsePlan()
			flushThinkingStreams()
			return
		}

		// 流式思考/推理结束：聚合落库（与 eino_agent_reply_stream_end 同理）
		if eventType == "thinking_stream_end" || eventType == "reasoning_chain_stream_end" {
			flushResponsePlan()
			flushThinkingStreams()
			return
		}

		// 聚合 thinking_stream_* / reasoning_chain_stream_*，不逐条落库
		if eventType == "thinking_stream_start" || eventType == "reasoning_chain_stream_start" {
			persistAs := "thinking"
			if eventType == "reasoning_chain_stream_start" {
				persistAs = "reasoning_chain"
			}
			if dataMap, ok := data.(map[string]interface{}); ok {
				if sid, ok2 := dataMap["streamId"].(string); ok2 && sid != "" {
					tb := thinkingStreams[sid]
					if tb == nil {
						tb = &thinkingBuf{meta: map[string]interface{}{}, persistAs: persistAs}
						thinkingStreams[sid] = tb
					} else {
						tb.persistAs = persistAs
					}
					// 记录元信息（source/einoAgent/einoRole/iteration 等）
					for k, v := range dataMap {
						tb.meta[k] = v
					}
				}
			}
			return
		}
		if eventType == "thinking_stream_delta" || eventType == "reasoning_chain_stream_delta" {
			persistAs := "thinking"
			if eventType == "reasoning_chain_stream_delta" {
				persistAs = "reasoning_chain"
			}
			if dataMap, ok := data.(map[string]interface{}); ok {
				if sid, ok2 := dataMap["streamId"].(string); ok2 && sid != "" {
					tb := thinkingStreams[sid]
					if tb == nil {
						tb = &thinkingBuf{meta: map[string]interface{}{}, persistAs: persistAs}
						thinkingStreams[sid] = tb
					} else if tb.persistAs == "" {
						tb.persistAs = persistAs
					}
					if acc, okAcc := dataMap[openai.SSEAccumulatedKey].(string); okAcc {
						tb.b.Reset()
						tb.b.WriteString(acc)
					} else {
						tb.b.WriteString(message)
					}
					// 有时 delta 先到 start 未到，补充元信息
					for k, v := range dataMap {
						tb.meta[k] = v
					}
				}
			}
			syncHitlCognition()
			return
		}

		// 当 Agent 同时发送 *_stream_* 与同名 streamId 的 thinking/reasoning_chain 时，
		// 流式聚合已会在 flushThinkingStreams() 落库；此处跳过逐条重复。
		if eventType == "thinking" || eventType == "reasoning_chain" {
			if dataMap, ok := data.(map[string]interface{}); ok {
				if sid, ok2 := dataMap["streamId"].(string); ok2 && sid != "" {
					if tb, exists := thinkingStreams[sid]; exists && tb != nil {
						if strings.TrimSpace(tb.b.String()) != "" {
							return
						}
					}
					if flushedThinking[sid] {
						return
					}
				}
			}
		}

		// 保存过程详情到数据库（排除 response/done；response 正文已在 messages 表）
		// response_start/response_delta 已聚合为 planning，不落逐条。
		// [Eino] agent 心跳 progress 仅用于实时进度标题，不落库以免时间线刷屏。
		skipEinoAgentHeartbeat := eventType == "progress" && strings.HasPrefix(strings.TrimSpace(message), "[Eino] ")
		if assistantMessageID != "" &&
			!skipEinoAgentHeartbeat &&
			eventType != "response" &&
			eventType != "done" &&
			eventType != "response_start" &&
			eventType != "response_delta" &&
			eventType != "tool_result_delta" &&
			eventType != "eino_trace_run" &&
			eventType != "eino_trace_start" &&
			eventType != "eino_trace_end" &&
			eventType != "eino_trace_error" &&
			eventType != "eino_agent_reply_stream_start" &&
			eventType != "eino_agent_reply_stream_delta" &&
			eventType != "eino_agent_reply_stream_end" {
			if eventType == "tool_result" {
				if detailID := discardPlanningIfEchoesToolResult(&respPlan, data); detailID != "" {
					if err := h.db.DeleteProcessDetail(detailID); err != nil {
						h.logger.Warn("删除工具结果回显规划失败", zap.Error(err), zap.String("processDetailId", detailID))
					}
				}
			}
			// 在关键过程事件落库前，先把「规划中」与聚合中的 thinking / reasoning_chain 流落库
			flushResponsePlan()
			flushThinkingStreams()
			processDetailID, err := h.db.AddProcessDetailWithID(assistantMessageID, conversationID, eventType, message, data)
			if err != nil {
				h.logger.Warn("保存过程详情失败", zap.Error(err), zap.String("eventType", eventType))
			}
			if deferToolProgressSend {
				clientData := enrichProgressEventData(summarizeProcessDetailData(eventType, data), conversationID, assistantMessageID)
				if m, ok := clientData.(map[string]interface{}); ok {
					m["processDetailId"] = processDetailID
				}
				if sendEventFunc != nil {
					sendEventFunc(eventType, message, clientData)
				}
				h.publishProgressToTaskEventBus(conversationID, eventType, message, clientData)
			}
		} else if deferToolProgressSend {
			clientData := enrichProgressEventData(summarizeProcessDetailData(eventType, data), conversationID, assistantMessageID)
			if sendEventFunc != nil {
				sendEventFunc(eventType, message, clientData)
			}
			h.publishProgressToTaskEventBus(conversationID, eventType, message, clientData)
		}
	}
}

// cancelToolContinueAfter 仅终止当前工具调用，不停止整条 Agent 任务（对话「中断并继续」与 MCP 监控终止共用）。
func (h *AgentHandler) cancelToolContinueAfter(conversationID, preferredExecID, note string) (bool, gin.H) {
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" || h.tasks.GetTask(conversationID) == nil {
		return false, nil
	}
	note = strings.TrimSpace(note)
	execID := strings.TrimSpace(preferredExecID)
	if execID == "" {
		execID = h.tasks.ActiveMCPExecutionID(conversationID)
	}
	if execID != "" {
		if h.agent.CancelMCPToolExecutionWithNote(execID, note) {
			return true, gin.H{
				"status":              "tool_abort_requested",
				"conversationId":      conversationID,
				"executionId":         execID,
				"message":             "已请求终止当前工具调用；工具返回后本轮推理将继续（与 MCP 监控页终止一致）。",
				"continueAfter":       true,
				"interruptWithNote":   note != "",
				"continueWithoutTool": false,
			}
		}
		if h.tasks.AbortActiveEinoExecute(conversationID, note) {
			return true, gin.H{
				"status":              "tool_abort_requested",
				"conversationId":      conversationID,
				"executionId":         execID,
				"message":             "已请求终止当前 execute 命令；命令返回后本轮推理将继续。",
				"continueAfter":       true,
				"interruptWithNote":   note != "",
				"continueWithoutTool": false,
			}
		}
		return false, nil
	}
	if h.tasks.AbortActiveEinoExecute(conversationID, note) {
		return true, gin.H{
			"status":              "tool_abort_requested",
			"conversationId":      conversationID,
			"message":             "已请求终止当前 execute 命令；命令返回后本轮推理将继续。",
			"continueAfter":       true,
			"interruptWithNote":   note != "",
			"continueWithoutTool": false,
		}
	}
	return false, nil
}

// CancelAgentLoop 取消正在执行的任务
func (h *AgentHandler) CancelAgentLoop(c *gin.Context) {
	var req struct {
		ConversationID string `json:"conversationId" binding:"required"`
		ExecutionID    string `json:"executionId,omitempty"`
		Reason         string `json:"reason,omitempty"`
		ContinueAfter  bool   `json:"continueAfter,omitempty"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !h.agentConversationAllowed(c, req.ConversationID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权访问该资源"})
		return
	}

	if req.ContinueAfter {
		if h.tasks.GetTask(req.ConversationID) == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "未找到正在执行的任务"})
			return
		}
		note := strings.TrimSpace(req.Reason)
		activeExec := strings.TrimSpace(h.tasks.ActiveMCPExecutionID(req.ConversationID))
		if ok, payload := h.cancelToolContinueAfter(req.ConversationID, strings.TrimSpace(req.ExecutionID), note); ok {
			execID, _ := payload["executionId"].(string)
			h.logger.Info("对话页仅终止当前工具",
				zap.String("conversationId", req.ConversationID),
				zap.String("executionId", execID),
				zap.Bool("hasNote", note != ""),
			)
			c.JSON(http.StatusOK, payload)
			return
		}
		if activeExec != "" {
			c.JSON(http.StatusNotFound, gin.H{"error": "未找到进行中的工具执行或该调用已结束"})
			return
		}
		// 无进行中的 MCP 工具（模型纯推理/流式输出阶段）：取消当前上下文并由 Eino 流式处理器合并用户补充后自动续跑。
		h.tasks.SetInterruptContinueNote(req.ConversationID, note)
		ok, err := h.tasks.CancelTask(req.ConversationID, multiagent.ErrInterruptContinue)
		if err != nil {
			h.logger.Error("中断并继续（无工具）失败", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "未找到正在执行的任务"})
			return
		}
		h.logger.Info("对话页中断并继续（无 MCP 工具，将自动续跑）",
			zap.String("conversationId", req.ConversationID),
			zap.Bool("hasNote", note != ""),
		)
		c.JSON(http.StatusOK, gin.H{
			"status":              "interrupt_continue_scheduled",
			"conversationId":      req.ConversationID,
			"message":             "已请求暂停当前推理；用户补充将合并到上下文并自动继续执行（无需整轮停止）。",
			"continueAfter":       true,
			"interruptWithNote":   note != "",
			"continueWithoutTool": true,
		})
		return
	}

	var cause error = ErrTaskCancelled
	msg := "已提交取消请求，任务将在当前步骤完成后停止。"
	h.cancelRunningMCPToolsForConversation(req.ConversationID)
	h.tasks.AbortActiveEinoExecute(req.ConversationID, "")
	ok, err := h.tasks.CancelTask(req.ConversationID, cause)
	if err != nil {
		h.logger.Error("取消任务失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "未找到正在执行的任务"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":            "cancelling",
		"conversationId":    req.ConversationID,
		"message":           msg,
		"continueAfter":     false,
		"interruptWithNote": false,
	})
}

// SubscribeAgentTaskEvents GET SSE：订阅指定会话当前运行中任务的事件镜像（帧格式与 POST .../stream 一致），用于刷新页面或断线后接续 UI。
func (h *AgentHandler) SubscribeAgentTaskEvents(c *gin.Context) {
	conversationID := strings.TrimSpace(c.Query("conversationId"))
	if conversationID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "conversationId is required"})
		return
	}
	if !h.agentConversationAllowed(c, conversationID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权访问该资源"})
		return
	}
	if h.tasks.GetTask(conversationID) == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no active task for this conversation"})
		return
	}
	if h.taskEventBus == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "task event bus unavailable"})
		return
	}

	c.Header("Content-Type", "text/event-stream; charset=utf-8")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	sub, ch := h.taskEventBus.Subscribe(conversationID)
	defer h.taskEventBus.Unsubscribe(conversationID, sub)

	flusher, _ := c.Writer.(http.Flusher)
	ctx := c.Request.Context()
	var writeMu sync.Mutex
	stopKeepalive := runSSEKeepalive(c, &writeMu)
	defer stopKeepalive()

	for {
		select {
		case <-ctx.Done():
			return
		case chunk, ok := <-ch:
			if !ok {
				return
			}
			writeMu.Lock()
			if _, err := c.Writer.Write(chunk); err != nil {
				writeMu.Unlock()
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
			writeMu.Unlock()
		}
	}
}

// enrichAgentTasksWithConversationTitles 为任务列表附加当前会话标题（供顶栏/任务页展示，重命名后自动同步）
func (h *AgentHandler) enrichAgentTasksWithConversationTitles(tasks []*AgentTask) {
	if h == nil || h.db == nil {
		return
	}
	for _, task := range tasks {
		if task == nil || strings.TrimSpace(task.ConversationID) == "" {
			continue
		}
		if title, err := h.db.GetConversationTitle(task.ConversationID); err == nil {
			task.Title = strings.TrimSpace(title)
		}
	}
}

// enrichCompletedTasksWithConversationTitles 为已完成任务附加当前会话标题
func (h *AgentHandler) enrichCompletedTasksWithConversationTitles(tasks []*CompletedTask) {
	if h == nil || h.db == nil {
		return
	}
	for _, task := range tasks {
		if task == nil || strings.TrimSpace(task.ConversationID) == "" {
			continue
		}
		if title, err := h.db.GetConversationTitle(task.ConversationID); err == nil {
			task.Title = strings.TrimSpace(title)
		}
	}
}

// ListAgentTasks 列出所有运行中的任务
func (h *AgentHandler) ListAgentTasks(c *gin.Context) {
	tasks := h.tasks.GetActiveTasks()
	tasks = filterSlice(tasks, func(task *AgentTask) bool {
		return task != nil && h.agentConversationAllowed(c, task.ConversationID)
	})
	h.enrichAgentTasksWithConversationTitles(tasks)
	c.JSON(http.StatusOK, gin.H{
		"tasks": tasks,
	})
}

// ListCompletedTasks 列出最近完成的任务历史
func (h *AgentHandler) ListCompletedTasks(c *gin.Context) {
	tasks := h.tasks.GetCompletedTasks()
	tasks = filterSlice(tasks, func(task *CompletedTask) bool {
		return task != nil && h.agentConversationAllowed(c, task.ConversationID)
	})
	h.enrichCompletedTasksWithConversationTitles(tasks)
	c.JSON(http.StatusOK, gin.H{
		"tasks": tasks,
	})
}

func (h *AgentHandler) agentConversationAllowed(c *gin.Context, conversationID string) bool {
	session, ok := security.CurrentSession(c)
	return ok && h.db != nil && h.db.UserCanAccessResource(session.UserID, session.Scope, "conversation", strings.TrimSpace(conversationID))
}

func filterSlice[T any](items []T, keep func(T) bool) []T {
	out := make([]T, 0, len(items))
	for _, item := range items {
		if keep(item) {
			out = append(out, item)
		}
	}
	return out
}

// BatchTaskRequest 批量任务请求
type BatchTaskRequest struct {
	HITLPolicy   string   `json:"hitlPolicy"`
	Title        string   `json:"title"`                    // 任务标题（可选）
	Tasks        []string `json:"tasks" binding:"required"` // 任务列表，每行一个任务
	Role         string   `json:"role,omitempty"`           // 角色名称（可选，空字符串表示默认角色）
	AgentMode    string   `json:"agentMode,omitempty"`      // eino_single | deep | plan_execute | supervisor
	ScheduleMode string   `json:"scheduleMode,omitempty"`   // manual | cron
	CronExpr     string   `json:"cronExpr,omitempty"`       // scheduleMode=cron 时必填
	ExecuteNow   bool     `json:"executeNow,omitempty"`     // 创建后是否立即执行（默认 false）
	ProjectID    string   `json:"projectId,omitempty"`      // 队列内子对话绑定的项目（可选）
	Concurrency  int      `json:"concurrency,omitempty"`    // 同时执行的子任务数，默认 1，最大 8
}

// batchQueueWantsEino 队列是否配置为走 Eino 多代理。
func batchQueueWantsEino(agentMode string) bool {
	m := strings.TrimSpace(strings.ToLower(agentMode))
	return m == "deep" || m == "plan_execute" || m == "supervisor"
}

func normalizeBatchQueueScheduleMode(mode string) string {
	if strings.TrimSpace(mode) == "cron" {
		return "cron"
	}
	return "manual"
}

// CreateBatchQueue 创建批量任务队列
func (h *AgentHandler) CreateBatchQueue(c *gin.Context) {
	var req BatchTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if len(req.Tasks) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "任务列表不能为空"})
		return
	}

	// 过滤空任务
	validTasks := make([]string, 0, len(req.Tasks))
	for _, task := range req.Tasks {
		if task != "" {
			validTasks = append(validTasks, task)
		}
	}

	if len(validTasks) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "没有有效的任务"})
		return
	}
	if session, ok := security.CurrentSession(c); ok && h.db != nil && session.Scope != database.RBACScopeAll && strings.TrimSpace(req.ProjectID) != "" {
		if !h.db.UserCanAccessResource(session.UserID, session.Scope, "project", strings.TrimSpace(req.ProjectID)) {
			c.JSON(http.StatusForbidden, gin.H{"error": "无权在该项目下创建批量任务"})
			return
		}
	}

	agentMode := config.NormalizeAgentMode(req.AgentMode)
	scheduleMode := normalizeBatchQueueScheduleMode(req.ScheduleMode)
	cronExpr := strings.TrimSpace(req.CronExpr)
	var nextRunAt *time.Time
	if scheduleMode == "cron" {
		if cronExpr == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "启用 Cron 调度时，调度表达式不能为空"})
			return
		}
		schedule, err := h.batchCronParser.Parse(cronExpr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "无效的 Cron 表达式: " + err.Error()})
			return
		}
		next := schedule.Next(time.Now())
		nextRunAt = &next
	}

	queue, createErr := h.batchTaskManager.CreateBatchQueue(req.Title, req.Role, agentMode, scheduleMode, cronExpr, req.ProjectID, nextRunAt, req.Concurrency, validTasks, req.HITLPolicy)
	if createErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": createErr.Error()})
		return
	}
	if session, ok := security.CurrentSession(c); ok && h.db != nil {
		_ = h.db.SetResourceOwner("batch_task", queue.ID, session.UserID)
		_ = h.db.AssignResourceToUser(session.UserID, "batch_task", queue.ID)
	}
	started := false
	if req.ExecuteNow {
		ok, err := h.startBatchQueueExecution(queue.ID, false)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "队列不存在"})
			return
		}
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "queueId": queue.ID})
			return
		}
		started = true
		if refreshed, exists := h.batchTaskManager.GetBatchQueue(queue.ID); exists {
			queue = refreshed
		}
	}
	if h.audit != nil {
		h.audit.RecordOK(c, "task", "create_queue", "创建批量任务队列", "batch_queue", queue.ID, map[string]interface{}{
			"task_count": len(validTasks), "started": started,
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"queueId": queue.ID,
		"queue":   queue,
		"started": started,
	})
}

// GetBatchQueue 获取批量任务队列
func (h *AgentHandler) GetBatchQueue(c *gin.Context) {
	queueID := c.Param("queueId")
	queue, exists := h.batchTaskManager.GetBatchQueue(queueID)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "队列不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"queue": queue})
}

// ListBatchQueuesResponse 批量任务队列列表响应
type ListBatchQueuesResponse struct {
	Queues     []*BatchTaskQueue `json:"queues"`
	Total      int               `json:"total"`
	Page       int               `json:"page"`
	PageSize   int               `json:"page_size"`
	TotalPages int               `json:"total_pages"`
}

// ListBatchQueues 列出所有批量任务队列（支持筛选和分页）
func (h *AgentHandler) ListBatchQueues(c *gin.Context) {
	limitStr := c.DefaultQuery("limit", "10")
	offsetStr := c.DefaultQuery("offset", "0")
	pageStr := c.Query("page")
	status := c.Query("status")
	keyword := c.Query("keyword")

	limit, _ := strconv.Atoi(limitStr)
	offset, _ := strconv.Atoi(offsetStr)
	page := 1

	// 如果提供了page参数，优先使用page计算offset
	if pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
			offset = (page - 1) * limit
		}
	}

	// 限制pageSize范围
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	if offset < 0 {
		offset = 0
	}
	// 防止恶意大 offset 导致 DB 性能问题
	const maxOffset = 100000
	if offset > maxOffset {
		offset = maxOffset
	}

	// 默认status为"all"
	if status == "" {
		status = "all"
	}

	// 获取队列列表和总数
	session, _ := security.CurrentSession(c)
	queues, total, err := h.batchTaskManager.ListQueuesForAccess(limit, offset, status, keyword, session.UserID, session.Scope)
	if err != nil {
		h.logger.Error("获取批量任务队列列表失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 计算总页数
	totalPages := (total + limit - 1) / limit
	if totalPages == 0 {
		totalPages = 1
	}

	// 如果使用offset计算page，需要重新计算
	if pageStr == "" {
		page = (offset / limit) + 1
	}

	response := ListBatchQueuesResponse{
		Queues:     queues,
		Total:      total,
		Page:       page,
		PageSize:   limit,
		TotalPages: totalPages,
	}

	c.JSON(http.StatusOK, response)
}

// StartBatchQueue 开始执行批量任务队列
func (h *AgentHandler) StartBatchQueue(c *gin.Context) {
	queueID := c.Param("queueId")
	h.batchTaskManager.ClearSingleRunTask(queueID)
	ok, err := h.startBatchQueueExecution(queueID, false)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "队列不存在"})
		return
	}
	if h.audit != nil {
		h.audit.RecordOK(c, "task", "start_queue", "启动批量任务队列", "batch_queue", queueID, nil)
	}
	c.JSON(http.StatusOK, gin.H{"message": "批量任务已开始执行", "queueId": queueID})
}

// RerunBatchQueue 重跑批量任务队列（重置所有子任务后重新执行）
func (h *AgentHandler) RerunBatchQueue(c *gin.Context) {
	queueID := c.Param("queueId")
	queue, exists := h.batchTaskManager.GetBatchQueue(queueID)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "队列不存在"})
		return
	}
	if queue.Status != "completed" && queue.Status != "cancelled" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "仅已完成或已取消的队列可以重跑"})
		return
	}
	if !h.batchTaskManager.ResetQueueForRerun(queueID) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "重置队列失败"})
		return
	}
	h.batchTaskManager.ClearSingleRunTask(queueID)
	ok, err := h.startBatchQueueExecution(queueID, false)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "启动失败"})
		return
	}
	if h.audit != nil {
		h.audit.RecordOK(c, "task", "rerun_queue", "重跑批量任务队列", "batch_queue", queueID, nil)
	}
	c.JSON(http.StatusOK, gin.H{"message": "批量任务已重新开始执行", "queueId": queueID})
}

// PauseBatchQueue 暂停批量任务队列
func (h *AgentHandler) PauseBatchQueue(c *gin.Context) {
	queueID := c.Param("queueId")
	success := h.batchTaskManager.PauseQueue(queueID)
	if !success {
		c.JSON(http.StatusNotFound, gin.H{"error": "队列不存在或无法暂停"})
		return
	}
	if h.audit != nil {
		h.audit.RecordOK(c, "task", "pause_queue", "暂停批量任务队列", "batch_queue", queueID, nil)
	}
	c.JSON(http.StatusOK, gin.H{"message": "批量任务已暂停"})
}

// UpdateBatchQueueMetadata 修改批量任务队列的标题、角色和代理模式
func (h *AgentHandler) UpdateBatchQueueMetadata(c *gin.Context) {
	queueID := c.Param("queueId")
	var req struct {
		HITLPolicy  *string `json:"hitlPolicy"`
		Title       string  `json:"title"`
		Role        string  `json:"role"`
		AgentMode   string  `json:"agentMode"`
		Concurrency *int    `json:"concurrency"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	var policies []string
	if req.HITLPolicy != nil {
		policies = append(policies, *req.HITLPolicy)
	}
	if err := h.batchTaskManager.UpdateQueueMetadata(queueID, req.Title, req.Role, req.AgentMode, req.Concurrency, policies...); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	updated, _ := h.batchTaskManager.GetBatchQueue(queueID)
	c.JSON(http.StatusOK, gin.H{"queue": updated})
}

// UpdateBatchQueueSchedule 修改批量任务队列的调度配置（scheduleMode / cronExpr）
func (h *AgentHandler) UpdateBatchQueueSchedule(c *gin.Context) {
	queueID := c.Param("queueId")
	queue, exists := h.batchTaskManager.GetBatchQueue(queueID)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "队列不存在"})
		return
	}
	// 仅在非 running 状态下允许修改调度
	if queue.Status == "running" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "队列正在运行中，无法修改调度配置"})
		return
	}
	var req struct {
		ScheduleMode string `json:"scheduleMode"`
		CronExpr     string `json:"cronExpr"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	scheduleMode := normalizeBatchQueueScheduleMode(req.ScheduleMode)
	cronExpr := strings.TrimSpace(req.CronExpr)
	var nextRunAt *time.Time
	if scheduleMode == "cron" {
		if cronExpr == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "启用 Cron 调度时，调度表达式不能为空"})
			return
		}
		schedule, err := h.batchCronParser.Parse(cronExpr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "无效的 Cron 表达式: " + err.Error()})
			return
		}
		next := schedule.Next(time.Now())
		nextRunAt = &next
	}
	h.batchTaskManager.UpdateQueueSchedule(queueID, scheduleMode, cronExpr, nextRunAt)
	updated, _ := h.batchTaskManager.GetBatchQueue(queueID)
	c.JSON(http.StatusOK, gin.H{"queue": updated})
}

// SetBatchQueueScheduleEnabled 开启/关闭 Cron 自动调度（手工执行不受影响）
func (h *AgentHandler) SetBatchQueueScheduleEnabled(c *gin.Context) {
	queueID := c.Param("queueId")
	if _, exists := h.batchTaskManager.GetBatchQueue(queueID); !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "队列不存在"})
		return
	}
	var req struct {
		ScheduleEnabled bool `json:"scheduleEnabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !h.batchTaskManager.SetScheduleEnabled(queueID, req.ScheduleEnabled) {
		c.JSON(http.StatusNotFound, gin.H{"error": "队列不存在"})
		return
	}
	queue, _ := h.batchTaskManager.GetBatchQueue(queueID)
	c.JSON(http.StatusOK, gin.H{"queue": queue})
}

// DeleteBatchQueue 删除批量任务队列
func (h *AgentHandler) DeleteBatchQueue(c *gin.Context) {
	queueID := c.Param("queueId")
	if err := h.batchTaskManager.DeleteQueue(queueID); err != nil {
		switch {
		case errors.Is(err, ErrBatchQueueNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "队列不存在"})
		case errors.Is(err, ErrBatchQueueExecutorActive):
			c.JSON(http.StatusConflict, gin.H{"error": "队列执行器仍在运行，请稍后再删除"})
		case errors.Is(err, ErrBatchQueueStillRunning):
			c.JSON(http.StatusConflict, gin.H{"error": "队列正在运行中，无法删除"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}
	if h.audit != nil {
		h.audit.Record(c, audit.Entry{
			Category:     "task",
			Action:       "delete_queue",
			Result:       "success",
			ResourceType: "batch_queue",
			ResourceID:   queueID,
			Message:      "删除批量任务队列",
		})
	}
	c.JSON(http.StatusOK, gin.H{"message": "批量任务队列已删除"})
}

// UpdateBatchTask 更新批量任务消息
func (h *AgentHandler) UpdateBatchTask(c *gin.Context) {
	queueID := c.Param("queueId")
	taskID := c.Param("taskId")

	var req struct {
		Message string `json:"message" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的请求参数: " + err.Error()})
		return
	}

	if req.Message == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "任务消息不能为空"})
		return
	}

	err := h.batchTaskManager.UpdateTaskMessage(queueID, taskID, req.Message)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 返回更新后的队列信息
	queue, exists := h.batchTaskManager.GetBatchQueue(queueID)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "队列不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "任务已更新", "queue": queue})
}

// AddBatchTask 添加任务到批量任务队列
func (h *AgentHandler) AddBatchTask(c *gin.Context) {
	queueID := c.Param("queueId")

	var req struct {
		Message string `json:"message" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的请求参数: " + err.Error()})
		return
	}

	if req.Message == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "任务消息不能为空"})
		return
	}

	task, err := h.batchTaskManager.AddTaskToQueue(queueID, req.Message)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 返回更新后的队列信息
	queue, exists := h.batchTaskManager.GetBatchQueue(queueID)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "队列不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "任务已添加", "task": task, "queue": queue})
}

// RunSingleBatchTask 单条执行指定子任务（可覆盖已成功项），完成后暂停队列
func (h *AgentHandler) RunSingleBatchTask(c *gin.Context) {
	queueID := c.Param("queueId")
	taskID := c.Param("taskId")

	if err := h.batchTaskManager.PrepareSingleTaskRun(queueID, taskID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.batchTaskManager.SetSingleRunTask(queueID, taskID)

	// 暂停态单条执行：旧批量协程可能仍占用执行槽，先回收以便重新启动
	if queue, ok := h.batchTaskManager.GetBatchQueue(queueID); ok && queue.Status == BatchQueueStatusPaused {
		h.batchTaskManager.ForceUnmarkQueueExecutor(queueID)
	}

	autoStarted := true
	autoStartMsg := "已开始单条执行"
	ok, startErr := h.startBatchQueueExecution(queueID, false)
	if startErr != nil {
		h.batchTaskManager.ClearSingleRunTask(queueID)
		autoStarted = false
		autoStartMsg = "任务已准备就绪，但自动启动失败: " + startErr.Error()
	} else if !ok {
		h.batchTaskManager.ClearSingleRunTask(queueID)
		autoStarted = false
		autoStartMsg = "任务已准备就绪，但队列不存在"
	}

	queue, exists := h.batchTaskManager.GetBatchQueue(queueID)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "队列不存在"})
		return
	}
	if h.audit != nil {
		h.audit.RecordOK(c, "task", "run_single_batch_task", "单条执行批量子任务", "batch_task", taskID, map[string]interface{}{
			"batch_queue_id": queueID,
			"auto_started":   autoStarted,
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"message":     autoStartMsg,
		"queue":       queue,
		"autoStarted": autoStarted,
	})
}

// DeleteBatchTask 删除批量任务
func (h *AgentHandler) DeleteBatchTask(c *gin.Context) {
	queueID := c.Param("queueId")
	taskID := c.Param("taskId")

	err := h.batchTaskManager.DeleteTask(queueID, taskID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 返回更新后的队列信息
	queue, exists := h.batchTaskManager.GetBatchQueue(queueID)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "队列不存在"})
		return
	}
	if h.audit != nil {
		h.audit.RecordOK(c, "task", "delete_batch_task", "删除批量子任务", "batch_task", taskID, map[string]interface{}{
			"batch_queue_id": queueID,
		})
	}
	c.JSON(http.StatusOK, gin.H{"message": "任务已删除", "queue": queue})
}

func (h *AgentHandler) nextBatchQueueRunAt(cronExpr string, from time.Time) (*time.Time, error) {
	expr := strings.TrimSpace(cronExpr)
	if expr == "" {
		return nil, nil
	}
	schedule, err := h.batchCronParser.Parse(expr)
	if err != nil {
		return nil, err
	}
	next := schedule.Next(from)
	return &next, nil
}

func (h *AgentHandler) startBatchQueueExecution(queueID string, scheduled bool) (bool, error) {
	// 先获取执行互斥门，再读取队列状态，避免基于过时快照做判断
	if !h.batchTaskManager.TryMarkQueueExecutor(queueID) {
		return true, nil
	}

	queue, exists := h.batchTaskManager.GetBatchQueue(queueID)
	if !exists {
		h.batchTaskManager.UnmarkQueueExecutor(queueID)
		return false, nil
	}

	if scheduled {
		if queue.ScheduleMode != "cron" {
			h.batchTaskManager.UnmarkQueueExecutor(queueID)
			err := fmt.Errorf("队列未启用 cron 调度")
			h.batchTaskManager.SetLastScheduleError(queueID, err.Error())
			return true, err
		}
		if queue.Status == "running" || queue.Status == "paused" || queue.Status == "cancelled" {
			h.batchTaskManager.UnmarkQueueExecutor(queueID)
			err := fmt.Errorf("当前队列状态不允许被调度执行")
			h.batchTaskManager.SetLastScheduleError(queueID, err.Error())
			return true, err
		}
		if !h.batchTaskManager.ResetQueueForRerun(queueID) {
			h.batchTaskManager.UnmarkQueueExecutor(queueID)
			err := fmt.Errorf("重置队列失败")
			h.batchTaskManager.SetLastScheduleError(queueID, err.Error())
			return true, err
		}
		queue, _ = h.batchTaskManager.GetBatchQueue(queueID)
	} else if queue.Status != "pending" && queue.Status != "paused" {
		h.batchTaskManager.UnmarkQueueExecutor(queueID)
		return true, fmt.Errorf("队列状态不允许启动")
	}

	if queue != nil && batchQueueWantsEino(queue.AgentMode) && (h.config == nil || !h.config.MultiAgent.Enabled) {
		h.batchTaskManager.UnmarkQueueExecutor(queueID)
		err := fmt.Errorf("当前队列配置为 Eino 多代理，但系统未启用多代理")
		if scheduled {
			h.batchTaskManager.SetLastScheduleError(queueID, err.Error())
		}
		return true, err
	}

	if scheduled {
		h.batchTaskManager.RecordScheduledRunStart(queueID)
	}
	h.batchTaskManager.UpdateQueueStatus(queueID, "running")
	if queue != nil && queue.ScheduleMode == "cron" {
		nextRunAt, err := h.nextBatchQueueRunAt(queue.CronExpr, time.Now())
		if err == nil {
			h.batchTaskManager.UpdateQueueSchedule(queueID, "cron", queue.CronExpr, nextRunAt)
		}
	}

	go h.executeBatchQueue(queueID)
	return true, nil
}

func (h *AgentHandler) batchQueueSchedulerLoop() {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		queues := h.batchTaskManager.GetLoadedQueues()
		now := time.Now()
		for _, queue := range queues {
			if queue == nil || queue.ScheduleMode != "cron" || !queue.ScheduleEnabled || queue.Status == "cancelled" || queue.Status == "running" || queue.Status == "paused" {
				continue
			}
			nextRunAt := queue.NextRunAt
			if nextRunAt == nil {
				next, err := h.nextBatchQueueRunAt(queue.CronExpr, now)
				if err != nil {
					h.logger.Warn("批量任务 cron 表达式无效，跳过调度", zap.String("queueId", queue.ID), zap.String("cronExpr", queue.CronExpr), zap.Error(err))
					continue
				}
				h.batchTaskManager.UpdateQueueSchedule(queue.ID, "cron", queue.CronExpr, next)
				nextRunAt = next
			}
			if nextRunAt != nil && (nextRunAt.Before(now) || nextRunAt.Equal(now)) {
				if _, err := h.startBatchQueueExecution(queue.ID, true); err != nil {
					h.logger.Warn("自动调度批量任务失败", zap.String("queueId", queue.ID), zap.Error(err))
				}
			}
		}
	}
}

// loadHistoryFromAgentTrace 从库中保存的代理消息轨迹恢复历史（列 last_react_*；含单代理与 Eino）。
// 逻辑与攻击链一致：优先用已保存的 JSON 消息带 + 最后一轮助手摘要，否则回退消息表。
func (h *AgentHandler) loadHistoryFromAgentTrace(conversationID string) ([]agent.ChatMessage, error) {
	traceInputJSON, assistantOut, err := h.db.GetAgentTrace(conversationID)
	if err != nil {
		return nil, fmt.Errorf("获取代理轨迹失败: %w", err)
	}

	if traceInputJSON == "" {
		return nil, fmt.Errorf("代理轨迹为空，将使用消息表")
	}

	dataSource := "database_last_agent_trace"

	var messagesArray []map[string]interface{}
	if err := json.Unmarshal([]byte(traceInputJSON), &messagesArray); err != nil {
		return nil, fmt.Errorf("解析代理轨迹 JSON 失败: %w", err)
	}

	messageCount := len(messagesArray)
	modelFacingTrace := agent.IsModelFacingTraceJSON(traceInputJSON)

	h.logger.Info("使用保存的代理轨迹恢复历史上下文",
		zap.String("conversationId", conversationID),
		zap.String("dataSource", dataSource),
		zap.Int("traceInputSize", len(traceInputJSON)),
		zap.Int("messageCount", messageCount),
		zap.Int("assistantOutSize", len(assistantOut)),
	)
	// fmt.Println("messagesArray:", messagesArray)//debug

	// 转换为Agent消息格式
	agentMessages := make([]agent.ChatMessage, 0, len(messagesArray))
	for _, msgMap := range messagesArray {
		msg := agent.ChatMessage{}
		msg.ModelFacingTrace = modelFacingTrace

		// 解析role
		if role, ok := msgMap["role"].(string); ok {
			msg.Role = role
		} else {
			continue // 跳过无效消息
		}

		// 跳过 system 消息（由 Eino Instruction 提供）
		if msg.Role == "system" {
			continue
		}

		// 解析content
		if content, ok := msgMap["content"].(string); ok {
			msg.Content = content
		}
		// DeepSeek 思考模式：含工具调用的 assistant 须在后续请求中回传 reasoning_content
		if rc, ok := msgMap["reasoning_content"].(string); ok && strings.TrimSpace(rc) != "" {
			msg.ReasoningContent = rc
		}

		// 解析tool_calls（如果存在）
		if toolCallsRaw, ok := msgMap["tool_calls"]; ok && toolCallsRaw != nil {
			if toolCallsArray, ok := toolCallsRaw.([]interface{}); ok {
				msg.ToolCalls = make([]agent.ToolCall, 0, len(toolCallsArray))
				for _, tcRaw := range toolCallsArray {
					if tcMap, ok := tcRaw.(map[string]interface{}); ok {
						toolCall := agent.ToolCall{}

						// 解析ID
						if id, ok := tcMap["id"].(string); ok {
							toolCall.ID = id
						}

						// 解析Type
						if toolType, ok := tcMap["type"].(string); ok {
							toolCall.Type = toolType
						}

						// 解析Function
						if funcMap, ok := tcMap["function"].(map[string]interface{}); ok {
							toolCall.Function = agent.FunctionCall{}

							// 解析函数名
							if name, ok := funcMap["name"].(string); ok {
								toolCall.Function.Name = name
							}

							// 解析arguments（可能是字符串或对象）
							if argsRaw, ok := funcMap["arguments"]; ok {
								if argsStr, ok := argsRaw.(string); ok {
									// 如果是字符串，解析为JSON
									var argsMap map[string]interface{}
									if err := json.Unmarshal([]byte(argsStr), &argsMap); err == nil {
										toolCall.Function.Arguments = argsMap
									}
								} else if argsMap, ok := argsRaw.(map[string]interface{}); ok {
									// 如果已经是对象，直接使用
									toolCall.Function.Arguments = argsMap
								}
							}
						}

						if toolCall.ID != "" {
							msg.ToolCalls = append(msg.ToolCalls, toolCall)
						}
					}
				}
			}
		}

		// 解析tool_call_id（tool角色消息）
		if toolCallID, ok := msgMap["tool_call_id"].(string); ok {
			msg.ToolCallID = toolCallID
		}
		if tn, ok := msgMap["tool_name"].(string); ok && strings.TrimSpace(tn) != "" {
			msg.ToolName = strings.TrimSpace(tn)
		} else if tn, ok := msgMap["name"].(string); ok && strings.TrimSpace(tn) != "" && strings.EqualFold(msg.Role, "tool") {
			msg.ToolName = strings.TrimSpace(tn)
		}

		agentMessages = append(agentMessages, msg)
	}

	// 若存在 last_react_output（助手摘要），合并为最后一条 assistant（与保存格式一致）
	if assistantOut != "" {
		if len(agentMessages) > 0 {
			lastMsg := &agentMessages[len(agentMessages)-1]
			if strings.EqualFold(lastMsg.Role, "assistant") && len(lastMsg.ToolCalls) == 0 {
				lastMsg.Content = assistantOut
			} else {
				agentMessages = append(agentMessages, agent.ChatMessage{
					Role:    "assistant",
					Content: assistantOut,
				})
			}
		} else {
			agentMessages = append(agentMessages, agent.ChatMessage{
				Role:    "assistant",
				Content: assistantOut,
			})
		}
	}

	if len(agentMessages) == 0 {
		return nil, fmt.Errorf("从代理轨迹解析的消息为空")
	}

	if h.agent != nil {
		if fixed := h.agent.RepairOrphanToolMessages(&agentMessages); fixed {
			h.logger.Info("修复了从代理轨迹恢复的历史消息中的失配 tool 消息",
				zap.String("conversationId", conversationID),
			)
		}
	}

	h.logger.Info("从代理轨迹恢复历史消息完成",
		zap.String("conversationId", conversationID),
		zap.String("dataSource", dataSource),
		zap.Int("originalMessageCount", messageCount),
		zap.Int("finalMessageCount", len(agentMessages)),
		zap.Bool("hasAssistantOut", assistantOut != ""),
	)
	return agentMessages, nil
}

// dbMessagesToAgentChatMessages maps DB rows to agent ChatMessage for history fallback
// (includes reasoning_content for DeepSeek thinking + tool replay).
func dbMessagesToAgentChatMessages(msgs []database.Message) []agent.ChatMessage {
	out := make([]agent.ChatMessage, 0, len(msgs))
	for i := range msgs {
		m := msgs[i]
		out = append(out, agent.ChatMessage{
			Role:             m.Role,
			Content:          m.Content,
			ReasoningContent: m.ReasoningContent,
		})
	}
	return out
}
