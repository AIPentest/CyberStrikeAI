package handler

import (
	"net/http"

	"cyberstrike-ai/internal/database"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// OpenAPIHandler OpenAPI处理器
type OpenAPIHandler struct {
	db               *database.DB
	logger           *zap.Logger
	conversationHdlr *ConversationHandler
	agentHdlr        *AgentHandler
}

// NewOpenAPIHandler 创建新的OpenAPI处理器
func NewOpenAPIHandler(db *database.DB, logger *zap.Logger, conversationHdlr *ConversationHandler, agentHdlr *AgentHandler) *OpenAPIHandler {
	return &OpenAPIHandler{
		db:               db,
		logger:           logger,
		conversationHdlr: conversationHdlr,
		agentHdlr:        agentHdlr,
	}
}

// GetOpenAPISpec 获取OpenAPI规范
func (h *OpenAPIHandler) GetOpenAPISpec(c *gin.Context) {
	host := c.Request.Host
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}

	finalizationRequestSchema := map[string]interface{}{
		"type":        "object",
		"description": "最终回复交付策略。后端不会从自然语言内容推断执行意图；执行入口应显式声明是否要求 completed 工具证据。",
		"properties": map[string]interface{}{
			"requireExecutionEvidence": map[string]interface{}{
				"type":        "boolean",
				"description": "为 true 时，缺少 completed 工具执行记录会触发无注入续跑或最终阻断；普通聊天可省略或设为 false。",
			},
		},
	}

	spec := map[string]interface{}{
		"openapi": "3.0.0",
		"info": map[string]interface{}{
			"title":       "CyberStrikeAI API",
			"description": "AI驱动的自动化安全测试平台API文档",
			"version":     "1.0.0",
			"contact": map[string]interface{}{
				"name": "CyberStrikeAI",
			},
		},
		"servers": []map[string]interface{}{
			{
				"url":         scheme + "://" + host,
				"description": "当前服务器",
			},
		},
		"components": map[string]interface{}{
			"securitySchemes": map[string]interface{}{
				"bearerAuth": map[string]interface{}{
					"type":         "http",
					"scheme":       "bearer",
					"bearerFormat": "JWT",
					"description":  "使用Bearer Token进行认证。Token通过 /api/auth/login 接口获取。",
				},
			},
			"schemas": map[string]interface{}{
				"CreateConversationRequest": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"title": map[string]interface{}{
							"type":        "string",
							"description": "对话标题",
							"example":     "Web应用安全测试",
						},
						"projectId": map[string]interface{}{
							"type":        "string",
							"description": "绑定的项目 ID（可选，共享事实黑板）",
						},
					},
				},
				"SetConversationProjectRequest": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"projectId": map[string]interface{}{
							"type":        "string",
							"description": "项目 ID；空字符串表示解除绑定",
						},
					},
					"required": []string{"projectId"},
				},
				"AgentChatResponse": map[string]interface{}{
					"type":        "object",
					"description": "Agent 非流式响应。response 只是交付文本；是否为成功最终回复必须以 finalized/finalizable/status 为准。",
					"properties": map[string]interface{}{
						"response": map[string]interface{}{
							"type":        "string",
							"description": "交付给用户的文本。finalized=false 时为阻断/未完成说明，不是成功结论。",
						},
						"conversationId": map[string]interface{}{
							"type":        "string",
							"description": "对话 ID",
						},
						"assistantMessageId": map[string]interface{}{
							"type":        "string",
							"description": "助手消息 ID（部分接口返回）",
						},
						"mcpExecutionIds": map[string]interface{}{
							"type":        "array",
							"description": "本轮关联的 MCP 工具执行 ID",
							"items":       map[string]interface{}{"type": "string"},
						},
						"agentMode": map[string]interface{}{
							"type":        "string",
							"description": "agent 模式，例如 eino_single、eino_deep、workflow",
						},
						"finalized": map[string]interface{}{
							"type":        "boolean",
							"description": "是否已经通过最终回复检查。只有 true 才能当成功最终回复。",
						},
						"finalizable": map[string]interface{}{
							"type":        "boolean",
							"description": "候选输出是否可提升为最终回复。",
						},
						"status": map[string]interface{}{
							"type":        "string",
							"description": "最终化状态",
							"enum":        []string{"completed", "in_progress", "blocked", "failed", "cancelled", "awaiting_hitl"},
						},
						"completionReason": map[string]interface{}{
							"type":        "string",
							"description": "最终化或阻断原因，例如 verified、pending_tool_executions、missing_execution_evidence",
						},
						"evidenceVerified": map[string]interface{}{
							"type":        "boolean",
							"description": "证据是否满足最终化要求",
						},
						"evidenceRefs": map[string]interface{}{
							"type":        "array",
							"description": "证据引用，例如 mcp_execution:<id>",
							"items":       map[string]interface{}{"type": "string"},
						},
						"pendingExecutionIds": map[string]interface{}{
							"type":        "array",
							"description": "仍处于 queued/running 的工具执行 ID",
							"items":       map[string]interface{}{"type": "string"},
						},
						"missingChecks": map[string]interface{}{
							"type":        "array",
							"description": "未通过最终化检查的原因列表",
							"items":       map[string]interface{}{"type": "string"},
						},
					},
					"required": []string{"response", "conversationId", "finalized", "finalizable", "status", "evidenceVerified"},
				},
				"Conversation": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id": map[string]interface{}{
							"type":        "string",
							"description": "对话ID",
							"example":     "550e8400-e29b-41d4-a716-446655440000",
						},
						"title": map[string]interface{}{
							"type":        "string",
							"description": "对话标题",
							"example":     "Web应用安全测试",
						},
						"createdAt": map[string]interface{}{
							"type":        "string",
							"format":      "date-time",
							"description": "创建时间",
						},
						"updatedAt": map[string]interface{}{
							"type":        "string",
							"format":      "date-time",
							"description": "更新时间",
						},
						"projectId": map[string]interface{}{
							"type":        "string",
							"description": "绑定的项目 ID（可选）",
						},
					},
				},
				"ConversationDetail": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id": map[string]interface{}{
							"type":        "string",
							"description": "对话ID",
						},
						"title": map[string]interface{}{
							"type":        "string",
							"description": "对话标题",
						},
						"status": map[string]interface{}{
							"type":        "string",
							"description": "对话状态：active（进行中）、completed（已完成）、failed（失败）",
							"enum":        []string{"active", "completed", "failed"},
						},
						"createdAt": map[string]interface{}{
							"type":        "string",
							"format":      "date-time",
							"description": "创建时间",
						},
						"updatedAt": map[string]interface{}{
							"type":        "string",
							"format":      "date-time",
							"description": "更新时间",
						},
						"messages": map[string]interface{}{
							"type":        "array",
							"description": "消息列表",
							"items": map[string]interface{}{
								"$ref": "#/components/schemas/Message",
							},
						},
						"messageCount": map[string]interface{}{
							"type":        "integer",
							"description": "消息数量",
						},
					},
				},
				"Message": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id": map[string]interface{}{
							"type":        "string",
							"description": "消息ID",
						},
						"conversationId": map[string]interface{}{
							"type":        "string",
							"description": "对话ID",
						},
						"role": map[string]interface{}{
							"type":        "string",
							"description": "消息角色：user（用户）、assistant（助手）",
							"enum":        []string{"user", "assistant"},
						},
						"content": map[string]interface{}{
							"type":        "string",
							"description": "消息内容",
						},
						"createdAt": map[string]interface{}{
							"type":        "string",
							"format":      "date-time",
							"description": "创建时间",
						},
					},
				},
				"ConversationResults": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"conversationId": map[string]interface{}{
							"type":        "string",
							"description": "对话ID",
						},
						"messages": map[string]interface{}{
							"type":        "array",
							"description": "消息列表",
							"items": map[string]interface{}{
								"$ref": "#/components/schemas/Message",
							},
						},
						"vulnerabilities": map[string]interface{}{
							"type":        "array",
							"description": "发现的漏洞列表",
							"items": map[string]interface{}{
								"$ref": "#/components/schemas/Vulnerability",
							},
						},
						"executionResults": map[string]interface{}{
							"type":        "array",
							"description": "执行结果列表",
							"items": map[string]interface{}{
								"$ref": "#/components/schemas/ExecutionResult",
							},
						},
					},
				},
				"Vulnerability": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id": map[string]interface{}{
							"type":        "string",
							"description": "漏洞ID",
						},
						"title": map[string]interface{}{
							"type":        "string",
							"description": "漏洞标题",
						},
						"description": map[string]interface{}{
							"type":        "string",
							"description": "漏洞描述",
						},
						"severity": map[string]interface{}{
							"type":        "string",
							"description": "严重程度",
							"enum":        []string{"critical", "high", "medium", "low", "info"},
						},
						"status": map[string]interface{}{
							"type":        "string",
							"description": "状态",
							"enum":        []string{"open", "confirmed", "fixed", "false_positive", "ignored"},
						},
						"target": map[string]interface{}{
							"type":        "string",
							"description": "受影响的目标",
						},
					},
				},
				"AssetImportItem": map[string]interface{}{
					"type":        "object",
					"description": "待导入资产；host、ip、domain 至少一项非空",
					"properties": map[string]interface{}{
						"project_id":         map[string]interface{}{"type": "string", "description": "所属项目 ID；调用者必须有权访问"},
						"host":               map[string]interface{}{"type": "string", "maxLength": 500, "example": "https://app.example.com:443"},
						"ip":                 map[string]interface{}{"type": "string", "example": "192.0.2.10"},
						"port":               map[string]interface{}{"type": "integer", "minimum": 0, "maximum": 65535, "example": 443},
						"domain":             map[string]interface{}{"type": "string", "example": "app.example.com"},
						"protocol":           map[string]interface{}{"type": "string", "example": "https"},
						"title":              map[string]interface{}{"type": "string", "maxLength": 500},
						"server":             map[string]interface{}{"type": "string", "maxLength": 255, "example": "nginx"},
						"country":            map[string]interface{}{"type": "string"},
						"province":           map[string]interface{}{"type": "string"},
						"city":               map[string]interface{}{"type": "string"},
						"responsible_person": map[string]interface{}{"type": "string", "maxLength": 255, "description": "资产负责人"},
						"department":         map[string]interface{}{"type": "string", "maxLength": 255, "description": "所属部门"},
						"business_system":    map[string]interface{}{"type": "string", "maxLength": 255, "description": "所属业务系统"},
						"environment":        map[string]interface{}{"type": "string", "enum": []string{"production", "staging", "testing", "development", "other"}},
						"criticality":        map[string]interface{}{"type": "string", "enum": []string{"critical", "high", "medium", "low"}},
						"source":             map[string]interface{}{"type": "string"},
						"source_query":       map[string]interface{}{"type": "string"},
						"status":             map[string]interface{}{"type": "string", "enum": []string{"active", "inactive"}, "default": "active"},
						"tags": map[string]interface{}{
							"type":     "array",
							"maxItems": 30,
							"items":    map[string]interface{}{"type": "string", "maxLength": 64},
						},
					},
				},
				"AssetImportRequest": map[string]interface{}{
					"type":     "object",
					"required": []string{"assets"},
					"properties": map[string]interface{}{
						"assets": map[string]interface{}{
							"type":     "array",
							"minItems": 1,
							"maxItems": 100000,
							"items":    map[string]interface{}{"$ref": "#/components/schemas/AssetImportItem"},
						},
						"source":       map[string]interface{}{"type": "string", "description": "未在资产中填写来源时使用的默认来源"},
						"source_query": map[string]interface{}{"type": "string", "description": "默认来源查询或导入文件名"},
					},
				},
				"AssetImportResult": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"created": map[string]interface{}{"type": "integer", "description": "新建数量", "example": 120},
						"updated": map[string]interface{}{"type": "integer", "description": "去重合并数量", "example": 8},
						"skipped": map[string]interface{}{"type": "integer", "description": "跳过数量", "example": 2},
					},
				},
				"ExecutionResult": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id": map[string]interface{}{
							"type":        "string",
							"description": "执行ID",
						},
						"toolName": map[string]interface{}{
							"type":        "string",
							"description": "工具名称",
						},
						"status": map[string]interface{}{
							"type":        "string",
							"description": "执行状态",
							"enum":        []string{"queued", "running", "completed", "failed", "cancelled", "hard_timeout", "orphaned"},
						},
						"result": map[string]interface{}{
							"type":        "string",
							"description": "执行结果",
						},
						"createdAt": map[string]interface{}{
							"type":        "string",
							"format":      "date-time",
							"description": "创建时间",
						},
					},
				},
				"Error": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"error": map[string]interface{}{
							"type":        "string",
							"description": "错误信息",
						},
					},
				},
				"LoginRequest": map[string]interface{}{
					"type":     "object",
					"required": []string{"password"},
					"properties": map[string]interface{}{
						"password": map[string]interface{}{
							"type":        "string",
							"description": "登录密码",
						},
					},
				},
				"LoginResponse": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"token": map[string]interface{}{
							"type":        "string",
							"description": "认证Token",
						},
						"expires_at": map[string]interface{}{
							"type":        "string",
							"format":      "date-time",
							"description": "Token过期时间",
						},
						"session_duration_hr": map[string]interface{}{
							"type":        "integer",
							"description": "会话持续时间（小时）",
						},
					},
				},
				"ChangePasswordRequest": map[string]interface{}{
					"type":     "object",
					"required": []string{"oldPassword", "newPassword"},
					"properties": map[string]interface{}{
						"oldPassword": map[string]interface{}{
							"type":        "string",
							"description": "当前密码",
						},
						"newPassword": map[string]interface{}{
							"type":        "string",
							"description": "新密码（至少8位）",
						},
					},
				},
				"UpdateConversationRequest": map[string]interface{}{
					"type":     "object",
					"required": []string{"title"},
					"properties": map[string]interface{}{
						"title": map[string]interface{}{
							"type":        "string",
							"description": "对话标题",
						},
					},
				},
				"BatchTaskRequest": map[string]interface{}{
					"type":     "object",
					"required": []string{"tasks"},
					"properties": map[string]interface{}{
						"title": map[string]interface{}{
							"type":        "string",
							"description": "任务标题（可选）",
						},
						"tasks": map[string]interface{}{
							"type":        "array",
							"description": "任务列表，每行一个任务",
							"items": map[string]interface{}{
								"type": "string",
							},
						},
						"role": map[string]interface{}{
							"type":        "string",
							"description": "角色名称（可选）",
						},
						"agentMode": map[string]interface{}{
							"type":        "string",
							"description": "代理模式：eino_single（Eino ADK 单代理，默认）| deep | plan_execute | supervisor",
							"enum":        []string{"eino_single", "deep", "plan_execute", "supervisor"},
						},
						"scheduleMode": map[string]interface{}{
							"type":        "string",
							"description": "调度方式（manual | cron）",
							"enum":        []string{"manual", "cron"},
						},
						"cronExpr": map[string]interface{}{
							"type":        "string",
							"description": "Cron 表达式（scheduleMode=cron 时必填）",
						},
						"executeNow": map[string]interface{}{
							"type":        "boolean",
							"description": "是否创建后立即执行（默认 false）",
						},
					},
				},
				"BatchQueue": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id": map[string]interface{}{
							"type":        "string",
							"description": "队列ID",
						},
						"title": map[string]interface{}{
							"type":        "string",
							"description": "队列标题",
						},
						"status": map[string]interface{}{
							"type":        "string",
							"description": "队列状态",
							"enum":        []string{"pending", "running", "paused", "completed", "failed"},
						},
						"tasks": map[string]interface{}{
							"type":        "array",
							"description": "任务列表",
							"items": map[string]interface{}{
								"type": "object",
							},
						},
						"createdAt": map[string]interface{}{
							"type":        "string",
							"format":      "date-time",
							"description": "创建时间",
						},
					},
				},
				"CancelAgentLoopRequest": map[string]interface{}{
					"type":     "object",
					"required": []string{"conversationId"},
					"properties": map[string]interface{}{
						"conversationId": map[string]interface{}{
							"type":        "string",
							"description": "对话ID",
						},
						"reason": map[string]interface{}{
							"type":        "string",
							"description": "可选。与 MCP 监控页「终止并说明」一致：非空时合并进当前工具返回给模型的文本（含 USER INTERRUPT NOTE 块）",
						},
						"continueAfter": map[string]interface{}{
							"type":        "boolean",
							"description": "为 true 时仅终止当前进行中的 MCP 工具调用（不取消整轮任务）；须已有工具在执行，否则 400",
						},
					},
				},
				"AgentTask": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"conversationId": map[string]interface{}{
							"type":        "string",
							"description": "对话ID",
						},
						"status": map[string]interface{}{
							"type":        "string",
							"description": "任务状态",
							"enum":        []string{"running", "completed", "failed", "cancelled", "timeout"},
						},
						"startedAt": map[string]interface{}{
							"type":        "string",
							"format":      "date-time",
							"description": "开始时间",
						},
					},
				},
				"CreateVulnerabilityRequest": map[string]interface{}{
					"type":     "object",
					"required": []string{"conversation_id", "title", "description", "severity", "type", "target", "reproduction_steps", "evidence", "impact", "recommendation"},
					"properties": map[string]interface{}{
						"conversation_id": map[string]interface{}{
							"type":        "string",
							"description": "对话ID",
						},
						"title": map[string]interface{}{
							"type":        "string",
							"description": "漏洞标题",
						},
						"description": map[string]interface{}{
							"type":        "string",
							"description": "漏洞描述",
						},
						"severity": map[string]interface{}{
							"type":        "string",
							"description": "严重程度",
							"enum":        []string{"critical", "high", "medium", "low", "info"},
						},
						"status": map[string]interface{}{
							"type":        "string",
							"description": "状态",
							"enum":        []string{"open", "closed", "fixed"},
						},
						"type": map[string]interface{}{
							"type":        "string",
							"description": "漏洞类型",
						},
						"target": map[string]interface{}{
							"type":        "string",
							"description": "受影响的目标",
						},
						"preconditions":      map[string]interface{}{"type": "string", "description": "前置条件"},
						"reproduction_steps": map[string]interface{}{"type": "string", "description": "复现步骤"},
						"evidence":           map[string]interface{}{"type": "string", "description": "证据/POC，包含请求响应、命令输出、截图说明、日志等"},
						"impact": map[string]interface{}{
							"type":        "string",
							"description": "影响",
						},
						"recommendation": map[string]interface{}{
							"type":        "string",
							"description": "修复建议",
						},
						"retest_notes": map[string]interface{}{"type": "string", "description": "复测方式"},
					},
				},
				"UpdateVulnerabilityRequest": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"title": map[string]interface{}{
							"type":        "string",
							"description": "漏洞标题",
						},
						"description": map[string]interface{}{
							"type":        "string",
							"description": "漏洞描述",
						},
						"severity": map[string]interface{}{
							"type":        "string",
							"description": "严重程度",
							"enum":        []string{"critical", "high", "medium", "low", "info"},
						},
						"status": map[string]interface{}{
							"type":        "string",
							"description": "状态",
							"enum":        []string{"open", "confirmed", "fixed", "false_positive", "ignored"},
						},
						"type": map[string]interface{}{
							"type":        "string",
							"description": "漏洞类型",
						},
						"target": map[string]interface{}{
							"type":        "string",
							"description": "受影响的目标",
						},
						"preconditions":      map[string]interface{}{"type": "string", "description": "前置条件"},
						"reproduction_steps": map[string]interface{}{"type": "string", "description": "复现步骤"},
						"evidence":           map[string]interface{}{"type": "string", "description": "证据/POC，包含请求响应、命令输出、截图说明、日志等"},
						"impact": map[string]interface{}{
							"type":        "string",
							"description": "影响",
						},
						"recommendation": map[string]interface{}{
							"type":        "string",
							"description": "修复建议",
						},
						"retest_notes": map[string]interface{}{"type": "string", "description": "复测方式"},
					},
				},
				"ListVulnerabilitiesResponse": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"vulnerabilities": map[string]interface{}{
							"type":        "array",
							"description": "漏洞列表",
							"items": map[string]interface{}{
								"$ref": "#/components/schemas/Vulnerability",
							},
						},
						"total": map[string]interface{}{
							"type":        "integer",
							"description": "总数",
						},
						"page": map[string]interface{}{
							"type":        "integer",
							"description": "当前页",
						},
						"page_size": map[string]interface{}{
							"type":        "integer",
							"description": "每页数量",
						},
						"total_pages": map[string]interface{}{
							"type":        "integer",
							"description": "总页数",
						},
					},
				},
				"VulnerabilityStats": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"total": map[string]interface{}{
							"type":        "integer",
							"description": "总漏洞数",
						},
						"by_severity": map[string]interface{}{
							"type":        "object",
							"description": "按严重程度统计",
						},
						"by_status": map[string]interface{}{
							"type":        "object",
							"description": "按状态统计",
						},
					},
				},
				"RoleConfig": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"name": map[string]interface{}{
							"type":        "string",
							"description": "角色名称",
						},
						"description": map[string]interface{}{
							"type":        "string",
							"description": "角色描述",
						},
						"enabled": map[string]interface{}{
							"type":        "boolean",
							"description": "是否启用",
						},
						"systemPrompt": map[string]interface{}{
							"type":        "string",
							"description": "系统提示词",
						},
						"userPrompt": map[string]interface{}{
							"type":        "string",
							"description": "用户提示词",
						},
						"tools": map[string]interface{}{
							"type":        "array",
							"description": "工具列表",
							"items": map[string]interface{}{
								"type": "string",
							},
						},
					},
				},
				"Skill": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"name": map[string]interface{}{
							"type":        "string",
							"description": "Skill名称",
						},
						"description": map[string]interface{}{
							"type":        "string",
							"description": "Skill描述",
						},
						"path": map[string]interface{}{
							"type":        "string",
							"description": "Skill路径",
						},
					},
				},
				"CreateSkillRequest": map[string]interface{}{
					"type":     "object",
					"required": []string{"name", "description"},
					"properties": map[string]interface{}{
						"name": map[string]interface{}{
							"type":        "string",
							"description": "Skill名称",
						},
						"description": map[string]interface{}{
							"type":        "string",
							"description": "Skill描述",
						},
					},
				},
				"UpdateSkillRequest": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"description": map[string]interface{}{
							"type":        "string",
							"description": "Skill描述",
						},
					},
				},
				"ToolExecution": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id": map[string]interface{}{
							"type":        "string",
							"description": "执行ID",
						},
						"toolName": map[string]interface{}{
							"type":        "string",
							"description": "工具名称",
						},
						"status": map[string]interface{}{
							"type":        "string",
							"description": "执行状态",
							"enum":        []string{"queued", "running", "completed", "failed", "cancelled", "hard_timeout", "orphaned"},
						},
						"createdAt": map[string]interface{}{
							"type":        "string",
							"format":      "date-time",
							"description": "创建时间",
						},
					},
				},
				"MonitorResponse": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"executions": map[string]interface{}{
							"type":        "array",
							"description": "执行记录列表（轻量字段，不含 arguments/result）",
							"items": map[string]interface{}{
								"$ref": "#/components/schemas/ToolExecution",
							},
						},
						"summary": map[string]interface{}{
							"type":        "object",
							"description": "工具调用汇总",
						},
						"topTools": map[string]interface{}{
							"type":        "array",
							"description": "调用量 Top N 工具",
							"items": map[string]interface{}{
								"type": "object",
							},
						},
						"timestamp": map[string]interface{}{
							"type":        "string",
							"format":      "date-time",
							"description": "时间戳",
						},
						"total": map[string]interface{}{
							"type":        "integer",
							"description": "执行记录总数",
						},
						"page": map[string]interface{}{
							"type":        "integer",
							"description": "当前页",
						},
						"pageSize": map[string]interface{}{
							"type":        "integer",
							"description": "每页数量",
						},
						"totalPages": map[string]interface{}{
							"type":        "integer",
							"description": "总页数",
						},
						"retentionDays": map[string]interface{}{
							"type":        "integer",
							"description": "执行记录保留天数",
						},
					},
				},
				"ConfigResponse": map[string]interface{}{
					"type":        "object",
					"description": "配置信息（含 openai、vision、multi_agent 等）",
					"properties": map[string]interface{}{
						"agent": map[string]interface{}{
							"$ref": "#/components/schemas/AgentConfig",
						},
						"vision": map[string]interface{}{
							"$ref": "#/components/schemas/VisionConfig",
						},
					},
				},
				"UpdateConfigRequest": map[string]interface{}{
					"type":        "object",
					"description": "更新配置请求",
					"properties": map[string]interface{}{
						"agent": map[string]interface{}{
							"$ref": "#/components/schemas/AgentConfig",
						},
						"vision": map[string]interface{}{
							"$ref": "#/components/schemas/VisionConfig",
						},
					},
				},
				"AgentConfig": map[string]interface{}{
					"type":        "object",
					"description": "Agent 运行与外部 MCP 防卡死保护配置",
					"properties": map[string]interface{}{
						"max_iterations":                         map[string]interface{}{"type": "integer", "description": "最大迭代次数"},
						"tool_timeout_minutes":                   map[string]interface{}{"type": "integer", "description": "单次工具执行硬超时（分钟）"},
						"tool_wait_timeout_seconds":              map[string]interface{}{"type": "integer", "description": "工具单轮等待秒数；到时返回 execution_id，worker 继续后台执行"},
						"external_mcp_max_concurrent_per_server": map[string]interface{}{"type": "integer", "description": "单个外部 MCP server 并发上限；0=默认2；负数=不限制"},
						"external_mcp_max_concurrent_total":      map[string]interface{}{"type": "integer", "description": "外部 MCP 全局并发上限；0=默认16；负数=不限制"},
						"external_mcp_circuit_failure_threshold": map[string]interface{}{"type": "integer", "description": "连续失败熔断阈值；0=默认3；负数=关闭熔断"},
						"external_mcp_circuit_cooldown_seconds":  map[string]interface{}{"type": "integer", "description": "熔断冷却秒数；0=默认60"},
						"shell_no_output_timeout_seconds":        map[string]interface{}{"type": "integer", "description": "execute/exec 连续无输出终止秒数"},
						"workspace_root_dir":                     map[string]interface{}{"type": "string", "description": "会话工作目录根路径"},
						"system_prompt_path":                     map[string]interface{}{"type": "string", "description": "单代理系统提示文件路径"},
					},
				},
				"VisionConfig": map[string]interface{}{
					"type":        "object",
					"description": "视觉分析（analyze_image MCP 工具）；enabled 且 model 非空时注册工具",
					"properties": map[string]interface{}{
						"enabled":                     map[string]interface{}{"type": "boolean", "description": "是否启用 analyze_image"},
						"model":                       map[string]interface{}{"type": "string", "description": "视觉模型名（必填）", "example": "qwen-vl-max"},
						"api_key":                     map[string]interface{}{"type": "string", "description": "API Key；留空复用 openai.api_key"},
						"base_url":                    map[string]interface{}{"type": "string", "description": "Base URL；留空复用 openai.base_url"},
						"provider":                    map[string]interface{}{"type": "string", "description": "提供商；留空复用 openai.provider"},
						"timeout_seconds":             map[string]interface{}{"type": "integer", "description": "VL 调用超时（秒）"},
						"max_image_bytes":             map[string]interface{}{"type": "integer", "description": "原始文件大小上限（字节）"},
						"max_dimension":               map[string]interface{}{"type": "integer", "description": "长边缩放像素"},
						"jpeg_quality":                map[string]interface{}{"type": "integer", "description": "JPEG 质量 60-100"},
						"max_payload_bytes":           map[string]interface{}{"type": "integer", "description": "送 API 体积上限（字节）"},
						"skip_preprocess_below_bytes": map[string]interface{}{"type": "integer", "description": "低于该字节且尺寸合规时可原图直传；0=始终压缩"},
						"detail":                      map[string]interface{}{"type": "string", "enum": []string{"low", "high", "auto"}, "description": "OpenAI 兼容 image detail"},
					},
				},
				"AnalyzeImageToolCall": map[string]interface{}{
					"type":        "object",
					"description": "内置 MCP 工具 analyze_image：分析服务器本地图片，返回纯文本（验证码/UI/报错等）",
					"properties": map[string]interface{}{
						"path": map[string]interface{}{
							"type":        "string",
							"description": "图片绝对路径或相对于进程工作目录的路径",
						},
						"question": map[string]interface{}{
							"type":        "string",
							"description": "可选：重点问题；验证码建议「只输出验证码字符」",
						},
					},
					"required": []string{"path"},
				},
				"ExternalMCPConfig": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"enabled": map[string]interface{}{
							"type":        "boolean",
							"description": "是否启用",
						},
						"command": map[string]interface{}{
							"type":        "string",
							"description": "命令",
						},
						"args": map[string]interface{}{
							"type":        "array",
							"description": "参数列表",
							"items": map[string]interface{}{
								"type": "string",
							},
						},
					},
				},
				"ExternalMCPResponse": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"config": map[string]interface{}{
							"$ref": "#/components/schemas/ExternalMCPConfig",
						},
						"status": map[string]interface{}{
							"type":        "string",
							"description": "状态",
							"enum":        []string{"connected", "disconnected", "error", "disabled"},
						},
						"toolCount": map[string]interface{}{
							"type":        "integer",
							"description": "工具数量",
						},
						"error": map[string]interface{}{
							"type":        "string",
							"description": "错误信息",
						},
					},
				},
				"AddOrUpdateExternalMCPRequest": map[string]interface{}{
					"type":     "object",
					"required": []string{"config"},
					"properties": map[string]interface{}{
						"config": map[string]interface{}{
							"$ref": "#/components/schemas/ExternalMCPConfig",
						},
					},
				},
				"AttackChain": map[string]interface{}{
					"type":        "object",
					"description": "攻击链数据",
				},
				"MCPMessage": map[string]interface{}{
					"type":        "object",
					"description": "MCP消息（符合JSON-RPC 2.0规范）",
					"required":    []string{"jsonrpc"},
					"properties": map[string]interface{}{
						"id": map[string]interface{}{
							"description": "消息ID，可以是字符串、数字或null。对于请求，必须提供；对于通知，可以省略",
							"oneOf": []map[string]interface{}{
								{"type": "string"},
								{"type": "number"},
								{"type": "null"},
							},
							"example": "550e8400-e29b-41d4-a716-446655440000",
						},
						"method": map[string]interface{}{
							"type":        "string",
							"description": "方法名。支持的方法：\n- `initialize`: 初始化MCP连接\n- `tools/list`: 列出所有可用工具\n- `tools/call`: 调用工具\n- `prompts/list`: 列出所有提示词模板\n- `prompts/get`: 获取提示词模板\n- `resources/list`: 列出所有资源\n- `resources/read`: 读取资源内容\n- `sampling/request`: 采样请求",
							"enum": []string{
								"initialize",
								"tools/list",
								"tools/call",
								"prompts/list",
								"prompts/get",
								"resources/list",
								"resources/read",
								"sampling/request",
							},
							"example": "tools/list",
						},
						"params": map[string]interface{}{
							"description": "方法参数（JSON对象），根据不同的method有不同的结构",
							"type":        "object",
						},
						"jsonrpc": map[string]interface{}{
							"type":        "string",
							"description": "JSON-RPC版本，固定为\"2.0\"",
							"enum":        []string{"2.0"},
							"example":     "2.0",
						},
					},
				},
				"MCPInitializeParams": map[string]interface{}{
					"type":     "object",
					"required": []string{"protocolVersion", "capabilities", "clientInfo"},
					"properties": map[string]interface{}{
						"protocolVersion": map[string]interface{}{
							"type":        "string",
							"description": "协议版本",
							"example":     "2024-11-05",
						},
						"capabilities": map[string]interface{}{
							"type":        "object",
							"description": "客户端能力",
						},
						"clientInfo": map[string]interface{}{
							"type":     "object",
							"required": []string{"name", "version"},
							"properties": map[string]interface{}{
								"name": map[string]interface{}{
									"type":        "string",
									"description": "客户端名称",
									"example":     "MyClient",
								},
								"version": map[string]interface{}{
									"type":        "string",
									"description": "客户端版本",
									"example":     "1.0.0",
								},
							},
						},
					},
				},
				"MCPCallToolParams": map[string]interface{}{
					"type":     "object",
					"required": []string{"name", "arguments"},
					"properties": map[string]interface{}{
						"name": map[string]interface{}{
							"type":        "string",
							"description": "工具名称",
							"example":     "nmap",
						},
						"arguments": map[string]interface{}{
							"type":        "object",
							"description": "工具参数（键值对），具体参数取决于工具定义",
							"example": map[string]interface{}{
								"target": "192.168.1.1",
								"ports":  "80,443",
							},
						},
					},
				},
				"MCPResponse": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id": map[string]interface{}{
							"description": "消息ID（与请求中的id相同）",
							"oneOf": []map[string]interface{}{
								{"type": "string"},
								{"type": "number"},
								{"type": "null"},
							},
						},
						"result": map[string]interface{}{
							"description": "方法执行结果（JSON对象），结构取决于调用的方法",
							"type":        "object",
						},
						"error": map[string]interface{}{
							"type":        "object",
							"description": "错误信息（如果执行失败）",
							"properties": map[string]interface{}{
								"code": map[string]interface{}{
									"type":        "integer",
									"description": "错误代码",
									"example":     -32600,
								},
								"message": map[string]interface{}{
									"type":        "string",
									"description": "错误消息",
									"example":     "Invalid Request",
								},
								"data": map[string]interface{}{
									"description": "错误详情（可选）",
								},
							},
						},
						"jsonrpc": map[string]interface{}{
							"type":        "string",
							"description": "JSON-RPC版本",
							"example":     "2.0",
						},
					},
				},
			},
		},
		"security": []map[string]interface{}{
			{
				"bearerAuth": []string{},
			},
		},
		"paths": map[string]interface{}{
			"/api/auth/login": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"认证"},
					"summary":     "用户登录",
					"description": "使用密码登录获取认证Token",
					"operationId": "login",
					"security":    []map[string]interface{}{},
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/LoginRequest",
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "登录成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/LoginResponse",
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "密码错误",
						},
					},
				},
			},
			"/api/auth/logout": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"认证"},
					"summary":     "用户登出",
					"description": "登出当前会话，使Token失效",
					"operationId": "logout",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "登出成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"message": map[string]interface{}{
												"type":    "string",
												"example": "已退出登录",
											},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
			},
			"/api/auth/change-password": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"认证"},
					"summary":     "修改密码",
					"description": "修改登录密码，修改后所有会话将失效",
					"operationId": "changePassword",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/ChangePasswordRequest",
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "密码修改成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"message": map[string]interface{}{
												"type":    "string",
												"example": "密码已更新，请使用新密码重新登录",
											},
										},
									},
								},
							},
						},
						"400": map[string]interface{}{
							"description": "请求参数错误",
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
			},
			"/api/auth/validate": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"认证"},
					"summary":     "验证Token",
					"description": "验证当前Token是否有效",
					"operationId": "validateToken",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Token有效",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"token": map[string]interface{}{
												"type":        "string",
												"description": "Token",
											},
											"expires_at": map[string]interface{}{
												"type":        "string",
												"format":      "date-time",
												"description": "过期时间",
											},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "Token无效或已过期",
						},
					},
				},
			},
			"/api/conversations": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"对话管理"},
					"summary":     "创建对话",
					"description": "创建一个新的安全测试对话。\n**重要说明**：\n- ✅ 创建的对话会**立即保存到数据库**\n- ✅ 前端页面会**自动刷新**显示新对话\n- ✅ 与前端创建的对话**完全一致**\n**创建对话的两种方式**：\n**方式1（推荐）：** 直接使用 `/api/eino-agent` 发送消息，**不提供** `conversationId` 参数，系统会自动创建新对话并发送消息。这是最简单的方式，一步完成创建和发送。\n**方式2：** 先调用此端点创建空对话，然后使用返回的 `conversationId` 调用 `/api/eino-agent` 发送消息。适用于需要先创建对话，稍后再发送消息的场景。\n**示例**：\n```json\n{\n  \"title\": \"Web应用安全测试\"\n}\n```",
					"operationId": "createConversation",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/CreateConversationRequest",
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "对话创建成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/Conversation",
									},
								},
							},
						},
						"400": map[string]interface{}{
							"description": "请求参数错误",
						},
						"401": map[string]interface{}{
							"description": "未授权，需要有效的Token",
						},
						"500": map[string]interface{}{
							"description": "服务器内部错误",
						},
					},
				},
				"get": map[string]interface{}{
					"tags":        []string{"对话管理"},
					"summary":     "列出对话",
					"description": "获取对话列表，支持分页和搜索",
					"operationId": "listConversations",
					"parameters": []map[string]interface{}{
						{
							"name":        "limit",
							"in":          "query",
							"required":    false,
							"description": "返回数量限制",
							"schema": map[string]interface{}{
								"type":    "integer",
								"default": 50,
								"minimum": 1,
								"maximum": 100,
							},
						},
						{
							"name":        "offset",
							"in":          "query",
							"required":    false,
							"description": "偏移量",
							"schema": map[string]interface{}{
								"type":    "integer",
								"default": 0,
								"minimum": 0,
							},
						},
						{
							"name":        "search",
							"in":          "query",
							"required":    false,
							"description": "搜索关键词",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
						{
							"name":        "project_id",
							"in":          "query",
							"required":    false,
							"description": "按项目筛选；传 __none__ 表示仅未绑定项目的对话",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
						{
							"name":        "sort_by",
							"in":          "query",
							"required":    false,
							"description": "排序字段：updated_at（默认）或 created_at",
							"schema": map[string]interface{}{
								"type": "string",
								"enum": []string{"updated_at", "created_at"},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "获取成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "array",
										"items": map[string]interface{}{
											"$ref": "#/components/schemas/Conversation",
										},
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "未授权，需要有效的Token",
						},
					},
				},
			},
			"/api/conversations/{id}": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"对话管理"},
					"summary":     "查看对话详情",
					"description": "获取指定对话的详细信息，包括对话信息和消息列表",
					"operationId": "getConversation",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "对话ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "获取成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/ConversationDetail",
									},
								},
							},
						},
						"404": map[string]interface{}{
							"description": "对话不存在",
						},
						"401": map[string]interface{}{
							"description": "未授权，需要有效的Token",
						},
					},
				},
				"put": map[string]interface{}{
					"tags":        []string{"对话管理"},
					"summary":     "更新对话",
					"description": "更新对话标题",
					"operationId": "updateConversation",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "对话ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/UpdateConversationRequest",
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "更新成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/Conversation",
									},
								},
							},
						},
						"400": map[string]interface{}{
							"description": "请求参数错误",
						},
						"404": map[string]interface{}{
							"description": "对话不存在",
						},
						"401": map[string]interface{}{
							"description": "未授权，需要有效的Token",
						},
					},
				},
				"delete": map[string]interface{}{
					"tags":        []string{"对话管理"},
					"summary":     "删除对话",
					"description": "删除指定的对话及其会话数据（消息、攻击链等）。**漏洞记录会保留**，仅解除与会话的关联。**此操作不可恢复**。",
					"operationId": "deleteConversation",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "对话ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "删除成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"message": map[string]interface{}{
												"type":        "string",
												"description": "成功消息",
												"example":     "删除成功",
											},
										},
									},
								},
							},
						},
						"404": map[string]interface{}{
							"description": "对话不存在",
						},
						"401": map[string]interface{}{
							"description": "未授权，需要有效的Token",
						},
						"500": map[string]interface{}{
							"description": "服务器内部错误",
						},
					},
				},
			},
			"/api/conversations/{id}/project": map[string]interface{}{
				"put": map[string]interface{}{
					"tags":        []string{"对话管理"},
					"summary":     "设置对话所属项目",
					"description": "绑定或解除对话与项目的关联，用于共享事实黑板",
					"operationId": "setConversationProject",
					"parameters": []map[string]interface{}{
						{
							"name": "id", "in": "path", "required": true,
							"description": "对话ID",
							"schema":      map[string]interface{}{"type": "string"},
						},
					},
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/SetConversationProjectRequest",
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "设置成功"},
						"400": map[string]interface{}{"description": "项目不存在或参数错误"},
						"404": map[string]interface{}{"description": "对话不存在"},
						"401": map[string]interface{}{"description": "未授权"},
					},
				},
			},
			"/api/conversations/{id}/results": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"对话管理"},
					"summary":     "获取对话结果",
					"description": "获取指定对话的执行结果，包括消息、漏洞信息和执行结果",
					"operationId": "getConversationResults",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "对话ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "获取成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/ConversationResults",
									},
								},
							},
						},
						"404": map[string]interface{}{
							"description": "对话不存在或结果不存在",
						},
						"401": map[string]interface{}{
							"description": "未授权，需要有效的Token",
						},
					},
				},
			},
			"/api/eino-agent": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"对话交互"},
					"summary":     "发送消息并获取 AI 回复（Eino ADK 单代理，非流式）",
					"description": "向 AI 发送消息并获取回复（非流式）。由 **CloudWeGo Eino** `adk.NewChatModelAgent` + `adk.NewRunner.Run` 执行单代理 MCP 工具链。**不依赖** `multi_agent.enabled`；`multi_agent.eino_skills` / `eino_middleware` 等与多代理主代理一致时可生效。支持 `webshellConnectionId`、角色与附件。",
					"operationId": "sendMessageEinoSingleAgent",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"message":              map[string]interface{}{"type": "string"},
										"conversationId":       map[string]interface{}{"type": "string"},
										"role":                 map[string]interface{}{"type": "string"},
										"webshellConnectionId": map[string]interface{}{"type": "string"},
										"finalization":         finalizationRequestSchema,
									},
									"required": []string{"message"},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "成功。只有 finalized=true 表示成功最终回复；finalized=false 时 response 为未完成/阻断说明。",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{"$ref": "#/components/schemas/AgentChatResponse"},
								},
							},
						},
						"400": map[string]interface{}{"description": "参数错误"},
						"401": map[string]interface{}{"description": "未授权"},
						"500": map[string]interface{}{"description": "执行失败"},
					},
				},
			},
			"/api/eino-agent/stream": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"对话交互"},
					"summary":     "发送消息并获取 AI 回复（Eino ADK 单代理，SSE）",
					"description": "向 AI 发送消息并获取流式回复（SSE）。由 Eino **单代理** ADK 执行；事件类型与多代理流式一致（含 `tool_call` / `response_delta` / `thinking` 等）。`response_start` / `response_delta` 仅为候选/过程输出；只有 `type: response` 且 `data.finalized=true` 才表示成功最终回复。缺 completed 执行证据时可能先发送 `finalization_auto_continue`，表示服务端基于已有 trace 无注入续跑。`data.finalized=false` 时 message 为未完成/阻断说明。**不依赖** `multi_agent.enabled`。",
					"operationId": "sendMessageEinoSingleAgentStream",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"message":              map[string]interface{}{"type": "string"},
										"conversationId":       map[string]interface{}{"type": "string"},
										"role":                 map[string]interface{}{"type": "string"},
										"webshellConnectionId": map[string]interface{}{"type": "string"},
										"finalization":         finalizationRequestSchema,
									},
									"required": []string{"message"},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "text/event-stream（SSE）",
							"content": map[string]interface{}{
								"text/event-stream": map[string]interface{}{
									"schema": map[string]interface{}{
										"type":        "string",
										"description": "SSE 流。终态 response 事件 data 包含 finalized、finalizable、status、completionReason、evidenceVerified、evidenceRefs、pendingExecutionIds、missingChecks；过程事件可能包含 finalization_auto_continue。",
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "未授权"},
					},
				},
			},
			"/api/multi-agent": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"对话交互"},
					"summary":     "发送消息并获取 AI 回复（Eino 多代理，非流式）",
					"description": "与 `POST /api/eino-agent` 请求体相同，但由 **CloudWeGo Eino** 多代理执行。编排由请求体 `orchestration`（`deep` | `plan_execute` | `supervisor`）指定，缺省为 `deep`。**前提**：`multi_agent.enabled: true`；未启用时返回 404 JSON。支持 `webshellConnectionId`。",
					"operationId": "sendMessageMultiAgent",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"message": map[string]interface{}{
											"type":        "string",
											"description": "要发送的消息（必需）",
										},
										"conversationId": map[string]interface{}{
											"type":        "string",
											"description": "对话 ID（可选，不提供则新建）",
										},
										"role": map[string]interface{}{
											"type":        "string",
											"description": "角色名称（可选）",
										},
										"webshellConnectionId": map[string]interface{}{
											"type":        "string",
											"description": "WebShell 连接 ID（可选，与 Eino 单/多代理流式行为一致）",
										},
										"finalization": finalizationRequestSchema,
										"orchestration": map[string]interface{}{
											"type":        "string",
											"description": "Eino 预置编排：deep | plan_execute | supervisor；缺省 deep",
											"enum":        []string{"deep", "plan_execute", "supervisor"},
										},
									},
									"required": []string{"message"},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "成功。只有 finalized=true 表示成功最终回复；finalized=false 时 response 为未完成/阻断说明。",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{"$ref": "#/components/schemas/AgentChatResponse"},
								},
							},
						},
						"400": map[string]interface{}{"description": "参数错误"},
						"401": map[string]interface{}{"description": "未授权"},
						"404": map[string]interface{}{"description": "多代理未启用或对话不存在"},
						"500": map[string]interface{}{"description": "执行失败"},
					},
				},
			},
			"/api/multi-agent/stream": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"对话交互"},
					"summary":     "发送消息并获取 AI 回复（Eino 多代理，SSE）",
					"description": "与 `POST /api/eino-agent/stream` 类似；由 Eino 多代理执行。`orchestration` 指定 deep / plan_execute / supervisor，缺省 deep。`response_start` / `response_delta` 仅为候选/过程输出；只有 `type: response` 且 `data.finalized=true` 才表示成功最终回复。缺 completed 执行证据时可能先发送 `finalization_auto_continue`，表示服务端基于已有 trace 无注入续跑。**前提**：`multi_agent.enabled: true`；未启用时 SSE 内首条为 `type: error` 后接 `done`。支持 `webshellConnectionId`。",
					"operationId": "sendMessageMultiAgentStream",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"message":              map[string]interface{}{"type": "string"},
										"conversationId":       map[string]interface{}{"type": "string"},
										"role":                 map[string]interface{}{"type": "string"},
										"webshellConnectionId": map[string]interface{}{"type": "string"},
										"finalization":         finalizationRequestSchema,
										"orchestration": map[string]interface{}{
											"type":        "string",
											"description": "deep | plan_execute | supervisor；缺省 deep",
											"enum":        []string{"deep", "plan_execute", "supervisor"},
										},
									},
									"required": []string{"message"},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "text/event-stream（SSE）",
							"content": map[string]interface{}{
								"text/event-stream": map[string]interface{}{
									"schema": map[string]interface{}{
										"type":        "string",
										"description": "SSE 流。终态 response 事件 data 包含 finalized、finalizable、status、completionReason、evidenceVerified、evidenceRefs、pendingExecutionIds、missingChecks；过程事件可能包含 finalization_auto_continue。",
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "未授权"},
					},
				},
			},
			"/api/agent-loop/cancel": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"对话交互"},
					"summary":     "取消任务",
					"description": "取消正在执行的Agent Loop任务",
					"operationId": "cancelAgentLoop",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/CancelAgentLoopRequest",
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "取消请求已提交",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"status": map[string]interface{}{
												"type":    "string",
												"example": "cancelling",
											},
											"conversationId": map[string]interface{}{
												"type":        "string",
												"description": "对话ID",
											},
											"message": map[string]interface{}{
												"type":    "string",
												"example": "已提交取消请求，任务将在当前步骤完成后停止。",
											},
										},
									},
								},
							},
						},
						"404": map[string]interface{}{
							"description": "未找到正在执行的任务",
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
			},
			"/api/agent-loop/tasks": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"对话交互"},
					"summary":     "列出运行中的任务",
					"description": "获取所有正在运行的Agent Loop任务",
					"operationId": "listAgentTasks",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "获取成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"tasks": map[string]interface{}{
												"type":        "array",
												"description": "任务列表",
												"items": map[string]interface{}{
													"$ref": "#/components/schemas/AgentTask",
												},
											},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
			},
			"/api/agent-loop/tasks/completed": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"对话交互"},
					"summary":     "列出已完成的任务",
					"description": "获取最近完成的Agent Loop任务历史",
					"operationId": "listCompletedTasks",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "获取成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"tasks": map[string]interface{}{
												"type":        "array",
												"description": "已完成任务列表",
												"items": map[string]interface{}{
													"$ref": "#/components/schemas/AgentTask",
												},
											},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
			},
			"/api/batch-tasks": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"批量任务"},
					"summary":     "创建批量任务队列",
					"description": "创建一个批量任务队列，包含多个任务",
					"operationId": "createBatchQueue",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/BatchTaskRequest",
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "创建成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"queueId": map[string]interface{}{
												"type":        "string",
												"description": "队列ID",
											},
											"queue": map[string]interface{}{
												"$ref": "#/components/schemas/BatchQueue",
											},
											"started": map[string]interface{}{
												"type":        "boolean",
												"description": "是否已立即启动执行",
											},
										},
									},
								},
							},
						},
						"400": map[string]interface{}{
							"description": "请求参数错误",
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
				"get": map[string]interface{}{
					"tags":        []string{"批量任务"},
					"summary":     "列出批量任务队列",
					"description": "获取所有批量任务队列",
					"operationId": "listBatchQueues",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "获取成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"queues": map[string]interface{}{
												"type":        "array",
												"description": "队列列表",
												"items": map[string]interface{}{
													"$ref": "#/components/schemas/BatchQueue",
												},
											},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
			},
			"/api/batch-tasks/{queueId}": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"批量任务"},
					"summary":     "获取批量任务队列",
					"description": "获取指定批量任务队列的详细信息",
					"operationId": "getBatchQueue",
					"parameters": []map[string]interface{}{
						{
							"name":        "queueId",
							"in":          "path",
							"required":    true,
							"description": "队列ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "获取成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/BatchQueue",
									},
								},
							},
						},
						"404": map[string]interface{}{
							"description": "队列不存在",
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
				"delete": map[string]interface{}{
					"tags":        []string{"批量任务"},
					"summary":     "删除批量任务队列",
					"description": "删除指定的批量任务队列",
					"operationId": "deleteBatchQueue",
					"parameters": []map[string]interface{}{
						{
							"name":        "queueId",
							"in":          "path",
							"required":    true,
							"description": "队列ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "删除成功",
						},
						"404": map[string]interface{}{
							"description": "队列不存在",
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
			},
			"/api/batch-tasks/{queueId}/start": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"批量任务"},
					"summary":     "启动批量任务队列",
					"description": "开始执行批量任务队列中的任务",
					"operationId": "startBatchQueue",
					"parameters": []map[string]interface{}{
						{
							"name":        "queueId",
							"in":          "path",
							"required":    true,
							"description": "队列ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "启动成功",
						},
						"404": map[string]interface{}{
							"description": "队列不存在",
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
			},
			"/api/batch-tasks/{queueId}/pause": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"批量任务"},
					"summary":     "暂停批量任务队列",
					"description": "暂停正在执行的批量任务队列",
					"operationId": "pauseBatchQueue",
					"parameters": []map[string]interface{}{
						{
							"name":        "queueId",
							"in":          "path",
							"required":    true,
							"description": "队列ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "暂停成功",
						},
						"404": map[string]interface{}{
							"description": "队列不存在",
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
			},
			"/api/batch-tasks/{queueId}/tasks": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"批量任务"},
					"summary":     "添加任务到队列",
					"description": "向批量任务队列添加新任务。任务会添加到队列末尾，按照队列顺序依次执行。每个任务会创建一个独立的对话，支持完整的状态跟踪。\n**任务格式**：\n任务内容是一个字符串，描述要执行的安全测试任务。例如：\n- \"扫描 http://example.com 的SQL注入漏洞\"\n- \"对 192.168.1.1 进行端口扫描\"\n- \"检测 https://target.com 的XSS漏洞\"\n**使用示例**：\n```json\n{\n  \"task\": \"扫描 http://example.com 的SQL注入漏洞\"\n}\n```",
					"operationId": "addBatchTask",
					"parameters": []map[string]interface{}{
						{
							"name":        "queueId",
							"in":          "path",
							"required":    true,
							"description": "队列ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"task"},
									"properties": map[string]interface{}{
										"task": map[string]interface{}{
											"type":        "string",
											"description": "任务内容，描述要执行的安全测试任务（必需）",
											"example":     "扫描 http://example.com 的SQL注入漏洞",
										},
									},
								},
								"examples": map[string]interface{}{
									"sqlInjection": map[string]interface{}{
										"summary":     "SQL注入扫描",
										"description": "扫描目标网站的SQL注入漏洞",
										"value": map[string]interface{}{
											"task": "扫描 http://example.com 的SQL注入漏洞",
										},
									},
									"portScan": map[string]interface{}{
										"summary":     "端口扫描",
										"description": "对目标IP进行端口扫描",
										"value": map[string]interface{}{
											"task": "对 192.168.1.1 进行端口扫描",
										},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "添加成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"taskId": map[string]interface{}{
												"type":        "string",
												"description": "新添加的任务ID",
											},
											"message": map[string]interface{}{
												"type":        "string",
												"description": "成功消息",
												"example":     "任务已添加到队列",
											},
										},
									},
								},
							},
						},
						"400": map[string]interface{}{
							"description": "请求参数错误（如task为空）",
						},
						"404": map[string]interface{}{
							"description": "队列不存在",
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
			},
			"/api/batch-tasks/{queueId}/tasks/{taskId}": map[string]interface{}{
				"put": map[string]interface{}{
					"tags":        []string{"批量任务"},
					"summary":     "更新批量任务",
					"description": "更新批量任务队列中的指定任务",
					"operationId": "updateBatchTask",
					"parameters": []map[string]interface{}{
						{
							"name":        "queueId",
							"in":          "path",
							"required":    true,
							"description": "队列ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
						{
							"name":        "taskId",
							"in":          "path",
							"required":    true,
							"description": "任务ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"task": map[string]interface{}{
											"type":        "string",
											"description": "任务内容",
										},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "更新成功",
						},
						"404": map[string]interface{}{
							"description": "任务不存在",
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
				"delete": map[string]interface{}{
					"tags":        []string{"批量任务"},
					"summary":     "删除批量任务",
					"description": "从批量任务队列中删除指定任务",
					"operationId": "deleteBatchTask",
					"parameters": []map[string]interface{}{
						{
							"name":        "queueId",
							"in":          "path",
							"required":    true,
							"description": "队列ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
						{
							"name":        "taskId",
							"in":          "path",
							"required":    true,
							"description": "任务ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "删除成功",
						},
						"404": map[string]interface{}{
							"description": "任务不存在",
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
			},
			"/api/assets/import": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"资产管理"},
					"summary":     "批量导入资产",
					"description": "新增或按“目标 + 端口 + 协议”去重更新资产。接收 JSON，不直接接收 XLSX/CSV 文件；单次最多 100000 条，需要 asset:write 权限。",
					"operationId": "importAssets",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{"$ref": "#/components/schemas/AssetImportRequest"},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "导入完成",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{"$ref": "#/components/schemas/AssetImportResult"},
								},
							},
						},
						"400": map[string]interface{}{"description": "数量或资产字段校验失败"},
						"401": map[string]interface{}{"description": "未授权"},
						"403": map[string]interface{}{"description": "缺少 asset:write 权限或无权访问指定项目"},
						"500": map[string]interface{}{"description": "导入事务失败"},
					},
				},
			},
			"/api/projects": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"项目管理"},
					"summary":     "列出项目",
					"operationId": "listProjects",
					"parameters": []map[string]interface{}{
						{"name": "status", "in": "query", "schema": map[string]interface{}{"type": "string", "enum": []string{"active", "archived"}}},
						{"name": "limit", "in": "query", "schema": map[string]interface{}{"type": "integer", "default": 200}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "项目列表"},
						"401": map[string]interface{}{"description": "未授权"},
					},
				},
				"post": map[string]interface{}{
					"tags":        []string{"项目管理"},
					"summary":     "创建项目",
					"operationId": "createProject",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"name":        map[string]interface{}{"type": "string"},
										"description": map[string]interface{}{"type": "string"},
										"scope_json":  map[string]interface{}{"type": "string"},
									},
									"required": []string{"name"},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "创建成功"},
						"401": map[string]interface{}{"description": "未授权"},
					},
				},
			},
			"/api/projects/{id}": map[string]interface{}{
				"get": map[string]interface{}{
					"tags": []string{"项目管理"}, "summary": "获取项目", "operationId": "getProject",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{"200": map[string]interface{}{"description": "项目详情"}},
				},
				"put": map[string]interface{}{
					"tags": []string{"项目管理"}, "summary": "更新项目", "operationId": "updateProject",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{"200": map[string]interface{}{"description": "更新成功"}},
				},
				"delete": map[string]interface{}{
					"tags": []string{"项目管理"}, "summary": "删除项目", "operationId": "deleteProject",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{"200": map[string]interface{}{"description": "删除成功"}},
				},
			},
			"/api/projects/{id}/facts": map[string]interface{}{
				"get": map[string]interface{}{
					"tags": []string{"项目管理"}, "summary": "列出或按 key 获取事实", "operationId": "listProjectFacts",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "schema": map[string]interface{}{"type": "string"}},
						{"name": "fact_key", "in": "query", "schema": map[string]interface{}{"type": "string"}},
						{"name": "include_links", "in": "query", "schema": map[string]interface{}{"type": "boolean"}},
						{"name": "include_link_counts", "in": "query", "schema": map[string]interface{}{"type": "boolean"}},
					},
					"responses": map[string]interface{}{"200": map[string]interface{}{"description": "事实列表或单条（可含 link_counts / outgoing_links）"}},
				},
				"post": map[string]interface{}{
					"tags": []string{"项目管理"}, "summary": "创建/更新事实", "operationId": "upsertProjectFactREST",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "schema": map[string]interface{}{"type": "string"}},
					},
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"fact_key": map[string]interface{}{"type": "string"},
										"summary":  map[string]interface{}{"type": "string"},
										"links": map[string]interface{}{
											"type": "array",
											"items": map[string]interface{}{
												"type": "object",
												"properties": map[string]interface{}{
													"to":   map[string]interface{}{"type": "string"},
													"type": map[string]interface{}{"type": "string"},
												},
											},
										},
										"links_text": map[string]interface{}{"type": "string", "description": "type: fact_key 每行一条"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{"200": map[string]interface{}{"description": "成功"}},
				},
			},
			"/api/projects/{id}/fact-graph": map[string]interface{}{
				"get": map[string]interface{}{
					"tags": []string{"项目管理"}, "summary": "获取项目事实攻击路径图", "operationId": "getProjectFactGraph",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "schema": map[string]interface{}{"type": "string"}},
						{"name": "view", "in": "query", "schema": map[string]interface{}{"type": "string", "enum": []string{"path", "full"}, "default": "path"}},
						{"name": "exclude_deprecated", "in": "query", "schema": map[string]interface{}{"type": "boolean", "default": true}},
					},
					"responses": map[string]interface{}{"200": map[string]interface{}{"description": "nodes + edges"}},
				},
			},
			"/api/projects/{id}/fact-edges": map[string]interface{}{
				"get": map[string]interface{}{
					"tags": []string{"项目管理"}, "summary": "列出项目全部事实边", "operationId": "listProjectFactEdges",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{"200": map[string]interface{}{"description": "边列表"}},
				},
				"post": map[string]interface{}{
					"tags": []string{"项目管理"}, "summary": "添加事实边", "operationId": "createProjectFactEdge",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "schema": map[string]interface{}{"type": "string"}},
					},
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"source_fact_key", "target_fact_key", "edge_type"},
									"properties": map[string]interface{}{
										"source_fact_key": map[string]interface{}{"type": "string"},
										"target_fact_key": map[string]interface{}{"type": "string"},
										"edge_type":       map[string]interface{}{"type": "string"},
										"confidence":      map[string]interface{}{"type": "string"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{"200": map[string]interface{}{"description": "边已创建"}},
				},
			},
			"/api/projects/{id}/fact-edges/{edgeId}": map[string]interface{}{
				"delete": map[string]interface{}{
					"tags": []string{"项目管理"}, "summary": "删除事实边", "operationId": "deleteProjectFactEdge",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "schema": map[string]interface{}{"type": "string"}},
						{"name": "edgeId", "in": "path", "required": true, "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{"200": map[string]interface{}{"description": "删除成功"}},
				},
			},
			"/api/projects/{id}/promote-attack-chain/{conversationId}": map[string]interface{}{
				"post": map[string]interface{}{
					"tags": []string{"项目管理"}, "summary": "将对话攻击链沉淀到项目事实图", "operationId": "promoteAttackChainToProject",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "schema": map[string]interface{}{"type": "string"}},
						{"name": "conversationId", "in": "path", "required": true, "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{"200": map[string]interface{}{"description": "沉淀结果（facts/edges/graph）"}},
				},
			},
			"/api/vulnerabilities": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"漏洞管理"},
					"summary":     "列出漏洞",
					"description": "获取漏洞列表，支持分页和筛选",
					"operationId": "listVulnerabilities",
					"parameters": []map[string]interface{}{
						{
							"name":        "limit",
							"in":          "query",
							"required":    false,
							"description": "每页数量",
							"schema": map[string]interface{}{
								"type":    "integer",
								"default": 20,
								"minimum": 1,
								"maximum": 100,
							},
						},
						{
							"name":        "offset",
							"in":          "query",
							"required":    false,
							"description": "偏移量",
							"schema": map[string]interface{}{
								"type":    "integer",
								"default": 0,
								"minimum": 0,
							},
						},
						{
							"name":        "page",
							"in":          "query",
							"required":    false,
							"description": "页码（与offset二选一）",
							"schema": map[string]interface{}{
								"type":    "integer",
								"minimum": 1,
							},
						},
						{
							"name":        "id",
							"in":          "query",
							"required":    false,
							"description": "漏洞ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
						{
							"name":        "conversation_id",
							"in":          "query",
							"required":    false,
							"description": "对话ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
						{
							"name":        "project_id",
							"in":          "query",
							"required":    false,
							"description": "项目ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
						{
							"name":        "severity",
							"in":          "query",
							"required":    false,
							"description": "严重程度",
							"schema": map[string]interface{}{
								"type": "string",
								"enum": []string{"critical", "high", "medium", "low", "info"},
							},
						},
						{
							"name":        "status",
							"in":          "query",
							"required":    false,
							"description": "状态",
							"schema": map[string]interface{}{
								"type": "string",
								"enum": []string{"open", "closed", "fixed"},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "获取成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/ListVulnerabilitiesResponse",
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
				"post": map[string]interface{}{
					"tags":        []string{"漏洞管理"},
					"summary":     "创建漏洞",
					"description": "创建一个新的漏洞记录",
					"operationId": "createVulnerability",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/CreateVulnerabilityRequest",
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "创建成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/Vulnerability",
									},
								},
							},
						},
						"400": map[string]interface{}{
							"description": "请求参数错误",
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
			},
			"/api/vulnerabilities/stats": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"漏洞管理"},
					"summary":     "获取漏洞统计",
					"description": "获取漏洞统计信息",
					"operationId": "getVulnerabilityStats",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "获取成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/VulnerabilityStats",
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
			},
			"/api/vulnerabilities/{id}": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"漏洞管理"},
					"summary":     "获取漏洞",
					"description": "获取指定漏洞的详细信息",
					"operationId": "getVulnerability",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "漏洞ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "获取成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/Vulnerability",
									},
								},
							},
						},
						"404": map[string]interface{}{
							"description": "漏洞不存在",
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
				"put": map[string]interface{}{
					"tags":        []string{"漏洞管理"},
					"summary":     "更新漏洞",
					"description": "更新漏洞信息",
					"operationId": "updateVulnerability",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "漏洞ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/UpdateVulnerabilityRequest",
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "更新成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/Vulnerability",
									},
								},
							},
						},
						"400": map[string]interface{}{
							"description": "请求参数错误",
						},
						"404": map[string]interface{}{
							"description": "漏洞不存在",
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
				"delete": map[string]interface{}{
					"tags":        []string{"漏洞管理"},
					"summary":     "删除漏洞",
					"description": "删除指定漏洞",
					"operationId": "deleteVulnerability",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "漏洞ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "删除成功",
						},
						"404": map[string]interface{}{
							"description": "漏洞不存在",
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
			},
			"/api/roles": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"角色管理"},
					"summary":     "列出角色",
					"description": "获取所有安全测试角色",
					"operationId": "getRoles",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "获取成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"roles": map[string]interface{}{
												"type":        "array",
												"description": "角色列表",
												"items": map[string]interface{}{
													"$ref": "#/components/schemas/RoleConfig",
												},
											},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
				"post": map[string]interface{}{
					"tags":        []string{"角色管理"},
					"summary":     "创建角色",
					"description": "创建一个新的安全测试角色",
					"operationId": "createRole",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/RoleConfig",
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "创建成功",
						},
						"400": map[string]interface{}{
							"description": "请求参数错误",
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
			},
			"/api/roles/{name}": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"角色管理"},
					"summary":     "获取角色",
					"description": "获取指定角色的详细信息",
					"operationId": "getRole",
					"parameters": []map[string]interface{}{
						{
							"name":        "name",
							"in":          "path",
							"required":    true,
							"description": "角色名称",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "获取成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"role": map[string]interface{}{
												"$ref": "#/components/schemas/RoleConfig",
											},
										},
									},
								},
							},
						},
						"404": map[string]interface{}{
							"description": "角色不存在",
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
				"put": map[string]interface{}{
					"tags":        []string{"角色管理"},
					"summary":     "更新角色",
					"description": "更新指定角色的配置",
					"operationId": "updateRole",
					"parameters": []map[string]interface{}{
						{
							"name":        "name",
							"in":          "path",
							"required":    true,
							"description": "角色名称",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/RoleConfig",
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "更新成功",
						},
						"400": map[string]interface{}{
							"description": "请求参数错误",
						},
						"404": map[string]interface{}{
							"description": "角色不存在",
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
				"delete": map[string]interface{}{
					"tags":        []string{"角色管理"},
					"summary":     "删除角色",
					"description": "删除指定角色",
					"operationId": "deleteRole",
					"parameters": []map[string]interface{}{
						{
							"name":        "name",
							"in":          "path",
							"required":    true,
							"description": "角色名称",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "删除成功",
						},
						"404": map[string]interface{}{
							"description": "角色不存在",
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
			},
			"/api/skills": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Skills管理"},
					"summary":     "列出Skills",
					"description": "获取所有Skills列表，支持分页和搜索",
					"operationId": "getSkills",
					"parameters": []map[string]interface{}{
						{
							"name":        "limit",
							"in":          "query",
							"required":    false,
							"description": "每页数量",
							"schema": map[string]interface{}{
								"type":    "integer",
								"default": 20,
							},
						},
						{
							"name":        "offset",
							"in":          "query",
							"required":    false,
							"description": "偏移量",
							"schema": map[string]interface{}{
								"type":    "integer",
								"default": 0,
							},
						},
						{
							"name":        "search",
							"in":          "query",
							"required":    false,
							"description": "搜索关键词",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "获取成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"skills": map[string]interface{}{
												"type":        "array",
												"description": "Skills列表",
												"items": map[string]interface{}{
													"$ref": "#/components/schemas/Skill",
												},
											},
											"total": map[string]interface{}{
												"type":        "integer",
												"description": "总数",
											},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
				"post": map[string]interface{}{
					"tags":        []string{"Skills管理"},
					"summary":     "创建Skill",
					"description": "创建一个新的Skill",
					"operationId": "createSkill",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/CreateSkillRequest",
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "创建成功",
						},
						"400": map[string]interface{}{
							"description": "请求参数错误",
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
			},
			"/api/skills/stats": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Skills管理"},
					"summary":     "获取Skill统计",
					"description": "获取Skill调用统计信息",
					"operationId": "getSkillStats",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "获取成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type":        "object",
										"description": "统计信息",
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
				"delete": map[string]interface{}{
					"tags":        []string{"Skills管理"},
					"summary":     "清空Skill统计",
					"description": "清空所有Skill的调用统计",
					"operationId": "clearSkillStats",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "清空成功",
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
			},
			"/api/skills/{name}": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Skills管理"},
					"summary":     "获取Skill",
					"description": "获取指定Skill的详细信息",
					"operationId": "getSkill",
					"parameters": []map[string]interface{}{
						{
							"name":        "name",
							"in":          "path",
							"required":    true,
							"description": "Skill名称",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "获取成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/Skill",
									},
								},
							},
						},
						"404": map[string]interface{}{
							"description": "Skill不存在",
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
				"put": map[string]interface{}{
					"tags":        []string{"Skills管理"},
					"summary":     "更新Skill",
					"description": "更新指定Skill的信息",
					"operationId": "updateSkill",
					"parameters": []map[string]interface{}{
						{
							"name":        "name",
							"in":          "path",
							"required":    true,
							"description": "Skill名称",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/UpdateSkillRequest",
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "更新成功",
						},
						"400": map[string]interface{}{
							"description": "请求参数错误",
						},
						"404": map[string]interface{}{
							"description": "Skill不存在",
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
				"delete": map[string]interface{}{
					"tags":        []string{"Skills管理"},
					"summary":     "删除Skill",
					"description": "删除指定Skill",
					"operationId": "deleteSkill",
					"parameters": []map[string]interface{}{
						{
							"name":        "name",
							"in":          "path",
							"required":    true,
							"description": "Skill名称",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "删除成功",
						},
						"404": map[string]interface{}{
							"description": "Skill不存在",
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
			},
			"/api/skills/{name}/bound-roles": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Skills管理"},
					"summary":     "获取绑定角色",
					"description": "获取使用指定Skill的所有角色",
					"operationId": "getSkillBoundRoles",
					"parameters": []map[string]interface{}{
						{
							"name":        "name",
							"in":          "path",
							"required":    true,
							"description": "Skill名称",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "获取成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"roles": map[string]interface{}{
												"type":        "array",
												"description": "角色列表",
												"items": map[string]interface{}{
													"type": "string",
												},
											},
										},
									},
								},
							},
						},
						"404": map[string]interface{}{
							"description": "Skill不存在",
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
			},
			"/api/skills/{name}/stats": map[string]interface{}{
				"delete": map[string]interface{}{
					"tags":        []string{"Skills管理"},
					"summary":     "清空Skill统计",
					"description": "清空指定Skill的调用统计",
					"operationId": "clearSkillStatsByName",
					"parameters": []map[string]interface{}{
						{
							"name":        "name",
							"in":          "path",
							"required":    true,
							"description": "Skill名称",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "清空成功",
						},
						"404": map[string]interface{}{
							"description": "Skill不存在",
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
			},
			"/api/monitor": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"监控"},
					"summary":     "获取监控信息",
					"description": "获取工具执行监控信息，支持分页和筛选",
					"operationId": "monitor",
					"parameters": []map[string]interface{}{
						{
							"name":        "page",
							"in":          "query",
							"required":    false,
							"description": "页码",
							"schema": map[string]interface{}{
								"type":    "integer",
								"default": 1,
								"minimum": 1,
							},
						},
						{
							"name":        "page_size",
							"in":          "query",
							"required":    false,
							"description": "每页数量",
							"schema": map[string]interface{}{
								"type":    "integer",
								"default": 20,
								"minimum": 1,
								"maximum": 100,
							},
						},
						{
							"name":        "status",
							"in":          "query",
							"required":    false,
							"description": "状态筛选",
							"schema": map[string]interface{}{
								"type": "string",
								"enum": []string{"queued", "running", "completed", "failed", "cancelled", "hard_timeout", "orphaned"},
							},
						},
						{
							"name":        "tool",
							"in":          "query",
							"required":    false,
							"description": "工具名称筛选（支持部分匹配）",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "获取成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/MonitorResponse",
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
			},
			"/api/monitor/execution/{id}": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"监控"},
					"summary":     "获取执行记录",
					"description": "获取指定执行记录的详细信息",
					"operationId": "getExecution",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "执行ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "获取成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/ToolExecution",
									},
								},
							},
						},
						"404": map[string]interface{}{
							"description": "执行记录不存在",
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
				"delete": map[string]interface{}{
					"tags":        []string{"监控"},
					"summary":     "删除执行记录",
					"description": "删除指定的执行记录",
					"operationId": "deleteExecution",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "执行ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "删除成功",
						},
						"404": map[string]interface{}{
							"description": "执行记录不存在",
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
			},
			"/api/monitor/execution/{id}/cancel": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"监控"},
					"summary":     "取消进行中的工具执行",
					"description": "对当前进程内正在执行的 MCP 工具调用发送 context 取消信号；上层对话/多步任务可继续。若执行已结束或未在本进程内运行则返回 404。",
					"operationId": "cancelExecution",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "执行ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"requestBody": map[string]interface{}{
						"required": false,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"note": map[string]interface{}{
											"type":        "string",
											"description": "可选。非空时与工具已返回输出合并交给大模型，并带有「用户终止说明」标题块以便与命令行原文区分",
										},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "已发送终止信号",
						},
						"400": map[string]interface{}{
							"description": "请求体不是合法 JSON",
						},
						"404": map[string]interface{}{
							"description": "未找到进行中的工具执行",
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
			},
			"/api/monitor/executions": map[string]interface{}{
				"delete": map[string]interface{}{
					"tags":        []string{"监控"},
					"summary":     "批量删除执行记录",
					"description": "批量删除执行记录",
					"operationId": "deleteExecutions",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "删除成功",
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
			},
			"/api/monitor/stats": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"监控"},
					"summary":     "获取统计信息",
					"description": "获取工具执行统计信息",
					"operationId": "getStats",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "获取成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type":        "object",
										"description": "统计信息",
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
			},
			"/api/config": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"配置管理"},
					"summary":     "获取配置",
					"description": "获取系统配置信息",
					"operationId": "getConfig",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "获取成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/ConfigResponse",
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
				"put": map[string]interface{}{
					"tags":        []string{"配置管理"},
					"summary":     "更新配置",
					"description": "更新系统配置",
					"operationId": "updateConfig",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/UpdateConfigRequest",
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "更新成功",
						},
						"400": map[string]interface{}{
							"description": "请求参数错误",
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
			},
			"/api/config/tools": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"配置管理"},
					"summary":     "获取工具配置",
					"description": "获取所有工具的配置信息",
					"operationId": "getTools",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "获取成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type":        "array",
										"description": "工具配置列表",
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
			},
			"/api/config/apply": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"配置管理"},
					"summary":     "应用配置",
					"description": "应用配置更改",
					"operationId": "applyConfig",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "应用成功",
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
			},
			"/api/external-mcp": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"外部MCP管理"},
					"summary":     "列出外部MCP",
					"description": "获取所有外部MCP配置和状态",
					"operationId": "getExternalMCPs",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "获取成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"servers": map[string]interface{}{
												"type":        "object",
												"description": "MCP服务器配置",
												"additionalProperties": map[string]interface{}{
													"$ref": "#/components/schemas/ExternalMCPResponse",
												},
											},
											"stats": map[string]interface{}{
												"type":        "object",
												"description": "统计信息",
											},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
			},
			"/api/external-mcp/stats": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"外部MCP管理"},
					"summary":     "获取外部MCP统计",
					"description": "获取外部MCP统计信息",
					"operationId": "getExternalMCPStats",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "获取成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type":        "object",
										"description": "统计信息",
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
			},
			"/api/external-mcp/{name}": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"外部MCP管理"},
					"summary":     "获取外部MCP",
					"description": "获取指定外部MCP的配置和状态",
					"operationId": "getExternalMCP",
					"parameters": []map[string]interface{}{
						{
							"name":        "name",
							"in":          "path",
							"required":    true,
							"description": "MCP名称",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "获取成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/ExternalMCPResponse",
									},
								},
							},
						},
						"404": map[string]interface{}{
							"description": "MCP不存在",
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
				"put": map[string]interface{}{
					"tags":        []string{"外部MCP管理"},
					"summary":     "添加或更新外部MCP",
					"description": "添加新的外部MCP配置或更新现有配置。\n**传输方式**：\n支持两种传输方式：\n**1. stdio（标准输入输出）**：\n```json\n{\n  \"config\": {\n    \"enabled\": true,\n    \"command\": \"node\",\n    \"args\": [\"/path/to/mcp-server.js\"],\n    \"env\": {}\n  }\n}\n```\n**2. sse（Server-Sent Events）**：\n```json\n{\n  \"config\": {\n    \"enabled\": true,\n    \"transport\": \"sse\",\n    \"url\": \"http://127.0.0.1:8082/sse\",\n    \"timeout\": 30\n  }\n}\n```\n**配置参数说明**：\n- `enabled`: 是否启用（boolean，必需）\n- `command`: 命令（stdio模式必需，如：\"node\", \"python\"）\n- `args`: 命令参数数组（stdio模式必需）\n- `env`: 环境变量（object，可选）\n- `transport`: 传输方式（\"stdio\" 或 \"sse\"，sse模式必需）\n- `url`: SSE端点URL（sse模式必需）\n- `timeout`: 超时时间（秒，可选，默认30）\n- `description`: 描述（可选）",
					"operationId": "addOrUpdateExternalMCP",
					"parameters": []map[string]interface{}{
						{
							"name":        "name",
							"in":          "path",
							"required":    true,
							"description": "MCP名称（唯一标识符）",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/AddOrUpdateExternalMCPRequest",
								},
								"examples": map[string]interface{}{
									"stdio": map[string]interface{}{
										"summary":     "stdio模式配置",
										"description": "使用标准输入输出方式连接外部MCP服务器",
										"value": map[string]interface{}{
											"config": map[string]interface{}{
												"enabled":     true,
												"command":     "node",
												"args":        []string{"/path/to/mcp-server.js"},
												"env":         map[string]interface{}{},
												"timeout":     30,
												"description": "Node.js MCP服务器",
											},
										},
									},
									"sse": map[string]interface{}{
										"summary":     "SSE模式配置",
										"description": "使用Server-Sent Events方式连接外部MCP服务器",
										"value": map[string]interface{}{
											"config": map[string]interface{}{
												"enabled":     true,
												"transport":   "sse",
												"url":         "http://127.0.0.1:8082/sse",
												"timeout":     30,
												"description": "SSE MCP服务器",
											},
										},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "操作成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"message": map[string]interface{}{
												"type":    "string",
												"example": "外部MCP配置已保存",
											},
										},
									},
								},
							},
						},
						"400": map[string]interface{}{
							"description": "请求参数错误（如配置格式不正确、缺少必需字段等）",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/Error",
									},
									"example": map[string]interface{}{
										"error": "stdio模式需要提供command和args参数",
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
				"delete": map[string]interface{}{
					"tags":        []string{"外部MCP管理"},
					"summary":     "删除外部MCP",
					"description": "删除指定的外部MCP配置",
					"operationId": "deleteExternalMCP",
					"parameters": []map[string]interface{}{
						{
							"name":        "name",
							"in":          "path",
							"required":    true,
							"description": "MCP名称",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "删除成功",
						},
						"404": map[string]interface{}{
							"description": "MCP不存在",
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
			},
			"/api/external-mcp/{name}/start": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"外部MCP管理"},
					"summary":     "启动外部MCP",
					"description": "启动指定的外部MCP服务器",
					"operationId": "startExternalMCP",
					"parameters": []map[string]interface{}{
						{
							"name":        "name",
							"in":          "path",
							"required":    true,
							"description": "MCP名称",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "启动成功",
						},
						"404": map[string]interface{}{
							"description": "MCP不存在",
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
			},
			"/api/external-mcp/{name}/stop": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"外部MCP管理"},
					"summary":     "停止外部MCP",
					"description": "停止指定的外部MCP服务器",
					"operationId": "stopExternalMCP",
					"parameters": []map[string]interface{}{
						{
							"name":        "name",
							"in":          "path",
							"required":    true,
							"description": "MCP名称",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "停止成功",
						},
						"404": map[string]interface{}{
							"description": "MCP不存在",
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
			},
			"/api/attack-chain/{conversationId}": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"攻击链"},
					"summary":     "获取攻击链",
					"description": "获取指定对话的攻击链可视化数据",
					"operationId": "getAttackChain",
					"parameters": []map[string]interface{}{
						{
							"name":        "conversationId",
							"in":          "path",
							"required":    true,
							"description": "对话ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "获取成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/AttackChain",
									},
								},
							},
						},
						"404": map[string]interface{}{
							"description": "对话不存在",
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
			},
			"/api/attack-chain/{conversationId}/regenerate": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"攻击链"},
					"summary":     "重新生成攻击链",
					"description": "重新生成指定对话的攻击链可视化数据",
					"operationId": "regenerateAttackChain",
					"parameters": []map[string]interface{}{
						{
							"name":        "conversationId",
							"in":          "path",
							"required":    true,
							"description": "对话ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "重新生成成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/AttackChain",
									},
								},
							},
						},
						"404": map[string]interface{}{
							"description": "对话不存在",
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
			},
			"/api/conversations/{id}/pinned": map[string]interface{}{
				"put": map[string]interface{}{
					"tags":        []string{"对话管理"},
					"summary":     "设置对话置顶",
					"description": "设置或取消对话的置顶状态",
					"operationId": "updateConversationPinned",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "对话ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"pinned"},
									"properties": map[string]interface{}{
										"pinned": map[string]interface{}{
											"type":        "boolean",
											"description": "是否置顶",
										},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "更新成功",
						},
						"404": map[string]interface{}{
							"description": "对话不存在",
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
			},
			"/api/knowledge/categories": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"知识库"},
					"summary":     "获取分类",
					"description": "获取知识库的所有分类",
					"operationId": "getKnowledgeCategories",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "获取成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"categories": map[string]interface{}{
												"type":        "array",
												"description": "分类列表",
												"items": map[string]interface{}{
													"type": "string",
												},
											},
											"enabled": map[string]interface{}{
												"type":        "boolean",
												"description": "知识库是否启用",
											},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
			},
			"/api/knowledge/items": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"知识库"},
					"summary":     "列出知识项",
					"description": "获取知识库中的所有知识项",
					"operationId": "getKnowledgeItems",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "获取成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"items": map[string]interface{}{
												"type":        "array",
												"description": "知识项列表",
											},
											"enabled": map[string]interface{}{
												"type":        "boolean",
												"description": "知识库是否启用",
											},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
				"post": map[string]interface{}{
					"tags":        []string{"知识库"},
					"summary":     "创建知识项",
					"description": "创建新的知识项",
					"operationId": "createKnowledgeItem",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":        "object",
									"description": "知识项数据",
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "创建成功",
						},
						"400": map[string]interface{}{
							"description": "请求参数错误",
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
			},
			"/api/knowledge/items/{id}": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"知识库"},
					"summary":     "获取知识项",
					"description": "获取指定知识项的详细信息",
					"operationId": "getKnowledgeItem",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "知识项ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "获取成功",
						},
						"404": map[string]interface{}{
							"description": "知识项不存在",
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
				"put": map[string]interface{}{
					"tags":        []string{"知识库"},
					"summary":     "更新知识项",
					"description": "更新指定知识项",
					"operationId": "updateKnowledgeItem",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "知识项ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":        "object",
									"description": "知识项数据",
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "更新成功",
						},
						"404": map[string]interface{}{
							"description": "知识项不存在",
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
				"delete": map[string]interface{}{
					"tags":        []string{"知识库"},
					"summary":     "删除知识项",
					"description": "删除指定知识项",
					"operationId": "deleteKnowledgeItem",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "知识项ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "删除成功",
						},
						"404": map[string]interface{}{
							"description": "知识项不存在",
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
			},
			"/api/knowledge/index-status": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"知识库"},
					"summary":     "获取索引状态",
					"description": "获取知识库索引的构建状态",
					"operationId": "getIndexStatus",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "获取成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"enabled": map[string]interface{}{
												"type":        "boolean",
												"description": "知识库是否启用",
											},
											"total_items": map[string]interface{}{
												"type":        "integer",
												"description": "总知识项数",
											},
											"indexed_items": map[string]interface{}{
												"type":        "integer",
												"description": "已索引知识项数",
											},
											"progress_percent": map[string]interface{}{
												"type":        "number",
												"description": "索引进度百分比",
											},
											"is_complete": map[string]interface{}{
												"type":        "boolean",
												"description": "索引是否完成",
											},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
			},
			"/api/knowledge/index": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"知识库"},
					"summary":     "构建索引",
					"description": "构建知识库向量索引。默认仅处理尚无向量的知识项；mode=full 时全量重建。",
					"operationId": "startKnowledgeIndex",
					"parameters": []map[string]interface{}{
						{
							"name":        "mode",
							"in":          "query",
							"required":    false,
							"description": "索引模式：missing（默认，补齐缺失向量）或 full（全量重建）",
							"schema": map[string]interface{}{
								"type":    "string",
								"enum":    []string{"missing", "full"},
								"default": "missing",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "索引任务已启动",
						},
						"400": map[string]interface{}{
							"description": "无效的 mode 参数",
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
						"409": map[string]interface{}{
							"description": "已有索引任务正在进行",
						},
					},
				},
			},
			"/api/knowledge/scan": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"知识库"},
					"summary":     "扫描知识库",
					"description": "扫描知识库目录，导入新的知识文件",
					"operationId": "scanKnowledgeBase",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "扫描任务已启动",
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
			},
			"/api/knowledge/search": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"知识库"},
					"summary":     "搜索知识库",
					"description": "在知识库中搜索相关内容。基于向量检索，按查询与知识片段的语义相似度（余弦）返回最相关结果。\n**搜索说明**：\n- 语义相似度搜索：嵌入向量 + 余弦相似度，可配置相似度阈值与 TopK\n- 可按风险类型等元数据过滤（如：SQL注入、XSS、文件上传等）\n- 建议先调用 `/api/knowledge/categories` 获取可用的风险类型列表\n**使用示例**：\n```json\n{\n  \"query\": \"SQL注入漏洞的检测方法\",\n  \"riskType\": \"SQL注入\",\n  \"topK\": 5,\n  \"threshold\": 0.7\n}\n```",
					"operationId": "searchKnowledge",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"query"},
									"properties": map[string]interface{}{
										"query": map[string]interface{}{
											"type":        "string",
											"description": "搜索查询内容，描述你想要了解的安全知识主题（必需）",
											"example":     "SQL注入漏洞的检测方法",
										},
										"riskType": map[string]interface{}{
											"type":        "string",
											"description": "可选：指定风险类型（如：SQL注入、XSS、文件上传等）。建议先调用 `/api/knowledge/categories` 获取可用的风险类型列表，然后使用正确的风险类型进行精确搜索，这样可以大幅减少检索时间。如果不指定则搜索所有类型。",
											"example":     "SQL注入",
										},
										"topK": map[string]interface{}{
											"type":        "integer",
											"description": "可选：返回Top-K结果数量，默认5",
											"default":     5,
											"minimum":     1,
											"maximum":     50,
											"example":     5,
										},
										"threshold": map[string]interface{}{
											"type":        "number",
											"format":      "float",
											"description": "可选：相似度阈值（0-1之间），默认0.7。只有相似度大于等于此值的结果才会返回",
											"default":     0.7,
											"minimum":     0,
											"maximum":     1,
											"example":     0.7,
										},
									},
								},
								"examples": map[string]interface{}{
									"basic": map[string]interface{}{
										"summary":     "基础搜索",
										"description": "最简单的搜索，只提供查询内容",
										"value": map[string]interface{}{
											"query": "SQL注入漏洞的检测方法",
										},
									},
									"withRiskType": map[string]interface{}{
										"summary":     "按风险类型搜索",
										"description": "指定风险类型进行精确搜索",
										"value": map[string]interface{}{
											"query":     "SQL注入漏洞的检测方法",
											"riskType":  "SQL注入",
											"topK":      5,
											"threshold": 0.7,
										},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "搜索成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"results": map[string]interface{}{
												"type":        "array",
												"description": "搜索结果列表，每个结果包含：item（知识项信息）、chunks（匹配的知识片段）、score（相似度分数）",
												"items": map[string]interface{}{
													"type": "object",
													"properties": map[string]interface{}{
														"item": map[string]interface{}{
															"type":        "object",
															"description": "知识项信息",
														},
														"chunks": map[string]interface{}{
															"type":        "array",
															"description": "匹配的知识片段列表",
														},
														"score": map[string]interface{}{
															"type":        "number",
															"description": "相似度分数（0-1之间）",
														},
													},
												},
											},
											"enabled": map[string]interface{}{
												"type":        "boolean",
												"description": "知识库是否启用",
											},
										},
									},
									"example": map[string]interface{}{
										"results": []map[string]interface{}{
											{
												"item": map[string]interface{}{
													"id":       "item-1",
													"title":    "SQL注入漏洞检测",
													"category": "SQL注入",
												},
												"chunks": []map[string]interface{}{
													{
														"text": "SQL注入漏洞的检测方法包括...",
													},
												},
												"score": 0.85,
											},
										},
										"enabled": true,
									},
								},
							},
						},
						"400": map[string]interface{}{
							"description": "请求参数错误（如query为空）",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/Error",
									},
									"example": map[string]interface{}{
										"error": "查询不能为空",
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
						"500": map[string]interface{}{
							"description": "服务器内部错误（如知识库未启用或检索失败）",
						},
					},
				},
			},
			"/api/knowledge/retrieval-logs": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"知识库"},
					"summary":     "获取检索日志",
					"description": "获取知识库检索日志",
					"operationId": "getRetrievalLogs",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "获取成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"logs": map[string]interface{}{
												"type":        "array",
												"description": "检索日志列表",
											},
											"enabled": map[string]interface{}{
												"type":        "boolean",
												"description": "知识库是否启用",
											},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
			},
			"/api/knowledge/retrieval-logs/{id}": map[string]interface{}{
				"delete": map[string]interface{}{
					"tags":        []string{"知识库"},
					"summary":     "删除检索日志",
					"description": "删除指定的检索日志",
					"operationId": "deleteRetrievalLog",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "日志ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "删除成功",
						},
						"404": map[string]interface{}{
							"description": "日志不存在",
						},
						"401": map[string]interface{}{
							"description": "未授权",
						},
					},
				},
			},
			// ==================== 对话交互 - 缺失端点 ====================
			"/api/conversations/{id}/delete-turn": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"对话交互"},
					"summary":     "删除对话轮次",
					"description": "删除指定消息所在的对话轮次（从该轮 user 消息到下一轮 user 消息之前的所有消息），并清空 last_react 状态。",
					"operationId": "deleteConversationTurn",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "对话ID",
							"schema":      map[string]interface{}{"type": "string"},
						},
					},
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"messageId"},
									"properties": map[string]interface{}{
										"messageId": map[string]interface{}{
											"type":        "string",
											"description": "锚点消息ID，标识要删除的轮次",
										},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "删除成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"deletedMessageIds": map[string]interface{}{
												"type":        "array",
												"items":       map[string]interface{}{"type": "string"},
												"description": "被删除的消息ID列表",
											},
											"message": map[string]interface{}{
												"type":    "string",
												"example": "ok",
											},
										},
									},
								},
							},
						},
						"400": map[string]interface{}{"description": "参数错误或删除失败"},
						"401": map[string]interface{}{"description": "未授权"},
						"404": map[string]interface{}{"description": "对话不存在"},
					},
				},
			},
			"/api/messages/{id}/process-details": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"对话交互"},
					"summary":     "获取消息过程详情",
					"description": "按需分页加载指定消息的执行过程详情，包括工具调用、思考过程等事件。默认返回 50 条；导出或旧集成需要全量时可显式传 full=1。",
					"operationId": "getMessageProcessDetails",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "消息ID",
							"schema":      map[string]interface{}{"type": "string"},
						},
						{
							"name":        "summary",
							"in":          "query",
							"required":    false,
							"description": "仅返回过程详情摘要（total / iterationCount / maxIteration）",
							"schema":      map[string]interface{}{"type": "boolean"},
						},
						{
							"name":        "limit",
							"in":          "query",
							"required":    false,
							"description": "分页大小，默认 50，最大 500",
							"schema":      map[string]interface{}{"type": "integer", "default": 50, "maximum": 500},
						},
						{
							"name":        "offset",
							"in":          "query",
							"required":    false,
							"description": "分页偏移量，默认 0",
							"schema":      map[string]interface{}{"type": "integer", "default": 0},
						},
						{
							"name":        "full",
							"in":          "query",
							"required":    false,
							"description": "显式返回全量过程详情；仅建议导出/兼容旧集成使用",
							"schema":      map[string]interface{}{"type": "boolean"},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "获取成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"processDetails": map[string]interface{}{
												"type": "array",
												"items": map[string]interface{}{
													"type": "object",
													"properties": map[string]interface{}{
														"id":             map[string]interface{}{"type": "string", "description": "详情记录ID"},
														"messageId":      map[string]interface{}{"type": "string", "description": "所属消息ID"},
														"conversationId": map[string]interface{}{"type": "string", "description": "所属对话ID"},
														"eventType":      map[string]interface{}{"type": "string", "description": "事件类型（如tool_call, thinking等）"},
														"message":        map[string]interface{}{"type": "string", "description": "事件消息"},
														"data":           map[string]interface{}{"description": "事件附加数据（JSON对象）"},
														"createdAt":      map[string]interface{}{"type": "string", "format": "date-time", "description": "创建时间"},
													},
												},
											},
											"total":   map[string]interface{}{"type": "integer", "description": "过程详情总数"},
											"offset":  map[string]interface{}{"type": "integer", "description": "当前分页偏移量"},
											"limit":   map[string]interface{}{"type": "integer", "description": "当前分页大小"},
											"hasMore": map[string]interface{}{"type": "boolean", "description": "是否还有更多过程详情"},
											"summary": map[string]interface{}{
												"type":        "object",
												"description": "summary=1 时返回的摘要",
											},
										},
									},
								},
							},
						},
						"400": map[string]interface{}{"description": "参数错误"},
						"401": map[string]interface{}{"description": "未授权"},
					},
				},
			},

			// ==================== 批量任务 - 缺失端点 ====================
			"/api/batch-tasks/{queueId}/rerun": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"批量任务"},
					"summary":     "重跑批量任务队列",
					"description": "重置已完成或已取消的批量任务队列，重新开始执行所有任务。",
					"operationId": "rerunBatchQueue",
					"parameters": []map[string]interface{}{
						{
							"name":        "queueId",
							"in":          "path",
							"required":    true,
							"description": "队列ID",
							"schema":      map[string]interface{}{"type": "string"},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "重跑成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"message": map[string]interface{}{"type": "string", "example": "批量任务已重新开始执行"},
											"queueId": map[string]interface{}{"type": "string", "description": "队列ID"},
										},
									},
								},
							},
						},
						"400": map[string]interface{}{"description": "仅已完成或已取消的队列可以重跑"},
						"401": map[string]interface{}{"description": "未授权"},
						"404": map[string]interface{}{"description": "队列不存在"},
					},
				},
			},
			"/api/batch-tasks/{queueId}/metadata": map[string]interface{}{
				"put": map[string]interface{}{
					"tags":        []string{"批量任务"},
					"summary":     "修改队列元数据",
					"description": "修改批量任务队列的标题、角色和代理模式。",
					"operationId": "updateBatchQueueMetadata",
					"parameters": []map[string]interface{}{
						{
							"name":        "queueId",
							"in":          "path",
							"required":    true,
							"description": "队列ID",
							"schema":      map[string]interface{}{"type": "string"},
						},
					},
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"title":     map[string]interface{}{"type": "string", "description": "队列标题"},
										"role":      map[string]interface{}{"type": "string", "description": "使用的角色名称"},
										"agentMode": map[string]interface{}{"type": "string", "description": "代理模式", "enum": []string{"eino_single", "deep", "plan_execute", "supervisor"}},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "更新成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"queue": map[string]interface{}{"$ref": "#/components/schemas/BatchQueue"},
										},
									},
								},
							},
						},
						"400": map[string]interface{}{"description": "参数错误"},
						"401": map[string]interface{}{"description": "未授权"},
					},
				},
			},
			"/api/batch-tasks/{queueId}/schedule": map[string]interface{}{
				"put": map[string]interface{}{
					"tags":        []string{"批量任务"},
					"summary":     "修改队列调度配置",
					"description": "修改批量任务队列的调度模式和Cron表达式。队列运行中无法修改。",
					"operationId": "updateBatchQueueSchedule",
					"parameters": []map[string]interface{}{
						{
							"name":        "queueId",
							"in":          "path",
							"required":    true,
							"description": "队列ID",
							"schema":      map[string]interface{}{"type": "string"},
						},
					},
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"scheduleMode": map[string]interface{}{"type": "string", "description": "调度模式", "enum": []string{"manual", "cron"}},
										"cronExpr":     map[string]interface{}{"type": "string", "description": "Cron表达式（scheduleMode为cron时必填）", "example": "0 2 * * *"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "更新成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"queue": map[string]interface{}{"$ref": "#/components/schemas/BatchQueue"},
										},
									},
								},
							},
						},
						"400": map[string]interface{}{"description": "参数错误或队列正在运行中"},
						"401": map[string]interface{}{"description": "未授权"},
						"404": map[string]interface{}{"description": "队列不存在"},
					},
				},
			},
			"/api/batch-tasks/{queueId}/schedule-enabled": map[string]interface{}{
				"put": map[string]interface{}{
					"tags":        []string{"批量任务"},
					"summary":     "开关Cron自动调度",
					"description": "开启或关闭批量任务队列的Cron自动调度功能，手工执行不受影响。",
					"operationId": "setBatchQueueScheduleEnabled",
					"parameters": []map[string]interface{}{
						{
							"name":        "queueId",
							"in":          "path",
							"required":    true,
							"description": "队列ID",
							"schema":      map[string]interface{}{"type": "string"},
						},
					},
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"scheduleEnabled"},
									"properties": map[string]interface{}{
										"scheduleEnabled": map[string]interface{}{"type": "boolean", "description": "是否启用自动调度"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "设置成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"queue": map[string]interface{}{"$ref": "#/components/schemas/BatchQueue"},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "未授权"},
						"404": map[string]interface{}{"description": "队列不存在"},
					},
				},
			},
			// ==================== FOFA信息收集 ====================
			"/api/fofa/search": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"FOFA信息收集"},
					"summary":     "FOFA搜索",
					"description": "通过后端代理执行FOFA搜索查询，返回资产信息。",
					"operationId": "fofaSearch",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"query"},
									"properties": map[string]interface{}{
										"query":  map[string]interface{}{"type": "string", "description": "FOFA查询语法", "example": "domain=\"example.com\""},
										"size":   map[string]interface{}{"type": "integer", "description": "返回数量（默认100，最大10000）", "default": 100},
										"page":   map[string]interface{}{"type": "integer", "description": "页码（默认1）", "default": 1},
										"fields": map[string]interface{}{"type": "string", "description": "返回字段，逗号分隔", "example": "host,ip,port,title"},
										"full":   map[string]interface{}{"type": "boolean", "description": "是否查询全部数据", "default": false},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "搜索成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"query":         map[string]interface{}{"type": "string", "description": "实际执行的查询"},
											"size":          map[string]interface{}{"type": "integer"},
											"page":          map[string]interface{}{"type": "integer"},
											"total":         map[string]interface{}{"type": "integer", "description": "总匹配数"},
											"fields":        map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
											"results_count": map[string]interface{}{"type": "integer"},
											"results":       map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "object"}, "description": "搜索结果列表"},
										},
									},
								},
							},
						},
						"400": map[string]interface{}{"description": "参数错误"},
						"401": map[string]interface{}{"description": "未授权"},
					},
				},
			},
			"/api/fofa/parse": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"FOFA信息收集"},
					"summary":     "自然语言解析为FOFA语法",
					"description": "使用AI将自然语言描述解析为FOFA查询语法，需人工确认后再执行查询。",
					"operationId": "fofaParse",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"text"},
									"properties": map[string]interface{}{
										"text": map[string]interface{}{"type": "string", "description": "自然语言描述", "example": "查找使用WordPress的网站"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "解析成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"query":       map[string]interface{}{"type": "string", "description": "生成的FOFA查询语法"},
											"explanation": map[string]interface{}{"type": "string", "description": "语法解释"},
											"warnings":    map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "潜在风险或歧义提示"},
										},
									},
								},
							},
						},
						"400": map[string]interface{}{"description": "参数错误"},
						"401": map[string]interface{}{"description": "未授权"},
					},
				},
			},

			// ==================== 配置管理 - 缺失端点 ====================
			"/api/config/test-vision": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"配置管理"},
					"summary":     "测试视觉模型连接",
					"description": "测试 Vision 模型 API 是否可用。vision.api_key/base_url 留空时可传 openai 段作回退。",
					"operationId": "testVision",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"vision"},
									"properties": map[string]interface{}{
										"vision": map[string]interface{}{"$ref": "#/components/schemas/VisionConfig"},
										"openai": map[string]interface{}{
											"type":        "object",
											"description": "主 LLM 配置（vision 字段留空时用于 API Key/Base URL 回退）",
										},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "测试结果",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"success":    map[string]interface{}{"type": "boolean"},
											"error":      map[string]interface{}{"type": "string"},
											"model":      map[string]interface{}{"type": "string"},
											"latency_ms": map[string]interface{}{"type": "number"},
										},
									},
								},
							},
						},
						"400": map[string]interface{}{"description": "参数错误"},
						"401": map[string]interface{}{"description": "未授权"},
					},
				},
			},
			"/api/config/test-openai": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"配置管理"},
					"summary":     "测试OpenAI API连接",
					"description": "测试指定的OpenAI/Claude API配置是否可用，发送一个最小请求验证连通性。",
					"operationId": "testOpenAI",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"api_key", "model"},
									"properties": map[string]interface{}{
										"provider": map[string]interface{}{"type": "string", "description": "LLM提供商（openai/claude/orcarouter）", "example": "openai"},
										"base_url": map[string]interface{}{"type": "string", "description": "API基地址（可选，默认根据provider自动选择）"},
										"api_key":  map[string]interface{}{"type": "string", "description": "API密钥"},
										"model":    map[string]interface{}{"type": "string", "description": "模型名称", "example": "gpt-4"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "测试结果",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"success":    map[string]interface{}{"type": "boolean", "description": "是否连接成功"},
											"error":      map[string]interface{}{"type": "string", "description": "失败原因（success=false时）"},
											"model":      map[string]interface{}{"type": "string", "description": "实际使用的模型（success=true时）"},
											"latency_ms": map[string]interface{}{"type": "number", "description": "延迟毫秒数（success=true时）"},
										},
									},
								},
							},
						},
						"400": map[string]interface{}{"description": "参数错误"},
						"401": map[string]interface{}{"description": "未授权"},
					},
				},
			},
			"/api/config/list-models": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"配置管理"},
					"summary":     "获取模型列表",
					"description": "代理调用 OpenAI 兼容 GET /models，返回可用模型 id 列表。Claude 不支持。",
					"operationId": "listModels",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"api_key"},
									"properties": map[string]interface{}{
										"provider": map[string]interface{}{"type": "string", "description": "LLM提供商（openai/claude/orcarouter）", "example": "openai"},
										"base_url": map[string]interface{}{"type": "string", "description": "API基地址（可选）"},
										"api_key":  map[string]interface{}{"type": "string", "description": "API密钥"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "获取结果",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"success":   map[string]interface{}{"type": "boolean"},
											"supported": map[string]interface{}{"type": "boolean"},
											"error":     map[string]interface{}{"type": "string"},
											"models":    map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
											"count":     map[string]interface{}{"type": "integer"},
										},
									},
								},
							},
						},
						"400": map[string]interface{}{"description": "参数错误"},
						"401": map[string]interface{}{"description": "未授权"},
					},
				},
			},

			// ==================== 终端 ====================
			"/api/terminal/run": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"终端"},
					"summary":     "执行终端命令",
					"description": "在服务器上执行Shell命令并返回结果。",
					"operationId": "terminalRun",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"command"},
									"properties": map[string]interface{}{
										"command": map[string]interface{}{"type": "string", "description": "要执行的命令"},
										"shell":   map[string]interface{}{"type": "string", "description": "Shell类型（默认sh/cmd）"},
										"cwd":     map[string]interface{}{"type": "string", "description": "工作目录（可选）"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "执行完成",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"stdout":    map[string]interface{}{"type": "string", "description": "标准输出"},
											"stderr":    map[string]interface{}{"type": "string", "description": "标准错误"},
											"exit_code": map[string]interface{}{"type": "integer", "description": "退出码"},
											"error":     map[string]interface{}{"type": "string", "description": "执行错误（可选）"},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "未授权"},
					},
				},
			},
			"/api/terminal/run/stream": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"终端"},
					"summary":     "流式执行终端命令",
					"description": "以SSE流式方式执行Shell命令，实时返回输出。每个事件包含 JSON: {\"t\": \"out\"|\"err\"|\"exit\", \"d\": \"数据\", \"c\": 退出码}",
					"operationId": "terminalRunStream",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"command"},
									"properties": map[string]interface{}{
										"command": map[string]interface{}{"type": "string", "description": "要执行的命令"},
										"shell":   map[string]interface{}{"type": "string", "description": "Shell类型（默认sh/cmd）"},
										"cwd":     map[string]interface{}{"type": "string", "description": "工作目录（可选）"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "SSE事件流",
							"content": map[string]interface{}{
								"text/event-stream": map[string]interface{}{
									"schema": map[string]interface{}{
										"type":        "string",
										"description": "Server-Sent Events流，每个事件为JSON: {\"t\":\"out|err|exit\",\"d\":\"data\",\"c\":exitCode}",
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "未授权"},
					},
				},
			},
			"/api/terminal/ws": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"终端"},
					"summary":     "WebSocket终端",
					"description": "通过WebSocket建立交互式终端连接，支持PTY。客户端发送文本/二进制数据作为命令输入，也可发送JSON: {\"type\":\"resize\",\"cols\":80,\"rows\":24} 调整终端大小。服务端返回二进制PTY输出。",
					"operationId": "terminalWS",
					"responses": map[string]interface{}{
						"101": map[string]interface{}{"description": "WebSocket连接已建立"},
						"401": map[string]interface{}{"description": "未授权"},
					},
				},
			},

			// ==================== WebShell管理 ====================
			"/api/webshell/connections": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"WebShell管理"},
					"summary":     "列出WebShell连接",
					"description": "获取所有已保存的WebShell连接配置列表。",
					"operationId": "listWebshellConnections",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "获取成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "array",
										"items": map[string]interface{}{
											"type": "object",
											"properties": map[string]interface{}{
												"id":         map[string]interface{}{"type": "string", "description": "连接ID"},
												"url":        map[string]interface{}{"type": "string", "description": "WebShell URL"},
												"password":   map[string]interface{}{"type": "string", "description": "连接密码"},
												"type":       map[string]interface{}{"type": "string", "description": "Shell类型", "enum": []string{"php", "asp", "aspx", "jsp", "custom"}},
												"method":     map[string]interface{}{"type": "string", "description": "请求方法", "enum": []string{"get", "post"}},
												"cmd_param":  map[string]interface{}{"type": "string", "description": "命令参数名"},
												"remark":     map[string]interface{}{"type": "string", "description": "备注"},
												"created_at": map[string]interface{}{"type": "string", "format": "date-time"},
											},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "未授权"},
					},
				},
				"post": map[string]interface{}{
					"tags":        []string{"WebShell管理"},
					"summary":     "创建WebShell连接",
					"description": "保存一个新的WebShell连接配置。",
					"operationId": "createWebshellConnection",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"url"},
									"properties": map[string]interface{}{
										"url":       map[string]interface{}{"type": "string", "description": "WebShell URL"},
										"password":  map[string]interface{}{"type": "string", "description": "连接密码"},
										"type":      map[string]interface{}{"type": "string", "description": "Shell类型", "enum": []string{"php", "asp", "aspx", "jsp", "custom"}},
										"method":    map[string]interface{}{"type": "string", "description": "请求方法", "enum": []string{"get", "post"}},
										"cmd_param": map[string]interface{}{"type": "string", "description": "命令参数名"},
										"remark":    map[string]interface{}{"type": "string", "description": "备注"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "创建成功"},
						"400": map[string]interface{}{"description": "参数错误"},
						"401": map[string]interface{}{"description": "未授权"},
					},
				},
			},
			"/api/webshell/connections/{id}": map[string]interface{}{
				"put": map[string]interface{}{
					"tags":        []string{"WebShell管理"},
					"summary":     "更新WebShell连接",
					"description": "更新已有的WebShell连接配置。",
					"operationId": "updateWebshellConnection",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "description": "连接ID", "schema": map[string]interface{}{"type": "string"}},
					},
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"url":       map[string]interface{}{"type": "string"},
										"password":  map[string]interface{}{"type": "string"},
										"type":      map[string]interface{}{"type": "string", "enum": []string{"php", "asp", "aspx", "jsp", "custom"}},
										"method":    map[string]interface{}{"type": "string", "enum": []string{"get", "post"}},
										"cmd_param": map[string]interface{}{"type": "string"},
										"remark":    map[string]interface{}{"type": "string"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "更新成功"},
						"401": map[string]interface{}{"description": "未授权"},
						"404": map[string]interface{}{"description": "连接不存在"},
					},
				},
				"delete": map[string]interface{}{
					"tags":        []string{"WebShell管理"},
					"summary":     "删除WebShell连接",
					"description": "删除指定的WebShell连接配置。",
					"operationId": "deleteWebshellConnection",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "description": "连接ID", "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "删除成功"},
						"401": map[string]interface{}{"description": "未授权"},
						"404": map[string]interface{}{"description": "连接不存在"},
					},
				},
			},
			"/api/webshell/connections/{id}/state": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"WebShell管理"},
					"summary":     "获取连接状态",
					"description": "获取WebShell连接的保存状态数据。",
					"operationId": "getWebshellConnectionState",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "description": "连接ID", "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "获取成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"state": map[string]interface{}{"type": "object", "description": "状态数据（任意JSON）"},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "未授权"},
					},
				},
				"put": map[string]interface{}{
					"tags":        []string{"WebShell管理"},
					"summary":     "保存连接状态",
					"description": "保存WebShell连接的状态数据。",
					"operationId": "saveWebshellConnectionState",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "description": "连接ID", "schema": map[string]interface{}{"type": "string"}},
					},
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"state": map[string]interface{}{"type": "object", "description": "状态数据（任意JSON）"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "保存成功"},
						"401": map[string]interface{}{"description": "未授权"},
					},
				},
			},
			"/api/webshell/connections/{id}/ai-history": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"WebShell管理"},
					"summary":     "获取AI对话历史",
					"description": "获取指定WebShell连接的AI辅助对话历史消息。",
					"operationId": "getWebshellAIHistory",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "description": "连接ID", "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "获取成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"conversationId": map[string]interface{}{"type": "string"},
											"messages": map[string]interface{}{
												"type": "array",
												"items": map[string]interface{}{
													"type": "object",
													"properties": map[string]interface{}{
														"id":        map[string]interface{}{"type": "string"},
														"role":      map[string]interface{}{"type": "string"},
														"content":   map[string]interface{}{"type": "string"},
														"createdAt": map[string]interface{}{"type": "string", "format": "date-time"},
													},
												},
											},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "未授权"},
					},
				},
			},
			"/api/webshell/connections/{id}/ai-conversations": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"WebShell管理"},
					"summary":     "列出AI对话",
					"description": "获取指定WebShell连接的所有AI辅助对话列表。",
					"operationId": "listWebshellAIConversations",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "description": "连接ID", "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "获取成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "array",
										"items": map[string]interface{}{
											"type": "object",
											"properties": map[string]interface{}{
												"id":        map[string]interface{}{"type": "string"},
												"title":     map[string]interface{}{"type": "string"},
												"createdAt": map[string]interface{}{"type": "string", "format": "date-time"},
											},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "未授权"},
					},
				},
			},
			"/api/webshell/exec": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"WebShell管理"},
					"summary":     "执行WebShell命令",
					"description": "通过指定的WebShell连接执行远程命令。",
					"operationId": "webshellExec",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"url", "command"},
									"properties": map[string]interface{}{
										"url":       map[string]interface{}{"type": "string", "description": "WebShell URL"},
										"password":  map[string]interface{}{"type": "string"},
										"type":      map[string]interface{}{"type": "string", "enum": []string{"php", "asp", "aspx", "jsp", "custom"}},
										"method":    map[string]interface{}{"type": "string", "enum": []string{"get", "post"}},
										"cmd_param": map[string]interface{}{"type": "string"},
										"command":   map[string]interface{}{"type": "string", "description": "要执行的命令"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "执行结果",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"ok":        map[string]interface{}{"type": "boolean"},
											"output":    map[string]interface{}{"type": "string", "description": "命令输出"},
											"error":     map[string]interface{}{"type": "string", "description": "错误信息"},
											"http_code": map[string]interface{}{"type": "integer", "description": "HTTP响应码"},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "未授权"},
					},
				},
			},
			"/api/webshell/file": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"WebShell管理"},
					"summary":     "WebShell文件操作",
					"description": "通过WebShell执行远程文件操作（列目录、读写文件、创建目录、重命名、删除、上传等）。",
					"operationId": "webshellFileOp",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"url", "action", "path"},
									"properties": map[string]interface{}{
										"url":         map[string]interface{}{"type": "string", "description": "WebShell URL"},
										"password":    map[string]interface{}{"type": "string"},
										"type":        map[string]interface{}{"type": "string", "enum": []string{"php", "asp", "aspx", "jsp", "custom"}},
										"method":      map[string]interface{}{"type": "string", "enum": []string{"get", "post"}},
										"cmd_param":   map[string]interface{}{"type": "string"},
										"action":      map[string]interface{}{"type": "string", "description": "操作类型", "enum": []string{"list", "read", "delete", "write", "mkdir", "rename", "upload", "upload_chunk"}},
										"path":        map[string]interface{}{"type": "string", "description": "目标文件/目录路径"},
										"target_path": map[string]interface{}{"type": "string", "description": "目标路径（rename时使用）"},
										"content":     map[string]interface{}{"type": "string", "description": "文件内容（write/upload时使用）"},
										"chunk_index": map[string]interface{}{"type": "integer", "description": "分块索引（upload_chunk时使用）"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "操作结果",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"ok":     map[string]interface{}{"type": "boolean"},
											"output": map[string]interface{}{"type": "string"},
											"error":  map[string]interface{}{"type": "string"},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "未授权"},
					},
				},
			},

			// ==================== 对话附件 ====================
			"/api/chat-uploads": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"对话附件"},
					"summary":     "列出附件",
					"description": "获取对话文件列表，包含手动上传附件、工具输出和会话产物，可按会话、项目、来源、文件名搜索和分页过滤。",
					"operationId": "listChatUploads",
					"parameters": []map[string]interface{}{
						{"name": "conversation", "in": "query", "required": false, "description": "按对话ID过滤", "schema": map[string]interface{}{"type": "string"}},
						{"name": "project", "in": "query", "required": false, "description": "按项目ID过滤", "schema": map[string]interface{}{"type": "string"}},
						{"name": "source", "in": "query", "required": false, "description": "按来源过滤：upload/reduction/workspace/conversation_artifact/all", "schema": map[string]interface{}{"type": "string", "enum": []string{"all", "upload", "reduction", "workspace", "conversation_artifact"}}},
						{"name": "search", "in": "query", "required": false, "description": "按文件名或子路径搜索", "schema": map[string]interface{}{"type": "string"}},
						{"name": "page", "in": "query", "required": false, "description": "页码，从1开始", "schema": map[string]interface{}{"type": "integer", "default": 1}},
						{"name": "pageSize", "in": "query", "required": false, "description": "每页数量，传 all 返回全部", "schema": map[string]interface{}{"oneOf": []map[string]interface{}{{"type": "integer"}, {"type": "string", "enum": []string{"all"}}}}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "获取成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"files": map[string]interface{}{
												"type": "array",
												"items": map[string]interface{}{
													"type": "object",
													"properties": map[string]interface{}{
														"relativePath":      map[string]interface{}{"type": "string"},
														"absolutePath":      map[string]interface{}{"type": "string"},
														"name":              map[string]interface{}{"type": "string"},
														"size":              map[string]interface{}{"type": "integer"},
														"modifiedUnix":      map[string]interface{}{"type": "integer"},
														"date":              map[string]interface{}{"type": "string"},
														"conversationId":    map[string]interface{}{"type": "string"},
														"conversationTitle": map[string]interface{}{"type": "string"},
														"projectId":         map[string]interface{}{"type": "string"},
														"projectName":       map[string]interface{}{"type": "string"},
														"subPath":           map[string]interface{}{"type": "string"},
														"source":            map[string]interface{}{"type": "string", "description": "upload/reduction/workspace/conversation_artifact"},
													},
												},
											},
											"folders": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
											"total":   map[string]interface{}{"type": "integer"},
											"page":    map[string]interface{}{"type": "integer"},
											"pageSize": map[string]interface{}{
												"type": "integer",
											},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "未授权"},
					},
				},
				"post": map[string]interface{}{
					"tags":        []string{"对话附件"},
					"summary":     "上传附件",
					"description": "上传文件到对话附件目录（multipart/form-data）。",
					"operationId": "uploadChatFile",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"multipart/form-data": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"file"},
									"properties": map[string]interface{}{
										"file":           map[string]interface{}{"type": "string", "format": "binary", "description": "上传的文件"},
										"conversationId": map[string]interface{}{"type": "string", "description": "关联的对话ID（可选）"},
										"relativeDir":    map[string]interface{}{"type": "string", "description": "目标目录相对路径（可选）"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "上传成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"ok":           map[string]interface{}{"type": "boolean"},
											"relativePath": map[string]interface{}{"type": "string"},
											"absolutePath": map[string]interface{}{"type": "string"},
											"name":         map[string]interface{}{"type": "string"},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "未授权"},
					},
				},
				"delete": map[string]interface{}{
					"tags":        []string{"对话附件"},
					"summary":     "删除附件",
					"description": "删除指定的对话附件文件。",
					"operationId": "deleteChatUpload",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"path"},
									"properties": map[string]interface{}{
										"path": map[string]interface{}{"type": "string", "description": "文件相对路径"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "删除成功"},
						"401": map[string]interface{}{"description": "未授权"},
					},
				},
			},
			"/api/chat-uploads/export": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"对话附件"},
					"summary":     "导出附件",
					"description": "按当前过滤条件导出对话文件 ZIP，包含 manifest.json。",
					"operationId": "exportChatUploads",
					"parameters": []map[string]interface{}{
						{"name": "conversation", "in": "query", "required": false, "description": "按对话ID过滤", "schema": map[string]interface{}{"type": "string"}},
						{"name": "project", "in": "query", "required": false, "description": "按项目ID过滤", "schema": map[string]interface{}{"type": "string"}},
						{"name": "source", "in": "query", "required": false, "description": "按来源过滤：upload/reduction/workspace/conversation_artifact/all", "schema": map[string]interface{}{"type": "string", "enum": []string{"all", "upload", "reduction", "workspace", "conversation_artifact"}}},
						{"name": "search", "in": "query", "required": false, "description": "按文件名或子路径搜索", "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "ZIP文件下载",
							"content": map[string]interface{}{
								"application/zip": map[string]interface{}{
									"schema": map[string]interface{}{"type": "string", "format": "binary"},
								},
							},
						},
						"401": map[string]interface{}{"description": "未授权"},
					},
				},
			},
			"/api/chat-uploads/download": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"对话附件"},
					"summary":     "下载附件",
					"description": "下载指定的对话附件文件。",
					"operationId": "downloadChatUpload",
					"parameters": []map[string]interface{}{
						{"name": "path", "in": "query", "required": true, "description": "文件相对路径", "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "文件下载",
							"content": map[string]interface{}{
								"application/octet-stream": map[string]interface{}{
									"schema": map[string]interface{}{"type": "string", "format": "binary"},
								},
							},
						},
						"401": map[string]interface{}{"description": "未授权"},
						"404": map[string]interface{}{"description": "文件不存在"},
					},
				},
			},
			"/api/chat-uploads/path": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"对话附件"},
					"summary":     "解析附件路径",
					"description": "将文件管理中的相对路径或内部虚拟路径解析为服务器绝对路径，用于复制文件/目录路径。",
					"operationId": "resolveChatUploadPath",
					"parameters": []map[string]interface{}{
						{"name": "path", "in": "query", "required": true, "description": "相对路径或虚拟路径（如 __workspace__/projects/<id>/csv）", "schema": map[string]interface{}{"type": "string"}},
						{"name": "kind", "in": "query", "required": false, "description": "路径类型：file/directory，默认 file", "schema": map[string]interface{}{"type": "string", "enum": []string{"file", "directory"}}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "解析成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"absolutePath": map[string]interface{}{"type": "string"},
											"isDir":        map[string]interface{}{"type": "boolean"},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "未授权"},
						"403": map[string]interface{}{"description": "无权访问"},
						"404": map[string]interface{}{"description": "路径不存在"},
					},
				},
			},
			"/api/chat-uploads/content": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"对话附件"},
					"summary":     "获取附件文本内容",
					"description": "读取并返回文本文件的内容。",
					"operationId": "getChatUploadContent",
					"parameters": []map[string]interface{}{
						{"name": "path", "in": "query", "required": true, "description": "文件相对路径", "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "获取成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"content": map[string]interface{}{"type": "string", "description": "文件文本内容"},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "未授权"},
						"404": map[string]interface{}{"description": "文件不存在"},
					},
				},
				"put": map[string]interface{}{
					"tags":        []string{"对话附件"},
					"summary":     "写入附件文本内容",
					"description": "写入或覆盖文本文件的内容。",
					"operationId": "putChatUploadContent",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"path", "content"},
									"properties": map[string]interface{}{
										"path":    map[string]interface{}{"type": "string", "description": "文件相对路径"},
										"content": map[string]interface{}{"type": "string", "description": "文件文本内容"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "写入成功"},
						"401": map[string]interface{}{"description": "未授权"},
					},
				},
			},
			"/api/chat-uploads/mkdir": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"对话附件"},
					"summary":     "创建附件目录",
					"description": "在对话附件目录下创建子目录。",
					"operationId": "mkdirChatUpload",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"name"},
									"properties": map[string]interface{}{
										"parent": map[string]interface{}{"type": "string", "description": "父目录相对路径"},
										"name":   map[string]interface{}{"type": "string", "description": "目录名称"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "创建成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"ok":           map[string]interface{}{"type": "boolean"},
											"relativePath": map[string]interface{}{"type": "string"},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "未授权"},
					},
				},
			},
			"/api/chat-uploads/rename": map[string]interface{}{
				"put": map[string]interface{}{
					"tags":        []string{"对话附件"},
					"summary":     "重命名附件",
					"description": "重命名对话附件文件或目录。",
					"operationId": "renameChatUpload",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"path", "newName"},
									"properties": map[string]interface{}{
										"path":    map[string]interface{}{"type": "string", "description": "当前文件相对路径"},
										"newName": map[string]interface{}{"type": "string", "description": "新名称"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "重命名成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"ok":           map[string]interface{}{"type": "boolean"},
											"relativePath": map[string]interface{}{"type": "string"},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "未授权"},
					},
				},
			},

			// ==================== 机器人集成 ====================
			"/api/robot/wecom": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"机器人集成"},
					"summary":     "企业微信回调验证",
					"description": "企业微信服务器URL验证回调（用于配置消息接收地址时的验证）。无需认证。",
					"operationId": "wecomCallbackVerify",
					"security":    []map[string]interface{}{},
					"parameters": []map[string]interface{}{
						{"name": "msg_signature", "in": "query", "required": true, "schema": map[string]interface{}{"type": "string"}},
						{"name": "timestamp", "in": "query", "required": true, "schema": map[string]interface{}{"type": "string"}},
						{"name": "nonce", "in": "query", "required": true, "schema": map[string]interface{}{"type": "string"}},
						{"name": "echostr", "in": "query", "required": true, "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "验证成功，返回解密后的echostr"},
					},
				},
				"post": map[string]interface{}{
					"tags":        []string{"机器人集成"},
					"summary":     "企业微信消息回调",
					"description": "接收企业微信推送的消息事件。无需认证，由企业微信服务器调用。",
					"operationId": "wecomCallbackMessage",
					"security":    []map[string]interface{}{},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "处理成功"},
					},
				},
			},
			"/api/robot/dingtalk": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"机器人集成"},
					"summary":     "钉钉消息回调",
					"description": "接收钉钉推送的消息事件。无需认证，由钉钉服务器调用。",
					"operationId": "dingtalkCallback",
					"security":    []map[string]interface{}{},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "处理成功"},
					},
				},
			},
			"/api/robot/lark": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"机器人集成"},
					"summary":     "飞书消息回调",
					"description": "接收飞书推送的消息事件。无需认证，由飞书服务器调用。",
					"operationId": "larkCallback",
					"security":    []map[string]interface{}{},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "处理成功"},
					},
				},
			},
			"/api/robot/test": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"机器人集成"},
					"summary":     "测试机器人消息处理",
					"description": "模拟机器人消息处理流程，用于调试和验证。需要登录认证。",
					"operationId": "testRobot",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"platform", "text"},
									"properties": map[string]interface{}{
										"platform": map[string]interface{}{"type": "string", "description": "平台类型", "enum": []string{"dingtalk", "lark", "wecom"}},
										"user_id":  map[string]interface{}{"type": "string", "description": "模拟用户ID", "example": "test"},
										"text":     map[string]interface{}{"type": "string", "description": "消息文本", "example": "帮助"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "处理成功"},
						"401": map[string]interface{}{"description": "未授权"},
					},
				},
			},

			// ==================== 多代理Markdown ====================
			"/api/multi-agent/markdown-agents": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"多代理Markdown"},
					"summary":     "列出Markdown代理",
					"description": "获取所有多代理Markdown定义文件列表。",
					"operationId": "listMarkdownAgents",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "获取成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"agents": map[string]interface{}{
												"type": "array",
												"items": map[string]interface{}{
													"type": "object",
													"properties": map[string]interface{}{
														"filename":        map[string]interface{}{"type": "string", "description": "文件名"},
														"id":              map[string]interface{}{"type": "string", "description": "代理ID"},
														"name":            map[string]interface{}{"type": "string", "description": "代理名称"},
														"description":     map[string]interface{}{"type": "string", "description": "代理描述"},
														"is_orchestrator": map[string]interface{}{"type": "boolean", "description": "是否为编排器"},
														"kind":            map[string]interface{}{"type": "string", "description": "编排类型"},
													},
												},
											},
											"dir": map[string]interface{}{"type": "string", "description": "代理定义目录路径"},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "未授权"},
					},
				},
				"post": map[string]interface{}{
					"tags":        []string{"多代理Markdown"},
					"summary":     "创建Markdown代理",
					"description": "创建新的多代理Markdown定义文件。",
					"operationId": "createMarkdownAgent",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"name"},
									"properties": map[string]interface{}{
										"filename":       map[string]interface{}{"type": "string", "description": "文件名（可选，自动生成）"},
										"id":             map[string]interface{}{"type": "string", "description": "代理ID"},
										"name":           map[string]interface{}{"type": "string", "description": "代理名称"},
										"description":    map[string]interface{}{"type": "string", "description": "代理描述"},
										"tools":          map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "可用工具列表"},
										"instruction":    map[string]interface{}{"type": "string", "description": "代理指令"},
										"bind_role":      map[string]interface{}{"type": "string", "description": "绑定角色"},
										"max_iterations": map[string]interface{}{"type": "integer", "description": "最大迭代次数"},
										"kind":           map[string]interface{}{"type": "string", "description": "编排类型"},
										"raw":            map[string]interface{}{"type": "string", "description": "原始Markdown内容"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "创建成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"filename": map[string]interface{}{"type": "string"},
											"message":  map[string]interface{}{"type": "string", "example": "已创建"},
										},
									},
								},
							},
						},
						"400": map[string]interface{}{"description": "参数错误"},
						"401": map[string]interface{}{"description": "未授权"},
					},
				},
			},
			"/api/multi-agent/markdown-agents/{filename}": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"多代理Markdown"},
					"summary":     "获取Markdown代理详情",
					"description": "获取指定Markdown代理定义文件的详细内容。",
					"operationId": "getMarkdownAgent",
					"parameters": []map[string]interface{}{
						{"name": "filename", "in": "path", "required": true, "description": "文件名", "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "获取成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"filename":        map[string]interface{}{"type": "string"},
											"raw":             map[string]interface{}{"type": "string", "description": "原始Markdown内容"},
											"id":              map[string]interface{}{"type": "string"},
											"name":            map[string]interface{}{"type": "string"},
											"description":     map[string]interface{}{"type": "string"},
											"tools":           map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
											"instruction":     map[string]interface{}{"type": "string"},
											"bind_role":       map[string]interface{}{"type": "string"},
											"max_iterations":  map[string]interface{}{"type": "integer"},
											"kind":            map[string]interface{}{"type": "string"},
											"is_orchestrator": map[string]interface{}{"type": "boolean"},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "未授权"},
						"404": map[string]interface{}{"description": "代理不存在"},
					},
				},
				"put": map[string]interface{}{
					"tags":        []string{"多代理Markdown"},
					"summary":     "更新Markdown代理",
					"description": "更新指定的Markdown代理定义。",
					"operationId": "updateMarkdownAgent",
					"parameters": []map[string]interface{}{
						{"name": "filename", "in": "path", "required": true, "description": "文件名", "schema": map[string]interface{}{"type": "string"}},
					},
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"name":           map[string]interface{}{"type": "string"},
										"description":    map[string]interface{}{"type": "string"},
										"tools":          map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
										"instruction":    map[string]interface{}{"type": "string"},
										"bind_role":      map[string]interface{}{"type": "string"},
										"max_iterations": map[string]interface{}{"type": "integer"},
										"kind":           map[string]interface{}{"type": "string"},
										"raw":            map[string]interface{}{"type": "string"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "更新成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"message": map[string]interface{}{"type": "string", "example": "已保存"},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "未授权"},
						"404": map[string]interface{}{"description": "代理不存在"},
					},
				},
				"delete": map[string]interface{}{
					"tags":        []string{"多代理Markdown"},
					"summary":     "删除Markdown代理",
					"description": "删除指定的Markdown代理定义文件。",
					"operationId": "deleteMarkdownAgent",
					"parameters": []map[string]interface{}{
						{"name": "filename", "in": "path", "required": true, "description": "文件名", "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "删除成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"message": map[string]interface{}{"type": "string", "example": "已删除"},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "未授权"},
						"404": map[string]interface{}{"description": "代理不存在"},
					},
				},
			},

			// ==================== Skills管理 - 缺失端点 ====================
			"/api/skills/{name}/files": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Skills管理"},
					"summary":     "列出技能包文件",
					"description": "获取指定技能包目录下的所有文件列表。",
					"operationId": "listSkillPackageFiles",
					"parameters": []map[string]interface{}{
						{"name": "name", "in": "path", "required": true, "description": "技能名称/ID", "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "获取成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"files": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "文件路径列表"},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "未授权"},
						"404": map[string]interface{}{"description": "技能不存在"},
					},
				},
			},
			"/api/skills/{name}/file": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Skills管理"},
					"summary":     "获取技能包文件内容",
					"description": "读取技能包中指定文件的内容。",
					"operationId": "getSkillPackageFile",
					"parameters": []map[string]interface{}{
						{"name": "name", "in": "path", "required": true, "description": "技能名称/ID", "schema": map[string]interface{}{"type": "string"}},
						{"name": "path", "in": "query", "required": true, "description": "文件相对路径", "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "获取成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"path":    map[string]interface{}{"type": "string", "description": "文件路径"},
											"content": map[string]interface{}{"type": "string", "description": "文件内容"},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "未授权"},
						"404": map[string]interface{}{"description": "文件不存在"},
					},
				},
				"put": map[string]interface{}{
					"tags":        []string{"Skills管理"},
					"summary":     "写入技能包文件",
					"description": "写入或更新技能包中的文件内容。",
					"operationId": "putSkillPackageFile",
					"parameters": []map[string]interface{}{
						{"name": "name", "in": "path", "required": true, "description": "技能名称/ID", "schema": map[string]interface{}{"type": "string"}},
					},
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"path"},
									"properties": map[string]interface{}{
										"path":    map[string]interface{}{"type": "string", "description": "文件相对路径"},
										"content": map[string]interface{}{"type": "string", "description": "文件内容"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "保存成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"message": map[string]interface{}{"type": "string", "example": "saved"},
											"path":    map[string]interface{}{"type": "string"},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "未授权"},
					},
				},
			},

			// ==================== 监控 - 缺失端点 ====================
			"/api/monitor/executions/names": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"监控"},
					"summary":     "批量获取工具名称",
					"description": "根据执行ID列表批量获取对应的工具名称，消除前端N+1请求问题。",
					"operationId": "batchGetToolNames",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"ids"},
									"properties": map[string]interface{}{
										"ids": map[string]interface{}{
											"type":        "array",
											"items":       map[string]interface{}{"type": "string"},
											"description": "执行记录ID列表",
										},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "获取成功，返回ID到工具名称的映射",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type":                 "object",
										"additionalProperties": map[string]interface{}{"type": "string"},
										"description":          "键为执行ID，值为工具名称",
										"example":              map[string]interface{}{"exec-001": "nmap", "exec-002": "sqlmap"},
									},
								},
							},
						},
						"400": map[string]interface{}{"description": "参数错误"},
						"401": map[string]interface{}{"description": "未授权"},
					},
				},
			},

			// ==================== 知识库 - 缺失端点 ====================
			"/api/knowledge/stats": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"知识库"},
					"summary":     "获取知识库统计",
					"description": "获取知识库的总体统计信息，包括分类数和条目数。",
					"operationId": "getKnowledgeStats",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "获取成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"enabled":          map[string]interface{}{"type": "boolean", "description": "知识库是否启用"},
											"total_categories": map[string]interface{}{"type": "integer", "description": "分类总数"},
											"total_items":      map[string]interface{}{"type": "integer", "description": "条目总数"},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "未授权"},
					},
				},
			},

			"/api/mcp": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"MCP"},
					"summary":     "MCP端点",
					"description": "MCP (Model Context Protocol) 端点，用于处理MCP协议请求。\n**协议说明**：\n本端点遵循 JSON-RPC 2.0 规范，支持以下方法：\n**1. initialize** - 初始化MCP连接\n```json\n{\n  \"jsonrpc\": \"2.0\",\n  \"id\": \"init-1\",\n  \"method\": \"initialize\",\n  \"params\": {\n    \"protocolVersion\": \"2024-11-05\",\n    \"capabilities\": {},\n    \"clientInfo\": {\n      \"name\": \"MyClient\",\n      \"version\": \"1.0.0\"\n    }\n  }\n}\n```\n**2. tools/list** - 列出所有可用工具\n```json\n{\n  \"jsonrpc\": \"2.0\",\n  \"id\": \"list-1\",\n  \"method\": \"tools/list\",\n  \"params\": {}\n}\n```\n**3. tools/call** - 调用工具\n```json\n{\n  \"jsonrpc\": \"2.0\",\n  \"id\": \"call-1\",\n  \"method\": \"tools/call\",\n  \"params\": {\n    \"name\": \"nmap\",\n    \"arguments\": {\n      \"target\": \"192.168.1.1\",\n      \"ports\": \"80,443\"\n    }\n  }\n}\n```\n**4. prompts/list** - 列出所有提示词模板\n```json\n{\n  \"jsonrpc\": \"2.0\",\n  \"id\": \"prompts-list-1\",\n  \"method\": \"prompts/list\",\n  \"params\": {}\n}\n```\n**5. prompts/get** - 获取提示词模板\n```json\n{\n  \"jsonrpc\": \"2.0\",\n  \"id\": \"prompt-get-1\",\n  \"method\": \"prompts/get\",\n  \"params\": {\n    \"name\": \"prompt-name\",\n    \"arguments\": {}\n  }\n}\n```\n**6. resources/list** - 列出所有资源\n```json\n{\n  \"jsonrpc\": \"2.0\",\n  \"id\": \"resources-list-1\",\n  \"method\": \"resources/list\",\n  \"params\": {}\n}\n```\n**7. resources/read** - 读取资源内容\n```json\n{\n  \"jsonrpc\": \"2.0\",\n  \"id\": \"resource-read-1\",\n  \"method\": \"resources/read\",\n  \"params\": {\n    \"uri\": \"resource://example\"\n  }\n}\n```\n**错误代码说明**：\n- `-32700`: Parse error - JSON解析错误\n- `-32600`: Invalid Request - 无效请求\n- `-32601`: Method not found - 方法不存在\n- `-32602`: Invalid params - 参数无效\n- `-32603`: Internal error - 内部错误",
					"operationId": "mcpEndpoint",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/MCPMessage",
								},
								"examples": map[string]interface{}{
									"listTools": map[string]interface{}{
										"summary":     "列出所有工具",
										"description": "获取系统中所有可用的MCP工具列表",
										"value": map[string]interface{}{
											"jsonrpc": "2.0",
											"id":      "list-tools-1",
											"method":  "tools/list",
											"params":  map[string]interface{}{},
										},
									},
									"callTool": map[string]interface{}{
										"summary":     "调用工具",
										"description": "调用指定的MCP工具",
										"value": map[string]interface{}{
											"jsonrpc": "2.0",
											"id":      "call-tool-1",
											"method":  "tools/call",
											"params": map[string]interface{}{
												"name": "nmap",
												"arguments": map[string]interface{}{
													"target": "192.168.1.1",
													"ports":  "80,443",
												},
											},
										},
									},
									"initialize": map[string]interface{}{
										"summary":     "初始化连接",
										"description": "初始化MCP连接，获取服务器能力",
										"value": map[string]interface{}{
											"jsonrpc": "2.0",
											"id":      "init-1",
											"method":  "initialize",
											"params": map[string]interface{}{
												"protocolVersion": "2024-11-05",
												"capabilities":    map[string]interface{}{},
												"clientInfo": map[string]interface{}{
													"name":    "MyClient",
													"version": "1.0.0",
												},
											},
										},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "MCP响应（JSON-RPC 2.0格式）",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/MCPResponse",
									},
									"examples": map[string]interface{}{
										"success": map[string]interface{}{
											"summary":     "成功响应",
											"description": "工具调用成功的响应示例",
											"value": map[string]interface{}{
												"jsonrpc": "2.0",
												"id":      "call-tool-1",
												"result": map[string]interface{}{
													"content": []map[string]interface{}{
														{
															"type": "text",
															"text": "工具执行结果...",
														},
													},
													"isError": false,
												},
											},
										},
										"error": map[string]interface{}{
											"summary":     "错误响应",
											"description": "工具调用失败的响应示例",
											"value": map[string]interface{}{
												"jsonrpc": "2.0",
												"id":      "call-tool-1",
												"error": map[string]interface{}{
													"code":    -32601,
													"message": "Tool not found",
													"data":    "工具 'unknown-tool' 不存在",
												},
											},
										},
									},
								},
							},
						},
						"400": map[string]interface{}{
							"description": "请求格式错误（JSON解析失败）",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/MCPResponse",
									},
									"example": map[string]interface{}{
										"id": nil,
										"error": map[string]interface{}{
											"code":    -32700,
											"message": "Parse error",
											"data":    "unexpected end of JSON input",
										},
										"jsonrpc": "2.0",
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "未授权，需要有效的Token",
						},
						"405": map[string]interface{}{
							"description": "方法不允许（仅支持POST请求）",
						},
					},
				},
			},
		},
	}

	enrichSpecWithI18nKeys(spec)
	c.JSON(http.StatusOK, spec)
}

