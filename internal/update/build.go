package update

import (
	"context"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// toolchain reports whether a Go compiler is reachable from the install tree. An update
// that cannot compile must still update the source and say so plainly, because "no Go on
// this box" is an environment fact the operator can fix, not a reason to refuse the pull.
func toolchain(ctx context.Context, root string) (string, bool) {
	path, err := exec.LookPath("go")
	if err != nil {
		return "", false
	}
	out, err := exec.CommandContext(ctx, path, "env", "GOVERSION").Output()
	if err != nil {
		return path, true
	}
	v := strings.TrimSpace(string(out))
	if v == "" {
		return path, true
	}
	return v, true
}

// build compiles the server binary into outPath, never into the tree's own executable
// name: a build that fails halfway must not leave a truncated binary where the running
// one lives. Output is returned on failure because the operator needs the compiler's
// message, not "build failed".
func build(ctx context.Context, root, outPath string) error {
	cmd := exec.CommandContext(ctx, "go", "build", "-o", outPath, "./cmd/server")
	cmd.Dir = root
	// The module cache goes next to the install rather than into whoever's HOME started
	// the service, so a first build under a service account cannot fail on a HOME that is
	// not writable.
	if home := os.Getenv("HOME"); home == "" || home == "/" {
		cmd.Env = append(os.Environ(), "HOME="+root)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("%s", tail(msg, 4000))
	}
	if _, err := os.Stat(outPath); err != nil {
		return fmt.Errorf("go build reported success but produced no binary at %s", outPath)
	}
	return nil
}

// installBinary puts src in place of dst and keeps the previous dst as dst+".prev", which
// is what makes a rollback possible without a second download.
//
// The rename is the whole trick: a running executable's file cannot be written to on
// Linux (ETXTBSY) but renaming it aside is always allowed, so the old process keeps its
// inode and the new path is complete before anyone can observe it.
func installBinary(src, dst string) (string, error) {
	// Validate the staged file before moving the live executable aside.
	if err := os.Chmod(src, 0o755); err != nil {
		return "", err
	}
	prev := ""
	if fileExists(dst) {
		prev = dst + ".prev"
		if err := os.Remove(prev); err != nil && !os.IsNotExist(err) {
			return "", err
		}
		if err := os.Rename(dst, prev); err != nil {
			return "", fmt.Errorf("failed to keep the old binary: %w", err)
		}
	}
	if err := os.Rename(src, dst); err != nil {
		// Put the kept-previous binary back rather than leaving the install with no
		// executable because one rename failed.
		if prev != "" && !fileExists(dst) {
			_ = os.Rename(prev, dst)
		}
		return "", fmt.Errorf("failed to swap in the new binary: %w", err)
	}
	return prev, nil
}

// restorePrevBinary rolls back one swap. It is deliberately not installBinary in
// reverse: renaming dst onto src first would overwrite the very .prev file being
// restored, so the current binary is discarded instead.
func restorePrevBinary(bin string) error {
	prev := bin + ".prev"
	if !fileExists(prev) {
		return fmt.Errorf("no old binary: %s", prev)
	}
	if err := os.Chmod(prev, 0o755); err != nil {
		return err
	}
	return replaceBinary(prev, bin)

}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func binaryRevision(path string) string {
	info, err := buildinfo.ReadFile(path)
	if err != nil {
		return ""
	}
	var revision string
	for _, setting := range info.Settings {
		if setting.Key == "vcs.modified" && setting.Value == "true" {
			return ""
		}
		if setting.Key == "vcs.revision" {
			revision = strings.TrimSpace(setting.Value)
		}
	}
	return revision
}

// tail keeps the last bytes of a compiler's output: the end of a build log is where the
// error is, while the beginning is where the module downloads are.
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…(earlier output omitted)\n" + s[len(s)-n:]
}

// copyFile is used for the operator-owned content that an update puts aside and takes
// back. It creates the parent directories because the backup tree mirrors repository
// paths, and it preserves the mode because a tool recipe or a script that lost its exec
// bit stops working in a way nothing reports.
func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode.Perm())
	if err != nil {
		return err
	}
	if err := out.Chmod(mode.Perm()); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}
