//go:build darwin || linux

package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cyberstrike-ai/internal/update"
)

func TestStandaloneUpdateRestartExecsReplacement(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	binary := filepath.Join(root, update.Binary)
	src, err := os.Open(self)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	dst, err := os.OpenFile(binary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	_, copyErr := io.Copy(dst, src)
	if closeErr := dst.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if copyErr != nil {
		t.Fatal(copyErr)
	}
	replacement := filepath.Join(root, "next")
	if err := os.WriteFile(replacement, []byte("#!/bin/sh\nprintf 'replacement:%s:%s:%s:%s\\n' \"$$\" \"$PWD\" \"$CSAI_RESTART_TEST\" \"$1\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "-test.run=^TestUpdateRestartChild$")
	cmd.Dir = canonicalRoot
	// No launchd/systemd markers or supervisor in this child.
	cmd.Env = []string{"CSAI_RESTART_TEST=standalone"}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("restart helper: %v\n%s", err, out)
	}
	want := fmt.Sprintf("replacement:%d:%s:standalone:-test.run=^TestUpdateRestartChild$", cmd.Process.Pid, canonicalRoot)
	if !strings.Contains(string(out), want) {
		t.Fatalf("new executable did not inherit PID, cwd, environment and arguments: %q", out)
	}
}

func TestUpdateRestartChild(t *testing.T) {
	if os.Getenv("CSAI_RESTART_TEST") != "standalone" {
		return
	}
	var app App
	prepare := app.updateRestart(".")
	if prepare == nil {
		t.Fatal("standalone install did not offer restart")
	}
	if err := os.Rename("next", update.Binary); err != nil {
		t.Fatal(err)
	}
	restart, err := prepare()
	if err != nil {
		t.Fatal(err)
	}
	restart()
	t.Fatal("exec returned instead of switching executables")
}
