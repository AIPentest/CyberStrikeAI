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
	hasSteps := strings.Contains(lower, "attack chain") || strings.Contains(lower, "攻击链") || strings.Contains(lower, "## 攻击") || strings.Contains(lower, "## attack") ||
		strings.Contains(lower, "## exploit") || strings.Contains(lower, "## poc")
	hasHTTP := strings.Contains(lower, "```http") || strings.Contains(lower, "```bash") ||
		strings.Contains(lower, "curl ") || strings.Contains(lower, "get ") || strings.Contains(lower, "post ")
	hasReq := strings.Contains(lower, "请求") || strings.Contains(lower, "响应") || strings.Contains(lower, "request") || strings.Contains(lower, "response") || strings.Contains(lower, "payload")
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

const attackChainFactBodyTemplate = `## Conclusion (verifiable, in one sentence)
<do not just write "there is a vulnerability"; state the type + location + trigger conditions>

## Target and entry
- Target: <URL / IP:Port / hostname>
- Entry: <path / interface / parameter>
- Preconditions: <anonymous / role / Cookie / other dependencies>

## Attack chain (step by step, reproducible)
1. <recon/discovery>
2. <exploitation/trigger>
3. <impact proof (file read, RCE echo, cross-authority data etc.)>

## Exploit / POC
### Request
` + "```http\n<METHOD> <path> HTTP/1.1\nHost: ...\n...\n\n<body>\n```" + `

### Response / phenomenon
<key response fragment, status code, differences>

### Command / script (if any)
` + "```bash\n<command>\n```" + `

## Key evidence
- <tool output summary / screenshot path / session or message ID>

## Related links
- related_vulnerability_id: <optional, the id of the corresponding record_vulnerability>
- links (upsert parameter): [{ "from": "<fact_key>", "type": "discovered_on|..." }] (from → current fact)
- dependency facts (human-readable mirror in body): <fact_key, e.g. auth/session_cookie>

## Remarks and uncertainty
<hypotheses to verify, environment differences, bypass attempts>`

const envFactBodyTemplate = `## Summary
<the core insight of this fact>

## Details
<ports / versions / paths / credential traits / business rules etc.>

## Source and evidence
<command output, response fragment, discovery time>

## Related links
- related fact_key: <optional>`

func FactRecordingGuidanceBlock() string {
	return projectprompt.FactRecordingGuidanceBlock()
}

// SparseBodyWarning 攻击链类事实 body 不足时的工具返回提示（不阻断保存）。
func SparseBodyWarning(category, factKey string) string {
	if !IsSparseFactBody(category, factKey, "") {
		return ""
	}
	return fmt.Sprintf(
		"\n\n⚠ Hint: category=%q / fact_key=%q belongs to the attack-chain class of facts, but the body is empty or too sparse. Please add the complete attack chain and POC (see the template) so later audits can reproduce it.\nSuggested body skeleton:\n%s",
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

const artifactFactBodyTemplate = `## Outcome and verification status
<purpose, applicable target/entry; verified successful / failed / not verified>

## File and invocation
- relative path in the project working directory: artifacts/<file name>
- dependencies and versions: <dependency files, install/run environment>
- invocation and variable parameters: <command, input/output; auth references auth/*>

## Verification evidence
<verification time, original command/output, applicable conditions; do not infer success from the file existing>

## Related links
<source session, dependency fact_key, related_vulnerability_id>`
const testResultFactBodyTemplate = `## Tested conditions
<target, entry, parameters, identity/authentication, version, test time>

## Method and evidence
<request/command, response differences, tool output; errors/timeouts must be marked separately>

## Conclusion and boundaries
<success / not found under the tested conditions / blocked / to-verify; do not extend to untested entries>

## Retest trigger conditions
<target/entry/identity/version changes, original evidence insufficient, or the user asks for a retest>

## Related links
<source session, artifact/* script, related fact/vulnerability ID>`
