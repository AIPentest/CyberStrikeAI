package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"cyberstrike-ai/internal/multiagent"
	"cyberstrike-ai/internal/runlease"
	"cyberstrike-ai/internal/security"
)

// ErrTaskCancelled 用户取消任务的错误
var ErrTaskCancelled = errors.New("agent task cancelled by user")

// ErrTaskAlreadyRunning 会话已有任务正在执行
var ErrTaskAlreadyRunning = errors.New("agent task already running for conversation")

// shouldPersistEinoAgentTraceAfterRunError：Eino 相关 Run 非成功返回时，是否仍写入 last_react_* 供下轮 loadHistoryFromAgentTrace。
// 当前策略：无论正常结束、异常结束或用户主动停止，都尽量保留最后可用轨迹，
// 以便在同一会话继续时可基于原始上下文续跑，而不是回退到仅消息文本历史。
func shouldPersistEinoAgentTraceAfterRunError(baseCtx context.Context) bool {
	return true
}

// AgentTask 描述正在运行的Agent任务
type AgentTask struct {
	RunID            string `json:"runId"`
	CleanupError     string `json:"cleanupError,omitempty"`
	processes        *security.ProcessScope
	workers          *runlease.Scope
	IsolationBackend string `json:"isolationBackend,omitempty"`
	finishing        chan struct{}
	stopping         chan struct{}
	finalStatus      string
	ConversationID   string    `json:"conversationId"`
	Title            string    `json:"title,omitempty"`
	Message          string    `json:"message,omitempty"`
	StartedAt        time.Time `json:"startedAt"`
	Status           string    `json:"status"`
	CancellingAt     time.Time `json:"-"` // 进入 cancelling 状态的时间，用于清理长时间卡住的任务

	// ActiveMCPExecutionID 当前正在执行的 MCP 工具 executionId（仅内存，供「中断并继续」= 仅掐当前工具）
	ActiveMCPExecutionID string `json:"-"`

	// InterruptContinueNote 无 MCP 时「中断并继续」由用户在弹窗中填写的补充说明（Cancel 前写入，续跑轮次读取后清空）
	InterruptContinueNote string `json:"-"`

	// activeEinoExecuteCancel 当前进行中的 Eino filesystem execute 取消函数（与 MCP 工具并行，供中断并继续）
	activeEinoExecuteCancel context.CancelFunc
	// activeEinoExecuteAbortNote AbortActiveEinoExecute 写入的用户说明，由 execute 收尾时合并进工具结果
	activeEinoExecuteAbortNote string

	// hitlCognition 本轮运行中供 HITL/审计 Agent 读取的上下文（用户原话 + 思考，不含会话历史）
	hitlCognition *hitlCognitionState

	// agentRuntimeCancel 当前 Eino ADK 原生 AgentCancelFunc 包装；取消任务时先触发它，再走 context 兜底。
	agentRuntimeCancel        func(error) bool
	agentRuntimeCancelVersion uint64

	// agentTurnLoopInterrupt 当前 Eino TurnLoop 用户补充 push hook；中断并继续时优先将补充作为新 turn item 入队。
	agentTurnLoopInterrupt        func(string) bool
	agentTurnLoopInterruptVersion uint64

	cancel func(error)
}

// RegisterRunningTool 实现 mcp.ToolRunRegistry：工具开始时登记本会话当前 executionId。
func (m *AgentTaskManager) RegisterRunningTool(conversationID, executionID string) {
	conversationID = strings.TrimSpace(conversationID)
	executionID = strings.TrimSpace(executionID)
	if conversationID == "" || executionID == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tasks[conversationID]; ok && t != nil {
		t.ActiveMCPExecutionID = executionID
	}
}

