package c2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"cyberstrike-ai/internal/database"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Manager 是 C2 模块对外的统一门面：
//   - HTTP handler / MCP 工具 / 多代理 / 攻击链记录器 全部通过 Manager 操作 C2，
//     不直接接触 listener 实现细节，避免循环依赖；
//   - 持有数据库句柄 + 事件总线 + 内存中的 listener 实例 map；
//   - 启动期可调用 RestoreRunningListeners() 把 status=running 的 listener 重新拉起。
//
// 实例化由 internal/app 负责，注入到全局 App 之后再分别交给 handler / mcp.
type Manager struct {
	db       *database.DB
	logger   *zap.Logger
	bus      *EventBus
	registry *ListenerRegistry

	mu               sync.RWMutex
	runningListeners map[string]Listener // listener_id → 已 Start 的 listener 实例
	storageDir       string              // 大结果（截图/下载）落盘根目录

	hitlBridge        HITLBridge                                    // 危险任务在 EnqueueTask 时调它发起审批（nil 表示不接 HITL）
	hitlDangerousGate func(conversationID, mcpToolName string) bool // 与人机协同一致：为 nil 或返回 false 时不走桥
	hooks             Hooks                                         // 扩展挂钩：会话上线 / 任务完成 时通知漏洞库与攻击链
}

// MCPToolC2Task 与 MCP builtin、c2_task 工具名一致，供 HITL 白名单与 Agent 侧对齐。
const MCPToolC2Task = "c2_task"

var (
	resultBlobSuffixPattern = regexp.MustCompile(`^\.[A-Za-z0-9][A-Za-z0-9_-]{0,31}$`)
	uploadTaskIDPattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)
)

// HITLBridge 把"危险任务"桥到现有 internal/handler/hitl 审批流的接口。
// internal/app 实例化时传入；空实现表示禁用 HITL 拦截（开发期方便）。
type HITLBridge interface {
	// RequestApproval 阻塞等待人工审批；返回 nil 表示批准，error 表示拒绝/超时。
	// ctx 携带用户/会话信息；危险任务调用时会创建超时 ctx 避免无限挂起。
	RequestApproval(ctx context.Context, req HITLApprovalRequest) error
}

// HITLApprovalRequest 待审批的 C2 操作描述
type HITLApprovalRequest struct {
	TaskID         string
	SessionID      string
	TaskType       string
	PayloadJSON    string
	ConversationID string
	Source         string
	Reason         string
}

// Hooks 给上层（漏洞管理 / 攻击链）注入回调
type Hooks struct {
	OnSessionFirstSeen func(session *database.C2Session)             // 新会话首次上线
	OnTaskCompleted    func(task *database.C2Task, sessionID string) // 任务完成（success/failed）
}

// NewManager 创建 Manager；不会启动任何 listener，请显式调 RestoreRunningListeners
func NewManager(db *database.DB, logger *zap.Logger, storageDir string) *Manager {
	if logger == nil {
		logger = zap.NewNop()
	}
	if storageDir == "" {
		storageDir = "tmp/c2"
	}
	return &Manager{
		db:               db,
		logger:           logger,
		bus:              NewEventBus(),
		registry:         NewListenerRegistry(),
		runningListeners: make(map[string]Listener),
		storageDir:       storageDir,
	}
}

// SetHITLBridge 设置危险任务审批桥；nil 表示禁用
func (m *Manager) SetHITLBridge(b HITLBridge) {
	m.mu.Lock()
	m.hitlBridge = b
	m.mu.Unlock()
}

// SetHITLDangerousGate 设置 C2 危险任务是否应走 HITL 桥；须与 Agent 人机协同判定一致（例如 handler.HITLManager.NeedsToolApproval）。
// gate 为 nil 时，即使已设置桥也不会对危险任务发起审批（与未开启人机协同时其他工具行为一致）。
func (m *Manager) SetHITLDangerousGate(gate func(conversationID, mcpToolName string) bool) {
	m.mu.Lock()
	m.hitlDangerousGate = gate
	m.mu.Unlock()
}

// SetHooks 注入业务钩子
func (m *Manager) SetHooks(h Hooks) {
	m.mu.Lock()
	m.hooks = h
	m.mu.Unlock()
}

