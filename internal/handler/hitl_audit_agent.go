package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/hitl"
	"cyberstrike-ai/internal/openai"
	"cyberstrike-ai/internal/typesafe"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// auditAgentReview 在 reviewer=audit_agent 时由 LLM 代行审批。
// 白名单工具在 shouldInterrupt 阶段已跳过，到达此处的一律需要裁决。
func (h *AgentHandler) auditAgentReview(ctx context.Context, hitlMode, toolName string, payload map[string]interface{}) hitlDecision {
	if h == nil {
		return hitlDecision{Decision: "reject", Comment: "audit agent: handler unavailable"}
	}
	mode := normalizeHitlMode(hitlMode)
	if h.config != nil && h.config.Hitl.EffectiveAuditBackend() == config.HitlAuditBackendTypeSafe {
		return h.auditAgentReviewTypeSafe(ctx, mode, toolName, payload)
	}
	prompt := config.DefaultHitlAuditAgentPrompt()
	if h.config != nil {
		prompt = h.config.Hitl.EffectiveAuditAgentPromptForMode(mode)
	}
	llmCfg := h.auditLLMConfig()
	if strings.TrimSpace(llmCfg.APIKey) == "" || strings.TrimSpace(llmCfg.Model) == "" {
		return hitlDecision{Decision: "reject", Comment: "audit agent: LLM 未配置"}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	callCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	userContent := buildAuditAgentReviewInput(mode, toolName, payload)
	requestBody := map[string]interface{}{
		"model": strings.TrimSpace(llmCfg.Model),
		"messages": []map[string]interface{}{
			{"role": "system", "content": prompt},
			{"role": "user", "content": userContent},
		},
		"temperature":           0.1,
		"max_completion_tokens": 1024,
		// 审计裁决需要结构化 JSON；关闭 thinking 避免 Qwen 等把正文放进 reasoning_content 导致解析失败。
		"thinking": map[string]interface{}{"type": "disabled"},
	}

	var apiResponse struct {
		Choices []struct {
			Message struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
			} `json:"message"`
		} `json:"choices"`
	}
	client := openai.NewClient(&llmCfg, nil, h.logger)
	if err := client.ChatCompletion(callCtx, requestBody, &apiResponse); err != nil {
		h.logger.Warn("审计 Agent LLM 调用失败", zap.Error(err), zap.String("tool", toolName))
		return hitlDecision{
			Decision: "reject",
			Comment:  "audit agent: LLM 调用失败，保守拒绝",
		}
	}
	if len(apiResponse.Choices) == 0 {
		return hitlDecision{Decision: "reject", Comment: "audit agent: LLM 无有效响应，保守拒绝"}
	}
	msg := apiResponse.Choices[0].Message
	raw := strings.TrimSpace(msg.Content)
	if raw == "" {
		raw = strings.TrimSpace(msg.ReasoningContent)
	}
	dec, err := parseAuditAgentLLMContent(raw)
	if err != nil {
		snippet := raw
		if len(snippet) > 240 {
			snippet = snippet[:240] + "..."
		}
		h.logger.Warn("审计 Agent 响应解析失败",
			zap.Error(err),
			zap.String("tool", toolName),
			zap.String("mode", mode),
			zap.String("snippet", snippet),
		)
		return hitlDecision{Decision: "reject", Comment: "audit agent: 响应无法解析，保守拒绝"}
	}
	if mode != "review_edit" && len(dec.EditedArguments) > 0 {
		h.logger.Warn("审计 Agent 在审批模式下返回 editedArguments，已忽略",
			zap.String("tool", toolName),
		)
		dec.EditedArguments = nil
	}
	if dec.Comment == "" {
		dec.Comment = "audit agent: " + dec.Decision
	} else if !strings.HasPrefix(strings.ToLower(dec.Comment), "audit agent") {
		dec.Comment = "audit agent: " + dec.Comment
	}
	return dec
}

func (h *AgentHandler) auditLLMConfig() config.OpenAIConfig {
	if h != nil && h.config != nil {
		return h.config.Hitl.AuditModelEffective(h.config.OpenAI)
	}
	return config.OpenAIConfig{}
}

