// Package report renders evidence already collected by a conversation into Word.
// It never runs tools, calls an LLM, or interprets template content as code.
package report

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"cyberstrike-ai/internal/database"
)

const MIME = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"

// Template is intentionally structured: text is literal, not executable markup.
type Template struct {
	Title              string `json:"title"`
	Organization       string `json:"organization"`
	Scope              string `json:"scope"`
	Introduction       string `json:"introduction"`
	IncludeSummary     bool   `json:"include_summary"`
	IncludeEvidence    bool   `json:"include_evidence"`
	IncludeRemediation bool   `json:"include_remediation"`
}

func DefaultTemplate() Template {
	return Template{Title: "安全检查报告", Introduction: "本报告汇总当前会话已记录的检查结果，供人工复核与修复跟踪。", IncludeSummary: true, IncludeEvidence: true, IncludeRemediation: true}
}
func (t Template) Validate() error {
	if strings.TrimSpace(t.Title) == "" {
		return fmt.Errorf("报告标题不能为空")
	}
	for _, f := range []struct {
		name, value string
		limit       int
	}{{"标题", t.Title, 200}, {"单位", t.Organization, 500}, {"检查范围", t.Scope, 10000}, {"章节说明", t.Introduction, 10000}} {
		if !utf8.ValidString(f.value) || utf8.RuneCountInString(f.value) > f.limit {
			return fmt.Errorf("%s超出长度限制（%d 字）", f.name, f.limit)
		}
	}
	return nil
}

type Snapshot struct {
	ConversationID, ConversationTitle, RunID, Summary string
	GeneratedAt                                       time.Time
	Findings                                          []*database.Vulnerability
}

