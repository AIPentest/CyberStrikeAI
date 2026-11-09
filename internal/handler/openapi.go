package handler

import (
	"net/http"

	"cyberstrike-ai/internal/database"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// OpenAPIHandler OpenAPI handler
type OpenAPIHandler struct {
	db               *database.DB
	logger           *zap.Logger
	conversationHdlr *ConversationHandler
	agentHdlr        *AgentHandler
}

// NewOpenAPIHandler creates a new OpenAPIHandler
func NewOpenAPIHandler(db *database.DB, logger *zap.Logger, conversationHdlr *ConversationHandler, agentHdlr *AgentHandler) *OpenAPIHandler {
	return &OpenAPIHandler{
		db:               db,
		logger:           logger,
		conversationHdlr: conversationHdlr,
		agentHdlr:        agentHdlr,
	}
}

// GetOpenAPISpec returns the OpenAPI specification
func (h *OpenAPIHandler) GetOpenAPISpec(c *gin.Context) {
	host := c.Request.Host
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}

	finalizationRequestSchema := map[string]interface{}{
		"type":        "object",
		"description": "Final reply delivery policy. The backend does not infer execution intent from natural-language content; execution entry points must explicitly declare whether completed tool evidence is required.",
		"properties": map[string]interface{}{
			"requireExecutionEvidence": map[string]interface{}{
				"type":        "boolean",
				"description": "When true, a missing completed tool execution record triggers a no-injection continue run or a final block; ordinary chat can omit this or set it to false.",
			},
		},
	}

	spec := map[string]interface{}{
		"openapi": "3.0.0",
		"info": map[string]interface{}{
			"title":       "CyberStrikeAI API",
			"description": "API documentation for the AI-driven automated security testing platform",
			"version":     "1.0.0",
			"contact": map[string]interface{}{
				"name": "CyberStrikeAI",
			},
		},
		"servers": []map[string]interface{}{
			{
				"url":         scheme + "://" + host,
				"description": "Current server",
			},
		},
		"components": map[string]interface{}{
			"securitySchemes": map[string]interface{}{
				"bearerAuth": map[string]interface{}{
					"type":         "http",
					"scheme":       "bearer",
					"bearerFormat": "JWT",
					"description":  "Authenticate with a Bearer Token. The token is obtained from the /api/auth/login endpoint.",
				},
			},
			"schemas": map[string]interface{}{
				"CreateConversationRequest": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"title": map[string]interface{}{
							"type":        "string",
							"description": "Conversation title",
							"example":     "Web application security testing",
						},
						"projectId": map[string]interface{}{
							"type":        "string",
							"description": "Bound project ID (optional, shared fact whiteboard)",
						},
					},
				},
				"SetConversationProjectRequest": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"projectId": map[string]interface{}{
							"type":        "string",
							"description": "Project ID; an empty string unbinds",
						},
					},
					"required": []string{"projectId"},
				},
				"AgentChatResponse": map[string]interface{}{
					"type":        "object",
					"description": "Agent non-streaming response. response is just the delivered text; whether it is a successful final reply must be judged by finalized/finalizable/status.",
					"properties": map[string]interface{}{
						"response": map[string]interface{}{
							"type":        "string",
							"description": "Text delivered to the user. When finalized=false it is a block/incomplete explanation, not a successful conclusion.",
						},
						"conversationId": map[string]interface{}{
							"type":        "string",
							"description": "Conversation ID",
						},
						"assistantMessageId": map[string]interface{}{
							"type":        "string",
							"description": "Assistant message ID (returned by some endpoints)",
						},
						"mcpExecutionIds": map[string]interface{}{
							"type":        "array",
							"description": "MCP tool execution IDs associated with this turn",
							"items":       map[string]interface{}{"type": "string"},
						},
						"agentMode": map[string]interface{}{
							"type":        "string",
							"description": "agent mode, e.g. eino_single, eino_deep, workflow",
						},
						"finalized": map[string]interface{}{
							"type":        "boolean",
							"description": "Whether the final reply check has passed. Only true counts as a successful final reply.",
						},
						"finalizable": map[string]interface{}{
							"type":        "boolean",
							"description": "Whether the candidate output can be promoted to a final reply.",
						},
						"status": map[string]interface{}{
							"type":        "string",
							"description": "Finalization status",
							"enum":        []string{"completed", "in_progress", "blocked", "failed", "cancelled", "awaiting_hitl"},
						},
						"completionReason": map[string]interface{}{
							"type":        "string",
							"description": "Finalization or block reason, e.g. verified, pending_tool_executions, missing_execution_evidence",
						},
						"evidenceVerified": map[string]interface{}{
							"type":        "boolean",
							"description": "Whether the evidence satisfies the finalization requirement",
						},
						"evidenceRefs": map[string]interface{}{
							"type":        "array",
							"description": "Evidence references, e.g. mcp_execution:<id>",
							"items":       map[string]interface{}{"type": "string"},
						},
						"pendingExecutionIds": map[string]interface{}{
							"type":        "array",
							"description": "Tool execution IDs still in queued/running state",
							"items":       map[string]interface{}{"type": "string"},
						},
						"missingChecks": map[string]interface{}{
							"type":        "array",
							"description": "List of reasons the finalization check failed",
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
							"description": "Conversation ID",
							"example":     "550e8400-e29b-41d4-a716-446655440000",
						},
						"title": map[string]interface{}{
							"type":        "string",
							"description": "Conversation title",
							"example":     "Web application security testing",
						},
						"createdAt": map[string]interface{}{
							"type":        "string",
							"format":      "date-time",
							"description": "Created time",
						},
						"updatedAt": map[string]interface{}{
							"type":        "string",
							"format":      "date-time",
							"description": "Updated time",
						},
						"projectId": map[string]interface{}{
							"type":        "string",
							"description": "Bound project ID (optional)",
						},
					},
				},
				"ConversationDetail": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id": map[string]interface{}{
							"type":        "string",
							"description": "Conversation ID",
						},
						"title": map[string]interface{}{
							"type":        "string",
							"description": "Conversation title",
						},
						"status": map[string]interface{}{
							"type":        "string",
							"description": "Conversation status: active (in progress), completed (done), failed (failed)",
							"enum":        []string{"active", "completed", "failed"},
						},
						"createdAt": map[string]interface{}{
							"type":        "string",
							"format":      "date-time",
							"description": "Created time",
						},
						"updatedAt": map[string]interface{}{
							"type":        "string",
							"format":      "date-time",
							"description": "Updated time",
						},
						"messages": map[string]interface{}{
							"type":        "array",
							"description": "Message list",
							"items": map[string]interface{}{
								"$ref": "#/components/schemas/Message",
							},
						},
						"messageCount": map[string]interface{}{
							"type":        "integer",
							"description": "Message count",
						},
					},
				},
				"Message": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id": map[string]interface{}{
							"type":        "string",
							"description": "Message ID",
						},
						"conversationId": map[string]interface{}{
							"type":        "string",
							"description": "Conversation ID",
						},
						"role": map[string]interface{}{
							"type":        "string",
							"description": "Message role: user (user), assistant (assistant)",
							"enum":        []string{"user", "assistant"},
						},
						"content": map[string]interface{}{
							"type":        "string",
							"description": "Message content",
						},
						"createdAt": map[string]interface{}{
							"type":        "string",
							"format":      "date-time",
							"description": "Created time",
						},
					},
				},
				"ConversationResults": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"conversationId": map[string]interface{}{
							"type":        "string",
							"description": "Conversation ID",
						},
						"messages": map[string]interface{}{
							"type":        "array",
							"description": "Message list",
							"items": map[string]interface{}{
								"$ref": "#/components/schemas/Message",
							},
						},
						"vulnerabilities": map[string]interface{}{
							"type":        "array",
							"description": "List of discovered vulnerabilities",
							"items": map[string]interface{}{
								"$ref": "#/components/schemas/Vulnerability",
							},
						},
						"executionResults": map[string]interface{}{
							"type":        "array",
							"description": "List of execution results",
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
							"description": "Vulnerability ID",
						},
						"title": map[string]interface{}{
							"type":        "string",
							"description": "Vulnerability title",
						},
						"description": map[string]interface{}{
							"type":        "string",
							"description": "Vulnerability description",
						},
						"severity": map[string]interface{}{
							"type":        "string",
							"description": "Severity",
							"enum":        []string{"critical", "high", "medium", "low", "info"},
						},
						"status": map[string]interface{}{
							"type":        "string",
							"description": "Status",
							"enum":        []string{"open", "confirmed", "fixed", "false_positive", "ignored"},
						},
						"target": map[string]interface{}{
							"type":        "string",
							"description": "Affected target",
						},
					},
				},
				"AssetImportItem": map[string]interface{}{
					"type":        "object",
					"description": "Asset to import; at least one of host, ip, domain must be non-empty",
					"properties": map[string]interface{}{
						"project_id":         map[string]interface{}{"type": "string", "description": "Owning project ID; the caller must have access to it"},
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
						"responsible_person": map[string]interface{}{"type": "string", "maxLength": 255, "description": "Asset owner"},
						"department":         map[string]interface{}{"type": "string", "maxLength": 255, "description": "Owning department"},
						"business_system":    map[string]interface{}{"type": "string", "maxLength": 255, "description": "Owning business system"},
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
						"source":       map[string]interface{}{"type": "string", "description": "Default source used when the asset does not fill in a source"},
						"source_query": map[string]interface{}{"type": "string", "description": "Default source query or import file name"},
					},
				},
				"AssetImportResult": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"created": map[string]interface{}{"type": "integer", "description": "Created count", "example": 120},
						"updated": map[string]interface{}{"type": "integer", "description": "Deduplicated/merged count", "example": 8},
						"skipped": map[string]interface{}{"type": "integer", "description": "Skipped count", "example": 2},
					},
				},
				"ExecutionResult": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id": map[string]interface{}{
							"type":        "string",
							"description": "Execution ID",
						},
						"toolName": map[string]interface{}{
							"type":        "string",
							"description": "Tool name",
						},
						"status": map[string]interface{}{
							"type":        "string",
							"description": "Execution status",
							"enum":        []string{"queued", "running", "completed", "failed", "cancelled", "hard_timeout", "orphaned"},
						},
						"result": map[string]interface{}{
							"type":        "string",
							"description": "Execution result",
						},
						"createdAt": map[string]interface{}{
							"type":        "string",
							"format":      "date-time",
							"description": "Created time",
						},
					},
				},
				"Error": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"error": map[string]interface{}{
							"type":        "string",
							"description": i18nText(requestWantsEnglish(c), "Error message", "error message"),
						},
					},
				},
				"LoginRequest": map[string]interface{}{
					"type":     "object",
					"required": []string{"password"},
					"properties": map[string]interface{}{
						"password": map[string]interface{}{
							"type":        "string",
							"description": "Login password",
						},
					},
				},
				"LoginResponse": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"token": map[string]interface{}{
							"type":        "string",
							"description": "Authentication token",
						},
						"expires_at": map[string]interface{}{
							"type":        "string",
							"format":      "date-time",
							"description": "Token expiry time",
						},
						"session_duration_hr": map[string]interface{}{
							"type":        "integer",
							"description": "Session duration (hours)",
						},
					},
				},
				"ChangePasswordRequest": map[string]interface{}{
					"type":     "object",
					"required": []string{"oldPassword", "newPassword"},
					"properties": map[string]interface{}{
						"oldPassword": map[string]interface{}{
							"type":        "string",
							"description": "Current password",
						},
						"newPassword": map[string]interface{}{
							"type":        "string",
							"description": "New password (at least 8 characters)",
						},
					},
				},
				"UpdateConversationRequest": map[string]interface{}{
					"type":     "object",
					"required": []string{"title"},
					"properties": map[string]interface{}{
						"title": map[string]interface{}{
							"type":        "string",
							"description": "Conversation title",
						},
					},
				},
				"BatchTaskRequest": map[string]interface{}{
					"type":     "object",
					"required": []string{"tasks"},
					"properties": map[string]interface{}{
						"title": map[string]interface{}{
							"type":        "string",
							"description": "Task title (optional)",
						},
						"tasks": map[string]interface{}{
							"type":        "array",
							"description": "Task list, one task per line",
							"items": map[string]interface{}{
								"type": "string",
							},
						},
						"role": map[string]interface{}{
							"type":        "string",
							"description": "Role name (optional)",
						},
						"agentMode": map[string]interface{}{
							"type":        "string",
							"description": "Agent mode: eino_single (Eino ADK single agent, default) | deep | plan_execute | supervisor",
							"enum":        []string{"eino_single", "deep", "plan_execute", "supervisor"},
						},
						"scheduleMode": map[string]interface{}{
							"type":        "string",
							"description": "Scheduling mode (manual | cron)",
							"enum":        []string{"manual", "cron"},
						},
						"cronExpr": map[string]interface{}{
							"type":        "string",
							"description": "Cron expression (required when scheduleMode=cron)",
						},
						"executeNow": map[string]interface{}{
							"type":        "boolean",
							"description": "Whether to execute immediately after creation (default false)",
						},
					},
				},
				"BatchQueue": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id": map[string]interface{}{
							"type":        "string",
							"description": "Queue ID",
						},
						"title": map[string]interface{}{
							"type":        "string",
							"description": "Queue title",
						},
						"status": map[string]interface{}{
							"type":        "string",
							"description": "Queue status",
							"enum":        []string{"pending", "running", "paused", "completed", "failed"},
						},
						"tasks": map[string]interface{}{
							"type":        "array",
							"description": "Task list",
							"items": map[string]interface{}{
								"type": "object",
							},
						},
						"createdAt": map[string]interface{}{
							"type":        "string",
							"format":      "date-time",
							"description": "Created time",
						},
					},
				},
				"CancelAgentLoopRequest": map[string]interface{}{
					"type":     "object",
					"required": []string{"conversationId"},
					"properties": map[string]interface{}{
						"conversationId": map[string]interface{}{
							"type":        "string",
							"description": "Conversation ID",
						},
						"reason": map[string]interface{}{
							"type":        "string",
							"description": "Optional. Matches the MCP monitoring page's \"terminate with note\": when non-empty, it is merged into the current tool's text returned to the model (including the USER INTERRUPT NOTE block)",
						},
						"continueAfter": map[string]interface{}{
							"type":        "boolean",
							"description": "When true, only terminates the current in-progress MCP tool call (does not cancel the whole turn); a tool must already be executing, otherwise 400",
						},
					},
				},
				"AgentTask": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"conversationId": map[string]interface{}{
							"type":        "string",
							"description": "Conversation ID",
						},
						"status": map[string]interface{}{
							"type":        "string",
							"description": "Task status",
							"enum":        []string{"running", "completed", "failed", "cancelled", "timeout"},
						},
						"startedAt": map[string]interface{}{
							"type":        "string",
							"format":      "date-time",
							"description": "Start time",
						},
					},
				},
				"CreateVulnerabilityRequest": map[string]interface{}{
					"type":     "object",
					"required": []string{"conversation_id", "title", "description", "severity", "type", "target", "reproduction_steps", "evidence", "impact", "recommendation"},
					"properties": map[string]interface{}{
						"conversation_id": map[string]interface{}{
							"type":        "string",
							"description": "Conversation ID",
						},
						"title": map[string]interface{}{
							"type":        "string",
							"description": "Vulnerability title",
						},
						"description": map[string]interface{}{
							"type":        "string",
							"description": "Vulnerability description",
						},
						"severity": map[string]interface{}{
							"type":        "string",
							"description": "Severity",
							"enum":        []string{"critical", "high", "medium", "low", "info"},
						},
						"status": map[string]interface{}{
							"type":        "string",
							"description": "Status",
							"enum":        []string{"open", "closed", "fixed"},
						},
						"type": map[string]interface{}{
							"type":        "string",
							"description": "Vulnerability type",
						},
						"target": map[string]interface{}{
							"type":        "string",
							"description": "Affected target",
						},
						"preconditions":      map[string]interface{}{"type": "string", "description": "Preconditions"},
						"reproduction_steps": map[string]interface{}{"type": "string", "description": "Reproduction steps"},
						"evidence":           map[string]interface{}{"type": "string", "description": "Evidence/POC, including request and response, command output, screenshot descriptions, logs, etc."},
						"impact": map[string]interface{}{
							"type":        "string",
							"description": "Impact",
						},
						"recommendation": map[string]interface{}{
							"type":        "string",
							"description": "Remediation recommendation",
						},
						"retest_notes": map[string]interface{}{"type": "string", "description": "Retest method"},
						"retest_log":   map[string]interface{}{"type": "string", "description": "Retest log (read-only, appended by the retest flow)"},
					},
				},
				"UpdateVulnerabilityRequest": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"title": map[string]interface{}{
							"type":        "string",
							"description": "Vulnerability title",
						},
						"description": map[string]interface{}{
							"type":        "string",
							"description": "Vulnerability description",
						},
						"severity": map[string]interface{}{
							"type":        "string",
							"description": "Severity",
							"enum":        []string{"critical", "high", "medium", "low", "info"},
						},
						"status": map[string]interface{}{
							"type":        "string",
							"description": "Status",
							"enum":        []string{"open", "confirmed", "fixed", "false_positive", "ignored"},
						},
						"type": map[string]interface{}{
							"type":        "string",
							"description": "Vulnerability type",
						},
						"target": map[string]interface{}{
							"type":        "string",
							"description": "Affected target",
						},
						"preconditions":      map[string]interface{}{"type": "string", "description": "Preconditions"},
						"reproduction_steps": map[string]interface{}{"type": "string", "description": "Reproduction steps"},
						"evidence":           map[string]interface{}{"type": "string", "description": "Evidence/POC, including request and response, command output, screenshot descriptions, logs, etc."},
						"impact": map[string]interface{}{
							"type":        "string",
							"description": "Impact",
						},
						"recommendation": map[string]interface{}{
							"type":        "string",
							"description": "Remediation recommendation",
						},
						"retest_notes": map[string]interface{}{"type": "string", "description": "Retest method"},
					},
				},
				"ListVulnerabilitiesResponse": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"vulnerabilities": map[string]interface{}{
							"type":        "array",
							"description": "Vulnerability list",
							"items": map[string]interface{}{
								"$ref": "#/components/schemas/Vulnerability",
							},
						},
						"total": map[string]interface{}{
							"type":        "integer",
							"description": "Total",
						},
						"page": map[string]interface{}{
							"type":        "integer",
							"description": "Current page",
						},
						"page_size": map[string]interface{}{
							"type":        "integer",
							"description": "Page size",
						},
						"total_pages": map[string]interface{}{
							"type":        "integer",
							"description": "Total pages",
						},
					},
				},
				"VulnerabilityStats": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"total": map[string]interface{}{
							"type":        "integer",
							"description": "Total vulnerabilities",
						},
						"by_severity": map[string]interface{}{
							"type":        "object",
							"description": "Statistics by severity",
						},
						"by_status": map[string]interface{}{
							"type":        "object",
							"description": "Statistics by status",
						},
					},
				},
				"RoleConfig": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"name": map[string]interface{}{
							"type":        "string",
							"description": "Role name",
						},
						"description": map[string]interface{}{
							"type":        "string",
							"description": "Role description",
						},
						"enabled": map[string]interface{}{
							"type":        "boolean",
							"description": "Whether enabled",
						},
						"systemPrompt": map[string]interface{}{
							"type":        "string",
							"description": "System prompt",
						},
						"userPrompt": map[string]interface{}{
							"type":        "string",
							"description": "User prompt",
						},
						"tools": map[string]interface{}{
							"type":        "array",
							"description": "Tool list",
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
							"description": "Skill name",
						},
						"description": map[string]interface{}{
							"type":        "string",
							"description": "Skill description",
						},
						"path": map[string]interface{}{
							"type":        "string",
							"description": "Skill path",
						},
					},
				},
				"CreateSkillRequest": map[string]interface{}{
					"type":     "object",
					"required": []string{"name", "description"},
					"properties": map[string]interface{}{
						"name": map[string]interface{}{
							"type":        "string",
							"description": "Skill name",
						},
						"description": map[string]interface{}{
							"type":        "string",
							"description": "Skill description",
						},
					},
				},
				"UpdateSkillRequest": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"description": map[string]interface{}{
							"type":        "string",
							"description": "Skill description",
						},
					},
				},
				"ToolExecution": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id": map[string]interface{}{
							"type":        "string",
							"description": "Execution ID",
						},
						"toolName": map[string]interface{}{
							"type":        "string",
							"description": "Tool name",
						},
						"status": map[string]interface{}{
							"type":        "string",
							"description": "Execution status",
							"enum":        []string{"queued", "running", "completed", "failed", "cancelled", "hard_timeout", "orphaned"},
						},
						"createdAt": map[string]interface{}{
							"type":        "string",
							"format":      "date-time",
							"description": "Created time",
						},
					},
				},
				"MonitorResponse": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"executions": map[string]interface{}{
							"type":        "array",
							"description": "Execution record list (lightweight fields, without arguments/result)",
							"items": map[string]interface{}{
								"$ref": "#/components/schemas/ToolExecution",
							},
						},
						"summary": map[string]interface{}{
							"type":        "object",
							"description": "Tool call summary",
						},
						"topTools": map[string]interface{}{
							"type":        "array",
							"description": "Top N tools by call volume",
							"items": map[string]interface{}{
								"type": "object",
							},
						},
						"timestamp": map[string]interface{}{
							"type":        "string",
							"format":      "date-time",
							"description": "Timestamp",
						},
						"total": map[string]interface{}{
							"type":        "integer",
							"description": "Total execution records",
						},
						"page": map[string]interface{}{
							"type":        "integer",
							"description": "Current page",
						},
						"pageSize": map[string]interface{}{
							"type":        "integer",
							"description": "Page size",
						},
						"totalPages": map[string]interface{}{
							"type":        "integer",
							"description": "Total pages",
						},
						"retentionDays": map[string]interface{}{
							"type":        "integer",
							"description": "Execution record retention days",
						},
					},
				},
				"ConfigResponse": map[string]interface{}{
					"type":        "object",
					"description": "Configuration information (including openai, vision, multi_agent, etc.)",
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
					"description": "Update configuration request",
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
					"description": "Agent run and external MCP anti-deadlock protection configuration",
					"properties": map[string]interface{}{
						"max_iterations":                         map[string]interface{}{"type": "integer", "description": "Maximum iteration count"},
						"tool_timeout_minutes":                   map[string]interface{}{"type": "integer", "description": "Hard timeout for a single tool execution (minutes)"},
						"tool_wait_timeout_seconds":              map[string]interface{}{"type": "integer", "description": "Tool single-turn wait seconds; when reached it returns execution_id and the worker continues background execution"},
						"external_mcp_max_concurrent_per_server": map[string]interface{}{"type": "integer", "description": "Per external MCP server concurrency limit; 0=default 2; negative=no limit"},
						"external_mcp_max_concurrent_total":      map[string]interface{}{"type": "integer", "description": "External MCP global concurrency limit; 0=default 16; negative=no limit"},
						"external_mcp_circuit_failure_threshold": map[string]interface{}{"type": "integer", "description": "Consecutive failure circuit-breaker threshold; 0=default 3; negative=disabled"},
						"external_mcp_circuit_cooldown_seconds":  map[string]interface{}{"type": "integer", "description": "Circuit-breaker cooldown seconds; 0=default 60"},
						"shell_no_output_timeout_seconds":        map[string]interface{}{"type": "integer", "description": "Seconds to terminate execute/exec on continuous no output"},
						"workspace_root_dir":                     map[string]interface{}{"type": "string", "description": "Root path of the session working directory"},
						"system_prompt_path":                     map[string]interface{}{"type": "string", "description": "Single-agent system prompt file path"},
					},
				},
				"VisionConfig": map[string]interface{}{
					"type":        "object",
					"description": "Visual analysis (analyze_image MCP tool); the tool is registered when enabled and model is non-empty",
					"properties": map[string]interface{}{
						"enabled":                     map[string]interface{}{"type": "boolean", "description": "Whether analyze_image is enabled"},
						"model":                       map[string]interface{}{"type": "string", "description": "Vision model name (required)", "example": "qwen-vl-max"},
						"api_key":                     map[string]interface{}{"type": "string", "description": "API Key; falls back to openai.api_key when empty"},
						"base_url":                    map[string]interface{}{"type": "string", "description": "Base URL; falls back to openai.base_url when empty"},
						"provider":                    map[string]interface{}{"type": "string", "description": "Provider; falls back to openai.provider when empty"},
						"timeout_seconds":             map[string]interface{}{"type": "integer", "description": "VL call timeout (seconds)"},
						"max_image_bytes":             map[string]interface{}{"type": "integer", "description": "Original file size limit (bytes)"},
						"max_dimension":               map[string]interface{}{"type": "integer", "description": "Long-edge scaling pixels"},
						"jpeg_quality":                map[string]interface{}{"type": "integer", "description": "JPEG quality 60-100"},
						"max_payload_bytes":           map[string]interface{}{"type": "integer", "description": "Payload size limit sent to the API (bytes)"},
						"skip_preprocess_below_bytes": map[string]interface{}{"type": "integer", "description": "Below this byte size and within dimension limits the original image is sent directly; 0=always compress"},
						"detail":                      map[string]interface{}{"type": "string", "enum": []string{"low", "high", "auto"}, "description": "OpenAI-compatible image detail"},
					},
				},
				"AnalyzeImageToolCall": map[string]interface{}{
					"type":        "object",
					"description": "Built-in MCP tool analyze_image: analyzes server-local images and returns plain text (captchas/UI/error pages, etc.)",
					"properties": map[string]interface{}{
						"path": map[string]interface{}{
							"type":        "string",
							"description": "Absolute image path, or a path relative to the process working directory",
						},
						"question": map[string]interface{}{
							"type":        "string",
							"description": "Optional: focus question; for captchas \"output only the captcha characters\" is recommended",
						},
					},
					"required": []string{"path"},
				},
				"ExternalMCPConfig": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"enabled": map[string]interface{}{
							"type":        "boolean",
							"description": "Whether enabled",
						},
						"command": map[string]interface{}{
							"type":        "string",
							"description": "Command",
						},
						"args": map[string]interface{}{
							"type":        "array",
							"description": "Parameter list",
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
							"description": "Status",
							"enum":        []string{"connected", "disconnected", "error", "disabled"},
						},
						"toolCount": map[string]interface{}{
							"type":        "integer",
							"description": "Tool count",
						},
						"error": map[string]interface{}{
							"type":        "string",
							"description": i18nText(requestWantsEnglish(c), "Error message", "error message"),
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
					"description": "Attack chain data",
				},
				"MCPMessage": map[string]interface{}{
					"type":        "object",
					"description": "MCP message (following the JSON-RPC 2.0 specification)",
					"required":    []string{"jsonrpc"},
					"properties": map[string]interface{}{
						"id": map[string]interface{}{
							"description": "Message ID, can be a string, number, or null. Required for requests; may be omitted for notifications",
							"oneOf": []map[string]interface{}{
								{"type": "string"},
								{"type": "number"},
								{"type": "null"},
							},
							"example": "550e8400-e29b-41d4-a716-446655440000",
						},
						"method": map[string]interface{}{
							"type":        "string",
							"description": "Method name. Supported methods:\n- `initialize`: initialize the MCP connection\n- `tools/list`: list all available tools\n- `tools/call`: invoke a tool\n- `prompts/list`: list all prompt templates\n- `prompts/get`: get a prompt template\n- `resources/list`: list all resources\n- `resources/read`: read resource content\n- `sampling/request`: sampling request",
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
							"description": "Method parameters (JSON object); structure varies by method",
							"type":        "object",
						},
						"jsonrpc": map[string]interface{}{
							"type":        "string",
							"description": "JSON-RPC version, fixed to \"2.0\"",
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
							"description": "Protocol version",
							"example":     "2024-11-05",
						},
						"capabilities": map[string]interface{}{
							"type":        "object",
							"description": "Client capabilities",
						},
						"clientInfo": map[string]interface{}{
							"type":     "object",
							"required": []string{"name", "version"},
							"properties": map[string]interface{}{
								"name": map[string]interface{}{
									"type":        "string",
									"description": "Client name",
									"example":     "MyClient",
								},
								"version": map[string]interface{}{
									"type":        "string",
									"description": "Client version",
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
							"description": "Tool name",
							"example":     "nmap",
						},
						"arguments": map[string]interface{}{
							"type":        "object",
							"description": "Tool parameters (key-value pairs); the specific parameters depend on the tool definition",
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
							"description": "Message ID (same as the id in the request)",
							"oneOf": []map[string]interface{}{
								{"type": "string"},
								{"type": "number"},
								{"type": "null"},
							},
						},
						"result": map[string]interface{}{
							"description": "Method execution result (JSON object); structure depends on the method called",
							"type":        "object",
						},
						"error": map[string]interface{}{
							"type":        "object",
							"description": "Error message (if execution failed)",
							"properties": map[string]interface{}{
								"code": map[string]interface{}{
									"type":        "integer",
									"description": "Error code",
									"example":     -32600,
								},
								"message": map[string]interface{}{
									"type":        "string",
									"description": "Error message",
									"example":     "Invalid Request",
								},
								"data": map[string]interface{}{
									"description": "Error details (optional)",
								},
							},
						},
						"jsonrpc": map[string]interface{}{
							"type":        "string",
							"description": "JSON-RPC version",
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
					"tags":        []string{"Authentication"},
					"summary":     "User login",
					"description": "Log in with a password to obtain an authentication token",
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
							"description": "Login successful",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/LoginResponse",
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "Wrong password",
						},
					},
				},
			},
			"/api/auth/logout": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Authentication"},
					"summary":     "User logout",
					"description": "Log out of the current session, invalidating the token",
					"operationId": "logout",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Logout successful",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"message": map[string]interface{}{
												"type":    "string",
												"example": "Logged out",
											},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/auth/change-password": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Authentication"},
					"summary":     "Change password",
					"description": "Change the login password; all sessions are invalidated after the change",
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
							"description": "Password changed successfully",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"message": map[string]interface{}{
												"type":    "string",
												"example": "Password updated; please log in again with the new password",
											},
										},
									},
								},
							},
						},
						"400": map[string]interface{}{
							"description": "Bad request",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/auth/validate": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Authentication"},
					"summary":     "Validate token",
					"description": "Validate whether the current token is valid",
					"operationId": "validateToken",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Token valid",
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
												"description": "Expiry time",
											},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "Token invalid or expired",
						},
					},
				},
			},
			"/api/conversations": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Conversation Management"},
					"summary":     "Create conversation",
					"description": "Create a new security testing conversation.\n**Important notes**:\n- ✅ The created conversation is **immediately saved to the database**\n- ✅ The frontend page **automatically refreshes** to show the new conversation\n- ✅ Fully consistent with conversations created from the frontend\n**Two ways to create a conversation**:\n**Option 1 (recommended):** Send a message directly via `/api/eino-agent` **without** the `conversationId` parameter; the system automatically creates a new conversation and sends the message. This is the simplest way, creating and sending in one step.\n**Option 2:** First call this endpoint to create an empty conversation, then use the returned `conversationId` to call `/api/eino-agent` to send the message. Suitable when you need to create the conversation first and send the message later.\n**Example**:\n```json\n{\n  \"title\": \"Web application security testing\"\n}\n```",
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
							"description": "Conversation created successfully",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/Conversation",
									},
								},
							},
						},
						"400": map[string]interface{}{
							"description": "Bad request",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized, a valid Token is required",
						},
						"500": map[string]interface{}{
							"description": "Internal server error",
						},
					},
				},
				"get": map[string]interface{}{
					"tags":        []string{"Conversation Management"},
					"summary":     "List conversations",
					"description": "Get the conversation list, supporting pagination and search",
					"operationId": "listConversations",
					"parameters": []map[string]interface{}{
						{
							"name":        "limit",
							"in":          "query",
							"required":    false,
							"description": "Return count limit",
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
							"description": "Offset",
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
							"description": "Search keyword",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
						{
							"name":        "project_id",
							"in":          "query",
							"required":    false,
							"description": "Filter by project; passing __none__ means only conversations not bound to a project",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
						{
							"name":        "sort_by",
							"in":          "query",
							"required":    false,
							"description": "Sort field: updated_at (default) or created_at",
							"schema": map[string]interface{}{
								"type": "string",
								"enum": []string{"updated_at", "created_at"},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
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
							"description": "Unauthorized, a valid Token is required",
						},
					},
				},
			},
			"/api/conversations/{id}": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Conversation Management"},
					"summary":     "Get conversation detail",
					"description": "Get the details of the specified conversation, including conversation information and the message list",
					"operationId": "getConversation",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "Conversation ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/ConversationDetail",
									},
								},
							},
						},
						"404": map[string]interface{}{
							"description": "Conversation not found",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized, a valid Token is required",
						},
					},
				},
				"put": map[string]interface{}{
					"tags":        []string{"Conversation Management"},
					"summary":     "Update conversation",
					"description": "Update the conversation title",
					"operationId": "updateConversation",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "Conversation ID",
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
							"description": "Updated successfully",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/Conversation",
									},
								},
							},
						},
						"400": map[string]interface{}{
							"description": "Bad request",
						},
						"404": map[string]interface{}{
							"description": "Conversation not found",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized, a valid Token is required",
						},
					},
				},
				"delete": map[string]interface{}{
					"tags":        []string{"Conversation Management"},
					"summary":     "Delete conversation",
					"description": "Delete the specified conversation and its session data (messages, attack chains, etc.). **Vulnerability records are preserved**, only unlinked from the conversation. **This operation is not recoverable**.",
					"operationId": "deleteConversation",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "Conversation ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Deleted successfully",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"message": map[string]interface{}{
												"type":        "string",
												"description": "Success message",
												"example":     "Deleted successfully",
											},
										},
									},
								},
							},
						},
						"404": map[string]interface{}{
							"description": "Conversation not found",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized, a valid Token is required",
						},
						"500": map[string]interface{}{
							"description": "Internal server error",
						},
					},
				},
			},
			"/api/conversations/{id}/project": map[string]interface{}{
				"put": map[string]interface{}{
					"tags":        []string{"Conversation Management"},
					"summary":     "Set conversation's project",
					"description": "Bind or unbind the conversation's association with a project, used for the shared fact whiteboard",
					"operationId": "setConversationProject",
					"parameters": []map[string]interface{}{
						{
							"name": "id", "in": "path", "required": true,
							"description": "Conversation ID",
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
						"200": map[string]interface{}{"description": "Set successfully"},
						"400": map[string]interface{}{"description": "Project not found or bad request"},
						"404": map[string]interface{}{"description": "Conversation not found"},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},
			"/api/conversations/{id}/results": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Conversation Management"},
					"summary":     "Get conversation results",
					"description": "Get the execution results of the specified conversation, including messages, vulnerability information and execution results",
					"operationId": "getConversationResults",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "Conversation ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/ConversationResults",
									},
								},
							},
						},
						"404": map[string]interface{}{
							"description": "Conversation or result not found",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized, a valid Token is required",
						},
					},
				},
			},
			"/api/eino-agent": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Conversation Interaction"},
					"summary":     "Send a message and get the AI reply (Eino ADK single agent, non-streaming)",
					"description": "Send a message to the AI and get a streaming reply (SSE). Executed by the Eino **single-agent** ADK; event types match the multi-agent streaming ones (including `tool_call` / `response_delta` / `thinking`, etc.). `response_start` / `response_delta` are only candidate/process output; only an event with `type: response` and `data.finalized=true` represents a successful final reply. When completed execution evidence is missing, a `finalization_auto_continue` event may be sent first, meaning the server-side continued the run from an existing trace without injection. When `data.finalized=false`, message is an incomplete/blocked explanation. **Does not depend on** `multi_agent.enabled`.",
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
							"description": "Success. Only finalized=true indicates a successful final reply; when finalized=false, response is a block/incomplete explanation.",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{"$ref": "#/components/schemas/AgentChatResponse"},
								},
							},
						},
						"400": map[string]interface{}{"description": "Bad request"},
						"401": map[string]interface{}{"description": "Unauthorized"},
						"500": map[string]interface{}{"description": "Execution failed"},
					},
				},
			},
			"/api/eino-agent/stream": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Conversation Interaction"},
					"summary":     "Send a message and get the AI reply (Eino ADK single agent, SSE)",
					"description": "Send a message to the AI and get a streaming reply (SSE). Executed by the Eino **single-agent** ADK; event types match the multi-agent streaming ones (including `tool_call` / `response_delta` / `thinking`, etc.). `response_start` / `response_delta` are only candidate/process output; only an event with `type: response` and `data.finalized=true` represents a successful final reply. When completed execution evidence is missing, a `finalization_auto_continue` event may be sent first, meaning the server-side continued the run from an existing trace without injection. When `data.finalized=false`, message is an incomplete/blocked explanation. **Does not depend on** `multi_agent.enabled`.",
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
							"description": "text/event-stream (SSE)",
							"content": map[string]interface{}{
								"text/event-stream": map[string]interface{}{
									"schema": map[string]interface{}{
										"type":        "string",
										"description": "SSE stream. The terminal response event data contains finalized, finalizable, status, completionReason, evidenceVerified, evidenceRefs, pendingExecutionIds, missingChecks; process events may contain finalization_auto_continue.",
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},
			"/api/multi-agent": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Conversation Interaction"},
					"summary":     "Send a message and get the AI reply (Eino multi-agent, non-streaming)",
					"description": "Same request body as `POST /api/eino-agent`, but executed by the **CloudWeGo Eino** multi-agent. Orchestration is specified by the request body's `orchestration` (`deep` | `plan_execute` | `supervisor`), defaulting to `deep`. **Prerequisite**: `multi_agent.enabled: true`; when not enabled it returns 404 JSON. Supports `webshellConnectionId`.",
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
											"description": "The message to send (required)",
										},
										"conversationId": map[string]interface{}{
											"type":        "string",
											"description": "Conversation ID (optional; a new one is created if not provided)",
										},
										"role": map[string]interface{}{
											"type":        "string",
											"description": "Role name (optional)",
										},
										"webshellConnectionId": map[string]interface{}{
											"type":        "string",
											"description": "WebShell connection ID (optional, consistent with Eino single/multi-agent streaming behavior)",
										},
										"finalization": finalizationRequestSchema,
										"orchestration": map[string]interface{}{
											"type":        "string",
											"description": "Eino preset orchestration: deep | plan_execute | supervisor; default deep",
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
							"description": "Success. Only finalized=true indicates a successful final reply; when finalized=false, response is a block/incomplete explanation.",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{"$ref": "#/components/schemas/AgentChatResponse"},
								},
							},
						},
						"400": map[string]interface{}{"description": "Bad request"},
						"401": map[string]interface{}{"description": "Unauthorized"},
						"404": map[string]interface{}{"description": "Multi-agent not enabled or conversation not found"},
						"500": map[string]interface{}{"description": "Execution failed"},
					},
				},
			},
			"/api/multi-agent/stream": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Conversation Interaction"},
					"summary":     "Send a message and get the AI reply (Eino multi-agent, SSE)",
					"description": "Similar to `POST /api/eino-agent/stream`, but executed by the Eino multi-agent. `orchestration` specifies deep / plan_execute / supervisor, defaulting to deep. `response_start` / `response_delta` are only candidate/process output; only an event with `type: response` and `data.finalized=true` represents a successful final reply. When completed execution evidence is missing, a `finalization_auto_continue` event may be sent first, meaning the server-side continued the run from an existing trace without injection. **Prerequisite**: `multi_agent.enabled: true`; when not enabled, the first SSE entry is `type: error` followed by `done`. Supports `webshellConnectionId`.",
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
											"description": "deep | plan_execute | supervisor; default deep",
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
							"description": "text/event-stream (SSE)",
							"content": map[string]interface{}{
								"text/event-stream": map[string]interface{}{
									"schema": map[string]interface{}{
										"type":        "string",
										"description": "SSE stream. The terminal response event data contains finalized, finalizable, status, completionReason, evidenceVerified, evidenceRefs, pendingExecutionIds, missingChecks; process events may contain finalization_auto_continue.",
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},
			"/api/agent-loop/cancel": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Conversation Interaction"},
					"summary":     "Cancel task",
					"description": "Cancel a currently executing agent-loop task",
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
							"description": "Cancel request submitted",
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
												"description": "Conversation ID",
											},
											"message": map[string]interface{}{
												"type":    "string",
												"example": "The cancel request has been submitted; the task will stop after the current step completes.",
											},
										},
									},
								},
							},
						},
						"404": map[string]interface{}{
							"description": "No running task found",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/agent-loop/tasks": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Conversation Interaction"},
					"summary":     "List running tasks",
					"description": "Get all currently running agent-loop tasks",
					"operationId": "listAgentTasks",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"tasks": map[string]interface{}{
												"type":        "array",
												"description": "Task list",
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
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/agent-loop/tasks/completed": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Conversation Interaction"},
					"summary":     "List completed tasks",
					"description": "Get the history of recently completed agent-loop tasks",
					"operationId": "listCompletedTasks",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"tasks": map[string]interface{}{
												"type":        "array",
												"description": "List of completed tasks",
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
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/batch-tasks": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Batch Tasks"},
					"summary":     "Create batch task queue",
					"description": "Create a batch task queue containing multiple tasks",
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
							"description": "Created successfully",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"queueId": map[string]interface{}{
												"type":        "string",
												"description": "Queue ID",
											},
											"queue": map[string]interface{}{
												"$ref": "#/components/schemas/BatchQueue",
											},
											"started": map[string]interface{}{
												"type":        "boolean",
												"description": "Whether execution was started immediately",
											},
										},
									},
								},
							},
						},
						"400": map[string]interface{}{
							"description": "Bad request",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
				"get": map[string]interface{}{
					"tags":        []string{"Batch Tasks"},
					"summary":     "List batch task queues",
					"description": "Get all batch task queues",
					"operationId": "listBatchQueues",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"queues": map[string]interface{}{
												"type":        "array",
												"description": "Queue list",
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
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/batch-tasks/{queueId}": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Batch Tasks"},
					"summary":     "Get batch task queue",
					"description": "Get the details of the specified batch task queue",
					"operationId": "getBatchQueue",
					"parameters": []map[string]interface{}{
						{
							"name":        "queueId",
							"in":          "path",
							"required":    true,
							"description": "Queue ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/BatchQueue",
									},
								},
							},
						},
						"404": map[string]interface{}{
							"description": "Queue not found",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
				"delete": map[string]interface{}{
					"tags":        []string{"Batch Tasks"},
					"summary":     "Delete batch task queue",
					"description": "Delete the specified batch task queue",
					"operationId": "deleteBatchQueue",
					"parameters": []map[string]interface{}{
						{
							"name":        "queueId",
							"in":          "path",
							"required":    true,
							"description": "Queue ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Deleted successfully",
						},
						"404": map[string]interface{}{
							"description": "Queue not found",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/batch-tasks/{queueId}/start": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Batch Tasks"},
					"summary":     "Start batch task queue",
					"description": "Start executing the tasks in a batch task queue",
					"operationId": "startBatchQueue",
					"parameters": []map[string]interface{}{
						{
							"name":        "queueId",
							"in":          "path",
							"required":    true,
							"description": "Queue ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Started successfully",
						},
						"404": map[string]interface{}{
							"description": "Queue not found",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/batch-tasks/{queueId}/pause": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Batch Tasks"},
					"summary":     "Pause batch task queue",
					"description": "Pause a currently executing batch task queue",
					"operationId": "pauseBatchQueue",
					"parameters": []map[string]interface{}{
						{
							"name":        "queueId",
							"in":          "path",
							"required":    true,
							"description": "Queue ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Paused successfully",
						},
						"404": map[string]interface{}{
							"description": "Queue not found",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/batch-tasks/{queueId}/tasks": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Batch Tasks"},
					"summary":     "Add task to queue",
					"description": "Add a new task to a batch task queue. Tasks are appended to the end of the queue and executed in queue order. Each task creates an independent conversation with full status tracking.\n**Task format**:\nTask content is a string describing the security testing task to execute, for example:\n- \"Scan http://example.com for SQL injection vulnerabilities\"\n- \"Run a port scan against 192.168.1.1\"\n- \"Detect XSS vulnerabilities on https://target.com\"\n**Usage example**:\n```json\n{\n  \"task\": \"Scan http://example.com for SQL injection vulnerabilities\"\n}\n```",
					"operationId": "addBatchTask",
					"parameters": []map[string]interface{}{
						{
							"name":        "queueId",
							"in":          "path",
							"required":    true,
							"description": "Queue ID",
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
											"description": "Task content, describing the security testing task to execute (required)",
											"example":     "Scan http://example.com for SQL injection vulnerabilities",
										},
									},
								},
								"examples": map[string]interface{}{
									"sqlInjection": map[string]interface{}{
										"summary":     "SQL injection scan",
										"description": "Scan the target website for SQL injection vulnerabilities",
										"value": map[string]interface{}{
											"task": "Scan http://example.com for SQL injection vulnerabilities",
										},
									},
									"portScan": map[string]interface{}{
										"summary":     "Port scan",
										"description": "Run a port scan against the target IP",
										"value": map[string]interface{}{
											"task": "Run a port scan against 192.168.1.1",
										},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Added successfully",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"taskId": map[string]interface{}{
												"type":        "string",
												"description": "The newly added task ID",
											},
											"message": map[string]interface{}{
												"type":        "string",
												"description": "Success message",
												"example":     "Task added to the queue",
											},
										},
									},
								},
							},
						},
						"400": map[string]interface{}{
							"description": "Bad request (e.g. task is empty)",
						},
						"404": map[string]interface{}{
							"description": "Queue not found",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/batch-tasks/{queueId}/tasks/{taskId}": map[string]interface{}{
				"put": map[string]interface{}{
					"tags":        []string{"Batch Tasks"},
					"summary":     "Update batch task",
					"description": "Update the specified task in a batch task queue",
					"operationId": "updateBatchTask",
					"parameters": []map[string]interface{}{
						{
							"name":        "queueId",
							"in":          "path",
							"required":    true,
							"description": "Queue ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
						{
							"name":        "taskId",
							"in":          "path",
							"required":    true,
							"description": "Task ID",
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
											"description": "Task content",
										},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Updated successfully",
						},
						"404": map[string]interface{}{
							"description": "Task not found",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
				"delete": map[string]interface{}{
					"tags":        []string{"Batch Tasks"},
					"summary":     "Delete batch task",
					"description": "Delete the specified task from a batch task queue",
					"operationId": "deleteBatchTask",
					"parameters": []map[string]interface{}{
						{
							"name":        "queueId",
							"in":          "path",
							"required":    true,
							"description": "Queue ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
						{
							"name":        "taskId",
							"in":          "path",
							"required":    true,
							"description": "Task ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Deleted successfully",
						},
						"404": map[string]interface{}{
							"description": "Task not found",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/assets/import": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Asset Management"},
					"summary":     "Bulk import assets",
					"description": "Create new assets or deduplicate/update assets by \"target + port + protocol\". Receives JSON, not XLSX/CSV files directly; up to 100000 items per call, requires the asset:write permission.",
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
							"description": "Import completed",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{"$ref": "#/components/schemas/AssetImportResult"},
								},
							},
						},
						"400": map[string]interface{}{"description": "Count or asset field validation failed"},
						"401": map[string]interface{}{"description": "Unauthorized"},
						"403": map[string]interface{}{"description": "Missing asset:write permission or no access to the specified project"},
						"500": map[string]interface{}{"description": "Import transaction failed"},
					},
				},
			},
			"/api/projects": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Project Management"},
					"summary":     "List projects",
					"operationId": "listProjects",
					"parameters": []map[string]interface{}{
						{"name": "status", "in": "query", "schema": map[string]interface{}{"type": "string", "enum": []string{"active", "archived"}}},
						{"name": "limit", "in": "query", "schema": map[string]interface{}{"type": "integer", "default": 200}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Project list"},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
				"post": map[string]interface{}{
					"tags":        []string{"Project Management"},
					"summary":     "Create project",
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
						"200": map[string]interface{}{"description": "Created successfully"},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},
			"/api/projects/{id}": map[string]interface{}{
				"get": map[string]interface{}{
					"tags": []string{"Project Management"}, "summary": "Get project", "operationId": "getProject",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{"200": map[string]interface{}{"description": "Project detail"}},
				},
				"put": map[string]interface{}{
					"tags": []string{"Project Management"}, "summary": "Update project", "operationId": "updateProject",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{"200": map[string]interface{}{"description": "Updated successfully"}},
				},
				"delete": map[string]interface{}{
					"tags": []string{"Project Management"}, "summary": "Delete project", "operationId": "deleteProject",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{"200": map[string]interface{}{"description": "Deleted successfully"}},
				},
			},
			"/api/projects/{id}/facts": map[string]interface{}{
				"get": map[string]interface{}{
					"tags": []string{"Project Management"}, "summary": "List facts or get by key", "operationId": "listProjectFacts",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "schema": map[string]interface{}{"type": "string"}},
						{"name": "fact_key", "in": "query", "schema": map[string]interface{}{"type": "string"}},
						{"name": "include_links", "in": "query", "schema": map[string]interface{}{"type": "boolean"}},
						{"name": "include_link_counts", "in": "query", "schema": map[string]interface{}{"type": "boolean"}},
					},
					"responses": map[string]interface{}{"200": map[string]interface{}{"description": "Fact list or single item (may include link_counts / outgoing_links)"}},
				},
				"post": map[string]interface{}{
					"tags": []string{"Project Management"}, "summary": "Create/update fact", "operationId": "upsertProjectFactREST",
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
										"links_text": map[string]interface{}{"type": "string", "description": "One per line, format: fact_key"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{"200": map[string]interface{}{"description": "Success"}},
				},
			},
			"/api/projects/{id}/fact-graph": map[string]interface{}{
				"get": map[string]interface{}{
					"tags": []string{"Project Management"}, "summary": "Get project fact attack-path graph", "operationId": "getProjectFactGraph",
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
					"tags": []string{"Project Management"}, "summary": "List all project fact edges", "operationId": "listProjectFactEdges",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{"200": map[string]interface{}{"description": "Edge list"}},
				},
				"post": map[string]interface{}{
					"tags": []string{"Project Management"}, "summary": "Add fact edge", "operationId": "createProjectFactEdge",
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
					"responses": map[string]interface{}{"200": map[string]interface{}{"description": "Edge created"}},
				},
			},
			"/api/projects/{id}/fact-edges/{edgeId}": map[string]interface{}{
				"delete": map[string]interface{}{
					"tags": []string{"Project Management"}, "summary": "Delete fact edge", "operationId": "deleteProjectFactEdge",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "schema": map[string]interface{}{"type": "string"}},
						{"name": "edgeId", "in": "path", "required": true, "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{"200": map[string]interface{}{"description": "Deleted successfully"}},
				},
			},
			"/api/projects/{id}/promote-attack-chain/{conversationId}": map[string]interface{}{
				"post": map[string]interface{}{
					"tags": []string{"Project Management"}, "summary": "Promote conversation attack chain into the project fact graph", "operationId": "promoteAttackChainToProject",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "schema": map[string]interface{}{"type": "string"}},
						{"name": "conversationId", "in": "path", "required": true, "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{"200": map[string]interface{}{"description": "Promotion result (facts/edges/graph)"}},
				},
			},
			"/api/vulnerabilities": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Vulnerability Management"},
					"summary":     "List vulnerabilities",
					"description": "Get the vulnerability list, supporting pagination and filtering",
					"operationId": "listVulnerabilities",
					"parameters": []map[string]interface{}{
						{
							"name":        "limit",
							"in":          "query",
							"required":    false,
							"description": "Page size",
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
							"description": "Offset",
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
							"description": "Page number (choose one of page or offset)",
							"schema": map[string]interface{}{
								"type":    "integer",
								"minimum": 1,
							},
						},
						{
							"name":        "id",
							"in":          "query",
							"required":    false,
							"description": "Vulnerability ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
						{
							"name":        "conversation_id",
							"in":          "query",
							"required":    false,
							"description": "Conversation ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
						{
							"name":        "project_id",
							"in":          "query",
							"required":    false,
							"description": "Project ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
						{
							"name":        "severity",
							"in":          "query",
							"required":    false,
							"description": "Severity",
							"schema": map[string]interface{}{
								"type": "string",
								"enum": []string{"critical", "high", "medium", "low", "info"},
							},
						},
						{
							"name":        "status",
							"in":          "query",
							"required":    false,
							"description": "Status",
							"schema": map[string]interface{}{
								"type": "string",
								"enum": []string{"open", "closed", "fixed"},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/ListVulnerabilitiesResponse",
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
				"post": map[string]interface{}{
					"tags":        []string{"Vulnerability Management"},
					"summary":     "Create vulnerability",
					"description": "Create a new vulnerability record",
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
							"description": "Created successfully",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/Vulnerability",
									},
								},
							},
						},
						"400": map[string]interface{}{
							"description": "Bad request",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/vulnerabilities/stats": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Vulnerability Management"},
					"summary":     "Get vulnerability statistics",
					"description": "Get vulnerability statistics",
					"operationId": "getVulnerabilityStats",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/VulnerabilityStats",
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/vulnerabilities/{id}": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Vulnerability Management"},
					"summary":     "Get vulnerability",
					"description": "Get the details of the specified vulnerability",
					"operationId": "getVulnerability",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "Vulnerability ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/Vulnerability",
									},
								},
							},
						},
						"404": map[string]interface{}{
							"description": "Vulnerability not found",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
				"put": map[string]interface{}{
					"tags":        []string{"Vulnerability Management"},
					"summary":     "Update vulnerability",
					"description": "Update vulnerability information",
					"operationId": "updateVulnerability",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "Vulnerability ID",
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
							"description": "Updated successfully",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/Vulnerability",
									},
								},
							},
						},
						"400": map[string]interface{}{
							"description": "Bad request",
						},
						"404": map[string]interface{}{
							"description": "Vulnerability not found",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
				"delete": map[string]interface{}{
					"tags":        []string{"Vulnerability Management"},
					"summary":     "Delete vulnerability",
					"description": "Delete the specified vulnerability",
					"operationId": "deleteVulnerability",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "Vulnerability ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Deleted successfully",
						},
						"404": map[string]interface{}{
							"description": "Vulnerability not found",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/roles": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Role Management"},
					"summary":     "List roles",
					"description": "Get all security testing roles",
					"operationId": "getRoles",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"roles": map[string]interface{}{
												"type":        "array",
												"description": "Role list",
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
							"description": "Unauthorized",
						},
					},
				},
				"post": map[string]interface{}{
					"tags":        []string{"Role Management"},
					"summary":     "Create role",
					"description": "Create a new security testing role",
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
							"description": "Created successfully",
						},
						"400": map[string]interface{}{
							"description": "Bad request",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/roles/{name}": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Role Management"},
					"summary":     "Get role",
					"description": "Get the details of the specified role",
					"operationId": "getRole",
					"parameters": []map[string]interface{}{
						{
							"name":        "name",
							"in":          "path",
							"required":    true,
							"description": "Role name",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
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
							"description": "Role not found",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
				"put": map[string]interface{}{
					"tags":        []string{"Role Management"},
					"summary":     "Update role",
					"description": "Update the configuration of the specified role",
					"operationId": "updateRole",
					"parameters": []map[string]interface{}{
						{
							"name":        "name",
							"in":          "path",
							"required":    true,
							"description": "Role name",
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
							"description": "Updated successfully",
						},
						"400": map[string]interface{}{
							"description": "Bad request",
						},
						"404": map[string]interface{}{
							"description": "Role not found",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
				"delete": map[string]interface{}{
					"tags":        []string{"Role Management"},
					"summary":     "Delete role",
					"description": "Delete the specified role",
					"operationId": "deleteRole",
					"parameters": []map[string]interface{}{
						{
							"name":        "name",
							"in":          "path",
							"required":    true,
							"description": "Role name",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Deleted successfully",
						},
						"404": map[string]interface{}{
							"description": "Role not found",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/skills": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Skills Management"},
					"summary":     "List skills",
					"description": "Get the list of all skills, supporting pagination and search",
					"operationId": "getSkills",
					"parameters": []map[string]interface{}{
						{
							"name":        "limit",
							"in":          "query",
							"required":    false,
							"description": "Page size",
							"schema": map[string]interface{}{
								"type":    "integer",
								"default": 20,
							},
						},
						{
							"name":        "offset",
							"in":          "query",
							"required":    false,
							"description": "Offset",
							"schema": map[string]interface{}{
								"type":    "integer",
								"default": 0,
							},
						},
						{
							"name":        "search",
							"in":          "query",
							"required":    false,
							"description": "Search keyword",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"skills": map[string]interface{}{
												"type":        "array",
												"description": "Skill list",
												"items": map[string]interface{}{
													"$ref": "#/components/schemas/Skill",
												},
											},
											"total": map[string]interface{}{
												"type":        "integer",
												"description": "Total",
											},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
				"post": map[string]interface{}{
					"tags":        []string{"Skills Management"},
					"summary":     "Create skill",
					"description": "Create a new skill",
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
							"description": "Created successfully",
						},
						"400": map[string]interface{}{
							"description": "Bad request",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/skills/stats": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Skills Management"},
					"summary":     "Get skill statistics",
					"description": "Get skill call statistics",
					"operationId": "getSkillStats",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type":        "object",
										"description": "Statistics information",
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
				"delete": map[string]interface{}{
					"tags":        []string{"Skills Management"},
					"summary":     "Clear skill statistics",
					"description": "Clear the call statistics of all skills",
					"operationId": "clearSkillStats",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Clear successful",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/skills/{name}": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Skills Management"},
					"summary":     "Get skill",
					"description": "Get the details of the specified skill",
					"operationId": "getSkill",
					"parameters": []map[string]interface{}{
						{
							"name":        "name",
							"in":          "path",
							"required":    true,
							"description": "Skill name",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/Skill",
									},
								},
							},
						},
						"404": map[string]interface{}{
							"description": "Skill not found",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
				"put": map[string]interface{}{
					"tags":        []string{"Skills Management"},
					"summary":     "Update skill",
					"description": "Update the information of the specified skill",
					"operationId": "updateSkill",
					"parameters": []map[string]interface{}{
						{
							"name":        "name",
							"in":          "path",
							"required":    true,
							"description": "Skill name",
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
							"description": "Updated successfully",
						},
						"400": map[string]interface{}{
							"description": "Bad request",
						},
						"404": map[string]interface{}{
							"description": "Skill not found",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
				"delete": map[string]interface{}{
					"tags":        []string{"Skills Management"},
					"summary":     "Delete skill",
					"description": "Delete the specified skill",
					"operationId": "deleteSkill",
					"parameters": []map[string]interface{}{
						{
							"name":        "name",
							"in":          "path",
							"required":    true,
							"description": "Skill name",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Deleted successfully",
						},
						"404": map[string]interface{}{
							"description": "Skill not found",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/skills/{name}/bound-roles": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Skills Management"},
					"summary":     "Get bound roles",
					"description": "Get all roles that use the specified skill",
					"operationId": "getSkillBoundRoles",
					"parameters": []map[string]interface{}{
						{
							"name":        "name",
							"in":          "path",
							"required":    true,
							"description": "Skill name",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"roles": map[string]interface{}{
												"type":        "array",
												"description": "Role list",
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
							"description": "Skill not found",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/skills/{name}/stats": map[string]interface{}{
				"delete": map[string]interface{}{
					"tags":        []string{"Skills Management"},
					"summary":     "Clear skill statistics",
					"description": "Clear the call statistics of the specified skill",
					"operationId": "clearSkillStatsByName",
					"parameters": []map[string]interface{}{
						{
							"name":        "name",
							"in":          "path",
							"required":    true,
							"description": "Skill name",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Clear successful",
						},
						"404": map[string]interface{}{
							"description": "Skill not found",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/monitor": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Monitoring"},
					"summary":     "Get monitoring information",
					"description": "Get tool execution monitoring information, supporting pagination and filtering",
					"operationId": "monitor",
					"parameters": []map[string]interface{}{
						{
							"name":        "page",
							"in":          "query",
							"required":    false,
							"description": "Page number",
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
							"description": "Page size",
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
							"description": "Status filter",
							"schema": map[string]interface{}{
								"type": "string",
								"enum": []string{"queued", "running", "completed", "failed", "cancelled", "hard_timeout", "orphaned"},
							},
						},
						{
							"name":        "tool",
							"in":          "query",
							"required":    false,
							"description": "Tool name filter (supports partial match)",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/MonitorResponse",
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/monitor/execution/{id}": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Monitoring"},
					"summary":     "Get execution record",
					"description": "Get the details of the specified execution record",
					"operationId": "getExecution",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "Execution ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/ToolExecution",
									},
								},
							},
						},
						"404": map[string]interface{}{
							"description": "Execution record not found",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
				"delete": map[string]interface{}{
					"tags":        []string{"Monitoring"},
					"summary":     "Delete execution record",
					"description": "Delete the specified execution record",
					"operationId": "deleteExecution",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "Execution ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Deleted successfully",
						},
						"404": map[string]interface{}{
							"description": "Execution record not found",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/monitor/execution/{id}/cancel": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Monitoring"},
					"summary":     "Cancel in-progress tool execution",
					"description": "Send a context cancellation signal to the MCP tool call currently executing in this process; the parent conversation or multi-step task can continue. If execution has already ended or is not running in this process, returns 404.",
					"operationId": "cancelExecution",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "Execution ID",
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
											"description": "Optional. When non-empty, it is merged with the tool's already-returned output before being given to the model, with a \"user termination note\" header block so it can be distinguished from the raw command-line output",
										},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Termination signal sent",
						},
						"400": map[string]interface{}{
							"description": "Request body is not valid JSON",
						},
						"404": map[string]interface{}{
							"description": "No in-progress tool execution found",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/monitor/executions": map[string]interface{}{
				"delete": map[string]interface{}{
					"tags":        []string{"Monitoring"},
					"summary":     "Batch delete execution records",
					"description": "Batch delete execution records",
					"operationId": "deleteExecutions",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Deleted successfully",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/monitor/stats": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Monitoring"},
					"summary":     "Get statistics",
					"description": "Get tool execution statistics",
					"operationId": "getStats",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type":        "object",
										"description": "Statistics information",
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/system/update/source": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"System Update"},
					"summary":     "Configure update source (which repository this installation updates from)",
					"description": "Write the update source into the update section of config.yaml: a single repo field (repository address), and updates follow that repository's default branch. Leaving it empty (or writing an empty string) reverts to the official repository https://github.com/AIPentest/CyberStrikeAI.git. The address whitelist is https/http/ssh/git/file:// plus local absolute paths (git's ext:: transport executes commands and is rejected outright).",
					"operationId": "saveUpdateSource",
					"requestBody": map[string]interface{}{
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"repo": map[string]interface{}{"type": "string", "description": "Update source repository address; empty = official repository"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Saved (returns the config value and the official default address)"},
						"400": map[string]interface{}{"description": "Invalid repository address"},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},
			"/api/system/update/adopt": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"System Update"},
					"summary":     "Adopt a non-git directory into the update source",
					"description": "Unpack the directory containing the installation and adopt it into the configured update source (unconfigured = official repository): without confirm it is a side-effect-free preview (fetch in a temporary repository, listing the files to be replaced by the target version and the operator content to be kept); with confirm=true it executes asynchronously (202 + job_id, poll /api/system/update/job): git init, add origin, land the update source default branch's content, operator content is stashed first and restored afterwards, and every replaced file is backed up to .update-backup/<timestamp>/overwritten/. If the directory is already a git working tree it returns 409 (use the one-click update instead of adoption).",
					"operationId": "adoptUpdateSource",
					"requestBody": map[string]interface{}{
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"confirm": map[string]interface{}{"type": "boolean", "description": "false/omitted = preview only; true = confirm execution"},
										"restart": map[string]interface{}{"type": "boolean", "description": "After successful adoption, exit the process for an external supervisor to restart; default false"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Preview result (plan: the list and count of files to be replaced/kept)"},
						"202": map[string]interface{}{"description": "Adopt task accepted, returns job_id"},
						"400": map[string]interface{}{"description": "Invalid update source address, or restart required but no restart hook"},
						"409": map[string]interface{}{"description": "The directory is already a git working tree, or a task is in progress"},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},
			"/api/config": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Configuration Management"},
					"summary":     "Get configuration",
					"description": "Get system configuration information",
					"operationId": "getConfig",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/ConfigResponse",
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
				"put": map[string]interface{}{
					"tags":        []string{"Configuration Management"},
					"summary":     "Update configuration",
					"description": "Update system configuration",
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
							"description": "Updated successfully",
						},
						"400": map[string]interface{}{
							"description": "Bad request",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/storage/meta": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Storage Cleanup"},
					"summary":     "Get storage cleanup policy",
					"description": "Returns the auto-cleanup toggle, interval, grace window, and every cleanup category's enabled state and retention days",
					"operationId": "getStorageMeta",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
						"403": map[string]interface{}{
							"description": "Missing storage:read permission",
						},
					},
				},
			},
			"/api/storage/status": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Storage Cleanup"},
					"summary":     "Get runtime storage usage",
					"description": "Returns file system capacity (including inodes), per-category usage and reclaimable amounts; results are cached with a short TTL",
					"operationId": "getStorageStatus",
					"parameters": []interface{}{
						map[string]interface{}{
							"name":        "refresh",
							"in":          "query",
							"description": "When true, forces re-traversal of the directory, bypassing the cache",
							"required":    false,
							"schema":      map[string]interface{}{"type": "boolean"},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
						"403": map[string]interface{}{
							"description": "Missing storage:read permission",
						},
					},
				},
			},
			"/api/storage/cleanup": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Storage Cleanup"},
					"summary":     "Preview or run storage cleanup",
					"description": "By default dry_run=true only counts without deleting; a real deletion must pass both dry_run=false and confirm=true. Only enabled categories are processed, and only one run is allowed at a time",
					"operationId": "runStorageCleanup",
					"requestBody": map[string]interface{}{
						"required": false,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"dry_run": map[string]interface{}{
											"type":        "boolean",
											"description": "Treated as true when omitted",
										},
										"confirm": map[string]interface{}{
											"type":        "boolean",
											"description": "Must be true when dry_run=false",
										},
										"categories": map[string]interface{}{
											"type":        "array",
											"description": "Restricted categories; omitted means all enabled categories",
											"items":       map[string]interface{}{"type": "string"},
										},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Execution completed (or preview completed)",
						},
						"400": map[string]interface{}{
							"description": "Missing confirm, or the category key is not registered",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
						"403": map[string]interface{}{
							"description": "Missing storage:write permission",
						},
						"409": map[string]interface{}{
							"description": "A cleanup run is already in progress",
						},
					},
				},
			},
			"/api/config/tools": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Configuration Management"},
					"summary":     "Get tool configuration",
					"description": "Get the configuration of all tools",
					"operationId": "getTools",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type":        "array",
										"description": "Tool configuration list",
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/config/apply": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Configuration Management"},
					"summary":     "Apply configuration",
					"description": "Apply configuration changes",
					"operationId": "applyConfig",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Applied successfully",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/external-mcp": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"External MCP Management"},
					"summary":     "List external MCP",
					"description": "Get all external MCP configurations and statuses",
					"operationId": "getExternalMCPs",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"servers": map[string]interface{}{
												"type":        "object",
												"description": "MCP server configuration",
												"additionalProperties": map[string]interface{}{
													"$ref": "#/components/schemas/ExternalMCPResponse",
												},
											},
											"stats": map[string]interface{}{
												"type":        "object",
												"description": "Statistics information",
											},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/external-mcp/stats": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"External MCP Management"},
					"summary":     "Get external MCP statistics",
					"description": "Get external MCP statistics",
					"operationId": "getExternalMCPStats",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type":        "object",
										"description": "Statistics information",
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/external-mcp/{name}": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"External MCP Management"},
					"summary":     "Get external MCP",
					"description": "Get the configuration and status of the specified external MCP",
					"operationId": "getExternalMCP",
					"parameters": []map[string]interface{}{
						{
							"name":        "name",
							"in":          "path",
							"required":    true,
							"description": "MCP name",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/ExternalMCPResponse",
									},
								},
							},
						},
						"404": map[string]interface{}{
							"description": "MCP not found",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
				"put": map[string]interface{}{
					"tags":        []string{"External MCP Management"},
					"summary":     "Add or update external MCP",
					"description": "Add a new external MCP configuration or update an existing one.\n**Transport**:\nTwo transports are supported:\n**1. stdio (standard input/output)**:\n```json\n{\n  \"config\": {\n    \"enabled\": true,\n    \"command\": \"node\",\n    \"args\": [\"/path/to/mcp-server.js\"],\n    \"env\": {}\n  }\n}\n```\n**2. sse (Server-Sent Events)**:\n```json\n{\n  \"config\": {\n    \"enabled\": true,\n    \"transport\": \"sse\",\n    \"url\": \"http://127.0.0.1:8082/sse\",\n    \"timeout\": 30\n  }\n}\n```\n**Configuration parameters**:\n- `enabled`: Whether enabled (boolean, required)\n- `command`: Command (required for stdio mode, e.g. \"node\", \"python\")\n- `args`: Command argument array (required for stdio mode)\n- `env`: Environment variables (object, optional)\n- `transport`: Transport (\"stdio\" or \"sse\", required for sse mode)\n- `url`: SSE endpoint URL (required for sse mode)\n- `timeout`: Timeout in seconds (optional, default 30)\n- `description`: Description (optional)",
					"operationId": "addOrUpdateExternalMCP",
					"parameters": []map[string]interface{}{
						{
							"name":        "name",
							"in":          "path",
							"required":    true,
							"description": "MCP name (unique identifier)",
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
										"summary":     "stdio mode configuration",
										"description": "Connect to the external MCP server over standard input/output",
										"value": map[string]interface{}{
											"config": map[string]interface{}{
												"enabled":     true,
												"command":     "node",
												"args":        []string{"/path/to/mcp-server.js"},
												"env":         map[string]interface{}{},
												"timeout":     30,
												"description": "Node.js MCP server",
											},
										},
									},
									"sse": map[string]interface{}{
										"summary":     "SSE mode configuration",
										"description": "Connect to the external MCP server over Server-Sent Events",
										"value": map[string]interface{}{
											"config": map[string]interface{}{
												"enabled":     true,
												"transport":   "sse",
												"url":         "http://127.0.0.1:8082/sse",
												"timeout":     30,
												"description": "SSE MCP server",
											},
										},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Operation successful",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"message": map[string]interface{}{
												"type":    "string",
												"example": "External MCP configuration saved",
											},
										},
									},
								},
							},
						},
						"400": map[string]interface{}{
							"description": "Bad request (e.g. invalid config format or missing required fields)",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/Error",
									},
									"example": map[string]interface{}{
										"error": i18nText(requestWantsEnglish(c), "stdio mode requires the command and args parameters", "stdio mode requires the command and args parameters"),
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
				"delete": map[string]interface{}{
					"tags":        []string{"External MCP Management"},
					"summary":     "Delete external MCP",
					"description": "Delete the specified external MCP configuration",
					"operationId": "deleteExternalMCP",
					"parameters": []map[string]interface{}{
						{
							"name":        "name",
							"in":          "path",
							"required":    true,
							"description": "MCP name",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Deleted successfully",
						},
						"404": map[string]interface{}{
							"description": "MCP not found",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/external-mcp/{name}/start": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"External MCP Management"},
					"summary":     "Start external MCP",
					"description": "Start the specified external MCP server",
					"operationId": "startExternalMCP",
					"parameters": []map[string]interface{}{
						{
							"name":        "name",
							"in":          "path",
							"required":    true,
							"description": "MCP name",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Started successfully",
						},
						"404": map[string]interface{}{
							"description": "MCP not found",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/external-mcp/{name}/stop": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"External MCP Management"},
					"summary":     "Stop external MCP",
					"description": "Stop the specified external MCP server",
					"operationId": "stopExternalMCP",
					"parameters": []map[string]interface{}{
						{
							"name":        "name",
							"in":          "path",
							"required":    true,
							"description": "MCP name",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Stopped successfully",
						},
						"404": map[string]interface{}{
							"description": "MCP not found",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/attack-chain/{conversationId}": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Attack Chain"},
					"summary":     "Get attack chain",
					"description": "Get the attack chain visualization data of the specified conversation",
					"operationId": "getAttackChain",
					"parameters": []map[string]interface{}{
						{
							"name":        "conversationId",
							"in":          "path",
							"required":    true,
							"description": "Conversation ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/AttackChain",
									},
								},
							},
						},
						"404": map[string]interface{}{
							"description": "Conversation not found",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/attack-chain/{conversationId}/regenerate": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Attack Chain"},
					"summary":     "Regenerate attack chain",
					"description": "Regenerate the attack chain visualization data of the specified conversation",
					"operationId": "regenerateAttackChain",
					"parameters": []map[string]interface{}{
						{
							"name":        "conversationId",
							"in":          "path",
							"required":    true,
							"description": "Conversation ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Regeneration successful",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/AttackChain",
									},
								},
							},
						},
						"404": map[string]interface{}{
							"description": "Conversation not found",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/conversations/{id}/pinned": map[string]interface{}{
				"put": map[string]interface{}{
					"tags":        []string{"Conversation Management"},
					"summary":     "Set conversation pinned state",
					"description": "Set or cancel the pinned state of the conversation",
					"operationId": "updateConversationPinned",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "Conversation ID",
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
											"description": "Whether pinned",
										},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Updated successfully",
						},
						"404": map[string]interface{}{
							"description": "Conversation not found",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/knowledge/categories": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Knowledge Base"},
					"summary":     "Get categories",
					"description": "Get all categories of the knowledge base",
					"operationId": "getKnowledgeCategories",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"categories": map[string]interface{}{
												"type":        "array",
												"description": "Category list",
												"items": map[string]interface{}{
													"type": "string",
												},
											},
											"enabled": map[string]interface{}{
												"type":        "boolean",
												"description": "Whether the knowledge base is enabled",
											},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/knowledge/items": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Knowledge Base"},
					"summary":     "List knowledge items",
					"description": "Get all knowledge items in the knowledge base",
					"operationId": "getKnowledgeItems",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"items": map[string]interface{}{
												"type":        "array",
												"description": "Knowledge item list",
											},
											"enabled": map[string]interface{}{
												"type":        "boolean",
												"description": "Whether the knowledge base is enabled",
											},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
				"post": map[string]interface{}{
					"tags":        []string{"Knowledge Base"},
					"summary":     "Create knowledge item",
					"description": "Create a new knowledge item",
					"operationId": "createKnowledgeItem",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":        "object",
									"description": "Knowledge item data",
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Created successfully",
						},
						"400": map[string]interface{}{
							"description": "Bad request",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/knowledge/items/{id}": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Knowledge Base"},
					"summary":     "Get knowledge item",
					"description": "Get the details of the specified knowledge item",
					"operationId": "getKnowledgeItem",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "Knowledge item ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
						},
						"404": map[string]interface{}{
							"description": "Knowledge item not found",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
				"put": map[string]interface{}{
					"tags":        []string{"Knowledge Base"},
					"summary":     "Update knowledge item",
					"description": "Update the specified knowledge item",
					"operationId": "updateKnowledgeItem",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "Knowledge item ID",
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
									"description": "Knowledge item data",
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Updated successfully",
						},
						"404": map[string]interface{}{
							"description": "Knowledge item not found",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
				"delete": map[string]interface{}{
					"tags":        []string{"Knowledge Base"},
					"summary":     "Delete knowledge item",
					"description": "Delete the specified knowledge item",
					"operationId": "deleteKnowledgeItem",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "Knowledge item ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Deleted successfully",
						},
						"404": map[string]interface{}{
							"description": "Knowledge item not found",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/knowledge/index-status": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Knowledge Base"},
					"summary":     "Get index status",
					"description": "Get the build status of the knowledge base index",
					"operationId": "getIndexStatus",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"enabled": map[string]interface{}{
												"type":        "boolean",
												"description": "Whether the knowledge base is enabled",
											},
											"total_items": map[string]interface{}{
												"type":        "integer",
												"description": "Total knowledge items",
											},
											"indexed_items": map[string]interface{}{
												"type":        "integer",
												"description": "Indexed knowledge items count",
											},
											"progress_percent": map[string]interface{}{
												"type":        "number",
												"description": "Index progress percent",
											},
											"is_complete": map[string]interface{}{
												"type":        "boolean",
												"description": "Whether indexing is complete",
											},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/knowledge/index": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Knowledge Base"},
					"summary":     "Build index",
					"description": "Build the knowledge base vector index. By default only knowledge items without vectors are processed; mode=full rebuilds everything.",
					"operationId": "startKnowledgeIndex",
					"parameters": []map[string]interface{}{
						{
							"name":        "mode",
							"in":          "query",
							"required":    false,
							"description": "Index mode: missing (default, fill in the missing vectors) or full (full rebuild)",
							"schema": map[string]interface{}{
								"type":    "string",
								"enum":    []string{"missing", "full"},
								"default": "missing",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Index task started",
						},
						"400": map[string]interface{}{
							"description": "Invalid mode parameter",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
						"409": map[string]interface{}{
							"description": "An index task is already in progress",
						},
					},
				},
			},
			"/api/knowledge/scan": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Knowledge Base"},
					"summary":     "Scan knowledge base",
					"description": "Scan the knowledge base directory and import new knowledge files",
					"operationId": "scanKnowledgeBase",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Scan task started",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/knowledge/search": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Knowledge Base"},
					"summary":     "Search knowledge base",
					"description": "Search the knowledge base for relevant content. Based on vector retrieval, it returns the most relevant results by semantic similarity (cosine) between the query and knowledge chunks.\n**Search notes**:\n- Semantic similarity search: embedding vectors + cosine similarity, with configurable similarity threshold and TopK\n- Can filter by risk type and other metadata (e.g. SQL injection, XSS, file upload, etc.)\n- Recommended to first call `/api/knowledge/categories` to get the available risk types\n**Usage example**:\n```json\n{\n  \"query\": \"SQL injection vulnerability detection methods\",\n  \"riskType\": \"SQL injection\",\n  \"topK\": 5,\n  \"threshold\": 0.7\n}\n```",
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
											"description": "Search query content, describing the security knowledge topic you want to learn (required)",
											"example":     "SQL injection vulnerability detection methods",
										},
										"riskType": map[string]interface{}{
											"type":        "string",
											"description": "Optional: restrict to a risk type (e.g. SQL injection, XSS, file upload). It is recommended to first call /api/knowledge/categories to get the available risk types, then use the correct risk type for a precise search, which greatly reduces retrieval time. If not specified, all types are searched.",
											"example":     "SQL injection",
										},
										"topK": map[string]interface{}{
											"type":        "integer",
											"description": "Optional: number of Top-K results to return, default 5",
											"default":     5,
											"minimum":     1,
											"maximum":     50,
											"example":     5,
										},
										"threshold": map[string]interface{}{
											"type":        "number",
											"format":      "float",
											"description": "Optional: similarity threshold (between 0-1), default 0.7. Only results with similarity greater than or equal to this value are returned",
											"default":     0.7,
											"minimum":     0,
											"maximum":     1,
											"example":     0.7,
										},
									},
								},
								"examples": map[string]interface{}{
									"basic": map[string]interface{}{
										"summary":     "Basic search",
										"description": "The simplest search, only providing query content",
										"value": map[string]interface{}{
											"query": "SQL injection vulnerability detection methods",
										},
									},
									"withRiskType": map[string]interface{}{
										"summary":     "Search by risk type",
										"description": "Precise search restricted to a risk type",
										"value": map[string]interface{}{
											"query":     "SQL injection vulnerability detection methods",
											"riskType":  "SQL injection",
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
							"description": "Search successful",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"results": map[string]interface{}{
												"type":        "array",
												"description": "Search result list; each result contains: item (knowledge item information), chunks (matched knowledge chunks), score (similarity score)",
												"items": map[string]interface{}{
													"type": "object",
													"properties": map[string]interface{}{
														"item": map[string]interface{}{
															"type":        "object",
															"description": "Knowledge item information",
														},
														"chunks": map[string]interface{}{
															"type":        "array",
															"description": "List of matched knowledge chunks",
														},
														"score": map[string]interface{}{
															"type":        "number",
															"description": "Similarity score (between 0-1)",
														},
													},
												},
											},
											"enabled": map[string]interface{}{
												"type":        "boolean",
												"description": "Whether the knowledge base is enabled",
											},
										},
									},
									"example": map[string]interface{}{
										"results": []map[string]interface{}{
											{
												"item": map[string]interface{}{
													"id":       "item-1",
													"title":    "SQL injection vulnerability detection",
													"category": "SQL injection",
												},
												"chunks": []map[string]interface{}{
													{
														"text": "SQL injection vulnerability detection methods include...",
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
							"description": "Bad request (e.g. query is empty)",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/Error",
									},
									"example": map[string]interface{}{
										"error": i18nText(requestWantsEnglish(c), "Query cannot be empty", "query cannot be empty"),
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
						"500": map[string]interface{}{
							"description": "Internal server error (e.g. knowledge base not enabled or retrieval failed)",
						},
					},
				},
			},
			"/api/knowledge/retrieval-logs": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Knowledge Base"},
					"summary":     "Get retrieval logs",
					"description": "Get knowledge base retrieval logs",
					"operationId": "getRetrievalLogs",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"logs": map[string]interface{}{
												"type":        "array",
												"description": "Retrieval log list",
											},
											"enabled": map[string]interface{}{
												"type":        "boolean",
												"description": "Whether the knowledge base is enabled",
											},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/knowledge/retrieval-logs/{id}": map[string]interface{}{
				"delete": map[string]interface{}{
					"tags":        []string{"Knowledge Base"},
					"summary":     "Delete retrieval log",
					"description": "Delete the specified retrieval log",
					"operationId": "deleteRetrievalLog",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "Log ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Deleted successfully",
						},
						"404": map[string]interface{}{
							"description": "Log not found",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			// ==================== Conversation Interaction - missing endpoints ====================
			"/api/conversations/{id}/delete-turn": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Conversation Interaction"},
					"summary":     "Delete conversation turn",
					"description": "Delete the conversation turn containing the specified message (all messages from that turn's user message up to just before the next turn's user message) and clear the last_react state.",
					"operationId": "deleteConversationTurn",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "Conversation ID",
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
											"description": "Anchor message ID identifying the turn to delete",
										},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Deleted successfully",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"deletedMessageIds": map[string]interface{}{
												"type":        "array",
												"items":       map[string]interface{}{"type": "string"},
												"description": "List of deleted message IDs",
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
						"400": map[string]interface{}{"description": "Bad request or deletion failed"},
						"401": map[string]interface{}{"description": "Unauthorized"},
						"404": map[string]interface{}{"description": "Conversation not found"},
					},
				},
			},
			"/api/messages/{id}/process-details": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Conversation Interaction"},
					"summary":     "Get message process details",
					"description": "Load the execution process details of the specified message with paged loading, including tool calls, thinking processes and other events. Returns 50 by default; pass full=1 explicitly when export or legacy integration needs the full set.",
					"operationId": "getMessageProcessDetails",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "Message ID",
							"schema":      map[string]interface{}{"type": "string"},
						},
						{
							"name":        "summary",
							"in":          "query",
							"required":    false,
							"description": "Only return the process-details summary (total / iterationCount / maxIteration)",
							"schema":      map[string]interface{}{"type": "boolean"},
						},
						{
							"name":        "limit",
							"in":          "query",
							"required":    false,
							"description": "Page size, default 50, maximum 500",
							"schema":      map[string]interface{}{"type": "integer", "default": 50, "maximum": 500},
						},
						{
							"name":        "offset",
							"in":          "query",
							"required":    false,
							"description": "Pagination offset, default 0",
							"schema":      map[string]interface{}{"type": "integer", "default": 0},
						},
						{
							"name":        "full",
							"in":          "query",
							"required":    false,
							"description": "Explicitly return the full process details; only recommended for export or old-integration compatibility",
							"schema":      map[string]interface{}{"type": "boolean"},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
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
														"id":             map[string]interface{}{"type": "string", "description": "Detail record ID"},
														"messageId":      map[string]interface{}{"type": "string", "description": "Owning message ID"},
														"conversationId": map[string]interface{}{"type": "string", "description": "Owning conversation ID"},
														"eventType":      map[string]interface{}{"type": "string", "description": "Event type (e.g. tool_call, thinking)"},
														"message":        map[string]interface{}{"type": "string", "description": "Event message"},
														"data":           map[string]interface{}{"description": "Event extra data (JSON object)"},
														"createdAt":      map[string]interface{}{"type": "string", "format": "date-time", "description": "Created time"},
													},
												},
											},
											"total":   map[string]interface{}{"type": "integer", "description": "Total process details"},
											"offset":  map[string]interface{}{"type": "integer", "description": "Current pagination offset"},
											"limit":   map[string]interface{}{"type": "integer", "description": "Current pagination size"},
											"hasMore": map[string]interface{}{"type": "boolean", "description": "Whether more process details exist"},
											"summary": map[string]interface{}{
												"type":        "object",
												"description": "The summary returned when summary=1",
											},
										},
									},
								},
							},
						},
						"400": map[string]interface{}{"description": "Bad request"},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},

			// ==================== Batch Tasks - missing endpoints ====================
			"/api/batch-tasks/{queueId}/rerun": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Batch Tasks"},
					"summary":     "Rerun batch task queue",
					"description": "Reset a completed or cancelled batch task queue and start executing all tasks again.",
					"operationId": "rerunBatchQueue",
					"parameters": []map[string]interface{}{
						{
							"name":        "queueId",
							"in":          "path",
							"required":    true,
							"description": "Queue ID",
							"schema":      map[string]interface{}{"type": "string"},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Rerun successful",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"message": map[string]interface{}{"type": "string", "example": "Batch tasks started executing again"},
											"queueId": map[string]interface{}{"type": "string", "description": "Queue ID"},
										},
									},
								},
							},
						},
						"400": map[string]interface{}{"description": "Only completed or cancelled queues can be rerun"},
						"401": map[string]interface{}{"description": "Unauthorized"},
						"404": map[string]interface{}{"description": "Queue not found"},
					},
				},
			},
			"/api/batch-tasks/{queueId}/metadata": map[string]interface{}{
				"put": map[string]interface{}{
					"tags":        []string{"Batch Tasks"},
					"summary":     "Update queue metadata",
					"description": "Modify the title, role and agent mode of a batch task queue.",
					"operationId": "updateBatchQueueMetadata",
					"parameters": []map[string]interface{}{
						{
							"name":        "queueId",
							"in":          "path",
							"required":    true,
							"description": "Queue ID",
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
										"title":     map[string]interface{}{"type": "string", "description": "Queue title"},
										"role":      map[string]interface{}{"type": "string", "description": "Role name to use"},
										"agentMode": map[string]interface{}{"type": "string", "description": "Agent mode", "enum": []string{"eino_single", "deep", "plan_execute", "supervisor"}},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Updated successfully",
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
						"400": map[string]interface{}{"description": "Bad request"},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},
			"/api/batch-tasks/{queueId}/schedule": map[string]interface{}{
				"put": map[string]interface{}{
					"tags":        []string{"Batch Tasks"},
					"summary":     "Update queue schedule configuration",
					"description": "Modify the scheduling mode and Cron expression of a batch task queue. Cannot be modified while the queue is running.",
					"operationId": "updateBatchQueueSchedule",
					"parameters": []map[string]interface{}{
						{
							"name":        "queueId",
							"in":          "path",
							"required":    true,
							"description": "Queue ID",
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
										"scheduleMode": map[string]interface{}{"type": "string", "description": "Scheduling mode", "enum": []string{"manual", "cron"}},
										"cronExpr":     map[string]interface{}{"type": "string", "description": "Cron expression (required when scheduleMode is cron)", "example": "0 2 * * *"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Updated successfully",
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
						"400": map[string]interface{}{"description": "Bad request or the queue is currently running"},
						"401": map[string]interface{}{"description": "Unauthorized"},
						"404": map[string]interface{}{"description": "Queue not found"},
					},
				},
			},
			"/api/batch-tasks/{queueId}/schedule-enabled": map[string]interface{}{
				"put": map[string]interface{}{
					"tags":        []string{"Batch Tasks"},
					"summary":     "Toggle Cron auto scheduling",
					"description": "Enable or disable the Cron auto scheduling of a batch task queue; manual execution is unaffected.",
					"operationId": "setBatchQueueScheduleEnabled",
					"parameters": []map[string]interface{}{
						{
							"name":        "queueId",
							"in":          "path",
							"required":    true,
							"description": "Queue ID",
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
										"scheduleEnabled": map[string]interface{}{"type": "boolean", "description": "Whether auto scheduling is enabled"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Set successfully",
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
						"401": map[string]interface{}{"description": "Unauthorized"},
						"404": map[string]interface{}{"description": "Queue not found"},
					},
				},
			},
			// ==================== FOFA Recon ====================
			"/api/fofa/search": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"FOFA Recon"},
					"summary":     "FOFA search",
					"description": "Run a FOFA search query through the backend proxy, returning asset information.",
					"operationId": "fofaSearch",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"query"},
									"properties": map[string]interface{}{
										"query":  map[string]interface{}{"type": "string", "description": "FOFA query syntax", "example": "domain=\"example.com\""},
										"size":   map[string]interface{}{"type": "integer", "description": "Return count (default 100, maximum 10000)", "default": 100},
										"page":   map[string]interface{}{"type": "integer", "description": "Page number (default 1)", "default": 1},
										"fields": map[string]interface{}{"type": "string", "description": "Returned fields, comma-separated", "example": "host,ip,port,title"},
										"full":   map[string]interface{}{"type": "boolean", "description": "Whether to query all data", "default": false},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Search successful",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"query":         map[string]interface{}{"type": "string", "description": "The actually executed query"},
											"size":          map[string]interface{}{"type": "integer"},
											"page":          map[string]interface{}{"type": "integer"},
											"total":         map[string]interface{}{"type": "integer", "description": "Total matches"},
											"fields":        map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
											"results_count": map[string]interface{}{"type": "integer"},
											"results":       map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "object"}, "description": "Search result list"},
										},
									},
								},
							},
						},
						"400": map[string]interface{}{"description": "Bad request"},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},
			"/api/fofa/parse": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"FOFA Recon"},
					"summary":     "Parse natural language into FOFA syntax",
					"description": "Use AI to parse a natural-language description into FOFA query syntax; manual confirmation is required before executing the query.",
					"operationId": "fofaParse",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"text"},
									"properties": map[string]interface{}{
										"text": map[string]interface{}{"type": "string", "description": "Natural language description", "example": "Find websites using WordPress"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Parse successful",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"query":       map[string]interface{}{"type": "string", "description": "Generated FOFA query syntax"},
											"explanation": map[string]interface{}{"type": "string", "description": "Syntax explanation"},
											"warnings":    map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Potential risk or ambiguity hints"},
										},
									},
								},
							},
						},
						"400": map[string]interface{}{"description": "Bad request"},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},

			// ==================== Configuration Management - missing endpoints ====================
			"/api/config/test-vision": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Configuration Management"},
					"summary":     "Test vision model connection",
					"description": "Test whether the Vision model API is usable. When vision.api_key/base_url is empty, the openai section can be passed as a fallback.",
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
											"description": "Main LLM configuration (used for API Key/Base URL fallback when the vision fields are empty)",
										},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Test result",
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
						"400": map[string]interface{}{"description": "Bad request"},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},
			"/api/config/test-openai": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Configuration Management"},
					"summary":     "Test OpenAI API connection",
					"description": "Test whether the specified OpenAI/Claude API configuration is usable, sending a minimal request to verify connectivity.",
					"operationId": "testOpenAI",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"api_key", "model"},
									"properties": map[string]interface{}{
										"provider": map[string]interface{}{"type": "string", "description": "LLM provider (openai/claude)", "example": "openai"},
										"base_url": map[string]interface{}{"type": "string", "description": "API base URL (optional; selected automatically by provider by default)"},
										"api_key":  map[string]interface{}{"type": "string", "description": "API key"},
										"model":    map[string]interface{}{"type": "string", "description": "Model name", "example": "gpt-4"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Test result",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"success":    map[string]interface{}{"type": "boolean", "description": "Whether the connection succeeded"},
											"error":      map[string]interface{}{"type": "string", "description": i18nText(requestWantsEnglish(c), "Failure reason (when success=false)", "failure reason (when success=false)")},
											"model":      map[string]interface{}{"type": "string", "description": "Model actually used (when success=true)"},
											"latency_ms": map[string]interface{}{"type": "number", "description": "Latency in milliseconds (when success=true)"},
										},
									},
								},
							},
						},
						"400": map[string]interface{}{"description": "Bad request"},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},
			"/api/config/test-typesafe": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Configuration Management"},
					"summary":     "Test TypeSafe Jev connection",
					"description": "Send a minimal Noul request to verify that the TypeSafe System One API Key is usable.",
					"operationId": "testTypeSafe",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"api_key"},
									"properties": map[string]interface{}{
										"base_url": map[string]interface{}{"type": "string", "description": "Optional, default https://api.typesafe.ai"},
										"api_key":  map[string]interface{}{"type": "string", "description": "TypeSafe API Key"},
										"model":    map[string]interface{}{"type": "string", "description": "Optional, default jev-latest", "example": "jev-latest"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Test result",
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
						"400": map[string]interface{}{"description": "Bad request"},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},
			"/api/config/list-models": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Configuration Management"},
					"summary":     "List models",
					"description": "Proxies the OpenAI-compatible GET /models, returning the list of available model ids. Not supported for Claude.",
					"operationId": "listModels",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"api_key"},
									"properties": map[string]interface{}{
										"provider": map[string]interface{}{"type": "string", "description": "LLM provider (openai/claude)", "example": "openai"},
										"base_url": map[string]interface{}{"type": "string", "description": "API base URL (optional)"},
										"api_key":  map[string]interface{}{"type": "string", "description": "API key"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Get result",
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
						"400": map[string]interface{}{"description": "Bad request"},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},

			// ==================== Terminal ====================
			"/api/terminal/run": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Terminal"},
					"summary":     "Run terminal command",
					"description": "Execute a shell command on the server and return the result.",
					"operationId": "terminalRun",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"command"},
									"properties": map[string]interface{}{
										"command": map[string]interface{}{"type": "string", "description": "The command to execute"},
										"shell":   map[string]interface{}{"type": "string", "description": "Shell type (default sh/cmd)"},
										"cwd":     map[string]interface{}{"type": "string", "description": "Working directory (optional)"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Execution completed",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"stdout":    map[string]interface{}{"type": "string", "description": "Standard output"},
											"stderr":    map[string]interface{}{"type": "string", "description": "Standard error"},
											"exit_code": map[string]interface{}{"type": "integer", "description": "Exit code"},
											"error":     map[string]interface{}{"type": "string", "description": i18nText(requestWantsEnglish(c), "Execution error (optional)", "execution error (optional)")},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},
			"/api/terminal/run/stream": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Terminal"},
					"summary":     "Run terminal command (streaming)",
					"description": "Execute a shell command via SSE streaming, returning output in real time. Each event contains JSON: {\"t\": \"out\"|\"err\"|\"exit\", \"d\": \"data\", \"c\": exit code}",
					"operationId": "terminalRunStream",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"command"},
									"properties": map[string]interface{}{
										"command": map[string]interface{}{"type": "string", "description": "The command to execute"},
										"shell":   map[string]interface{}{"type": "string", "description": "Shell type (default sh/cmd)"},
										"cwd":     map[string]interface{}{"type": "string", "description": "Working directory (optional)"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "SSE event stream",
							"content": map[string]interface{}{
								"text/event-stream": map[string]interface{}{
									"schema": map[string]interface{}{
										"type":        "string",
										"description": "Server-Sent Events stream; each event is JSON: {\"t\":\"out|err|exit\",\"d\":\"data\",\"c\":exitCode}",
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},
			"/api/terminal/ws": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Terminal"},
					"summary":     "WebSocket terminal",
					"description": "Establish an interactive terminal connection via WebSocket with PTY support. The client sends text or binary data as command input, and can also send JSON: {\"type\":\"resize\",\"cols\":80,\"rows\":24} to resize the terminal. The server returns raw PTY output.",
					"operationId": "terminalWS",
					"responses": map[string]interface{}{
						"101": map[string]interface{}{"description": "WebSocket connection established"},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},

			// ==================== WebShell Management ====================
			"/api/webshell/connections": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"WebShell Management"},
					"summary":     "List WebShell connections",
					"description": "Get the list of all saved WebShell connection configurations.",
					"operationId": "listWebshellConnections",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "array",
										"items": map[string]interface{}{
											"type": "object",
											"properties": map[string]interface{}{
												"id":         map[string]interface{}{"type": "string", "description": "Connection ID"},
												"url":        map[string]interface{}{"type": "string", "description": "WebShell URL"},
												"password":   map[string]interface{}{"type": "string", "description": "Connection password"},
												"type":       map[string]interface{}{"type": "string", "description": "Shell type", "enum": []string{"php", "asp", "aspx", "jsp", "custom"}},
												"method":     map[string]interface{}{"type": "string", "description": "Request method", "enum": []string{"get", "post"}},
												"cmd_param":  map[string]interface{}{"type": "string", "description": "Command parameter name"},
												"remark":     map[string]interface{}{"type": "string", "description": "Note"},
												"created_at": map[string]interface{}{"type": "string", "format": "date-time"},
											},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
				"post": map[string]interface{}{
					"tags":        []string{"WebShell Management"},
					"summary":     "Create WebShell connection",
					"description": "Save a new WebShell connection configuration.",
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
										"password":  map[string]interface{}{"type": "string", "description": "Connection password"},
										"type":      map[string]interface{}{"type": "string", "description": "Shell type", "enum": []string{"php", "asp", "aspx", "jsp", "custom"}},
										"method":    map[string]interface{}{"type": "string", "description": "Request method", "enum": []string{"get", "post"}},
										"cmd_param": map[string]interface{}{"type": "string", "description": "Command parameter name"},
										"remark":    map[string]interface{}{"type": "string", "description": "Note"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Created successfully"},
						"400": map[string]interface{}{"description": "Bad request"},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},
			"/api/webshell/connections/{id}": map[string]interface{}{
				"put": map[string]interface{}{
					"tags":        []string{"WebShell Management"},
					"summary":     "Update WebShell connection",
					"description": "Update an existing WebShell connection configuration.",
					"operationId": "updateWebshellConnection",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "description": "Connection ID", "schema": map[string]interface{}{"type": "string"}},
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
						"200": map[string]interface{}{"description": "Updated successfully"},
						"401": map[string]interface{}{"description": "Unauthorized"},
						"404": map[string]interface{}{"description": "Connection not found"},
					},
				},
				"delete": map[string]interface{}{
					"tags":        []string{"WebShell Management"},
					"summary":     "Delete WebShell connection",
					"description": "Delete the specified WebShell connection configuration.",
					"operationId": "deleteWebshellConnection",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "description": "Connection ID", "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Deleted successfully"},
						"401": map[string]interface{}{"description": "Unauthorized"},
						"404": map[string]interface{}{"description": "Connection not found"},
					},
				},
			},
			"/api/webshell/connections/{id}/state": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"WebShell Management"},
					"summary":     "Get connection state",
					"description": "Get the saved state data of the WebShell connection.",
					"operationId": "getWebshellConnectionState",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "description": "Connection ID", "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"state": map[string]interface{}{"type": "object", "description": "State data (arbitrary JSON)"},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
				"put": map[string]interface{}{
					"tags":        []string{"WebShell Management"},
					"summary":     "Save connection state",
					"description": "Save the state data of the WebShell connection.",
					"operationId": "saveWebshellConnectionState",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "description": "Connection ID", "schema": map[string]interface{}{"type": "string"}},
					},
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"state": map[string]interface{}{"type": "object", "description": "State data (arbitrary JSON)"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Save successful"},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},
			"/api/webshell/connections/{id}/ai-history": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"WebShell Management"},
					"summary":     "Get AI conversation history",
					"description": "Get the AI-assisted conversation history messages of the specified WebShell connection.",
					"operationId": "getWebshellAIHistory",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "description": "Connection ID", "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
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
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},
			"/api/webshell/connections/{id}/ai-conversations": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"WebShell Management"},
					"summary":     "List AI conversations",
					"description": "Get the list of all AI-assisted conversations of the specified WebShell connection.",
					"operationId": "listWebshellAIConversations",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "description": "Connection ID", "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
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
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},
			"/api/webshell/exec": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"WebShell Management"},
					"summary":     "Execute WebShell command",
					"description": "Execute a remote command through the specified WebShell connection.",
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
										"command":   map[string]interface{}{"type": "string", "description": "The command to execute"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Execution result",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"ok":        map[string]interface{}{"type": "boolean"},
											"output":    map[string]interface{}{"type": "string", "description": "Command output"},
											"error":     map[string]interface{}{"type": "string", "description": i18nText(requestWantsEnglish(c), "Error message", "error message")},
											"http_code": map[string]interface{}{"type": "integer", "description": "HTTP response code"},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},
			"/api/webshell/file": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"WebShell Management"},
					"summary":     "WebShell file operation",
					"description": "Perform remote file operations via WebShell (list directories, read/write files, create directories, rename, delete, upload, etc.).",
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
										"action":      map[string]interface{}{"type": "string", "description": "Operation type", "enum": []string{"list", "read", "delete", "write", "mkdir", "rename", "upload", "upload_chunk"}},
										"path":        map[string]interface{}{"type": "string", "description": "Target file/directory path"},
										"target_path": map[string]interface{}{"type": "string", "description": "Target path (used for rename)"},
										"content":     map[string]interface{}{"type": "string", "description": "File content (used for write/upload)"},
										"chunk_index": map[string]interface{}{"type": "integer", "description": "Chunk index (used for upload_chunk)"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Operation result",
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
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},

			// ==================== Chat Uploads ====================
			"/api/chat-uploads": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Chat Uploads"},
					"summary":     "List attachments",
					"description": "Get the list of conversation files, including manually uploaded attachments, tool output and session artifacts; can be searched and filtered by session, project, source and file name with pagination.",
					"operationId": "listChatUploads",
					"parameters": []map[string]interface{}{
						{"name": "conversation", "in": "query", "required": false, "description": "Filter by conversation ID", "schema": map[string]interface{}{"type": "string"}},
						{"name": "project", "in": "query", "required": false, "description": "Filter by project ID", "schema": map[string]interface{}{"type": "string"}},
						{"name": "source", "in": "query", "required": false, "description": "Filter by source: upload/reduction/workspace/conversation_artifact/all", "schema": map[string]interface{}{"type": "string", "enum": []string{"all", "upload", "reduction", "workspace", "conversation_artifact"}}},
						{"name": "search", "in": "query", "required": false, "description": "Search by file name or sub-path", "schema": map[string]interface{}{"type": "string"}},
						{"name": "page", "in": "query", "required": false, "description": "Page number, starting from 1", "schema": map[string]interface{}{"type": "integer", "default": 1}},
						{"name": "pageSize", "in": "query", "required": false, "description": "Page size; pass all to return everything", "schema": map[string]interface{}{"oneOf": []map[string]interface{}{{"type": "integer"}, {"type": "string", "enum": []string{"all"}}}}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
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
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
				"post": map[string]interface{}{
					"tags":        []string{"Chat Uploads"},
					"summary":     "Upload attachment",
					"description": "Upload a file to the conversation attachments directory (multipart/form-data).",
					"operationId": "uploadChatFile",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"multipart/form-data": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"file"},
									"properties": map[string]interface{}{
										"file":           map[string]interface{}{"type": "string", "format": "binary", "description": "The uploaded file"},
										"conversationId": map[string]interface{}{"type": "string", "description": "Associated conversation ID (optional)"},
										"relativeDir":    map[string]interface{}{"type": "string", "description": "Target directory relative path (optional)"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Upload successful",
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
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
				"delete": map[string]interface{}{
					"tags":        []string{"Chat Uploads"},
					"summary":     "Delete attachment",
					"description": "Delete the specified conversation attachment file.",
					"operationId": "deleteChatUpload",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"path"},
									"properties": map[string]interface{}{
										"path": map[string]interface{}{"type": "string", "description": "File relative path"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Deleted successfully"},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},
			"/api/chat-uploads/export": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Chat Uploads"},
					"summary":     "Export attachments",
					"description": "Export the conversation files matching the current filters as a ZIP containing manifest.json.",
					"operationId": "exportChatUploads",
					"parameters": []map[string]interface{}{
						{"name": "conversation", "in": "query", "required": false, "description": "Filter by conversation ID", "schema": map[string]interface{}{"type": "string"}},
						{"name": "project", "in": "query", "required": false, "description": "Filter by project ID", "schema": map[string]interface{}{"type": "string"}},
						{"name": "source", "in": "query", "required": false, "description": "Filter by source: upload/reduction/workspace/conversation_artifact/all", "schema": map[string]interface{}{"type": "string", "enum": []string{"all", "upload", "reduction", "workspace", "conversation_artifact"}}},
						{"name": "search", "in": "query", "required": false, "description": "Search by file name or sub-path", "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "ZIP file download",
							"content": map[string]interface{}{
								"application/zip": map[string]interface{}{
									"schema": map[string]interface{}{"type": "string", "format": "binary"},
								},
							},
						},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},
			"/api/chat-uploads/download": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Chat Uploads"},
					"summary":     "Download attachment",
					"description": "Download the specified conversation attachment file.",
					"operationId": "downloadChatUpload",
					"parameters": []map[string]interface{}{
						{"name": "path", "in": "query", "required": true, "description": "File relative path", "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "File download",
							"content": map[string]interface{}{
								"application/octet-stream": map[string]interface{}{
									"schema": map[string]interface{}{"type": "string", "format": "binary"},
								},
							},
						},
						"401": map[string]interface{}{"description": "Unauthorized"},
						"404": map[string]interface{}{"description": "File not found"},
					},
				},
			},
			"/api/chat-uploads/path": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Chat Uploads"},
					"summary":     "Resolve attachment path",
					"description": "Resolve a relative path or internal virtual path from file management to a server absolute path, for copying file/directory paths.",
					"operationId": "resolveChatUploadPath",
					"parameters": []map[string]interface{}{
						{"name": "path", "in": "query", "required": true, "description": "Relative path or virtual path (e.g. __workspace__/projects/<id>/csv)", "schema": map[string]interface{}{"type": "string"}},
						{"name": "kind", "in": "query", "required": false, "description": "Path type: file/directory, default file", "schema": map[string]interface{}{"type": "string", "enum": []string{"file", "directory"}}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Parse successful",
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
						"401": map[string]interface{}{"description": "Unauthorized"},
						"403": map[string]interface{}{"description": "No access"},
						"404": map[string]interface{}{"description": "Path does not exist"},
					},
				},
			},
			"/api/chat-uploads/content": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Chat Uploads"},
					"summary":     "Get attachment text content",
					"description": "Read and return the contents of a text file.",
					"operationId": "getChatUploadContent",
					"parameters": []map[string]interface{}{
						{"name": "path", "in": "query", "required": true, "description": "File relative path", "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"content": map[string]interface{}{"type": "string", "description": "File text content"},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "Unauthorized"},
						"404": map[string]interface{}{"description": "File not found"},
					},
				},
				"put": map[string]interface{}{
					"tags":        []string{"Chat Uploads"},
					"summary":     "Write attachment text content",
					"description": "Write or overwrite the contents of a text file.",
					"operationId": "putChatUploadContent",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"path", "content"},
									"properties": map[string]interface{}{
										"path":    map[string]interface{}{"type": "string", "description": "File relative path"},
										"content": map[string]interface{}{"type": "string", "description": "File text content"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Write successful"},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},
			"/api/chat-uploads/mkdir": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Chat Uploads"},
					"summary":     "Create attachment directory",
					"description": "Create a subdirectory under the conversation attachments directory.",
					"operationId": "mkdirChatUpload",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"name"},
									"properties": map[string]interface{}{
										"parent": map[string]interface{}{"type": "string", "description": "Parent directory relative path"},
										"name":   map[string]interface{}{"type": "string", "description": "Directory name"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Created successfully",
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
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},
			"/api/chat-uploads/rename": map[string]interface{}{
				"put": map[string]interface{}{
					"tags":        []string{"Chat Uploads"},
					"summary":     "Rename attachment",
					"description": "Rename a conversation attachment file or directory.",
					"operationId": "renameChatUpload",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"path", "newName"},
									"properties": map[string]interface{}{
										"path":    map[string]interface{}{"type": "string", "description": "Current file relative path"},
										"newName": map[string]interface{}{"type": "string", "description": "New name"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Rename successful",
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
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},

			// ==================== Robot Integration ====================
			"/api/robot/wecom": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Robot Integration"},
					"summary":     "WeCom callback verification",
					"description": "WeCom server URL verification callback (used to verify when configuring the message receiving address). No authentication required.",
					"operationId": "wecomCallbackVerify",
					"security":    []map[string]interface{}{},
					"parameters": []map[string]interface{}{
						{"name": "msg_signature", "in": "query", "required": true, "schema": map[string]interface{}{"type": "string"}},
						{"name": "timestamp", "in": "query", "required": true, "schema": map[string]interface{}{"type": "string"}},
						{"name": "nonce", "in": "query", "required": true, "schema": map[string]interface{}{"type": "string"}},
						{"name": "echostr", "in": "query", "required": true, "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Verification successful, returns the decrypted echostr"},
					},
				},
				"post": map[string]interface{}{
					"tags":        []string{"Robot Integration"},
					"summary":     "WeCom message callback",
					"description": "Receive message events pushed by WeCom. No authentication required; called by the WeCom server.",
					"operationId": "wecomCallbackMessage",
					"security":    []map[string]interface{}{},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Processed successfully"},
					},
				},
			},
			"/api/robot/dingtalk": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Robot Integration"},
					"summary":     "DingTalk message callback",
					"description": "Receive message events pushed by DingTalk. No authentication required; called by the DingTalk server.",
					"operationId": "dingtalkCallback",
					"security":    []map[string]interface{}{},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Processed successfully"},
					},
				},
			},
			"/api/robot/lark": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Robot Integration"},
					"summary":     "Lark message callback",
					"description": "Receive message events pushed by Lark. No authentication required; called by the Lark server.",
					"operationId": "larkCallback",
					"security":    []map[string]interface{}{},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Processed successfully"},
					},
				},
			},
			"/api/robot/test": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Robot Integration"},
					"summary":     "Test robot message processing",
					"description": "Simulate the robot message processing flow, for debugging and verification. Requires login authentication.",
					"operationId": "testRobot",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"platform", "text"},
									"properties": map[string]interface{}{
										"platform": map[string]interface{}{"type": "string", "description": "Platform type", "enum": []string{"dingtalk", "lark", "wecom"}},
										"user_id":  map[string]interface{}{"type": "string", "description": "Simulated user ID", "example": "test"},
										"text":     map[string]interface{}{"type": "string", "description": "Message text", "example": "help"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Processed successfully"},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},

			// ==================== Markdown Agents ====================
			"/api/multi-agent/markdown-agents": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Markdown Agents"},
					"summary":     "List Markdown agents",
					"description": "Get the list of all multi-agent Markdown definition files.",
					"operationId": "listMarkdownAgents",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
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
														"filename":        map[string]interface{}{"type": "string", "description": "File name"},
														"id":              map[string]interface{}{"type": "string", "description": "Agent ID"},
														"name":            map[string]interface{}{"type": "string", "description": "Agent name"},
														"description":     map[string]interface{}{"type": "string", "description": "Agent description"},
														"is_orchestrator": map[string]interface{}{"type": "boolean", "description": "Whether it is an orchestrator"},
														"kind":            map[string]interface{}{"type": "string", "description": "Orchestration type"},
													},
												},
											},
											"dir": map[string]interface{}{"type": "string", "description": "Agent definition directory path"},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
				"post": map[string]interface{}{
					"tags":        []string{"Markdown Agents"},
					"summary":     "Create Markdown agent",
					"description": "Create a new multi-agent Markdown definition file.",
					"operationId": "createMarkdownAgent",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"name"},
									"properties": map[string]interface{}{
										"filename":       map[string]interface{}{"type": "string", "description": "File name (optional, auto-generated)"},
										"id":             map[string]interface{}{"type": "string", "description": "Agent ID"},
										"name":           map[string]interface{}{"type": "string", "description": "Agent name"},
										"description":    map[string]interface{}{"type": "string", "description": "Agent description"},
										"tools":          map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Available tool list"},
										"instruction":    map[string]interface{}{"type": "string", "description": "Agent instruction"},
										"bind_role":      map[string]interface{}{"type": "string", "description": "Bound role"},
										"max_iterations": map[string]interface{}{"type": "integer", "description": "Maximum iteration count"},
										"kind":           map[string]interface{}{"type": "string", "description": "Orchestration type"},
										"raw":            map[string]interface{}{"type": "string", "description": "Raw Markdown content"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Created successfully",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"filename": map[string]interface{}{"type": "string"},
											"message":  map[string]interface{}{"type": "string", "example": "Created"},
										},
									},
								},
							},
						},
						"400": map[string]interface{}{"description": "Bad request"},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},
			"/api/multi-agent/markdown-agents/{filename}": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Markdown Agents"},
					"summary":     "Get Markdown agent detail",
					"description": "Get the detailed content of the specified Markdown agent definition file.",
					"operationId": "getMarkdownAgent",
					"parameters": []map[string]interface{}{
						{"name": "filename", "in": "path", "required": true, "description": "File name", "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"filename":        map[string]interface{}{"type": "string"},
											"raw":             map[string]interface{}{"type": "string", "description": "Raw Markdown content"},
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
						"401": map[string]interface{}{"description": "Unauthorized"},
						"404": map[string]interface{}{"description": "Agent not found"},
					},
				},
				"put": map[string]interface{}{
					"tags":        []string{"Markdown Agents"},
					"summary":     "Update Markdown agent",
					"description": "Update the specified Markdown agent definition.",
					"operationId": "updateMarkdownAgent",
					"parameters": []map[string]interface{}{
						{"name": "filename", "in": "path", "required": true, "description": "File name", "schema": map[string]interface{}{"type": "string"}},
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
							"description": "Updated successfully",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"message": map[string]interface{}{"type": "string", "example": "Saved"},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "Unauthorized"},
						"404": map[string]interface{}{"description": "Agent not found"},
					},
				},
				"delete": map[string]interface{}{
					"tags":        []string{"Markdown Agents"},
					"summary":     "Delete Markdown agent",
					"description": "Delete the specified Markdown agent definition file.",
					"operationId": "deleteMarkdownAgent",
					"parameters": []map[string]interface{}{
						{"name": "filename", "in": "path", "required": true, "description": "File name", "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Deleted successfully",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"message": map[string]interface{}{"type": "string", "example": "Deleted"},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "Unauthorized"},
						"404": map[string]interface{}{"description": "Agent not found"},
					},
				},
			},

			// ==================== Skills Management - missing endpoints ====================
			"/api/skills/{name}/files": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Skills Management"},
					"summary":     "List skill package files",
					"description": "Get the list of all files under the specified skill package directory.",
					"operationId": "listSkillPackageFiles",
					"parameters": []map[string]interface{}{
						{"name": "name", "in": "path", "required": true, "description": "Skill name/ID", "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"files": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "File path list"},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "Unauthorized"},
						"404": map[string]interface{}{"description": "Skill not found"},
					},
				},
			},
			"/api/skills/{name}/file": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Skills Management"},
					"summary":     "Get skill package file content",
					"description": "Read the content of the specified file in a skill package.",
					"operationId": "getSkillPackageFile",
					"parameters": []map[string]interface{}{
						{"name": "name", "in": "path", "required": true, "description": "Skill name/ID", "schema": map[string]interface{}{"type": "string"}},
						{"name": "path", "in": "query", "required": true, "description": "File relative path", "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"path":    map[string]interface{}{"type": "string", "description": "File path"},
											"content": map[string]interface{}{"type": "string", "description": "File content"},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "Unauthorized"},
						"404": map[string]interface{}{"description": "File not found"},
					},
				},
				"put": map[string]interface{}{
					"tags":        []string{"Skills Management"},
					"summary":     "Write skill package file",
					"description": "Write or update the content of a file in a skill package.",
					"operationId": "putSkillPackageFile",
					"parameters": []map[string]interface{}{
						{"name": "name", "in": "path", "required": true, "description": "Skill name/ID", "schema": map[string]interface{}{"type": "string"}},
					},
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"path"},
									"properties": map[string]interface{}{
										"path":    map[string]interface{}{"type": "string", "description": "File relative path"},
										"content": map[string]interface{}{"type": "string", "description": "File content"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Save successful",
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
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},

			// ==================== Monitoring - missing endpoints ====================
			"/api/monitor/executions/names": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Monitoring"},
					"summary":     "Batch get tool names",
					"description": "Batch-get the corresponding tool names from a list of execution IDs, eliminating the frontend N+1 request problem.",
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
											"description": "List of execution record IDs",
										},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success, returns a mapping from execution ID to tool name",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type":                 "object",
										"additionalProperties": map[string]interface{}{"type": "string"},
										"description":          "Keys are execution IDs, values are tool names",
										"example":              map[string]interface{}{"exec-001": "nmap", "exec-002": "sqlmap"},
									},
								},
							},
						},
						"400": map[string]interface{}{"description": "Bad request"},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},

			// ==================== Knowledge Base - missing endpoints ====================
			"/api/knowledge/stats": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Knowledge Base"},
					"summary":     "Get knowledge base statistics",
					"description": "Get the overall statistics of the knowledge base, including category count and item count.",
					"operationId": "getKnowledgeStats",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"enabled":          map[string]interface{}{"type": "boolean", "description": "Whether the knowledge base is enabled"},
											"total_categories": map[string]interface{}{"type": "integer", "description": "Total categories"},
											"total_items":      map[string]interface{}{"type": "integer", "description": "Total entries"},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},

			"/api/mcp": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"MCP"},
					"summary":     "MCP endpoint",
					"description": "MCP (Model Context Protocol) endpoint for handling MCP protocol requests.\n**Protocol notes**:\nThis endpoint follows the JSON-RPC 2.0 specification and supports the following methods:\n**1. initialize** - initialize the MCP connection\n```json\n{\n  \"jsonrpc\": \"2.0\",\n  \"id\": \"init-1\",\n  \"method\": \"initialize\",\n  \"params\": {\n    \"protocolVersion\": \"2024-11-05\",\n    \"capabilities\": {},\n    \"clientInfo\": {\n      \"name\": \"MyClient\",\n      \"version\": \"1.0.0\"\n    }\n  }\n}\n```\n**2. tools/list** - list all available tools\n```json\n{\n  \"jsonrpc\": \"2.0\",\n  \"id\": \"list-1\",\n  \"method\": \"tools/list\",\n  \"params\": {}\n}\n```\n**3. tools/call** - invoke a tool\n```json\n{\n  \"jsonrpc\": \"2.0\",\n  \"id\": \"call-1\",\n  \"method\": \"tools/call\",\n  \"params\": {\n    \"name\": \"nmap\",\n    \"arguments\": {\n      \"target\": \"192.168.1.1\",\n      \"ports\": \"80,443\"\n    }\n  }\n}\n```\n**4. prompts/list** - list all prompt templates\n```json\n{\n  \"jsonrpc\": \"2.0\",\n  \"id\": \"prompts-list-1\",\n  \"method\": \"prompts/list\",\n  \"params\": {}\n}\n```\n**5. prompts/get** - get a prompt template\n```json\n{\n  \"jsonrpc\": \"2.0\",\n  \"id\": \"prompt-get-1\",\n  \"method\": \"prompts/get\",\n  \"params\": {\n    \"name\": \"prompt-name\",\n    \"arguments\": {}\n  }\n}\n```\n**6. resources/list** - list all resources\n```json\n{\n  \"jsonrpc\": \"2.0\",\n  \"id\": \"resources-list-1\",\n  \"method\": \"resources/list\",\n  \"params\": {}\n}\n```\n**7. resources/read** - read resource content\n```json\n{\n  \"jsonrpc\": \"2.0\",\n  \"id\": \"resource-read-1\",\n  \"method\": \"resources/read\",\n  \"params\": {\n    \"uri\": \"resource://example\"\n  }\n}\n```\n**Error code notes**:\n- `-32700`: Parse error - JSON parse error\n- `-32600`: Invalid Request - invalid request\n- `-32601`: Method not found - method not found\n- `-32602`: Invalid params - invalid params\n- `-32603`: Internal error - internal error",
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
										"summary":     "List all tools",
										"description": "Get the list of all MCP tools available in the system",
										"value": map[string]interface{}{
											"jsonrpc": "2.0",
											"id":      "list-tools-1",
											"method":  "tools/list",
											"params":  map[string]interface{}{},
										},
									},
									"callTool": map[string]interface{}{
										"summary":     "Invoke tool",
										"description": "Invoke the specified MCP tool",
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
										"summary":     "Initialize connection",
										"description": "Initialize the MCP connection, obtaining server capabilities",
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
							"description": "MCP response (JSON-RPC 2.0 format)",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/MCPResponse",
									},
									"examples": map[string]interface{}{
										"success": map[string]interface{}{
											"summary":     "Successful response",
											"description": "Example response of a successful tool call",
											"value": map[string]interface{}{
												"jsonrpc": "2.0",
												"id":      "call-tool-1",
												"result": map[string]interface{}{
													"content": []map[string]interface{}{
														{
															"type": "text",
															"text": "Tool execution result...",
														},
													},
													"isError": false,
												},
											},
										},
										"error": map[string]interface{}{
											"summary":     "Error response",
											"description": "Example response of a failed tool call",
											"value": map[string]interface{}{
												"jsonrpc": "2.0",
												"id":      "call-tool-1",
												"error": map[string]interface{}{
													"code":    -32601,
													"message": "Tool not found",
													"data":    "Tool 'unknown-tool' not found",
												},
											},
										},
									},
								},
							},
						},
						"400": map[string]interface{}{
							"description": "Request format error (JSON parse failure)",
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
							"description": "Unauthorized, a valid Token is required",
						},
						"405": map[string]interface{}{
							"description": "Method not allowed (only POST requests are supported)",
						},
					},
				},
			},
			"/api/system/update": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"System Update"},
					"summary":     "View this installation's source version",
					"description": "Read the installation directory's own git working tree (branch, remote, current commit, local changes, whether a Go toolchain is available, whether a rollback point exists), making no network requests",
					"operationId": "getUpdateStatus",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Installation status and the most recent update job",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/system/update/check": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"System Update"},
					"summary":     "Check this installation's update source for new commits",
					"description": "Run fetch against the default branch of the configured update source repository (unconfigured = official repository https://github.com/AIPentest/CyberStrikeAI.git), reporting behind/ahead commit counts and the new commit list. If the check fails, it returns 200 with checkError, without claiming \"up-to-date\" when it cannot check",
					"operationId": "checkUpdate",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Check result (contains checkError when the check did not succeed)",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/system/update/apply": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"System Update"},
					"summary":     "One-click source update and rebuild",
					"description": "Fast-forward to the latest commit of the update source repository's default branch, rebuild the binary and swap it in atomically (the old binary is kept as .prev for rollback). If local source files have been modified or the branch has diverged, execution is refused and the files are named; operator content such as roles/skills/tools/agents/bundles/knowledge_base/data/config.yaml is stashed before the merge and restored afterwards, and the preserved files are listed in keptContent. The request returns job_id immediately; poll progress with GET /api/system/update/job; if a job is already running it returns 409",
					"operationId": "applyUpdate",
					"requestBody": map[string]interface{}{
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"restart": map[string]interface{}{
											"type":        "boolean",
											"description": "After a successful update, shut down gracefully and exit the process; only restarted by an external supervisor; default false",
										},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"202": map[string]interface{}{
							"description": "Task accepted, returns job_id",
						},
						"400": map[string]interface{}{
							"description": "Invalid request body, or restart required but this launch did not wire a restart hook",
						},
						"409": map[string]interface{}{
							"description": "An update is already in progress",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/system/update/job": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"System Update"},
					"summary":     "View update job progress",
					"description": "Returns the status of the currently running or most recent update job, its per-line progress steps, result and failure; job is null if never updated",
					"operationId": "getUpdateJob",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Task status",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/system/update/rollback": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"System Update"},
					"summary":     "Roll back to before this update",
					"description": "Check out the old commit recorded in the previous update entry and restore the old binary preserved before the swap-in. If HEAD has changed since that update, or there are local source modifications, it returns 409 refusing, rather than discarding the work done since the update",
					"operationId": "rollbackUpdate",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Rolled back",
						},
						"409": map[string]interface{}{
							"description": "No rollback record, an update is in progress, or HEAD has been moved since the update",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
		},
	}

	addAgentRunSpec(spec)
	enrichSpecWithI18nKeys(spec)
	c.JSON(http.StatusOK, spec)
}

