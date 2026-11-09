package app

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/authctx"
	"cyberstrike-ai/internal/c2"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/mcp/builtin"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// registerC2Tools 注册所有 C2 MCP 工具（合并同类项，减少工具数量以节省上下文 token）。
// webListenPort 为本进程 Web/API 监听端口（配置 server.port，启动时已加载），用于 MCP 描述中提示勿与 C2 bind_port 冲突。
func registerC2Tools(mcpServer *mcp.Server, c2Manager *c2.Manager, logger *zap.Logger, webListenPort int) {
	registerC2ListenerTool(mcpServer, c2Manager, logger, webListenPort)
	registerC2SessionTool(mcpServer, c2Manager, logger)
	registerC2TaskTool(mcpServer, c2Manager, logger)
	registerC2TaskManageTool(mcpServer, c2Manager, logger)
	registerC2PayloadTool(mcpServer, c2Manager, logger, webListenPort)
	registerC2EventTool(mcpServer, c2Manager, logger)
	registerC2ProfileTool(mcpServer, c2Manager, logger)
	registerC2FileTool(mcpServer, c2Manager, logger)
	logger.Debug("C2 MCP tools registered (8 unified tools)")
}

func makeC2Result(data interface{}, err error) (*mcp.ToolResult, error) {
	if err != nil {
		return &mcp.ToolResult{
			Content: []mcp.Content{{Type: "text", Text: err.Error()}},
			IsError: true,
		}, nil
	}
	text, _ := json.Marshal(data)
	return &mcp.ToolResult{
		Content: []mcp.Content{{Type: "text", Text: string(text)}},
	}, nil
}

// ============================================================================
// c2_listener — 监听器统一工具
// ============================================================================