// GetConversationResults 获取对话结果（OpenAPI端点）
// 注意：创建对话和获取对话详情直接使用标准的 /api/conversations 端点
// 这个端点只是为了提供结果聚合功能
func (h *OpenAPIHandler) GetConversationResults(c *gin.Context) {
	conversationID := c.Param("id")

	// 验证对话是否存在
	conv, err := h.db.GetConversation(conversationID)
	if err != nil {
		h.logger.Error("获取对话失败", zap.Error(err))
		c.JSON(http.StatusNotFound, gin.H{"error": "对话不存在"})
		return
	}

	// 获取消息列表
	messages, err := h.db.GetMessages(conversationID)
	if err != nil {
		h.logger.Error("获取消息失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 获取漏洞列表
	vulnList, err := h.db.ListVulnerabilities(1000, 0, database.VulnerabilityListFilter{ConversationID: conversationID})
	if err != nil {
		h.logger.Warn("获取漏洞列表失败", zap.Error(err))
		vulnList = []*database.Vulnerability{}
	}
	vulnerabilities := make([]database.Vulnerability, len(vulnList))
	for i, v := range vulnList {
		vulnerabilities[i] = *v
	}

	// 获取执行结果（历史大结果由 Eino reduction 落盘，此处不再聚合文件存储）
	executionResults := []map[string]interface{}{}

	response := map[string]interface{}{
		"conversationId":   conv.ID,
		"messages":         messages,
		"vulnerabilities":  vulnerabilities,
		"executionResults": executionResults,
	}

	c.JSON(http.StatusOK, response)
}