// UnregisterRunningTool 工具结束时清除登记（仅当 id 仍匹配时清除，避免并发串单）。
func (m *AgentTaskManager) UnregisterRunningTool(conversationID, executionID string) {
	conversationID = strings.TrimSpace(conversationID)
	executionID = strings.TrimSpace(executionID)
	if conversationID == "" || executionID == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tasks[conversationID]; ok && t != nil {
		if t.ActiveMCPExecutionID == executionID {
			t.ActiveMCPExecutionID = ""
		}
	}
}

// RegisterActiveEinoExecute 登记进行中的 Eino filesystem execute（每会话同时仅一条）。
func (m *AgentTaskManager) RegisterActiveEinoExecute(conversationID string, cancel context.CancelFunc) {
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" || cancel == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tasks[conversationID]; ok && t != nil {
		t.activeEinoExecuteCancel = cancel
		t.activeEinoExecuteAbortNote = ""
	}
}

// UnregisterActiveEinoExecute execute 正常结束或已取消后清除登记。
func (m *AgentTaskManager) UnregisterActiveEinoExecute(conversationID string) {
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tasks[conversationID]; ok && t != nil {
		t.activeEinoExecuteCancel = nil
		t.activeEinoExecuteAbortNote = ""
	}
}

// ConversationIDForActiveMCPExecution 根据当前登记的工具 executionId 反查会话 ID（供 MCP 监控页按 executionId 终止）。
func (m *AgentTaskManager) ConversationIDForActiveMCPExecution(executionID string) string {
	executionID = strings.TrimSpace(executionID)
	if executionID == "" {
		return ""
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for convID, t := range m.tasks {
		if t != nil && t.ActiveMCPExecutionID == executionID {
			return convID
		}
	}
	return ""
}

// ConversationIDForActiveEinoExecute 返回当前唯一进行 Eino execute 的会话 ID；多会话并行时返回空。
func (m *AgentTaskManager) ConversationIDForActiveEinoExecute() (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var found string
	count := 0
	for convID, t := range m.tasks {
		if t != nil && t.activeEinoExecuteCancel != nil {
			found = convID
			count++
		}
	}
	if count == 1 {
		return found, true
	}
	return "", false
}

// AbortActiveEinoExecute 终止当前 Eino execute 并暂存用户说明（与 MCP 工具终止一致）。
func (m *AgentTaskManager) AbortActiveEinoExecute(conversationID, note string) bool {
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" {
		return false
	}
	m.mu.Lock()
	t, ok := m.tasks[conversationID]
	if !ok || t == nil || t.activeEinoExecuteCancel == nil {
		m.mu.Unlock()
		return false
	}
	t.activeEinoExecuteAbortNote = strings.TrimSpace(note)
	cancel := t.activeEinoExecuteCancel
	m.mu.Unlock()
	cancel()
	return true
}

// TakeEinoExecuteAbortNote 读取并清空 execute 终止说明（execute 收尾时调用一次）。
func (m *AgentTaskManager) TakeEinoExecuteAbortNote(conversationID string) string {
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" {
		return ""
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tasks[conversationID]; ok && t != nil {
		n := t.activeEinoExecuteAbortNote
		t.activeEinoExecuteAbortNote = ""
		return n
	}
	return ""
}

// SetInterruptContinueNote 在发起 ErrInterruptContinue 取消前写入用户补充说明（仅内存）。
func (m *AgentTaskManager) SetInterruptContinueNote(conversationID, note string) {
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tasks[conversationID]; ok && t != nil {
		t.InterruptContinueNote = note
	}
}

// TakeInterruptContinueNote 读取并清空补充说明（续跑开始时调用一次）。
func (m *AgentTaskManager) TakeInterruptContinueNote(conversationID string) string {
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" {
		return ""
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tasks[conversationID]; ok && t != nil {
		n := t.InterruptContinueNote
		t.InterruptContinueNote = ""
		return n
	}
	return ""
}

