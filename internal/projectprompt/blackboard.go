// Package projectprompt provides the system prompt text for the project blackboard (plain strings, no database dependency).
// It is referenced by the agent / multiagent packages, avoiding the agent → project import cycle that would break gopls metadata.
package projectprompt

import (
	"strings"

	"cyberstrike-ai/internal/mcp/builtin"
)

const (
	factRhythmCore              = "Do not wait for the session to end or wind down before writing in batch. As soon as one new insight is **confirmed** (open port / service version, entry path, authentication state or credential traits, exploitable point or attack-surface change), **immediately** call `upsert_project_fact` (the same fact_key overwrites and updates). As soon as one reproducible vulnerability is **verified** (including POC / impact), **immediately** call `record_vulnerability`; a fact and a vulnerability may each be recorded once. Land the record in the database before continuing the next step of work, so details are not lost when the context is compacted. If no project is bound, say the blackboard cannot be written to, and still keep an evidence summary in this turn."
	factRhythmCoordinatorSuffix = " When a delegation / sub task returns new insights or vulnerabilities, the coordinator writes them promptly itself; do not assume the sub agent already recorded them."
	factRhythmSubAgentSuffix    = " If these tools are not available, the deliverable must end with structured entries awaiting record (suggested fact_key, summary, body / POC highlights) that the coordinator will write **immediately**."
)

// FactRecordingIncrementalRhythmMarkdown returns the penetration-as-you-record rhythm (Markdown, matching agents/*.md and the docs).
func FactRecordingIncrementalRhythmMarkdown(coordinator, subAgent bool) string {
	var b strings.Builder
	b.WriteString("- **Record as you penetrate (mandatory rhythm)**: ")
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
	b.WriteString("- **Record as you penetrate (mandatory rhythm)**: Do not wait for the session to end or wind down before writing in batch. As soon as one new insight is **confirmed** (open port / service version, entry path, authentication state or credential traits, exploitable point or attack-surface change), **immediately** call ")
	b.WriteString(builtin.ToolUpsertProjectFact)
	b.WriteString(" (the same fact_key overwrites and updates). As soon as one reproducible vulnerability is **verified** (including POC / impact), **immediately** call ")
	b.WriteString(builtin.ToolRecordVulnerability)
	b.WriteString("; a fact and a vulnerability may each be recorded once. Land the record in the database before continuing the next step of work, so details are not lost when the context is compacted. If no project is bound, say the blackboard cannot be written to, and still keep an evidence summary in this turn.")
	if coordinator {
		b.WriteString(factRhythmCoordinatorSuffix)
	}
	if subAgent {
		b.WriteString(factRhythmSubAgentSuffix)
	}
	return b.String()
}

func factEdgeRecordingGuidance() string {
	return `### Fact relation edges (links)

- When writing a **finding / chain / exploit / poc**, ` + "`links`" + ` **must** be provided in ` + "`upsert_project_fact`" + ` (**recommended ` + "`from`" + `**: the source fact pointing to the current one, i.e. ` + "`from`" + ` → current ` + "`fact_key`" + `).
- **Minimum requirement**: a finding records at least 1 edge from=target/* + type=discovered_on (i.e. target → finding); on the finding itself record an exploit with from=exploit/* + type=exploits (i.e. exploit → finding).
- **Common types**: ` + "`discovered_on`" + ` (where it was found), ` + "`depends_on`" + ` (prerequisite for reproduction), ` + "`leads_to`" + ` (insight advancing), ` + "`enables`" + ` (widening the attack surface), ` + "`exploits`" + ` (exploitation relation), ` + "`contains`" + ` (asset containment), ` + "`part_of`" + ` (belongs to a chain/group), ` + "`supports`" + ` (evidence support).
- On update: **omitting links keeps the existing edges**; passing links **replaces** all relation edges (from → current fact).
- A "dependent facts" paragraph inside body can coexist with links (for humans to read); the structured relations are authoritative in links.`
}