func registerC2ListenerTool(s *mcp.Server, m *c2.Manager, l *zap.Logger, webListenPort int) {
	s.RegisterTool(mcp.Tool{
		Name: builtin.ToolC2Listener,
		Description: fmt.Sprintf(`C2 listener management. Choose an operation via the action parameter:
- list: list all listeners
- get: get listener details (requires listener_id)
- create: create a listener (requires name, type, bind_port). On success, returns implant_token alongside the listener (shown only this once, for X-Implant-Token / oneliner; no longer returned by list/get/start)
- update: update listener configuration (requires listener_id; can change name/bind_host/bind_port/remark/config/callback_host)
- start: start the listener (requires listener_id)
- stop: stop the listener (requires listener_id)
- delete: delete the listener (requires listener_id)
Listener types: tcp_reverse, http_beacon, https_beacon, websocket
tcp_reverse by default only accepts encrypted CSB1 Beacons (AES-GCM + ImplantToken) to register a session; classic bash/nc reverse shells require config.allow_legacy_shell=true (not recommended on the public internet).
Port constraint: bind_port for create/update must not equal the port used by this platform's Web/API. That port is currently %d (config item server.port, loaded from the config file at process start). If bind_port matches it, either this service or the listener will fail to bind, and Beacons/oneliners will misdial the Web service instead of C2. Please pick a different free port for the listener.`, webListenPort),
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action":        map[string]interface{}{"type": "string", "description": "Operation: list/get/create/update/start/stop/delete", "enum": []string{"list", "get", "create", "update", "start", "stop", "delete"}},
				"listener_id":   map[string]interface{}{"type": "string", "description": "Listener ID (needed for get/update/start/stop/delete)"},
				"name":          map[string]interface{}{"type": "string", "description": "Listener name (create/update)"},
				"type":          map[string]interface{}{"type": "string", "description": "Listener type (create)", "enum": []string{"tcp_reverse", "http_beacon", "https_beacon", "websocket"}},
				"bind_host":     map[string]interface{}{"type": "string", "description": "Bind address, defaults to 127.0.0.1; internet-facing listeners commonly use 0.0.0.0"},
				"callback_host": map[string]interface{}{"type": "string", "description": "Optional: host the implant/payload dials back to (public IP or domain). Written to config_json; takes precedence over bind_host when generating oneliner/beacon. Passing an empty string on update clears it"},
				"bind_port":     map[string]interface{}{"type": "integer", "description": fmt.Sprintf("Bind port (required for create). Must not equal %d (this service's current Web/API port, config server.port)", webListenPort), "minimum": 1, "maximum": 65535},
				"project_id":    map[string]interface{}{"type": "string", "description": "Owning project ID. When omitted on create, defaults to the conversation's bound project; conversations with no bound project create an unbound listener"},
				"profile_id":    map[string]interface{}{"type": "string", "description": "Malleable Profile ID"},
				"remark":        map[string]interface{}{"type": "string", "description": "Remark/note"},
				"config":        map[string]interface{}{"type": "object", "description": "Advanced configuration (beacon paths/TLS/OPSEC etc.), available on create/update. tcp_reverse optionally accepts allow_legacy_shell:true to allow unencrypted classic shells (default false)"},
			},
			"required": []string{"action"},
		},
	}, func(ctx context.Context, params map[string]interface{}) (*mcp.ToolResult, error) {
		action := getString(params, "action")
		id := getString(params, "listener_id")

		switch action {
		case "list":
			listeners, err := m.DB().ListC2ListenersForAccess(c2ToolAccess(ctx), mcpEffectiveProjectFilter(ctx, m.DB()))
			if err != nil {
				return makeC2Result(nil, err)
			}
			for _, li := range listeners {
				li.EncryptionKey = ""
				li.ImplantToken = ""
			}
			return makeC2Result(map[string]interface{}{"listeners": listeners, "count": len(listeners)}, nil)

		case "get":
			listener, err := m.DB().GetC2Listener(id)
			if err != nil {
				return makeC2Result(nil, err)
			}
			if listener == nil {
				return makeC2Result(nil, fmt.Errorf("listener not found"))
			}
			listener.EncryptionKey = ""
			listener.ImplantToken = ""
			return makeC2Result(map[string]interface{}{"listener": listener}, nil)

		case "create":
			var cfg *c2.ListenerConfig
			if cfgRaw, ok := params["config"]; ok && cfgRaw != nil {
				cfgBytes, _ := json.Marshal(cfgRaw)
				cfg = &c2.ListenerConfig{}
				_ = json.Unmarshal(cfgBytes, cfg)
			}
			projectID := strings.TrimSpace(getString(params, "project_id"))
			if projectID == "" {
				projectID = mcpEffectiveProjectFilter(ctx, m.DB())
				if projectID == database.ProjectFilterUnbound {
					projectID = ""
				}
			}
			input := c2.CreateListenerInput{
				Name:         getString(params, "name"),
				Type:         getString(params, "type"),
				BindHost:     getString(params, "bind_host"),
				BindPort:     int(getFloat64(params, "bind_port")),
				ProfileID:    getString(params, "profile_id"),
				Remark:       getString(params, "remark"),
				ProjectID:    projectID,
				Config:       cfg,
				CallbackHost: getString(params, "callback_host"),
			}
			listener, err := m.CreateListener(input)
			if err != nil {
				return makeC2Result(nil, err)
			}
			if principal, ok := authctx.PrincipalFromContext(ctx); ok {
				_ = m.DB().SetResourceOwner("c2_listener", listener.ID, principal.UserID)
				_ = m.DB().AssignResourceToUser(principal.UserID, "c2_listener", listener.ID)
			}
			implantToken := listener.ImplantToken
			listener.EncryptionKey = ""
			listener.ImplantToken = ""
			return makeC2Result(map[string]interface{}{
				"listener":      listener,
				"implant_token": implantToken,
			}, nil)

		case "update":
			listener, err := m.DB().GetC2Listener(id)
			if err != nil {
				return makeC2Result(nil, err)
			}
			if listener == nil {
				return makeC2Result(nil, fmt.Errorf("listener not found"))
			}
			if m.IsListenerRunning(id) {
				newHost := getString(params, "bind_host")
				newPort := int(getFloat64(params, "bind_port"))
				if (newHost != "" && newHost != listener.BindHost) || (newPort > 0 && newPort != listener.BindPort) {
					return makeC2Result(nil, fmt.Errorf("cannot modify bind address while listener is running"))
				}
			}
			if v := getString(params, "name"); v != "" {
				listener.Name = v
			}
			if v := getString(params, "bind_host"); v != "" {
				listener.BindHost = v
			}
			if v := int(getFloat64(params, "bind_port")); v > 0 {
				listener.BindPort = v
			}
			if v := getString(params, "profile_id"); v != "" {
				listener.ProfileID = v
			}
			if v, ok := params["remark"]; ok {
				listener.Remark, _ = v.(string)
			}
			if cfgRaw, ok := params["config"]; ok && cfgRaw != nil {
				cfgBytes, _ := json.Marshal(cfgRaw)
				listener.ConfigJSON = string(cfgBytes)
			}
			if _, ok := params["callback_host"]; ok {
				pcfg := &c2.ListenerConfig{}
				raw := strings.TrimSpace(listener.ConfigJSON)
				if raw == "" {
					raw = "{}"
				}
				_ = json.Unmarshal([]byte(raw), pcfg)
				pcfg.CallbackHost = strings.TrimSpace(getString(params, "callback_host"))
				pcfg.ApplyDefaults()
				cfgBytes, err := json.Marshal(pcfg)
				if err != nil {
					return makeC2Result(nil, err)
				}
				listener.ConfigJSON = string(cfgBytes)
			}
			if err := m.DB().UpdateC2Listener(listener); err != nil {
				return makeC2Result(nil, err)
			}
			listener.EncryptionKey = ""
			listener.ImplantToken = ""
			return makeC2Result(map[string]interface{}{"listener": listener}, nil)

		case "start":
			listener, err := m.StartListener(id)
			if err != nil {
				return makeC2Result(nil, err)
			}
			listener.EncryptionKey = ""
			listener.ImplantToken = ""
			return makeC2Result(map[string]interface{}{"listener": listener}, nil)

		case "stop":
			err := m.StopListener(id)
			return makeC2Result(map[string]interface{}{"stopped": err == nil}, err)

		case "delete":
			err := m.DeleteListener(id)
			return makeC2Result(map[string]interface{}{"deleted": err == nil}, err)

		default:
			return makeC2Result(nil, fmt.Errorf("unknown action: %s", action))
		}
	})
}