// GetConversationResults returns conversation results (OpenAPI endpoint)
// Note: creating conversations and getting conversation details use the standard /api/conversations endpoints directly
// This endpoint exists only to provide the results aggregation function
func (h *OpenAPIHandler) GetConversationResults(c *gin.Context) {
	conversationID := c.Param("id")

	// Verify the conversation exists
	conv, err := h.db.GetConversation(conversationID)
	if err != nil {
		h.logger.Error("Get conversation failed", zap.Error(err))
		respondError(c, http.StatusNotFound, "Conversation not found", "conversation not found")
		return
	}

	// Get the message list
	messages, err := h.db.GetMessages(conversationID)
	if err != nil {
		h.logger.Error("Get messages failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Get the vulnerability list
	vulnList, err := h.db.ListVulnerabilities(1000, 0, database.VulnerabilityListFilter{ConversationID: conversationID})
	if err != nil {
		h.logger.Warn("Get vulnerability list failed", zap.Error(err))
		vulnList = []*database.Vulnerability{}
	}
	vulnerabilities := make([]database.Vulnerability, len(vulnList))
	for i, v := range vulnList {
		vulnerabilities[i] = *v
	}

	// Get the execution results (historical large results are persisted by Eino reduction; file storage is no longer aggregated here)
	executionResults := []map[string]interface{}{}

	response := map[string]interface{}{
		"conversationId":   conv.ID,
		"messages":         messages,
		"vulnerabilities":  vulnerabilities,
		"executionResults": executionResults,
	}

	c.JSON(http.StatusOK, response)
}
