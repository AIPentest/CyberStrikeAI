package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"cyberstrike-ai/internal/authctx"
	"cyberstrike-ai/internal/runlease"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

const (
	ToolExecutionStatusQueued      = "queued"
	ToolExecutionStatusRunning     = "running"
	ToolExecutionStatusCompleted   = "completed"
	ToolExecutionStatusBlocked     = "blocked"
	ToolExecutionStatusFailed      = "failed"
	ToolExecutionStatusCancelled   = "cancelled"
	ToolExecutionStatusHardTimeout = "hard_timeout"
	ToolExecutionStatusOrphaned    = "orphaned"
)

var ErrExecutionWaitTimeout = errors.New("tool execution wait timeout")

// ExecutionRunFunc is the blocking operation owned by a worker.
type ExecutionRunFunc func(context.Context) (*ToolResult, error)

type ExecutionPreRunFunc func(context.Context, *ToolExecution) (func(), error)

// ExecutionDoneFunc observes the final persisted state. It is invoked once,
// including for late completions after an agent has stopped waiting.
type ExecutionDoneFunc func(*ToolExecution)

type ExecutionRequest struct {
	Remote bool
	// A remote adapter may positively confirm server-side cancellation.
	ConfirmCancellation func(context.Context) error
	ID                  string
	ToolName            string
	Arguments           map[string]interface{}
	ConversationID      string
	OwnerUserID         string
	HardTimeout         time.Duration
	PreRun              ExecutionPreRunFunc
	Run                 ExecutionRunFunc
	OnDone              ExecutionDoneFunc
}

type ExecutionHandle struct {
	ID string
}

type ExecutionSnapshot struct {
	Execution *ToolExecution
}

type executionEntry struct {
	releaseLease        func()
	remote              bool
	runStarted          bool
	confirmCancellation func(context.Context) error
	exec                *ToolExecution
	cancel              context.CancelFunc
	done                chan struct{}
	preRun              ExecutionPreRunFunc
	run                 ExecutionRunFunc
	result              *ToolResult
	err                 error
}

// ExecutionService keeps Eino-facing tool calls synchronous while moving the
// untrusted blocking work into cancellable workers with explicit execution IDs.
type ExecutionService struct {
	storage MonitorStorage
	logger  *zap.Logger

	mu             sync.Mutex
	entries        map[string]*executionEntry
	abortUserNotes map[string]string
	maxInMemory    int
	resultMaxBytes int
	spillRootDir   string
}

func NewExecutionService(storage MonitorStorage, logger *zap.Logger) *ExecutionService {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &ExecutionService{
		storage:        storage,
		logger:         logger,
		entries:        make(map[string]*executionEntry),
		abortUserNotes: make(map[string]string),
		maxInMemory:    1000,
		resultMaxBytes: DefaultToolResultMaxBytes,
	}
}

func (s *ExecutionService) ConfigureToolResultMaxBytes(maxBytes int) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resultMaxBytes = maxBytes
}

// ConfigureToolResultSpillRoot sets the reduction-compatible root used when
// oversized tool results are spilled to local files (empty → tmp/reduction).
func (s *ExecutionService) ConfigureToolResultSpillRoot(rootDir string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.spillRootDir = strings.TrimSpace(rootDir)
}