// ============================================================================
// c2_session — 会话统一工具
// ============================================================================

func registerC2SessionTool(s *mcp.Server, m *c2.Manager, l *zap.Logger) {
	s.RegisterTool(mcp.Tool{
		Name: builtin.ToolC2Session,
		Description: `C2 session management. Choose an operation via the action parameter:
- list: list sessions (filterable by listener_id/status/os/search/suspicious)
- get: get session details with recent task history (requires session_id)
- set_sleep: set the heartbeat interval (requires session_id)
- kill: dispatch an exit task to stop the implant (requires session_id)
- delete: delete a single session record (requires session_id)
- delete_batch: delete sessions in bulk (requires session_ids array)`,
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action":         map[string]interface{}{"type": "string", "description": "Operation: list/get/set_sleep/kill/delete/delete_batch", "enum": []string{"list", "get", "set_sleep", "kill", "delete", "delete_batch"}},
				"session_id":     map[string]interface{}{"type": "string", "description": "Session ID (needed for get/set_sleep/kill/delete)"},
				"session_ids":    map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "List of session IDs (delete_batch)"},
				"listener_id":    map[string]interface{}{"type": "string", "description": "Filter by listener (list)"},
				"status":         map[string]interface{}{"type": "string", "description": "Filter by status: active/sleeping/dead/killed (list)"},
				"os":             map[string]interface{}{"type": "string", "description": "Filter by OS: linux/windows/darwin (list)"},
				"search":         map[string]interface{}{"type": "string", "description": "Fuzzy search of hostname/username/IP (list)"},
				"suspicious":     map[string]interface{}{"type": "boolean", "description": "Only likely false positives: offline and tcp_* / unknown / PID 0 (list)"},
				"limit":          map[string]interface{}{"type": "integer", "description": "Maximum number of results (list)"},
				"sleep_seconds":  map[string]interface{}{"type": "integer", "description": "Heartbeat interval in seconds (set_sleep)"},
				"jitter_percent": map[string]interface{}{"type": "integer", "description": "Jitter percent 0-100 (set_sleep)"},
			},
			"required": []string{"action"},
		},
	}, func(ctx context.Context, params map[string]interface{}) (*mcp.ToolResult, error) {
		action := getString(params, "action")
		id := getString(params, "session_id")

		switch action {
		case "list":
			filter := database.ListC2SessionsFilter{
				ListenerID: getString(params, "listener_id"),
				ProjectID:  mcpEffectiveProjectFilter(ctx, m.DB()),
				Status:     getString(params, "status"),
				OS:         getString(params, "os"),
				Search:     getString(params, "search"),
			}
			if limit := int(getFloat64(params, "limit")); limit > 0 {
				filter.Limit = limit
			}
			if v, ok := params["suspicious"].(bool); ok && v {
				filter.Suspicious = true
			}
			sessions, err := m.DB().ListC2SessionsForAccess(filter, c2ToolAccess(ctx))
			return makeC2Result(map[string]interface{}{"sessions": sessions, "count": len(sessions)}, err)

		case "get":
			session, err := m.DB().GetC2Session(id)
			if err != nil {
				return makeC2Result(nil, err)
			}
			if session == nil {
				return makeC2Result(nil, fmt.Errorf("session not found"))
			}
			tasks, _ := m.DB().ListC2Tasks(database.ListC2TasksFilter{SessionID: id, Limit: 10})
			return makeC2Result(map[string]interface{}{"session": session, "tasks": tasks}, nil)

		case "set_sleep":
			sleep := int(getFloat64(params, "sleep_seconds"))
			jitter := int(getFloat64(params, "jitter_percent"))
			task, err := m.SetSessionSleep(id, sleep, jitter)
			out := map[string]interface{}{
				"updated":        err == nil,
				"sleep_seconds":  sleep,
				"jitter_percent": jitter,
			}
			if task != nil {
				out["task_id"] = task.ID
			}
			return makeC2Result(out, err)

		case "kill":
			task, err := m.EnqueueTask(c2.EnqueueTaskInput{
				SessionID:      id,
				TaskType:       c2.TaskTypeExit,
				Payload:        map[string]interface{}{},
				Source:         "ai",
				ConversationID: agent.ConversationIDFromContext(ctx),
				UserCtx:        ctx,
			})
			return makeC2Result(map[string]interface{}{"task": task}, err)

		case "delete":
			err := m.DB().DeleteC2Session(id)
			return makeC2Result(map[string]interface{}{"deleted": err == nil}, err)

		case "delete_batch":
			rawIDs, _ := params["session_ids"].([]interface{})
			ids := make([]string, 0, len(rawIDs))
			for _, v := range rawIDs {
				if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
					ids = append(ids, strings.TrimSpace(s))
				}
			}
			n, err := m.DB().DeleteC2SessionsByIDs(ids)
			return makeC2Result(map[string]interface{}{"deleted": n}, err)

		default:
			return makeC2Result(nil, fmt.Errorf("unknown action: %s", action))
		}
	})
}

// ============================================================================
// c2_task — 任务下发统一工具（合并所有 task 类型）
// ============================================================================

