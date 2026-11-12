package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"cyberstrike-ai/internal/authctx"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/mcp/builtin"

	"go.uber.org/zap"
)

// RegisterBatchTaskMCPTools 注册批量任务队列相关 MCP 工具（需传入已初始化 DB 的 AgentHandler）
func RegisterBatchTaskMCPTools(mcpServer *mcp.Server, h *AgentHandler, logger *zap.Logger) {
	if mcpServer == nil || h == nil || logger == nil {
		return
	}

	reg := func(tool mcp.Tool, fn func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error)) {
		mcpServer.RegisterTool(tool, fn)
	}

	// --- list ---
	reg(mcp.Tool{
		Name:             builtin.ToolBatchTaskList,
		Description:      "列出批量任务队列（精简摘要，省上下文）。含队列元数据、子任务 id/status/截断后的 message、各状态计数。完整子任务（含 result/error/conversationId/时间等）请用 batch_task_get(queue_id)。\n\n⚠️ 调用约束：本工具属于「任务管理」模块，仅当用户明确提及查看/管理批量任务、任务队列时才可调用。不要在用户未要求时自行调用。",
		ShortDescription: "列出批量任务队列",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"status": map[string]interface{}{
					"type":        "string",
					"description": "筛选状态：all（默认）、pending、running、paused、completed、cancelled",
					"enum":        []string{"all", "pending", "running", "paused", "completed", "cancelled"},
				},
				"keyword": map[string]interface{}{
					"type":        "string",
					"description": "按队列 ID 或标题模糊搜索",
				},
				"page": map[string]interface{}{
					"type":        "integer",
					"description": "页码，从 1 开始，默认 1",
				},
				"page_size": map[string]interface{}{
					"type":        "integer",
					"description": "每页条数，默认 20，最大 100",
				},
			},
		},
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		status := mcpArgString(args, "status")
		if status == "" {
			status = "all"
		}
		keyword := mcpArgString(args, "keyword")
		page := int(mcpArgFloat(args, "page"))
		if page <= 0 {
			page = 1
		}
		pageSize := int(mcpArgFloat(args, "page_size"))
		if pageSize <= 0 {
			pageSize = 20
		}
		if pageSize > 100 {
			pageSize = 100
		}
		offset := (page - 1) * pageSize
		if offset > 100000 {
			offset = 100000
		}
		queues := []*BatchTaskQueue{}
		total := 0
		var err error
		if principal, ok := authctx.PrincipalFromContext(ctx); ok {
			queues, total, err = h.batchTaskManager.ListQueuesForAccess(pageSize, offset, status, keyword, principal.UserID, principal.ScopeFor("tasks:read"))
		} else {
			return batchMCPTextResult("缺少认证身份", true), nil
		}
		if err != nil {
			return batchMCPTextResult(fmt.Sprintf("列出队列失败: %v", err), true), nil
		}
		totalPages := (total + pageSize - 1) / pageSize
		if totalPages == 0 {
			totalPages = 1
		}
		slim := make([]batchTaskQueueMCPListItem, 0, len(queues))
		for _, q := range queues {
			if q == nil {
				continue
			}
			slim = append(slim, toBatchTaskQueueMCPListItem(q))
		}
		payload := map[string]interface{}{
			"queues":      slim,
			"total":       total,
			"page":        page,
			"page_size":   pageSize,
			"total_pages": totalPages,
		}
		logger.Info("MCP batch_task_list", zap.String("status", status), zap.Int("total", total))
		return batchMCPJSONResult(payload)
	})

	// --- get ---
	reg(mcp.Tool{
		Name:             builtin.ToolBatchTaskGet,
		Description:      "根据 queue_id 获取单个批量任务队列详情（含子任务列表、Cron、调度开关与最近错误信息）。\n\n⚠️ 调用约束：本工具属于「任务管理」模块，仅当用户明确提及查看/管理批量任务、任务队列时才可调用。不要在用户未要求时自行调用。",
		ShortDescription: "获取批量任务队列详情",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"queue_id": map[string]interface{}{
					"type":        "string",
					"description": "队列 ID",
				},
			},
			"required": []string{"queue_id"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		qid := mcpArgString(args, "queue_id")
		if qid == "" {
			return batchMCPTextResult("queue_id 不能为空", true), nil
		}
		queue, ok := h.batchTaskManager.GetBatchQueue(qid)
		if !ok {
			return batchMCPTextResult("队列不存在: "+qid, true), nil
		}
		return batchMCPJSONResult(queue)
	})

	// --- create ---
	reg(mcp.Tool{
		Name: builtin.ToolBatchTaskCreate,
		Description: `⚠️ 调用约束：本工具属于「任务管理」模块，仅当用户明确要求创建批量任务、任务队列时才可调用。禁止在用户未提及”批量任务””任务队列””定时任务”等关键词时自行调用。如果用户只是让你做某件事，请在当前对话中直接完成，不要自作主张创建任务队列。

【用途】应用内「任务管理 / 批量任务队列」：把多条彼此独立的用户指令登记成一条队列，便于在界面里查看进度、暂停/继续、定时重跑等。这是队列数据与调度入口，不是再开一个”子代理会话”替你探索当前问题。

【何时用】用户明确要批量排队执行、Cron 周期跑同一批指令、或需要与任务管理页面对齐时调用。需要即时追问、强依赖当前对话上下文的分析/编码，应在本对话内直接完成，不要为了”委派”而创建队列。

【参数】tasks（字符串数组）或 tasks_text（多行，每行一条）二选一；每项是一条将来由系统按队列顺序执行的指令文案。agent_mode：single（Pi 单代理，默认）、deep / plan_execute / supervisor（需系统启用多代理）。非”把主对话拆给子代理”。schedule_mode：manual（默认）或 cron；cron 须填 cron_expr（5 段，如 “0 */6 * * *”）。

【执行】默认创建后为 pending，不自动跑。execute_now=true 可创建后立即跑；否则之后调用 batch_task_start。Cron 自动下一轮需 schedule_enabled 为 true（可用 batch_task_schedule_enabled）。`,
		ShortDescription: "任务管理：创建批量任务队列（登记多条指令，可选立即或 Cron）",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"title": map[string]interface{}{
					"type":        "string",
					"description": "可选队列标题，便于在任务管理中识别",
				},
				"role": map[string]interface{}{
					"type":        "string",
					"description": "队列使用的角色名，空表示默认",
				},
				"tasks": map[string]interface{}{
					"type":        "array",
					"description": "队列中的子任务指令，每项一条独立待执行文案（与 tasks_text 二选一）",
					"items":       map[string]interface{}{"type": "string"},
				},
				"tasks_text": map[string]interface{}{
					"type":        "string",
					"description": "多行文本，每行一条子任务指令（与 tasks 二选一）",
				},
				"agent_mode": map[string]interface{}{
					"type":        "string",
					"description": "执行模式：single（Pi，默认）、deep/plan_execute/supervisor（Agent 编排，需启用多代理）",
					"enum":        []string{"single", "deep", "plan_execute", "supervisor"},
				},
				"schedule_mode": map[string]interface{}{
					"type":        "string",
					"description": "manual（仅手工/启动后跑）或 cron（按表达式触发）",
					"enum":        []string{"manual", "cron"},
				},
				"cron_expr": map[string]interface{}{
					"type":        "string",
					"description": "schedule_mode 为 cron 时必填。标准 5 段：分钟 小时 日 月 星期，例如 \"0 */6 * * *\"、\"30 2 * * 1-5\"",
				},
				"execute_now": map[string]interface{}{
					"type":        "boolean",
					"description": "创建后是否立即开始执行队列，默认 false（pending，需 batch_task_start）",
				},
				"project_id": map[string]interface{}{
					"type":        "string",
					"description": "队列内子对话绑定的项目 ID（可选，未指定时使用 config.project.default_project_id）",
				},
				"concurrency": map[string]interface{}{
					"type":        "integer",
					"description": "同时执行的子任务数，默认 1（串行），最大 8。含扫描类工具时建议 1-2。",
				},
			},
		},
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		tasks, errMsg := batchMCPTasksFromArgs(args)
		if errMsg != "" {
			return batchMCPTextResult(errMsg, true), nil
		}
		title := mcpArgString(args, "title")
		role := mcpArgString(args, "role")
		agentMode := config.NormalizeAgentMode(mcpArgString(args, "agent_mode"))
		scheduleMode := normalizeBatchQueueScheduleMode(mcpArgString(args, "schedule_mode"))
		cronExpr := strings.TrimSpace(mcpArgString(args, "cron_expr"))
		var nextRunAt *time.Time
		if scheduleMode == "cron" {
			if cronExpr == "" {
				return batchMCPTextResult("Cron 调度模式下 cron_expr 不能为空", true), nil
			}
			sch, err := h.batchCronParser.Parse(cronExpr)
			if err != nil {
				return batchMCPTextResult("无效的 Cron 表达式: "+err.Error(), true), nil
			}
			n := sch.Next(time.Now())
			nextRunAt = &n
		}
		executeNow, ok := mcpArgBool(args, "execute_now")
		if !ok {
			executeNow = false
		}
		projectID := strings.TrimSpace(mcpArgString(args, "project_id"))
		if principal, ok := authctx.PrincipalFromContext(ctx); ok && projectID != "" && principal.ScopeFor("tasks:write") != database.RBACScopeAll {
			if h.db == nil || !h.db.UserCanAccessResource(principal.UserID, principal.ScopeFor("tasks:write"), "project", projectID) {
				return batchMCPTextResult("无权访问目标项目", true), nil
			}
		}
		concurrency := int(mcpArgFloat(args, "concurrency"))
		queue, createErr := h.batchTaskManager.CreateBatchQueue(title, role, agentMode, scheduleMode, cronExpr, projectID, nextRunAt, concurrency, tasks)
		if createErr != nil {
			return batchMCPTextResult("创建队列失败: "+createErr.Error(), true), nil
		}
		if principal, ok := authctx.PrincipalFromContext(ctx); ok && h.db != nil {
			_ = h.db.SetResourceOwner("batch_task", queue.ID, principal.UserID)
			_ = h.db.AssignResourceToUser(principal.UserID, "batch_task", queue.ID)
		}
		started := false
		if executeNow {
			ok, err := h.startBatchQueueExecution(queue.ID, false)
			if !ok {
				return batchMCPTextResult("队列不存在: "+queue.ID, true), nil
			}
			if err != nil {
				return batchMCPTextResult("创建成功但启动失败: "+err.Error(), true), nil
			}
			started = true
			if refreshed, exists := h.batchTaskManager.GetBatchQueue(queue.ID); exists {
				queue = refreshed
			}
		}
		logger.Info("MCP batch_task_create", zap.String("queueId", queue.ID), zap.Int("taskCount", len(tasks)))
		return batchMCPJSONResult(map[string]interface{}{
			"queue_id":    queue.ID,
			"queue":       queue,
			"started":     started,
			"execute_now": executeNow,
			"reminder": func() string {
				if started {
					return "队列已创建并立即启动。"
				}
				return "队列已创建，当前为 pending。需要开始执行时请调用 MCP 工具 batch_task_start（queue_id 同上）。Cron 自动调度需 schedule_enabled 为 true，可用 batch_task_schedule_enabled。"
			}(),
		})
	})

	// --- start ---
	reg(mcp.Tool{
		Name: builtin.ToolBatchTaskStart,
		Description: `启动或继续执行批量任务队列（pending / paused）。
与 batch_task_create 配合使用：仅创建队列不会自动执行，需调用本工具才会开始跑子任务。

⚠️ 调用约束：本工具属于「任务管理」模块，仅当用户明确要求启动/继续批量任务时才可调用。不要在用户未要求时自行调用。`,
		ShortDescription: "启动/继续批量任务队列（创建后需调用才会执行）",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"queue_id": map[string]interface{}{
					"type":        "string",
					"description": "队列 ID",
				},
			},
			"required": []string{"queue_id"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		qid := mcpArgString(args, "queue_id")
		if qid == "" {
			return batchMCPTextResult("queue_id 不能为空", true), nil
		}
		ok, err := h.startBatchQueueExecution(qid, false)
		if !ok {
			return batchMCPTextResult("队列不存在: "+qid, true), nil
		}
		if err != nil {
			return batchMCPTextResult("启动失败: "+err.Error(), true), nil
		}
		logger.Info("MCP batch_task_start", zap.String("queueId", qid))
		return batchMCPTextResult("已提交启动，队列将开始执行。", false), nil
	})

	// --- rerun (reset + start for completed/cancelled queues) ---
	reg(mcp.Tool{
		Name:             builtin.ToolBatchTaskRerun,
		Description:      "重跑已完成或已取消的批量任务队列。会重置所有子任务状态后重新执行一轮。\n\n⚠️ 调用约束：本工具属于「任务管理」模块，仅当用户明确要求重跑批量任务时才可调用。不要在用户未要求时自行调用。",
		ShortDescription: "重跑批量任务队列",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"queue_id": map[string]interface{}{
					"type":        "string",
					"description": "队列 ID",
				},
			},
			"required": []string{"queue_id"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		qid := mcpArgString(args, "queue_id")
		if qid == "" {
			return batchMCPTextResult("queue_id 不能为空", true), nil
		}
		queue, exists := h.batchTaskManager.GetBatchQueue(qid)
		if !exists {
			return batchMCPTextResult("队列不存在: "+qid, true), nil
		}
		if queue.Status != "completed" && queue.Status != "cancelled" {
			return batchMCPTextResult("仅已完成或已取消的队列可以重跑，当前状态: "+queue.Status, true), nil
		}
		if !h.batchTaskManager.ResetQueueForRerun(qid) {
			return batchMCPTextResult("重置队列失败", true), nil
		}
		ok, err := h.startBatchQueueExecution(qid, false)
		if !ok {
			return batchMCPTextResult("启动失败", true), nil
		}
		if err != nil {
			return batchMCPTextResult("启动失败: "+err.Error(), true), nil
		}
		logger.Info("MCP batch_task_rerun", zap.String("queueId", qid))
		return batchMCPTextResult("已重置并重新启动队列。", false), nil
	})

	// --- pause ---
	reg(mcp.Tool{
		Name:             builtin.ToolBatchTaskPause,
		Description:      "暂停正在运行的批量任务队列（当前子任务会被取消）。\n\n⚠️ 调用约束：本工具属于「任务管理」模块，仅当用户明确要求暂停批量任务时才可调用。不要在用户未要求时自行调用。",
		ShortDescription: "暂停批量任务队列",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"queue_id": map[string]interface{}{
					"type":        "string",
					"description": "队列 ID",
				},
			},
			"required": []string{"queue_id"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		qid := mcpArgString(args, "queue_id")
		if qid == "" {
			return batchMCPTextResult("queue_id 不能为空", true), nil
		}
		if !h.batchTaskManager.PauseQueue(qid) {
			return batchMCPTextResult("无法暂停：队列不存在或当前非 running 状态", true), nil
		}
		logger.Info("MCP batch_task_pause", zap.String("queueId", qid))
		return batchMCPTextResult("队列已暂停。", false), nil
	})

	// --- delete queue ---
	reg(mcp.Tool{
		Name:             builtin.ToolBatchTaskDelete,
		Description:      "删除批量任务队列及其子任务记录。\n\n⚠️ 调用约束：本工具属于「任务管理」模块，仅当用户明确要求删除批量任务队列时才可调用。不要在用户未要求时自行调用。",
		ShortDescription: "删除批量任务队列",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"queue_id": map[string]interface{}{
					"type":        "string",
					"description": "队列 ID",
				},
			},
			"required": []string{"queue_id"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		qid := mcpArgString(args, "queue_id")
		if qid == "" {
			return batchMCPTextResult("queue_id 不能为空", true), nil
		}
		if err := h.batchTaskManager.DeleteQueue(qid); err != nil {
			switch {
			case errors.Is(err, ErrBatchQueueNotFound):
				return batchMCPTextResult("删除失败：队列不存在", true), nil
			case errors.Is(err, ErrBatchQueueExecutorActive):
				return batchMCPTextResult("删除失败：队列执行器仍在运行，请稍后再试", true), nil
			case errors.Is(err, ErrBatchQueueStillRunning):
				return batchMCPTextResult("删除失败：队列正在运行中", true), nil
			default:
				return batchMCPTextResult("删除失败："+err.Error(), true), nil
			}
		}
		logger.Info("MCP batch_task_delete", zap.String("queueId", qid))
		return batchMCPTextResult("队列已删除。", false), nil
	})

	// --- update metadata (title/role/agentMode) ---
	reg(mcp.Tool{
		Name:             builtin.ToolBatchTaskUpdateMetadata,
		Description:      "修改批量任务队列的标题、角色和代理模式。仅在队列非 running 状态下可修改。\n\n⚠️ 调用约束：本工具属于「任务管理」模块，仅当用户明确要求修改批量任务队列属性时才可调用。不要在用户未要求时自行调用。",
		ShortDescription: "修改批量任务队列标题/角色/代理模式",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"queue_id": map[string]interface{}{
					"type":        "string",
					"description": "队列 ID",
				},
				"title": map[string]interface{}{
					"type":        "string",
					"description": "新标题（空字符串清除标题）",
				},
				"role": map[string]interface{}{
					"type":        "string",
					"description": "新角色名（空字符串使用默认角色）",
				},
				"agent_mode": map[string]interface{}{
					"type":        "string",
					"description": "代理模式：single、deep、plan_execute、supervisor",
					"enum":        []string{"single", "deep", "plan_execute", "supervisor"},
				},
				"concurrency": map[string]interface{}{
					"type":        "integer",
					"description": "同时执行的子任务数，默认 1，最大 8",
				},
			},
			"required": []string{"queue_id"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		qid := mcpArgString(args, "queue_id")
		if qid == "" {
			return batchMCPTextResult("queue_id 不能为空", true), nil
		}
		title := mcpArgString(args, "title")
		role := mcpArgString(args, "role")
		agentMode := mcpArgString(args, "agent_mode")
		var concurrency *int
		if raw, ok := args["concurrency"]; ok && raw != nil {
			v := int(mcpArgFloat(args, "concurrency"))
			concurrency = &v
		}
		if err := h.batchTaskManager.UpdateQueueMetadata(qid, title, role, agentMode, concurrency); err != nil {
			return batchMCPTextResult(err.Error(), true), nil
		}
		updated, _ := h.batchTaskManager.GetBatchQueue(qid)
		logger.Info("MCP batch_task_update_metadata", zap.String("queueId", qid))
		return batchMCPJSONResult(updated)
	})

	// --- update schedule ---
	reg(mcp.Tool{
		Name: builtin.ToolBatchTaskUpdateSchedule,
		Description: `修改批量任务队列的调度方式和 Cron 表达式。仅在队列非 running 状态下可修改。
schedule_mode 为 cron 时必须提供有效 cron_expr；为 manual 时会清除 Cron 配置。

⚠️ 调用约束：本工具属于「任务管理」模块，仅当用户明确要求修改批量任务调度配置时才可调用。不要在用户未要求时自行调用。`,
		ShortDescription: "修改批量任务调度配置（Cron 表达式）",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"queue_id": map[string]interface{}{
					"type":        "string",
					"description": "队列 ID",
				},
				"schedule_mode": map[string]interface{}{
					"type":        "string",
					"description": "manual 或 cron",
					"enum":        []string{"manual", "cron"},
				},
				"cron_expr": map[string]interface{}{
					"type":        "string",
					"description": "Cron 表达式（schedule_mode 为 cron 时必填）。标准 5 段格式：分钟 小时 日 月 星期，如 \"0 */6 * * *\"（每6小时）、\"30 2 * * 1-5\"（工作日凌晨2:30）",
				},
			},
			"required": []string{"queue_id", "schedule_mode"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		qid := mcpArgString(args, "queue_id")
		if qid == "" {
			return batchMCPTextResult("queue_id 不能为空", true), nil
		}
		queue, exists := h.batchTaskManager.GetBatchQueue(qid)
		if !exists {
			return batchMCPTextResult("队列不存在: "+qid, true), nil
		}
		if queue.Status == "running" {
			return batchMCPTextResult("队列正在运行中，无法修改调度配置", true), nil
		}
		scheduleMode := normalizeBatchQueueScheduleMode(mcpArgString(args, "schedule_mode"))
		cronExpr := strings.TrimSpace(mcpArgString(args, "cron_expr"))
		var nextRunAt *time.Time
		if scheduleMode == "cron" {
			if cronExpr == "" {
				return batchMCPTextResult("Cron 调度模式下 cron_expr 不能为空", true), nil
			}
			sch, err := h.batchCronParser.Parse(cronExpr)
			if err != nil {
				return batchMCPTextResult("无效的 Cron 表达式: "+err.Error(), true), nil
			}
			n := sch.Next(time.Now())
			nextRunAt = &n
		}
		h.batchTaskManager.UpdateQueueSchedule(qid, scheduleMode, cronExpr, nextRunAt)
		updated, _ := h.batchTaskManager.GetBatchQueue(qid)
		logger.Info("MCP batch_task_update_schedule", zap.String("queueId", qid), zap.String("scheduleMode", scheduleMode), zap.String("cronExpr", cronExpr))
		return batchMCPJSONResult(updated)
	})

	// --- schedule enabled ---
	reg(mcp.Tool{
		Name: builtin.ToolBatchTaskScheduleEnabled,
		Description: `设置是否允许 Cron 自动触发该队列。关闭后仍保留 Cron 表达式，仅停止定时自动跑；可用手工「启动」执行。
仅对 schedule_mode 为 cron 的队列有意义。

⚠️ 调用约束：本工具属于「任务管理」模块，仅当用户明确要求开关批量任务自动调度时才可调用。不要在用户未要求时自行调用。`,
		ShortDescription: "开关批量任务 Cron 自动调度",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"queue_id": map[string]interface{}{
					"type":        "string",
					"description": "队列 ID",
				},
				"schedule_enabled": map[string]interface{}{
					"type":        "boolean",
					"description": "true 允许定时触发，false 仅手工执行",
				},
			},
			"required": []string{"queue_id", "schedule_enabled"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		qid := mcpArgString(args, "queue_id")
		if qid == "" {
			return batchMCPTextResult("queue_id 不能为空", true), nil
		}
		en, ok := mcpArgBool(args, "schedule_enabled")
		if !ok {
			return batchMCPTextResult("schedule_enabled 必须为布尔值", true), nil
		}
		if _, exists := h.batchTaskManager.GetBatchQueue(qid); !exists {
			return batchMCPTextResult("队列不存在", true), nil
		}
		if !h.batchTaskManager.SetScheduleEnabled(qid, en) {
			return batchMCPTextResult("更新失败", true), nil
		}
		queue, _ := h.batchTaskManager.GetBatchQueue(qid)
		logger.Info("MCP batch_task_schedule_enabled", zap.String("queueId", qid), zap.Bool("enabled", en))
		return batchMCPJSONResult(queue)
	})

	// --- add task ---
	reg(mcp.Tool{
		Name:             builtin.ToolBatchTaskAdd,
		Description:      "向处于 pending 状态的队列追加一条子任务。\n\n⚠️ 调用约束：本工具属于「任务管理」模块，仅当用户明确要求向批量任务队列添加子任务时才可调用。不要在用户未要求时自行调用。",
		ShortDescription: "批量队列添加子任务",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"queue_id": map[string]interface{}{
					"type":        "string",
					"description": "队列 ID",
				},
				"message": map[string]interface{}{
					"type":        "string",
					"description": "任务指令内容",
				},
			},
			"required": []string{"queue_id", "message"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		qid := mcpArgString(args, "queue_id")
		msg := strings.TrimSpace(mcpArgString(args, "message"))
		if qid == "" || msg == "" {
			return batchMCPTextResult("queue_id 与 message 均不能为空", true), nil
		}
		task, err := h.batchTaskManager.AddTaskToQueue(qid, msg)
		if err != nil {
			return batchMCPTextResult(err.Error(), true), nil
		}
		queue, _ := h.batchTaskManager.GetBatchQueue(qid)
		logger.Info("MCP batch_task_add_task", zap.String("queueId", qid), zap.String("taskId", task.ID))
		return batchMCPJSONResult(map[string]interface{}{"task": task, "queue": queue})
	})

	// --- update task ---
	reg(mcp.Tool{
		Name:             builtin.ToolBatchTaskUpdate,
		Description:      "修改 pending 队列中仍为 pending 的子任务文案。\n\n⚠️ 调用约束：本工具属于「任务管理」模块，仅当用户明确要求修改批量子任务内容时才可调用。不要在用户未要求时自行调用。",
		ShortDescription: "更新批量子任务内容",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"queue_id": map[string]interface{}{
					"type":        "string",
					"description": "队列 ID",
				},
				"task_id": map[string]interface{}{
					"type":        "string",
					"description": "子任务 ID",
				},
				"message": map[string]interface{}{
					"type":        "string",
					"description": "新的任务指令",
				},
			},
			"required": []string{"queue_id", "task_id", "message"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		qid := mcpArgString(args, "queue_id")
		tid := mcpArgString(args, "task_id")
		msg := strings.TrimSpace(mcpArgString(args, "message"))
		if qid == "" || tid == "" || msg == "" {
			return batchMCPTextResult("queue_id、task_id、message 均不能为空", true), nil
		}
		if err := h.batchTaskManager.UpdateTaskMessage(qid, tid, msg); err != nil {
			return batchMCPTextResult(err.Error(), true), nil
		}
		queue, _ := h.batchTaskManager.GetBatchQueue(qid)
		logger.Info("MCP batch_task_update_task", zap.String("queueId", qid), zap.String("taskId", tid))
		return batchMCPJSONResult(queue)
	})

	// --- remove task ---
	reg(mcp.Tool{
		Name:             builtin.ToolBatchTaskRemove,
		Description:      "从 pending 队列中删除仍为 pending 的子任务。\n\n⚠️ 调用约束：本工具属于「任务管理」模块，仅当用户明确要求删除批量子任务时才可调用。不要在用户未要求时自行调用。",
		ShortDescription: "删除批量子任务",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"queue_id": map[string]interface{}{
					"type":        "string",
					"description": "队列 ID",
				},
				"task_id": map[string]interface{}{
					"type":        "string",
					"description": "子任务 ID",
				},
			},
			"required": []string{"queue_id", "task_id"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		qid := mcpArgString(args, "queue_id")
		tid := mcpArgString(args, "task_id")
		if qid == "" || tid == "" {
			return batchMCPTextResult("queue_id 与 task_id 均不能为空", true), nil
		}
		if err := h.batchTaskManager.DeleteTask(qid, tid); err != nil {
			return batchMCPTextResult(err.Error(), true), nil
		}
		queue, _ := h.batchTaskManager.GetBatchQueue(qid)
		logger.Info("MCP batch_task_remove_task", zap.String("queueId", qid), zap.String("taskId", tid))
		return batchMCPJSONResult(queue)
	})

	logger.Debug("批量任务 MCP 工具已注册", zap.Int("count", 12))
}