func (s *ExecutionService) Submit(ctx context.Context, req ExecutionRequest) (*ExecutionHandle, error) {
	if s == nil {
		return nil, fmt.Errorf("execution service is nil")
	}
	if req.Run == nil {
		return nil, fmt.Errorf("execution run func is nil")
	}
	id := strings.TrimSpace(req.ID)
	if id == "" {
		id = uuid.New().String()
	}
	start := time.Now()
	exec := &ToolExecution{
		ID:             id,
		ToolName:       strings.TrimSpace(req.ToolName),
		Arguments:      cloneArgsMap(req.Arguments),
		Status:         ToolExecutionStatusQueued,
		StartTime:      start,
		ConversationID: strings.TrimSpace(req.ConversationID),
		OwnerUserID:    strings.TrimSpace(req.OwnerUserID),
	}
	if exec.ConversationID == "" {
		exec.ConversationID = MCPConversationIDFromContext(ctx)
	}
	if exec.OwnerUserID == "" {
		if principal, ok := authctx.PrincipalFromContext(ctx); ok {
			exec.OwnerUserID = principal.UserID
		}
	}

	runCtx := detachedExecutionContext(ctx)
	var cancel context.CancelFunc
	if req.HardTimeout > 0 {
		runCtx, cancel = context.WithTimeout(runCtx, req.HardTimeout)
	} else {
		runCtx, cancel = context.WithCancel(runCtx)
	}
	releaseLease, leaseErr := runlease.FromContext(ctx).Register(id, cancel)
	if leaseErr != nil {
		cancel()
		return nil, leaseErr
	}
	entry := &executionEntry{exec: exec, cancel: cancel, done: make(chan struct{}), preRun: req.PreRun, run: req.Run,
		releaseLease: releaseLease, remote: req.Remote, confirmCancellation: req.ConfirmCancellation}

	s.mu.Lock()
	if _, exists := s.entries[id]; exists {
		s.mu.Unlock()
		cancel()
		releaseLease()
		return nil, fmt.Errorf("execution already exists: %s", id)
	}
	s.entries[id] = entry
	s.cleanupOldEntriesLocked()
	s.mu.Unlock()

	if s.storage != nil {
		if err := s.storage.SaveToolExecution(exec); err != nil {
			s.logger.Warn("保存执行记录到数据库失败", zap.Error(err), zap.String("executionId", id))
		}
	}
	notifyToolRunBegin(ctx, id)

	go s.runWorker(runCtx, entry, req.OnDone)
	return &ExecutionHandle{ID: id}, nil
}

func (s *ExecutionService) runWorker(ctx context.Context, entry *executionEntry, onDone ExecutionDoneFunc) {
	id := entry.exec.ID
	ctx = WithMCPExecutionID(ctx, id)
	if conv := strings.TrimSpace(entry.exec.ConversationID); conv != "" {
		ctx = WithMCPConversationID(ctx, conv)
	}
	var release func()
	defer func() {
		if release != nil {
			release()
		}
		entry.cancel()
		notifyToolRunEnd(ctx, id)
		close(entry.done)
		if entry.releaseLease != nil {
			entry.releaseLease()
		}
	}()

	if ctx.Err() != nil {
		s.finishEntry(ctx, entry, nil, ctx.Err(), onDone)
		return
	}
	if entry.preRun != nil {
		var preErr error
		release, preErr = entry.preRun(ctx, cloneToolExecution(entry.exec))
		if preErr != nil {
			s.finishEntry(ctx, entry, nil, preErr, onDone)
			return
		}
	}
	if ctx.Err() != nil {
		s.finishEntry(ctx, entry, nil, ctx.Err(), onDone)
		return
	}
	s.markEntryRunning(entry)
	entry.runStarted = true

	result, err := entryResultRecover(ctx, entry.exec.ToolName, s.logger, func() (*ToolResult, error) {
		return nilSafeRun(ctx, entry)
	})
	s.finishEntry(ctx, entry, result, err, onDone)
}

func (s *ExecutionService) markEntryRunning(entry *executionEntry) {
	if s == nil || entry == nil || entry.exec == nil {
		return
	}
	s.mu.Lock()
	if !isExecutionTerminal(entry.exec.Status) {
		entry.exec.Status = ToolExecutionStatusRunning
	}
	runningExec := cloneToolExecution(entry.exec)
	s.mu.Unlock()
	if s.storage != nil {
		if err := s.storage.SaveToolExecution(runningExec); err != nil {
			s.logger.Warn("保存执行记录到数据库失败", zap.Error(err), zap.String("executionId", runningExec.ID))
		}
	}
}

