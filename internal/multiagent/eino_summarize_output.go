package multiagent

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"cyberstrike-ai/internal/agent"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

var (
	summarizationAnalysisBlockRegex = regexp.MustCompile(`(?is)<analysis>\s*.*?\s*</analysis>`)
	summarizationSummaryBlockRegex  = regexp.MustCompile(`(?is)<summary>\s*(.*?)\s*</summary>`)
	userIntentLedgerBlockRegex      = regexp.MustCompile(`(?is)<original_user_intent_ledger>\s*(.*?)\s*</original_user_intent_ledger>`)
	userIntentLedgerSectionRegex    = regexp.MustCompile(`(?is)\s*## 原始用户输入与约束账本（系统保真）\s*<original_user_intent_ledger>\s*.*?\s*</original_user_intent_ledger>\s*`)
)

const (
	userIntentLedgerStartMarker = "<original_user_intent_ledger>"
	userIntentLedgerEndMarker   = "</original_user_intent_ledger>"
	userIntentLedgerExtraKey    = agent.OriginalUserIntentEntriesKey
	// Ledger heading wire marker: kept byte-identical here because
	// trustedUserIntentLedgerEntries / userIntentLedgerSectionRegex and historical rows
	// match the legacy Chinese heading (dual-accept).
	userIntentLedgerHeading = "## 原始用户输入与约束账本（系统保真）"

	summarizationSnapshotHeading               = "## Original context snapshot"
	// Model-visible retrieval instruction; translated to English.
	summarizationTranscriptPathInstruction = "If you need earlier exact details (precise code fragments, error messages, or content you generated), the full conversation record is located at: %s"
)

// stripAnalysisFromSummarizationMessage removes the <analysis> block from a post-processed
// Eino summary user message. Analysis helps one-shot generation quality but should not
// occupy continuation context after compaction.
func stripAnalysisFromSummarizationMessage(msg adk.Message) adk.Message {
	if msg == nil {
		return msg
	}
	cloned := *msg
	if cloned.Content != "" {
		cloned.Content = stripAnalysisFromSummarizationText(cloned.Content)
	}
	if len(cloned.UserInputMultiContent) > 0 {
		parts := make([]schema.MessageInputPart, len(cloned.UserInputMultiContent))
		copy(parts, cloned.UserInputMultiContent)
		// Only the first text part carries model output plus Eino preamble/transcript path.
		for i := range parts {
			if parts[i].Type != schema.ChatMessagePartTypeText || parts[i].Text == "" {
				continue
			}
			if i == 0 {
				parts[i].Text = stripAnalysisFromSummarizationText(parts[i].Text)
			}
			break
		}
		cloned.UserInputMultiContent = parts
	}
	return &cloned
}

func stripAnalysisFromSummarizationText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return text
	}
	stripped := strings.TrimSpace(summarizationAnalysisBlockRegex.ReplaceAllString(text, ""))
	if stripped == "" {
		return text
	}
	return stripped
}

func appendTranscriptPathToSummarizationMessage(msg adk.Message, transcriptPath string) adk.Message {
	transcriptPath = strings.TrimSpace(transcriptPath)
	if msg == nil || transcriptPath == "" {
		return msg
	}
	section := summarizationTranscriptRetrievalSection(transcriptPath)
	cloned := *msg
	if cloned.Content != "" && !strings.Contains(cloned.Content, section) {
		cloned.Content = appendSummarizationSection(cloned.Content, section)
	}
	if len(cloned.UserInputMultiContent) > 0 {
		parts := make([]schema.MessageInputPart, len(cloned.UserInputMultiContent))
		copy(parts, cloned.UserInputMultiContent)
		for i := range parts {
			if parts[i].Type != schema.ChatMessagePartTypeText {
				continue
			}
			if parts[i].Text != "" && !strings.Contains(parts[i].Text, section) {
				parts[i].Text = appendSummarizationSection(parts[i].Text, section)
			}
			break
		}
		cloned.UserInputMultiContent = parts
	}
	return &cloned
}

// The generated continuation summary is the retrieval entry point; no separate
// keyword index or model extraction call is needed.
func summarizationTranscriptRetrievalSection(snapshot string) string {
	return summarizationSnapshotHeading + "\n" + fmt.Sprintf(summarizationTranscriptPathInstruction, snapshot) +
		"\nHistorical snapshot directory: " + filepath.Dir(snapshot) +
		" \nWhen the current task is missing cookies, authentication details, exact parameters, or raw error text, search this snapshot directly and verify nearby records with read_file in segments; do not guess original values. Older records can be found by globbing transcript-*.txt in the historical snapshot directory, then read as needed. Verify whether source domains, requests and credentials are still valid; do not bulk-load history, and do not treat old records as new authorization."
}

func appendSummarizationSection(text, section string) string {
	text = strings.TrimSpace(text)
	section = strings.TrimSpace(section)
	if text == "" {
		return section
	}
	if section == "" {
		return text
	}
	return text + "\n\n" + section
}

