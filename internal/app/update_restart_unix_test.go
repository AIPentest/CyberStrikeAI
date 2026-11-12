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

	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/security"
	"cyberstrike-ai/internal/update"

	"go.uber.org/zap"
)

func copyRestartTestBinary(t *testing.T, binary string) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
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
}

func TestStandaloneUpdateRestartExecsReplacement(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, update.Binary)
	copyRestartTestBinary(t, binary)
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

func TestUpdateRestartExecsWithLiveSessions(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, update.Binary)
	copyRestartTestBinary(t, binary)
	copyRestartTestBinary(t, filepath.Join(root, "next"))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "-test.run=^TestUpdateSessionHandoffChild$")
	cmd.Dir = root
	cmd.Env = []string{"CSAI_RESTART_TEST=handoff"}
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "session handoff verified") {
		t.Fatalf("session handoff helper: %v\n%s", err, out)
	}
}

func TestUpdateSessionHandoffChild(t *testing.T) {
	if os.Getenv("CSAI_RESTART_TEST") != "handoff" {
		return
	}
	handoff := TakeUpdateSessionHandoff()
	db, err := database.NewDB("test.db", zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	a := App{auth: security.NewAuthManager(12), db: db}
	defer a.Shutdown()
	password, err := a.auth.AttachRBACStore(db)
	if err != nil {
		t.Fatal(err)
	}
	if token := os.Getenv("CSAI_RESTART_TEST_TOKEN"); token != "" {
		if handoff == nil || os.Getenv(updateSessionsEnv) != "" {
			t.Fatal("replacement did not claim the private handoff")
		}
		a.RestoreUpdateSessions(handoff)
		session, ok := a.auth.ValidateToken(token)
		if !ok || session.Token != token || session.UserID != "admin" || !session.Permissions["update:apply"] ||
			session.ExpiresAt.Format(time.RFC3339Nano) != os.Getenv("CSAI_RESTART_TEST_EXPIRY") {
			t.Fatal("replacement lost login, permissions or original expiration")
		}
		if _, ok := a.auth.ValidateToken(os.Getenv("CSAI_RESTART_TEST_REVOKED")); ok {
			t.Fatal("replacement accepted a logged-out session")
		}
		fmt.Println("session handoff verified")
		return
	}

	token, expires, err := a.auth.Authenticate("admin", password)
	if err != nil {
		t.Fatal(err)
	}
	revoked, _, err := a.auth.Authenticate("admin", password)
	if err != nil {
		t.Fatal(err)
	}
	a.auth.RevokeToken(revoked)
	// The test replacement needs the original client credentials to exercise validation.
	t.Setenv("CSAI_RESTART_TEST_TOKEN", token)
	t.Setenv("CSAI_RESTART_TEST_EXPIRY", expires.Format(time.RFC3339Nano))
	t.Setenv("CSAI_RESTART_TEST_REVOKED", revoked)

	prepare := a.updateRestart(".")
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
