package skillpackage

import (
	"fmt"
	"regexp"
	"strings"
)

var reH2 = regexp.MustCompile(`(?m)^##\s+(.+)$`)

const summaryContentRunes = 6000

type markdownSection struct {
	Heading string
	Title   string
	Content string
}

func splitMarkdownSections(body string) []markdownSection {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil
	}
	idxs := reH2.FindAllStringIndex(body, -1)
	titles := reH2.FindAllStringSubmatch(body, -1)
	if len(idxs) == 0 {
		return []markdownSection{{
			Heading: "",
			Title:   "_body",
			Content: body,
		}}
	}
	var out []markdownSection
	for i := range idxs {
		title := strings.TrimSpace(titles[i][1])
		start := idxs[i][0]
		end := len(body)
		if i+1 < len(idxs) {
			end = idxs[i+1][0]
		}
		chunk := strings.TrimSpace(body[start:end])
		out = append(out, markdownSection{
			Heading: "## " + title,
			Title:   title,
			Content: chunk,
		})
	}
	return out
}

func deriveSections(body string) []SkillSection {
	md := splitMarkdownSections(body)
	out := make([]SkillSection, 0, len(md))
	for _, ms := range md {
		if ms.Title == "_body" {
			continue
		}
		out = append(out, SkillSection{
			ID:      slugifySectionID(ms.Title),
			Title:   ms.Title,
			Heading: ms.Heading,
			Level:   2,
		})
	}
	return out
}

func slugifySectionID(title string) string {
	title = strings.TrimSpace(strings.ToLower(title))
	if title == "" {
		return "section"
	}
	var b strings.Builder
	for _, r := range title {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ', r == '-', r == '_':
			b.WriteRune('-')
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		return "section"
	}
	return s
}

func findSectionContent(sections []markdownSection, sec string) string {
	sec = strings.TrimSpace(sec)
	if sec == "" {
		return ""
	}
	want := strings.ToLower(sec)
	for _, s := range sections {
		if strings.EqualFold(slugifySectionID(s.Title), want) || strings.EqualFold(s.Title, sec) {
			return s.Content
		}
		if strings.EqualFold(strings.ReplaceAll(s.Title, " ", "-"), want) {
			return s.Content
		}
	}
	return ""
}

func buildSummaryMarkdown(name, description string, tags []string, scripts []SkillScriptInfo, sections []SkillSection, body string) string {
	var b strings.Builder
	if description != "" {
		b.WriteString(description)
		b.WriteString("\n\n")
	}
	if len(tags) > 0 {
		b.WriteString("**Tags**: ")
		b.WriteString(strings.Join(tags, ", "))
		b.WriteString("\n\n")
	}
	if len(scripts) > 0 {
		b.WriteString("### Bundled scripts\n\n")
		for _, sc := range scripts {
			line := "- `" + sc.RelPath + "`"
			if sc.Description != "" {
				line += " — " + sc.Description
			}
			b.WriteString(line)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	if len(sections) > 0 {
		b.WriteString("### Sections\n\n")
		for _, sec := range sections {
			line := "- **" + sec.ID + "**"
			if sec.Title != "" && sec.Title != sec.ID {
				line += ": " + sec.Title
			}
			b.WriteString(line)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	mdSecs := splitMarkdownSections(body)
	preview := body
	if len(mdSecs) > 0 && mdSecs[0].Title != "_body" {
		preview = mdSecs[0].Content
	}
	b.WriteString("### Preview (SKILL.md)\n\n")
	b.WriteString(truncateRunes(strings.TrimSpace(preview), summaryContentRunes))
	b.WriteString("\n\n---\n\n_(Summary for admin UI. Agents use Agent `skill` tool for full SKILL.md progressive loading.)_")
	if name != "" {
		b.WriteString(fmt.Sprintf("\n\n_Skill name: %s_", name))
	}
	return b.String()
}

func truncateRunes(s string, max int) string {
	if max <= 0 || s == "" {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}
