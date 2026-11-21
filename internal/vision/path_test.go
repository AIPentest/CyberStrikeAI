package vision

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveImagePath_underCWD(t *testing.T) {
	dir := t.TempDir()
	img := filepath.Join(dir, "shot.png")
	if err := os.WriteFile(img, []byte{0x89, 0x50, 0x4e, 0x47}, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveImagePath(img, dir)
	if err != nil {
		t.Fatal(err)
	}
	want := normalizeAbsPath(img)
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestResolveImagePath_absoluteOutsideCWD(t *testing.T) {
	dir := t.TempDir()
	cwd := t.TempDir()
	img := filepath.Join(dir, "remote.png")
	if err := os.WriteFile(img, []byte{0x89, 0x50, 0x4e, 0x47}, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveImagePath(img, cwd)
	if err != nil {
		t.Fatalf("expected absolute path outside cwd to be allowed: %v", err)
	}
	want := normalizeAbsPath(img)
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestResolveImagePath_rejectsNonImageExt(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "notes.body")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ResolveImagePath(f, dir)
	if err == nil || !strings.Contains(err.Error(), "unsupported image extension \".body\"") || !strings.Contains(err.Error(), ".png or .jpg") {
		t.Fatalf("expected extension error with recovery guidance, got %v", err)
	}
}
