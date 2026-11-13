package handler

import (
	"strings"

	"cyberstrike-ai/internal/database"
)

// WebshellSkillHintDefault 对话页 / Agent 单代理共用的 Skills 说明，放在 webshell 上下文末尾，
// 供 AI 选择 skill 加载入口时参考。
const WebshellSkillHintDefault = "Skills 包请使用「多代理 / Agent DeepAgent」会话中的内置 `skill` 工具渐进加载。"

// WebshellSkillHintMultiAgent 多代理 / Agent 多代理准备阶段使用的 Skills 说明
const WebshellSkillHintMultiAgent = "Skills 包请使用 Agent 多代理内置 `skill` 工具。"

// webshellAssistantToolList AI 助手在 WebShell 上下文下允许使用的工具清单（展示给模型用）。
// 注意：此处只是展示字符串，真正的权限限制是在调用方设置的 roleTools 切片里。
const webshellAssistantToolList = "webshell_exec、webshell_file_list、webshell_file_read、webshell_file_write、record_vulnerability、list_vulnerabilities、get_vulnerability、upsert_project_fact、get_project_fact、list_project_facts、search_project_facts、deprecate_project_fact、restore_project_fact、list_knowledge_risk_types、search_knowledge_base"

// BuildWebshellAssistantContext 根据连接信息与用户原始消息组装 AI 助手的上下文提示词。
// 上下文包含：连接 ID、备注、目标系统（及对应命令集建议）、响应编码、可用工具清单、Skills 加载入口、
// 以及最终的用户请求。调用方只需要决定 skillHint 的文案（默认使用 WebshellSkillHintDefault）。
//
// 之所以把这段逻辑抽到共享函数里，是为了避免 agent.go / multi_agent_prepare.go 等多处复制粘贴，
// 并确保当我们升级 OS / Encoding 文案时只需要改一处、测一处、同步生效。
func BuildWebshellAssistantContext(conn *database.WebShellConnection, skillHint, userMsg string) string {
	if conn == nil {
		// 兜底：调用方已保证 conn 非 nil，这里只是防御性返回原消息
		return userMsg
	}
	remark := conn.Remark
	if remark == "" {
		remark = conn.URL
	}

	targetOS := resolveWebshellOS(conn.OS, conn.Type) // 归一为 "linux" / "windows"
	encoding := normalizeWebshellEncoding(conn.Encoding)
	if skillHint == "" {
		skillHint = WebshellSkillHintDefault
	}

	var b strings.Builder
	b.Grow(512 + len(userMsg))

	b.WriteString("[WebShell 助手上下文] 连接 ID：")
	b.WriteString(conn.ID)
	b.WriteString("，备注：")
	b.WriteString(remark)
	b.WriteByte('\n')

	// 目标系统：明确告诉 AI 能用/不能用的命令集，避免它对着 Windows 发 ls/cat/rm
	b.WriteString("- 目标系统：")
	b.WriteString(describeTargetOSForPrompt(targetOS))
	b.WriteByte('\n')

	// 响应编码：仅在非 auto 时显式告知，auto 模式由后端自适应，不打扰模型
	if encHint := describeEncodingForPrompt(encoding); encHint != "" {
		b.WriteString("- 响应编码：")
		b.WriteString(encHint)
		b.WriteByte('\n')
	}

	// 工具清单 & connection_id 约束：保持旧有表达，AI 已熟悉
	b.WriteString("可用工具（仅在该连接上操作时使用，connection_id 填 \"")
	b.WriteString(conn.ID)
	b.WriteString("\"）：")
	b.WriteString(webshellAssistantToolList)
	b.WriteString("。边渗透边记录：每确认新认知即 upsert_project_fact，每验证漏洞即 record_vulnerability，勿等会话结束。")
	b.WriteString(skillHint)
	b.WriteString("\n\n用户请求：")
	b.WriteString(userMsg)

	return b.String()
}

// describeTargetOSForPrompt 返回某个 OS 对应的中文描述 + 推荐命令集 + 反例，
// 命令列表覆盖文件管理最常用的 6 类动作（查看/读/删/改名/建目录/查找），让 AI 能直接照抄。
func describeTargetOSForPrompt(targetOS string) string {
	switch targetOS {
	case "windows":
		return "Windows（推荐 cmd/PowerShell：dir /a、type、del /q /f、move /y、md、ren；" +
			"查找文件用 `dir /s /b 过滤词` 或 PowerShell `Get-ChildItem -Recurse`；" +
			"避免 ls / cat / rm / mv / find 等 Unix 命令，否则将返回 `不是内部或外部命令`）"
	case "linux":
		return "Linux/Unix（推荐 sh/bash：ls -la、cat、rm -f、mv、mkdir -p；" +
			"查找文件用 `find /path -name '*pattern*'`；" +
			"避免 dir、type、del、move 等 Windows 命令）"
	default:
		// 理论上不会走到这里，resolveWebshellOS 已经兜底
		return "未知（请先执行 `uname || ver` 探测再决定命令集）"
	}
}

// describeEncodingForPrompt 返回响应编码的人类可读描述；auto 返回空串以减少 token。
func describeEncodingForPrompt(encoding string) string {
	switch encoding {
	case "utf-8":
		return "UTF-8（目标原生 UTF-8，无需额外解码）"
	case "gbk":
		return "GBK（中文 Windows；后端已自动转码为 UTF-8 返回，若仍出现大量 \\uFFFD 替换字符说明命令失败或编码识别错误）"
	case "gb18030":
		return "GB18030（后端已自动转码为 UTF-8 返回）"
	default:
		return ""
	}
}
