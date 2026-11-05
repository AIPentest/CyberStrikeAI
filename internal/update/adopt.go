package update

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

// recoverAdoption resumes a checkout interrupted before the directory had a
// usable HEAD. It only repeats a forced checkout if every differing local file
// still matches its recorded backup; later operator edits are never overwritten.
func recoverAdoption(ctx context.Context, opts Options) error {
	root, err := opts.installRoot()
	if err != nil {
		return err
	}
	pending, present, err := readBuildPending(root)
	if err != nil {
		return &Error{Reason: "state_unreadable", Message: "failed to read the adopt recovery record: " + err.Error()}
	}
	if !present || pending.AdoptBranch == "" {
		return nil
	}
	if !ValidName(pending.AdoptBranch) {
		return &Error{Reason: "bad_state", Message: "the adopt recovery record branch is invalid"}
	}
	head, headErr := gitCmd(ctx, root, "rev-parse", "HEAD")
	if headErr != nil {
		scan, err := scanAdoption(ctx, root, root, pending.Commit)
		if err != nil {
			return err
		}
		for _, rel := range append(scan.changed, scan.kept...) {
			backup := filepath.Join(pending.BackupDir, "overwritten", filepath.FromSlash(rel))
			if Protected(rel) {
				backup = filepath.Join(pending.BackupDir, filepath.FromSlash(rel))
			}
			old, oldErr := os.ReadFile(backup)
			current, currentErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
			if oldErr != nil || currentErr != nil || !bytes.Equal(old, current) {
				return &Error{Reason: "local_source_edits", Message: "local files changed after the adopt interruption; the current state is preserved: " + rel}
			}
		}
		if _, err := gitCmd(ctx, root, "checkout", "--force", "-B", pending.AdoptBranch, pending.Commit); err != nil {
			return err
		}
	} else if head != pending.Commit {
		return &Error{Reason: "build_state_mismatch", Message: "HEAD changed after the adopt interruption; the current state is preserved"}
	}
	if err := restoreInterruptedProtected(ctx, root, pending, true); err != nil {
		return err
	}
	pending.AdoptBranch = ""
	pending.VersionBefore, pending.VersionAfter = plannedVersionChange(root)
	return writeBuildPending(root, pending)
}

// Adopt connects a plain directory - a tarball or zip installation, or a copy carried in by
// hand - to the configured update source, so it becomes a git work tree the one-click
// update applies to from then on.
//
// The rules are the ones an update already keeps: operator content (roles/skills/tools/
// agents/knowledge_base/data/config.yaml) is put aside and put back, and nothing else is
// replaced without having been named first - the preview lists every local file whose
// content differs from the target, and each replaced file is kept under
// <backup>/overwritten/ so "the update ate my edit" always has an address.

// maxAdoptList caps the file lists a plan carries over the wire; totals are separate so a
// truncated list can never be read as the whole truth.
const maxAdoptList = 50

// AdoptPlan is the preview of connecting a directory to a source. Producing it must not
// change the directory.
type AdoptPlan struct {
	Root    string `json:"root"`
	Source  string `json:"source"`
	Branch  string `json:"branch"`
	Commit  string `json:"commit"`
	Subject string `json:"subject"`

	Incoming int `json:"incoming"` // files the target branch tracks

	OverwrittenTotal int      `json:"overwrittenTotal"`
	Overwritten      []string `json:"overwritten"` // differing local files the target's version will replace (capped)

	ProtectedTotal int      `json:"protectedTotal"`
	Protected      []string `json:"protected"` // operator content put aside and put back (capped)
}

// adoptionScan is the full (uncapped) comparison an adoption is based on.
type adoptionScan struct {
	target  map[string]string // tracked path -> blob sha
	paths   []string          // every tracked path, sorted
	changed []string          // local file differs from the target and is not protected
	kept    []string          // local file differs from the target and is protected
}

// Preview builds an adoption plan without touching the directory: the fetch happens in a
// throwaway bare repository outside it, so a failure cannot leave a half-adopted tree.
func Preview(ctx context.Context, opts Options) (*AdoptPlan, error) {
	root, source, err := adoptPreflight(ctx, opts)
	if err != nil {
		return nil, err
	}

	tmp, err := os.MkdirTemp("", "csai-adopt-preview-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	if _, err := gitCmd(ctx, tmp, "init", "--quiet", "--bare"); err != nil {
		return nil, err
	}
	branch, commit, subject, err := fetchSource(ctx, tmp, source)
	if err != nil {
		return nil, err
	}
	scan, err := scanAdoption(ctx, root, tmp, "origin/"+branch)
	if err != nil {
		return nil, err
	}
	return scan.plan(root, source, branch, commit, subject), nil
}