func (h *AgentHandler) auditAgentReviewTypeSafe(ctx context.Context, hitlMode, toolName string, payload map[string]interface{}) hitlDecision {
	if h == nil || h.config == nil {
		return hitlDecision{Decision: "reject", Comment: "audit agent: TypeSafe 未配置"}
	}
	baseURL, apiKey, model := h.config.Hitl.TypeSafeConfigEffective()
	if apiKey == "" {
		return hitlDecision{Decision: "reject", Comment: "audit agent: TypeSafe API Key 未配置"}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	callCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	client := typesafe.NewClient(baseURL, apiKey, model, nil)
	policy := h.config.Hitl.JevOperatorPolicy(hitlMode)
	result, err := client.SystemOne(callCtx, hitl.BuildJevState(hitlMode, toolName, payload, policy), hitl.JevAuditQuestions(policy))
	if err != nil {
		h.logger.Warn("审计 Agent TypeSafe 调用失败", zap.Error(err), zap.String("tool", toolName))
		return hitlDecision{Decision: "reject", Comment: "audit agent: TypeSafe 调用失败，保守拒绝"}
	}
	decision, comment := hitl.DecideJev(result)
	if comment == "" {
		comment = "audit agent: " + decision
	}
	return hitlDecision{Decision: decision, Comment: comment}
}

func buildAuditAgentReviewInput(hitlMode, toolName string, payload map[string]interface{}) string {
	review := map[string]interface{}{
		"hitlMode": normalizeHitlMode(hitlMode),
		"toolName": strings.TrimSpace(toolName),
	}
	if payload != nil {
		for _, k := range []string{"arguments", "argumentsObj", "command", hitlPayloadUserMessage, hitlPayloadThinking, hitlPayloadReasoningChain, hitlPayloadPlanning} {
			if v, ok := payload[k]; ok && v != nil && fmt.Sprint(v) != "" {
				review[k] = v
			}
		}
	}
	b, err := json.MarshalIndent(review, "", "  ")
	if err != nil {
		return fmt.Sprintf(`{"hitlMode":%q,"toolName":%q}`, normalizeHitlMode(hitlMode), toolName)
	}
	return string(b)
}

func parseAuditAgentLLMContent(content string) (hitlDecision, error) {
	s := strings.TrimSpace(content)
	if s == "" {
		return hitlDecision{}, errors.New("empty content")
	}
	for _, candidate := range auditAgentJSONCandidates(s) {
		dec, comment, editedArgs, err := parseAuditAgentDecisionObject(candidate)
		if err == nil {
			return hitlDecision{
				Decision:        dec,
				Comment:         comment,
				EditedArguments: editedArgs,
			}, nil
		}
	}
	return hitlDecision{}, fmt.Errorf("no valid decision json in response")
}

func auditAgentJSONCandidates(s string) []string {
	out := make([]string, 0, 4)
	seen := make(map[string]struct{})
	add := func(c string) {
		c = strings.TrimSpace(c)
		if c == "" {
			return
		}
		if _, ok := seen[c]; ok {
			return
		}
		seen[c] = struct{}{}
		out = append(out, c)
	}
	add(s)
	add(stripMarkdownCodeFence(s))
	if obj := extractFirstJSONObject(s); obj != "" {
		add(obj)
	}
	if obj := extractFirstJSONObject(stripMarkdownCodeFence(s)); obj != "" {
		add(obj)
	}
	return out
}

func stripMarkdownCodeFence(s string) string {
	s = strings.TrimSpace(s)
	for _, fence := range []string{"```json", "```JSON", "```"} {
		if strings.HasPrefix(s, fence) {
			s = strings.TrimPrefix(s, fence)
		}
	}
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}

func extractFirstJSONObject(s string) string {
	start := strings.Index(s, "{")
	if start < 0 {
		return ""
	}
	depth := 0
	inStr := false
	esc := false
	for i := start; i < len(s); i++ {
		ch := s[i]
		if inStr {
			if esc {
				esc = false
				continue
			}
			if ch == '\\' {
				esc = true
				continue
			}
			if ch == '"' {
				inStr = false
			}
			continue
		}
		switch ch {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}

func parseAuditAgentDecisionObject(jsonText string) (decision, comment string, editedArgs map[string]interface{}, err error) {
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(jsonText), &parsed); err != nil {
		return "", "", nil, err
	}
	rawDecision := auditAgentPickString(parsed, "decision", "Decision", "result", "action", "verdict", "决策", "决定")
	decision = normalizeAuditAgentDecision(rawDecision)
	if decision == "" {
		return "", "", nil, fmt.Errorf("missing decision")
	}
	comment = auditAgentPickString(parsed, "comment", "Comment", "reason", "message", "rationale", "备注", "理由", "说明")
	editedArgs = auditAgentPickObject(parsed, "editedArguments", "edited_arguments", "editedArgs")
	return decision, strings.TrimSpace(comment), editedArgs, nil
}

func auditAgentPickString(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			s := strings.TrimSpace(fmt.Sprint(v))
			if s != "" {
				return s
			}
		}
	}
	return ""
}