// --- batch_task_list 精简结构（避免把每条子任务的 result 等大段文本塞进列表上下文） ---

const mcpBatchListTaskMessageMaxRunes = 160

// batchTaskMCPListSummary 列表中的子任务摘要（完整字段用 batch_task_get）
type batchTaskMCPListSummary struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// batchTaskQueueMCPListItem 列表中的队列摘要
type batchTaskQueueMCPListItem struct {
	ID                    string                    `json:"id"`
	Title                 string                    `json:"title,omitempty"`
	Role                  string                    `json:"role,omitempty"`
	AgentMode             string                    `json:"agentMode"`
	ScheduleMode          string                    `json:"scheduleMode"`
	CronExpr              string                    `json:"cronExpr,omitempty"`
	NextRunAt             *time.Time                `json:"nextRunAt,omitempty"`
	ScheduleEnabled       bool                      `json:"scheduleEnabled"`
	LastScheduleTriggerAt *time.Time                `json:"lastScheduleTriggerAt,omitempty"`
	Status                string                    `json:"status"`
	CreatedAt             time.Time                 `json:"createdAt"`
	StartedAt             *time.Time                `json:"startedAt,omitempty"`
	CompletedAt           *time.Time                `json:"completedAt,omitempty"`
	CurrentIndex          int                       `json:"currentIndex"`
	Concurrency           int                       `json:"concurrency"`
	TaskTotal             int                       `json:"task_total"`
	TaskCounts            map[string]int            `json:"task_counts"`
	Tasks                 []batchTaskMCPListSummary `json:"tasks"`
}

