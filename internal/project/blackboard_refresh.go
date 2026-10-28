package project

import "strings"

// FactIndexSectionHeading 黑板索引可读标题行前缀（块内保留，供 Agent 阅读）。
const FactIndexSectionHeading = "## Project blackboard index"

// FactIndexSectionStartMarker / EndMarker：HTML 注释边界，供程序化替换；对模型无指令语义。
const (
	FactIndexSectionStartMarker = "<!-- fact-index-start -->"
	FactIndexSectionEndMarker   = "<!-- fact-index-end -->"
)

// ReplaceFactIndexSection 用 freshIndex 替换 content 中已有的项目黑板索引段。
// freshIndex 须为 BuildFactIndexBlock 的完整输出。起止 HTML 注释缺失时返回 (_, false)。
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

// wrapFactIndexBlock 为 BuildFactIndexBlock 正文加上统一起止 HTML 注释边界。
func wrapFactIndexBlock(content string) string {
	content = strings.TrimSpace(content)
	return FactIndexSectionStartMarker + "\n" + content + "\n" + FactIndexSectionEndMarker + "\n"
}

// factIndexSafeText 去掉事实文本里的宿主定界符。索引块按字面量定位起止，内容里
// 出现同名注释会提前闭合块，把其余索引内容甩到系统提示的块外。
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
