// Package projectprompt 提供项目黑板相关的系统提示文本（纯字符串，无 database 依赖）。
// 供 agent / multiagent 等包引用，避免 agent → project 导入环导致 gopls 元数据失败。
package projectprompt

import (
	"strings"

	"cyberstrike-ai/internal/mcp/builtin"
)

const (
	factRhythmCore              = "勿等会话结束或收尾再批量写入。每**确认**一条新认知（开放端口/服务版本、入口路径、认证态或凭据特征、可利用点或攻击面变化）后，**立即**调用 `upsert_project_fact`（同 fact_key 覆盖更新）。每**验证**出一条可复现漏洞（含 POC/影响）后，**立即**调用 `record_vulnerability`；与事实可各记一次。继续下一步工作前优先落库，避免上下文压缩后细节丢失。未绑项目时说明无法写黑板，仍在本轮保留证据摘要。"
	factRhythmCoordinatorSuffix = "委派/子任务返回新认知或漏洞时，由协调者及时写入，勿假定子代理已记。"
	factRhythmSubAgentSuffix    = "若工具集中无上述工具，须在交付物末尾给出「待落库」结构化条目（fact_key 建议、summary、body/POC 要点），供协调者**立即**写入。"
)

// FactRecordingIncrementalRhythmMarkdown 返回边渗透边记录节奏（Markdown，供 agents/*.md 与文档对齐）。
func FactRecordingIncrementalRhythmMarkdown(coordinator, subAgent bool) string {
	var b strings.Builder
	b.WriteString("- **边渗透边记录（强制节奏）**：")
	b.WriteString(factRhythmCore)
	if coordinator {
		b.WriteString(factRhythmCoordinatorSuffix)
	}
	if subAgent {
		b.WriteString(factRhythmSubAgentSuffix)
	}
	return b.String()
}

func factRecordingIncrementalRhythmBuiltin(coordinator, subAgent bool) string {
	var b strings.Builder
	b.WriteString("- **边渗透边记录（强制节奏）**：勿等会话结束或收尾再批量写入。每**确认**一条新认知（开放端口/服务版本、入口路径、认证态或凭据特征、可利用点或攻击面变化）后，**立即**调用 ")
	b.WriteString(builtin.ToolUpsertProjectFact)
	b.WriteString("（同 fact_key 覆盖更新）。每**验证**出一条可复现漏洞（含 POC/影响）后，**立即**调用 ")
	b.WriteString(builtin.ToolRecordVulnerability)
	b.WriteString("；与事实可各记一次。继续下一步工作前优先落库，避免上下文压缩后细节丢失。未绑项目时说明无法写黑板，仍在本轮保留证据摘要。")
	if coordinator {
		b.WriteString(factRhythmCoordinatorSuffix)
	}
	if subAgent {
		b.WriteString(factRhythmSubAgentSuffix)
	}
	return b.String()
}

func factEdgeRecordingGuidance() string {
	return `### 事实关系边（links）

- 写入 **finding / chain / exploit / poc** 时，**必须**在 ` + "`upsert_project_fact`" + ` 中提供 ` + "`links`" + `（**推荐 ` + "`from`" + `**：来源 fact 指向当前 fact，即 ` + "`from`" + ` → 当前 ` + "`fact_key`" + `）。
- **最少要求**：finding 类至少 1 条 from=target/* + type=discovered_on（即 target → finding）；在 finding 上记录 exploit 用 from=exploit/* + type=exploits（即 exploit → finding）。
- **常用 type**：` + "`discovered_on`" + `（发现在哪）、` + "`depends_on`" + `（复现前置）、` + "`leads_to`" + `（认知推进）、` + "`enables`" + `（扩大攻击面）、` + "`exploits`" + `（利用关系）、` + "`contains`" + `（资产包含）、` + "`part_of`" + `（属于链/组）、` + "`supports`" + `（证据支撑）。
- 更新时：**省略 links 保留已有边**；传入 links 则**替换**全部关系边（from → 当前 fact）。
- body 中「依赖事实」段落可与 links 并存（人读）；结构化关系以 links 为准。`
}

