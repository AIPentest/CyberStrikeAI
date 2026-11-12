package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceRootDirProjectScoped(t *testing.T) {
	got := WorkspaceRootDir("", "proj-1", "conv-1")
	want := filepath.Join("tmp", "workspace", "projects", "proj-1")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestWorkspaceRootDirConversationScoped(t *testing.T) {
	got := WorkspaceRootDir("/data/ws", "", "conv-abc")
	want := filepath.Join("/data/ws", "conversations", "conv-abc")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestEnsureWorkspaceCreatesDir(t *testing.T) {
	root := filepath.Join(t.TempDir(), "nested", "workspace")
	abs, err := EnsureWorkspace(root)
	if err != nil {
		t.Fatalf("EnsureWorkspace: %v", err)
	}
	st, err := os.Stat(abs)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if !st.IsDir() {
		t.Fatal("expected directory")
	}
}

func TestBuildWorkspaceBlockMentionsPath(t *testing.T) {
	block := BuildWorkspaceBlock("/opt/csai/tmp/workspace/projects/p1")
	if block == "" {
		t.Fatal("expected non-empty block")
	}
	if !strings.Contains(block, "/opt/csai/tmp/workspace/projects/p1") {
		t.Fatalf("block missing path: %s", block)
	}
	if !strings.Contains(block, "/tmp") {
		t.Fatalf("block should warn about /tmp: %s", block)
	}
	if !strings.Contains(block, "current directory") || !strings.Contains(block, "service process's current working directory") {
		t.Fatalf("block should distinguish current/project dir from workspace: %s", block)
	}
	if !strings.Contains(block, "do not mistake an empty session working directory for the project root") {
		t.Fatalf("block should warn about empty workspace confusion: %s", block)
	}
}
