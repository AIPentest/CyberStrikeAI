package project

import (
	"fmt"
	"regexp"
	"strings"

	"cyberstrike-ai/internal/database"
)

var (
	bodyDepFactLine   = regexp.MustCompile(`(?im)^[\s\-*]*依赖事实\s*[:：]\s*([a-zA-Z0-9][a-zA-Z0-9._/-]*)`)
	bodyRelFactLine   = regexp.MustCompile(`(?im)^[\s\-*]*相关\s*fact_key\s*[:：]\s*([a-zA-Z0-9][a-zA-Z0-9._/-]*)`)
	// bodyAssocSection / bodySyncLinksHead match markers stored verbatim inside
	// existing fact bodies (legacy Chinese). Keep them byte-identical; the English
	// aliases below are also accepted so newly written English bodies parse too.
	bodyAssocSection      = regexp.MustCompile(`(?im)^##\s*(关联|Related links)\s*$`)
	bodySyncLinksHead     = "结构化关系边（自动同步）"
	bodySyncLinksHeadEn   = "structured relation edges (auto-sync)"
)

// ParseLinksFromBody 从 body「关联」段落解析 from 语义的关系边（无显式 links 时的兜底）。
func ParseLinksFromBody(body string) []database.ProjectFactEdgeFromInput {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil
	}
	seen := map[string]struct{}{}
	var out []database.ProjectFactEdgeFromInput
	add := func(key, edgeType string) {
		key = strings.TrimSpace(key)
		if key == "" {
			return
		}
		if err := database.ValidateFactKey(key); err != nil {
			return
		}
		sig := edgeType + "\x00" + key
		if _, ok := seen[sig]; ok {
			return
		}
		seen[sig] = struct{}{}
		out = append(out, database.ProjectFactEdgeFromInput{From: key, Type: edgeType})
	}
	for _, m := range bodyDepFactLine.FindAllStringSubmatch(body, -1) {
		if len(m) > 1 {
			add(m[1], "depends_on")
		}
	}
	for _, m := range bodyRelFactLine.FindAllStringSubmatch(body, -1) {
		if len(m) > 1 {
			add(m[1], "supports")
		}
	}
	// 自动同步块：type: key
	syncBlock := extractBodySyncLinksBlock(body)
	for _, line := range strings.Split(syncBlock, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "-"))
		if line == "" {
			continue
		}
		edgeType, source, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		edgeType = strings.TrimSpace(edgeType)
		source = strings.TrimSpace(source)
		if err := database.ValidateProjectFactEdgeType(edgeType); err != nil {
			continue
		}
		add(source, edgeType)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func extractBodySyncLinksBlock(body string) string {
	lines := strings.Split(body, "\n")
	var b strings.Builder
	inAssoc := false
	inSync := false
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if bodyAssocSection.MatchString(trim) {
			inAssoc = true
			inSync = false
			continue
		}
		if inAssoc && strings.HasPrefix(trim, "## ") && !bodyAssocSection.MatchString(trim) {
			break
		}
		if inAssoc && (strings.Contains(trim, bodySyncLinksHead) || strings.Contains(trim, bodySyncLinksHeadEn)) {
			inSync = true
			continue
		}
		if inSync {
			if trim == "" || strings.HasPrefix(trim, "-") || strings.Contains(trim, ":") {
				if strings.HasPrefix(trim, "-") || (strings.Contains(trim, ":") && !strings.Contains(trim, "related_vulnerability")) {
					b.WriteString(trim)
					b.WriteByte('\n')
				}
			} else if strings.HasPrefix(trim, "##") {
				break
			}
		}
	}
	return b.String()
}