func factRecordingGuidanceBlock() string {
	return `### 事实写入规范（审计复现 / 知识沉淀）

- **summary**：索引用一行，须含「什么 + 在哪 + 如何触发/验证」要点，禁止只写结论（如仅写「存在 SQLi」）。
- **body**：完整可复现上下文，写入 ` + "`upsert_project_fact`" + ` 的 body 字段；索引不含 body，后续会话须靠 ` + "`get_project_fact`" + ` 取回。
- **category / fact_key 建议**：
  - 环境认知：` + "`target/`" + `、` + "`auth/`" + `、` + "`infra/`" + `、` + "`business/`" + `（body 用环境模板即可）
  - 发现与利用：` + "`finding/`" + `、` + "`chain/`" + `、` + "`exploit/`" + `、` + "`poc/`" + `（**必须**用攻击链模板填满 body：入口、逐步攻击链、原始请求/响应或命令、证据、关联漏洞 ID）
- **自动续接记录**：` + "`note/context-*`" + ` 是系统保留的会话工作摘要，不是项目已确认事实，禁止用 upsert_project_fact 覆盖该前缀。默认索引和事实工具只提供当前会话的续接记录，项目普通事实仍共享。需跨会话参考时，显式用 list_project_facts / search_project_facts / get_project_fact 的 include_history=true；列表和搜索可用 source_conversation_id 缩小来源范围。历史返回标注来源会话、更新时间及历史参考，核对当前授权、目标和凭据有效性，勿自动执行旧任务或将旧摘要当作已确认发现。缺少精确值时按正文中的快照路径分段回读，不猜测原值。同一会话/代理更新同一 key；需沉淀已核实的共享事实时另用独立 finding/auth 等 key，保留来源证据。
- **与漏洞记录分工**：` + "`record_vulnerability`" + ` 记可交付 findings；事实记**复现所需的全部上下文**（含失败尝试、绕过、依赖会话），二者可各记一次。
- 更新同一发现时保持相同 ` + "`fact_key`" + ` 覆盖写入，勿散落多个 key 导致上下文丢失。`
}

// FactRecordingBlackboardSection 项目黑板与漏洞记录的完整系统提示块（单/多 Agent 主代理共用）。
func FactRecordingBlackboardSection(coordinatorDelegate bool) string {
	var b strings.Builder
	b.WriteString("## 项目黑板（事实）与漏洞记录（分离）\n\n")
	b.WriteString("当前对话若已绑定项目，系统会自动注入「项目黑板索引」（仅 fact_key + 摘要）。**摘要不足时必须调用 ")
	b.WriteString(builtin.ToolGetProjectFact)
	b.WriteString("(fact_key) 获取 body，禁止凭摘要臆造细节。**\n\n")
	b.WriteString(factRecordingIncrementalRhythmBuiltin(coordinatorDelegate, false))
	b.WriteString("\n\n")
	b.WriteString("- **环境/目标/认证等认知**（非正式漏洞条目）：使用 ")
	b.WriteString(builtin.ToolUpsertProjectFact)
	b.WriteString("，fact_key 建议 `category/slug`（如 target/primary_domain），同 key 覆盖更新；body 记端口/版本/凭据特征与证据来源。\n")
	b.WriteString("- **发现与利用上下文**（审计复现）：fact_key 建议 finding/、chain/、exploit/、poc/ 前缀；**body 必填**完整攻击链（入口 → 步骤 → 原始请求/响应或命令 → 现象 → 关联 related_vulnerability_id），**禁止仅写结论**；summary 写「什么 + 在哪 + 如何验证」一行要点。\n")
	b.WriteString("- **可交付漏洞**：使用 ")
	b.WriteString(builtin.ToolRecordVulnerability)
	b.WriteString("，含标题、严重程度、类型、目标、证明（POC）、影响、修复建议。记前可先 ")
	b.WriteString(builtin.ToolListVulnerabilities)
	b.WriteString(" 查重，详情用 ")
	b.WriteString(builtin.ToolGetVulnerability)
	b.WriteString("(id)（默认仅当前项目/会话）。复测已有漏洞后用 update_vulnerability_status(id, status, retest_notes) 更新状态并追加证据，勿重复记录；无法验证时保留原状态。\n")
	b.WriteString("- 同一发现可能需**各记一次**（事实记**完整攻击链与 exploit 细节**供复现，漏洞记正式 findings）。误报用 ")
	b.WriteString(builtin.ToolDeprecateProjectFact)
	b.WriteString(" 或漏洞状态 false_positive。\n")
	b.WriteString("- 事实多时用 ")
	b.WriteString(builtin.ToolListProjectFacts)
	b.WriteString(" / ")
	b.WriteString(builtin.ToolSearchProjectFacts)
	b.WriteString(" 检索。\n\n")
	b.WriteString(factEdgeRecordingGuidance())
	b.WriteString("\n\n")
	b.WriteString(factRecordingGuidanceBlock())
	b.WriteString("\n\n严重程度：critical / high / medium / low / info。证明须含足够证据（请求响应、截图、命令输出等）。")
	return b.String()
}