func registerC2TaskTool(s *mcp.Server, m *c2.Manager, l *zap.Logger) {
	s.RegisterTool(mcp.Tool{
		Name: builtin.ToolC2Task,
		Description: `Dispatch a task on a C2 session. All task types are specified via the task_type parameter:
- exec: execute a command (requires command)
- shell: interactive command that keeps its cwd (requires command)
- pwd/ps/screenshot/socks_stop: no extra parameters
- cd/ls: requires path
- kill_proc: requires pid
- upload: requires remote_path + file_id
- download: requires remote_path
- port_fwd: requires action(start/stop) + local_port + remote_host + remote_port
- socks_start: requires port (default 1080)
- load_assembly: requires data(base64) or file_id, optional args
- persist: optional method(auto/cron/bashrc/launchagent/registry/schtasks)
Returns task_id; fetch the result via c2_task_manage wait/get_result.`,
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"session_id":      map[string]interface{}{"type": "string", "description": "C2 session ID (s_xxx)"},
				"task_type":       map[string]interface{}{"type": "string", "description": "Task type", "enum": []string{"exec", "shell", "pwd", "cd", "ls", "ps", "kill_proc", "upload", "download", "screenshot", "port_fwd", "socks_start", "socks_stop", "load_assembly", "persist"}},
				"command":         map[string]interface{}{"type": "string", "description": "Command (exec/shell)"},
				"path":            map[string]interface{}{"type": "string", "description": "Path (cd/ls)"},
				"pid":             map[string]interface{}{"type": "integer", "description": "Process ID (kill_proc)"},
				"remote_path":     map[string]interface{}{"type": "string", "description": "Remote path (upload/download)"},
				"file_id":         map[string]interface{}{"type": "string", "description": "Server-side file ID (upload/load_assembly)"},
				"data":            map[string]interface{}{"type": "string", "description": "base64 data (load_assembly)"},
				"args":            map[string]interface{}{"type": "string", "description": "Command line arguments (load_assembly)"},
				"action":          map[string]interface{}{"type": "string", "description": "start/stop (port_fwd)"},
				"local_port":      map[string]interface{}{"type": "integer", "description": "Local port (port_fwd)"},
				"remote_host":     map[string]interface{}{"type": "string", "description": "Remote host (port_fwd)"},
				"remote_port":     map[string]interface{}{"type": "integer", "description": "Remote port (port_fwd)"},
				"port":            map[string]interface{}{"type": "integer", "description": "SOCKS5 port (socks_start), default 1080"},
				"method":          map[string]interface{}{"type": "string", "description": "Persistence method (persist): auto/cron/bashrc/launchagent/registry/schtasks"},
				"timeout_seconds": map[string]interface{}{"type": "integer", "description": "Timeout in seconds, default 60"},
			},
			"required": []string{"session_id", "task_type"},
		},
	}, func(ctx context.Context, params map[string]interface{}) (*mcp.ToolResult, error) {
		sessionID := getString(params, "session_id")
		taskTypeStr := getString(params, "task_type")
		taskType := c2.TaskType(taskTypeStr)
		timeout := getFloat64(params, "timeout_seconds")

		payload := map[string]interface{}{"timeout_seconds": timeout}

		switch taskType {
		case c2.TaskTypeExec, c2.TaskTypeShell:
			payload["command"] = getString(params, "command")
		case c2.TaskTypeCd, c2.TaskTypeLs:
			payload["path"] = getString(params, "path")
		case c2.TaskTypeKillProc:
			payload["pid"] = params["pid"]
		case c2.TaskTypeUpload:
			payload["remote_path"] = getString(params, "remote_path")
			payload["file_id"] = getString(params, "file_id")
		case c2.TaskTypeDownload:
			payload["remote_path"] = getString(params, "remote_path")
		case c2.TaskTypePortFwd:
			payload["action"] = getString(params, "action")
			payload["local_port"] = params["local_port"]
			payload["remote_host"] = getString(params, "remote_host")
			payload["remote_port"] = params["remote_port"]
		case c2.TaskTypeSocksStart:
			payload["port"] = params["port"]
		case c2.TaskTypeLoadAssembly:
			payload["data"] = getString(params, "data")
			payload["file_id"] = getString(params, "file_id")
			payload["args"] = getString(params, "args")
		case c2.TaskTypePersist:
			payload["method"] = getString(params, "method")
		case c2.TaskTypePwd, c2.TaskTypePs, c2.TaskTypeScreenshot, c2.TaskTypeSocksStop:
			// no extra params
		default:
			return makeC2Result(nil, fmt.Errorf("unsupported task_type: %s", taskTypeStr))
		}

		input := c2.EnqueueTaskInput{
			SessionID:      sessionID,
			TaskType:       taskType,
			Payload:        payload,
			Source:         "ai",
			ConversationID: agent.ConversationIDFromContext(ctx),
			UserCtx:        ctx,
		}
		task, err := m.EnqueueTask(input)
		if err != nil {
			return makeC2Result(nil, err)
		}
		return makeC2Result(map[string]interface{}{"task_id": task.ID, "status": task.Status}, nil)
	})
}

