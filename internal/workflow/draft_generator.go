package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/openai"

	"go.uber.org/zap"
)

type DraftTool struct {
	Key     string `json:"key"`
	Name    string `json:"name,omitempty"`
	Enabled bool   `json:"enabled"`
}

type DraftOptions struct {
	IncludeObjective bool `json:"include_objective"`
	AllowSchedule    bool `json:"allow_schedule"`
	AllowHighRisk    bool `json:"allow_high_risk"`
}

type DraftRequest struct {
	Prompt         string       `json:"prompt"`
	Options        DraftOptions `json:"options"`
	AvailableTools []DraftTool  `json:"available_tools,omitempty"`
}

type DraftMeta struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
}

type DraftCapability struct {
	Label          string   `json:"label"`
	ToolName       string   `json:"tool_name,omitempty"`
	ToolCandidates []string `json:"tool_candidates,omitempty"`
}

type DraftAudit struct {
	Savable       bool     `json:"savable"`
	Validation    []string `json:"validation,omitempty"`
	MissingFields []string `json:"missing_fields,omitempty"`
	RiskWarnings  []string `json:"risk_warnings,omitempty"`
	Assumptions   []string `json:"assumptions,omitempty"`
	HighRisk      bool     `json:"high_risk"`
	NeedsHITL     bool     `json:"needs_hitl"`
}

type DraftResult struct {
	Graph        *graphDef         `json:"graph"`
	Meta         DraftMeta         `json:"meta"`
	Generator    string            `json:"generator"`
	Audit        DraftAudit        `json:"audit"`
	Capabilities []DraftCapability `json:"capabilities,omitempty"`
	Stats        map[string]int    `json:"stats"`
}

type llmDraftEnvelope struct {
	Graph        graphDef          `json:"graph"`
	Meta         DraftMeta         `json:"meta"`
	Capabilities []DraftCapability `json:"capabilities,omitempty"`
	Audit        DraftAudit        `json:"audit,omitempty"`
}

type draftToolHint struct {
	Label    string
	Keywords []string
	Tools    []string
}

var draftToolHints = []draftToolHint{
	{Label: "子域名发现", Keywords: []string{"子域名", "subdomain", "subfinder", "amass"}, Tools: []string{"subfinder", "amass"}},
	{Label: "端口扫描", Keywords: []string{"端口", "port", "nmap", "rustscan", "masscan"}, Tools: []string{"nmap", "rustscan", "masscan"}},
	{Label: "漏洞扫描", Keywords: []string{"漏洞", "vuln", "漏洞扫描", "nuclei", "nikto", "zap"}, Tools: []string{"nuclei", "nikto", "zap"}},
	{Label: "暴露面探测", Keywords: []string{"目录", "路径", "暴露页面", "dir", "ffuf", "gobuster", "feroxbuster"}, Tools: []string{"ffuf", "gobuster", "feroxbuster", "dirsearch"}},
	{Label: "证书与域名线索收集", Keywords: []string{"证书", "certificate", "crt"}, Tools: []string{"subfinder"}},
	{Label: "云配置审计", Keywords: []string{"云", "cloud", "配置审计", "prowler", "scout"}, Tools: []string{"prowler", "scout-suite"}},
	{Label: "容器安全检查", Keywords: []string{"容器", "镜像", "k8s", "kubernetes", "trivy", "kube"}, Tools: []string{"trivy", "kube-bench", "kube-hunter"}},
	{Label: "威胁情报收集", Keywords: []string{"情报", "威胁情报", "threat", "ioc", "virustotal", "shodan", "fofa"}, Tools: []string{"virustotal_search", "shodan_search", "fofa_search"}},
}

var highRiskDraftRE = regexp.MustCompile(`(?i)(隔离|封禁|加固|修复|执行|命令|脚本|删除|清理|阻断|封锁|攻击|利用|getshell|shell|payload|exploit|isolate|block|execute|script|delete|exploit|payload)`)

