package project

import (
	"fmt"
	"strings"

	"cyberstrike-ai/internal/projectprompt"
)

// 事实 category 常量（写入 upsert_project_fact 的 category 字段）。
const (
	FactCategoryTarget   = "target"
	FactCategoryAuth     = "auth"
	FactCategoryInfra    = "infra"
	FactCategoryBusiness = "business"
	FactCategoryFinding  = "finding"
	FactCategoryChain    = "chain"
	FactCategoryExploit  = "exploit"
	FactCategoryPOC      = "poc"
	FactCategoryNote     = "note"
)

// RequiresAttackChainBody 判断该事实是否应携带可复现的攻击链 / exploit 详情（写在 body，非仅 summary）。
func RequiresAttackChainBody(category, factKey string) bool {
	c := strings.ToLower(strings.TrimSpace(category))
	switch c {
	case FactCategoryFinding, FactCategoryChain, FactCategoryExploit, FactCategoryPOC, "vuln":
		return true
	}
	key := strings.ToLower(strings.TrimSpace(factKey))
	for _, prefix := range []string{"finding/", "chain/", "exploit/", "poc/"} {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

// IsSparseFactBody 攻击链类事实 body 过短或缺少关键段落时返回 true（软校验，不阻断写入）。
func IsSparseFactBody(category, factKey, body string) bool {
	if !RequiresAttackChainBody(category, factKey) {
		return false
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return true
	}
	lower := strings.ToLower(body)
	// 至少应包含可复现线索：步骤/请求/命令/代码块 之一
	hasSteps := strings.Contains(lower, "攻击链") || strings.Contains(lower, "## 攻击") ||
		strings.Contains(lower, "## exploit") || strings.Contains(lower, "## poc")
	hasHTTP := strings.Contains(lower, "```http") || strings.Contains(lower, "```bash") ||
		strings.Contains(lower, "curl ") || strings.Contains(lower, "get ") || strings.Contains(lower, "post ")
	hasReq := strings.Contains(lower, "请求") || strings.Contains(lower, "响应") || strings.Contains(lower, "payload")
	// 无攻击链/POC/请求等结构线索，视为仅结论性描述（不论长短）
	return !(hasSteps || hasHTTP || hasReq)
}

// FactBodyTemplate 按 category 返回建议的 body Markdown 骨架（供 Agent 填入真实内容）。
func FactBodyTemplate(category, factKey string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(factKey)), "artifact/") {
		return artifactFactBodyTemplate
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(factKey)), "test/") || strings.HasPrefix(strings.ToLower(strings.TrimSpace(factKey)), "negative/") {
		return testResultFactBodyTemplate
	}
	if RequiresAttackChainBody(category, factKey) {
		return attackChainFactBodyTemplate
	}
	return envFactBodyTemplate
}

const attackChainFactBodyTemplate = `## 结论（可验证，一句话）
<勿仅写「存在漏洞」；写明类型 + 位置 + 触发条件>

## 目标与入口
- 目标: <URL / IP:Port / 主机名>
- 入口: <路径 / 接口 / 参数>
- 前置条件: <匿名 / 角色 / Cookie / 其他依赖>

## 攻击链（逐步可复现）
1. <侦察/发现>
2. <利用/触发>
3. <影响证明（读文件、RCE 回显、越权数据等）>

## Exploit / POC
### 请求
` + "```http\n<METHOD> <path> HTTP/1.1\nHost: ...\n...\n\n<body>\n```" + `

### 响应 / 现象
<关键响应片段、状态码、差异点>

### 命令 / 脚本（如有）
` + "```bash\n<command>\n```" + `

## 关键证据
- <工具输出摘要 / 截图路径 / 会话或消息 ID>

## 关联
- related_vulnerability_id: <可选，对应 record_vulnerability 的 id>
- links（upsert 参数）: [{ "from": "<fact_key>", "type": "discovered_on|..." }]（from → 当前 fact）
- 依赖事实（body 可读镜像）: <fact_key，如 auth/session_cookie>

## 备注与不确定性
<待验证假设、环境差异、绕过尝试记录>`

const envFactBodyTemplate = `## 摘要
<该事实的核心认知>

## 细节
<端口/版本/路径/凭据特征/业务规则等>

## 来源与证据
<命令输出、响应片段、发现时间>

## 关联
- 相关 fact_key: <可选>`

// FactRecordingGuidanceBlock 写入系统提示：要求事实沉淀攻击链上下文而非仅结论。
func FactRecordingGuidanceBlock() string {
	return projectprompt.FactRecordingGuidanceBlock()
}

// SparseBodyWarning 攻击链类事实 body 不足时的工具返回提示（不阻断保存）。
func SparseBodyWarning(category, factKey string) string {
	if !IsSparseFactBody(category, factKey, "") {
		return ""
	}
	return fmt.Sprintf(
		"\n\n⚠ 提示：category=%q / fact_key=%q 属于攻击链类事实，但 body 为空或过简。请补充完整攻击链与 POC（参考模板），便于后续审计复现。\n建议 body 骨架：\n%s",
		category, factKey, FactBodyTemplate(category, factKey),
	)
}

// SparseBodyWarningIfNeeded 根据实际 body 判断是否追加警告。
func SparseBodyWarningIfNeeded(category, factKey, body string) string {
	if !IsSparseFactBody(category, factKey, body) {
		return ""
	}
	return SparseBodyWarning(category, factKey)
}

const artifactFactBodyTemplate = `## 成果与验证状态
<用途、适用目标/入口；已验证成功 / 失败 / 未验证>

## 文件与调用
- 项目工作目录相对路径: artifacts/<文件名>
- 依赖与版本: <依赖文件、安装/运行环境>
- 调用方式与可变参数: <命令、输入输出；认证引用 auth/*>

## 验证证据
<验证时间、原始命令/输出、适用条件；不要从文件存在推断成功>

## 关联
<来源会话、依赖 fact_key、related_vulnerability_id>`

const testResultFactBodyTemplate = `## 已测条件
<目标、入口、参数、身份/认证、版本、测试时间>

## 方法与证据
<请求/命令、响应差异、工具输出；错误/超时须单独标明>

## 结论与边界
<成功 / 在已测条件下未发现 / 受阻 / 待验证；不扩展到未测试入口>

## 复测触发条件
<目标/入口/身份/版本变化、原证据不足或用户要求复测>

## 关联
<来源会话、脚本 artifact/*、相关事实/漏洞 ID>`
