package multiagent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/einomcp"
	"cyberstrike-ai/internal/security"
	"cyberstrike-ai/internal/tooloutput"

	localbk "github.com/cloudwego/eino-ext/adk/backend/local"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/filesystem"
	"github.com/cloudwego/eino/adk/middlewares/skill"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

func prepareEinoAgenticSkills(
	ctx context.Context,
	skillsDir string,
	ma *config.MultiAgentConfig,
	logger *zap.Logger,
) (loc *localbk.Local, skillMW adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage], fsTools bool, skillsRoot string, err error) {
	resetSessionReadFiles()
	if ma == nil {
		return nil, nil, false, "", nil
	}
	needLocalBackend := ma.EinoMiddleware.ReductionEnable
	newLocalBackend := func() (*localbk.Local, error) {
		backend, backendErr := localbk.NewBackend(ctx, &localbk.Config{})
		if backendErr != nil {
			return nil, fmt.Errorf("eino local backend: %w", backendErr)
		}
		return backend, nil
	}
	if ma.EinoSkills.Disable {
		if !needLocalBackend {
			return nil, nil, false, "", nil
		}
		loc, err = newLocalBackend()
		return loc, nil, false, "", err
	}
	root := strings.TrimSpace(skillsDir)
	if root == "" {
		if logger != nil {
			logger.Warn("eino agentic skills: skills_dir empty, skip")
		}
		if !needLocalBackend {
			return nil, nil, false, "", nil
		}
		loc, err = newLocalBackend()
		return loc, nil, false, "", err
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, nil, false, "", fmt.Errorf("skills_dir abs: %w", err)
	}
	if st, err := os.Stat(abs); err != nil || !st.IsDir() {
		if logger != nil {
			logger.Warn("eino agentic skills: directory missing, skip", zap.String("dir", abs), zap.Error(err))
		}
		if !needLocalBackend {
			return nil, nil, false, "", nil
		}
		loc, err = newLocalBackend()
		return loc, nil, false, "", err
	}

	loc, err = newLocalBackend()
	if err != nil {
		return nil, nil, false, "", err
	}

	skillBE, err := skill.NewBackendFromFilesystem(ctx, &skill.BackendFromFilesystemConfig{
		Backend: loc,
		BaseDir: abs,
	})
	if err != nil {
		return nil, nil, false, "", fmt.Errorf("eino agentic skill filesystem backend: %w", err)
	}

	sc := &skill.TypedConfig[*schema.AgenticMessage]{Backend: skillBE}
	if name := strings.TrimSpace(ma.EinoSkills.SkillToolName); name != "" {
		sc.SkillToolName = &name
	}
	skillMW, err = skill.NewTyped[*schema.AgenticMessage](ctx, sc)
	if err != nil {
		return nil, nil, false, "", fmt.Errorf("eino agentic skill middleware: %w", err)
	}

	fsTools = ma.EinoSkills.EinoSkillFilesystemToolsEffective()
	return loc, skillMW, fsTools, abs, nil
}

func subAgentAgenticFilesystemMiddleware(
	ctx context.Context,
	loc *localbk.Local,
	invokeNotify *einomcp.ToolInvokeNotifyHolder,
	einoAgentName string,
	conversationID string,
	projectID string,
	reductionRootDir string,
	toolMaxBytes int,
	binder *MCPExecutionBinder,
	beginMonitor func(toolCallID, command string) string,
	appendPartialMonitor func(executionID, toolCallID, chunk string),
	registerCancelMonitor func(executionID string, cancel context.CancelFunc),
	unregisterCancelMonitor func(executionID string),
	finishMonitor func(executionID, toolCallID, command, stdout string, success bool, invokeErr error),
	toolTimeoutMinutes int,
	toolWaitTimeoutSeconds int,
	shellNoOutputTimeoutSec int,
	outputChunk func(toolName, toolCallID, chunk string),
) (adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage], error) {
	if loc == nil {
		return nil, nil
	}
	mw, err := filesystem.NewTyped[*schema.AgenticMessage](ctx, &filesystem.MiddlewareConfig{
		Backend:                          loc,
		StreamingShell: &einoStreamingShellWrap{
			inner:                   security.NewEinoStreamingShell(),
			invokeNotify:            invokeNotify,
			einoAgentName:           strings.TrimSpace(einoAgentName),
			outputChunk:             outputChunk,
			beginMonitor:            beginMonitor,
			appendPartialMonitor:    appendPartialMonitor,
			registerCancelMonitor:   registerCancelMonitor,
			unregisterCancelMonitor: unregisterCancelMonitor,
			finishMonitor:           finishMonitor,
			toolTimeoutMinutes:      toolTimeoutMinutes,
			toolWaitTimeoutSeconds:  toolWaitTimeoutSeconds,
			shellNoOutputTimeoutSec: shellNoOutputTimeoutSec,
		},
	})
	if err != nil {
		return nil, err
	}
	return &einoAgenticFilesystemToolMiddleware{
		TypedChatModelAgentMiddleware: mw,
		conversationID:                conversationID,
		projectID:                     projectID,
		reductionRootDir:              reductionRootDir,
		toolMaxBytes:                  toolMaxBytes,
		binder:                        binder,
	}, nil
}