// BindTaskCancel 在同一运行任务内替换与 context 绑定的 cancel 函数（用于中断后继续时换新 baseCtx）。
func (m *AgentTaskManager) BindTaskCancel(conversationID string, cancel context.CancelCauseFunc) {
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" || cancel == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tasks[conversationID]; ok && t != nil {
		t.cancel = func(err error) {
			cancel(err)
		}
	}
}

// BindAgentRuntimeCancel 登记当前运行段的 Eino 原生 cancel hook。
func (m *AgentTaskManager) BindAgentRuntimeCancel(conversationID string, cancel func(error) bool) func() {
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" || cancel == nil {
		return func() {}
	}
	m.mu.Lock()
	t, ok := m.tasks[conversationID]
	if !ok || t == nil {
		m.mu.Unlock()
		return func() {}
	}
	t.agentRuntimeCancelVersion++
	version := t.agentRuntimeCancelVersion
	t.agentRuntimeCancel = cancel
	m.mu.Unlock()

	return func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if cur, exists := m.tasks[conversationID]; exists && cur != nil && cur.agentRuntimeCancelVersion == version {
			cur.agentRuntimeCancel = nil
		}
	}
}

// BindAgentTurnLoopInterrupt 登记当前运行任务的 Eino TurnLoop 用户补充入队 hook。
func (m *AgentTaskManager) BindAgentTurnLoopInterrupt(conversationID string, push func(string) bool) func() {
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" || push == nil {
		return func() {}
	}
	m.mu.Lock()
	t, ok := m.tasks[conversationID]
	if !ok || t == nil {
		m.mu.Unlock()
		return func() {}
	}
	t.agentTurnLoopInterruptVersion++
	version := t.agentTurnLoopInterruptVersion
	t.agentTurnLoopInterrupt = push
	m.mu.Unlock()

	return func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if cur, exists := m.tasks[conversationID]; exists && cur != nil && cur.agentTurnLoopInterruptVersion == version {
			cur.agentTurnLoopInterrupt = nil
		}
	}
}

// ActiveMCPExecutionID 返回当前会话进行中的工具 executionId，无则空串。
func (m *AgentTaskManager) ActiveMCPExecutionID(conversationID string) string {
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" {
		return ""
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if t, ok := m.tasks[conversationID]; ok && t != nil {
		return strings.TrimSpace(t.ActiveMCPExecutionID)
	}
	return ""
}

// CompletedTask 已完成的任务（用于历史记录）
type CompletedTask struct {
	CleanupError     string    `json:"cleanupError,omitempty"`
	IsolationBackend string    `json:"isolationBackend,omitempty"`
	RunID            string    `json:"runId"`
	ConversationID   string    `json:"conversationId"`
	Title            string    `json:"title,omitempty"`
	Message          string    `json:"message,omitempty"`
	StartedAt        time.Time `json:"startedAt"`
	CompletedAt      time.Time `json:"completedAt"`
	Status           string    `json:"status"`
}

// AgentTaskManager 管理正在运行的Agent任务
type AgentTaskManager struct {
	mu               sync.RWMutex
	tasks            map[string]*AgentTask
	completedTasks   []*CompletedTask // 最近完成的任务历史
	maxHistorySize   int              // 最大历史记录数
	historyRetention time.Duration    // 历史记录保留时间
	eventBus         *TaskEventBus    // 可选：任务结束时关闭镜像 SSE 订阅
	// toolCanceler 在用户整轮停止任务或会话结束时终止该会话仍在运行的 MCP 工具（非「中断并继续」）。
	toolCanceler func(conversationID string)
	shuttingDown bool
	shutdown     chan struct{}
	shutdownOnce sync.Once
}

const (
	// cancellingStuckThreshold 处于「取消中」超过此时长则强制从运行列表移除。正常取消会在当前步骤内返回，
	// 超过则视为卡住，尽快释放会话。常见做法多为 30–60s 内释放。
	cancellingStuckThreshold = 45 * time.Second
	// cancellingStuckThresholdLegacy 未记录 CancellingAt 时用 StartedAt 判断的兜底时长
	cancellingStuckThresholdLegacy = 2 * time.Minute
	cleanupInterval                = 15 * time.Second // 与上面阈值配合，最长约 60s 内移除
)

