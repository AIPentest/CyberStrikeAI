// Package projectprompt Provides the project-blackboard system-prompt text (a plain string, with no database dependency).
// referenced by packages such as agent / multiagent, to avoid agent → project an import cycle causing gopls metadata to fail.
package projectprompt

import (
	"strings"

	"cyberstrike-ai/internal/mcp/builtin"
)

const (
	factRhythmCore              = "Do not wait until the session ends or wraps up to write in bulk. After each **confirmed** new insight (an open port/service version, an entry path, an auth state or credential characteristic, an exploitable point or attack-surface change), **immediately** call `upsert_project_fact` (the same fact_key is overwritten in place). After each **verified** reproducible vulnerability (with POC/impact), **immediately** call `record_vulnerability`; and the fact can each be recorded once. Persist them before moving on to the next step, to avoid losing details after context compaction. When no project is bound, note that the blackboard cannot be written and still keep an evidence summary in this turn."
	factRhythmCoordinatorSuffix = "When a delegated/subtask returns a new insight or vulnerability, the coordinator writes it in promptly; do not assume the sub-agent has already recorded it."
	factRhythmSubAgentSuffix    = "If the above tools are not in the toolset, append a 'to be persisted' structured entry at the end of the deliverable (suggested fact_key, summary, body/POC key points) for the coordinator to **immediately** write in."
)

// FactRecordingIncrementalRhythmMarkdown Returns the record-as-you-test cadence (Markdown, to keep agents/*.md aligned with the documentation).
func FactRecordingIncrementalRhythmMarkdown(coordinator, subAgent bool) string {
	var b strings.Builder
	b.WriteString("- **Record as you test (mandatory cadence)**: ")
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
	b.WriteString("- **Record as you test (mandatory cadence)**: do not wait until the session ends or wraps up to write in bulk. After each **confirmed** new insight (an open port/service version, an entry path, an auth state or credential characteristic, an exploitable point or attack-surface change), **immediately** call ")
	b.WriteString(builtin.ToolUpsertProjectFact)
	b.WriteString(" (the same fact_key is overwritten in place). After each **verified** reproducible vulnerability (with POC/impact), **immediately** call ")
	b.WriteString(builtin.ToolRecordVulnerability)
	b.WriteString("; and the fact can each be recorded once. Persist them before moving on to the next step, to avoid losing details after context compaction. When no project is bound, note that the blackboard cannot be written and still keep an evidence summary in this turn.")
	if coordinator {
		b.WriteString(factRhythmCoordinatorSuffix)
	}
	if subAgent {
		b.WriteString(factRhythmSubAgentSuffix)
	}
	return b.String()
}

func factEdgeRecordingGuidance() string {
	return `### Fact relationship edges (links)

- When writing **finding / chain / exploit / poc**, you **must** ` + "`upsert_project_fact`" + ` provided in ` + "`links`" + ` (**Recommended ` + "`from`" + `**: the source fact points to the current fact, i.e. ` + "`from`" + ` → current ` + "`fact_key`" + `).
- **Minimum requirement**: at least 1 finding-type entry from=target/* + type=discovered_on (i.e. target → finding); record on the finding for exploit use from=exploit/* + type=exploits (i.e. exploit → finding).
- **commonly used type**: ` + "`discovered_on`" + ` (where it was found), ` + "`depends_on`" + ` (reproduction prerequisites), ` + "`leads_to`" + ` (insight progression), ` + "`enables`" + ` (expand the attack surface), ` + "`exploits`" + ` (exploitation relationship), ` + "`contains`" + ` (asset contains), ` + "`part_of`" + ` (belongs to a chain/group), ` + "`supports`" + ` (evidence support).
- On update: **omitting links keeps the existing edges**; passing links **replaces** all relationship edges (from → current fact).
- body the 'dependency facts' section can coexist with links (for human reading); the structured relationships are governed by links.`
}

