package workflowpackage

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
	"time"
)

func testDocument() Document {
	return Document{
		ID:          "web-src-hunting",
		Name:        "Web SRC Bug hunting",
		Description: "A reconnaissance and vulnerability-candidate flow for SRC web assets",
		Version:     18,
		Enabled:     true,
		GraphJSON:   `{"nodes":[{"id":"start-1","type":"start","label":"Start","position":{"x":0,"y":0},"config":{}},{"id":"out-1","type":"output","label":"Output","position":{"x":0,"y":120},"config":{"output_key":"result","source_binding":{"from":"inputs","field":"message"}}}],"edges":[{"id":"e1","source":"start-1","target":"out-1"}],"config":{"schema_version":1}}`,
		UpdatedAt:   time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC),
	}
}

func TestExportIsDeterministicAndSelfDescribing(t *testing.T) {
	first, firstMeta, err := Export(testDocument())
	if err != nil {
		t.Fatalf("first export: %v", err)
	}
	second, secondMeta, err := Export(testDocument())
	if err != nil {
		t.Fatalf("second export: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("identical document must produce byte-identical package")
	}
	if firstMeta.PackageHash != secondMeta.PackageHash || !strings.HasPrefix(firstMeta.PackageHash, "sha256:") {
		t.Fatalf("unexpected deterministic package hash: %#v / %#v", firstMeta, secondMeta)
	}

	zr, err := zip.NewReader(bytes.NewReader(first), int64(len(first)))
	if err != nil {
		t.Fatalf("open package: %v", err)
	}
	if len(zr.File) != 3 {
		t.Fatalf("zip entry count = %d, want 3", len(zr.File))
	}
	wantNames := []string{"checksums.sha256", "manifest.json", "workflows/web-src-hunting.json"}
	for i, f := range zr.File {
		if f.Name != wantNames[i] {
			t.Fatalf("entry %d = %q, want %q", i, f.Name, wantNames[i])
		}
	}
	if firstMeta.SourceRevision != 18 {
		t.Fatalf("source revision = %d, want 18", firstMeta.SourceRevision)
	}
	if !strings.HasPrefix(firstMeta.ContentHash, "sha256:") || !strings.HasPrefix(firstMeta.GraphHash, "sha256:") {
		t.Fatalf("content/graph hashes must be sha256: %#v", firstMeta)
	}
}
