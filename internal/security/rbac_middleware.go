package security

import (
	"net/http"
	"strings"

	"cyberstrike-ai/internal/database"

	"github.com/gin-gonic/gin"
)

// RBACMiddleware maps protected API routes to platform permissions. It keeps
// enforcement centralized so route declarations stay readable.
func RBACMiddleware(db *database.DB) gin.HandlerFunc {
	return RBACMiddlewareWithDenyHook(db, nil)
}

type RBACDenyHook func(c *gin.Context, reason, permission string)

func RBACMiddlewareWithDenyHook(db *database.DB, denyHook RBACDenyHook) gin.HandlerFunc {
	return func(c *gin.Context) {
		permission := permissionForRequest(c.Request.Method, c.FullPath())
		if permission == "" {
			if denyHook != nil {
				denyHook(c, "unmapped_route", "")
			}
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "未配置访问权限",
			})
			return
		}
		permission, allowed := sessionHasRoutePermission(c, c.Request.Method, c.FullPath())
		if !allowed {
			if denyHook != nil {
				denyHook(c, "permission_denied", permission)
			}
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error":      "权限不足",
				"permission": permission,
			})
			return
		}
		// Bind the scope of the permission authorizing this request. Scope is
		// permission-specific; using the user's broadest role scope here would
		// let an unrelated global read role widen a write permission.
		session, _ := CurrentSession(c)
		session.Scope = session.ScopeFor(permission)
		c.Set(ContextSessionKey, session)
		c.Set(ContextUserScopeKey, session.Scope)
		if db != nil && !resourceAllowed(c, db) {
			if denyHook != nil {
				denyHook(c, "resource_denied", permission)
			}
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "无权访问该资源"})
			return
		}
		c.Next()
	}
}

func sessionHasRoutePermission(c *gin.Context, method, fullPath string) (string, bool) {
	path := strings.TrimPrefix(fullPath, "/api")
	if alts := permissionAlternativesForRequest(method, path); len(alts) > 0 {
		for _, permission := range alts {
			if SessionHasPermission(c, permission) {
				return permission, true
			}
		}
		return alts[0], false
	}
	permission := permissionForRequest(method, fullPath)
	if permission == "" {
		return "", false
	}
	return permission, SessionHasPermission(c, permission)
}

func permissionAlternativesForRequest(method, path string) []string {
	if method != http.MethodGet && method != http.MethodHead {
		return nil
	}
	switch {
	case strings.HasPrefix(path, "/config/tools"):
		// MCP 管理页只需 mcp:read；系统设置页仍可用 config:read 访问同一接口。
		return []string{"mcp:read", "config:read"}
	default:
		return nil
	}
}