// NewAgentTaskManager 创建任务管理器
func NewAgentTaskManager() *AgentTaskManager {
	m := &AgentTaskManager{
		tasks:            make(map[string]*AgentTask),
		shutdown:         make(chan struct{}),
		completedTasks:   make([]*CompletedTask, 0),
		maxHistorySize:   50,             // 最多保留50条历史记录
		historyRetention: 24 * time.Hour, // 保留24小时
	}
	go m.runStuckCancellingCleanup()
	return m
}

// SetTaskEventBus 设置任务事件总线（与 AgentHandler 共用同一实例）。
func (m *AgentTaskManager) SetTaskEventBus(b *TaskEventBus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.eventBus = b
}

// SetToolCanceler 设置整轮停止任务/会话结束时终止仍在运行 MCP 工具的回调（由 AgentHandler 注入）。
func (m *AgentTaskManager) SetToolCanceler(fn func(conversationID string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.toolCanceler = fn
}

// GetTask 返回运行中任务（无则 nil）。
func (m *AgentTaskManager) GetTask(conversationID string) *AgentTask {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.tasks[conversationID]
}

// GetTaskSnapshot 返回运行任务的只读副本，供状态展示使用，避免锁外读取可变任务字段。
func (m *AgentTaskManager) GetTaskSnapshot(conversationID string) *AgentTask {
	m.mu.RLock()
	defer m.mu.RUnlock()
	task := m.tasks[conversationID]
	if task == nil {
		return nil
	}
	snapshot := *task
	snapshot.IsolationBackend = task.processes.IsolationBackend()
	return &snapshot
}

// runStuckCancellingCleanup 定期将长时间处于「取消中」的任务强制结束，避免卡住无法发新消息
func (m *AgentTaskManager) runStuckCancellingCleanup() {
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-m.shutdown:
			return
		case <-ticker.C:
			m.cleanupStuckCancelling()
		}
	}
}

func (m *AgentTaskManager) cleanupStuckCancelling() {
	m.mu.Lock()
	type pendingFinish struct{ id, runID, status string }
	var toFinish []pendingFinish
	now := time.Now()
	for id, task := range m.tasks {
		if task.Status == "cleanup_failed" {
			toFinish = append(toFinish, pendingFinish{id, task.RunID, task.finalStatus})
			continue
		}
		if task.Status != "cancelling" {
			continue
		}
		var elapsed time.Duration
		if !task.CancellingAt.IsZero() {
			elapsed = now.Sub(task.CancellingAt)
			if elapsed < cancellingStuckThreshold {
				continue
			}
		} else {
			elapsed = now.Sub(task.StartedAt)
			if elapsed < cancellingStuckThresholdLegacy {
				continue
			}
		}
		toFinish = append(toFinish, pendingFinish{id, task.RunID, "cancelled"})
	}
	m.mu.Unlock()
	for _, pending := range toFinish {
		_ = m.FinishTaskRun(pending.id, pending.runID, pending.status)
	}
}

// StartTask 注册并开始一个新的任务
func (m *AgentTaskManager) StartTask(conversationID, message string, cancel context.CancelCauseFunc) (*AgentTask, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.shuttingDown {
		return nil, errors.New("task manager is shutting down")
	}
	if _, exists := m.tasks[conversationID]; exists {
		return nil, ErrTaskAlreadyRunning
	}

	scope := security.NewProcessScope()
	task := &AgentTask{
		RunID: scope.ID, processes: scope, workers: runlease.New(),
		ConversationID: conversationID,
		Message:        message,
		StartedAt:      time.Now(),
		Status:         "running",
		cancel: func(err error) {
			if cancel != nil {
				cancel(err)
			}
		},
	}

	m.tasks[conversationID] = task
	task.hitlCognition = &hitlCognitionState{UserMessage: strings.TrimSpace(message)}
	return task, nil
}