func auditAgentPickObject(m map[string]interface{}, keys ...string) map[string]interface{} {
	for _, k := range keys {
		v, ok := m[k]
		if !ok || v == nil {
			continue
		}
		switch t := v.(type) {
		case map[string]interface{}:
			if len(t) > 0 {
				return t
			}
		case string:
			s := strings.TrimSpace(t)
			if s == "" || s == "{}" {
				continue
			}
			var obj map[string]interface{}
			if err := json.Unmarshal([]byte(s), &obj); err == nil && len(obj) > 0 {
				return obj
			}
		}
	}
	return nil
}

func normalizeAuditAgentDecision(v string) string {
	d := strings.ToLower(strings.TrimSpace(v))
	switch d {
	case "approve", "approved", "pass", "passed", "allow", "allowed", "yes", "ok", "accept", "accepted":
		return "approve"
	case "reject", "rejected", "deny", "denied", "no", "block", "blocked", "refuse", "refused":
		return "reject"
	}
	switch strings.TrimSpace(v) {
	case "通过", "批准", "允许", "同意", "放行":
		return "approve"
	case "拒绝", "驳回", "禁止", "否决":
		return "reject"
	}
	return ""
}

type hitlAuditStrategyReq struct {
	AuditAgentPrompt           string `json:"auditAgentPrompt"`
	AuditAgentPromptReviewEdit string `json:"auditAgentPromptReviewEdit"`
}

func (h *AgentHandler) GetHITLAuditStrategy(c *gin.Context) {
	approvalPrompt := config.DefaultHitlAuditAgentPrompt()
	reviewEditPrompt := config.DefaultHitlAuditAgentPromptReviewEdit()
	approvalCustom := false
	reviewEditCustom := false
	if h.config != nil {
		approvalPrompt = h.config.Hitl.EffectiveAuditAgentPromptForMode("approval")
		reviewEditPrompt = h.config.Hitl.EffectiveAuditAgentPromptForMode("review_edit")
		approvalCustom = strings.TrimSpace(h.config.Hitl.AuditAgentPrompt) != ""
		reviewEditCustom = strings.TrimSpace(h.config.Hitl.AuditAgentPromptReviewEdit) != ""
	}
	c.JSON(http.StatusOK, gin.H{
		"auditAgentPrompt":                  approvalPrompt,
		"auditAgentPromptCustom":            approvalCustom,
		"auditAgentPromptReviewEdit":        reviewEditPrompt,
		"auditAgentPromptReviewEditCustom":  reviewEditCustom,
		"defaultAuditAgentPrompt":           config.DefaultHitlAuditAgentPrompt(),
		"defaultAuditAgentPromptReviewEdit": config.DefaultHitlAuditAgentPromptReviewEdit(),
	})
}

func (h *AgentHandler) UpdateHITLAuditStrategy(c *gin.Context) {
	if h.hitlStrategySaver == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "HITL 策略持久化不可用"})
		return
	}
	var req hitlAuditStrategyReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	approvalPrompt := strings.TrimSpace(req.AuditAgentPrompt)
	reviewEditPrompt := strings.TrimSpace(req.AuditAgentPromptReviewEdit)
	if err := h.hitlStrategySaver.UpdateHitlAuditAgentStrategy(approvalPrompt, reviewEditPrompt); err != nil {
		h.logger.Warn("保存审计 Agent 提示词失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if h.audit != nil {
		h.audit.RecordOK(c, "hitl", "audit_strategy_update", "HITL 审计策略更新", "hitl_config", "audit_agent_prompt", nil)
	}
	if h.config != nil {
		h.config.Hitl.AuditAgentPrompt = approvalPrompt
		h.config.Hitl.AuditAgentPromptReviewEdit = reviewEditPrompt
	}
	c.JSON(http.StatusOK, gin.H{
		"ok":                               true,
		"auditAgentPrompt":                 config.HitlConfig{AuditAgentPrompt: approvalPrompt}.EffectiveAuditAgentPromptForMode("approval"),
		"auditAgentPromptCustom":           approvalPrompt != "",
		"auditAgentPromptReviewEdit":       config.HitlConfig{AuditAgentPromptReviewEdit: reviewEditPrompt}.EffectiveAuditAgentPromptForMode("review_edit"),
		"auditAgentPromptReviewEditCustom": reviewEditPrompt != "",
	})
}

// HitlAuditStrategySaver 持久化审计 Agent 提示词到 config.yaml。
type HitlAuditStrategySaver interface {
	UpdateHitlAuditAgentStrategy(approvalPrompt, reviewEditPrompt string) error
}

// SetHitlAuditStrategySaver 设置审计策略落盘。
func (h *AgentHandler) SetHitlAuditStrategySaver(s HitlAuditStrategySaver) {
	h.hitlStrategySaver = s
}