func (s *ExecutionService) finishEntry(ctx context.Context, entry *executionEntry, result *ToolResult, err error, onDone ExecutionDoneFunc) {
	id := entry.exec.ID
	var blockedErr *toolGuardBlockError
	if errors.As(err, &blockedErr) {
		result, err = blockedErr.result, nil
	}
	cancellationUnconfirmed := entry.remote && entry.runStarted && ctx.Err() != nil && err != nil
	if cancellationUnconfirmed && entry.confirmCancellation != nil {
		confirmCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		cancellationUnconfirmed = entry.confirmCancellation(confirmCtx) != nil
		cancel()
	}
	cancelledWithUserNote := s.applyAbortUserNoteToCancelledToolResult(id, &result, &err)

	now := time.Now()
	s.mu.Lock()
	spill := ToolResultSpillConfig{
		RootDir:        s.spillRootDir,
		ConversationID: entry.exec.ConversationID,
		ExecutionID:    id,
	}
	if ctx != nil {
		if pid := MCPProjectIDFromContext(ctx); pid != "" {
			spill.ProjectID = pid
		}
		if conv := MCPConversationIDFromContext(ctx); conv != "" {
			spill.ConversationID = conv
		}
	}
	result = NormalizeToolResultForStorageWithSpill(result, s.resultMaxBytes, spill)
	entry.result = result
	entry.err = err
	entry.exec.EndTime = &now
	entry.exec.Duration = now.Sub(entry.exec.StartTime)
	if err != nil {
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			entry.exec.Status = ToolExecutionStatusHardTimeout
			entry.exec.Error = "工具执行超过硬超时限制"
		case errors.Is(err, context.Canceled):
			entry.exec.Status = ToolExecutionStatusCancelled
			entry.exec.Error = "已手动终止或任务已取消"
		default:
			entry.exec.Status = ToolExecutionStatusFailed
			entry.exec.Error = err.Error()
		}
	} else if result != nil && result.Blocked {
		entry.exec.Status = ToolExecutionStatusBlocked
		entry.exec.Error = firstToolResultText(result, "工具调用已被安全规则拦截")
		entry.exec.Result = result
	} else if result != nil && result.IsError {
		if cancelledWithUserNote {
			entry.exec.Status = ToolExecutionStatusCancelled
			entry.exec.Error = ""
		} else if isBackgroundWaitToolResult(result) {
			entry.exec.Status = ToolExecutionStatusCompleted
			entry.exec.Error = ""
		} else {
			entry.exec.Status = ToolExecutionStatusFailed
			entry.exec.Error = firstToolResultText(result, "工具执行返回错误结果")
		}
		entry.exec.Result = result
	} else {
		entry.exec.Status = ToolExecutionStatusCompleted
		if result == nil {
			result = &ToolResult{Content: []Content{{Type: "text", Text: "工具执行完成，但未返回结果"}}}
			entry.result = result
		}
		entry.exec.Result = result
	}
	if cancellationUnconfirmed {
		entry.exec.Status = ToolExecutionStatusOrphaned
		entry.exec.Error = "取消已请求，但远端 MCP 未确认执行已停止"
		runlease.FromContext(ctx).MarkUnconfirmed(id, entry.exec.Error)
	}
	finalExec := cloneToolExecution(entry.exec)
	s.mu.Unlock()

	if s.storage != nil {
		if saveErr := s.storage.SaveToolExecution(finalExec); saveErr != nil {
			s.logger.Warn("保存执行记录到数据库失败", zap.Error(saveErr), zap.String("executionId", id))
		}
	}
	if onDone != nil {
		onDone(finalExec)
	}
}

func nilSafeRun(ctx context.Context, entry *executionEntry) (*ToolResult, error) {
	if entry == nil {
		return nil, fmt.Errorf("execution entry is nil")
	}
	if entry.run == nil {
		return nil, fmt.Errorf("execution run func not wired")
	}
	return entry.run(ctx)
}

func entryResultRecover(ctx context.Context, toolName string, logger *zap.Logger, fn func() (*ToolResult, error)) (res *ToolResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			if logger != nil {
				logger.Error("tool execution worker panic recovered", zap.Any("recover", r), zap.String("toolName", toolName), zap.Stack("stack"))
			}
			err = fmt.Errorf("tool execution panic: %v", r)
		}
	}()
	return fn()
}

func (s *ExecutionService) Wait(ctx context.Context, executionID string, timeout time.Duration) (*ExecutionSnapshot, error) {
	entry := s.getEntry(executionID)
	if entry == nil {
		return s.getPersistedSnapshot(executionID)
	}
	select {
	case <-entry.done:
		return s.snapshotEntry(entry), nil
	default:
	}
	var timeoutCh <-chan time.Time
	var timer *time.Timer
	if timeout > 0 {
		timer = time.NewTimer(timeout)
		timeoutCh = timer.C
		defer timer.Stop()
	}

	select {
	case <-entry.done:
		return s.snapshotEntry(entry), nil
	case <-timeoutCh:
		return s.snapshotEntry(entry), ErrExecutionWaitTimeout
	case <-ctxDone(ctx):
		return s.snapshotEntry(entry), ctx.Err()
	}
}