// EventBus 暴露事件总线给 SSE handler
func (m *Manager) EventBus() *EventBus { return m.bus }

// DB 暴露 DB 句柄给 handler/mcptools 直接读写（避免到处包装）
func (m *Manager) DB() *database.DB { return m.db }

// Logger 暴露日志句柄
func (m *Manager) Logger() *zap.Logger { return m.logger }

// StorageDir 大结果落盘根目录
func (m *Manager) StorageDir() string { return m.storageDir }

// Registry 暴露 listener 注册表，便于在 internal/app 启动时按 type 注册具体实现
func (m *Manager) Registry() *ListenerRegistry { return m.registry }

// Close 优雅关闭：停掉所有运行中的 listener，关闭事件总线
func (m *Manager) Close() {
	m.mu.Lock()
	listeners := make([]Listener, 0, len(m.runningListeners))
	for _, l := range m.runningListeners {
		listeners = append(listeners, l)
	}
	m.runningListeners = make(map[string]Listener)
	m.mu.Unlock()
	for _, l := range listeners {
		_ = l.Stop()
	}
	m.bus.Close()
}

// ----------------------------------------------------------------------------
// Listener 生命周期
// ----------------------------------------------------------------------------

// CreateListenerInput Web/MCP 创建监听器的入参（已校验 + 已 trim）
type CreateListenerInput struct {
	Name      string
	ProjectID string
	Type      string
	BindHost  string
	BindPort  int
	ProfileID string
	Remark    string
	Config    *ListenerConfig
	// CallbackHost 非空时写入 config_json.callback_host，供 Payload 默认回连（不修改 bind）
	CallbackHost string
}

// CreateListener 校验并落库；不自动启动（与 systemd unit 一致：先创建后启动）
func (m *Manager) CreateListener(in CreateListenerInput) (*database.C2Listener, error) {
	if strings.TrimSpace(in.Name) == "" {
		return nil, ErrInvalidInput
	}
	if !IsValidListenerType(in.Type) {
		return nil, ErrUnsupportedType
	}
	if err := SafeBindPort(in.BindPort); err != nil {
		return nil, &CommonError{Code: "invalid_port", Message: err.Error(), HTTP: 400}
	}
	bindHost := strings.TrimSpace(in.BindHost)
	if bindHost == "" {
		bindHost = "127.0.0.1" // 默认绑定环回，需要外网时操作员显式改
	}
	cfg := in.Config
	if cfg == nil {
		cfg = &ListenerConfig{}
	} else {
		cp := *cfg
		cfg = &cp
	}
	if ch := strings.TrimSpace(in.CallbackHost); ch != "" {
		cfg.CallbackHost = ch
	}
	cfg.ApplyDefaults()
	cfgJSON, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshal listener config: %w", err)
	}
	keyB64, err := GenerateAESKey()
	if err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}
	tokenB64, err := GenerateImplantToken()
	if err != nil {
		return nil, fmt.Errorf("generate token: %w", err)
	}

	listener := &database.C2Listener{
		ID:            "l_" + strings.ReplaceAll(uuid.New().String(), "-", "")[:14],
		ProjectID:     strings.TrimSpace(in.ProjectID),
		Name:          strings.TrimSpace(in.Name),
		Type:          strings.ToLower(strings.TrimSpace(in.Type)),
		BindHost:      bindHost,
		BindPort:      in.BindPort,
		ProfileID:     strings.TrimSpace(in.ProfileID),
		EncryptionKey: keyB64,
		ImplantToken:  tokenB64,
		Status:        "stopped",
		ConfigJSON:    string(cfgJSON),
		Remark:        strings.TrimSpace(in.Remark),
		CreatedAt:     time.Now(),
	}
	if err := m.db.CreateC2Listener(listener); err != nil {
		return nil, err
	}
	m.publishEvent("info", "listener", "", "", fmt.Sprintf("Listener created: %s", listener.Name), map[string]interface{}{
		"listener_id": listener.ID,
		"type":        listener.Type,
	})
	return listener, nil
}

