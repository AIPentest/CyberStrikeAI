package project

import (
	"fmt"
	"strings"

	"cyberstrike-ai/internal/projectprompt"
)

// The fact category constants (written to the category field of upsert_project_fact).
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

// RequiresAttackChainBody Decides whether the fact should carry a reproducible attack chain / exploit details (written in the body, not only summary).
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

// IsSparseFactBody Returns true when an attack-chain fact's body is too short or missing key sections (a soft check that does not block writing).
func IsSparseFactBody(category, factKey, body string) bool {
	if !RequiresAttackChainBody(category, factKey) {
		return false
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return true
	}
	lower := strings.ToLower(body)
	// Should contain at least one reproducible clue: steps/request/command/code block
	hasSteps := strings.Contains(lower, "Attack chain") || strings.Contains(lower, "## Attack") ||
		strings.Contains(lower, "## exploit") || strings.Contains(lower, "## poc")
	hasHTTP := strings.Contains(lower, "```http") || strings.Contains(lower, "```bash") ||
		strings.Contains(lower, "curl ") || strings.Contains(lower, "get ") || strings.Contains(lower, "post ")
	hasReq := strings.Contains(lower, "Request") || strings.Contains(lower, "Response") || strings.Contains(lower, "payload")
	// With no structural clues such as an attack chain/POC/request, it is treated as a conclusion-only description (regardless of length)
	return !(hasSteps || hasHTTP || hasReq)
}

// FactBodyTemplate Returns the suggested body Markdown skeleton by category (for the agent to fill with real content).
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

const attackChainFactBodyTemplate = `## Conclusion (verifiable, one sentence)
<Do not write only 'a vulnerability exists'; state the type + Location + Trigger conditions>

## Target and entry point
- Target: <URL / IP:Port / hostname>
- Entry point: <path / API / parameter>
- Preconditions: <anonymous / role / cookie / other dependencies>

## Attack chain (step-by-step reproducible)
1. <Reconnaissance/discovery>
2. <Exploitation/trigger>
3. <Impact proof (file read, RCE echo, unauthorized data, etc.)>

## Exploit / POC
### Request
` + "```http\n<METHOD> <path> HTTP/1.1\nHost: ...\n...\n\n<body>\n```" + `

### Response / symptom
<Key response fragment, status code, difference points>

### Command / script (if any)
` + "```bash\n<command>\n```" + `

## Key evidence
- <Tool-output summary / screenshot path / session or message ID>

## Related
- related_vulnerability_id: <Optional, corresponds to record_vulnerability's id>
- links (upsert Parameter): [{ "from": "<fact_key>", "type": "discovered_on|..." }] (from → Current fact)
- Dependency facts (human-readable mirror in the body): <fact_key, e.g. auth/session_cookie>

## Notes and uncertainties
<Hypotheses to verify, environment differences, records of bypass attempts>`

const envFactBodyTemplate = `## Summary
<The core insight of this fact>

## Details
<port/version/path/credential characteristics/business rules, etc.>

## Source and evidence
<Command output, response fragment, discovery time>

## Related
- Related fact_key: <optional>`

// FactRecordingGuidanceBlock Written into the system prompt: requires facts to persist the attack-chain context rather than only the conclusion.
func FactRecordingGuidanceBlock() string {
	return projectprompt.FactRecordingGuidanceBlock()
}

// SparseBodyWarning The tool-return hint when an attack-chain fact's body is insufficient (does not block saving).
func SparseBodyWarning(category, factKey string) string {
	if !IsSparseFactBody(category, factKey, "") {
		return ""
	}
	return fmt.Sprintf(
		"\n\n⚠ Note: category=%q / fact_key=%q is an attack-chain fact, but the body is empty or too brief. Please fill in the full attack chain and POC (see the template), to aid later audit reproduction.\n Suggested body skeleton: \n%s",
		category, factKey, FactBodyTemplate(category, factKey),
	)
}

// SparseBodyWarningIfNeeded Decides whether to append a warning based on the actual body.
func SparseBodyWarningIfNeeded(category, factKey, body string) string {
	if !IsSparseFactBody(category, factKey, body) {
		return ""
	}
	return SparseBodyWarning(category, factKey)
}

const artifactFactBodyTemplate = `## Results and verification status
<purpose, applicable target/entry point; verified success / failure / unverified>

## File and invocation
- Project working-directory relative path: artifacts/<filename>
- Dependencies and versions: <dependency files, install/runtime environment>
- Invocation method and variable parameters: <command, inputs/outputs; auth reference auth/*>

## Verification evidence
<verification time, raw command/output, applicable conditions; do not infer success from a file's existence>

## Related
<Source session, dependencies fact_key, related_vulnerability_id>`

const testResultFactBodyTemplate = `## Tested conditions
<target, entry point, parameters, identity/auth, version, test time>

## Method and evidence
<request/command, response difference, tool output; errors/timeouts must be marked separately>

## Conclusion and boundaries
<success / not found under the tested conditions / blocked / pending; does not extend to untested entry points>

## Retest trigger conditions
<target/entry point/identity/version change, insufficient original evidence, or a user-requested retest>

## Related
<source session, script artifact/*, related facts/vulnerabilities ID>`
