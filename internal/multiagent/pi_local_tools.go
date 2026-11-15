package multiagent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cyberstrike-ai/internal/agentruntime"
	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/security"
	"cyberstrike-ai/internal/skillpackage"
	"cyberstrike-ai/internal/tooloutput"
)

// piLocalCatalog is explicit host registration: Pi never receives default host filesystem tools.
func (r *piRun) piLocalCatalog(ctx context.Context) ([]agentruntime.Tool, map[string]bool, string, error) {
	var tools []agentruntime.Tool
	names := map[string]bool{}
	var catalog strings.Builder
	if r.cfg.MultiAgent.Skills.Disable {
		return tools, names, "", nil
	}
	str := map[string]any{"type": "string"}
	if r.cfg.MultiAgent.Runtime.PlantaskEnable && r.cfg.SkillsDir != "" {
		for _, t := range piPlanTools() {
			tools = append(tools, t)
			names[t.Name] = true
		}
	}
	if root := strings.TrimSpace(r.cfg.SkillsDir); root != "" {
		skills, err := skillpackage.ListSkillSummaries(root)
		if err != nil && !os.IsNotExist(err) {
			return nil, nil, "", err
		}
		for _, s := range skills {
			fmt.Fprintf(&catalog, "- %s: %s\n", s.ID, s.Description)
		}
		if len(skills) > 0 {
			name := r.cfg.MultiAgent.Skills.SkillToolName
			if name == "" {
				name = "skill"
			}
			tools = append(tools, agentruntime.Tool{Name: name, Description: "Load the instructions and resource paths of an available skill", Parameters: piToolSchema(map[string]any{"name": str}, "name")})
			names[name] = true
		}
	}
	if r.cfg.MultiAgent.Skills.FilesystemToolsEffective() && !localToolPermissionDenied(ctx, "execute") {
		for _, t := range []agentruntime.Tool{
			{Name: "read_file", Description: "Read a text file, optionally selecting a 1-based line offset and line limit", Parameters: piToolSchema(map[string]any{"path": str, "offset": map[string]any{"type": "integer", "minimum": 1}, "limit": map[string]any{"type": "integer", "minimum": 1}}, "path")},
			{Name: "write_file", Description: "Write UTF-8 text to a file", Parameters: piToolSchema(map[string]any{"path": str, "content": str}, "path", "content")},
			{Name: "edit_file", Description: "Replace one exact occurrence in a text file; ambiguous matches fail", Parameters: piToolSchema(map[string]any{"path": str, "old_text": str, "new_text": str}, "path", "old_text", "new_text")},
			{Name: "ls", Description: "List directory entries", Parameters: piToolSchema(map[string]any{"path": str}, "path")},
			{Name: "glob", Description: "Find files recursively by a filename glob under a directory", Parameters: piToolSchema(map[string]any{"path": str, "pattern": str}, "path", "pattern")},
			{Name: "grep", Description: "Search UTF-8 files recursively with a regular expression", Parameters: piToolSchema(map[string]any{"path": str, "pattern": str}, "path", "pattern")},
			{Name: "execute", Description: "Execute a shell command in the host task context with bounded output and process-tree cancellation", Parameters: piToolSchema(map[string]any{"command": str, "cwd": str}, "command")},
		} {
			tools = append(tools, t)
			names[t.Name] = true
		}
	}
	if catalog.Len() > 0 {
		return tools, names, "\n\nAvailable skills (load a matching skill before using its resources):\n" + catalog.String(), nil
	}
	return tools, names, "", nil
}

type piBoundedOutput struct {
	mu        sync.Mutex
	body      strings.Builder
	truncated bool
	onChunk   func(string)
	spool     io.Writer
	spoolPath string
	lastWrite atomic.Int64
}

func (b *piBoundedOutput) Write(p []byte) (int, error) {
	b.lastWrite.Store(time.Now().UnixNano())
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.spool != nil {
		if _, err := b.spool.Write(p); err != nil {
			return 0, err
		}
	}
	left := 256*1024 - b.body.Len()
	if left > len(p) {
		left = len(p)
	}
	if left > 0 {
		b.body.Write(p[:left])
		if b.onChunk != nil {
			b.onChunk(string(p[:left]))
		}
	}
	if left < len(p) {
		b.truncated = true
	}
	return len(p), nil
}
func (b *piBoundedOutput) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	text := b.body.String()
	if b.truncated {
		text += "\n[Preview truncated at 256 KiB; full output: " + b.spoolPath + "]"
	}
	return text
}