// StartListener 启动指定 listener；幂等（已运行时返回 ErrListenerRunning）
func (m *Manager) StartListener(id string) (*database.C2Listener, error) {
	rec, err := m.db.GetC2Listener(id)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, ErrListenerNotFound
	}
	m.mu.Lock()
	if _, ok := m.runningListeners[id]; ok {
		m.mu.Unlock()
		return rec, ErrListenerRunning
	}
	m.mu.Unlock()

	cfg := &ListenerConfig{}
	if rec.ConfigJSON != "" {
		_ = json.Unmarshal([]byte(rec.ConfigJSON), cfg)
	}
	cfg.ApplyDefaults()

	// 通过工厂创建具体实现。必须使用 rec 的副本：HTTP handler 在返回 JSON 前会清空
	// rec.ImplantToken / EncryptionKey 做脱敏，若 listener 实现持有同一指针会导致 beacon 鉴权永久失败。
	listenerRec := *rec
	factory := m.registry.Get(rec.Type)
	if factory == nil {
		return nil, ErrUnsupportedType
	}
	inst, err := factory(ListenerCreationCtx{
		Listener: &listenerRec,
		Config:   cfg,
		Manager:  m,
		Logger:   m.logger.With(zap.String("listener_id", rec.ID), zap.String("type", rec.Type)),
	})
	if err != nil {
		return nil, err
	}
	if err := inst.Start(); err != nil {
		now := time.Now()
		_ = m.db.SetC2ListenerStatus(rec.ID, "error", err.Error(), &now)
		m.publishEvent("warn", "listener", "", "", fmt.Sprintf("Listener failed to start: %s: %v", rec.Name, err), map[string]interface{}{
			"listener_id": rec.ID,
		})
		return nil, err
	}
	m.mu.Lock()
	m.runningListeners[rec.ID] = inst
	m.mu.Unlock()
	now := time.Now()
	_ = m.db.SetC2ListenerStatus(rec.ID, "running", "", &now)
	rec.Status = "running"
	rec.StartedAt = &now
	rec.LastError = ""
	m.publishEvent("info", "listener", "", "", fmt.Sprintf("Listener started: %s", rec.Name), map[string]interface{}{
		"listener_id": rec.ID,
		"bind":        fmt.Sprintf("%s:%d", rec.BindHost, rec.BindPort),
	})
	return rec, nil
}

// StopListener 停止；幂等（未运行时返回 ErrListenerStopped）
func (m *Manager) StopListener(id string) error {
	m.mu.Lock()
	inst, ok := m.runningListeners[id]
	if ok {
		delete(m.runningListeners, id)
	}
	m.mu.Unlock()
	if !ok {
		return ErrListenerStopped
	}
	if err := inst.Stop(); err != nil {
		return err
	}
	_ = m.db.SetC2ListenerStatus(id, "stopped", "", nil)
	rec, _ := m.db.GetC2Listener(id)
	name := id
	if rec != nil {
		name = rec.Name
	}
	m.publishEvent("info", "listener", "", "", fmt.Sprintf("Listener stopped: %s", name), map[string]interface{}{
		"listener_id": id,
	})
	return nil
}

// DeleteListener 停止并删除（级联 sessions/tasks/files）
func (m *Manager) DeleteListener(id string) error {
	_ = m.StopListener(id)
	return m.db.DeleteC2Listener(id)
}

// IsListenerRunning 内存中的运行状态（DB 中的 status 可能因崩溃而过时）
func (m *Manager) IsListenerRunning(id string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.runningListeners[id]
	return ok
}

// RestoreRunningListeners 启动期把 DB 中 status=running 的 listener 重新拉起；
// 失败的会被改为 status=error，不会阻塞整个 App 启动。
func (m *Manager) RestoreRunningListeners() {
	listeners, err := m.db.ListC2Listeners()
	if err != nil {
		m.logger.Warn("Failed to restore the C2 listener: list query failed", zap.Error(err))
		return
	}
	for _, l := range listeners {
		if l.Status != "running" {
			continue
		}
		if _, err := m.StartListener(l.ID); err != nil && !errors.Is(err, ErrListenerRunning) {
			m.logger.Warn("Failed to restore the C2 listener", zap.String("listener_id", l.ID), zap.Error(err))
		}
	}
}

// ----------------------------------------------------------------------------
// Session 生命周期
// ----------------------------------------------------------------------------