func escaped(s string) string {
	// XML 1.0 excludes control characters sometimes found in tool output.
	s = strings.Map(func(r rune) rune {
		if r == 9 || r == 10 || r == 13 || r >= 0x20 && r <= 0xD7FF || r >= 0xE000 && r <= 0xFFFD || r >= 0x10000 && r <= 0x10FFFF {
			return r
		}
		return -1
	}, s)
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// Render creates a standalone OOXML document with A4 pages and Chinese fonts.
func Render(t Template, s Snapshot) ([]byte, error) {
	if err := t.Validate(); err != nil {
		return nil, err
	}
	if len(s.Findings) > 2000 {
		return nil, fmt.Errorf("报告超过 2000 条发现限制")
	}
	size := len(s.Summary)
	for _, v := range s.Findings {
		if v == nil {
			return nil, fmt.Errorf("发现记录不能为空")
		}
		for _, text := range []string{v.ID, v.Title, v.Description, v.Severity, v.Status, v.Type, v.Target, v.Preconditions, v.ReproSteps, v.Evidence, v.Impact, v.Recommendation, v.RetestNotes, v.RetestLog} {
			size += len(text)
		}
	}
	if size > 8*1024*1024 {
		return nil, fmt.Errorf("报告正文超过 8 MiB 限制，请缩小范围")
	}
	var body strings.Builder
	p := func(style, text string) {
		for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
			body.WriteString(`<w:p><w:pPr><w:pStyle w:val="` + style + `"/></w:pPr><w:r><w:t xml:space="preserve">` + escaped(line) + `</w:t></w:r></w:p>`)
		}
	}
	field := func(label, value string) {
		if strings.TrimSpace(value) == "" {
			value = "未记录"
		}
		p("Normal", label+"："+value)
	}
	p("Title", t.Title)
	if t.Organization != "" {
		field("单位", t.Organization)
	}
	field("生成时间", s.GeneratedAt.Format("2006-01-02 15:04:05 -07:00"))
	if s.ConversationID != "" {
		field("会话", s.ConversationTitle+"（"+s.ConversationID+"）")
	}
	if s.RunID != "" {
		field("运行 ID", s.RunID)
	}
	p("Heading1", "检查范围与说明")
	field("检查范围", t.Scope)
	p("Normal", t.Introduction)
	if len(s.Findings) != 1 {
		p("Normal", "本报告保留生成时的记录状态。未记录漏洞不代表目标安全，记录状态、证据充分性及覆盖范围需人工复核。")
		p("Heading1", "风险统计")
		counts := map[string]int{}
		for _, v := range s.Findings {
			counts[v.Severity]++
		}
		field("发现总数", fmt.Sprint(len(s.Findings)))
		for _, severity := range []string{"critical", "high", "medium", "low", "info"} {
			field(severity, fmt.Sprint(counts[severity]))
		}
		p("Normal", "统计包含当前筛选范围内的全部记录；误报、已忽略与待确认记录请结合各项状态判断，不应全部视为已验证漏洞。")
	}
	if t.IncludeSummary {
		p("Heading1", "最终回复（原文，待人工复核）")
		field("会话最终回复", s.Summary)
	}
	if len(s.Findings) == 1 {
		p("Heading1", "漏洞详情")
	} else {
		p("Heading1", "发现详情")
	}
	if len(s.Findings) == 0 {
		p("Normal", "当前范围内没有已记录的漏洞；这不构成安全保证。")
	}
	for i, v := range s.Findings {
		if len(s.Findings) != 1 {
			p("Heading2", fmt.Sprintf("%d. %s", i+1, v.Title))
		} else {
			p("Heading2", v.Title)
		}
		for _, f := range [][2]string{{"记录 ID", v.ID}, {"严重程度", v.Severity}, {"记录状态", v.Status}, {"类型", v.Type}, {"目标", v.Target}, {"描述", v.Description}, {"前置条件", v.Preconditions}, {"影响", v.Impact}} {
			field(f[0], f[1])
		}
		if t.IncludeEvidence {
			field("复现步骤", v.ReproSteps)
			field("证据 / POC", v.Evidence)
		}
		if t.IncludeRemediation {
			field("修复建议", v.Recommendation)
			field("复测方式", v.RetestNotes)
			field("复测记录", v.RetestLog)
		}
	}
	body.WriteString(`<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440"/></w:sectPr>`)
	doc := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` + body.String() + `</w:body></w:document>`
	files := []struct{ name, content string }{
		{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/><Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/></Types>`},
		{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`},
		{"word/_rels/document.xml.rels", `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>`},
		{"word/document.xml", doc},
		{"word/styles.xml", `<?xml version="1.0" encoding="UTF-8"?><w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="Calibri" w:hAnsi="Calibri" w:eastAsia="Microsoft YaHei"/><w:sz w:val="22"/></w:rPr></w:rPrDefault><w:pPrDefault><w:pPr><w:spacing w:after="140" w:line="320" w:lineRule="auto"/></w:pPr></w:pPrDefault></w:docDefaults><w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/></w:style><w:style w:type="paragraph" w:styleId="Title"><w:name w:val="Title"/><w:basedOn w:val="Normal"/><w:pPr><w:keepNext/><w:spacing w:after="360"/></w:pPr><w:rPr><w:b/><w:sz w:val="40"/><w:color w:val="000000"/></w:rPr></w:style><w:style w:type="paragraph" w:styleId="Heading1"><w:name w:val="heading 1"/><w:basedOn w:val="Normal"/><w:pPr><w:keepNext/><w:spacing w:before="300" w:after="160"/><w:outlineLvl w:val="0"/></w:pPr><w:rPr><w:b/><w:sz w:val="30"/><w:color w:val="17365D"/></w:rPr></w:style><w:style w:type="paragraph" w:styleId="Heading2"><w:name w:val="heading 2"/><w:basedOn w:val="Normal"/><w:pPr><w:keepNext/><w:spacing w:before="220"/><w:outlineLvl w:val="1"/></w:pPr><w:rPr><w:b/><w:sz w:val="25"/></w:rPr></w:style></w:styles>`},
	}
	var out bytes.Buffer
	z := zip.NewWriter(&out)
	for _, f := range files {
		w, err := z.Create(f.name)
		if err != nil {
			return nil, err
		}
		if _, err = w.Write([]byte(f.content)); err != nil {
			return nil, err
		}
	}
	if err := z.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