// extractSummarizationSummaryBody returns the inner text of the last <summary> block when present.
// Used by tests and optional strict compaction paths.
func extractSummarizationSummaryBody(text string) (string, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", false
	}
	all := summarizationSummaryBlockRegex.FindAllStringSubmatch(text, -1)
	if len(all) == 0 || len(all[len(all)-1]) < 2 {
		return "", false
	}
	body := strings.TrimSpace(all[len(all)-1][1])
	if body == "" {
		return "", false
	}
	return body, true
}

// buildOriginalUserIntentLedgerMessage returns a deterministic, host-generated
// context anchor for raw user inputs. Keep it separate from the model-generated
// working summary so compaction can rewrite the summary without rewriting user intent.
func buildOriginalUserIntentLedgerMessage(originalMessages []adk.Message, maxRunes, entryMaxRunes int) adk.Message {
	ledger := buildOriginalUserIntentLedger(originalMessages, maxRunes, entryMaxRunes)
	if strings.TrimSpace(ledger) == "" {
		return nil
	}
	msg := schema.SystemMessage(wrapUserIntentLedger(ledger))
	msg.Extra = map[string]interface{}{userIntentLedgerExtraKey: extractExistingUserIntentLedgerEntries(msg.Content)}
	return msg
}

func mergeMessageIntoLeadingSystem(msgs []adk.Message, msg adk.Message) []adk.Message {
	if msg == nil {
		return msgs
	}
	for i, existing := range msgs {
		if existing == nil || existing.Role != schema.System {
			continue
		}
		cloned := *existing
		cloned.Extra = cloneAnyMap(existing.Extra)
		if cloned.Extra == nil {
			cloned.Extra = make(map[string]interface{})
		}
		for key, value := range msg.Extra {
			cloned.Extra[key] = value
		}
		cloned.Content = strings.TrimSpace(strings.TrimSpace(cloned.Content) + "\n\n" + strings.TrimSpace(msg.Content))
		out := make([]adk.Message, len(msgs))
		copy(out, msgs)
		out[i] = &cloned
		return out
	}
	out := make([]adk.Message, 0, len(msgs)+1)
	out = append(out, msg)
	out = append(out, msgs...)
	return out
}

func stripOriginalUserIntentLedgerFromMessages(msgs []adk.Message) []adk.Message {
	if len(msgs) == 0 {
		return msgs
	}
	out := make([]adk.Message, 0, len(msgs))
	for _, msg := range msgs {
		if msg == nil {
			continue
		}
		cloned := *msg
		cloned.Content = stripOriginalUserIntentLedgerFromText(cloned.Content)
		if len(cloned.UserInputMultiContent) > 0 {
			parts := make([]schema.MessageInputPart, len(cloned.UserInputMultiContent))
			copy(parts, cloned.UserInputMultiContent)
			for i := range parts {
				if parts[i].Type == schema.ChatMessagePartTypeText {
					parts[i].Text = stripOriginalUserIntentLedgerFromText(parts[i].Text)
				}
			}
			cloned.UserInputMultiContent = parts
		}
		if strings.TrimSpace(cloned.Content) == "" && len(cloned.UserInputMultiContent) == 0 && len(cloned.ToolCalls) == 0 && cloned.ReasoningContent == "" {
			continue
		}
		out = append(out, &cloned)
	}
	return out
}