// IngestCheckIn beacon 上线/心跳的统一入口。
// 行为：
//  1. 若 implant_uuid 已有会话 → 更新心跳/状态
//  2. 否则创建新会话，触发 OnSessionFirstSeen 钩子
func (m *Manager) IngestCheckIn(listenerID string, req ImplantCheckInRequest) (*database.C2Session, error) {
	if strings.TrimSpace(req.ImplantUUID) == "" {
		return nil, ErrInvalidInput
	}
	existing, err := m.db.GetC2SessionByImplantUUID(req.ImplantUUID)
	if err != nil {
		return nil, err
	}
	if existing != nil && existing.ListenerID != listenerID {
		return nil, ErrAuthFailed
	}
	now := time.Now()
	isFirstSeen := existing == nil
	var sessID string
	if existing != nil {
		sessID = existing.ID
	} else {
		sessID = "s_" + strings.ReplaceAll(uuid.New().String(), "-", "")[:14]
	}
	session := &database.C2Session{
		ID:            sessID,
		ListenerID:    listenerID,
		ImplantUUID:   req.ImplantUUID,
		Hostname:      req.Hostname,
		Username:      req.Username,
		OS:            strings.ToLower(req.OS),
		Arch:          strings.ToLower(req.Arch),
		PID:           req.PID,
		ProcessName:   req.ProcessName,
		IsAdmin:       req.IsAdmin,
		InternalIP:    req.InternalIP,
		UserAgent:     req.UserAgent,
		SleepSeconds:  req.SleepSeconds,
		JitterPercent: req.JitterPercent,
		Status:        string(SessionActive),
		FirstSeenAt:   now,
		LastCheckIn:   now,
		Metadata:      req.Metadata,
	}
	if existing != nil {
		// 保留原 ID/FirstSeenAt/Note 与操作员设置的 sleep/jitter，避免被 beacon 心跳上报覆盖
		session.FirstSeenAt = existing.FirstSeenAt
		session.SleepSeconds = existing.SleepSeconds
		session.JitterPercent = existing.JitterPercent
		if session.Note == "" {
			session.Note = existing.Note
		}
	}
	if err := m.db.UpsertC2Session(session); err != nil {
		return nil, err
	}
	if isFirstSeen {
		m.publishEvent("critical", "session", session.ID, "",
			fmt.Sprintf("New session online: %s@%s (%s/%s)", session.Username, session.Hostname, session.OS, session.Arch),
			map[string]interface{}{
				"session_id":  session.ID,
				"listener_id": listenerID,
				"hostname":    session.Hostname,
				"os":          session.OS,
				"arch":        session.Arch,
				"internal_ip": session.InternalIP,
			})
		m.mu.RLock()
		hook := m.hooks.OnSessionFirstSeen
		m.mu.RUnlock()
		if hook != nil {
			go hook(session)
		}
	}
	// 普通心跳：last_check_in 已由 UpsertC2Session 写入 c2_sessions，不再落 c2_events。
	// 否则按 sleep 周期每条心跳一条审计，库表与 SSE 会被迅速撑爆；上线/掉线等仍照常 publishEvent。
	return session, nil
}

// SetSessionSleep 更新会话期望的心跳间隔，并向植入体下发 sleep 任务以尽快生效。
func (m *Manager) SetSessionSleep(sessionID string, sleepSeconds, jitterPercent int) (*database.C2Task, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, ErrInvalidInput
	}
	if sleepSeconds < 1 {
		sleepSeconds = 1
	}
	if jitterPercent < 0 {
		jitterPercent = 0
	}
	if jitterPercent > 100 {
		jitterPercent = 100
	}
	if err := m.db.SetC2SessionSleep(sessionID, sleepSeconds, jitterPercent); err != nil {
		return nil, err
	}
	task, err := m.EnqueueTask(EnqueueTaskInput{
		SessionID: sessionID,
		TaskType:  TaskTypeSleep,
		Payload: map[string]interface{}{
			"seconds": sleepSeconds,
			"jitter":  jitterPercent,
		},
		Source: "manual",
	})
	if err != nil {
		m.logger.Warn("Failed to enqueue the sleep task", zap.Error(err), zap.String("session_id", sessionID))
	}
	m.publishEvent("info", "session", sessionID, "",
		fmt.Sprintf("Sleep updated: %ds (jitter %d%%)", sleepSeconds, jitterPercent),
		map[string]interface{}{
			"sleep_seconds":  sleepSeconds,
			"jitter_percent": jitterPercent,
		})
	return task, nil
}