type einoAgenticFilesystemToolMiddleware struct {
	adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage]
	conversationID   string
	projectID        string
	reductionRootDir string
	toolMaxBytes     int
	binder           *MCPExecutionBinder
}

func (m *einoAgenticFilesystemToolMiddleware) WrapInvokableToolCall(ctx context.Context, endpoint adk.InvokableToolCallEndpoint, tCtx *adk.ToolContext) (adk.InvokableToolCallEndpoint, error) {
	wrapped, err := m.TypedChatModelAgentMiddleware.WrapInvokableToolCall(ctx, endpoint, tCtx)
	if err != nil {
		return nil, err
	}
	if tCtx == nil || !isBuiltinEinoADKFilesystemToolName(tCtx.Name) {
		return wrapped, nil
	}
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
		if strings.EqualFold(strings.TrimSpace(tCtx.Name), "read_file") {
			argumentsInJSON = normalizeReadFileArguments(argumentsInJSON)
		}
		args := parseToolArgumentsObject(argumentsInJSON)
		if len(args) > 0 && m.binder != nil {
			m.binder.BindArguments(tCtx.CallID, args)
		}
		result, runErr := wrapped(ctx, argumentsInJSON, opts...)
		if runErr != nil {
			return result, runErr
		}
		result = enrichReadFileToolResult(tCtx.Name, argumentsInJSON, result)
		return m.boundToolResult(tCtx.CallID, result), nil
	}, nil
}

func (m *einoAgenticFilesystemToolMiddleware) boundToolResult(toolCallID, result string) string {
	if m == nil || m.toolMaxBytes <= 0 || len(result) <= m.toolMaxBytes {
		return result
	}
	return tooloutput.BoundWithSpill(result, m.toolMaxBytes, tooloutput.SpillOpts{
		RootDir:        m.reductionRootDir,
		ProjectID:      m.projectID,
		ConversationID: m.conversationID,
		ExecutionID:    toolCallID,
	})
}

func parseToolArgumentsObject(raw string) map[string]interface{} {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "{}" || raw == "null" {
		return nil
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &args); err != nil || len(args) == 0 {
		return nil
	}
	return args
}

// agentToolTimeoutMinutes 返回 agent.tool_timeout_minutes（与 executeToolViaMCP 一致）；cfg 为 nil 时 0。
func agentToolTimeoutMinutes(cfg *config.Config) int {
	if cfg == nil {
		return 0
	}
	return cfg.Agent.ToolTimeoutMinutes
}

func agentToolWaitTimeoutSeconds(cfg *config.Config) int {
	if cfg == nil {
		return 0
	}
	return cfg.Agent.ToolWaitTimeoutSeconds
}

// agentShellNoOutputTimeoutSeconds：0=默认 300s（5 分钟）；-1=关闭；>0=自定义秒数。
func agentShellNoOutputTimeoutSeconds(cfg *config.Config) int {
	if cfg == nil {
		return 300
	}
	v := cfg.Agent.ShellNoOutputTimeoutSeconds
	if v < 0 {
		return 0
	}
	if v == 0 {
		return 300
	}
	return v
}


var (
	sessionReadFilesMu sync.Mutex
	sessionReadFiles   = make(map[string]int)
)

func resetSessionReadFiles() {
	sessionReadFilesMu.Lock()
	sessionReadFiles = make(map[string]int)
	sessionReadFilesMu.Unlock()
}