// ============================================================================
// c2_task_manage — 任务管理工具（查询/等待/取消）
// ============================================================================

func registerC2TaskManageTool(s *mcp.Server, m *c2.Manager, l *zap.Logger) {
	s.RegisterTool(mcp.Tool{
		Name: builtin.ToolC2TaskManage,
		Description: `C2 task management. Choose an operation via the action parameter:
- get_result: get task details and result (requires task_id)
- wait: block until the task completes and return the result (requires task_id)
- list: list tasks (filterable by session_id/status)
- cancel: cancel a queued task (requires task_id)`,
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action":          map[string]interface{}{"type": "string", "description": "Operation: get_result/wait/list/cancel", "enum": []string{"get_result", "wait", "list", "cancel"}},
				"task_id":         map[string]interface{}{"type": "string", "description": "Task ID (needed for get_result/wait/cancel)"},
				"session_id":      map[string]interface{}{"type": "string", "description": "Filter by session (list)"},
				"status":          map[string]interface{}{"type": "string", "description": "Filter by status: queued/sent/running/success/failed/cancelled (list)"},
				"limit":           map[string]interface{}{"type": "integer", "description": "Maximum number of results (list)"},
				"timeout_seconds": map[string]interface{}{"type": "integer", "description": "Wait timeout in seconds (wait), default 60"},
			},
			"required": []string{"action"},
		},
	}, func(ctx context.Context, params map[string]interface{}) (*mcp.ToolResult, error) {
		action := getString(params, "action")

		switch action {
		case "get_result":
			id := getString(params, "task_id")
			task, err := m.DB().GetC2Task(id)
			if err != nil {
				return makeC2Result(nil, err)
			}
			if task == nil {
				return makeC2Result(nil, fmt.Errorf("task not found"))
			}
			return makeC2Result(map[string]interface{}{"task": task}, nil)

		case "wait":
			id := getString(params, "task_id")
			timeout := int(getFloat64(params, "timeout_seconds"))
			if timeout <= 0 {
				timeout = 60
			}
			deadline := time.Now().Add(time.Duration(timeout) * time.Second)
			for time.Now().Before(deadline) {
				task, err := m.DB().GetC2Task(id)
				if err != nil {
					return makeC2Result(nil, err)
				}
				if task == nil {
					return makeC2Result(nil, fmt.Errorf("task not found"))
				}
				if task.Status == "success" || task.Status == "failed" || task.Status == "cancelled" {
					return makeC2Result(map[string]interface{}{"task": task}, nil)
				}
				select {
				case <-time.After(500 * time.Millisecond):
				case <-ctx.Done():
					return makeC2Result(nil, ctx.Err())
				}
			}
			return makeC2Result(nil, fmt.Errorf("timeout waiting for task completion"))

		case "list":
			filter := database.ListC2TasksFilter{
				SessionID: getString(params, "session_id"),
				ProjectID: mcpEffectiveProjectFilter(ctx, m.DB()),
				Status:    getString(params, "status"),
			}
			if limit := int(getFloat64(params, "limit")); limit > 0 {
				filter.Limit = limit
			}
			tasks, err := m.DB().ListC2TasksForAccess(filter, c2ToolAccess(ctx))
			return makeC2Result(map[string]interface{}{"tasks": tasks, "count": len(tasks)}, err)

		case "cancel":
			id := getString(params, "task_id")
			err := m.CancelTask(id)
			return makeC2Result(map[string]interface{}{"cancelled": err == nil}, err)

		default:
			return makeC2Result(nil, fmt.Errorf("unknown action: %s", action))
		}
	})
}

// ============================================================================
// c2_payload — Payload 统一工具
// ============================================================================

