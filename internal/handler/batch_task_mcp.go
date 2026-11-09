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
		Description:      "List batch task queues (compact summary, saves context). Includes queue metadata, sub-task id/status/truncated message, and per-status counts. For the complete sub-task (including result/error/conversationId/timestamps etc.), use batch_task_get(queue_id).\n\n⚠️ Usage constraint: this tool belongs to the 「Task Management」 module; call it only when the user explicitly mentions viewing/managing batch tasks or task queues. Do not call it on your own initiative when the user has not asked.",
		ShortDescription: "List batch task queues",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"status": map[string]interface{}{
					"type":        "string",
					"description": "Status filter: all (default), pending, running, paused, completed, cancelled",
					"enum":        []string{"all", "pending", "running", "paused", "completed", "cancelled"},
				},
				"keyword": map[string]interface{}{
					"type":        "string",
					"description": "Fuzzy search by queue ID or title",
				},
				"page": map[string]interface{}{
					"type":        "integer",
					"description": "Page number, starts at 1, default 1",
				},
				"page_size": map[string]interface{}{
					"type":        "integer",
					"description": "Page size, default 20, maximum 100",
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
			return batchMCPTextResult("missing authenticated identity", true), nil
		}
		if err != nil {
			return batchMCPTextResult(fmt.Sprintf("failed to list queues: %v", err), true), nil
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
		Description:      "Get a single batch task queue's details by queue_id (including sub-task list, Cron, scheduling toggle, and the latest error message).\n\n⚠️ Usage constraint: this tool belongs to the 「Task Management」 module; call it only when the user explicitly mentions viewing/managing batch tasks or task queues. Do not call it on your own initiative when the user has not asked.",
		ShortDescription: "Get batch task queue details",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"queue_id": map[string]interface{}{
					"type":        "string",
					"description": "Queue ID",
				},
			},
			"required": []string{"queue_id"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		qid := mcpArgString(args, "queue_id")
		if qid == "" {
			return batchMCPTextResult("queue_id cannot be empty", true), nil
		}
		queue, ok := h.batchTaskManager.GetBatchQueue(qid)
		if !ok {
			return batchMCPTextResult("queue not found: "+qid, true), nil
		}
		return batchMCPJSONResult(queue)
	})

	// --- create ---
	reg(mcp.Tool{
		Name: builtin.ToolBatchTaskCreate,
		Description: `⚠️ Usage constraint: this tool belongs to the 「Task Management」 module; call it only when the user explicitly requests creating batch tasks or a task queue. Do not call it on your own initiative when the user has not mentioned keywords such as “batch task” “task queue” “scheduled task”. If the user is just asking you to do something, complete it directly in the current conversation instead; do not take it upon yourself to create a task queue.

【Purpose】In-app 「Task Management / Batch Task Queue」: register multiple mutually independent user instructions as a single queue, so progress can be viewed in the UI, paused/continued, re-run on schedule, etc. This is the queue data and scheduling entry point, not a way to open another “sub-agent session” to explore the current problem for you.

【When to use】Call it when the user explicitly wants batch queued execution, running the same set of instructions on a Cron schedule, or needs alignment with the Task Management page. Analysis/coding that needs immediate follow-up questions or strongly depends on the current conversation context should be done directly within this conversation; do not create a queue just for “delegation”.

【Parameters】Choose one of tasks (string array) or tasks_text (multiple lines, one per line); each item is an instruction text that the system will later execute in queue order. agent_mode: eino_single (Eino ADK single agent, default), deep / plan_execute / supervisor (requires multi-agent to be enabled by the system). Not “splitting the main conversation into sub-agents”. schedule_mode: manual (default) or cron; cron must fill cron_expr (5 fields, e.g. “0 */6 * * *”).

【Execution】By default, after creation the queue is pending and does not run automatically. execute_now=true can start it immediately after creation; otherwise call batch_task_start afterwards. For Cron to trigger the next round automatically, schedule_enabled must be true (can use batch_task_schedule_enabled).`,
		ShortDescription: "Task Management: create a batch task queue (register multiple instructions, optional immediate or Cron)",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"title": map[string]interface{}{
					"type":        "string",
					"description": "Optional queue title, for identification in Task Management",
				},
				"role": map[string]interface{}{
					"type":        "string",
					"description": "Role name used by the queue; empty means default",
				},
				"tasks": map[string]interface{}{
					"type":        "array",
					"description": "Sub-task instructions for the queue; each item is one independent instruction text to execute later (either this or tasks_text)",
					"items":       map[string]interface{}{"type": "string"},
				},
				"tasks_text": map[string]interface{}{
					"type":        "string",
					"description": "Multi-line text, one sub-task instruction per line (either this or tasks)",
				},
				"agent_mode": map[string]interface{}{
					"type":        "string",
					"description": "Execution mode: eino_single (Eino ADK, default), deep/plan_execute/supervisor (Eino orchestration, requires multi-agent to be enabled)",
					"enum":        []string{"eino_single", "deep", "plan_execute", "supervisor"},
				},
				"schedule_mode": map[string]interface{}{
					"type":        "string",
					"description": "manual (only manual / starts on launch) or cron (triggered by the expression)",
					"enum":        []string{"manual", "cron"},
				},
				"cron_expr": map[string]interface{}{
					"type":        "string",
					"description": "Required when schedule_mode is cron. Standard 5 fields: minute hour day month weekday, e.g. \"0 */6 * * *\", \"30 2 * * 1-5\"",
				},
				"execute_now": map[string]interface{}{
					"type":        "boolean",
					"description": "Whether to start executing the queue immediately after creation, default false (pending, requires batch_task_start)",
				},
				"project_id": map[string]interface{}{
					"type":        "string",
					"description": "Project ID bound to the sub-conversations in the queue (optional; uses config.project.default_project_id when not specified)",
				},
				"concurrency": map[string]interface{}{
					"type":        "integer",
					"description": "Number of sub-tasks executed simultaneously, default 1 (serial), maximum 8. Recommended 1-2 when scan-type tools are involved.",
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
				return batchMCPTextResult("cron_expr cannot be empty in Cron scheduling mode", true), nil
			}
			sch, err := h.batchCronParser.Parse(cronExpr)
			if err != nil {
				return batchMCPTextResult("invalid Cron expression: "+err.Error(), true), nil
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
				return batchMCPTextResult("no permission to access the target project", true), nil
			}
		}
		concurrency := int(mcpArgFloat(args, "concurrency"))
		queue, createErr := h.batchTaskManager.CreateBatchQueue(title, role, agentMode, scheduleMode, cronExpr, projectID, nextRunAt, concurrency, tasks)
		if createErr != nil {
			return batchMCPTextResult("failed to create queue: "+createErr.Error(), true), nil
		}
		if principal, ok := authctx.PrincipalFromContext(ctx); ok && h.db != nil {
			_ = h.db.SetResourceOwner("batch_task", queue.ID, principal.UserID)
			_ = h.db.AssignResourceToUser(principal.UserID, "batch_task", queue.ID)
		}
		started := false
		if executeNow {
			ok, err := h.startBatchQueueExecution(queue.ID, false)
			if !ok {
				return batchMCPTextResult("queue not found: "+queue.ID, true), nil
			}
			if err != nil {
				return batchMCPTextResult("created successfully but failed to start: "+err.Error(), true), nil
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
					return "Queue created and started immediately."
				}
				return "Queue created, currently in pending state. When execution should begin, call the MCP tool batch_task_start (same queue_id as above). Automatic Cron scheduling requires schedule_enabled to be true; can use batch_task_schedule_enabled."
			}(),
		})
	})

	// --- start ---
	reg(mcp.Tool{
		Name: builtin.ToolBatchTaskStart,
		Description: `Start or continue executing a batch task queue (pending / paused).
Used together with batch_task_create: creating a queue alone does not execute it automatically; this tool must be called for the sub-tasks to start running.

⚠️ Usage constraint: this tool belongs to the 「Task Management」 module; call it only when the user explicitly requests starting/continuing a batch task. Do not call it on your own initiative when the user has not asked.`,
		ShortDescription: "Start/continue a batch task queue (must be called after creation for it to execute)",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"queue_id": map[string]interface{}{
					"type":        "string",
					"description": "Queue ID",
				},
			},
			"required": []string{"queue_id"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		qid := mcpArgString(args, "queue_id")
		if qid == "" {
			return batchMCPTextResult("queue_id cannot be empty", true), nil
		}
		ok, err := h.startBatchQueueExecution(qid, false)
		if !ok {
			return batchMCPTextResult("queue not found: "+qid, true), nil
		}
		if err != nil {
			return batchMCPTextResult("failed to start: "+err.Error(), true), nil
		}
		logger.Info("MCP batch_task_start", zap.String("queueId", qid))
		return batchMCPTextResult("Start submitted; the queue will begin executing.", false), nil
	})

	// --- rerun (reset + start for completed/cancelled queues) ---
	reg(mcp.Tool{
		Name:             builtin.ToolBatchTaskRerun,
		Description:      "Re-run a completed or cancelled batch task queue. Resets all sub-task statuses and then executes another round.\n\n⚠️ Usage constraint: this tool belongs to the 「Task Management」 module; call it only when the user explicitly requests re-running a batch task. Do not call it on your own initiative when the user has not asked.",
		ShortDescription: "Re-run a batch task queue",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"queue_id": map[string]interface{}{
					"type":        "string",
					"description": "Queue ID",
				},
			},
			"required": []string{"queue_id"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		qid := mcpArgString(args, "queue_id")
		if qid == "" {
			return batchMCPTextResult("queue_id cannot be empty", true), nil
		}
		queue, exists := h.batchTaskManager.GetBatchQueue(qid)
		if !exists {
			return batchMCPTextResult("queue not found: "+qid, true), nil
		}
		if queue.Status != "completed" && queue.Status != "cancelled" {
			return batchMCPTextResult("only completed or cancelled queues can be re-run; current status: "+queue.Status, true), nil
		}
		if !h.batchTaskManager.ResetQueueForRerun(qid) {
			return batchMCPTextResult("failed to reset queue", true), nil
		}
		ok, err := h.startBatchQueueExecution(qid, false)
		if !ok {
			return batchMCPTextResult("failed to start", true), nil
		}
		if err != nil {
			return batchMCPTextResult("failed to start: "+err.Error(), true), nil
		}
		logger.Info("MCP batch_task_rerun", zap.String("queueId", qid))
		return batchMCPTextResult("Queue has been reset and restarted.", false), nil
	})

	// --- pause ---
	reg(mcp.Tool{
		Name:             builtin.ToolBatchTaskPause,
		Description:      "Pause a running batch task queue (its current sub-task will be cancelled).\n\n⚠️ Usage constraint: this tool belongs to the 「Task Management」 module; call it only when the user explicitly requests pausing a batch task. Do not call it on your own initiative when the user has not asked.",
		ShortDescription: "Pause a batch task queue",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"queue_id": map[string]interface{}{
					"type":        "string",
					"description": "Queue ID",
				},
			},
			"required": []string{"queue_id"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		qid := mcpArgString(args, "queue_id")
		if qid == "" {
			return batchMCPTextResult("queue_id cannot be empty", true), nil
		}
		if !h.batchTaskManager.PauseQueue(qid) {
			return batchMCPTextResult("cannot pause: the queue does not exist or is not currently in running state", true), nil
		}
		logger.Info("MCP batch_task_pause", zap.String("queueId", qid))
		return batchMCPTextResult("Queue paused.", false), nil
	})

	// --- delete queue ---
	reg(mcp.Tool{
		Name:             builtin.ToolBatchTaskDelete,
		Description:      "Delete a batch task queue and its sub-task records.\n\n⚠️ Usage constraint: this tool belongs to the 「Task Management」 module; call it only when the user explicitly requests deleting a batch task queue. Do not call it on your own initiative when the user has not asked.",
		ShortDescription: "Delete a batch task queue",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"queue_id": map[string]interface{}{
					"type":        "string",
					"description": "Queue ID",
				},
			},
			"required": []string{"queue_id"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		qid := mcpArgString(args, "queue_id")
		if qid == "" {
			return batchMCPTextResult("queue_id cannot be empty", true), nil
		}
		if err := h.batchTaskManager.DeleteQueue(qid); err != nil {
			switch {
			case errors.Is(err, ErrBatchQueueNotFound):
				return batchMCPTextResult("delete failed: queue not found", true), nil
			case errors.Is(err, ErrBatchQueueExecutorActive):
				return batchMCPTextResult("delete failed: the queue executor is still running, please try again later", true), nil
			case errors.Is(err, ErrBatchQueueStillRunning):
				return batchMCPTextResult("delete failed: the queue is currently running", true), nil
			default:
				return batchMCPTextResult("delete failed: "+err.Error(), true), nil
			}
		}
		logger.Info("MCP batch_task_delete", zap.String("queueId", qid))
		return batchMCPTextResult("Queue deleted.", false), nil
	})

	// --- update metadata (title/role/agentMode) ---
	reg(mcp.Tool{
		Name:             builtin.ToolBatchTaskUpdateMetadata,
		Description:      "Modify a batch task queue's title, role, and agent mode. Only modifiable when the queue is not in running state.\n\n⚠️ Usage constraint: this tool belongs to the 「Task Management」 module; call it only when the user explicitly requests modifying batch task queue properties. Do not call it on your own initiative when the user has not asked.",
		ShortDescription: "Modify batch task queue title/role/agent mode",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"queue_id": map[string]interface{}{
					"type":        "string",
					"description": "Queue ID",
				},
				"title": map[string]interface{}{
					"type":        "string",
					"description": "New title (empty string clears the title)",
				},
				"role": map[string]interface{}{
					"type":        "string",
					"description": "New role name (empty string uses the default role)",
				},
				"agent_mode": map[string]interface{}{
					"type":        "string",
					"description": "Agent mode: eino_single, deep, plan_execute, supervisor",
					"enum":        []string{"eino_single", "deep", "plan_execute", "supervisor"},
				},
				"concurrency": map[string]interface{}{
					"type":        "integer",
					"description": "Number of sub-tasks executed simultaneously, default 1, maximum 8",
				},
			},
			"required": []string{"queue_id"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		qid := mcpArgString(args, "queue_id")
		if qid == "" {
			return batchMCPTextResult("queue_id cannot be empty", true), nil
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
		Description: `Modify a batch task queue's scheduling mode and Cron expression. Only modifiable when the queue is not in running state.
When schedule_mode is cron, a valid cron_expr must be provided; when it is manual, the Cron configuration is cleared.

⚠️ Usage constraint: this tool belongs to the 「Task Management」 module; call it only when the user explicitly requests modifying the batch task scheduling configuration. Do not call it on your own initiative when the user has not asked.`,
		ShortDescription: "Modify batch task scheduling configuration (Cron expression)",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"queue_id": map[string]interface{}{
					"type":        "string",
					"description": "Queue ID",
				},
				"schedule_mode": map[string]interface{}{
					"type":        "string",
					"description": "manual or cron",
					"enum":        []string{"manual", "cron"},
				},
				"cron_expr": map[string]interface{}{
					"type":        "string",
					"description": "Cron expression (required when schedule_mode is cron). Standard 5-field format: minute hour day month weekday, e.g. \"0 */6 * * *\" (every 6 hours), \"30 2 * * 1-5\" (weekdays at 2:30 AM)",
				},
			},
			"required": []string{"queue_id", "schedule_mode"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		qid := mcpArgString(args, "queue_id")
		if qid == "" {
			return batchMCPTextResult("queue_id cannot be empty", true), nil
		}
		queue, exists := h.batchTaskManager.GetBatchQueue(qid)
		if !exists {
			return batchMCPTextResult("queue not found: "+qid, true), nil
		}
		if queue.Status == "running" {
			return batchMCPTextResult("the queue is currently running; the scheduling configuration cannot be modified", true), nil
		}
		scheduleMode := normalizeBatchQueueScheduleMode(mcpArgString(args, "schedule_mode"))
		cronExpr := strings.TrimSpace(mcpArgString(args, "cron_expr"))
		var nextRunAt *time.Time
		if scheduleMode == "cron" {
			if cronExpr == "" {
				return batchMCPTextResult("cron_expr cannot be empty in Cron scheduling mode", true), nil
			}
			sch, err := h.batchCronParser.Parse(cronExpr)
			if err != nil {
				return batchMCPTextResult("invalid Cron expression: "+err.Error(), true), nil
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
		Description: `Set whether to allow Cron to trigger this queue automatically. When turned off, the Cron expression is retained but only scheduled automatic runs stop; manual 「Start」 execution is still available.
Only meaningful for queues whose schedule_mode is cron.

⚠️ Usage constraint: this tool belongs to the 「Task Management」 module; call it only when the user explicitly requests toggling the batch task's automatic scheduling. Do not call it on your own initiative when the user has not asked.`,
		ShortDescription: "Toggle batch task Cron automatic scheduling",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"queue_id": map[string]interface{}{
					"type":        "string",
					"description": "Queue ID",
				},
				"schedule_enabled": map[string]interface{}{
					"type":        "boolean",
					"description": "true allows scheduled triggering; false allows only manual execution",
				},
			},
			"required": []string{"queue_id", "schedule_enabled"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		qid := mcpArgString(args, "queue_id")
		if qid == "" {
			return batchMCPTextResult("queue_id cannot be empty", true), nil
		}
		en, ok := mcpArgBool(args, "schedule_enabled")
		if !ok {
			return batchMCPTextResult("schedule_enabled must be a boolean", true), nil
		}
		if _, exists := h.batchTaskManager.GetBatchQueue(qid); !exists {
			return batchMCPTextResult("queue not found", true), nil
		}
		if !h.batchTaskManager.SetScheduleEnabled(qid, en) {
			return batchMCPTextResult("update failed", true), nil
		}
		queue, _ := h.batchTaskManager.GetBatchQueue(qid)
		logger.Info("MCP batch_task_schedule_enabled", zap.String("queueId", qid), zap.Bool("enabled", en))
		return batchMCPJSONResult(queue)
	})

	// --- add task ---
	reg(mcp.Tool{
		Name:             builtin.ToolBatchTaskAdd,
		Description:      "Append a sub-task to a queue in pending state.\n\n⚠️ Usage constraint: this tool belongs to the 「Task Management」 module; call it only when the user explicitly requests adding a sub-task to a batch task queue. Do not call it on your own initiative when the user has not asked.",
		ShortDescription: "Append a sub-task to a batch queue",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"queue_id": map[string]interface{}{
					"type":        "string",
					"description": "Queue ID",
				},
				"message": map[string]interface{}{
					"type":        "string",
					"description": "Task instruction content",
				},
			},
			"required": []string{"queue_id", "message"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		qid := mcpArgString(args, "queue_id")
		msg := strings.TrimSpace(mcpArgString(args, "message"))
		if qid == "" || msg == "" {
			return batchMCPTextResult("queue_id and message cannot both be empty", true), nil
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
		Description:      "Modify the text of a sub-task that is still pending within a pending queue.\n\n⚠️ Usage constraint: this tool belongs to the 「Task Management」 module; call it only when the user explicitly requests modifying batch sub-task content. Do not call it on your own initiative when the user has not asked.",
		ShortDescription: "Update batch sub-task content",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"queue_id": map[string]interface{}{
					"type":        "string",
					"description": "Queue ID",
				},
				"task_id": map[string]interface{}{
					"type":        "string",
					"description": "Sub-task ID",
				},
				"message": map[string]interface{}{
					"type":        "string",
					"description": "New task instruction",
				},
			},
			"required": []string{"queue_id", "task_id", "message"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		qid := mcpArgString(args, "queue_id")
		tid := mcpArgString(args, "task_id")
		msg := strings.TrimSpace(mcpArgString(args, "message"))
		if qid == "" || tid == "" || msg == "" {
			return batchMCPTextResult("queue_id, task_id, and message cannot be empty", true), nil
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
		Description:      "Delete a sub-task that is still pending from a pending queue.\n\n⚠️ Usage constraint: this tool belongs to the 「Task Management」 module; call it only when the user explicitly requests deleting a batch sub-task. Do not call it on your own initiative when the user has not asked.",
		ShortDescription: "Delete a batch sub-task",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"queue_id": map[string]interface{}{
					"type":        "string",
					"description": "Queue ID",
				},
				"task_id": map[string]interface{}{
					"type":        "string",
					"description": "Sub-task ID",
				},
			},
			"required": []string{"queue_id", "task_id"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		qid := mcpArgString(args, "queue_id")
		tid := mcpArgString(args, "task_id")
		if qid == "" || tid == "" {
			return batchMCPTextResult("queue_id and task_id cannot both be empty", true), nil
		}
		if err := h.batchTaskManager.DeleteTask(qid, tid); err != nil {
			return batchMCPTextResult(err.Error(), true), nil
		}
		queue, _ := h.batchTaskManager.GetBatchQueue(qid)
		logger.Info("MCP batch_task_remove_task", zap.String("queueId", qid), zap.String("taskId", tid))
		return batchMCPJSONResult(queue)
	})

	logger.Debug("batch task MCP tools registered", zap.Int("count", 12))
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
		return batchMCPTextResult(fmt.Sprintf("JSON encoding failed: %v", err), true), nil
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
	return nil, "need to provide tasks (string array) or tasks_text (multi-line text, one task per line)"
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
