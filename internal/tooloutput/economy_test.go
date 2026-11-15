package tooloutput

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReduceForModelRetainsEvidence(t *testing.T) {
	jsonText := "{\n  \"id\": 9007199254740993,\n  \"value\": \"keep  spaces\\n\",\n  \"id\": 2\n}"
	if got := ReduceForModel(jsonText, 8000, SpillOpts{}); got != `{"id":9007199254740993,"value":"keep  spaces\n","id":2}` {
		t.Fatalf("JSON value or numeric precision changed: %s", got)
	}
	full := strings.Repeat("first line\n", 1000) + "middle evidence\n" + strings.Repeat("last line\n", 1000)
	root := t.TempDir()
	opts := SpillOpts{RootDir: root, ConversationID: "fixture", ExecutionID: "result"}
	preview := ReduceForModel(full, 8000, opts)
	if len(preview) >= len(full) || len(preview) > 8000 || !strings.Contains(preview, "read_file") {
		t.Fatalf("invalid preview: %d bytes", len(preview))
	}
	b, err := os.ReadFile(filepath.Join(SessionRoot(root, "", "fixture"), "trunc", "result"))
	if err != nil || string(b) != full {
		t.Fatalf("original evidence lost: %v", err)
	}
	ReduceForModel(preview, 1024, opts)
	b, err = os.ReadFile(filepath.Join(SessionRoot(root, "", "fixture"), "trunc", "result"))
	if err != nil || string(b) != full {
		t.Fatal("reducing an existing preview overwrote full evidence")
	}
	blocked := filepath.Join(root, "file")
	if err := os.WriteFile(blocked, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := ReduceForModel(full, 8000, SpillOpts{RootDir: blocked}); got != full {
		t.Fatal("failed spill lost evidence")
	}
}