func truncateStringRunes(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	n := 0
	for i := range s {
		if n == maxRunes {
			out := strings.TrimSpace(s[:i])
			if out == "" {
				return "…"
			}
			return out + "…"
		}
		n++
	}
	return s
}

const mcpBatchListMaxTasksPerQueue = 200 // 列表中每个队列最多返回的子任务摘要数

func toBatchTaskQueueMCPListItem(q *BatchTaskQueue) batchTaskQueueMCPListItem {
	counts := map[string]int{
		"pending":   0,
		"running":   0,
		"completed": 0,
		"failed":    0,
		"cancelled": 0,
	}
	tasks := make([]batchTaskMCPListSummary, 0, len(q.Tasks))
	for _, t := range q.Tasks {
		if t == nil {
			continue
		}
		counts[t.Status]++
		// 列表视图限制子任务摘要数量，完整列表通过 batch_task_get 查看
		if len(tasks) < mcpBatchListMaxTasksPerQueue {
			tasks = append(tasks, batchTaskMCPListSummary{
				ID:      t.ID,
				Status:  t.Status,
				Message: truncateStringRunes(t.Message, mcpBatchListTaskMessageMaxRunes),
			})
		}
	}
	return batchTaskQueueMCPListItem{
		ID:                    q.ID,
		Title:                 q.Title,
		Role:                  q.Role,
		AgentMode:             q.AgentMode,
		ScheduleMode:          q.ScheduleMode,
		CronExpr:              q.CronExpr,
		NextRunAt:             q.NextRunAt,
		ScheduleEnabled:       q.ScheduleEnabled,
		LastScheduleTriggerAt: q.LastScheduleTriggerAt,
		Status:                q.Status,
		CreatedAt:             q.CreatedAt,
		StartedAt:             q.StartedAt,
		CompletedAt:           q.CompletedAt,
		CurrentIndex:          q.CurrentIndex,
		Concurrency:           q.Concurrency,
		TaskTotal:             len(tasks),
		TaskCounts:            counts,
		Tasks:                 tasks,
	}
}