func factRecordingGuidanceBlock() string {
	return `### Fact-writing rules (auditable reproduction / knowledge capture)

- **summary**: one index line; it must carry the gist of "what + where + how triggered/verified"; never write the conclusion alone (e.g. only "SQLi exists").
- **body**: the full reproducible context, written into the body field of ` + "`upsert_project_fact`" + `; the index does not hold body, later sessions must fetch it via ` + "`get_project_fact`" + `.
- **category / fact_key suggestions**:
  - environment insight: ` + "`target/`" + `, ` + "`auth/`" + `, ` + "`infra/`" + `, ` + "`business/`" + ` (body can simply use the environment templates)
  - findings and exploitation: ` + "`finding/`" + `, ` + "`chain/`" + `, ` + "`exploit/`" + `, ` + "`poc/`" + ` (**must** fill body fully with the attack-chain template: entry point, step-by-step attack chain, raw request/response or command, evidence, related vulnerability ID)
- **Auto continuation notes**: ` + "`note/context-*`" + ` is the system-reserved session working summary, not a project-confirmed fact; never overwrite that prefix with upsert_project_fact. The default index and fact tools expose only the current session's continuation notes, while ordinary project facts remain shared. When a cross-session reference is needed, explicitly use list_project_facts / search_project_facts / get_project_fact with include_history=true; lists and searches can narrow the source range with source_conversation_id. History returns mark the source session, update time, and history reference - re-check the current authorization, target, and credential validity; do not auto-execute old tasks or treat old summaries as confirmed findings. When an exact value is missing, read back in segments along the snapshot path in the body; do not guess the original value. The same session/agent updates the same key; when a verified shared fact needs to be captured, use a separate finding/auth key and keep the source evidence.
- **Division of labor with vulnerability records**: ` + "`record_vulnerability`" + ` records deliverable findings; facts record **all context needed for reproduction** (including failed attempts, bypasses, dependent sessions); the two may each be recorded once.
- When updating the same finding, keep writing with the same ` + "`fact_key`" + ` overwrite; do not scatter it across several keys and lose the context.`
}

// FactRecordingBlackboardSection is the complete system-prompt block for the project blackboard and vulnerability recording (shared by single/multi agent main agents).
func FactRecordingBlackboardSection(coordinatorDelegate bool) string {
	var b strings.Builder
	b.WriteString("## Project blackboard (facts) and vulnerability records (kept separate)\n\n")
	b.WriteString("When the current conversation is bound to a project, the system automatically injects the \"project blackboard index\" (fact_key + summary only). **Whenever the summary is insufficient you must call ")
	b.WriteString(builtin.ToolGetProjectFact)
	b.WriteString("(fact_key) to fetch the body; do not invent details from the summary.**\n\n")
	b.WriteString(factRecordingIncrementalRhythmBuiltin(coordinatorDelegate, false))
	b.WriteString("\n\n")
	b.WriteString("- **Environment / target / authentication insight** (not an official vulnerability entry): use ")
	b.WriteString(builtin.ToolUpsertProjectFact)
	b.WriteString(", suggested fact_key `category/slug` (e.g. target/primary_domain); the same key overwrites and updates; body records ports / versions / credential traits and the evidence source.\n")
	b.WriteString("- **Findings and exploitation context** (auditable reproduction): suggested fact_key prefixes finding/, chain/, exploit/, poc/; **body is required** to hold the complete attack chain (entry point → steps → raw request/response or command → phenomenon → related related_vulnerability_id), **never the conclusion alone**; summary carries one line of \"what + where + how verified\".\n")
	b.WriteString("- **Deliverable vulnerabilities**: use ")
	b.WriteString(builtin.ToolRecordVulnerability)
	b.WriteString(", with title, severity, type, target, proof (POC), impact, and remediation advice. Before recording you can first dedupe with ")
	b.WriteString(builtin.ToolListVulnerabilities)
	b.WriteString(", and pull detail with ")
	b.WriteString(builtin.ToolGetVulnerability)
	b.WriteString("(id) (by default only the current project/conversation). When re-testing an existing vulnerability, use update_vulnerability_status(id, status, retest_notes) to update the status and append evidence instead of recording a duplicate; when verification is impossible, keep the original status.\n")
	b.WriteString("- The same finding may need to be **recorded once each** (the fact holds the **complete attack chain and exploit detail** for reproduction, the vulnerability holds the official findings). For a false positive use ")
	b.WriteString(builtin.ToolDeprecateProjectFact)
	b.WriteString(" or vulnerability status false_positive.\n")
	b.WriteString("- With many facts, retrieve via ")
	b.WriteString(builtin.ToolListProjectFacts)
	b.WriteString(" / ")
	b.WriteString(builtin.ToolSearchProjectFacts)
	b.WriteString(".\n\n")
	b.WriteString(factEdgeRecordingGuidance())
	b.WriteString("\n\n")
	b.WriteString(factRecordingGuidanceBlock())
	b.WriteString("\n\nSeverity: critical / high / medium / low / info. Proof must contain sufficient evidence (request/response, screenshots, command output, etc.).")
	return b.String()
}