func GenerateDraftFromNaturalLanguage(ctx context.Context, req DraftRequest) (*DraftResult, error) {
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return nil, fmt.Errorf("工作流需求不能为空")
	}
	capabilities := detectDraftCapabilities(prompt, req.AvailableTools)
	wantsApproval := containsAnyFold(prompt, "审批", "确认", "审核", "负责人", "人工", "review", "approve", "approval", "human")
	wantsReport := containsAnyFold(prompt, "报告", "汇总", "输出", "通知", "任务", "工单", "report", "summary", "notify", "ticket")
	wantsCondition := containsAnyFold(prompt, "如果", "发现", "存在", "高危", "新增", "失败", "通过", "否则", "if", "when", "high", "critical", "new", "fail")
	highRisk := highRiskDraftRE.MatchString(prompt)

	builder := &draftGraphBuilder{x: 120, y: 150}
	assumptions := make([]string, 0)
	riskWarnings := make([]string, 0)
	missingFields := make([]string, 0)

	start := builder.add("start", "开始", map[string]any{"input_keys": "message, conversationId, projectId, target"}, 0)
	previous := start
	for _, capability := range capabilities {
		hasTool := strings.TrimSpace(capability.ToolName) != ""
		var id string
		if hasTool {
			id = builder.add("tool", capability.Label, map[string]any{
				"tool_name":       capability.ToolName,
				"arguments":       `{"target":"{{inputs.target}}","message":"{{inputs.message}}"}`,
				"timeout_seconds": "120",
				"join_strategy":   "all_merge",
			}, 0)
		} else {
			id = builder.add("agent", capability.Label, map[string]any{
				"agent_mode":              "eino_single",
				"input_binding":           map[string]any{"from": "previous", "field": "output"},
				"instruction":             capability.Label + "。根据用户需求执行安全流程步骤，并输出结构化结果：" + prompt,
				"output_key":              "agent_result",
				"join_strategy":           "all_merge",
				"missing_tool_candidates": strings.Join(capability.ToolCandidates, ", "),
			}, 0)
			if len(capability.ToolCandidates) > 0 {
				assumptions = append(assumptions, capability.Label+" 未匹配到已启用工具，已生成 Agent 草稿节点。")
				missingFields = append(missingFields, capability.Label+": 选择或启用对应 MCP 工具")
			}
		}
		builder.connect(previous, id, "", nil)
		previous = id
	}

	openConditionID := ""
	if wantsCondition {
		expr := `{{previous.output}} != ""`
		label := "是否满足触发条件"
		if highRisk {
			expr = `{{previous.output}} contains "高危"`
			label = "是否需要高风险处置"
		}
		condition := builder.add("condition", label, map[string]any{"expression": expr, "join_strategy": "all_merge"}, 0)
		builder.connect(previous, condition, "", nil)
		openConditionID = condition
		report := builder.add("output", draftOutputLabel(wantsReport), map[string]any{
			"output_key":     "result",
			"source_binding": map[string]any{"from": "previous", "field": "output"},
			"static_value":   "",
			"join_strategy":  "all_merge",
		}, 130)
		builder.connect(condition, report, "否", map[string]any{"condition": `{{previous.matched}} == "false"`, "branch": "false"})
		previous = condition
	}

	insertedHITL := false
	if highRisk {
		if !req.Options.AllowHighRisk || wantsApproval {
			approval := builder.add("hitl", "人工审批", map[string]any{
				"prompt":         "请确认是否允许继续执行高风险处置：" + prompt,
				"prompt_binding": map[string]any{"from": "previous", "field": "output"},
				"reviewer":       "human",
				"join_strategy":  "all_merge",
				"risk_level":     "high",
			}, 0)
			builder.connect(previous, approval, branchLabel(previous, openConditionID), branchConfig(previous, openConditionID, true))
			if previous == openConditionID {
				openConditionID = ""
			}
			previous = approval
			insertedHITL = true
		}
		action := builder.add("agent", "执行受控处置", map[string]any{
			"agent_mode":                  "eino_single",
			"input_binding":               map[string]any{"from": "previous", "field": "output"},
			"instruction":                 "仅在授权范围内生成处置步骤草稿；实际执行前必须由人工确认。用户需求：" + prompt,
			"output_key":                  "remediation_plan",
			"join_strategy":               "all_merge",
			"risk_level":                  "high",
			"requires_human_confirmation": "true",
		}, 0)
		builder.connect(previous, action, branchLabel(previous, openConditionID), branchConfig(previous, openConditionID, true))
		if previous == openConditionID {
			openConditionID = ""
		}
		previous = action
		if insertedHITL {
			riskWarnings = append(riskWarnings, "检测到高风险动作，已加入人工审批与 requires_human_confirmation 标记。")
		} else {
			riskWarnings = append(riskWarnings, "检测到高风险动作，已保留为草稿并添加 requires_human_confirmation 标记。")
		}
	} else if wantsApproval {
		approval := builder.add("hitl", "人工审批", map[string]any{
			"prompt":         "请审核工作流阶段结果：" + prompt,
			"prompt_binding": map[string]any{"from": "previous", "field": "output"},
			"reviewer":       "human",
			"join_strategy":  "all_merge",
		}, 0)
		builder.connect(previous, approval, "", nil)
		previous = approval
		insertedHITL = true
	}

	output := builder.add("output", draftOutputLabel(wantsReport), map[string]any{
		"output_key":     "result",
		"source_binding": map[string]any{"from": "previous", "field": "output"},
		"static_value":   "",
		"join_strategy":  "all_merge",
	}, 0)
	builder.connect(previous, output, branchLabel(previous, openConditionID), branchConfig(previous, openConditionID, true))

	graph := &graphDef{Nodes: builder.nodes, Edges: builder.edges, Config: map[string]any{
		"schema_version": 1,
		"generated_by":   "natural_language",
		"source_prompt":  prompt,
	}}
	if req.Options.IncludeObjective {
		graph.Config["objective"] = prompt
	}
	if req.Options.AllowSchedule && containsAnyFold(prompt, "每天", "每周", "定时", "周期", "持续", "daily", "weekly", "schedule", "monitor") {
		if containsAnyFold(prompt, "每天", "daily") {
			graph.Config["trigger_suggestion"] = "daily"
		} else {
			graph.Config["trigger_suggestion"] = "scheduled"
		}
		assumptions = append(assumptions, "已记录定时触发建议；保存后仍需在触发器或角色绑定处配置。")
	}

	raw, _ := json.Marshal(graph)
	validation := make([]string, 0)
	if err := ValidateGraphJSON(ctx, string(raw)); err != nil {
		validation = append(validation, err.Error())
	}
	return &DraftResult{
		Graph:     graph,
		Meta:      DraftMeta{ID: draftSlug(prompt), Name: draftName(prompt), Description: prompt, Enabled: true},
		Generator: "deterministic",
		Audit: DraftAudit{
			Savable:       len(validation) == 0,
			Validation:    validation,
			MissingFields: missingFields,
			RiskWarnings:  riskWarnings,
			Assumptions:   assumptions,
			HighRisk:      highRisk,
			NeedsHITL:     insertedHITL,
		},
		Capabilities: capabilities,
		Stats:        map[string]int{"nodes": len(graph.Nodes), "edges": len(graph.Edges)},
	}, nil
}

