package handler

import (
	"strings"

	"cyberstrike-ai/internal/database"
)

// WebshellSkillHintDefault is the Skills guidance shared by the conversation
// page and Eino single-agent, appended at the end of the webshell context so
// the AI knows the `skill` load entry.
const WebshellSkillHintDefault = "For Skills packages use the built-in `skill` tool of the multi-agent / Eino DeepAgent session to load progressively."

// WebshellSkillHintMultiAgent is the Skills guidance used in the
// multi-agent / Eino multi-agent preparation stage.
const WebshellSkillHintMultiAgent = "For Skills packages use the Eino multi-agent built-in `skill` tool."

// webshellAssistantToolList is the tool list the AI assistant may use under a
// WebShell context (displayed to the model).
// Note: this is a display string only; the real permission restriction is the
// roleTools slice set by the caller.
const webshellAssistantToolList = "webshell_exec、webshell_file_list、webshell_file_read、webshell_file_write、record_vulnerability、list_vulnerabilities、get_vulnerability、upsert_project_fact、get_project_fact、list_project_facts、search_project_facts、deprecate_project_fact、restore_project_fact、list_knowledge_risk_types、search_knowledge_base"

// BuildWebshellAssistantContext builds the AI assistant context prompt from the
// connection info and the user's raw message.
// The context contains: connection ID, remark, target system (with the matching
// command-set guidance), response encoding, the available tool list, the Skills
// load entry, and the final user request. The caller only decides the
// skillHint copy (defaults to WebshellSkillHintDefault).
//
// This logic lives in a shared function so agent.go / multi_agent_prepare.go
// and friends do not copy-paste it, and upgrading the OS / Encoding wording
// only needs one change point, one test, synced everywhere.
func BuildWebshellAssistantContext(conn *database.WebShellConnection, skillHint, userMsg string) string {
	if conn == nil {
		// Fallback: the caller guarantees conn is non-nil; defensively return
		// the original message.
		return userMsg
	}
	remark := conn.Remark
	if remark == "" {
		remark = conn.URL
	}

	targetOS := resolveWebshellOS(conn.OS, conn.Type) // normalized to "linux" / "windows"
	encoding := normalizeWebshellEncoding(conn.Encoding)
	if skillHint == "" {
		skillHint = WebshellSkillHintDefault
	}

	var b strings.Builder
	b.Grow(512 + len(userMsg))

	b.WriteString("[WebShell assistant context] Connection ID: ")
	b.WriteString(conn.ID)
	b.WriteString(", remark: ")
	b.WriteString(remark)
	b.WriteByte('\n')

	// Target system: tell the AI exactly which command set it can/cannot use,
	// so it does not run ls/cat/rm against Windows.
	b.WriteString("- Target system: ")
	b.WriteString(describeTargetOSForPrompt(targetOS))
	b.WriteByte('\n')

	// Response encoding: only stated explicitly when not auto; auto is backend
	// adaptive and should not disturb the model.
	if encHint := describeEncodingForPrompt(encoding); encHint != "" {
		b.WriteString("- Response encoding: ")
		b.WriteString(encHint)
		b.WriteByte('\n')
	}

	// Tool list & connection_id constraint: keep the legacy phrasing the AI is
	// already used to.
	b.WriteString("Available tools (use only when operating on this connection, fill connection_id with \"")
	b.WriteString(conn.ID)
	b.WriteString("\"): ")
	b.WriteString(webshellAssistantToolList)
	b.WriteString(". Record while you penetrate: upsert_project_fact for every new confirmed knowledge, record_vulnerability for every verified vulnerability, do not wait for the session to end. ")
	b.WriteString(skillHint)
	b.WriteString("\n\nUser request: ")
	b.WriteString(userMsg)

	return b.String()
}

// describeTargetOSForPrompt returns the human-readable description + recommended
// command set + counter-examples for an OS. The command list covers the 6 most
// common file-management actions (list/read/delete/rename/mkdir/find) so the AI
// can copy them directly.
func describeTargetOSForPrompt(targetOS string) string {
	switch targetOS {
	case "windows":
		return "Windows (recommend cmd/PowerShell: dir /a, type, del /q /f, move /y, md, ren; " +
			"find files with `dir /s /b filter` or PowerShell `Get-ChildItem -Recurse`; " +
			"avoid ls / cat / rm / mv / find and other Unix commands, otherwise you will get `not recognized as an internal or external command`)"
	case "linux":
		return "Linux/Unix (recommend sh/bash: ls -la, cat, rm -f, mv, mkdir -p; " +
			"find files with `find /path -name '*pattern*'`; " +
			"avoid dir, type, del, move and other Windows commands)"
	default:
		// Unreachable in theory: resolveWebshellOS already falls back.
		return "unknown (run `uname || ver` first to probe, then pick the command set)"
	}
}

// describeEncodingForPrompt returns a human-readable description of the
// response encoding; auto returns an empty string to save tokens.
func describeEncodingForPrompt(encoding string) string {
	switch encoding {
	case "utf-8":
		return "UTF-8 (target is natively UTF-8, no extra decoding needed)"
	case "gbk":
		return "GBK (Chinese Windows; the backend already transcodes to UTF-8 when returning; if you still see many \\uFFFD replacement characters, the command failed or encoding detection was wrong)"
	case "gb18030":
		return "GB18030 (the backend already transcodes to UTF-8 when returning)"
	default:
		return ""
	}
}