func (r *piRun) executePiLocal(ctx context.Context, call agentruntime.ToolCall) (agentruntime.ToolResult, error) {
	if call.Name == "TaskCreate" || call.Name == "TaskUpdate" || call.Name == "TaskGet" || call.Name == "TaskList" {
		return r.executePiPlan(ctx, call)
	}
	arg := func(key string) string { v, _ := call.Arguments[key].(string); return v }
	skillName := r.cfg.MultiAgent.Skills.SkillToolName
	if skillName == "" {
		skillName = "skill"
	}
	if call.Name == skillName {
		name := arg("name")
		skills, err := skillpackage.ListSkillSummaries(r.cfg.SkillsDir)
		if err != nil {
			return agentruntime.ToolResult{}, err
		}
		found := false
		for _, s := range skills {
			if s.ID == name {
				found = true
				break
			}
		}
		if !found {
			return agentruntime.ToolResult{}, fmt.Errorf("unknown skill: %s", name)
		}
		view, err := skillpackage.LoadSkill(r.cfg.SkillsDir, name, skillpackage.LoadOptions{Depth: "full"})
		if err != nil {
			return agentruntime.ToolResult{}, err
		}
		body, _ := json.Marshal(view)
		return agentruntime.ToolResult{Content: string(body)}, nil
	}
	path := arg("path")
	if path != "" {
		var err error
		path, err = filepath.Abs(path)
		if err != nil {
			return agentruntime.ToolResult{}, err
		}
	}
	result := agentruntime.ToolResult{}
	switch call.Name {
	case "execute":
		if arg("command") == "" {
			return result, fmt.Errorf("command is required")
		}
		timeout := time.Duration(agentToolTimeoutMinutes(r.cfg)) * time.Minute
		execCtx, cancel := context.WithCancel(ctx)
		if timeout > 0 {
			cancel()
			execCtx, cancel = context.WithTimeout(ctx, timeout)
		}
		defer cancel()
		registry := mcp.AgentExecuteRunRegistryFromContext(ctx)
		if registry != nil {
			registry.RegisterActiveAgentExecute(r.conversationID, cancel)
			defer registry.UnregisterActiveAgentExecute(r.conversationID)
		}
		cmd := exec.CommandContext(execCtx, "/bin/sh", "-c", arg("command"))
		cmd.Dir = arg("cwd")
		security.ConfigureShellCmdForAgentExecute(cmd)
		output := &piBoundedOutput{}
		output.lastWrite.Store(time.Now().UnixNano())
		idleSeconds := r.cfg.Agent.ShellNoOutputTimeoutSeconds
		if idleSeconds == 0 {
			idleSeconds = 300
		}
		idleDone := make(chan struct{})
		defer close(idleDone)
		var idleExpired atomic.Bool
		if idleSeconds > 0 {
			go func() {
				ticker := time.NewTicker(250 * time.Millisecond)
				defer ticker.Stop()
				for {
					select {
					case <-idleDone:
						return
					case <-execCtx.Done():
						return
					case <-ticker.C:
						if time.Since(time.Unix(0, output.lastWrite.Load())) >= time.Duration(idleSeconds)*time.Second {
							idleExpired.Store(true)
							cancel()
							return
						}
					}
				}
			}()
		}
		spoolPath, err := tooloutput.WriteTruncFile(tooloutput.SpillOpts{RootDir: r.cfg.MultiAgent.Runtime.ReductionRootDir, ConversationID: r.conversationID}, "")
		if err != nil {
			return result, err
		}
		spool, err := os.OpenFile(spoolPath, os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return result, err
		}
		defer spool.Close()
		output.spool = spool
		output.spoolPath = spoolPath
		cmd.Stdout = output
		cmd.Stderr = output
		id := r.ag.BeginLocalToolExecution(ctx, call.Name, call.Arguments)
		r.ag.RegisterLocalToolExecutionCancel(id, cancel)
		defer r.ag.UnregisterLocalToolExecutionCancel(id)
		output.onChunk = func(chunk string) { r.ag.AppendLocalToolExecutionPartialOutput(id, chunk) }
		session, err := security.StartShellSessionContext(execCtx, cmd)
		if err == nil {
			err = session.Wait()
		}
		result.Content = output.String()
		result.IsError = err != nil
		result.ExecutionID = r.ag.FinishLocalToolExecution(ctx, id, call.Name, call.Arguments, result.Content, err)
		if err != nil {
			result.Content += "\n" + err.Error()
		}
		if idleExpired.Load() && err != nil {
			result.Content += fmt.Sprintf("\nCommand stopped after %d seconds without stdout/stderr.", idleSeconds)
		}
		if registry != nil {
			if note := registry.TakeAgentExecuteAbortNote(r.conversationID); note != "" {
				result.Content += "\nTool stopped by the user. Follow-up instruction: " + note
			}
		}
		return result, nil
	case "read_file":
		f, err := os.Open(path)
		if err != nil {
			return result, err
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil {
			return result, err
		}
		if !st.Mode().IsRegular() {
			return result, fmt.Errorf("not a regular file")
		}
		b, err := io.ReadAll(io.LimitReader(f, 2*1024*1024+1))
		if err != nil {
			return result, err
		}
		if len(b) > 2*1024*1024 {
			return result, fmt.Errorf("file exceeds 2 MiB; use execute to select a smaller region")
		}
		lines := strings.Split(string(b), "\n")
		offset, limit := 1, 1000
		if n, ok := call.Arguments["offset"].(float64); ok && n >= 1 {
			offset = int(n)
		}
		if n, ok := call.Arguments["limit"].(float64); ok && n > 0 && n < 10000 {
			limit = int(n)
		}
		if offset > len(lines) {
			return result, nil
		}
		end := offset - 1 + limit
		if end > len(lines) {
			end = len(lines)
		}
		result.Content = strings.Join(lines[offset-1:end], "\n")
	case "write_file", "edit_file":
		content := arg("content")
		mode := os.FileMode(0o600)
		if st, err := os.Stat(path); err == nil {
			if !st.Mode().IsRegular() {
				return result, fmt.Errorf("not a regular file")
			}
			mode = st.Mode().Perm()
		}
		if call.Name == "edit_file" {
			b, err := os.ReadFile(path)
			if err != nil {
				return result, err
			}
			old := arg("old_text")
			if old == "" || strings.Count(string(b), old) != 1 {
				return result, fmt.Errorf("old_text must match exactly once")
			}
			content = strings.Replace(string(b), old, arg("new_text"), 1)
		}
		if err := os.WriteFile(path, []byte(content), mode); err != nil {
			return result, err
		}
		result.Content = "Updated " + path
	case "ls":
		entries, err := os.ReadDir(path)
		if err != nil {
			return result, err
		}
		var names []string
		for _, e := range entries {
			n := e.Name()
			if e.IsDir() {
				n += "/"
			}
			names = append(names, n)
		}
		result.Content = strings.Join(names, "\n")
	case "glob", "grep":
		pattern := arg("pattern")
		var re *regexp.Regexp
		if call.Name == "grep" {
			var err error
			re, err = regexp.Compile(pattern)
			if err != nil {
				return result, err
			}
		}
		var hits []string
		err := filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err != nil {
				return err
			}
			if len(hits) >= 1000 {
				return fs.SkipAll
			}
			if d.IsDir() {
				if d.Name() == ".git" || d.Name() == "node_modules" {
					return fs.SkipDir
				}
				return nil
			}
			if d.Type()&os.ModeSymlink != 0 {
				return nil
			}
			if call.Name == "glob" {
				matched, e := filepath.Match(pattern, d.Name())
				if e != nil {
					return e
				}
				if matched {
					hits = append(hits, p)
				}
				return nil
			}
			st, e := d.Info()
			if e != nil || !st.Mode().IsRegular() || st.Size() > 2*1024*1024 {
				return nil
			}
			b, e := os.ReadFile(p)
			if e != nil {
				return e
			}
			for i, line := range strings.Split(string(b), "\n") {
				if re.MatchString(line) {
					hits = append(hits, fmt.Sprintf("%s:%d:%s", p, i+1, line))
					if len(hits) >= 1000 {
						break
					}
				}
			}
			return nil
		})
		if err != nil {
			return result, err
		}
		result.Content = strings.Join(hits, "\n")
	default:
		return result, fmt.Errorf("unknown local tool")
	}
	result.ExecutionID = r.ag.FinishLocalToolExecution(ctx, "", call.Name, call.Arguments, result.Content, nil)
	return result, nil
}