func GenerateDraftFromLLM(ctx context.Context, req DraftRequest, oa config.OpenAIConfig, logger *zap.Logger) (*DraftResult, error) {
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return nil, fmt.Errorf("工作流需求不能为空")
	}
	if strings.TrimSpace(oa.APIKey) == "" || strings.TrimSpace(oa.Model) == "" {
		return nil, fmt.Errorf("AI 通道未配置 api_key 或 model")
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	callCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	toolJSON, _ := json.Marshal(req.AvailableTools)
	systemPrompt := `你是 CyberStrikeAI 的工作流编排助手。你必须把用户的一句话需求转换为可保存的工作流草稿 JSON。
只返回 JSON 对象，不要 Markdown，不要解释。JSON 必须符合：
{
  "meta": {"id":"kebab-case-id","name":"短名称","description":"用户需求","enabled":true},
	"graph": {
    "nodes": [{"id":"start-1","type":"start","label":"显示名","position":{"x":120,"y":150},"config":{}}],
    "edges": [{"id":"edge-1","source":"start-1","target":"node-2","label":"","config":{}}],
    "config": {"schema_version":1,"generated_by":"llm","source_prompt":"用户原文"}
  },
  "capabilities": [{"label":"能力名","tool_name":"已匹配工具名","tool_candidates":["候选工具"]}],
  "audit": {"assumptions":[],"missing_fields":[],"risk_warnings":[]}
}
硬性规则：
- 只能输出一个合法 JSON object；不要输出 JSON Schema、注释、解释文字、Markdown 代码块或多余前后缀。
- 不要在 JSON 字符串值中使用竖线枚举写法；type 字段一次只能填写一个节点类型字符串。
- 至少 1 个 start 和 1 个 output；output/end 不能有出边。
- 节点 type 只能从这些字符串中选择：start、tool、agent、condition、hitl、output、end。
- 每个 agent、tool、output 节点都必须配置唯一的 output_key；output 节点默认使用 result。
- agent 节点必须配置 instruction 或 input_binding；默认 input_binding 为 {"from":"previous","field":"output"}。
- output 节点必须配置 source_binding 或 static_value；默认 source_binding 为 {"from":"previous","field":"output"}。
- tool 节点必须配置 tool_name、arguments、timeout_seconds；arguments 必须是合法 JSON 字符串。
- 所有非 start 且可能有多个上游的节点必须配置 join_strategy:"all_merge"。
- condition 最多 2 条出边，必须用 branch true/false，并用 label 是/否。
- tool 节点只有在 available_tools 中存在启用工具时才使用，否则用 agent 节点并在 audit.missing_fields 写明缺失工具。
- 高风险动作（执行脚本、隔离、封禁、删除、利用、payload、命令执行等）必须加入 hitl 审批，或在高风险节点 config 中标记 requires_human_confirmation:"true"、risk_level:"high"。
- 不要生成会真实执行攻击的参数；工具参数使用 {{inputs.target}}、{{inputs.message}} 占位。
- 所有节点 config 加 generated_by:"llm" 和 needs_review:"true"。`
	userPrompt := fmt.Sprintf("用户需求：%s\n\n选项：%+v\n\n可用工具 JSON：%s", prompt, req.Options, string(toolJSON))
	requestBody := map[string]interface{}{
		"model": strings.TrimSpace(oa.Model),
		"messages": []map[string]interface{}{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt},
		},
		"temperature":         0,
		oa.OutputLimitField(): 4096,
		"response_format":     map[string]interface{}{"type": "json_object"},
		"thinking":            map[string]interface{}{"type": "disabled"},
	}
	config.ApplyExtraRequestFields(requestBody, oa.Reasoning.ExtraRequestFields)
	var apiResponse struct {
		Choices []struct {
			Message struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
			} `json:"message"`
		} `json:"choices"`
	}
	client := openai.NewClient(&oa, nil, logger)
	if err := client.ChatCompletion(callCtx, requestBody, &apiResponse); err != nil {
		return nil, fmt.Errorf("调用大模型失败: %w", err)
	}
	if len(apiResponse.Choices) == 0 {
		return nil, fmt.Errorf("大模型未返回候选结果")
	}
	raw := strings.TrimSpace(apiResponse.Choices[0].Message.Content)
	if raw == "" {
		raw = strings.TrimSpace(apiResponse.Choices[0].Message.ReasoningContent)
	}
	env, err := parseLLMDraftEnvelope(raw)
	if err != nil {
		return nil, err
	}
	result := normalizeLLMDraft(prompt, req, env)
	graphRaw, _ := json.Marshal(result.Graph)
	validation := make([]string, 0)
	if err := ValidateGraphJSON(ctx, string(graphRaw)); err != nil {
		validation = append(validation, err.Error())
	}
	result.Audit.Validation = validation
	result.Audit.Savable = len(validation) == 0
	if !result.Audit.Savable {
		return nil, fmt.Errorf("大模型生成的工作流未通过校验: %s", strings.Join(validation, "；"))
	}
	return result, nil
}