// CancelTask 取消指定会话的任务。若任务已在取消中，仍返回 (true, nil) 以便接口幂等、前端不报错。
func (m *AgentTaskManager) CancelTask(conversationID string, cause error) (bool, error) {
	m.mu.Lock()
	task, exists := m.tasks[conversationID]
	if !exists {
		m.mu.Unlock()
		return false, nil
	}

	// 如果已经处于取消流程，视为成功（幂等），避免前端重复点击报「未找到任务」
	if task.Status == "cancelling" || task.finishing != nil {
		m.mu.Unlock()
		return true, nil
	}

	// ErrInterruptContinue：仅掐断当前推理步骤，随后由处理器续跑，不进入长时间「取消中」态。
	if cause != nil && errors.Is(cause, multiagent.ErrInterruptContinue) {
		task.Status = "running"
	} else {
		task.Status = "cancelling"
		task.CancellingAt = time.Now()
	}
	if cause != nil && errors.Is(cause, ErrTaskCancelled) {
		task.InterruptContinueNote = ""
	}
	cancel := task.cancel
	if cause == nil {
		cause = ErrTaskCancelled
	}
	interruptPush := task.agentTurnLoopInterrupt
	interruptNote := task.InterruptContinueNote
	runtimeCancel := task.agentRuntimeCancel
	activeExecuteCancel := task.activeEinoExecuteCancel
	if !errors.Is(cause, multiagent.ErrInterruptContinue) {
		task.processes.Seal()
		task.workers.Seal()
		task.stopping = make(chan struct{})
		defer close(task.stopping)
	}
	var toolCanceler func(string)
	if errors.Is(cause, ErrTaskCancelled) {
		toolCanceler = m.toolCanceler
	}
	m.mu.Unlock()

	if errors.Is(cause, multiagent.ErrInterruptContinue) && interruptPush != nil && interruptPush(interruptNote) {
		m.mu.Lock()
		if cur, exists := m.tasks[conversationID]; exists && cur != nil {
			cur.InterruptContinueNote = ""
		}
		m.mu.Unlock()
		return true, nil
	}

	runtimeHandled := false
	if runtimeCancel != nil {
		runtimeHandled = runtimeCancel(cause)
	}
	// 「彻底停止」必须同时取消宿主 context：原生 Agent Cancel 即使已受理，
	// 也可能只在安全点返回或报告超时，不能据此让整条任务继续存活。
	// 中断并继续仍保留原语义：原生取消已处理时由运行时负责恢复。
	if cancel != nil && (!runtimeHandled || errors.Is(cause, ErrTaskCancelled)) {
		cancel(cause)
	}
	if toolCanceler != nil {
		toolCanceler(conversationID)
	}
	if !errors.Is(cause, multiagent.ErrInterruptContinue) {
		task.workers.Cancel()
		if activeExecuteCancel != nil {
			activeExecuteCancel()
		}
		return true, task.processes.Close()
	}
	return true, nil
}

// UpdateTaskStatus 更新任务状态但不删除任务（用于在发送事件前更新状态）
func (m *AgentTaskManager) UpdateTaskStatus(conversationID string, status string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	task, exists := m.tasks[conversationID]
	if !exists {
		return
	}

	if task.finishing != nil || task.Status == "cleanup_failed" {
		return
	}
	switch status {
	case "completed", "cancelled", "failed", "timeout":
		task.finalStatus = status
		task.Status = "cleaning"
		task.processes.Seal()
		task.workers.Seal()
	default:
		if status != "" {
			task.Status = status
		}
	}
}

