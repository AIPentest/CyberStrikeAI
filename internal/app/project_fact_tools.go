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
		return "", fmt.Errorf("无法确定当前对话，请在对话上下文中使用项目事实工具")
	}
	pid, err := db.GetConversationProjectID(convID)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(pid) == "" {
		return "", fmt.Errorf("当前对话未绑定项目，请先在对话中选择项目或创建带项目的对话")
	}
	return pid, nil
}

func textResult(msg string, isErr bool) *mcp.ToolResult {
	return &mcp.ToolResult{
		Content: []mcp.Content{{Type: "text", Text: msg}},
		IsError: isErr,
	}
}

// registerProjectFactTools 注册项目黑板 MCP 工具。
func registerProjectFactTools(mcpServer *mcp.Server, db *database.DB, cfg *config.Config, logger *zap.Logger) {
	if db == nil || cfg == nil || !cfg.Project.Enabled {
		if logger != nil {
			logger.Info("项目黑板工具未注册（未启用）")
		}
		return
	}

	upsertTool := mcp.Tool{
		Name: builtin.ToolUpsertProjectFact,
		Description: "写入或更新项目黑板事实，用于跨会话沉淀可复现上下文（非正式漏洞条目；可交付漏洞另用 record_vulnerability）。" +
			"边渗透边记录：每确认新认知（端口/入口/凭据/可利用点）后立即调用，同 fact_key 覆盖更新，勿等会话结束。" +
			"禁止仅写结论：summary 须含什么+在哪+如何验证；body 须含攻击链/请求响应/命令等复现细节。" +
			"发现类建议 fact_key 为 finding|chain|exploit|poc/<slug>，category 对应 finding|chain|exploit|poc，body 按攻击链模板填写。" +
			"脚本成果用 category=note、artifact/<slug>，body 记工作目录相对路径、依赖、调用方式与成功证据；测试结果用 category=note、test/<slug>，body 记已测条件、证据及复测触发条件，阴性结论勿用 deprecated。" +
			"环境类用 target|auth|infra|business/<slug>。同 fact_key 覆盖更新。需当前对话已绑定项目。",
		ShortDescription: "写入/更新项目事实（含攻击链 body）",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"fact_key": map[string]interface{}{
					"type":        "string",
					"description": "项目内唯一 key：target/primary_domain、finding/sqli-login、exploit/upload-rce 等",
				},
				"category": map[string]interface{}{
					"type":        "string",
					"description": "target | auth | infra | business | finding | chain | exploit | poc | note",
					"enum":        []string{"target", "auth", "infra", "business", "finding", "chain", "exploit", "poc", "note"},
				},
				"summary": map[string]interface{}{
					"type":        "string",
					"description": "索引用一行：结论 + 位置 + 触发/验证要点（勿仅写「存在 XSS」等空话）",
				},
				"body": map[string]interface{}{
					"type": "string",
					"description": "完整可复现详情（仅 get_project_fact 返回）：须含攻击链步骤、原始 HTTP/命令、响应现象、证据与关联。" +
						"发现/利用类首次写入必填；环境类建议含来源证据。攻击链类可参考模板章节：结论、目标与入口、攻击链、Exploit/POC、关键证据、关联、备注。" +
						"更新已有 fact_key 时若省略或留空 body，将保留库中已有 body（可只改 summary）。",
				},
				"confidence": map[string]interface{}{
					"type":        "string",
					"description": "confirmed | tentative | deprecated",
					"enum":        []string{"confirmed", "tentative", "deprecated"},
				},
				"pinned": map[string]interface{}{
					"type":        "boolean",
					"description": "是否优先出现在黑板索引",
				},
				"related_vulnerability_id": map[string]interface{}{
					"type":        "string",
					"description": "可选：关联的漏洞记录 ID",
				},
				"links": map[string]interface{}{
					"type":        "array",
					"description": "可选：关系边（from → 当前 fact）。finding 至少 1 条 {from:target/*, type:discovered_on}；finding 上记录 exploit 用 {from:exploit/*, type:exploits}。省略保留已有边；传 [] 清空全部关系边。",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"from": map[string]interface{}{
								"type":        "string",
								"description": "来源 fact_key：存储为 from → 当前 fact",
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
			return textResult("错误: "+err.Error(), true), nil
		}
		factKey, _ := args["fact_key"].(string)
		summary, _ := args["summary"].(string)
		if database.IsContextCheckpointKey(factKey) {
			return textResult("错误: note/context-* 为系统续接记录，不能用事实写入工具覆盖；项目共享事实请使用独立 fact_key", true), nil
		}
		if strings.TrimSpace(factKey) == "" || strings.TrimSpace(summary) == "" {
			return textResult("错误: fact_key 与 summary 必填", true), nil
		}
		if len([]rune(summary)) > cfg.Project.FactSummaryMaxRunesEffective() {
			return textResult(fmt.Sprintf("错误: summary 过长（最多 %d 字）", cfg.Project.FactSummaryMaxRunesEffective()), true), nil
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
			return textResult("错误: "+err.Error(), true), nil
		}
		if _, hasLinks := args["links"]; hasLinks {
			linkInputs, err := project.ParseFactLinkInputs(args["links"])
			if err != nil {
				return textResult("错误: "+err.Error(), true), nil
			}
			convID := mcpAuthorizationConversationID(ctx)
			if err := project.PersistFactLinksFromParsed(db, projectID, created.FactKey, convID, linkInputs, true); err != nil {
				return textResult("错误: 保存关系边失败: "+err.Error(), true), nil
			}
			created, _ = db.GetProjectFactByKey(projectID, created.FactKey)
		} else if parsed := project.ParseLinksFromBody(created.Body); len(parsed) > 0 {
			if err := project.PersistFactIncomingLinks(db, projectID, created.FactKey, parsed, true); err != nil {
				return textResult("错误: 从 body 解析边失败: "+err.Error(), true), nil
			}
			created, _ = db.GetProjectFactByKey(projectID, created.FactKey)
		}
		msg := fmt.Sprintf("事实已保存。\nfact_key: %s\nid: %s\nconfidence: %s", created.FactKey, created.ID, created.Confidence)
		if in, _ := db.ListIncomingProjectFactEdges(projectID, created.FactKey); len(in) > 0 {
			in = scopedProjectMemoryEdges(db, projectID, mcpAuthorizationConversationID(ctx), false, in)
			if len(in) > 0 {
				msg += "\n关系边: " + project.FormatFactLinksText(in)
			}
		}
		if warn := project.SparseBodyWarningIfNeeded(f.Category, f.FactKey, f.Body); warn != "" {
			msg += warn
		}
		return textResult(msg, false), nil
	})

	getTool := mcp.Tool{
		Name:             builtin.ToolGetProjectFact,
		Description:      "按 fact_key 获取项目事实完整 body 与元数据。默认读取共享事实和当前会话续接记录；其他会话续接须显式 include_history=true，仅作历史参考。摘要不足时调用，禁止臆造细节。",
		ShortDescription: "按 key 获取事实详情",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"include_history": map[string]interface{}{"type": "boolean", "description": "显式查询本项目其他会话的自动续接记录；默认 false，历史记录不是当前任务指令"},
				"fact_key":        map[string]interface{}{"type": "string", "description": "事实 key"},
			},
			"required": []string{"fact_key"},
		},
	}
	mcpServer.RegisterTool(getTool, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		projectID, err := projectIDFromConversation(db, ctx)
		if err != nil {
			return textResult("错误: "+err.Error(), true), nil
		}
		key := strings.TrimSpace(strArg(args, "fact_key"))
		if key == "" {
			return textResult("错误: fact_key 必填", true), nil
		}
		f, err := db.GetProjectFactByKey(projectID, key)
		if err != nil {
			return textResult("错误: "+err.Error(), true), nil
		}
		conversationID := mcpAuthorizationConversationID(ctx)
		includeHistory := boolArg(args, "include_history")
		if !database.ContextCheckpointVisible(f, conversationID, includeHistory) {
			return textResult("该 key 是其他会话或未知来源的历史续接记录；如需参考，请显式传 include_history=true。不会自动执行历史任务。", true), nil
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
			msg += "\n关系边（from → 本 fact）:\n"
			for _, e := range in {
				msg += fmt.Sprintf("- %s ← %s (%s)\n", e.EdgeType, e.SourceFactKey, e.Confidence)
			}
		}
		if out, _ := db.ListOutgoingProjectFactEdges(projectID, f.FactKey); len(out) > 0 {
			out = scopedProjectMemoryEdges(db, projectID, conversationID, includeHistory, out)
			msg += "指向其他事实:\n"
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
		Description:      "列出当前项目共享事实与当前会话续接记录（分页）；include_history=true 可显式查本项目历史续接。",
		ShortDescription: "列出项目事实",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"source_conversation_id": map[string]interface{}{"type": "string", "description": "可选：按来源会话筛选；其他会话续接仍需 include_history=true"},
				"include_history":        map[string]interface{}{"type": "boolean", "description": "显式查询本项目其他会话的自动续接记录；默认 false，历史记录不是当前任务指令"},
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
			return textResult("错误: "+err.Error(), true), nil
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
			return textResult("错误: "+err.Error(), true), nil
		}
		var b strings.Builder
		b.WriteString(fmt.Sprintf("共 %d 条（limit=%d offset=%d）:\n", len(list), limit, offset))
		for _, f := range list {
			b.WriteString(fmt.Sprintf("- [%s] %s — %s (%s)\n", f.FactKey, f.Category, f.Summary, f.Confidence))
			b.WriteString(checkpointReadLabel(f, mcpAuthorizationConversationID(ctx)))
		}
		return textResult(b.String(), false), nil
	})

	searchTool := mcp.Tool{
		Name:             builtin.ToolSearchProjectFacts,
		Description:      "按关键词搜索本项目共享事实和当前会话续接（summary/body/fact_key）；其他会话续接需 include_history=true。",
		ShortDescription: "搜索项目事实",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"source_conversation_id": map[string]interface{}{"type": "string", "description": "可选：按来源会话筛选；其他会话续接仍需 include_history=true"},
				"include_history":        map[string]interface{}{"type": "boolean", "description": "显式查询本项目其他会话的自动续接记录；默认 false，历史记录不是当前任务指令"},
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
			return textResult("错误: "+err.Error(), true), nil
		}
		q := strings.TrimSpace(strArg(args, "query"))
		if q == "" {
			return textResult("错误: query 必填", true), nil
		}
		list, err := db.ListProjectFacts(projectID, database.ProjectFactListFilter{Search: q, RestrictContextCheckpoints: !boolArg(args, "include_history"), ContextConversationID: mcpAuthorizationConversationID(ctx), SourceConversationID: strArg(args, "source_conversation_id")}, intArg(args, "limit", 30), intArg(args, "offset", 0))
		if err != nil {
			return textResult("错误: "+err.Error(), true), nil
		}
		var b strings.Builder
		b.WriteString(fmt.Sprintf("搜索 \"%s\" 命中 %d 条:\n", q, len(list)))
		for _, f := range list {
			b.WriteString(fmt.Sprintf("- [%s] %s — %s\n", f.FactKey, f.Category, f.Summary))
			b.WriteString(checkpointReadLabel(f, mcpAuthorizationConversationID(ctx)))
		}
		return textResult(b.String(), false), nil
	})

	deprecateTool := mcp.Tool{
		Name:             builtin.ToolDeprecateProjectFact,
		Description:      "将事实标记为 deprecated，从黑板索引中排除。",
		ShortDescription: "废弃项目事实",
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
			return textResult("错误: "+err.Error(), true), nil
		}
		key := strings.TrimSpace(strArg(args, "fact_key"))
		if database.IsContextCheckpointKey(key) {
			f, err := db.GetProjectFactByKey(projectID, key)
			if err != nil || !database.ContextCheckpointVisible(f, mcpAuthorizationConversationID(ctx), false) {
				return textResult("错误: 不能修改其他会话的自动续接记录", true), nil
			}
		}
		if err := db.DeprecateProjectFact(projectID, key); err != nil {
			return textResult("错误: "+err.Error(), true), nil
		}
		return textResult("事实已标记为 deprecated: "+key, false), nil
	})

	restoreTool := mcp.Tool{
		Name:             builtin.ToolRestoreProjectFact,
		Description:      "将已废弃（deprecated）的事实恢复为 tentative 或 confirmed，重新参与黑板索引。",
		ShortDescription: "恢复已废弃的项目事实",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"fact_key": map[string]interface{}{"type": "string"},
				"confidence": map[string]interface{}{
					"type":        "string",
					"description": "恢复后的置信度：tentative（默认）或 confirmed",
					"enum":        []string{"tentative", "confirmed"},
				},
			},
			"required": []string{"fact_key"},
		},
	}
	mcpServer.RegisterTool(restoreTool, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		projectID, err := projectIDFromConversation(db, ctx)
		if err != nil {
			return textResult("错误: "+err.Error(), true), nil
		}
		key := strings.TrimSpace(strArg(args, "fact_key"))
		if database.IsContextCheckpointKey(key) {
			f, err := db.GetProjectFactByKey(projectID, key)
			if err != nil || !database.ContextCheckpointVisible(f, mcpAuthorizationConversationID(ctx), false) {
				return textResult("错误: 不能修改其他会话的自动续接记录", true), nil
			}
		}
		if key == "" {
			return textResult("错误: fact_key 必填", true), nil
		}
		conf := strArg(args, "confidence")
		if err := db.RestoreProjectFact(projectID, key, conf); err != nil {
			return textResult("错误: "+err.Error(), true), nil
		}
		if conf == "" {
			conf = "tentative"
		}
		return textResult(fmt.Sprintf("事实已恢复为 %s: %s", conf, key), false), nil
	})

	if logger != nil {
		logger.Debug("项目黑板 MCP 工具注册成功")
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
	label := "当前会话工作记录"
	if strings.TrimSpace(conversationID) == "" || f.SourceConversationID != conversationID {
		label = "历史参考，非当前任务；核对当前授权、目标及凭据有效性"
	}
	source := f.SourceConversationID
	if source == "" {
		source = "未知来源"
	}
	return fmt.Sprintf("\n[续接记录：%s；来源会话=%s；更新时间=%s]\n", label, source, f.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"))
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