type draftGraphBuilder struct {
	nodes   []graphNode
	edges   []graphEdge
	x       float64
	y       float64
	nodeSeq int
	edgeSeq int
}

func (b *draftGraphBuilder) add(nodeType, label string, config map[string]any, yOffset float64) string {
	b.nodeSeq++
	id := fmt.Sprintf("%s-%d", nodeType, b.nodeSeq)
	if config == nil {
		config = make(map[string]any)
	}
	config["generated_by"] = "natural_language"
	config["needs_review"] = "true"
	b.nodes = append(b.nodes, graphNode{
		ID:       id,
		Type:     nodeType,
		Label:    label,
		Position: graphPosition{X: b.x, Y: b.y + yOffset},
		Config:   config,
	})
	b.x += 210
	return id
}

func (b *draftGraphBuilder) connect(source, target, label string, config map[string]any) {
	b.edgeSeq++
	if config == nil {
		config = make(map[string]any)
	}
	b.edges = append(b.edges, graphEdge{ID: fmt.Sprintf("edge-ai-%d", b.edgeSeq), Source: source, Target: target, Label: label, Config: config})
}

func parseLLMDraftEnvelope(raw string) (llmDraftEnvelope, error) {
	var lastErr error
	for _, candidate := range jsonObjectCandidates(raw) {
		var env llmDraftEnvelope
		if err := json.Unmarshal([]byte(candidate), &env); err == nil {
			if len(env.Graph.Nodes) == 0 {
				lastErr = fmt.Errorf("大模型 JSON 缺少 graph.nodes")
				continue
			}
			return env, nil
		} else {
			lastErr = err
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("大模型响应为空")
	}
	return llmDraftEnvelope{}, fmt.Errorf("解析大模型工作流 JSON 失败: %w", lastErr)
}

func jsonObjectCandidates(raw string) []string {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimSpace(s)
	candidates := []string{s}
	if start := strings.Index(s, "{"); start >= 0 {
		if end := strings.LastIndex(s, "}"); end > start {
			candidates = append(candidates, s[start:end+1])
		}
	}
	return candidates
}

func normalizeLLMDraft(prompt string, req DraftRequest, env llmDraftEnvelope) *DraftResult {
	g := env.Graph
	if g.Config == nil {
		g.Config = make(map[string]any)
	}
	g.Config["schema_version"] = 1
	g.Config["generated_by"] = "llm"
	g.Config["source_prompt"] = prompt
	if req.Options.IncludeObjective {
		g.Config["objective"] = prompt
	}
	enabledTools := enabledDraftToolNames(req.AvailableTools)
	usedOutputKeys := make(map[string]bool)
	nodeTypes := make(map[string]string, len(g.Nodes))
	for i := range g.Nodes {
		if strings.TrimSpace(g.Nodes[i].ID) == "" {
			g.Nodes[i].ID = fmt.Sprintf("%s-%d", firstNonEmpty(g.Nodes[i].Type, "node"), i+1)
		}
		if strings.TrimSpace(g.Nodes[i].Type) == "" {
			g.Nodes[i].Type = "agent"
		}
		if strings.TrimSpace(g.Nodes[i].Label) == "" {
			g.Nodes[i].Label = displayNodeType(g.Nodes[i].Type)
		}
		if g.Nodes[i].Position.X == 0 && g.Nodes[i].Position.Y == 0 {
			g.Nodes[i].Position = graphPosition{X: 120 + float64(i)*210, Y: 150}
		}
		if g.Nodes[i].Config == nil {
			g.Nodes[i].Config = make(map[string]any)
		}
		g.Nodes[i].Config["generated_by"] = "llm"
		g.Nodes[i].Config["needs_review"] = "true"
		normalizeLLMNodeConfig(prompt, &g.Nodes[i], enabledTools, usedOutputKeys)
		nodeTypes[g.Nodes[i].ID] = strings.ToLower(strings.TrimSpace(g.Nodes[i].Type))
	}
	conditionBranchCounts := make(map[string]int)
	for i := range g.Edges {
		if strings.TrimSpace(g.Edges[i].ID) == "" {
			g.Edges[i].ID = fmt.Sprintf("edge-llm-%d", i+1)
		}
		if g.Edges[i].Config == nil {
			g.Edges[i].Config = make(map[string]any)
		}
		normalizeLLMEdgeConfig(&g.Edges[i], nodeTypes, conditionBranchCounts)
	}
	audit := env.Audit
	highRisk := highRiskDraftRE.MatchString(prompt) || graphHasHighRisk(g)
	audit.HighRisk = highRisk
	audit.NeedsHITL = graphHasNodeType(g, "hitl")
	if highRisk && !audit.NeedsHITL && !graphHasConfirmation(g) {
		audit.RiskWarnings = append(audit.RiskWarnings, "大模型生成包含高风险语义，请补充人工审批或确认标记后再运行。")
	}
	if len(audit.RiskWarnings) == 0 && highRisk {
		audit.RiskWarnings = append(audit.RiskWarnings, "检测到高风险动作，已标记为需要重点审计。")
	}
	meta := env.Meta
	if strings.TrimSpace(meta.Description) == "" {
		meta.Description = prompt
	}
	if strings.TrimSpace(meta.Name) == "" {
		meta.Name = draftName(prompt)
	}
	if strings.TrimSpace(meta.ID) == "" {
		meta.ID = draftSlug(prompt)
	}
	meta.Enabled = true
	return &DraftResult{
		Graph:        &g,
		Meta:         meta,
		Generator:    "llm",
		Audit:        audit,
		Capabilities: env.Capabilities,
		Stats:        map[string]int{"nodes": len(g.Nodes), "edges": len(g.Edges)},
	}
}

func normalizeLLMEdgeConfig(edge *graphEdge, nodeTypes map[string]string, conditionBranchCounts map[string]int) {
	if nodeTypes[strings.TrimSpace(edge.Source)] != "condition" {
		return
	}
	if conditionBranchHint(*edge) != "" {
		return
	}
	conditionBranchCounts[edge.Source]++
	branch := "true"
	label := "是"
	if conditionBranchCounts[edge.Source] > 1 {
		branch = "false"
		label = "否"
	}
	edge.Label = label
	edge.Config["branch"] = branch
}

func normalizeLLMNodeConfig(prompt string, node *graphNode, enabledTools map[string]bool, usedOutputKeys map[string]bool) {
	nodeType := strings.ToLower(strings.TrimSpace(node.Type))
	switch nodeType {
	case "start":
		if cfgString(node.Config, "input_keys") == "" {
			node.Config["input_keys"] = "message, conversationId, projectId, target"
		}
	case "tool":
		toolName := cfgString(node.Config, "tool_name")
		if toolName == "" || !enabledTools[strings.ToLower(toolName)] {
			node.Type = "agent"
			node.Config["missing_tool_name"] = toolName
			normalizeAgentDraftConfig(prompt, node, usedOutputKeys)
			return
		}
		if cfgString(node.Config, "arguments") == "" {
			node.Config["arguments"] = `{"target":"{{inputs.target}}","message":"{{inputs.message}}"}`
		}
		if cfgString(node.Config, "timeout_seconds") == "" {
			node.Config["timeout_seconds"] = "120"
		}
		ensureNodeOutputKey(node, usedOutputKeys, draftOutputKeyBase(node, "tool_result"))
		ensureJoinStrategy(node)
	case "agent":
		normalizeAgentDraftConfig(prompt, node, usedOutputKeys)
	case "condition":
		if cfgString(node.Config, "expression") == "" {
			node.Config["expression"] = `{{previous.output}} != ""`
		}
		ensureJoinStrategy(node)
	case "hitl":
		if cfgString(node.Config, "prompt") == "" {
			node.Config["prompt"] = "请审核工作流阶段结果：" + prompt
		}
		if cfgString(node.Config, "reviewer") == "" {
			node.Config["reviewer"] = "human"
		}
		ensureJoinStrategy(node)
	case "output":
		ensureNodeOutputKey(node, usedOutputKeys, "result")
		if cfgString(node.Config, "static_value") == "" {
			if _, ok := parseFieldBinding(node.Config, "source_binding"); !ok {
				node.Config["source_binding"] = map[string]any{"from": "previous", "field": "output"}
			}
		}
		ensureJoinStrategy(node)
	case "end":
		ensureJoinStrategy(node)
	}
}

func normalizeAgentDraftConfig(prompt string, node *graphNode, usedOutputKeys map[string]bool) {
	if cfgString(node.Config, "agent_mode") == "" {
		node.Config["agent_mode"] = "eino_single"
	}
	if cfgString(node.Config, "instruction") == "" {
		node.Config["instruction"] = node.Label + "。根据用户需求执行安全流程步骤，并输出结构化结果：" + prompt
	}
	if _, ok := parseFieldBinding(node.Config, "input_binding"); !ok {
		node.Config["input_binding"] = map[string]any{"from": "previous", "field": "output"}
	}
	ensureNodeOutputKey(node, usedOutputKeys, draftOutputKeyBase(node, "agent_result"))
	ensureJoinStrategy(node)
}

func ensureJoinStrategy(node *graphNode) {
	if cfgString(node.Config, "join_strategy") == "" {
		node.Config["join_strategy"] = "all_merge"
	}
}

func ensureNodeOutputKey(node *graphNode, used map[string]bool, fallback string) {
	current := sanitizeOutputKey(cfgString(node.Config, "output_key"))
	if current == "" {
		current = sanitizeOutputKey(fallback)
	}
	if current == "" {
		current = "result"
	}
	base := current
	for i := 2; used[current]; i++ {
		current = fmt.Sprintf("%s_%d", base, i)
	}
	node.Config["output_key"] = current
	used[current] = true
}

func draftOutputKeyBase(node *graphNode, fallback string) string {
	if name := cfgString(node.Config, "tool_name"); name != "" {
		return name + "_result"
	}
	if node.ID != "" {
		return node.ID + "_result"
	}
	return fallback
}

func sanitizeOutputKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastUnderscore := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if b.Len() > 0 && !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	return strings.Trim(b.String(), "_")
}