func registerC2PayloadTool(s *mcp.Server, m *c2.Manager, l *zap.Logger, webListenPort int) {
	s.RegisterTool(mcp.Tool{
		Name: builtin.ToolC2Payload,
		Description: fmt.Sprintf(`C2 payload generation. Choose an operation via the action parameter:
- oneliner: generate a one-liner payload. kind must match the listener protocol or it will fail:
  • tcp_reverse: by default only encrypted Beacons from build are supported; only if the listener sets config.allow_legacy_shell=true can kind be bash, nc, nc_mkfifo, python, perl, powershell.
  • http_beacon / https_beacon / websocket: HTTP(S) Beacon polling only; oneliner can only use kind: curl_beacon (the script uses bash+curl, unlike the "tcp bash"). The curl_beacon output string ends with " &" so the whole bash -c goes to the background; when run synchronously via exec/execute you must copy the entire string verbatim (including the trailing &). Dropping the & leaves the internal while loop holding the foreground, so the call keeps blocking until timeout/kill.
  • For public-internet tcp_reverse use build to produce an encrypted Beacon; do not enable allow_legacy_shell.
  • When kind is omitted, the first compatible type for the listener type is picked automatically (HTTP-based types default to curl_beacon).
- build: cross-compile a beacon binary. Supports http_beacon / https_beacon / websocket / tcp_reverse (for tcp_reverse the implant, on dial-back, first sends the CSB1 magic, then is decrypted via AES-GCM and validated against the ImplantToken before the session is registered).
The referenced listener's bind_port must avoid this service's Web port %d (config server.port, consistent with the c2_listener description), otherwise the Beacon cannot dial back correctly.`, webListenPort),
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action":         map[string]interface{}{"type": "string", "description": "Operation: oneliner/build", "enum": []string{"oneliner", "build"}},
				"listener_id":    map[string]interface{}{"type": "string", "description": "Listener ID (required). Before oneliner, confirm the listener's type, then pick a compatible kind"},
				"kind":           map[string]interface{}{"type": "string", "description": "Only needed for action=oneliner. tcp_reverse: bash|nc|nc_mkfifo|python|perl|powershell; http_beacon|https_beacon|websocket: curl_beacon only"},
				"host":           map[string]interface{}{"type": "string", "description": "Optional override for oneliner/build: when non-empty, forced as the host the implant dials back to. When empty the order is: listener callback_host (written by the create/update callback_host parameter) → bind_host (when 0.0.0.0, an attempt is made to probe this machine's outbound IP)"},
				"os":             map[string]interface{}{"type": "string", "description": "Target OS (build): linux/windows/darwin", "default": "linux"},
				"arch":           map[string]interface{}{"type": "string", "description": "Target architecture (build): amd64/arm64/386/arm", "default": "amd64"},
				"sleep_seconds":  map[string]interface{}{"type": "integer", "description": "Default heartbeat interval (build)"},
				"jitter_percent": map[string]interface{}{"type": "integer", "description": "Default jitter percent (build)"},
			},
			"required": []string{"action", "listener_id"},
		},
	}, func(ctx context.Context, params map[string]interface{}) (*mcp.ToolResult, error) {
		action := getString(params, "action")
		listenerID := getString(params, "listener_id")

		switch action {
		case "oneliner":
			listener, err := m.DB().GetC2Listener(listenerID)
			if err != nil {
				return makeC2Result(nil, err)
			}
			if listener == nil {
				return makeC2Result(nil, fmt.Errorf("listener not found"))
			}
			host := c2.ResolveBeaconDialHost(listener, getString(params, "host"), l, listenerID)
			kind := c2.OnelinerKind(getString(params, "kind"))
			if kind == "" {
				compatible := c2.OnelinerKindsForListener(listener.Type)
				if len(compatible) > 0 {
					kind = compatible[0]
				}
			}
			if !c2.IsOnelinerCompatible(listener.Type, kind) {
				compatible := c2.OnelinerKindsForListener(listener.Type)
				names := make([]string, len(compatible))
				for i, k := range compatible {
					names[i] = string(k)
				}
				return makeC2Result(nil, fmt.Errorf("listener type %s does not support %s, compatible types: %v", listener.Type, kind, names))
			}
			if err := c2.ValidateOnelinerForListener(listener, kind); err != nil {
				return makeC2Result(nil, err)
			}
			input := c2.OnelinerInput{
				Kind:         kind,
				Host:         host,
				Port:         listener.BindPort,
				HTTPBaseURL:  fmt.Sprintf("http://%s:%d", host, listener.BindPort),
				ImplantToken: listener.ImplantToken,
			}
			oneliner, err := c2.GenerateOneliner(input)
			if err != nil {
				return makeC2Result(nil, err)
			}
			out := map[string]interface{}{
				"oneliner": oneliner, "kind": input.Kind, "host": host, "port": listener.BindPort,
			}
			if kind == c2.OnelinerCurl {
				out["usage_note"] = "For synchronous exec/execute: run the whole string verbatim (it must end with \" &\"). Removing it means the while loop never ends and the tool keeps hanging."
			}
			return makeC2Result(out, nil)

		case "build":
			builder := c2.NewPayloadBuilder(m, l, "", "")
			input := c2.PayloadBuilderInput{
				ListenerID:    listenerID,
				OS:            getString(params, "os"),
				Arch:          getString(params, "arch"),
				SleepSeconds:  int(getFloat64(params, "sleep_seconds")),
				JitterPercent: int(getFloat64(params, "jitter_percent")),
				Host:          strings.TrimSpace(getString(params, "host")),
			}
			result, err := builder.BuildBeacon(input)
			if err != nil {
				return makeC2Result(nil, err)
			}
			if principal, ok := authctx.PrincipalFromContext(ctx); ok {
				_ = m.DB().RecordC2PayloadArtifact(filepath.Base(result.OutputPath), result.PayloadID, result.ListenerID, principal.UserID)
			}
			return makeC2Result(map[string]interface{}{
				"payload_id": result.PayloadID, "download_path": result.DownloadPath,
				"os": result.OS, "arch": result.Arch, "size_bytes": result.SizeBytes,
			}, nil)

		default:
			return makeC2Result(nil, fmt.Errorf("unknown action: %s", action))
		}
	})
}

// ============================================================================
// c2_event — 事件查询工具
// ============================================================================

