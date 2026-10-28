// Package agents 从 agents/ 目录加载 Markdown 代理定义（子代理 + 可选主代理 orchestrator.md / kind: orchestrator）。
package agents

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"cyberstrike-ai/internal/config"

	"gopkg.in/yaml.v3"
)

// OrchestratorMarkdownFilename 固定文件名：存在则视为 Deep 主代理定义，且不参与子代理列表。
const OrchestratorMarkdownFilename = "orchestrator.md"

// OrchestratorPlanExecuteMarkdownFilename plan_execute 模式主代理（规划侧）专用 Markdown 文件名。
const OrchestratorPlanExecuteMarkdownFilename = "orchestrator-plan-execute.md"

// OrchestratorSupervisorMarkdownFilename supervisor 模式主代理专用 Markdown 文件名。
const OrchestratorSupervisorMarkdownFilename = "orchestrator-supervisor.md"

// FrontMatter 对应 Markdown 文件头部字段（与文档示例一致）。
type FrontMatter struct {
	Name          string      `yaml:"name"`
	ID            string      `yaml:"id"`
	Description   string      `yaml:"description"`
	Tools         interface{} `yaml:"tools"` // 字符串 "A, B" 或 []string
	MaxIterations int         `yaml:"max_iterations"`
	BindRole      string      `yaml:"bind_role,omitempty"`
	Kind          string      `yaml:"kind,omitempty"` // orchestrator = 主代理（亦可仅用文件名 orchestrator.md）
}

// OrchestratorMarkdown 从 agents 目录解析出的主代理（Deep 协调者）定义。
type OrchestratorMarkdown struct {
	Filename    string
	EinoName    string // 写入 deep.Config.Name / 流式事件过滤
	DisplayName string
	Description string
	Instruction string
}

// MarkdownDirLoad 一次扫描 agents 目录的结果（子代理不含主代理文件）。
type MarkdownDirLoad struct {
	SubAgents               []config.MultiAgentSubConfig
	Orchestrator            *OrchestratorMarkdown // Deep 主代理
	OrchestratorPlanExecute *OrchestratorMarkdown // plan_execute 规划主代理
	OrchestratorSupervisor  *OrchestratorMarkdown // supervisor 监督主代理
	FileEntries             []FileAgent           // 含主代理与所有子代理，供管理 API 列表
}

// OrchestratorMarkdownKind 按固定文件名返回主代理类型：deep、plan_execute、supervisor；否则返回空。
func OrchestratorMarkdownKind(filename string) string {
	base := filepath.Base(strings.TrimSpace(filename))
	switch {
	case strings.EqualFold(base, OrchestratorPlanExecuteMarkdownFilename):
		return "plan_execute"
	case strings.EqualFold(base, OrchestratorSupervisorMarkdownFilename):
		return "supervisor"
	case strings.EqualFold(base, OrchestratorMarkdownFilename):
		return "deep"
	default:
		return ""
	}
}

// IsOrchestratorMarkdown 判断该文件是否占用 **Deep** 主代理槽位：orchestrator.md、或 kind: orchestrator（不含 plan_execute / supervisor 专用文件名）。
func IsOrchestratorMarkdown(filename string, fm FrontMatter) bool {
	base := filepath.Base(strings.TrimSpace(filename))
	switch OrchestratorMarkdownKind(base) {
	case "plan_execute", "supervisor":
		return false
	}
	if strings.EqualFold(base, OrchestratorMarkdownFilename) {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(fm.Kind), "orchestrator")
}

// IsOrchestratorLikeMarkdown 是否应在前端/API 中显示为「主代理类」文件。
func IsOrchestratorLikeMarkdown(filename string, kind string) bool {
	if OrchestratorMarkdownKind(filename) != "" {
		return true
	}
	return IsOrchestratorMarkdown(filename, FrontMatter{Kind: kind})
}

// WantsMarkdownOrchestrator 保存前判断是否会把该文件作为主代理（用于唯一性校验）。
func WantsMarkdownOrchestrator(filename string, kindField string, raw string) bool {
	base := filepath.Base(strings.TrimSpace(filename))
	if OrchestratorMarkdownKind(base) != "" {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(kindField), "orchestrator") {
		return true
	}
	if strings.EqualFold(base, OrchestratorMarkdownFilename) {
		return true
	}
	if strings.TrimSpace(raw) == "" {
		return false
	}
	sub, err := ParseMarkdownSubAgent(filename, raw)
	if err != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(sub.Kind), "orchestrator")
}