func enabledDraftToolNames(tools []DraftTool) map[string]bool {
	names := make(map[string]bool, len(tools)*2)
	for _, tool := range tools {
		if !tool.Enabled {
			continue
		}
		if key := strings.ToLower(strings.TrimSpace(tool.Key)); key != "" {
			names[key] = true
		}
		if name := strings.ToLower(strings.TrimSpace(tool.Name)); name != "" {
			names[name] = true
		}
	}
	return names
}

func graphHasNodeType(g graphDef, nodeType string) bool {
	for _, node := range g.Nodes {
		if strings.EqualFold(node.Type, nodeType) {
			return true
		}
	}
	return false
}

func graphHasConfirmation(g graphDef) bool {
	for _, node := range g.Nodes {
		if cfgString(node.Config, "requires_human_confirmation") == "true" {
			return true
		}
	}
	return false
}

func graphHasHighRisk(g graphDef) bool {
	for _, node := range g.Nodes {
		if cfgString(node.Config, "risk_level") == "high" || cfgString(node.Config, "requires_human_confirmation") == "true" {
			return true
		}
		if highRiskDraftRE.MatchString(node.Label) || highRiskDraftRE.MatchString(cfgString(node.Config, "instruction")) {
			return true
		}
	}
	return false
}

func detectDraftCapabilities(prompt string, tools []DraftTool) []DraftCapability {
	capabilities := make([]DraftCapability, 0)
	for _, hint := range draftToolHints {
		if containsAnyFold(prompt, hint.Keywords...) {
			capabilities = append(capabilities, DraftCapability{
				Label:          hint.Label,
				ToolName:       matchDraftTool(hint.Tools, tools),
				ToolCandidates: append([]string(nil), hint.Tools...),
			})
		}
	}
	if len(capabilities) == 0 {
		capabilities = append(capabilities, DraftCapability{Label: "节点能力", ToolCandidates: nil})
	}
	return capabilities
}

