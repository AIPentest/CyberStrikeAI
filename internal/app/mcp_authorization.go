package app

import (
	"context"
	"fmt"
	"strings"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/authctx"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/mcp/builtin"
)

func mcpToolAuthorizer(db *database.DB) func(context.Context, string, map[string]interface{}) error {
	return func(ctx context.Context, toolName string, args map[string]interface{}) error {
		principal, ok := authctx.PrincipalFromContext(ctx)
		if !ok {
			return fmt.Errorf("missing authenticated principal")
		}
		require := func(permission string) error {
			if !principal.HasPermission(permission) {
				return fmt.Errorf("missing permission %s", permission)
			}
			return nil
		}
		resource := func(permission, resourceType, argument string) error {
			if err := require(permission); err != nil {
				return err
			}
			id := mcpAuthorizationString(args, argument)
			if id == "" || db == nil || !db.UserCanAccessResource(principal.UserID, principal.ScopeFor(permission), resourceType, id) {
				return fmt.Errorf("no access to %s %s", resourceType, id)
			}
			if err := authorizeMCPProjectResourceBoundary(ctx, db, resourceType, id); err != nil {
				return err
			}
			return nil
		}
		toolExecutionResource := func(permission string) error {
			if err := require(permission); err != nil {
				return err
			}
			id := mcpAuthorizationString(args, "execution_id")
			if id == "" || db == nil || !db.UserCanAccessToolExecution(principal.UserID, principal.ScopeFor(permission), id) {
				return fmt.Errorf("no access to tool execution %s", id)
			}
			return nil
		}

		switch toolName {
		case builtin.ToolWebshellExec, builtin.ToolWebshellFileWrite:
			return resource("webshell:write", "webshell", "connection_id")
		case builtin.ToolWebshellFileList, builtin.ToolWebshellFileRead:
			return resource("webshell:read", "webshell", "connection_id")
		case builtin.ToolManageWebshellList:
			return require("webshell:read")
		case builtin.ToolManageWebshellAdd:
			return require("webshell:write")
		case builtin.ToolManageWebshellUpdate, builtin.ToolManageWebshellTest:
			return resource("webshell:write", "webshell", "connection_id")
		case builtin.ToolManageWebshellDelete:
			return resource("webshell:delete", "webshell", "connection_id")
		case builtin.ToolRecordVulnerability:
			if err := require("vulnerability:write"); err != nil {
				return err
			}
			conversationID := mcpAuthorizationString(args, "conversation_id")
			if conversationID == "" {
				conversationID = mcpAuthorizationConversationID(ctx)
			}
			if conversationID == "" || db == nil || !db.UserCanAccessResource(principal.UserID, principal.ScopeFor("vulnerability:write"), "conversation", conversationID) {
				return fmt.Errorf("no access to conversation %s", conversationID)
			}
			return nil
		case builtin.ToolListVulnerabilities:
			if err := require("vulnerability:read"); err != nil {
				return err
			}
			conversationID := mcpAuthorizationConversationID(ctx)
			if conversationID == "" || db == nil || !db.UserCanAccessResource(principal.UserID, principal.ScopeFor("vulnerability:read"), "conversation", conversationID) {
				return fmt.Errorf("no access to conversation %s", conversationID)
			}
			return nil
		case builtin.ToolGetVulnerability, builtin.ToolUpdateVulnerabilityStatus:
			permission := "vulnerability:read"
			if toolName == builtin.ToolUpdateVulnerabilityStatus {
				permission = "vulnerability:write"
			}
			if err := require(permission); err != nil {
				return err
			}
			id := mcpAuthorizationString(args, "id")
			if db == nil || id == "" || !db.UserCanAccessResource(principal.UserID, principal.ScopeFor(permission), "vulnerability", id) {
				return fmt.Errorf("no access to vulnerability %s", id)
			}
			convID := mcpAuthorizationConversationID(ctx)
			if r, err := db.GetVulnerabilityRetest(convID); err != nil {
				return err
			} else if r != nil {
				if r.VulnerabilityID != id || !db.UserCanAccessResource(principal.UserID, principal.ScopeFor(permission), "conversation", convID) {
					return fmt.Errorf("no access to retest vulnerability %s", id)
				}
				filter := mcpEffectiveProjectFilter(ctx, db)
				if (filter == database.ProjectFilterUnbound && r.SourceProjectID != "") || (filter != "" && filter != database.ProjectFilterUnbound && filter != r.SourceProjectID) {
					return fmt.Errorf("retest conversation conflicts with current project scope")
				}
				v, err := db.GetVulnerability(id)
				if err != nil {
					return err
				}
				projectID, err := db.GetConversationProjectID(convID)
				if err != nil || !canAccessVulnerabilityForRetest(db, v, convID, projectID) {
					return fmt.Errorf("retest conversation project no longer matches vulnerability %s", id)
				}
				return nil
			}
			return authorizeMCPProjectResourceBoundary(ctx, db, "vulnerability", id)
		case builtin.ToolQueryAssets:
			return require("asset:read")
		case builtin.ToolGetAsset:
			return resource("asset:read", "asset", "id")
		case builtin.ToolCreateAsset:
			if err := require("asset:write"); err != nil {
				return err
			}
			if projectID := mcpAuthorizationString(args, "project_id"); projectID != "" && (db == nil || !db.UserCanAccessResource(principal.UserID, principal.ScopeFor("asset:write"), "project", projectID)) {
				return fmt.Errorf("no access to project %s", projectID)
			}
			return nil
		case builtin.ToolUpdateAsset, builtin.ToolCompleteAssetScan:
			if err := resource("asset:write", "asset", "id"); err != nil {
				return err
			}
			if toolName == builtin.ToolCompleteAssetScan {
				conversationID := mcpAuthorizationConversationID(ctx)
				if conversationID == "" || db == nil || !db.UserCanAccessResource(principal.UserID, principal.ScopeFor("asset:write"), "conversation", conversationID) {
					return fmt.Errorf("no access to conversation %s", conversationID)
				}
				return nil
			}
			if projectID := mcpAuthorizationString(args, "project_id"); projectID != "" && (db == nil || !db.UserCanAccessResource(principal.UserID, principal.ScopeFor("asset:write"), "project", projectID)) {
				return fmt.Errorf("no access to project %s", projectID)
			}
			return nil
		case builtin.ToolDeleteAsset:
			return resource("asset:delete", "asset", "id")
		case builtin.ToolUpsertProjectFact, builtin.ToolDeprecateProjectFact, builtin.ToolRestoreProjectFact:
			return authorizeProjectTool(ctx, principal, db, "project:write")
		case builtin.ToolGetProjectFact, builtin.ToolListProjectFacts, builtin.ToolSearchProjectFacts:
			return authorizeProjectTool(ctx, principal, db, "project:read")
		case builtin.ToolListKnowledgeRiskTypes, builtin.ToolSearchKnowledgeBase:
			return require("knowledge:read")
		case builtin.ToolAnalyzeImage:
			return require("agent:execute")
		case builtin.ToolGetToolExecution, builtin.ToolWaitToolExecution:
			return toolExecutionResource("monitor:read")
		case builtin.ToolCancelToolExecution:
			return toolExecutionResource("monitor:write")
		case builtin.ToolBatchTaskList:
			return require("tasks:read")
		case builtin.ToolBatchTaskGet:
			return resource("tasks:read", "batch_task", "queue_id")
		case builtin.ToolBatchTaskCreate:
			if err := require("tasks:write"); err != nil {
				return err
			}
			if projectID := mcpAuthorizationString(args, "project_id"); projectID != "" && (db == nil || !db.UserCanAccessResource(principal.UserID, principal.ScopeFor("tasks:write"), "project", projectID)) {
				return fmt.Errorf("no access to project %s", projectID)
			}
			return nil
		case builtin.ToolBatchTaskDelete, builtin.ToolBatchTaskRemove:
			return resource("tasks:delete", "batch_task", "queue_id")
		case builtin.ToolBatchTaskStart, builtin.ToolBatchTaskRerun, builtin.ToolBatchTaskPause,
			builtin.ToolBatchTaskUpdateMetadata, builtin.ToolBatchTaskUpdateSchedule,
			builtin.ToolBatchTaskScheduleEnabled, builtin.ToolBatchTaskAdd, builtin.ToolBatchTaskUpdate:
			return resource("tasks:write", "batch_task", "queue_id")
		case builtin.ToolC2Listener:
			return authorizeC2Action(ctx, principal, db, args, "c2_listener", "listener_id")
		case builtin.ToolC2Session, builtin.ToolC2Task, builtin.ToolC2File:
			if toolName == builtin.ToolC2File && mcpAuthorizationString(args, "action") == "get_result" {
				return authorizeC2Action(ctx, principal, db, args, "c2_task", "task_id")
			}
			return authorizeC2Action(ctx, principal, db, args, "c2_session", "session_id")
		case builtin.ToolC2TaskManage:
			return authorizeC2Action(ctx, principal, db, args, "c2_task", "task_id")
		case builtin.ToolC2Payload:
			return resource("c2:write", "c2_listener", "listener_id")
		case builtin.ToolC2Event:
			if id := mcpAuthorizationString(args, "session_id"); id != "" {
				return resource("c2:read", "c2_session", "session_id")
			}
			if id := mcpAuthorizationString(args, "task_id"); id != "" {
				return resource("c2:read", "c2_task", "task_id")
			}
			if filter := mcpEffectiveProjectFilter(ctx, db); filter != "" {
				return require("c2:read")
			}
			if principal.ScopeFor("c2:read") != database.RBACScopeAll {
				return fmt.Errorf("unfiltered C2 event list requires global scope")
			}
			return require("c2:read")
		case builtin.ToolC2Profile:
			// Profiles are process-global and do not yet have an owner. Writes are
			// therefore reserved for global scope; reads require c2:read.
			if mcpAuthorizationString(args, "action") == "list" || mcpAuthorizationString(args, "action") == "get" {
				return require("c2:read")
			}
			permission := "c2:write"
			if mcpAuthorizationString(args, "action") == "delete" {
				permission = "c2:delete"
			}
			if principal.ScopeFor(permission) != database.RBACScopeAll {
				return fmt.Errorf("C2 profile mutation requires global scope")
			}
			if mcpAuthorizationString(args, "action") == "delete" {
				return require("c2:delete")
			}
			return require("c2:write")
		default:
			if builtin.IsBuiltinTool(toolName) {
				return fmt.Errorf("no authorization policy registered for builtin tool %s", toolName)
			}
			if principal.HasPermission("agent:local-execute") {
				return nil
			}
			return fmt.Errorf("missing agent:local-execute")
		}
	}
}

