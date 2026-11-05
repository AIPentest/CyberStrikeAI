package project

import (
	"path/filepath"
	"strings"
	"testing"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"

	"go.uber.org/zap"
)

func sampleFactIndexWithFacts(projectLabel, summary string) string {
	return wrapFactIndexBlock("## Project blackboard index (project: " + projectLabel + ", id: x)\n" +
		"- [target/a] target — " + summary + " (tentative)\n" +
		factIndexFooterGetDetail + "\n" +
		factIndexFooterWriteHint)
}

func TestReplaceFactIndexSection(t *testing.T) {
	t.Parallel()
	oldIndex := sampleFactIndexWithFacts("p1", "old summary")
	newIndex := sampleFactIndexWithFacts("p1", "new summary")

	t.Run("replaces index before next section", func(t *testing.T) {
		content := "你是助手\n\n" + oldIndex + "\n\n## 图片分析\n看截图"
		out, ok := ReplaceFactIndexSection(content, newIndex)
		if !ok {
			t.Fatal("expected replacement")
		}
		if strings.Contains(out, "old summary") {
			t.Fatalf("old index should be gone: %q", out)
		}
		if !strings.Contains(out, "new summary") || !strings.Contains(out, "## 图片分析") {
			t.Fatalf("expected new index and preserved vision section: %q", out)
		}
		if strings.Count(out, FactIndexSectionStartMarker) != 1 || strings.Count(out, FactIndexSectionEndMarker) != 1 {
			t.Fatalf("expected exactly one start/end marker pair: %q", out)
		}
	})

	t.Run("replaces index at end", func(t *testing.T) {
		content := "## 项目测试范围\nscope\n\n" + oldIndex
		out, ok := ReplaceFactIndexSection(content, newIndex)
		if !ok {
			t.Fatal("expected replacement")
		}
		if !strings.Contains(out, "## 项目测试范围") || !strings.Contains(out, "new summary") {
			t.Fatalf("scope preserved, index updated: %q", out)
		}
	})

	t.Run("summary with false markdown header does not truncate early", func(t *testing.T) {
		summaryWithFakeHeader := "see\n\n## fake header in summary"
		old := sampleFactIndexWithFacts("p1", summaryWithFakeHeader)
		newIdx := sampleFactIndexWithFacts("p1", "new summary")
		content := old + "\n\n## 图片分析\nvision"
		out, ok := ReplaceFactIndexSection(content, newIdx)
		if !ok {
			t.Fatal("expected replacement")
		}
		if strings.Contains(out, "fake header in summary") {
			t.Fatalf("old index tail should be fully removed: %q", out)
		}
	})

	t.Run("summary containing end marker text does not truncate early", func(t *testing.T) {
		summary := "note " + FactIndexSectionEndMarker + " in summary"
		old := sampleFactIndexWithFacts("p1", summary)
		newIdx := sampleFactIndexWithFacts("p1", "clean")
		content := old + "\n\n## 图片分析\nvision"
		out, ok := ReplaceFactIndexSection(content, newIdx)
		if !ok {
			t.Fatal("expected replacement")
		}
		if strings.Contains(out, "in summary") {
			t.Fatalf("old block should be fully removed: %q", out)
		}
	})

	t.Run("missing html markers does not replace", func(t *testing.T) {
		legacy := "## Project blackboard index (project: p1, id: x)\n- [a] note — old (tentative)\n"
		newIdx := sampleFactIndexWithFacts("p1", "new")
		out, ok := ReplaceFactIndexSection("prefix\n\n"+legacy, newIdx)
		if ok {
			t.Fatalf("expected no replacement without markers: %q", out)
		}
	})

	t.Run("empty facts block", func(t *testing.T) {
		oldEmpty := wrapFactIndexBlock("## Project blackboard index (project: p1, id: x)\n(no facts yet)\n" + factIndexFooterEmpty)
		newEmpty := sampleFactIndexWithFacts("p1", "first fact")
		out, ok := ReplaceFactIndexSection(oldEmpty, newEmpty)
		if !ok {
			t.Fatal("expected replacement")
		}
		if strings.Contains(out, "(no facts yet)") {
			t.Fatalf("old empty block should be gone: %q", out)
		}
	})

	t.Run("no marker", func(t *testing.T) {
		_, ok := ReplaceFactIndexSection("no blackboard here", newIndex)
		if ok {
			t.Fatal("expected false when marker missing")
		}
	})

	t.Run("empty fresh index", func(t *testing.T) {
		_, ok := ReplaceFactIndexSection(oldIndex, "  ")
		if ok {
			t.Fatal("expected false for empty fresh index")
		}
	})
}

func TestFactIndexSectionBounds_useHTMLMarkers(t *testing.T) {
	t.Parallel()
	body := sampleFactIndexWithFacts("p", "line with\n\n## not a real section") + "TAIL_SHOULD_DROP"
	start, ok := factIndexSectionStart(body)
	if !ok || !strings.HasPrefix(body[start:], FactIndexSectionStartMarker) {
		t.Fatalf("start should be at html start marker, got %d", start)
	}
	end, ok := factIndexSectionEnd(body, start)
	if !ok || body[end:] != "\nTAIL_SHOULD_DROP" {
		t.Fatalf("end should be after end marker, got remainder %q", body[end:])
	}
}

func TestBuildFactIndexBlock_includesHTMLMarkers(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "facts.db")
	db, err := database.NewDB(dbPath, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	proj, err := db.CreateProject(&database.Project{Name: "marker-proj"})
	if err != nil {
		t.Fatal(err)
	}
	block, err := BuildFactIndexBlock(db, proj.ID, config.ProjectConfig{Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(strings.TrimSpace(block), FactIndexSectionStartMarker) {
		t.Fatalf("block should start with start marker: %q", block)
	}
	if !strings.Contains(block, FactIndexSectionEndMarker) {
		t.Fatalf("block should include end marker: %q", block)
	}
}