// MarkSessionDead 心跳超时检测器调用：标记会话为 dead
func (m *Manager) MarkSessionDead(sessionID string) error {
	if err := m.db.SetC2SessionStatus(sessionID, string(SessionDead)); err != nil {
		return err
	}
	m.publishEvent("warn", "session", sessionID, "", "Session offline (heartbeat timeout)", nil)
	return nil
}

// ----------------------------------------------------------------------------
// Task 生命周期
// ----------------------------------------------------------------------------

// EnqueueTaskInput 下发任务入参
type EnqueueTaskInput struct {
	SessionID      string
	TaskType       TaskType
	Payload        map[string]interface{}
	Source         string // manual|ai|batch|api
	ConversationID string
	UserCtx        context.Context // 给 HITL 用
	BypassHITL     bool            // true 表示跳过 HITL 审批（仅供白名单机制 / 系统内部用）
}

// EnqueueTask 入队一个新任务；若任务类型危险且未 BypassHITL，且 SetHITLDangerousGate 对当前会话与 MCPToolC2Task 返回 true，才会调 HITL 桥审批。
// 返回任务记录；任务派发由 PopTasksForBeacon 在 beacon 拉任务时完成。
func (m *Manager) EnqueueTask(in EnqueueTaskInput) (*database.C2Task, error) {
	if strings.TrimSpace(in.SessionID) == "" {
		return nil, ErrInvalidInput
	}
	session, err := m.db.GetC2Session(in.SessionID)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, ErrSessionNotFound
	}
	if session.Status == string(SessionDead) || session.Status == string(SessionKilled) {
		return nil, &CommonError{Code: "session_inactive", Message: "Session is offline; cannot dispatch task", HTTP: 409}
	}

	// OPSEC: command deny regex enforcement
	if in.TaskType == TaskTypeExec || in.TaskType == TaskTypeShell {
		cmd, _ := in.Payload["command"].(string)
		if cmd != "" {
			listenerCfg := m.getListenerConfig(session.ListenerID)
			if listenerCfg != nil {
				for _, pattern := range listenerCfg.CommandDenyRegex {
					re, err := regexp.Compile(pattern)
					if err != nil {
						m.logger.Warn("invalid command_deny_regex", zap.String("pattern", pattern), zap.Error(err))
						continue
					}
					if re.MatchString(cmd) {
						return nil, &CommonError{
							Code:    "command_denied",
							Message: fmt.Sprintf("Command denied by OPSEC rule (match: %s)", pattern),
							HTTP:    403,
						}
					}
				}
			}
		}
	}

	// OPSEC: max_concurrent_tasks enforcement
	listenerCfg := m.getListenerConfig(session.ListenerID)
	if listenerCfg != nil && listenerCfg.MaxConcurrentTasks > 0 {
		activeTasks, _ := m.db.ListC2Tasks(database.ListC2TasksFilter{
			SessionID: in.SessionID,
			Status:    string(TaskQueued),
		})
		sentTasks, _ := m.db.ListC2Tasks(database.ListC2TasksFilter{
			SessionID: in.SessionID,
			Status:    string(TaskSent),
		})
		concurrent := len(activeTasks) + len(sentTasks)
		if concurrent >= listenerCfg.MaxConcurrentTasks {
			return nil, &CommonError{
				Code:    "concurrent_limit",
				Message: fmt.Sprintf("Session has %d queued/running tasks, exceeding the concurrency limit of %d", concurrent, listenerCfg.MaxConcurrentTasks),
				HTTP:    429,
			}
		}
	}

	taskID := "t_" + strings.ReplaceAll(uuid.New().String(), "-", "")[:14]
	task := &database.C2Task{
		ID:             taskID,
		SessionID:      in.SessionID,
		TaskType:       string(in.TaskType),
		Payload:        in.Payload,
		Status:         string(TaskQueued),
		Source:         strOr(in.Source, "manual"),
		ConversationID: in.ConversationID,
		CreatedAt:      time.Now(),
	}

	// HITL 检查：仅当注入的 gate 认为当前会话应对统一 MCP 工具 c2_task 做人机协同时才走桥（关闭人机协同时与其它工具一致，直接入队）。
	if IsDangerousTaskType(in.TaskType) && !in.BypassHITL {
		m.mu.RLock()
		bridge := m.hitlBridge
		gate := m.hitlDangerousGate
		m.mu.RUnlock()
		convID := strings.TrimSpace(in.ConversationID)
		useBridge := bridge != nil && gate != nil && gate(convID, MCPToolC2Task)
		if useBridge {
			task.ApprovalStatus = "pending"
			if err := m.db.CreateC2Task(task); err != nil {
				return nil, err
			}
			m.publishEvent("warn", "task", in.SessionID, taskID, fmt.Sprintf("Dangerous task awaiting approval: %s", in.TaskType), map[string]interface{}{
				"task_id":   taskID,
				"task_type": in.TaskType,
			})
			payloadBytes, _ := json.Marshal(in.Payload)
			ctx := HITLUserContext(in.UserCtx)
			if ctx == nil {
				ctx = context.Background()
			}
			go func() {
				err := bridge.RequestApproval(ctx, HITLApprovalRequest{
					TaskID:         taskID,
					SessionID:      in.SessionID,
					TaskType:       string(in.TaskType),
					PayloadJSON:    string(payloadBytes),
					ConversationID: in.ConversationID,
					Source:         task.Source,
					Reason:         fmt.Sprintf("C2 dangerous task %s", in.TaskType),
				})
				if err != nil {
					rejected := "rejected"
					failed := string(TaskFailed)
					errMsg := "HITL rejected: " + err.Error()
					_ = m.db.UpdateC2Task(taskID, database.C2TaskUpdate{
						ApprovalStatus: &rejected,
						Status:         &failed,
						Error:          &errMsg,
					})
					m.publishEvent("warn", "task", in.SessionID, taskID, errMsg, nil)
					return
				}
				approved := "approved"
				_ = m.db.UpdateC2Task(taskID, database.C2TaskUpdate{ApprovalStatus: &approved})
				m.publishEvent("info", "task", in.SessionID, taskID, "Dangerous task approved", nil)
			}()
			return task, nil
		}
		// 未接桥或会话未开启人机协同 / 工具在白名单：直接入队
		task.ApprovalStatus = "approved"
	}

	if err := m.db.CreateC2Task(task); err != nil {
		return nil, err
	}
	m.publishEvent("info", "task", in.SessionID, taskID, fmt.Sprintf("Task enqueued: %s", in.TaskType), map[string]interface{}{
		"task_id":   taskID,
		"task_type": in.TaskType,
		"source":    task.Source,
	})
	return task, nil
}