func externalMCPToolAuthorizer() func(context.Context, string, map[string]interface{}) error {
	return func(ctx context.Context, toolName string, _ map[string]interface{}) error {
		principal, ok := authctx.PrincipalFromContext(ctx)
		if !ok {
			return fmt.Errorf("missing authenticated principal")
		}
		if !principal.HasPermission("mcp:external:execute") {
			return fmt.Errorf("missing permission mcp:external:execute")
		}
		if principal.ScopeFor("mcp:external:execute") != database.RBACScopeAll {
			return fmt.Errorf("external MCP invocation requires global scope")
		}
		if strings.TrimSpace(toolName) == "" {
			return fmt.Errorf("missing external tool name")
		}
		return nil
	}
}

func authorizeC2Action(ctx context.Context, principal authctx.Principal, db *database.DB, args map[string]interface{}, resourceType, argument string) error {
	action := mcpAuthorizationString(args, "action")
	permission := "c2:write"
	if action == "list" || action == "get" || action == "get_result" || action == "wait" {
		permission = "c2:read"
	} else if action == "delete" || action == "delete_batch" {
		permission = "c2:delete"
	}
	if !principal.HasPermission(permission) {
		return fmt.Errorf("missing permission %s", permission)
	}
	id := mcpAuthorizationString(args, argument)
	if action == "delete_batch" {
		ids := mcpAuthorizationStrings(args, argument+"s")
		if len(ids) == 0 {
			return fmt.Errorf("missing resource identifiers %ss", argument)
		}
		for _, candidate := range ids {
			if db == nil || !db.UserCanAccessResource(principal.UserID, principal.ScopeFor(permission), resourceType, candidate) {
				return fmt.Errorf("no access to %s %s", resourceType, candidate)
			}
			if err := authorizeMCPProjectResourceBoundary(ctx, db, resourceType, candidate); err != nil {
				return err
			}
		}
		return nil
	}
	if id == "" {
		if action == "create" {
			projectID := mcpAuthorizationString(args, "project_id")
			if projectID == "" {
				projectID = mcpEffectiveProjectFilter(ctx, db)
				if projectID == database.ProjectFilterUnbound {
					projectID = ""
				}
			}
			if projectID != "" && (db == nil || !db.UserCanAccessResource(principal.UserID, principal.ScopeFor(permission), "project", projectID)) {
				return fmt.Errorf("no access to project %s", projectID)
			}
			return nil
		}
		if action == "list" {
			return nil
		}
		return fmt.Errorf("missing resource identifier %s", argument)
	}
	if db == nil || !db.UserCanAccessResource(principal.UserID, principal.ScopeFor(permission), resourceType, id) {
		return fmt.Errorf("no access to %s %s", resourceType, id)
	}
	if err := authorizeMCPProjectResourceBoundary(ctx, db, resourceType, id); err != nil {
		return err
	}
	return nil
}