func factRecordingGuidanceBlock() string {
	return `### Fact-writing spec (audit reproduction / knowledge retention)

- **summary**: a one-line index that must include the "what + where + how to trigger/verify" key points; do not write only the conclusion (such as only "SQLi exists").
- **body**: the full reproducible context, written into ` + "`upsert_project_fact`" + ` the body field; the index does not include the body, so later sessions must rely on ` + "`get_project_fact`" + ` retrieve.
- **Suggested category / fact_key**:
  - environment insight: ` + "`target/`" + `, ` + "`auth/`" + `, ` + "`infra/`" + `, ` + "`business/`" + ` (body just use the environment template)
  - Finding and exploitation: ` + "`finding/`" + `, ` + "`chain/`" + `, ` + "`exploit/`" + `, ` + "`poc/`" + ` (**You **must** fill the body using the attack-chain template: entry point, step-by-step attack chain, raw request/response or command, evidence, related vulnerabilities ID)
- **auto-continuation record**: ` + "`note/context-*`" + ` is a system-reserved session work summary, not a confirmed project fact; do not overwrite this prefix with upsert_project_fact. By default the index and fact tools only provide the current session's continuation records, while ordinary project facts are still shared. To reference across sessions, explicitly use include_history=true with list_project_facts / search_project_facts / get_project_fact; list and search can use source_conversation_id to narrow the source. History results annotate the source session, update time, and a historical note; verify the current authorization, target, and credential validity, and do not automatically run old tasks or treat an old summary as a confirmed finding. When an exact value is missing, read it back in segments by the snapshot path in the body rather than guessing the original value. The same session/agent updates the same key; to persist a verified shared fact, use a separate key such as finding/auth and keep the source evidence.
- **the division of labor with the vulnerability record**: ` + "`record_vulnerability`" + ` records the deliverable findings; the fact records **all the context needed for reproduction** (including failed attempts, bypasses, and session dependencies), and the two can each be recorded once.
- keep it the same when updating the same finding ` + "`fact_key`" + ` Overwrite in place; do not scatter across multiple keys and lose context.`
}

// FactRecordingBlackboardSection The complete system-prompt block for the project blackboard and vulnerability records (shared by the single-/multi-agent main agent).
func FactRecordingBlackboardSection(coordinatorDelegate bool) string {
	var b strings.Builder
	b.WriteString("## The project blackboard (facts) and vulnerability records (separate)\n\n")
	b.WriteString("If the current conversation is bound to a project, the system automatically injects the 'project blackboard index' (only fact_key + summary). **When the summary is insufficient you must call ")
	b.WriteString(builtin.ToolGetProjectFact)
	b.WriteString("(fact_key) retrieve the body; do not fabricate details from the summary.**\n\n")
	b.WriteString(factRecordingIncrementalRhythmBuiltin(coordinatorDelegate, false))
	b.WriteString("\n\n")
	b.WriteString("- **environment/target/auth insights** (not a formal vulnerability entry): use ")
	b.WriteString(builtin.ToolUpsertProjectFact)
	b.WriteString(", fact_key Recommendation `category/slug` (such as target/primary_domain); the same key is overwritten in place; the body records port/version/credential characteristics and the evidence source.\n")
	b.WriteString("- **Finding and exploitation context** (audit reproduction): a fact_key prefix of finding/, chain/, exploit/, or poc/ is recommended; **the body is required** and holds the full attack chain (entry point → Steps → raw request/response or command → Symptom → link it via related_vulnerability_id); **do not write only the conclusion**; summary states 'what + where + 'how to verify' as a one-line key point.\n")
	b.WriteString("- **Deliverable vulnerability**: use ")
	b.WriteString(builtin.ToolRecordVulnerability)
	b.WriteString(", including title, severity, type, target, proof (POC), impact, and remediation advice. Before recording you may first ")
	b.WriteString(builtin.ToolListVulnerabilities)
	b.WriteString(" deduplicate; for details use ")
	b.WriteString(builtin.ToolGetVulnerability)
	b.WriteString("(id) (by default only the current project/session). After retesting an existing vulnerability, use update_vulnerability_status(id, status, retest_notes) to update the status and append evidence rather than recording a duplicate; keep the original status when it cannot be verified.\n")
	b.WriteString("- The same finding may need to be **recorded once in each** (the fact records the **full attack chain and exploit details** for reproduction, the vulnerability records the formal findings). For false positives, use ")
	b.WriteString(builtin.ToolDeprecateProjectFact)
	b.WriteString(" or vulnerability status false_positive.\n")
	b.WriteString("- when there are many facts, use ")
	b.WriteString(builtin.ToolListProjectFacts)
	b.WriteString(" / ")
	b.WriteString(builtin.ToolSearchProjectFacts)
	b.WriteString(" Search.\n\n")
	b.WriteString(factEdgeRecordingGuidance())
	b.WriteString("\n\n")
	b.WriteString(factRecordingGuidanceBlock())
	b.WriteString("\n\n Severity: critical / high / medium / low / info. The proof must include sufficient evidence (request/response, screenshots, command output, etc.).")
	return b.String()
}