// CancelTask 取消队列中的任务（已 sent/running 的暂不支持回滚）
func (m *Manager) CancelTask(taskID string) error {
	t, err := m.db.GetC2Task(taskID)
	if err != nil {
		return err
	}
	if t == nil {
		return ErrTaskNotFound
	}
	if t.Status != string(TaskQueued) && t.Status != string(TaskSent) {
		return &CommonError{Code: "task_running", Message: "Task is already running; cannot cancel", HTTP: 409}
	}
	cancelled := string(TaskCancelled)
	now := time.Now()
	if err := m.db.UpdateC2Task(taskID, database.C2TaskUpdate{ExpectedStatus: &t.Status, Status: &cancelled, CompletedAt: &now}); err != nil {
		return err
	}
	m.publishEvent("info", "task", t.SessionID, taskID, "Task cancelled", nil)
	return nil
}

// PopTasksForBeacon beacon check_in 后调用：取该会话所有 queued+approved 的任务，
// 内部已置为 sent；返回 TaskEnvelope，便于 listener 直接编码下发。
func (m *Manager) PopTasksForBeacon(sessionID string, limit int) ([]TaskEnvelope, error) {
	session, err := m.db.GetC2Session(sessionID)
	if err != nil {
		return nil, err
	}
	if session != nil {
		listener, err := m.db.GetC2Listener(session.ListenerID)
		if err != nil {
			return nil, err
		}
		if listener != nil && listener.Type == string(ListenerTypeTCPReverse) {
			m.mu.Lock()
			defer m.mu.Unlock()
			for _, status := range []string{string(TaskSent), string(TaskRunning)} {
				count, err := m.db.CountC2Tasks(database.ListC2TasksFilter{SessionID: sessionID, Status: status})
				if err != nil {
					return nil, err
				}
				if count > 0 {
					return []TaskEnvelope{}, nil
				}
			}
			limit = 1
		}
	}
	tasks, err := m.db.PopQueuedC2Tasks(sessionID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]TaskEnvelope, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, TaskEnvelope{TaskID: t.ID, TaskType: t.TaskType, Payload: t.Payload})
	}
	return out, nil
}