func authorizeMCPProjectResourceBoundary(ctx context.Context, db *database.DB, resourceType, resourceID string) error {
	filter := mcpEffectiveProjectFilter(ctx, db)
	if filter == "" || db == nil {
		return nil
	}
	projectID, ok, err := mcpResourceProjectID(db, resourceType, resourceID)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	if filter == database.ProjectFilterUnbound {
		if projectID != "" {
			return fmt.Errorf("resource %s %s belongs to project %s, current conversation is unbound", resourceType, resourceID, projectID)
		}
		return nil
	}
	if projectID != filter {
		if projectID == "" {
			return fmt.Errorf("resource %s %s is unbound, current conversation project is %s", resourceType, resourceID, filter)
		}
		return fmt.Errorf("resource %s %s belongs to project %s, current conversation project is %s", resourceType, resourceID, projectID, filter)
	}
	return nil
}

func mcpResourceProjectID(db *database.DB, resourceType, resourceID string) (string, bool, error) {
	switch resourceType {
	case "webshell":
		conn, err := db.GetWebshellConnection(resourceID)
		if err != nil {
			return "", true, err
		}
		if conn == nil {
			return "", true, fmt.Errorf("webshell not found")
		}
		return strings.TrimSpace(conn.ProjectID), true, nil
	case "c2_listener":
		listener, err := db.GetC2Listener(resourceID)
		if err != nil {
			return "", true, err
		}
		if listener == nil {
			return "", true, fmt.Errorf("listener not found")
		}
		return strings.TrimSpace(listener.ProjectID), true, nil
	case "c2_session":
		session, err := db.GetC2Session(resourceID)
		if err != nil {
			return "", true, err
		}
		if session == nil {
			return "", true, fmt.Errorf("session not found")
		}
		return mcpResourceProjectID(db, "c2_listener", session.ListenerID)
	case "c2_task":
		task, err := db.GetC2Task(resourceID)
		if err != nil {
			return "", true, err
		}
		if task == nil {
			return "", true, fmt.Errorf("task not found")
		}
		return mcpResourceProjectIDFromC2Session(db, task.SessionID)
	default:
		return "", false, nil
	}
}