// FactRecordingSubAgentSection The sub-agent records as it tests (when it has no tools, it outputs a to-be-persisted entry).
func FactRecordingSubAgentSection() string {
	return "## Record as you test\n\n" + ProjectResultRecordingGuidance() + "\n\n" + factRecordingIncrementalRhythmBuiltin(false, true) + "\n"
}

// FactRecordingBlackboardSectionMarkdown Markdown equivalent to FactRecordingBlackboardSection (tool names are literals, for use by agents/*.md).
func FactRecordingBlackboardSectionMarkdown(coordinatorDelegate bool) string {
	var b strings.Builder
	b.WriteString("## The project blackboard (facts) and vulnerability records (separate)\n\n")
	b.WriteString("If the current conversation is bound to a project, the system automatically injects the 'project blackboard index' (only `fact_key` + summary). **When the summary is insufficient you must call `get_project_fact(fact_key)` retrieve the body; do not fabricate details from the summary.**\n\n")
	b.WriteString(FactRecordingIncrementalRhythmMarkdown(coordinatorDelegate, false))
	b.WriteString("\n\n")
	b.WriteString("- **environment/target/auth insights** (not a formal vulnerability): use **`upsert_project_fact`**, `fact_key` Recommendation `category/slug` (e.g. `target/primary_domain`), The same key is overwritten in place; the body records port/version/credential characteristics and the evidence source.\n")
	b.WriteString("- **Finding and exploitation context** (audit reproduction): `fact_key` Recommendation `finding/`, `chain/`, `exploit/`, `poc/` prefix; **the body is required** and holds the full attack chain (entry point → Steps → raw request/response or command → Symptom → Related `related_vulnerability_id`), **do not write only the conclusion**; summary states 'what + where + 'how to verify' as a one-line key point.\n")
	b.WriteString("- **Deliverable vulnerability**: use **`record_vulnerability`** (title, description, severity, type, target, proof POC, impact, remediation advice). Severity critical / high / medium / low / info.\n")
	b.WriteString("- The same finding may need to be **recorded once in each** (the fact records the reproducible attack chain, the vulnerability records the formal findings). For false positives, use **`deprecate_project_fact`** or vulnerability status false_positive.\n")
	b.WriteString("- when there are many facts, use **`list_project_facts`** / **`search_project_facts`** Search.\n\n")
	b.WriteString(factEdgeRecordingGuidance())
	b.WriteString("\n\n")
	b.WriteString(factRecordingGuidanceBlock())
	b.WriteString("\n\n Severity: critical / high / medium / low / info. The proof must include sufficient evidence (request/response, screenshots, command output, etc.).")
	return b.String()
}

// FactEdgeRecordingGuidance The agent spec for writing edges (reused by the project package).
func FactEdgeRecordingGuidance() string { return factEdgeRecordingGuidance() }

// FactRecordingGuidanceBlock The fact-writing spec block (reused by the project package).
func FactRecordingGuidanceBlock() string { return factRecordingGuidanceBlock() }