// IngestTaskResult beacon 回传任务结果的统一入口
// IngestTaskResultFromListener verifies transport-established ownership before
// accepting a result. HTTP transports authenticate the listener; persistent
// connections additionally bind the result to the session established at check-in.
func (m *Manager) IngestTaskResultFromListener(listenerID, sessionID string, report TaskResultReport) error {
	if listenerID == "" {
		return ErrAuthFailed
	}
	task, err := m.db.GetC2Task(report.TaskID)
	if err != nil {
		return err
	}
	if task == nil {
		return ErrTaskNotFound
	}
	session, err := m.db.GetC2Session(task.SessionID)
	if err != nil {
		return err
	}
	if session == nil || session.ListenerID != listenerID || (sessionID != "" && session.ID != sessionID) {
		return ErrAuthFailed
	}
	return m.IngestTaskResult(report)
}

func (m *Manager) IngestTaskResult(report TaskResultReport) error {
	if strings.TrimSpace(report.TaskID) == "" {
		return ErrInvalidInput
	}
	t, err := m.db.GetC2Task(report.TaskID)
	if err != nil {
		return err
	}
	if t == nil {
		return ErrTaskNotFound
	}
	if t.Status == string(TaskCancelled) || t.Status == string(TaskSuccess) || t.Status == string(TaskFailed) {
		return nil
	}

	startedAt := time.Unix(0, report.StartedAt*int64(time.Millisecond))
	endedAt := time.Unix(0, report.EndedAt*int64(time.Millisecond))
	if report.StartedAt == 0 {
		startedAt = time.Now()
	}
	if report.EndedAt == 0 {
		endedAt = time.Now()
	}

	status := string(TaskSuccess)
	if !report.Success {
		status = string(TaskFailed)
	}
	duration := endedAt.Sub(startedAt).Milliseconds()

	sessionOS := ""
	if sess, serr := m.db.GetC2Session(t.SessionID); serr == nil && sess != nil {
		sessionOS = sess.OS
	}
	resultText := ResolveTaskResultText(report.Output, report.OutputB64, sessionOS)
	errText := ResolveTaskResultText(report.Error, report.ErrorB64, sessionOS)

	upd := database.C2TaskUpdate{
		ExpectedStatus: &t.Status,
		Status:         &status,
		ResultText:     &resultText,
		Error:          &errText,
		StartedAt:      &startedAt,
		CompletedAt:    &endedAt,
		DurationMS:     &duration,
	}

	// blob（如截图）落盘
	if len(report.BlobBase64) > 0 {
		blobPath, err := m.saveResultBlob(t.ID, report.BlobBase64, report.BlobSuffix)
		if err == nil {
			upd.ResultBlobPath = &blobPath
		} else {
			m.logger.Warn("Failed to persist the result blob to disk", zap.Error(err), zap.String("task_id", t.ID))
		}
	}

	if err := m.db.UpdateC2Task(t.ID, upd); err != nil {
		return err
	}
	t.Status = status
	t.ResultText = resultText
	t.Error = errText

	level := "info"
	msg := fmt.Sprintf("Task completed: %s", t.TaskType)
	if !report.Success {
		level = "warn"
		msg = fmt.Sprintf("Task failed: %s (%s)", t.TaskType, report.Error)
	}
	m.publishEvent(level, "task", t.SessionID, t.ID, msg, map[string]interface{}{
		"task_id":   t.ID,
		"task_type": t.TaskType,
		"duration":  duration,
	})

	m.mu.RLock()
	hook := m.hooks.OnTaskCompleted
	m.mu.RUnlock()
	if hook != nil {
		go hook(t, t.SessionID)
	}
	return nil
}

