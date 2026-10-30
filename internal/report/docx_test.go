package report

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"strings"
	"testing"
	"time"

	"cyberstrike-ai/internal/database"
)

func TestWordReportEscapesEvidenceAndHonorsSections(t *testing.T) {
	template := DefaultTemplate()
	template.Title = "中文报告 & <客户>"
	template.IncludeRemediation = false
	v := &database.Vulnerability{Title: "待确认发现", Severity: "high", Status: "false_positive", Evidence: "<script>&\x00\nGET /?x=1&y=2", Recommendation: "must-be-omitted"}
	data, err := Render(template, Snapshot{Summary: "原始回复", GeneratedAt: time.Now(), Findings: []*database.Vulnerability{v}})
	if err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	parts := map[string]string{}
	for _, f := range z.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		decoder := xml.NewDecoder(bytes.NewReader(b))
		for {
			_, err := decoder.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("invalid XML in %s: %v", f.Name, err)
			}
		}
		parts[f.Name] = string(b)
	}
	for _, name := range []string{"[Content_Types].xml", "_rels/.rels", "word/document.xml", "word/styles.xml", "word/_rels/document.xml.rels"} {
		if parts[name] == "" {
			t.Fatalf("missing part %s", name)
		}
	}
	doc := parts["word/document.xml"]
	for _, text := range []string{"中文报告 &amp; &lt;客户&gt;", "&lt;script&gt;&amp;", "false_positive", "原始回复"} {
		if !strings.Contains(doc, text) {
			t.Errorf("missing %q", text)
		}
	}
	if strings.Contains(doc, "must-be-omitted") || strings.ContainsRune(doc, 0) {
		t.Fatal("excluded content or invalid control character retained")
	}
	template.IncludeEvidence = false
	data, err = Render(template, Snapshot{Findings: []*database.Vulnerability{v}})
	if err != nil {
		t.Fatal(err)
	}
	z, _ = zip.NewReader(bytes.NewReader(data), int64(len(data)))
	for _, f := range z.File {
		if f.Name == "word/document.xml" {
			r, _ := f.Open()
			b, _ := io.ReadAll(r)
			r.Close()
			if bytes.Contains(b, []byte("script")) {
				t.Fatal("evidence not excluded")
			}
		}
	}
}
func TestWordReportRejectsOversizedInput(t *testing.T) {
	if _, err := Render(DefaultTemplate(), Snapshot{Summary: strings.Repeat("x", 8*1024*1024+1)}); err == nil {
		t.Fatal("oversized report accepted")
	}
	template := DefaultTemplate()
	template.Title = ""
	if _, err := Render(template, Snapshot{}); err == nil {
		t.Fatal("empty title accepted")
	}
}