// FactRecordingSubAgentSection: the sub agent records as it penetrates (when no tools are available, it outputs entries awaiting record).
func FactRecordingSubAgentSection() string {
	return "## Record as you penetrate\n\n" + ProjectResultRecordingGuidance() + "\n\n" + factRecordingIncrementalRhythmBuiltin(false, true) + "\n"
}

// FactRecordingBlackboardSectionMarkdown is the Markdown equivalent of FactRecordingBlackboardSection (tool names as literals, matching agents/*.md).
func FactRecordingBlackboardSectionMarkdown(coordinatorDelegate bool) string {
	var b strings.Builder
	b.WriteString("## Project blackboard (facts) and vulnerability records (kept separate)\n\n")
	b.WriteString("When the current conversation is bound to a project, the system automatically injects the \"project blackboard index\" (fact_key + summary only). **Whenever the summary is insufficient you must call `get_project_fact(fact_key)` to fetch the body; do not invent details from the summary.**\n\n")
	b.WriteString(FactRecordingIncrementalRhythmMarkdown(coordinatorDelegate, false))
	b.WriteString("\n\n")
	b.WriteString("- **Environment / target / authentication insight** (not an official vulnerability): use **`upsert_project_fact`**, suggested `fact_key` of `category/slug` (e.g. `target/primary_domain`); the same key overwrites and updates; body records ports / versions / credential traits and the evidence source.\n")
	b.WriteString("- **Findings and exploitation context** (auditable reproduction): `fact_key` prefixes `finding/`, `chain/`, `exploit/`, `poc/`; **body is required** to hold the complete attack chain (entry point → steps → raw request/response or command → phenomenon → related `related_vulnerability_id`), **never the conclusion alone**; summary carries one line of \"what + where + how verified\".\n")
	b.WriteString("- **Deliverable vulnerabilities**: use **`record_vulnerability`** (title, description, severity, type, target, proof POC, impact, remediation advice). Severity critical / high / medium / low / info.\n")
	b.WriteString("- The same finding may need to be **recorded once each** (the fact holds the reproducible attack chain, the vulnerability holds the official findings). For a false positive use **`deprecate_project_fact`** or vulnerability status false_positive.\n")
	b.WriteString("- With many facts, retrieve via **`list_project_facts`** / **`search_project_facts`**.\n\n")
	b.WriteString(factEdgeRecordingGuidance())
	b.WriteString("\n\n")
	b.WriteString(factRecordingGuidanceBlock())
	b.WriteString("\n\nSeverity: critical / high / medium / low / info. Proof must contain sufficient evidence (request/response, screenshots, command output, etc.).")
	return b.String()
}

// FactEdgeRecordingGuidance: the agent rules when writing edges (shared with the project package).
func FactEdgeRecordingGuidance() string { return factEdgeRecordingGuidance() }

// FactRecordingGuidanceBlock: the fact-writing rules block (shared with the project package).
func FactRecordingGuidanceBlock() string { return factRecordingGuidanceBlock() }