func (m *Manager) saveResultBlob(taskID, b64Content, suffix string) (string, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" || taskID == "." || taskID == ".." ||
		strings.ContainsAny(taskID, `/\`) {
		return "", fmt.Errorf("invalid task_id")
	}

	suffix, err := normalizeResultBlobSuffix(suffix)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(m.storageDir, "results")
	if err := osMkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, taskID+suffix)
	if err := ensurePathInDir(dir, path); err != nil {
		return "", err
	}
	data, err := base64Decode(b64Content)
	if err != nil {
		return "", err
	}
	if err := osWriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func uploadPathForTask(storageDir, taskID string) (dir, path string, err error) {
	taskID = strings.TrimSpace(taskID)
	if !uploadTaskIDPattern.MatchString(taskID) {
		return "", "", fmt.Errorf("invalid task_id")
	}
	dir = filepath.Join(storageDir, "uploads")
	path = filepath.Join(dir, taskID+".bin")
	if err := ensurePathInDir(dir, path); err != nil {
		return "", "", err
	}
	return dir, path, nil
}

func normalizeResultBlobSuffix(suffix string) (string, error) {
	suffix = strings.TrimSpace(suffix)
	if suffix == "" {
		return ".bin", nil
	}
	if !strings.HasPrefix(suffix, ".") {
		suffix = "." + suffix
	}
	if !resultBlobSuffixPattern.MatchString(suffix) {
		return "", fmt.Errorf("invalid blob suffix")
	}
	return suffix, nil
}

func ensurePathInDir(dir, path string) error {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(absDir, absPath)
	if err != nil {
		return err
	}
	if rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." || filepath.IsAbs(rel) {
		return fmt.Errorf("path escapes result directory")
	}
	return nil
}

// ----------------------------------------------------------------------------
// 事件总线辅助
// ----------------------------------------------------------------------------

// publishEvent 同步写 c2_events 表 + 投放到内存事件总线
func (m *Manager) publishEvent(level, category, sessionID, taskID, message string, data map[string]interface{}) {
	id := "e_" + strings.ReplaceAll(uuid.New().String(), "-", "")[:14]
	now := time.Now()
	e := &database.C2Event{
		ID:        id,
		Level:     level,
		Category:  category,
		SessionID: sessionID,
		TaskID:    taskID,
		Message:   message,
		Data:      data,
		CreatedAt: now,
	}
	if err := m.db.AppendC2Event(e); err != nil {
		m.logger.Warn("Failed to write the C2 event", zap.Error(err), zap.String("category", category))
	}
	m.bus.Publish(&Event{
		ID:        id,
		Level:     level,
		Category:  category,
		SessionID: sessionID,
		TaskID:    taskID,
		Message:   message,
		Data:      data,
		CreatedAt: now,
	})
}

// PublishCustomEvent 给外部组件（HITL 桥 / handler）写自定义事件用
func (m *Manager) PublishCustomEvent(level, category, sessionID, taskID, message string, data map[string]interface{}) {
	m.publishEvent(level, category, sessionID, taskID, message, data)
}

// ----------------------------------------------------------------------------
// 工具函数
// ----------------------------------------------------------------------------

func strOr(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

// getListenerConfig loads and parses the listener's config JSON from DB.
func (m *Manager) getListenerConfig(listenerID string) *ListenerConfig {
	listener, err := m.db.GetC2Listener(listenerID)
	if err != nil || listener == nil {
		return nil
	}
	cfg := &ListenerConfig{}
	if listener.ConfigJSON != "" && listener.ConfigJSON != "{}" {
		_ = json.Unmarshal([]byte(listener.ConfigJSON), cfg)
	}
	return cfg
}

// GetProfile loads a C2Profile from DB by ID.
func (m *Manager) GetProfile(profileID string) (*database.C2Profile, error) {
	if strings.TrimSpace(profileID) == "" {
		return nil, nil
	}
	return m.db.GetC2Profile(profileID)
}