// BindProcessScope snapshots ownership once at task start. Continuations must
// derive from this context, never resolve ownership again using conversation ID.
func (m *AgentTaskManager) BindProcessScope(ctx context.Context, conversationID, runID string) context.Context {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if task := m.tasks[conversationID]; task != nil && task.RunID == runID {
		return runlease.WithScope(security.WithProcessScope(ctx, task.processes), task.workers)
	}
	// Fail closed if a task disappeared before its execution context was bound.
	scope := security.NewProcessScope()
	scope.Seal()
	workers := runlease.New()
	workers.Seal()
	return runlease.WithScope(security.WithProcessScope(ctx, scope), workers)
}

// FinishTask is retained for callers that operate on the current task. Owners
// use FinishTaskRun, so a delayed defer cannot finish a newer conversation run.
func (m *AgentTaskManager) FinishTask(conversationID string, finalStatus string) {
	m.mu.RLock()
	task := m.tasks[conversationID]
	m.mu.RUnlock()
	if task != nil {
		_ = m.FinishTaskRun(conversationID, task.RunID, finalStatus)
	}
}

func (m *AgentTaskManager) FinishTaskRun(conversationID, runID, finalStatus string) error {
	m.mu.Lock()
	task := m.tasks[conversationID]
	if task == nil || task.RunID != runID {
		m.mu.Unlock()
		return nil
	}
	if task.stopping != nil {
		select {
		case <-task.stopping:
		default:
			stopping := task.stopping
			m.mu.Unlock()
			<-stopping
			return m.FinishTaskRun(conversationID, runID, finalStatus)
		}
	}
	if task.finishing != nil {
		done := task.finishing
		m.mu.Unlock()
		<-done
		m.mu.RLock()
		cleanupError := task.CleanupError
		m.mu.RUnlock()
		if cleanupError != "" {
			return errors.New(cleanupError)
		}
		return nil
	}
	done := make(chan struct{})
	task.finishing = done
	task.finalStatus = finalStatus
	task.Status = "cleaning"
	task.processes.Seal()
	task.workers.Seal()
	toolCanceler := m.toolCanceler
	activeCancel := task.activeEinoExecuteCancel
	cancel := task.cancel
	bus := m.eventBus
	m.mu.Unlock()

	// Keep the conversation occupied throughout cleanup, including callbacks.
	if cancel != nil {
		cancel(nil)
	}
	if toolCanceler != nil {
		toolCanceler(conversationID)
	}
	if activeCancel != nil {
		activeCancel()
	}
	task.workers.Cancel()
	processErr := task.processes.Close()
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 3*time.Second)
	workerErr := task.workers.Wait(waitCtx)
	waitCancel()
	cleanupErr := errors.Join(processErr, workerErr)
	if processErr != nil && errors.Is(workerErr, runlease.ErrUnconfirmed) {
		// A simultaneous local failure must not be labelled as local completion.
		cleanupErr = fmt.Errorf("local cleanup: %w; remote state: %v", processErr, workerErr)
	}
	// The local worker has returned, but remote notification cancellation is
	// not an acknowledgement. Preserve an actionable history/tool status.
	unconfirmed := processErr == nil && errors.Is(workerErr, runlease.ErrUnconfirmed)
	if unconfirmed {
		finalStatus = "cleanup_unconfirmed"
	}
	cleanupMessage := ""
	if cleanupErr != nil {
		cleanupMessage = cleanupErr.Error()
	}
	if (cleanupErr == nil || unconfirmed) && bus != nil {
		// Subscribers must receive completion only after local processes are reaped.
		payload, _ := json.Marshal(StreamEvent{Type: "done", Data: map[string]interface{}{"conversationId": conversationID, "runId": runID, "status": finalStatus, "cleanupError": cleanupMessage}})
		bus.Publish(conversationID, append(append([]byte("data: "), payload...), '\n', '\n'))
		bus.CloseConversation(conversationID)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	defer close(done)
	if cleanupErr != nil && !unconfirmed {
		task.Status = "cleanup_failed"
		task.CleanupError = cleanupErr.Error()
		task.finishing = nil
		return cleanupErr
	}
	task.CleanupError = ""
	if unconfirmed {
		task.CleanupError = cleanupErr.Error()
	}
	task.Status = finalStatus
	m.completedTasks = append(m.completedTasks, &CompletedTask{
		RunID: task.RunID, CleanupError: task.CleanupError, IsolationBackend: task.processes.IsolationBackend(), ConversationID: task.ConversationID, Message: task.Message,
		StartedAt: task.StartedAt, CompletedAt: time.Now(), Status: finalStatus,
	})
	m.cleanupHistory()
	delete(m.tasks, conversationID)
	return cleanupErr
}