// SplitFrontMatter 分离 YAML front matter 与正文（--- ... ---）。
func SplitFrontMatter(content string) (frontYAML string, body string, err error) {
	s := strings.TrimSpace(content)
	if !strings.HasPrefix(s, "---") {
		return "", s, nil
	}
	rest := strings.TrimPrefix(s, "---")
	rest = strings.TrimLeft(rest, "\r\n")
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", "", fmt.Errorf("agents: missing a closing --- delimiter")
	}
	fm := strings.TrimSpace(rest[:end])
	body = strings.TrimSpace(rest[end+4:])
	body = strings.TrimLeft(body, "\r\n")
	return fm, body, nil
}

func parseToolsField(v interface{}) []string {
	if v == nil {
		return nil
	}
	switch t := v.(type) {
	case string:
		return splitToolList(t)
	case []interface{}:
		var out []string
		for _, x := range t {
			if s, ok := x.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return out
	case []string:
		var out []string
		for _, s := range t {
			if strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return out
	default:
		return nil
	}
}

func splitToolList(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ';' || r == '|'
	})
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// SlugID 从 name 生成可用的代理 id（小写、连字符）。
func SlugID(name string) string {
	var b strings.Builder
	name = strings.TrimSpace(strings.ToLower(name))
	lastDash := false
	for _, r := range name {
		switch {
		case unicode.IsLetter(r) && r < unicode.MaxASCII, unicode.IsDigit(r):
			b.WriteRune(r)
			lastDash = false
		case r == ' ' || r == '_' || r == '/' || r == '.':
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		return "agent"
	}
	return s
}

// sanitizeEinoAgentID 规范化 Deep 主代理在 Eino 中的 Name：小写 ASCII、数字、连字符，与默认 cyberstrike-deep 一致。
func sanitizeEinoAgentID(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) && r < unicode.MaxASCII, unicode.IsDigit(r):
			b.WriteRune(r)
		case r == '-':
			b.WriteRune(r)
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "cyberstrike-deep"
	}
	return out
}

func parseMarkdownAgentRaw(filename string, content string) (FrontMatter, string, error) {
	var fm FrontMatter
	fmStr, body, err := SplitFrontMatter(content)
	if err != nil {
		return fm, "", err
	}
	if strings.TrimSpace(fmStr) == "" {
		return fm, "", fmt.Errorf("agents: %s has no YAML front matter", filename)
	}
	if err := yaml.Unmarshal([]byte(fmStr), &fm); err != nil {
		return fm, "", fmt.Errorf("agents: parse front matter: %w", err)
	}
	return fm, body, nil
}

func orchestratorFromParsed(filename string, fm FrontMatter, body string) (*OrchestratorMarkdown, error) {
	display := strings.TrimSpace(fm.Name)
	if display == "" {
		display = "Orchestrator"
	}
	rawID := strings.TrimSpace(fm.ID)
	if rawID == "" {
		rawID = SlugID(display)
	}
	eino := sanitizeEinoAgentID(rawID)
	return &OrchestratorMarkdown{
		Filename:    filepath.Base(strings.TrimSpace(filename)),
		EinoName:    eino,
		DisplayName: display,
		Description: strings.TrimSpace(fm.Description),
		Instruction: strings.TrimSpace(body),
	}, nil
}

func orchestratorConfigFromOrchestrator(o *OrchestratorMarkdown) config.MultiAgentSubConfig {
	if o == nil {
		return config.MultiAgentSubConfig{}
	}
	return config.MultiAgentSubConfig{
		ID:          o.EinoName,
		Name:        o.DisplayName,
		Description: o.Description,
		Instruction: o.Instruction,
		Kind:        "orchestrator",
	}
}

