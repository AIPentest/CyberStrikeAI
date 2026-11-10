package project

import "strings"

// FactIndexSectionHeading The readable-title-line prefix of the blackboard index (kept inside the block, for the agent to read).
const FactIndexSectionHeading = "## Project blackboard index"

// FactIndexSectionStartMarker / EndMarker: HTML Comment boundaries, for programmatic replacement; they carry no instruction semantics for the model.
const (
	FactIndexSectionStartMarker = "<!-- fact-index-start -->"
	FactIndexSectionEndMarker   = "<!-- fact-index-end -->"
)

// ReplaceFactIndexSection Replaces the existing project-blackboard index section in content with freshIndex.
// freshIndex must be the complete output of BuildFactIndexBlock. When the start/end HTML comments are missing it returns (_, false).
func ReplaceFactIndexSection(content, freshIndex string) (string, bool) {
	freshIndex = strings.TrimSpace(freshIndex)
	if freshIndex == "" {
		return content, false
	}
	start, ok := factIndexSectionStart(content)
	if !ok {
		return content, false
	}
	end, ok := factIndexSectionEnd(content, start)
	if !ok || end <= start {
		return content, false
	}
	return content[:start] + freshIndex + content[end:], true
}

// RemoveFactIndexSection removes only a complete host-delimited index after a
// successful build returns no block (e.g. a budget too small for framing).
func RemoveFactIndexSection(content string) (string, bool) {
	start, ok := factIndexSectionStart(content)
	if !ok {
		return content, false
	}
	end, ok := factIndexSectionEnd(content, start)
	if !ok || end <= start {
		return content, false
	}
	return content[:start] + content[end:], true
}

// wrapFactIndexBlock Adds unified start/end HTML comment boundaries to the body of BuildFactIndexBlock.
func wrapFactIndexBlock(content string) string {
	content = strings.TrimSpace(content)
	return FactIndexSectionStartMarker + "\n" + content + "\n" + FactIndexSectionEndMarker + "\n"
}

// factIndexSafeText Removes host delimiters from fact text. The index block locates its start/end literally, and if the content
// contains a comment of the same name it would close the block early, pushing the rest of the index content outside the block in the system prompt.
func factIndexSafeText(s string) string {
	s = strings.ReplaceAll(s, FactIndexSectionStartMarker, "")
	return strings.ReplaceAll(s, FactIndexSectionEndMarker, "")
}

func factIndexSectionStart(content string) (int, bool) {
	idx := strings.Index(content, FactIndexSectionStartMarker)
	if idx < 0 {
		return 0, false
	}
	return idx, true
}

func factIndexSectionEnd(content string, start int) (int, bool) {
	if start < 0 || start >= len(content) {
		return 0, false
	}
	tail := content[start:]
	idx := strings.LastIndex(tail, FactIndexSectionEndMarker)
	if idx < 0 {
		return 0, false
	}
	return start + idx + len(FactIndexSectionEndMarker), true
}