// FactRecordingSubAgentSection 子代理边渗透边记录（无工具时输出待落库条目）。
func FactRecordingSubAgentSection() string {
	return "## 边渗透边记录\n\n" + ProjectResultRecordingGuidance() + "\n\n" + factRecordingIncrementalRhythmBuiltin(false, true) + "\n"
}

// FactRecordingBlackboardSectionMarkdown 与 FactRecordingBlackboardSection 等价的 Markdown（工具名为字面量，供 agents/*.md）。
func FactRecordingBlackboardSectionMarkdown(coordinatorDelegate bool) string {
	var b strings.Builder
	b.WriteString("## 项目黑板（事实）与漏洞记录（分离）\n\n")
	b.WriteString("当前对话若已绑定项目，系统会自动注入「项目黑板索引」（仅 `fact_key` + 摘要）。**摘要不足时必须调用 `get_project_fact(fact_key)` 获取 body，禁止凭摘要臆造细节。**\n\n")
	b.WriteString(FactRecordingIncrementalRhythmMarkdown(coordinatorDelegate, false))
	b.WriteString("\n\n")
	b.WriteString("- **环境/目标/认证等认知**（非正式漏洞）：使用 **`upsert_project_fact`**，`fact_key` 建议 `category/slug`（如 `target/primary_domain`），同 key 覆盖更新；body 记端口/版本/凭据特征与证据来源。\n")
	b.WriteString("- **发现与利用上下文**（审计复现）：`fact_key` 建议 `finding/`、`chain/`、`exploit/`、`poc/` 前缀；**body 必填**完整攻击链（入口 → 步骤 → 原始请求/响应或命令 → 现象 → 关联 `related_vulnerability_id`），**禁止仅写结论**；summary 写「什么 + 在哪 + 如何验证」一行要点。\n")
	b.WriteString("- **可交付漏洞**：使用 **`record_vulnerability`**（标题、描述、严重程度、类型、目标、证明 POC、影响、修复建议）。严重程度 critical / high / medium / low / info。\n")
	b.WriteString("- 同一发现可能需**各记一次**（事实记可复现攻击链，漏洞记正式 findings）。误报用 **`deprecate_project_fact`** 或漏洞状态 false_positive。\n")
	b.WriteString("- 事实多时用 **`list_project_facts`** / **`search_project_facts`** 检索。\n\n")
	b.WriteString(factEdgeRecordingGuidance())
	b.WriteString("\n\n")
	b.WriteString(factRecordingGuidanceBlock())
	b.WriteString("\n\n严重程度：critical / high / medium / low / info。证明须含足够证据（请求响应、截图、命令输出等）。")
	return b.String()
}

// FactEdgeRecordingGuidance 写入边时的 Agent 规范（供 project 包复用）。
func FactEdgeRecordingGuidance() string { return factEdgeRecordingGuidance() }

// FactRecordingGuidanceBlock 事实写入规范块（供 project 包复用）。
func FactRecordingGuidanceBlock() string { return factRecordingGuidanceBlock() }