// snapshotEntry synchronizes snapshots with worker state and partial output
// updates. Wait uses done to also observe persistence and completion callbacks.
func (s *ExecutionService) snapshotEntry(entry *executionEntry) *ExecutionSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return &ExecutionSnapshot{Execution: cloneToolExecution(entry.exec)}
}

func (s *ExecutionService) Get(executionID string) (*ExecutionSnapshot, error) {
	entry := s.getEntry(executionID)
	if entry != nil {
		return s.snapshotEntry(entry), nil
	}
	return s.getPersistedSnapshot(executionID)
}

func (s *ExecutionService) AppendPartialOutput(executionID, chunk string) bool {
	id := strings.TrimSpace(executionID)
	if s == nil || id == "" || chunk == "" {
		return false
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := s.entries[id]
	if entry == nil || entry.exec == nil {
		return false
	}
	appendPartialOutput(entry.exec, chunk, defaultPartialOutputMaxBytes, now)
	return true
}

func (s *ExecutionService) Cancel(executionID, note string) bool {
	id := strings.TrimSpace(executionID)
	if id == "" || s == nil {
		return false
	}
	s.mu.Lock()
	entry := s.entries[id]
	if entry == nil || isExecutionTerminal(entry.exec.Status) {
		s.mu.Unlock()
		return false
	}
	if strings.TrimSpace(note) != "" {
		s.abortUserNotes[id] = strings.TrimSpace(note)
	}
	cancel := entry.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return true
}

func (s *ExecutionService) ActiveRunningExecutionIDs() map[string]struct{} {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]struct{})
	for id, entry := range s.entries {
		if entry != nil && entry.exec != nil && !isExecutionTerminal(entry.exec.Status) {
			out[id] = struct{}{}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (s *ExecutionService) CancelAll(note string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(s.entries))
	for id, entry := range s.entries {
		if entry == nil || isExecutionTerminal(entry.exec.Status) {
			continue
		}
		if strings.TrimSpace(note) != "" {
			s.abortUserNotes[id] = strings.TrimSpace(note)
		}
		if entry.cancel != nil {
			cancels = append(cancels, entry.cancel)
		}
	}
	s.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

func (s *ExecutionService) getEntry(executionID string) *executionEntry {
	if s == nil {
		return nil
	}
	id := strings.TrimSpace(executionID)
	if id == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.entries[id]
}

func (s *ExecutionService) getPersistedSnapshot(executionID string) (*ExecutionSnapshot, error) {
	id := strings.TrimSpace(executionID)
	if id == "" {
		return nil, fmt.Errorf("execution_id is required")
	}
	if s != nil && s.storage != nil {
		exec, err := s.storage.GetToolExecution(id)
		if err == nil && exec != nil {
			return &ExecutionSnapshot{Execution: exec}, nil
		}
		if err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("execution not found: %s", id)
}

func (s *ExecutionService) applyAbortUserNoteToCancelledToolResult(executionID string, result **ToolResult, err *error) (cancelledWithUserNote bool) {
	note := strings.TrimSpace(s.takeAbortUserNote(executionID))
	if note == "" {
		return false
	}
	hasErr := err != nil && *err != nil
	hasRes := result != nil && *result != nil
	if hasRes && (*result).Blocked {
		return false
	}
	if !hasErr && !hasRes {
		return false
	}
	partial := ""
	if hasRes {
		partial = ToolResultPlainText(*result)
	}
	if partial == "" && hasErr {
		partial = (*err).Error()
	}
	merged := MergePartialToolOutputAndAbortNote(partial, note)
	if err != nil {
		*err = nil
	}
	if result != nil {
		*result = &ToolResult{Content: []Content{{Type: "text", Text: merged}}, IsError: true}
	}
	return true
}

func (s *ExecutionService) takeAbortUserNote(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	note := s.abortUserNotes[id]
	delete(s.abortUserNotes, id)
	return note
}

func (s *ExecutionService) cleanupOldEntriesLocked() {
	if s.maxInMemory <= 0 || len(s.entries) <= s.maxInMemory {
		return
	}
	type oldEntry struct {
		id        string
		startTime time.Time
	}
	var terminal []oldEntry
	for id, entry := range s.entries {
		if entry != nil && entry.exec != nil && isExecutionTerminal(entry.exec.Status) {
			terminal = append(terminal, oldEntry{id: id, startTime: entry.exec.StartTime})
		}
	}
	for len(s.entries) > s.maxInMemory && len(terminal) > 0 {
		oldest := 0
		for i := 1; i < len(terminal); i++ {
			if terminal[i].startTime.Before(terminal[oldest].startTime) {
				oldest = i
			}
		}
		delete(s.entries, terminal[oldest].id)
		terminal = append(terminal[:oldest], terminal[oldest+1:]...)
	}
}

func firstToolResultText(result *ToolResult, fallback string) string {
	if result != nil {
		for _, c := range result.Content {
			if strings.TrimSpace(c.Text) != "" {
				return c.Text
			}
		}
	}
	return fallback
}

func isBackgroundWaitToolResult(result *ToolResult) bool {
	text := strings.ToLower(strings.TrimSpace(ToolResultPlainText(result)))
	if text == "" {
		return false
	}
	hasExecutionID := strings.Contains(text, "execution_id:") || strings.Contains(text, `"execution_id"`)
	hasRunningStatus := strings.Contains(text, "status: running") || strings.Contains(text, "status: queued") ||
		strings.Contains(text, `"status": "running"`) || strings.Contains(text, `"status":"running"`) ||
		strings.Contains(text, `"status": "queued"`) || strings.Contains(text, `"status":"queued"`)
	hasSoftWaitSignal := strings.Contains(text, "工具已提交到后台执行") ||
		strings.Contains(text, "本次等待已到达") ||
		strings.Contains(text, "wait_timeout:") ||
		strings.Contains(text, "background execution") ||
		strings.Contains(text, "still running") ||
		strings.Contains(text, "仍未完成")
	return hasExecutionID && hasRunningStatus && hasSoftWaitSignal
}

func isExecutionTerminal(status string) bool {
	switch strings.TrimSpace(strings.ToLower(status)) {
	case ToolExecutionStatusCompleted, ToolExecutionStatusBlocked, ToolExecutionStatusFailed, ToolExecutionStatusCancelled, ToolExecutionStatusHardTimeout, ToolExecutionStatusOrphaned:
		return true
	default:
		return false
	}
}

func executionStatusCountsAsFailed(status string) bool {
	switch status {
	case ToolExecutionStatusFailed, ToolExecutionStatusHardTimeout, ToolExecutionStatusOrphaned:
		return true
	default:
		return false
	}
}

func ctxDone(ctx context.Context) <-chan struct{} {
	if ctx == nil {
		return nil
	}
	return ctx.Done()
}

func detachedExecutionContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return context.WithoutCancel(ctx)
}

func cloneArgsMap(in map[string]interface{}) map[string]interface{} {
	if in == nil {
		return map[string]interface{}{}
	}
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneToolExecution(in *ToolExecution) *ToolExecution {
	if in == nil {
		return nil
	}
	out := *in
	out.Arguments = cloneArgsMap(in.Arguments)
	if in.Result != nil {
		res := *in.Result
		if in.Result.Content != nil {
			res.Content = append([]Content(nil), in.Result.Content...)
		}
		out.Result = &res
	}
	if in.EndTime != nil {
		t := *in.EndTime
		out.EndTime = &t
	}
	if in.PartialOutputUpdatedAt != nil {
		t := *in.PartialOutputUpdatedAt
		out.PartialOutputUpdatedAt = &t
	}
	return &out
}

func appendPartialOutput(exec *ToolExecution, chunk string, maxBytes int, updatedAt time.Time) {
	if exec == nil || chunk == "" {
		return
	}
	if maxBytes <= 0 {
		maxBytes = defaultPartialOutputMaxBytes
	}
	exec.PartialOutputBytes += int64(len([]byte(chunk)))
	combined := exec.PartialOutput + chunk
	if len([]byte(combined)) > maxBytes {
		b := []byte(combined)
		combined = string(b[len(b)-maxBytes:])
		exec.PartialOutputTruncated = true
	}
	exec.PartialOutput = combined
	t := updatedAt
	exec.PartialOutputUpdatedAt = &t
}