func matchDraftTool(candidates []string, tools []DraftTool) string {
	if len(candidates) == 0 || len(tools) == 0 {
		return ""
	}
	for _, enabledOnly := range []bool{true, false} {
		for _, candidate := range candidates {
			candidate = strings.ToLower(strings.TrimSpace(candidate))
			for _, tool := range tools {
				if enabledOnly && !tool.Enabled {
					continue
				}
				key := strings.ToLower(strings.TrimSpace(firstNonEmpty(tool.Key, tool.Name)))
				if key != "" && strings.Contains(key, candidate) {
					return firstNonEmpty(tool.Key, tool.Name)
				}
			}
		}
	}
	return ""
}

func containsAnyFold(text string, needles ...string) bool {
	lower := strings.ToLower(text)
	for _, needle := range needles {
		if strings.Contains(lower, strings.ToLower(needle)) {
			return true
		}
	}
	return false
}

func draftOutputLabel(wantsReport bool) string {
	if wantsReport {
		return "输出报告"
	}
	return "输出"
}

func branchLabel(source, conditionID string) string {
	if source == conditionID && conditionID != "" {
		return "是"
	}
	return ""
}

func branchConfig(source, conditionID string, yes bool) map[string]any {
	if source != conditionID || conditionID == "" {
		return nil
	}
	if yes {
		return map[string]any{"condition": `{{previous.matched}} == "true"`, "branch": "true"}
	}
	return map[string]any{"condition": `{{previous.matched}} == "false"`, "branch": "false"}
}

func draftName(prompt string) string {
	runes := []rune(strings.TrimSpace(prompt))
	if len(runes) > 22 {
		return string(runes[:22]) + "..."
	}
	return string(runes)
}

func draftSlug(prompt string) string {
	lower := strings.ToLower(strings.TrimSpace(prompt))
	var b strings.Builder
	lastDash := false
	for _, r := range lower {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash && b.Len() > 0 {
			b.WriteByte('-')
			lastDash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug != "" {
		if len(slug) > 48 {
			return strings.Trim(slug[:48], "-")
		}
		return slug
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(lower))
	if !utf8.ValidString(lower) || lower == "" {
		lower = "workflow"
	}
	return fmt.Sprintf("ai-workflow-%x", h.Sum32())
}