func mcpResourceProjectIDFromC2Session(db *database.DB, sessionID string) (string, bool, error) {
	session, err := db.GetC2Session(sessionID)
	if err != nil {
		return "", true, err
	}
	if session == nil {
		return "", true, fmt.Errorf("session not found")
	}
	return mcpResourceProjectID(db, "c2_listener", session.ListenerID)
}

func mcpAuthorizationStrings(args map[string]interface{}, key string) []string {
	values := []string{}
	switch raw := args[key].(type) {
	case []string:
		for _, value := range raw {
			if value = strings.TrimSpace(value); value != "" {
				values = append(values, value)
			}
		}
	case []interface{}:
		for _, item := range raw {
			if value, ok := item.(string); ok {
				if value = strings.TrimSpace(value); value != "" {
					values = append(values, value)
				}
			}
		}
	}
	return values
}

func authorizeProjectTool(ctx context.Context, principal authctx.Principal, db *database.DB, permission string) error {
	if !principal.HasPermission(permission) {
		return fmt.Errorf("missing permission %s", permission)
	}
	conversationID := mcpAuthorizationConversationID(ctx)
	if conversationID == "" || db == nil || !db.UserCanAccessResource(principal.UserID, principal.ScopeFor(permission), "conversation", conversationID) {
		return fmt.Errorf("no access to conversation %s", conversationID)
	}
	projectID, err := db.GetConversationProjectID(conversationID)
	if err != nil {
		return fmt.Errorf("no access to project: %w", err)
	}
	if strings.TrimSpace(projectID) == "" {
		return fmt.Errorf("the current conversation is not bound to a project, so project blackboard tools cannot be used; first select a project in the conversation or create a conversation with a project")
	}
	if !db.UserCanAccessResource(principal.UserID, principal.ScopeFor(permission), "project", projectID) {
		return fmt.Errorf("no access to project %s", projectID)
	}
	return nil
}

func mcpAuthorizationConversationID(ctx context.Context) string {
	if id := strings.TrimSpace(agent.ConversationIDFromContext(ctx)); id != "" {
		return id
	}
	return strings.TrimSpace(mcp.MCPConversationIDFromContext(ctx))
}

func mcpAuthorizationString(args map[string]interface{}, key string) string {
	value, _ := args[key].(string)
	return strings.TrimSpace(value)
}