// Adopt carries the directory onto the source. It runs its own fetch inside the directory
// (the preview's throwaway fetch cannot be reused), and on any failure removes only the
// .git it created itself, so a retry starts from the same pristine state.
func Adopt(ctx context.Context, opts Options, onStep func(Step)) (*Result, error) {
	release, err := LockInstall(opts)
	if err != nil {
		return nil, err
	}
	defer release()
	if onStep == nil {
		onStep = func(Step) {}
	}
	step := func(phase, msg string) { onStep(Step{Phase: phase, Message: msg, At: time.Now().Format(time.RFC3339)}) }
	start := time.Now()

	root, source, err := adoptPreflight(ctx, opts)
	if err != nil {
		return nil, err
	}
	step("preflight", fmt.Sprintf("adopting directory %s into update source %s", root, source))

	if _, err := gitCmd(ctx, root, "init", "--quiet"); err != nil {
		return nil, &Error{Reason: "init_failed", Message: "git init failed: " + err.Error()}
	}
	adopted := false
	defer func() {
		if !adopted {
			// Only the repository git just created; the directory's own files are untouched.
			_ = os.RemoveAll(filepath.Join(root, ".git"))
		}
	}()
	if _, err := gitCmd(ctx, root, "remote", "add", "origin", source); err != nil {
		return nil, &Error{Reason: "init_failed", Message: "failed to add the remote: " + err.Error()}
	}

	branch, commit, subject, err := fetchSource(ctx, root, source)
	if err != nil {
		return nil, err
	}
	ref := "origin/" + branch
	scan, err := scanAdoption(ctx, root, root, ref)
	if err != nil {
		return nil, err
	}
	step("fetch", fmt.Sprintf("%s/%s (%s %s) totals %d files: %d local versions will be replaced by the target version, %d operator files are staged first and restored afterwards",
		source, branch, commit, subject, len(scan.paths), len(scan.changed), len(scan.kept)))

	backupDir := ""
	backupAll := append(append([]string{}, scan.changed...), scan.kept...)
	if len(backupAll) > 0 {
		backupDir, err = newBackupDir(root, time.Now().Format("20060102_150405"))
		if err != nil {
			return nil, err
		}
	}
	for _, p := range backupAll {
		abs := filepath.Join(root, filepath.FromSlash(p))
		info, statErr := os.Stat(abs)
		if statErr != nil || !info.Mode().IsRegular() {
			return nil, &Error{Reason: "backup_failed", Message: "local files changed before the backup; adopt not performed: " + p}
		}
		dst := filepath.Join(backupDir, "overwritten", filepath.FromSlash(p))
		if Protected(p) {
			dst = filepath.Join(backupDir, filepath.FromSlash(p))
		}
		if err := copyFile(abs, dst, info.Mode()); err != nil {
			return nil, &Error{Reason: "backup_failed", Message: fmt.Sprintf("backup of %s failed: %v", p, err)}
		}
	}
	if len(backupAll) > 0 {
		step("protect", fmt.Sprintf("%d files kept on record (replaced ones under %s/overwritten/, operator files restored as-is)", len(backupAll), backupDir))
	}

	targetCommit, err := gitCmd(ctx, root, "rev-parse", ref)
	if err != nil {
		return nil, err
	}
	pending := buildPendingState{
		Commit:               targetCommit,
		BackupDir:            backupDir,
		KeptContent:          append([]string(nil), scan.kept...),
		OverwrittenContent:   append([]string(nil), scan.changed...),
		AdoptBranch:          branch,
		PreviousBinaryAbsent: !fileExists(filepath.Join(root, opts.binaryName())),
	}
	if opts.binaryName() != "none" {
		bin := filepath.Join(root, opts.binaryName())
		if fileExists(bin) {
			pending.PreviousBinarySHA256, err = fileSHA256(bin)
			if err != nil {
				return nil, err
			}
		}
	}
	// Record both the target and backups before checkout can replace any file.
	if err := writeBuildPending(root, pending); err != nil {
		return nil, &Error{Reason: "state_unwritable", Message: "failed to write the adopt recovery state: " + err.Error()}
	}
	adopted = true
	if _, err := gitCmd(ctx, root, "checkout", "--force", "-B", branch, targetCommit); err != nil {
		return nil, &Error{Reason: "checkout_failed", Message: "failed to place the target content; the recovery record is preserved and a further update can recover: " + err.Error()}
	}
	if err := restoreProtected(backupDir, scan.kept); err != nil {
		return nil, err
	}
	pending.AdoptBranch = ""
	pending.VersionBefore, pending.VersionAfter = plannedVersionChange(root)
	if err := writeBuildPending(root, pending); err != nil {
		return nil, &Error{Reason: "state_unwritable", Message: "failed to update the adopt recovery state: " + err.Error()}
	}
	if _, err := gitCmd(ctx, root, "branch", "--set-upstream-to="+ref); err != nil {
		return nil, &Error{Reason: "upstream_failed", Message: "failed to set branch tracking: " + err.Error()}
	}

	versionBefore, versionAfter, versionErr := syncVersionToConfig(root)
	versionStep(step, versionBefore, versionAfter, versionErr)

	res := &Result{
		ToCommit:     commit,
		FilesTouched: len(scan.paths),
		KeptContent:  scan.kept,
		Overwritten:  scan.changed,
		BackupDir:    backupDir,
		Adopted:      true,
		NeedsRestart: true,
	}

	if opts.binaryName() == "none" {
		if err := clearBuildPending(root); err != nil {
			return res, err
		}
		step("done", fmt.Sprintf("directory adopted into %s/%s (%s); rebuild skipped as requested", source, branch, commit))
		res.Duration = time.Since(start).Round(time.Millisecond).String()
		return res, nil
	}

	bin := filepath.Join(root, opts.binaryName())
	res.BinaryPath = bin
	if _, canBuild := toolchain(ctx, root); !canBuild {
		step("done", fmt.Sprintf("directory adopted into %s/%s (%s); no Go toolchain on this machine, the binary was not rebuilt", source, branch, commit))
		res.Duration = time.Since(start).Round(time.Millisecond).String()
		return res, &Error{
			Reason:  "no_toolchain",
			Message: "the directory was adopted into the update source, but this machine has no Go toolchain and cannot rebuild. Install go and click update once more to get the binary.",
		}
	}

	if err := buildAndSwap(ctx, root, opts, res, step, targetCommit, nil, pending); err != nil {
		res.Duration = time.Since(start).Round(time.Millisecond).String()
		return res, err
	}
	step("done", fmt.Sprintf("directory adopted into %s/%s (%s) and the binary swapped in", source, branch, commit))
	res.Duration = time.Since(start).Round(time.Millisecond).String()
	return res, nil
}

