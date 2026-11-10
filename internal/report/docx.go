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
	return Template{Title: "Security Check Report", Introduction: "This report summarizes the check results recorded in the current session, for manual review and remediation tracking.", IncludeSummary: true, IncludeEvidence: true, IncludeRemediation: true}
}
func (t Template) Validate() error {
	if strings.TrimSpace(t.Title) == "" {
		return fmt.Errorf("The report title cannot be empty")
	}
	for _, f := range []struct {
		name, value string
		limit       int
	}{{"Title", t.Title, 200}, {"Organization", t.Organization, 500}, {"Scope", t.Scope, 10000}, {"Section description", t.Introduction, 10000}} {
		if !utf8.ValidString(f.value) || utf8.RuneCountInString(f.value) > f.limit {
			return fmt.Errorf("%s Exceeds the length limit (%d characters)", f.name, f.limit)
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
		return nil, fmt.Errorf("The report exceeds the 2000-finding limit")
	}
	size := len(s.Summary)
	for _, v := range s.Findings {
		if v == nil {
			return nil, fmt.Errorf("The finding record cannot be empty")
		}
		for _, text := range []string{v.ID, v.Title, v.Description, v.Severity, v.Status, v.Type, v.Target, v.Preconditions, v.ReproSteps, v.Evidence, v.Impact, v.Recommendation, v.RetestNotes, v.RetestLog} {
			size += len(text)
		}
	}
	if size > 8*1024*1024 {
		return nil, fmt.Errorf("The report body exceeds the 8 MiB limit; please narrow the scope")
	}
	var body strings.Builder
	p := func(style, text string) {
		for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
			body.WriteString(`<w:p><w:pPr><w:pStyle w:val="` + style + `"/></w:pPr><w:r><w:t xml:space="preserve">` + escaped(line) + `</w:t></w:r></w:p>`)
		}
	}
	field := func(label, value string) {
		if strings.TrimSpace(value) == "" {
			value = "Not recorded"
		}
		p("Normal", label+": "+value)
	}
	p("Title", t.Title)
	if t.Organization != "" {
		field("Organization", t.Organization)
	}
	field("Generated at", s.GeneratedAt.Format("2006-01-02 15:04:05 -07:00"))
	if s.ConversationID != "" {
		field("Session", s.ConversationTitle+" ("+s.ConversationID+")")
	}
	if s.RunID != "" {
		field("Run ID", s.RunID)
	}
	p("Heading1", "Scope and description")
	field("Scope", t.Scope)
	p("Normal", t.Introduction)
	if len(s.Findings) != 1 {
		p("Normal", "This report preserves the record state at generation time. An unrecorded vulnerability does not mean the target is secure; the record state, evidence sufficiency, and coverage require manual review.")
		p("Heading1", "Risk statistics")
		counts := map[string]int{}
		for _, v := range s.Findings {
			counts[v.Severity]++
		}
		field("Total findings", fmt.Sprint(len(s.Findings)))
		for _, severity := range []string{"critical", "high", "medium", "low", "info"} {
			field(severity, fmt.Sprint(counts[severity]))
		}
		p("Normal", "The statistics include all records within the current filter scope; false positives, ignored, and pending records should be judged together with each item's status and should not all be treated as verified vulnerabilities.")
	}
	if t.IncludeSummary {
		p("Heading1", "Final reply (original text, pending manual review)")
		field("Session final reply", s.Summary)
	}
	if len(s.Findings) == 1 {
		p("Heading1", "Vulnerability details")
	} else {
		p("Heading1", "Finding details")
	}
	if len(s.Findings) == 0 {
		p("Normal", "There are no recorded vulnerabilities within the current scope; this is not a security guarantee.")
	}
	for i, v := range s.Findings {
		if len(s.Findings) != 1 {
			p("Heading2", fmt.Sprintf("%d. %s", i+1, v.Title))
		} else {
			p("Heading2", v.Title)
		}
		for _, f := range [][2]string{{"Record ID", v.ID}, {"Severity", v.Severity}, {"Record status", v.Status}, {"Type", v.Type}, {"Target", v.Target}, {"Description", v.Description}, {"Preconditions", v.Preconditions}, {"Impact", v.Impact}} {
			field(f[0], f[1])
		}
		if t.IncludeEvidence {
			field("Reproduction steps", v.ReproSteps)
			field("Evidence / POC", v.Evidence)
		}
		if t.IncludeRemediation {
			field("Remediation advice", v.Recommendation)
			field("Retest method", v.RetestNotes)
			field("Retest record", v.RetestLog)
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