func subAgentFromFrontMatter(filename string, fm FrontMatter, body string) (config.MultiAgentSubConfig, error) {
	var out config.MultiAgentSubConfig
	name := strings.TrimSpace(fm.Name)
	if name == "" {
		return out, fmt.Errorf("agents: %s is missing the name field", filename)
	}
	id := strings.TrimSpace(fm.ID)
	if id == "" {
		id = SlugID(name)
	}
	out.ID = id
	out.Name = name
	out.Description = strings.TrimSpace(fm.Description)
	out.Instruction = strings.TrimSpace(body)
	out.RoleTools = parseToolsField(fm.Tools)
	out.MaxIterations = fm.MaxIterations
	out.BindRole = strings.TrimSpace(fm.BindRole)
	out.Kind = strings.TrimSpace(fm.Kind)
	return out, nil
}

func collectMarkdownBasenames(dir string) ([]string, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, nil
	}
	st, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("agents: not a directory: %s", dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if strings.HasPrefix(n, ".") {
			continue
		}
		if !strings.EqualFold(filepath.Ext(n), ".md") {
			continue
		}
		if strings.EqualFold(n, "README.md") {
			continue
		}
		names = append(names, n)
	}
	sort.Strings(names)
	return names, nil
}

// LoadMarkdownAgentsDir 扫描 agents 目录：拆出 Deep / plan_execute / supervisor 主代理各至多一个，及其余子代理。
func LoadMarkdownAgentsDir(dir string) (*MarkdownDirLoad, error) {
	out := &MarkdownDirLoad{}
	names, err := collectMarkdownBasenames(dir)
	if err != nil {
		return nil, err
	}
	for _, n := range names {
		p := filepath.Join(dir, n)
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		fm, body, err := parseMarkdownAgentRaw(n, string(b))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", n, err)
		}
		switch OrchestratorMarkdownKind(n) {
		case "plan_execute":
			if out.OrchestratorPlanExecute != nil {
				return nil, fmt.Errorf("agents: only one %s can be defined; already present: %s", OrchestratorPlanExecuteMarkdownFilename, out.OrchestratorPlanExecute.Filename)
			}
			orch, err := orchestratorFromParsed(n, fm, body)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", n, err)
			}
			out.OrchestratorPlanExecute = orch
			out.FileEntries = append(out.FileEntries, FileAgent{
				Filename:       n,
				Config:         orchestratorConfigFromOrchestrator(orch),
				IsOrchestrator: true,
			})
			continue
		case "supervisor":
			if out.OrchestratorSupervisor != nil {
				return nil, fmt.Errorf("agents: only one %s can be defined; already present: %s", OrchestratorSupervisorMarkdownFilename, out.OrchestratorSupervisor.Filename)
			}
			orch, err := orchestratorFromParsed(n, fm, body)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", n, err)
			}
			out.OrchestratorSupervisor = orch
			out.FileEntries = append(out.FileEntries, FileAgent{
				Filename:       n,
				Config:         orchestratorConfigFromOrchestrator(orch),
				IsOrchestrator: true,
			})
			continue
		}
		if IsOrchestratorMarkdown(n, fm) {
			if out.Orchestrator != nil {
				return nil, fmt.Errorf("agents: only one orchestrator (Deep coordinator) can be defined; already present: %s, also conflicts with %s", out.Orchestrator.Filename, n)
			}
			orch, err := orchestratorFromParsed(n, fm, body)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", n, err)
			}
			out.Orchestrator = orch
			out.FileEntries = append(out.FileEntries, FileAgent{
				Filename:       n,
				Config:         orchestratorConfigFromOrchestrator(orch),
				IsOrchestrator: true,
			})
			continue
		}
		sub, err := subAgentFromFrontMatter(n, fm, body)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", n, err)
		}
		out.SubAgents = append(out.SubAgents, sub)
		out.FileEntries = append(out.FileEntries, FileAgent{Filename: n, Config: sub, IsOrchestrator: false})
	}
	return out, nil
}