// adoptPreflight validates the operation's preconditions: a plain directory and a usable
// source address. It does not touch the directory.
func adoptPreflight(ctx context.Context, opts Options) (root, source string, err error) {
	root, err = opts.installRoot()
	if err != nil {
		return "", "", err
	}
	if _, err := exec.LookPath("git"); err != nil {
		return "", "", &Error{Reason: "no_git", Message: "no git on this machine; the directory cannot be adopted into the update source"}
	}
	if _, err := gitCmd(ctx, root, "rev-parse", "--is-inside-work-tree"); err == nil {
		return "", "", &Error{Reason: "already_a_repo", Message: "this directory is already a git work tree: use one-click update, no adoption needed"}
	}
	if fi, statErr := os.Stat(root); statErr != nil || !fi.IsDir() {
		return "", "", &Error{Reason: "bad_root", Message: "install directory does not exist: " + root}
	}
	if _, statErr := os.Lstat(filepath.Join(root, ".git")); !os.IsNotExist(statErr) {
		return "", "", &Error{Reason: "existing_git_metadata", Message: "the install directory already has .git metadata; adoption will not overwrite it. Inspect the repository state first"}
	}
	source = opts.repoURL()
	if !ValidRemoteURL(source) {
		return "", "", &Error{Reason: "bad_source", Message: "the update source repository address is invalid (only https/http/ssh/git/file:// or an absolute local path are allowed)"}
	}
	return root, source, nil
}