// enrichReadFileToolResult adds total line count metadata, an explicit [EOF REACHED]
// loop-breaker message, and session-level full-file deduplication so the model never
// loops past EOF or re-reads the entire repository 3-4 times in the same audit session.
func enrichReadFileToolResult(toolName, argumentsInJSON, result string) string {
	if !strings.EqualFold(strings.TrimSpace(toolName), "read_file") {
		return result
	}
	if strings.HasPrefix(result, "[File: ") || strings.HasPrefix(result, "[EOF REACHED]") || strings.HasPrefix(result, "[ALREADY READ IN THIS SESSION]") {
		return result
	}
	args := parseToolArgumentsObject(argumentsInJSON)
	if len(args) == 0 {
		return result
	}
	fp, _ := args["file_path"].(string)
	fp = strings.TrimSpace(fp)
	if fp == "" {
		return result
	}
	offset := 1
	if v, ok := args["offset"].(float64); ok && int(v) > 0 {
		offset = int(v)
	}
	limit := 200
	if v, ok := args["limit"].(float64); ok && int(v) > 0 {
		limit = int(v)
	}
	data, err := os.ReadFile(fp)
	if err != nil {
		return result
	}
	contentStr := string(data)
	totalLines := 0
	if len(contentStr) > 0 {
		totalLines = strings.Count(contentStr, "\n") + 1
		if strings.HasSuffix(contentStr, "\n") && totalLines > 1 {
			totalLines--
		}
	}
	if strings.Contains(result, "past its last line") || strings.Contains(result, "No content read from") {
		return fmt.Sprintf(
			"[EOF REACHED] File %s has ONLY %d lines in total. Your requested offset (%d) is past the last line (%d). You have ALREADY read to the end of this file. DO NOT call read_file on %s with a larger offset!",
			fp, totalLines, offset, totalLines, fp,
		)
	}
	endLine := offset + limit - 1
	sessionReadFilesMu.Lock()
	prevMaxLine := sessionReadFiles[fp]
	if prevMaxLine >= totalLines && totalLines > 0 {
		sessionReadFilesMu.Unlock()
		return fmt.Sprintf(
			"[ALREADY READ IN THIS SESSION] File %s (%d lines) was ALREADY read in full (lines 1..%d) earlier in this audit session, and its complete code/findings are already in your context above. DO NOT call read_file again on already-read files! Please analyze the code already in your context, mark your analysis tasks completed, and output the final comprehensive white-hat audit report immediately.",
			fp, totalLines, totalLines,
		)
	}
	if endLine >= totalLines && offset <= 1 {
		sessionReadFiles[fp] = totalLines
	} else if offset <= prevMaxLine+1 && endLine > prevMaxLine {
		sessionReadFiles[fp] = endLine
	}
	sessionReadFilesMu.Unlock()

	if endLine >= totalLines {
		return fmt.Sprintf(
			"[File: %s | Total lines: %d | Showing lines %d..%d (END OF FILE)]\n%s\n[EOF REACHED: End of file (%d total lines). Do not call read_file on this file again.]",
			fp, totalLines, offset, totalLines, result, totalLines,
		)
	}
	return fmt.Sprintf(
		"[File: %s | Total lines: %d | Showing lines %d..%d (%d lines remaining)]\n%s",
		fp, totalLines, offset, endLine, totalLines-endLine, result,
	)
}

// normalizeReadFileArguments coalesces tiny pagination limits on source files:
// if a file has <= 600 lines remaining, read to EOF in one call; if > 600 lines remaining
// and limit < 500, bump limit to 500 to avoid 15-turn micro-pagination loops.
func normalizeReadFileArguments(argumentsInJSON string) string {
	args := parseToolArgumentsObject(argumentsInJSON)
	if len(args) == 0 {
		return argumentsInJSON
	}
	fp, _ := args["file_path"].(string)
	fp = strings.TrimSpace(fp)
	if fp == "" {
		return argumentsInJSON
	}
	data, err := os.ReadFile(fp)
	if err != nil {
		return argumentsInJSON
	}
	contentStr := string(data)
	totalLines := 0
	if len(contentStr) > 0 {
		totalLines = strings.Count(contentStr, "\n") + 1
		if strings.HasSuffix(contentStr, "\n") && totalLines > 1 {
			totalLines--
		}
	}
	if totalLines <= 0 {
		return argumentsInJSON
	}
	offset := 1
	if v, ok := args["offset"].(float64); ok && int(v) > 0 {
		offset = int(v)
	}
	limit := 200
	if v, ok := args["limit"].(float64); ok && int(v) > 0 {
		limit = int(v)
	}
	if offset > totalLines {
		return argumentsInJSON
	}
	remaining := totalLines - offset + 1
	changed := false
	if v, ok := args["offset"].(float64); !ok || int(v) <= 0 {
		args["offset"] = 1
		changed = true
	}
	if remaining <= 600 && limit < remaining {
		args["limit"] = remaining
		changed = true
	} else if remaining > 600 && limit < 500 {
		args["limit"] = 500
		changed = true
	}
	if !changed {
		return argumentsInJSON
	}
	b, err := json.Marshal(args)
	if err != nil {
		return argumentsInJSON
	}
	return string(b)
}
