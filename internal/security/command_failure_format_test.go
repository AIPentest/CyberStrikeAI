package security

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func TestFormatCommandFailureResult(t *testing.T) {
	got := FormatCommandFailureResult(1, "sudo: password required")
	want := "Command execution failed: exit status 1\noutput: sudo: password required"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if FormatCommandFailureResult(2, "") != "Command execution failed: exit status 2" {
		t.Fatal("empty output format")
	}
	if FormatCommandFailureResult(1, "Command execution failed: exit status 1") != "Command execution failed: exit status 1" {
		t.Fatal("should not double-wrap")
	}
}

func TestIsCommandFailureResult(t *testing.T) {
	if !IsCommandFailureResult("sudo: err\nCommand execution failed: exit status 1") {
		t.Fatal("expected true")
	}
	if IsCommandFailureResult("sudo: err only") {
		t.Fatal("expected false")
	}
}

func TestFormatCommandFailureFromErr(t *testing.T) {
	cmd := exec.Command("sh", "-c", "exit 42")
	err := cmd.Run()
	got := FormatCommandFailureFromErr(err, "oops")
	if got != "Command execution failed: exit status 42\noutput: oops" {
		t.Fatalf("got %q", got)
	}
	timeoutErr := errors.New("shell inactivity timeout (300s)")
	got2 := FormatCommandFailureFromErr(timeoutErr, "already timed out")
	if !strings.Contains(got2, "shell inactivity timeout") || !strings.Contains(got2, "already timed out") {
		t.Fatalf("got %q", got2)
	}
}

func TestIsLegacyShellExitNoise(t *testing.T) {
	if !IsLegacyShellExitNoise("command exited with non-zero code 1\n") {
		t.Fatal("expected legacy noise")
	}
	if IsLegacyShellExitNoise("sudo: failed") {
		t.Fatal("unexpected noise")
	}
}