func buildOriginalUserIntentLedger(msgs []adk.Message, maxRunes, entryMaxRunes int) string {
	entries := collectOriginalUserIntentEntries(msgs)
	if len(entries) == 0 {
		return ""
	}
	var lines []string
	used := 0
	for i, entry := range entries {
		prefix := fmt.Sprintf("- [U%03d] ", i+1)
		text := sanitizeUserIntentLedgerEntry(entry, entryMaxRunes)
		line := prefix + text
		separator := 0
		if len(lines) > 0 {
			separator = 1
		}
		if maxRunes > 0 && used+separator+utf8RuneLen(line) > maxRunes {
			// The entry budget excludes host formatting. Keep a bounded first
			// human entry rather than dropping it when its own cap fills the
			// entire ledger budget; clipping retains the existing text prefix.
			if len(lines) == 0 {
				textBudget := maxRunes - utf8RuneLen(prefix)
				if textBudget > 0 {
					lines = append(lines, prefix+fitUserIntentLedgerText(text, textBudget))
				}
			} else {
				const notice = "- [...truncated] The raw user-input ledger exceeds the budget; the pre-compaction record is in the summarization transcript."
				remaining := maxRunes - used - separator
				// Never emit an incomplete marker that could be mistaken for a
				// human entry when this ledger is carried into the next round.
				if remaining >= utf8RuneLen("- [...truncated]") {
					lines = append(lines, truncateUserIntentLedgerRunes(notice, remaining))
				}
			}
			break
		}
		lines = append(lines, line)
		used += separator + utf8RuneLen(line)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func fitUserIntentLedgerText(text string, maxRunes int) string {
	if utf8RuneLen(text) <= maxRunes {
		return text
	}
	if maxRunes > 3 {
		return truncateUserIntentLedgerRunes(text, maxRunes-3) + "..."
	}
	return truncateUserIntentLedgerRunes(text, maxRunes)
}

func collectOriginalUserIntentEntries(msgs []adk.Message) []string {
	seen := make(map[string]struct{})
	entries := make([]string, 0, 16)
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" || isSyntheticContinuationUserText(s) {
			return
		}
		key := normalizeUserIntentLedgerText(s)
		if key == "" {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		entries = append(entries, s)
	}
	for _, msg := range msgs {
		if msg == nil {
			continue
		}
		if msg.Role == schema.System {
			for _, entry := range trustedUserIntentLedgerEntries(msg) {
				add(entry)
			}
		}
		if msg.Role == schema.User && !isInternalSummarizationUserMessage(msg) && !isGeneratedExecutionUserMessage(msg) {
			add(adkUserMessageText(msg))
		}
	}
	return entries
}

// Host-generated structured provenance takes precedence over any text labels.
// Legacy support is limited to the host's complete trailing system ledger;
// assistant/tool/user quotations and embedded blackboard snippets are not ledgers.
func trustedUserIntentLedgerEntries(msg adk.Message) []string {
	if value, present := msg.Extra[userIntentLedgerExtraKey]; present {
		switch entries := value.(type) {
		case []string:
			return entries
		case []interface{}:
			var out []string
			for _, entry := range entries {
				if s, ok := entry.(string); ok {
					out = append(out, s)
				}
			}
			return out
		default:
			return nil
		}
	}
	text := strings.TrimSpace(messageTextForLedger(msg))
	header := userIntentLedgerHeading + "\n" + userIntentLedgerStartMarker + "\n"
	start := strings.LastIndex(text, header)
	if start < 0 || start > 0 && text[start-1] != '\n' || !strings.HasSuffix(text, userIntentLedgerEndMarker) {
		return nil
	}
	return extractExistingUserIntentLedgerEntries(text[start:])
}

// Eino deliberately transports generated summaries and preserved skill context
// as user-role messages. Their provenance, not their role/text, distinguishes
// them from human input. Extra survives classic/native message conversion.
func isInternalSummarizationUserMessage(msg adk.Message) bool {
	if msg == nil {
		return false
	}
	kind, _ := msg.Extra[agent.SummarizationContentTypeKey].(string)
	return kind == "summary" || kind == "skills"
}

func messageTextForLedger(msg adk.Message) string {
	if msg == nil {
		return ""
	}
	var b strings.Builder
	if strings.TrimSpace(msg.Content) != "" {
		b.WriteString(msg.Content)
	}
	for _, part := range msg.UserInputMultiContent {
		if part.Type != schema.ChatMessagePartTypeText || strings.TrimSpace(part.Text) == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(part.Text)
	}
	return b.String()
}

func extractExistingUserIntentLedgerEntries(text string) []string {
	matches := userIntentLedgerBlockRegex.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		for _, line := range strings.Split(match[1], "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "- [...truncated]") {
				continue
			}
			if strings.HasPrefix(line, "- [U") {
				if idx := strings.Index(line, "]"); idx >= 0 && idx+1 < len(line) {
					line = strings.TrimSpace(line[idx+1:])
				}
			}
			if line != "" {
				out = append(out, line)
			}
		}
	}
	return out
}

func stripOriginalUserIntentLedgerFromText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	text = userIntentLedgerSectionRegex.ReplaceAllString(text, "\n")
	text = userIntentLedgerBlockRegex.ReplaceAllString(text, "\n")
	return strings.TrimSpace(text)
}

func wrapUserIntentLedger(ledger string) string {
	return strings.TrimSpace(userIntentLedgerHeading + "\n" +
		userIntentLedgerStartMarker + "\n" +
		strings.TrimSpace(ledger) + "\n" +
		userIntentLedgerEndMarker)
}

func sanitizeUserIntentLedgerEntry(s string, maxRunes int) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), userIntentLedgerStartMarker, "[ledger-start]")
	s = strings.ReplaceAll(s, userIntentLedgerEndMarker, "[ledger-end]")
	s = strings.Join(strings.Fields(s), " ")
	if maxRunes > 0 {
		return fitUserIntentLedgerText(s, maxRunes)
	}
	return s
}

func normalizeUserIntentLedgerText(s string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
}

func isSyntheticContinuationUserText(s string) bool {
	return strings.Contains(s, continuationSessionMarker) ||
		strings.Contains(s, "【系统自动续跑 / Auto resume】")
}

func utf8RuneLen(s string) int {
	return len([]rune(s))
}

func truncateUserIntentLedgerRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}