// fetchSource fetches the source's default branch into repo and reports which branch
// that ended up being, its short commit and its subject. Both callers fetch into a
// repository of their own.
func fetchSource(ctx context.Context, repo, source string) (string, string, string, error) {
	out, err := gitCmd(ctx, repo, "ls-remote", "--symref", source, "HEAD")
	if err != nil {
		return "", "", "", &Error{Reason: "fetch_failed", Message: "failed to read the remote default branch: " + err.Error()}
	}
	branch := parseDefaultBranch(out)
	if branch == "" {
		return "", "", "", &Error{Reason: "no_branch", Message: "cannot determine the remote default branch: " + source}
	}
	if _, err := gitCmd(ctx, repo, "fetch", "--quiet", "--no-tags", "--recurse-submodules=no",
		source, "+refs/heads/"+branch+":refs/remotes/origin/"+branch); err != nil {
		return "", "", "", &Error{Reason: "fetch_failed", Message: "fetch failed: " + err.Error()}
	}
	ref := "origin/" + branch
	commit, err := gitCmd(ctx, repo, "rev-parse", "--short", ref)
	if err != nil {
		return "", "", "", &Error{Reason: "no_branch", Message: fmt.Sprintf("the remote has no branch %s", branch)}
	}
	subject, _ := gitCmd(ctx, repo, "log", "-1", "--format=%s", ref)
	return branch, commit, subject, nil
}

// scanAdoption compares the directory against the tracked files of ref in repo.
func scanAdoption(ctx context.Context, root, repo, ref string) (*adoptionScan, error) {
	out, err := gitRaw(ctx, repo, "ls-tree", "-r", "-z", "--full-tree", ref)
	if err != nil {
		return nil, &Error{Reason: "diff_failed", Message: err.Error()}
	}
	scan := &adoptionScan{target: map[string]string{}}
	targetModes := map[string]os.FileMode{}
	for _, rec := range nulList(out) {
		tab := strings.IndexByte(rec, '\t')
		if tab < 0 {
			continue
		}
		fields := strings.Fields(rec[:tab])
		if len(fields) < 3 || fields[1] != "blob" {
			continue // submodules and trees are not files this comparison replaces
		}
		p := rec[tab+1:]
		scan.target[p] = fields[2]
		targetModes[p] = 0644
		if fields[0] == "100755" {
			targetModes[p] = 0755
		}
		scan.paths = append(scan.paths, p)
	}
	sort.Strings(scan.paths)
	for _, p := range scan.paths {
		if err := validateFilePath(root, p); err != nil {
			return nil, err
		}
		abs := filepath.Join(root, filepath.FromSlash(p))
		info, err := os.Lstat(abs)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		hash, err := gitBlobHash(abs)
		if err != nil {
			return nil, &Error{Reason: "backup_failed", Message: "failed to read the local file awaiting adoption; directory untouched: " + p + ": " + err.Error()}
		}
		modeChanged := runtime.GOOS != "windows" && Protected(p) && info.Mode().Perm() != targetModes[p]
		if hash == scan.target[p] && !modeChanged {
			continue
		}
		if Protected(p) {
			scan.kept = append(scan.kept, p)
		} else {
			scan.changed = append(scan.changed, p)
		}
	}
	return scan, nil
}

// plan renders the capped, wire-facing view of a scan.
func (s *adoptionScan) plan(root, source, branch, commit, subject string) *AdoptPlan {
	p := &AdoptPlan{
		Root: root, Source: source, Branch: branch, Commit: commit, Subject: subject,
		Incoming:         len(s.paths),
		OverwrittenTotal: len(s.changed),
		ProtectedTotal:   len(s.kept),
		Overwritten:      append([]string{}, s.changed...),
		Protected:        append([]string{}, s.kept...),
	}
	if len(p.Overwritten) > maxAdoptList {
		p.Overwritten = p.Overwritten[:maxAdoptList]
	}
	if len(p.Protected) > maxAdoptList {
		p.Protected = p.Protected[:maxAdoptList]
	}
	if p.Overwritten == nil {
		p.Overwritten = []string{}
	}
	if p.Protected == nil {
		p.Protected = []string{}
	}
	return p
}

// gitBlobHash computes the object id git would give a file's content. It hashes the bytes
// as they are (no filters), because the comparison is against raw blobs in the tree. sha1 is
// not a security choice here - it is the blob-id algorithm of the repository format.
func gitBlobHash(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(data))
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil)), nil
}