func batchMCPTextResult(text string, isErr bool) *mcp.ToolResult {
	return &mcp.ToolResult{
		Content: []mcp.Content{{Type: "text", Text: text}},
		IsError: isErr,
	}
}

func batchMCPJSONResult(v interface{}) (*mcp.ToolResult, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return batchMCPTextResult(fmt.Sprintf("JSON 编码失败: %v", err), true), nil
	}
	return &mcp.ToolResult{Content: []mcp.Content{{Type: "text", Text: string(b)}}}, nil
}

func batchMCPTasksFromArgs(args map[string]interface{}) ([]string, string) {
	if raw, ok := args["tasks"]; ok && raw != nil {
		switch t := raw.(type) {
		case []interface{}:
			out := make([]string, 0, len(t))
			for _, x := range t {
				if s, ok := x.(string); ok {
					if tr := strings.TrimSpace(s); tr != "" {
						out = append(out, tr)
					}
				}
			}
			if len(out) > 0 {
				return out, ""
			}
		}
	}
	if txt := mcpArgString(args, "tasks_text"); txt != "" {
		lines := strings.Split(txt, "\n")
		out := make([]string, 0, len(lines))
		for _, line := range lines {
			if tr := strings.TrimSpace(line); tr != "" {
				out = append(out, tr)
			}
		}
		if len(out) > 0 {
			return out, ""
		}
	}
	return nil, "需要提供 tasks（字符串数组）或 tasks_text（多行文本，每行一条任务）"
}

func mcpArgString(args map[string]interface{}, key string) string {
	v, ok := args[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case float64:
		return strings.TrimSpace(strconv.FormatFloat(t, 'f', -1, 64))
	case json.Number:
		return strings.TrimSpace(t.String())
	default:
		return strings.TrimSpace(fmt.Sprint(t))
	}
}

func mcpArgFloat(args map[string]interface{}, key string) float64 {
	v, ok := args[key]
	if !ok || v == nil {
		return 0
	}
	switch t := v.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case json.Number:
		f, _ := t.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return f
	default:
		return 0
	}
}

func mcpArgBool(args map[string]interface{}, key string) (val bool, ok bool) {
	v, exists := args[key]
	if !exists {
		return false, false
	}
	switch t := v.(type) {
	case bool:
		return t, true
	case string:
		s := strings.ToLower(strings.TrimSpace(t))
		if s == "true" || s == "1" || s == "yes" {
			return true, true
		}
		if s == "false" || s == "0" || s == "no" {
			return false, true
		}
	case float64:
		return t != 0, true
	}
	return false, false
}