func permissionForRequest(method, fullPath string) string {
	path := strings.TrimPrefix(fullPath, "/api")
	switch {
	case path == "/rbac/me":
		return "auth:self"
	case path == "/rbac/resources":
		// The picker enumerates resource names and IDs and is only needed by
		// administrators who can actually create assignments.
		return "rbac:write"
	case strings.HasPrefix(path, "/rbac"):
		if method == http.MethodGet {
			return "rbac:read"
		}
		return "rbac:write"
	case strings.HasPrefix(path, "/robot/wechat/status"):
		return "robot:read"
	case strings.HasPrefix(path, "/robot"):
		return "robot:write"
	case strings.HasPrefix(path, "/eino-agent"), strings.HasPrefix(path, "/multi-agent"):
		if strings.Contains(path, "/markdown-agents") {
			return crudPermission(method, "agents")
		}
		return "agent:execute"
	case strings.HasPrefix(path, "/hitl"):
		if method == http.MethodGet || method == http.MethodHead {
			return "hitl:read"
		}
		return "hitl:write"
	case strings.HasPrefix(path, "/agent-loop"), strings.HasPrefix(path, "/batch-tasks"):
		return crudPermission(method, "tasks")
	case path == "/usage/tokens":
		return "dashboard:read"
	case strings.HasPrefix(path, "/conversations"), strings.HasPrefix(path, "/messages"), strings.HasPrefix(path, "/process-details"):
		return crudPermission(method, "chat")
	case strings.HasPrefix(path, "/monitor"):
		return crudPermission(method, "monitor")
	case strings.HasPrefix(path, "/notifications"):
		if method == http.MethodGet {
			return "notification:read"
		}
		return "notification:write"
	case path == "/tool-guard/test" && method == http.MethodPost:
		return "config:read"
	case path == "/tool-guard":
		return crudPermission(method, "config")
	case strings.HasPrefix(path, "/config"):
		return crudPermission(method, "config")
	case strings.HasPrefix(path, "/storage"):
		return crudPermission(method, "storage")
	case strings.HasPrefix(path, "/terminal"):
		return "terminal:execute"
	case strings.HasPrefix(path, "/audit"):
		return crudPermission(method, "audit")
	case path == "/mcp":
		return "mcp:execute"
	case strings.HasPrefix(path, "/external-mcp"):
		if method == http.MethodGet || method == http.MethodHead {
			return "mcp:read"
		}
		return "mcp:write"
	case strings.HasPrefix(path, "/attack-chain"):
		return crudPermission(method, "attackchain")
	case strings.HasPrefix(path, "/knowledge"):
		if path == "/knowledge/search" {
			return "knowledge:read"
		}
		return crudPermission(method, "knowledge")
	case strings.HasPrefix(path, "/vulnerabilities"):
		return crudPermission(method, "vulnerability")
	case path == "/assets/batch-delete", path == "/assets/merge":
		return "asset:delete"
	case strings.HasPrefix(path, "/assets"):
		return crudPermission(method, "asset")
	case strings.HasPrefix(path, "/vulnerability-alerts"):
		// This endpoint only changes the authenticated user's own preference.
		return "vulnerability:read"
	case strings.HasPrefix(path, "/projects"):
		return crudPermission(method, "project")
	case strings.HasPrefix(path, "/webshell"):
		return crudPermission(method, "webshell")
	case strings.HasPrefix(path, "/c2"):
		return crudPermission(method, "c2")
	case strings.HasPrefix(path, "/chat-uploads"):
		return crudPermission(method, "files")
	case strings.HasPrefix(path, "/roles"):
		return crudPermission(method, "roles")
	case path == "/workflows/:id/package":
		return "workflow:read"
	case strings.HasPrefix(path, "/workflow-package-inspections"), strings.HasPrefix(path, "/workflow-package-imports"):
		return "workflow:write"
	case path == "/workflows/generate-draft":
		return "workflow:write"
	case strings.HasPrefix(path, "/workflows"):
		if path == "/workflows/validate" || path == "/workflows/dry-run" || strings.HasSuffix(path, "/resume") {
			return "workflow:execute"
		}
		return crudPermission(method, "workflow")
	case strings.HasPrefix(path, "/skills"):
		return crudPermission(method, "skills")
	case strings.HasPrefix(path, "/openapi"):
		return "openapi:read"
	case strings.HasPrefix(path, "/fofa"):
		return "fofa:execute"
	default:
		return ""
	}
}

func crudPermission(method, module string) string {
	switch method {
	case http.MethodGet, http.MethodHead:
		return module + ":read"
	case http.MethodDelete:
		return module + ":delete"
	default:
		return module + ":write"
	}
}