func registerC2EventTool(s *mcp.Server, m *c2.Manager, l *zap.Logger) {
	s.RegisterTool(mcp.Tool{
		Name:        builtin.ToolC2Event,
		Description: "Fetch C2 events (check-in/disconnect/task/error), filterable by level/category/session/task/time",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"level":      map[string]interface{}{"type": "string", "description": "Filter by level: info/warn/critical"},
				"category":   map[string]interface{}{"type": "string", "description": "Filter by category: listener/session/task/payload/opsec"},
				"session_id": map[string]interface{}{"type": "string", "description": "Filter by session"},
				"task_id":    map[string]interface{}{"type": "string", "description": "Filter by task"},
				"since":      map[string]interface{}{"type": "string", "description": "Start time (RFC3339 format, e.g. 2025-01-01T00:00:00Z)"},
				"limit":      map[string]interface{}{"type": "integer", "default": 50, "description": "Number of results to return"},
			},
		},
	}, func(ctx context.Context, params map[string]interface{}) (*mcp.ToolResult, error) {
		filter := database.ListC2EventsFilter{
			Level:     getString(params, "level"),
			Category:  getString(params, "category"),
			ProjectID: mcpEffectiveProjectFilter(ctx, m.DB()),
			SessionID: getString(params, "session_id"),
			TaskID:    getString(params, "task_id"),
			Limit:     int(getFloat64(params, "limit")),
		}
		if filter.Limit <= 0 {
			filter.Limit = 50
		}
		if since := getString(params, "since"); since != "" {
			if t, err := time.Parse(time.RFC3339, since); err == nil {
				filter.Since = &t
			}
		}
		events, err := m.DB().ListC2EventsForAccess(filter, c2ToolAccess(ctx))
		return makeC2Result(map[string]interface{}{"events": events, "count": len(events)}, err)
	})
}

func c2ToolAccess(ctx context.Context) database.RBACListAccess {
	principal, ok := authctx.PrincipalFromContext(ctx)
	if !ok {
		return database.RBACListAccess{Scope: database.RBACScopeAssigned}
	}
	return database.RBACListAccess{UserID: principal.UserID, Scope: principal.ScopeFor("c2:read")}
}

// ============================================================================
// c2_profile — Malleable Profile 管理工具（新增）
// ============================================================================

func registerC2ProfileTool(s *mcp.Server, m *c2.Manager, l *zap.Logger) {
	s.RegisterTool(mcp.Tool{
		Name: builtin.ToolC2Profile,
		Description: `C2 Malleable Profile management (controls beacon communication disguise). Choose an operation via the action parameter:
- list: list all Profiles
- get: get Profile details (requires profile_id)
- create: create a Profile (requires name, optional user_agent/uris/request_headers/response_headers/body_template/jitter_min_ms/jitter_max_ms)
- update: update a Profile (requires profile_id)
- delete: delete a Profile (requires profile_id)`,
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action":           map[string]interface{}{"type": "string", "description": "Operation: list/get/create/update/delete", "enum": []string{"list", "get", "create", "update", "delete"}},
				"profile_id":       map[string]interface{}{"type": "string", "description": "Profile ID (needed for get/update/delete)"},
				"name":             map[string]interface{}{"type": "string", "description": "Profile name"},
				"user_agent":       map[string]interface{}{"type": "string", "description": "User-Agent string"},
				"uris":             map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "List of URIs for beacon requests"},
				"request_headers":  map[string]interface{}{"type": "object", "description": "Custom request headers"},
				"response_headers": map[string]interface{}{"type": "object", "description": "Custom response headers"},
				"body_template":    map[string]interface{}{"type": "string", "description": "Response body template"},
				"jitter_min_ms":    map[string]interface{}{"type": "integer", "description": "Minimum jitter (milliseconds)"},
				"jitter_max_ms":    map[string]interface{}{"type": "integer", "description": "Maximum jitter (milliseconds)"},
			},
			"required": []string{"action"},
		},
	}, func(ctx context.Context, params map[string]interface{}) (*mcp.ToolResult, error) {
		action := getString(params, "action")
		id := getString(params, "profile_id")

		switch action {
		case "list":
			profiles, err := m.DB().ListC2Profiles()
			return makeC2Result(map[string]interface{}{"profiles": profiles, "count": len(profiles)}, err)

		case "get":
			profile, err := m.DB().GetC2Profile(id)
			if err != nil {
				return makeC2Result(nil, err)
			}
			if profile == nil {
				return makeC2Result(nil, fmt.Errorf("profile not found"))
			}
			return makeC2Result(map[string]interface{}{"profile": profile}, nil)

		case "create":
			profile := &database.C2Profile{
				ID:           "p_" + strings.ReplaceAll(uuid.New().String(), "-", "")[:14],
				Name:         getString(params, "name"),
				UserAgent:    getString(params, "user_agent"),
				BodyTemplate: getString(params, "body_template"),
				JitterMinMS:  int(getFloat64(params, "jitter_min_ms")),
				JitterMaxMS:  int(getFloat64(params, "jitter_max_ms")),
				CreatedAt:    time.Now(),
			}
			if uris, ok := params["uris"]; ok {
				if arr, ok := uris.([]interface{}); ok {
					for _, u := range arr {
						if s, ok := u.(string); ok {
							profile.URIs = append(profile.URIs, s)
						}
					}
				}
			}
			if rh, ok := params["request_headers"]; ok {
				if m, ok := rh.(map[string]interface{}); ok {
					profile.RequestHeaders = make(map[string]string)
					for k, v := range m {
						profile.RequestHeaders[k], _ = v.(string)
					}
				}
			}
			if rh, ok := params["response_headers"]; ok {
				if m, ok := rh.(map[string]interface{}); ok {
					profile.ResponseHeaders = make(map[string]string)
					for k, v := range m {
						profile.ResponseHeaders[k], _ = v.(string)
					}
				}
			}
			if err := m.DB().CreateC2Profile(profile); err != nil {
				return makeC2Result(nil, err)
			}
			return makeC2Result(map[string]interface{}{"profile": profile}, nil)

		case "update":
			profile, err := m.DB().GetC2Profile(id)
			if err != nil {
				return makeC2Result(nil, err)
			}
			if profile == nil {
				return makeC2Result(nil, fmt.Errorf("profile not found"))
			}
			if v := getString(params, "name"); v != "" {
				profile.Name = v
			}
			if v := getString(params, "user_agent"); v != "" {
				profile.UserAgent = v
			}
			if v := getString(params, "body_template"); v != "" {
				profile.BodyTemplate = v
			}
			if v := int(getFloat64(params, "jitter_min_ms")); v > 0 {
				profile.JitterMinMS = v
			}
			if v := int(getFloat64(params, "jitter_max_ms")); v > 0 {
				profile.JitterMaxMS = v
			}
			if uris, ok := params["uris"]; ok {
				if arr, ok := uris.([]interface{}); ok {
					profile.URIs = nil
					for _, u := range arr {
						if s, ok := u.(string); ok {
							profile.URIs = append(profile.URIs, s)
						}
					}
				}
			}
			if rh, ok := params["request_headers"]; ok {
				if mp, ok := rh.(map[string]interface{}); ok {
					profile.RequestHeaders = make(map[string]string)
					for k, v := range mp {
						profile.RequestHeaders[k], _ = v.(string)
					}
				}
			}
			if rh, ok := params["response_headers"]; ok {
				if mp, ok := rh.(map[string]interface{}); ok {
					profile.ResponseHeaders = make(map[string]string)
					for k, v := range mp {
						profile.ResponseHeaders[k], _ = v.(string)
					}
				}
			}
			if err := m.DB().UpdateC2Profile(profile); err != nil {
				return makeC2Result(nil, err)
			}
			return makeC2Result(map[string]interface{}{"profile": profile}, nil)

		case "delete":
			err := m.DB().DeleteC2Profile(id)
			return makeC2Result(map[string]interface{}{"deleted": err == nil}, err)

		default:
			return makeC2Result(nil, fmt.Errorf("unknown action: %s", action))
		}
	})
}

