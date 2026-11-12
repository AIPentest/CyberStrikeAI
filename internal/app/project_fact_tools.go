package app

import (
	"context"
	"fmt"
	"strings"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/mcp/builtin"
	"cyberstrike-ai/internal/project"

	"go.uber.org/zap"
)

func projectIDFromConversation(db *database.DB, ctx context.Context) (string, error) {
	convID := mcpAuthorizationConversationID(ctx)
	if convID == "" {
		return "", fmt.Errorf("cannot determine the current conversation; please use the project-fact tool within a conversation context")
	}
	pid, err := db.GetConversationProjectID(convID)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(pid) == "" {
		return "", fmt.Errorf("the current conversation is not bound to a project; please first select a project in the conversation or create a conversation with a project")
	}
	return pid, nil
}

func textResult(msg string, isErr bool) *mcp.ToolResult {
	return &mcp.ToolResult{
		Content: []mcp.Content{{Type: "text", Text: msg}},
		IsError: isErr,
	}
}

// registerProjectFactTools registers the project-blackboard MCP tools.
func registerProjectFactTools(mcpServer *mcp.Server, db *database.DB, cfg *config.Config, logger *zap.Logger) {
	if db == nil || cfg == nil || !cfg.Project.Enabled {
		if logger != nil {
			logger.Info("Project blackboard tools not registered (not enabled)")
		}
		return
	}

	upsertTool := mcp.Tool{
		Name: builtin.ToolUpsertProjectFact,
		Description: "Write or update a project-blackboard fact, used to accumulate reproducible context across sessions (not a formal vulnerability entry; use record_vulnerability for deliverable vulnerabilities). " +
			"Record as you go: call immediately after each confirmed new finding (port/entry/credential/exploitable point), overwrite-updating for the same fact_key; do not wait until session end. " +
			"Do not write only the conclusion: the summary must include what+where+how to verify; the body must include reproduction details such as the attack chain/request-response/commands. " +
			"For finding-type entries, the suggested fact_key is finding|chain|exploit|poc/<slug>, with category corresponding to finding|chain|exploit|poc, and the body filled in per the attack-chain template. " +
			"For script results, use category=note, artifact/<slug>, with the body recording the working-directory relative path, dependencies, invocation method, and success evidence; for test results, use category=note, test/<slug>, with the body recording the tested conditions, evidence, and retest trigger conditions, and do not use deprecated for negative conclusions. " +
			"For environment entries, use target|auth|infra|business/<slug>. Overwrite-update for the same fact_key. Requires the current conversation to be bound to a project.",
		ShortDescription: "Write/update a project fact (including attack-chain body)",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"fact_key": map[string]interface{}{
					"type":        "string",
					"description": "A key unique within the project: target/primary_domain, finding/sqli-login, exploit/upload-rce, etc.",
				},
				"category": map[string]interface{}{
					"type":        "string",
					"description": "target | auth | infra | business | finding | chain | exploit | poc | note",
					"enum":        []string{"target", "auth", "infra", "business", "finding", "chain", "exploit", "poc", "note"},
				},
				"summary": map[string]interface{}{
					"type":        "string",
					"description": "A one-line index: conclusion + location + trigger/verification key points (do not write empty phrases like \"XSS exists\")",
				},
				"body": map[string]interface{}{
					"type": "string",
					"description": "Complete reproducible details (returned only by get_project_fact): must include attack-chain steps, raw HTTP/commands, response behavior, evidence, and associations. " +
						"Required on the first write for finding/exploitation entries; for environment entries, including source evidence is recommended. For attack-chain entries, refer to the template sections: conclusion, target and entry, attack chain, Exploit/POC, key evidence, associations, notes. " +
						"When updating an existing fact_key, if the body is omitted or left empty, the existing body in the store is preserved (you can change only the summary).",
				},
				"confidence": map[string]interface{}{
					"type":        "string",
					"description": "confirmed | tentative | deprecated",
					"enum":        []string{"confirmed", "tentative", "deprecated"},
				},
				"pinned": map[string]interface{}{
					"type":        "boolean",
					"description": "Whether to appear first in the blackboard index",
				},
				"related_vulnerability_id": map[string]interface{}{
					"type":        "string",
					"description": "Optional: the associated vulnerability record ID",
				},
				"links": map[string]interface{}{
					"type":        "array",
					"description": "Optional: relationship edges (from → current fact). A finding needs at least 1 {from:target/*, type:discovered_on}; to record an exploit on a finding use {from:exploit/*, type:exploits}. Omit to keep existing edges; pass [] to clear all relationship edges.",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"from": map[string]interface{}{
								"type":        "string",
								"description": "Source fact_key: stored as from → current fact",
							},
							"type": map[string]interface{}{
								"type":        "string",
								"description": "depends_on | leads_to | enables | exploits | discovered_on | contains | part_of | supports",
							},
							"confidence": map[string]interface{}{
								"type":        "string",
								"description": "confirmed | tentative | deprecated",
							},
						},
						"required": []string{"from", "type"},
					},
				},
			},
			"required": []string{"fact_key", "summary"},
		},
	}

	mcpServer.RegisterTool(upsertTool, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		projectID, err := projectIDFromConversation(db, ctx)
		if err != nil {
			return textResult("Error: "+err.Error(), true), nil
		}
		factKey, _ := args["fact_key"].(string)
		summary, _ := args["summary"].(string)
		if database.IsContextCheckpointKey(factKey) {
			return textResult("Error: note/context-* are system continuation records and cannot be overwritten with the fact-write tool; use a separate fact_key for project-shared facts", true), nil
		}
		if strings.TrimSpace(factKey) == "" || strings.TrimSpace(summary) == "" {
			return textResult("Error: fact_key and summary are required", true), nil
		}
		if len([]rune(summary)) > cfg.Project.FactSummaryMaxRunesEffective() {
			return textResult(fmt.Sprintf("Error: summary too long (at most %d characters)", cfg.Project.FactSummaryMaxRunesEffective()), true), nil
		}
		f := &database.ProjectFact{
			ProjectID:              projectID,
			FactKey:                factKey,
			Category:               strArg(args, "category"),
			Summary:                summary,
			Body:                   strArg(args, "body"),
			Confidence:             strArg(args, "confidence"),
			Pinned:                 boolArg(args, "pinned"),
			RelatedVulnerabilityID: strArg(args, "related_vulnerability_id"),
		}
		if convID := mcpAuthorizationConversationID(ctx); convID != "" {
			f.SourceConversationID = convID
		}
		created, err := db.UpsertProjectFact(f)
		if err != nil {
			return textResult("Error: "+err.Error(), true), nil
		}
		if _, hasLinks := args["links"]; hasLinks {
			linkInputs, err := project.ParseFactLinkInputs(args["links"])
			if err != nil {
				return textResult("Error: "+err.Error(), true), nil
			}
			convID := mcpAuthorizationConversationID(ctx)
			if err := project.PersistFactLinksFromParsed(db, projectID, created.FactKey, convID, linkInputs, true); err != nil {
				return textResult("Error: failed to save relationship edges: "+err.Error(), true), nil
			}
			created, _ = db.GetProjectFactByKey(projectID, created.FactKey)
		} else if parsed := project.ParseLinksFromBody(created.Body); len(parsed) > 0 {
			if err := project.PersistFactIncomingLinks(db, projectID, created.FactKey, parsed, true); err != nil {
				return textResult("Error: failed to parse edges from body: "+err.Error(), true), nil
			}
			created, _ = db.GetProjectFactByKey(projectID, created.FactKey)
		}
		msg := fmt.Sprintf("Fact saved.\nfact_key: %s\nid: %s\nconfidence: %s", created.FactKey, created.ID, created.Confidence)
		if in, _ := db.ListIncomingProjectFactEdges(projectID, created.FactKey); len(in) > 0 {
			in = scopedProjectMemoryEdges(db, projectID, mcpAuthorizationConversationID(ctx), false, in)
			if len(in) > 0 {
				msg += "\nRelationship edges: " + project.FormatFactLinksText(in)
			}
		}
		if warn := project.SparseBodyWarningIfNeeded(f.Category, f.FactKey, f.Body); warn != "" {
			msg += warn
		}
		return textResult(msg, false), nil
	})

	getTool := mcp.Tool{
		Name:             builtin.ToolGetProjectFact,
		Description:      "Get the full body and metadata of a project fact by fact_key. By default it reads shared facts and the current session's continuation record; other sessions' continuations require an explicit include_history=true and are for historical reference only. Call it when the summary is insufficient; do not fabricate details.",
		ShortDescription: "Get fact details by key",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"include_history": map[string]interface{}{"type": "boolean", "description": "Explicitly query the automatic continuation records of other sessions in this project; default false, historical records are not the current task's instructions"},
				"fact_key":        map[string]interface{}{"type": "string", "description": "Fact key"},
			},
			"required": []string{"fact_key"},
		},
	}
	mcpServer.RegisterTool(getTool, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		projectID, err := projectIDFromConversation(db, ctx)
		if err != nil {
			return textResult("Error: "+err.Error(), true), nil
		}
		key := strings.TrimSpace(strArg(args, "fact_key"))
		if key == "" {
			return textResult("Error: fact_key is required", true), nil
		}
		f, err := db.GetProjectFactByKey(projectID, key)
		if err != nil {
			return textResult("Error: "+err.Error(), true), nil
		}
		conversationID := mcpAuthorizationConversationID(ctx)
		includeHistory := boolArg(args, "include_history")
		if !database.ContextCheckpointVisible(f, conversationID, includeHistory) {
			return textResult("This key is a historical continuation record from another session or an unknown source; to reference it, explicitly pass include_history=true. Historical tasks are not executed automatically.", true), nil
		}
		msg := fmt.Sprintf("fact_key: %s\ncategory: %s\nconfidence: %s\nsummary: %s\nupdated_at: %s",
			f.FactKey, f.Category, f.Confidence, f.Summary, f.UpdatedAt.Format("2006-01-02 15:04:05"))
		if f.RelatedVulnerabilityID != "" {
			msg += fmt.Sprintf("\nrelated_vulnerability_id: %s", f.RelatedVulnerabilityID)
		}
		if f.SourceConversationID != "" {
			msg += fmt.Sprintf("\nsource_conversation_id: %s", f.SourceConversationID)
		}
		if in, _ := db.ListIncomingProjectFactEdges(projectID, f.FactKey); len(in) > 0 {
			in = scopedProjectMemoryEdges(db, projectID, conversationID, includeHistory, in)
			msg += "\nRelationship edges (from → this fact):\n"
			for _, e := range in {
				msg += fmt.Sprintf("- %s ← %s (%s)\n", e.EdgeType, e.SourceFactKey, e.Confidence)
			}
		}
		if out, _ := db.ListOutgoingProjectFactEdges(projectID, f.FactKey); len(out) > 0 {
			out = scopedProjectMemoryEdges(db, projectID, conversationID, includeHistory, out)
			msg += "Pointing to other facts:\n"
			for _, e := range out {
				msg += fmt.Sprintf("- %s → %s (%s)\n", e.EdgeType, e.TargetFactKey, e.Confidence)
			}
		}
		msg += checkpointReadLabel(f, conversationID)
		msg += "\n\n--- body ---\n" + f.Body
		if warn := project.SparseBodyWarningIfNeeded(f.Category, f.FactKey, f.Body); warn != "" {
			msg += warn
		}
		return textResult(msg, false), nil
	})

	listTool := mcp.Tool{
		Name:             builtin.ToolListProjectFacts,
		Description:      "List the current project's shared facts and the current session's continuation records (paginated); include_history=true can explicitly query this project's historical continuations.",
		ShortDescription: "List project facts",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"source_conversation_id": map[string]interface{}{"type": "string", "description": "Optional: filter by source session; other sessions' continuations still require include_history=true"},
				"include_history":        map[string]interface{}{"type": "boolean", "description": "Explicitly query the automatic continuation records of other sessions in this project; default false, historical records are not the current task's instructions"},
				"category":               map[string]interface{}{"type": "string"},
				"confidence":             map[string]interface{}{"type": "string"},
				"limit":                  map[string]interface{}{"type": "integer"},
				"offset":                 map[string]interface{}{"type": "integer"},
			},
		},
	}
	mcpServer.RegisterTool(listTool, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		projectID, err := projectIDFromConversation(db, ctx)
		if err != nil {
			return textResult("Error: "+err.Error(), true), nil
		}
		limit := intArg(args, "limit", 50)
		offset := intArg(args, "offset", 0)
		filter := database.ProjectFactListFilter{
			Category:                   strArg(args, "category"),
			Confidence:                 strArg(args, "confidence"),
			RestrictContextCheckpoints: !boolArg(args, "include_history"),
			ContextConversationID:      mcpAuthorizationConversationID(ctx),
			SourceConversationID:       strArg(args, "source_conversation_id"),
		}
		list, err := db.ListProjectFacts(projectID, filter, limit, offset)
		if err != nil {
			return textResult("Error: "+err.Error(), true), nil
		}
		var b strings.Builder
		b.WriteString(fmt.Sprintf("%d total (limit=%d offset=%d):\n", len(list), limit, offset))
		for _, f := range list {
			b.WriteString(fmt.Sprintf("- [%s] %s — %s (%s)\n", f.FactKey, f.Category, f.Summary, f.Confidence))
			b.WriteString(checkpointReadLabel(f, mcpAuthorizationConversationID(ctx)))
		}
		return textResult(b.String(), false), nil
	})

	searchTool := mcp.Tool{
		Name:             builtin.ToolSearchProjectFacts,
		Description:      "Search the current project's shared facts and the current session's continuations by keyword (summary/body/fact_key); other sessions' continuations require include_history=true.",
		ShortDescription: "Search project facts",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"source_conversation_id": map[string]interface{}{"type": "string", "description": "Optional: filter by source session; other sessions' continuations still require include_history=true"},
				"include_history":        map[string]interface{}{"type": "boolean", "description": "Explicitly query the automatic continuation records of other sessions in this project; default false, historical records are not the current task's instructions"},
				"query":                  map[string]interface{}{"type": "string"},
				"limit":                  map[string]interface{}{"type": "integer"},
				"offset":                 map[string]interface{}{"type": "integer"},
			},
			"required": []string{"query"},
		},
	}
	mcpServer.RegisterTool(searchTool, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		projectID, err := projectIDFromConversation(db, ctx)
		if err != nil {
			return textResult("Error: "+err.Error(), true), nil
		}
		q := strings.TrimSpace(strArg(args, "query"))
		if q == "" {
			return textResult("Error: query is required", true), nil
		}
		list, err := db.ListProjectFacts(projectID, database.ProjectFactListFilter{Search: q, RestrictContextCheckpoints: !boolArg(args, "include_history"), ContextConversationID: mcpAuthorizationConversationID(ctx), SourceConversationID: strArg(args, "source_conversation_id")}, intArg(args, "limit", 30), intArg(args, "offset", 0))
		if err != nil {
			return textResult("Error: "+err.Error(), true), nil
		}
		var b strings.Builder
		b.WriteString(fmt.Sprintf("Search \"%s\" matched %d entries:\n", q, len(list)))
		for _, f := range list {
			b.WriteString(fmt.Sprintf("- [%s] %s — %s\n", f.FactKey, f.Category, f.Summary))
			b.WriteString(checkpointReadLabel(f, mcpAuthorizationConversationID(ctx)))
		}
		return textResult(b.String(), false), nil
	})

	deprecateTool := mcp.Tool{
		Name:             builtin.ToolDeprecateProjectFact,
		Description:      "Mark a fact as deprecated, excluding it from the blackboard index.",
		ShortDescription: "Deprecate a project fact",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"fact_key": map[string]interface{}{"type": "string"},
			},
			"required": []string{"fact_key"},
		},
	}
	mcpServer.RegisterTool(deprecateTool, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		projectID, err := projectIDFromConversation(db, ctx)
		if err != nil {
			return textResult("Error: "+err.Error(), true), nil
		}
		key := strings.TrimSpace(strArg(args, "fact_key"))
		if database.IsContextCheckpointKey(key) {
			f, err := db.GetProjectFactByKey(projectID, key)
			if err != nil || !database.ContextCheckpointVisible(f, mcpAuthorizationConversationID(ctx), false) {
				return textResult("Error: cannot modify the automatic continuation records of other sessions", true), nil
			}
		}
		if err := db.DeprecateProjectFact(projectID, key); err != nil {
			return textResult("Error: "+err.Error(), true), nil
		}
		return textResult("Fact marked as deprecated: "+key, false), nil
	})

	restoreTool := mcp.Tool{
		Name:             builtin.ToolRestoreProjectFact,
		Description:      "Restore a deprecated fact to tentative or confirmed, re-including it in the blackboard index.",
		ShortDescription: "Restore a deprecated project fact",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"fact_key": map[string]interface{}{"type": "string"},
				"confidence": map[string]interface{}{
					"type":        "string",
					"description": "Confidence after restoration: tentative (default) or confirmed",
					"enum":        []string{"tentative", "confirmed"},
				},
			},
			"required": []string{"fact_key"},
		},
	}
	mcpServer.RegisterTool(restoreTool, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		projectID, err := projectIDFromConversation(db, ctx)
		if err != nil {
			return textResult("Error: "+err.Error(), true), nil
		}
		key := strings.TrimSpace(strArg(args, "fact_key"))
		if database.IsContextCheckpointKey(key) {
			f, err := db.GetProjectFactByKey(projectID, key)
			if err != nil || !database.ContextCheckpointVisible(f, mcpAuthorizationConversationID(ctx), false) {
				return textResult("Error: cannot modify the automatic continuation records of other sessions", true), nil
			}
		}
		if key == "" {
			return textResult("Error: fact_key is required", true), nil
		}
		conf := strArg(args, "confidence")
		if err := db.RestoreProjectFact(projectID, key, conf); err != nil {
			return textResult("Error: "+err.Error(), true), nil
		}
		if conf == "" {
			conf = "tentative"
		}
		return textResult(fmt.Sprintf("Fact restored to %s: %s", conf, key), false), nil
	})

	if logger != nil {
		logger.Debug("Project blackboard MCP tools registered successfully")
	}
}