func resourceAllowed(c *gin.Context, db *database.DB) bool {
	session, ok := CurrentSession(c)
	if !ok || session.Scope == database.RBACScopeAll {
		return ok
	}
	path := strings.TrimPrefix(c.FullPath(), "/api")
	switch {
	case path == "/tool-guard" && isMutationMethod(c.Request.Method):
		return session.Scope == database.RBACScopeAll
	case path == "/monitor/stats", path == "/monitor/calls-timeline":
		// These APIs currently operate on process-global state. Until every MCP
		// invocation and persisted execution record carries an immutable owner,
		// allowing an assigned/own-scoped session would be a cross-user bypass.
		return session.Scope == database.RBACScopeAll
	case strings.HasPrefix(path, "/c2/profiles") && c.Request.Method != http.MethodGet:
		return session.Scope == database.RBACScopeAll
	case (strings.HasPrefix(path, "/hitl/tool-whitelist") || strings.HasPrefix(path, "/hitl/default-config") || strings.HasPrefix(path, "/hitl/default-reviewer") || strings.HasPrefix(path, "/hitl/audit-strategy")) && c.Request.Method != http.MethodGet:
		return session.Scope == database.RBACScopeAll
	case isMutationMethod(c.Request.Method) && isProcessGlobalMutationPath(path):
		// These definitions/configurations are shared by every user and do not
		// carry owners. A module write permission with assigned/own scope must
		// not silently become a process-global administrative capability.
		return session.Scope == database.RBACScopeAll
	case strings.HasPrefix(path, "/projects/:id"):
		return db.UserCanAccessResource(session.UserID, session.Scope, "project", c.Param("id"))
	case strings.HasPrefix(path, "/conversations/:id"):
		return db.UserCanAccessResource(session.UserID, session.Scope, "conversation", c.Param("id"))
	case strings.HasPrefix(path, "/messages/:id/process-details"):
		return db.UserCanAccessMessage(session.UserID, session.Scope, c.Param("id"))
	case strings.HasPrefix(path, "/process-details/:id"):
		return db.UserCanAccessProcessDetail(session.UserID, session.Scope, c.Param("id"))
	case strings.HasPrefix(path, "/attack-chain/:conversationId"):
		return db.UserCanAccessResource(session.UserID, session.Scope, "conversation", c.Param("conversationId"))
	case strings.HasPrefix(path, "/webshell/connections/:id"):
		return db.UserCanAccessResource(session.UserID, session.Scope, "webshell", c.Param("id"))
	case strings.HasPrefix(path, "/batch-tasks/:queueId"):
		return db.UserCanAccessResource(session.UserID, session.Scope, "batch_task", c.Param("queueId"))
	case strings.HasPrefix(path, "/vulnerabilities/:id"):
		return db.UserCanAccessResource(session.UserID, session.Scope, "vulnerability", c.Param("id"))
	case strings.HasPrefix(path, "/assets/:id"):
		return db.UserCanAccessResource(session.UserID, session.Scope, "asset", c.Param("id"))
	case strings.HasPrefix(path, "/c2/listeners/:id"):
		return db.UserCanAccessResource(session.UserID, session.Scope, "c2_listener", c.Param("id"))
	case strings.HasPrefix(path, "/c2/sessions/:id"):
		return db.UserCanAccessResource(session.UserID, session.Scope, "c2_session", c.Param("id"))
	case strings.HasPrefix(path, "/c2/tasks/:id"):
		return db.UserCanAccessResource(session.UserID, session.Scope, "c2_task", c.Param("id"))
	default:
		return true
	}
}

func isMutationMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func isProcessGlobalMutationPath(path string) bool {
	if strings.HasPrefix(path, "/roles") || strings.HasPrefix(path, "/skills") ||
		strings.HasPrefix(path, "/external-mcp") || strings.HasPrefix(path, "/robot") {
		return true
	}
	if strings.HasPrefix(path, "/workflows") {
		// Workflow runs inherit conversation access; definitions are global.
		return !strings.HasPrefix(path, "/workflows/runs/") && path != "/workflows/validate" && path != "/workflows/dry-run" && path != "/workflows/generate-draft"
	}
	if strings.HasPrefix(path, "/workflow-package-inspections") || strings.HasPrefix(path, "/workflow-package-imports") {
		return true
	}
	if strings.HasPrefix(path, "/knowledge") {
		return path != "/knowledge/search"
	}
	if strings.HasPrefix(path, "/eino-agent/markdown-agents") || strings.HasPrefix(path, "/multi-agent/markdown-agents") {
		return true
	}
	return false
}