// ============================================================================
// c2_file — 文件管理工具（新增）
// ============================================================================

func registerC2FileTool(s *mcp.Server, m *c2.Manager, l *zap.Logger) {
	s.RegisterTool(mcp.Tool{
		Name: builtin.ToolC2File,
		Description: `C2 file management. Choose an operation via the action parameter:
- list: list a session's file transfer records (requires session_id)
- get_result: get the task result file path (screenshots etc., requires task_id)`,
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action":     map[string]interface{}{"type": "string", "description": "Operation: list/get_result", "enum": []string{"list", "get_result"}},
				"session_id": map[string]interface{}{"type": "string", "description": "Session ID (needed for list)"},
				"task_id":    map[string]interface{}{"type": "string", "description": "Task ID (needed for get_result)"},
			},
			"required": []string{"action"},
		},
	}, func(ctx context.Context, params map[string]interface{}) (*mcp.ToolResult, error) {
		action := getString(params, "action")

		switch action {
		case "list":
			sessionID := getString(params, "session_id")
			if sessionID == "" {
				return makeC2Result(nil, fmt.Errorf("session_id required"))
			}
			files, err := m.DB().ListC2FilesBySession(sessionID)
			return makeC2Result(map[string]interface{}{"files": files, "count": len(files)}, err)

		case "get_result":
			taskID := getString(params, "task_id")
			task, err := m.DB().GetC2Task(taskID)
			if err != nil {
				return makeC2Result(nil, err)
			}
			if task == nil {
				return makeC2Result(nil, fmt.Errorf("task not found"))
			}
			if task.ResultBlobPath == "" {
				return makeC2Result(map[string]interface{}{"has_file": false, "task_id": taskID}, nil)
			}
			return makeC2Result(map[string]interface{}{
				"has_file":  true,
				"task_id":   taskID,
				"file_path": task.ResultBlobPath,
			}, nil)

		default:
			return makeC2Result(nil, fmt.Errorf("unknown action: %s", action))
		}
	})
}

// ============================================================================
// 工具函数
// ============================================================================

func getString(params map[string]interface{}, key string) string {
	if v, ok := params[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getFloat64(params map[string]interface{}, key string) float64 {
	if v, ok := params[key]; ok {
		switch n := v.(type) {
		case float64:
			return n
		case int:
			return float64(n)
		case string:
			if f, err := strconv.ParseFloat(n, 64); err == nil {
				return f
			}
		}
	}
	return 0
}