// Shutdown rejects new tasks before cancelling and reaping existing task jobs.
func (m *AgentTaskManager) Shutdown() {
	m.mu.Lock()
	m.shuttingDown = true
	m.shutdownOnce.Do(func() { close(m.shutdown) })
	tasks := make([]*AgentTask, 0, len(m.tasks))
	for _, task := range m.tasks {
		tasks = append(tasks, task)
		task.processes.Seal()
		task.workers.Seal()
	}
	m.mu.Unlock()
	var wg sync.WaitGroup
	for _, task := range tasks {
		wg.Add(1)
		go func(task *AgentTask) {
			defer wg.Done()
			_, _ = m.CancelTask(task.ConversationID, ErrTaskCancelled)
			_ = m.FinishTaskRun(task.ConversationID, task.RunID, "cancelled")
		}(task)
	}
	wg.Wait()
}

// cleanupHistory 清理过期的历史记录
func (m *AgentTaskManager) cleanupHistory() {
	now := time.Now()
	cutoffTime := now.Add(-m.historyRetention)

	// 过滤掉过期的记录
	validTasks := make([]*CompletedTask, 0, len(m.completedTasks))
	for _, task := range m.completedTasks {
		if task.CompletedAt.After(cutoffTime) {
			validTasks = append(validTasks, task)
		}
	}

	// 如果仍然超过最大数量，只保留最新的
	if len(validTasks) > m.maxHistorySize {
		// 按完成时间排序，保留最新的
		// 由于是追加的，最新的在最后，所以直接取最后N个
		start := len(validTasks) - m.maxHistorySize
		validTasks = validTasks[start:]
	}

	m.completedTasks = validTasks
}

// GetActiveTasks 返回所有正在运行的任务
func (m *AgentTaskManager) GetActiveTasks() []*AgentTask {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*AgentTask, 0, len(m.tasks))
	for _, task := range m.tasks {
		result = append(result, &AgentTask{
			RunID: task.RunID, CleanupError: task.CleanupError, IsolationBackend: task.processes.IsolationBackend(),
			ConversationID: task.ConversationID,
			Message:        task.Message,
			StartedAt:      task.StartedAt,
			Status:         task.Status,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].StartedAt.Equal(result[j].StartedAt) {
			return result[i].ConversationID < result[j].ConversationID
		}
		return result[i].StartedAt.Before(result[j].StartedAt)
	})
	return result
}

// GetCompletedTasks 返回最近完成的任务历史
func (m *AgentTaskManager) GetCompletedTasks() []*CompletedTask {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 清理过期记录（只读锁，不影响其他操作）
	// 注意：这里不能直接调用cleanupHistory，因为需要写锁
	// 所以返回时过滤过期记录
	now := time.Now()
	cutoffTime := now.Add(-m.historyRetention)

	result := make([]*CompletedTask, 0, len(m.completedTasks))
	for _, task := range m.completedTasks {
		if task.CompletedAt.After(cutoffTime) {
			result = append(result, task)
		}
	}

	// 按完成时间倒序排序（最新的在前）
	// 由于是追加的，最新的在最后，需要反转
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}

	// 限制返回数量
	if len(result) > m.maxHistorySize {
		result = result[:m.maxHistorySize]
	}

	return result
}