// ParseMarkdownSubAgent 将单个 Markdown 文件解析为 MultiAgentSubConfig。
func ParseMarkdownSubAgent(filename string, content string) (config.MultiAgentSubConfig, error) {
	fm, body, err := parseMarkdownAgentRaw(filename, content)
	if err != nil {
		return config.MultiAgentSubConfig{}, err
	}
	if OrchestratorMarkdownKind(filename) != "" {
		orch, err := orchestratorFromParsed(filename, fm, body)
		if err != nil {
			return config.MultiAgentSubConfig{}, err
		}
		return orchestratorConfigFromOrchestrator(orch), nil
	}
	if IsOrchestratorMarkdown(filename, fm) {
		orch, err := orchestratorFromParsed(filename, fm, body)
		if err != nil {
			return config.MultiAgentSubConfig{}, err
		}
		return orchestratorConfigFromOrchestrator(orch), nil
	}
	return subAgentFromFrontMatter(filename, fm, body)
}

// LoadMarkdownSubAgents 读取目录下所有子代理 .md（不含主代理 orchestrator.md / kind: orchestrator）。
func LoadMarkdownSubAgents(dir string) ([]config.MultiAgentSubConfig, error) {
	load, err := LoadMarkdownAgentsDir(dir)
	if err != nil {
		return nil, err
	}
	return load.SubAgents, nil
}

// FileAgent 单个 Markdown 文件及其解析结果。
type FileAgent struct {
	Filename       string
	Config         config.MultiAgentSubConfig
	IsOrchestrator bool
}

// LoadMarkdownAgentFiles 列出目录下全部 .md（含主代理），供管理 API 使用。
func LoadMarkdownAgentFiles(dir string) ([]FileAgent, error) {
	load, err := LoadMarkdownAgentsDir(dir)
	if err != nil {
		return nil, err
	}
	return load.FileEntries, nil
}

// MergeYAMLAndMarkdown 合并 config.yaml 中的 sub_agents 与 Markdown 定义：同 id 时 Markdown 覆盖 YAML；仅存在于 Markdown 的条目追加在 YAML 顺序之后。
func MergeYAMLAndMarkdown(yamlSubs []config.MultiAgentSubConfig, mdSubs []config.MultiAgentSubConfig) []config.MultiAgentSubConfig {
	mdByID := make(map[string]config.MultiAgentSubConfig)
	for _, m := range mdSubs {
		id := strings.TrimSpace(m.ID)
		if id == "" {
			continue
		}
		mdByID[id] = m
	}
	yamlIDSet := make(map[string]bool)
	for _, y := range yamlSubs {
		yamlIDSet[strings.TrimSpace(y.ID)] = true
	}
	out := make([]config.MultiAgentSubConfig, 0, len(yamlSubs)+len(mdSubs))
	for _, y := range yamlSubs {
		id := strings.TrimSpace(y.ID)
		if id == "" {
			continue
		}
		if m, ok := mdByID[id]; ok {
			out = append(out, m)
		} else {
			out = append(out, y)
		}
	}
	for _, m := range mdSubs {
		id := strings.TrimSpace(m.ID)
		if id == "" || yamlIDSet[id] {
			continue
		}
		out = append(out, m)
	}
	return out
}

// EffectiveSubAgents 供多代理运行时使用。
func EffectiveSubAgents(yamlSubs []config.MultiAgentSubConfig, agentsDir string) ([]config.MultiAgentSubConfig, error) {
	md, err := LoadMarkdownSubAgents(agentsDir)
	if err != nil {
		return nil, err
	}
	if len(md) == 0 {
		return yamlSubs, nil
	}
	return MergeYAMLAndMarkdown(yamlSubs, md), nil
}

// BuildMarkdownFile 根据配置序列化为可写回磁盘的 Markdown。
func BuildMarkdownFile(sub config.MultiAgentSubConfig) ([]byte, error) {
	fm := FrontMatter{
		Name:          sub.Name,
		ID:            sub.ID,
		Description:   sub.Description,
		MaxIterations: sub.MaxIterations,
		BindRole:      sub.BindRole,
	}
	if k := strings.TrimSpace(sub.Kind); k != "" {
		fm.Kind = k
	}
	if len(sub.RoleTools) > 0 {
		fm.Tools = sub.RoleTools
	}
	head, err := yaml.Marshal(fm)
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	b.WriteString("---\n")
	b.Write(head)
	b.WriteString("---\n\n")
	b.WriteString(strings.TrimSpace(sub.Instruction))
	if !strings.HasSuffix(sub.Instruction, "\n") && sub.Instruction != "" {
		b.WriteString("\n")
	}
	return []byte(b.String()), nil
}