// SyncBodyLinksSection 将入边镜像写入 body 的「关联」段（人读用；结构化以 links 为准）。
func SyncBodyLinksSection(body string, edges []*database.ProjectFactEdge) string {
	body = strings.TrimSpace(body)
	block := formatBodySyncLinksBlock(edges)
	if block == "" {
		return body
	}
	if body == "" {
		return "## 关联\n" + block
	}
	lines := strings.Split(body, "\n")
	var out []string
	inAssoc := false
	replaced := false
	for i := 0; i < len(lines); i++ {
		trim := strings.TrimSpace(lines[i])
		if bodyAssocSection.MatchString(trim) {
			inAssoc = true
			out = append(out, lines[i])
			// 跳过旧同步块
			j := i + 1
			for j < len(lines) {
				t := strings.TrimSpace(lines[j])
				if strings.HasPrefix(t, "## ") {
					break
				}
				if strings.Contains(t, bodySyncLinksHead) {
					for j < len(lines) {
						t2 := strings.TrimSpace(lines[j])
						if t2 != "" && !strings.HasPrefix(t2, "-") && !strings.Contains(t2, ":") && !strings.Contains(t2, bodySyncLinksHead) {
							if strings.HasPrefix(t2, "##") {
								break
							}
						}
						j++
						if j < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[j]), "## ") {
							break
						}
						if j >= len(lines) {
							break
						}
						if j > i+1 && strings.TrimSpace(lines[j-1]) == "" && strings.HasPrefix(strings.TrimSpace(lines[j]), "## ") {
							break
						}
					}
					break
				}
				j++
			}
			out = append(out, block)
			i = j - 1
			replaced = true
			continue
		}
		out = append(out, lines[i])
	}
	if !replaced {
		if !inAssoc {
			out = append(out, "", "## 关联", block)
		} else {
			out = append(out, block)
		}
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func formatBodySyncLinksBlock(edges []*database.ProjectFactEdge) string {
	// get_project_fact returns this body verbatim without scope filtering, so
	// conversation-scoped working memory keys must not be mirrored into a shared fact.
	var b strings.Builder
	b.WriteString("- ")
	b.WriteString(bodySyncLinksHead)
	b.WriteString(":")
	written := 0
	for _, e := range edges {
		if database.IsContextCheckpointKey(e.SourceFactKey) {
			continue
		}
		b.WriteString(fmt.Sprintf("\n  - %s: %s", e.EdgeType, e.SourceFactKey))
		written++
	}
	if written == 0 {
		b.WriteString("\n  (none)")
	}
	return b.String()
}

// ResolveFactLinksForUpsert 合并显式 links、links_text 与 body 解析结果。
func ResolveFactLinksForUpsert(explicit []database.ProjectFactEdgeFromInput, linksText *string, body string, explicitSet bool) ([]database.ProjectFactEdgeFromInput, bool, error) {
	if explicitSet {
		if len(explicit) > 0 {
			return explicit, true, nil
		}
		if linksText != nil {
			parsed, err := ParseFactLinksText(*linksText)
			if err != nil {
				return nil, true, err
			}
			if parsed == nil {
				return []database.ProjectFactEdgeFromInput{}, true, nil
			}
			return parsed, true, nil
		}
		return []database.ProjectFactEdgeFromInput{}, true, nil
	}
	if parsed := ParseLinksFromBody(body); len(parsed) > 0 {
		return parsed, true, nil
	}
	return nil, false, nil
}

// MergeLinkFromInputsUnique 合并多组 from 入边输入并去重。
func MergeLinkFromInputsUnique(groups ...[]database.ProjectFactEdgeFromInput) []database.ProjectFactEdgeFromInput {
	seen := map[string]struct{}{}
	var out []database.ProjectFactEdgeFromInput
	for _, g := range groups {
		for _, in := range g {
			sig := in.Type + "\x00" + in.From
			if _, ok := seen[sig]; ok {
				continue
			}
			if err := database.ValidateProjectFactEdgeType(in.Type); err != nil {
				continue
			}
			if err := database.ValidateFactKey(in.From); err != nil {
				continue
			}
			seen[sig] = struct{}{}
			out = append(out, in)
		}
	}
	return out
}

// MergeLinkInputsUnique 合并多组 link 输入并去重（内部出边写入用）。
func MergeLinkInputsUnique(groups ...[]database.ProjectFactEdgeInput) []database.ProjectFactEdgeInput {
	seen := map[string]struct{}{}
	var out []database.ProjectFactEdgeInput
	for _, g := range groups {
		for _, in := range g {
			sig := in.Type + "\x00" + in.To
			if _, ok := seen[sig]; ok {
				continue
			}
			if err := database.ValidateProjectFactEdgeType(in.Type); err != nil {
				continue
			}
			if err := database.ValidateFactKey(in.To); err != nil {
				continue
			}
			seen[sig] = struct{}{}
			out = append(out, in)
		}
	}
	return out
}