func strArg(args map[string]interface{}, key string) string {
	if v, ok := args[key].(string); ok {
		return v
	}
	return ""
}

func boolArg(args map[string]interface{}, key string) bool {
	if v, ok := args[key].(bool); ok {
		return v
	}
	return false
}

func intArg(args map[string]interface{}, key string, def int) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	default:
		return def
	}
}

// History is a read opt-in, not promotion to a confirmed fact or new authorization.
func checkpointReadLabel(f *database.ProjectFact, conversationID string) string {
	if !database.IsContextCheckpointKey(f.FactKey) {
		return ""
	}
	label := "Current session work record"
	if strings.TrimSpace(conversationID) == "" || f.SourceConversationID != conversationID {
		label = "Historical reference, not the current task; verify the current authorization, target, and credential validity"
	}
	source := f.SourceConversationID
	if source == "" {
		source = "Unknown source"
	}
	return fmt.Sprintf("\n[Continuation record: %s; source session=%s; updated at=%s]\n", label, source, f.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"))
}

func scopedProjectMemoryEdges(db *database.DB, projectID, conversationID string, includeHistory bool, edges []*database.ProjectFactEdge) []*database.ProjectFactEdge {
	if includeHistory {
		return edges
	}
	var visible []*database.ProjectFactEdge
	allowed := make(map[string]bool)
	for _, e := range edges {
		keep := true
		for _, key := range []string{e.SourceFactKey, e.TargetFactKey} {
			if !database.IsContextCheckpointKey(key) {
				continue
			}
			ok, seen := allowed[key]
			if !seen {
				f, err := db.GetProjectFactByKey(projectID, key)
				ok = err == nil && database.ContextCheckpointVisible(f, conversationID, false)
				allowed[key] = ok
			}
			if !ok {
				keep = false
				break
			}
		}
		if keep {
			visible = append(visible, e)
		}
	}
	return visible
}
