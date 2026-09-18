package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureProjectVenvOnPathFromRootMissingPython(t *testing.T) {
	origPath := "/usr/bin" + string(os.PathListSeparator) + "/bin"
	t.Setenv("PATH", origPath)
	t.Setenv("VIRTUAL_ENV", "")
	t.Setenv("CYBERSTRIKE_ROOT", "")

	root := t.TempDir()
	if got := ensureProjectVenvOnPathFromRoot(root); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
	if os.Getenv("PATH") != origPath {
		t.Fatalf("PATH changed to %q", os.Getenv("PATH"))
	}
}

func TestEnsureProjectVenvOnPathFromRootPrependsVenv(t *testing.T) {
	origPath := "/usr/bin" + string(os.PathListSeparator) + "/bin"
	t.Setenv("PATH", origPath)
	t.Setenv("VIRTUAL_ENV", "")
	t.Setenv("CYBERSTRIKE_ROOT", "")

	root := t.TempDir()
	venvBin := filepath.Join(root, "venv", "bin")
	if err := os.MkdirAll(venvBin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(venvBin, "python3"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := ensureProjectVenvOnPathFromRoot(root)
	if got != venvBin {
		t.Fatalf("got %q, want %q", got, venvBin)
	}
	wantPrefix := venvBin + string(os.PathListSeparator)
	if path := os.Getenv("PATH"); !strings.HasPrefix(path, wantPrefix) {
		t.Fatalf("PATH=%q, want prefix %q", path, wantPrefix)
	}
	if os.Getenv("VIRTUAL_ENV") != filepath.Join(root, "venv") {
		t.Fatalf("VIRTUAL_ENV=%q", os.Getenv("VIRTUAL_ENV"))
	}
	if os.Getenv("CYBERSTRIKE_ROOT") != root {
		t.Fatalf("CYBERSTRIKE_ROOT=%q", os.Getenv("CYBERSTRIKE_ROOT"))
	}

	// Second call must not duplicate PATH.
	if again := ensureProjectVenvOnPathFromRoot(root); again != venvBin {
		t.Fatalf("second call got %q", again)
	}
	if strings.Count(os.Getenv("PATH"), venvBin) != 1 {
		t.Fatalf("PATH duplicated: %q", os.Getenv("PATH"))
	}
}

func TestEnsureProjectVenvOnPathFromRootKeepsExistingEnv(t *testing.T) {
	root := t.TempDir()
	venvBin := filepath.Join(root, "venv", "bin")
	if err := os.MkdirAll(venvBin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(venvBin, "python3"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", venvBin+string(os.PathListSeparator)+"/usr/bin")
	t.Setenv("VIRTUAL_ENV", "/already/venv")
	t.Setenv("CYBERSTRIKE_ROOT", "/already/root")

	if got := ensureProjectVenvOnPathFromRoot(root); got != venvBin {
		t.Fatalf("got %q, want %q", got, venvBin)
	}
	if os.Getenv("VIRTUAL_ENV") != "/already/venv" {
		t.Fatalf("VIRTUAL_ENV overwritten: %q", os.Getenv("VIRTUAL_ENV"))
	}
	if os.Getenv("CYBERSTRIKE_ROOT") != "/already/root" {
		t.Fatalf("CYBERSTRIKE_ROOT overwritten: %q", os.Getenv("CYBERSTRIKE_ROOT"))
	}
}
