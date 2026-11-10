package update

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeGo is a scripted stand-in for the Go toolchain: `go env GOVERSION` answers, and
// `go build -o <out> ...` either writes a recognisable file or fails, decided by a marker
// file the test toggles. It goes first on PATH (the real PATH stays behind it, because git
// still has to resolve), so the update's real build step runs without compiling the whole
// server inside a test.
type fakeGo struct {
	marker string
}

func scriptFakeGo(t *testing.T) *fakeGo {
	t.Helper()
	if runtime.GOOS == "windows" {
		// The scripted toolchain is a POSIX shell script: Windows would not execute it,
		// LookPath would not even find it, and the scenario would silently assert
		// nothing. Driving it there needs a different harness, not a different promise.
		t.Skip("the scripted go toolchain needs a POSIX shell")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "fail-build")
	f := &fakeGo{marker: marker}
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = env ]; then echo go1.23.4; exit 0; fi\n" +
		"out=\"\"\n" +
		"prev=\"\"\n" +
		"for a in \"$@\"; do\n" +
		"  if [ \"$prev\" = -o ]; then out=\"$a\"; fi\n" +
		"  prev=\"$a\"\n" +
		"done\n" +
		"if [ -f \"" + marker + "\" ]; then echo 'vet: internal/foo.go:12: fake failure' >&2; exit 1; fi\n" +
		"commit=$(git rev-parse --short HEAD) || exit 1\n" +
		"echo \"fake binary for $commit\" > \"$out\"\n"
	if err := os.WriteFile(filepath.Join(dir, "go"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return f
}

func (f *fakeGo) failBuild(t *testing.T) {
	t.Helper()
	if err := os.WriteFile(f.marker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (f *fakeGo) succeed(t *testing.T) {
	t.Helper()
	if err := os.Remove(f.marker); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

// hideToolchain takes `go` off the PATH child processes see while leaving everything else
// (git) reachable: a directory holding a symlink to the real git and nothing else.
func hideToolchain(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		// Same reason as scriptFakeGo: symlinks need privileges there and the bare-name
		// file would not resolve through LookPath.
		t.Skip("PATH surgery here needs POSIX symlinks")
	}
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	if err := os.Symlink(realGit, filepath.Join(dir, "git")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

// buildOpts drives an update with a real binary to swap - the configuration of every
// installed tree, where "the source moved but the binary did not" is even possible.
func buildOpts(tr *tree) Options {
	return Options{Root: tr.install, Repo: tr.upstream, BinaryName: Binary}
}

// TestRetryAfterFailedBuildCompilesTheBinary: a failed build leaves the source updated
// (HEAD is on the new commit), so a retry sees "nothing to pull" - and used to report
// success while the old binary stayed in place. The retry has to finish the build.
func TestRetryAfterFailedBuildCompilesTheBinary(t *testing.T) {
	requireGit(t)
	tr := newTree(t)
	fake := scriptFakeGo(t)
	fake.failBuild(t)

	bin := filepath.Join(tr.install, Binary)
	writeFile(t, bin, "old binary bytes")
	tr.upstreamCommit(t, "upstream moves", map[string]string{"internal_service.go": "package service\n\nconst Version = \"2\"\n"})

	res, err := Apply(context.Background(), buildOpts(tr), nil)
	ue, ok := err.(*Error)
	if !ok || ue.Reason != "build_failed" {
		t.Fatalf("first apply: err=%v result=%+v, want build_failed", err, res)
	}
	if res.BinaryBuilt {
		t.Error("a failed build must not claim a binary")
	}
	if got := readFile(t, bin); got != "old binary bytes" {
		t.Errorf("binary after the failed build = %q, want the old one untouched", got)
	}
	snap, err := Status(context.Background(), buildOpts(tr))
	if err != nil {
		t.Fatal(err)
	}
	if !snap.BuildPending {
		t.Error("after a failed build the tree owes a compile; Status must say so")
	}

	// Same tree, a working toolchain: the retry compiles instead of reporting success for
	// work that never happened.
	fake.succeed(t)
	res, err = Apply(context.Background(), buildOpts(tr), nil)
	if err != nil {
		t.Fatalf("retry failed: %v (result %+v)", err, res)
	}
	if !res.BinaryBuilt || res.Commits != 0 || res.FromCommit != res.ToCommit {
		t.Errorf("retry result = %+v, want a build with no commits moved", res)
	}
	if got := readFile(t, bin); !strings.Contains(got, "fake binary") {
		t.Errorf("binary after retry = %q, want the freshly built one", got)
	}
	if got := readFile(t, bin+".prev"); got != "old binary bytes" {
		t.Errorf("prev binary = %q, want the one the swap kept", got)
	}
	if buildPending(tr.install) {
		t.Error("a finished build must clear the pending marker")
	}
}

// TestRetryFinishesTheBuildWhenTheStateWriteFailed: the merge landed, but the update
// aborted before the state file could be written (a full disk, a directory sitting where
// the file goes). The tree still owes a binary, and the retry must know that - the marker
// is written the moment the merge lands, not after the bookkeeping that may fail.
func TestRollbackAfterFailedBuildKeepsTheMatchingPreviousBinary(t *testing.T) {
	requireGit(t)
	tr := newTree(t)
	fake := scriptFakeGo(t)
	fake.failBuild(t)

	bin := filepath.Join(tr.install, Binary)
	writeFile(t, bin, "original binary")
	before := mustGit(t, tr.install, "rev-parse", "HEAD")
	tr.upstreamCommit(t, "upstream moves", map[string]string{"internal_service.go": "package service\n\nconst Version = \"2\"\n"})

	if _, err := Apply(context.Background(), buildOpts(tr), nil); err == nil {
		t.Fatal("the build must fail")
	}
	if fileExists(bin + ".prev") {
		t.Fatal("a failed build must not create a rollback binary")
	}
	snap, err := Status(context.Background(), buildOpts(tr))
	if err != nil {
		t.Fatal(err)
	}
	if !snap.HasRollback {
		t.Fatal("source can roll back to the commit already matching the live binary")
	}

	res, err := Rollback(context.Background(), buildOpts(tr))
	if err != nil {
		t.Fatalf("rollback failed: %v", err)
	}
	if got := mustGit(t, tr.install, "rev-parse", "HEAD"); got != before {
		t.Fatalf("rollback source = %s, want %s", got, before)
	}
	if got := readFile(t, bin); got != "original binary" {
		t.Fatalf("rollback changed an already-matching binary: %q", got)
	}
	if res.BinaryBuilt {
		t.Fatalf("rollback result must not claim a binary swap: %+v", res)
	}
	if buildPending(tr.install) {
		t.Fatal("rollback must clear the pending build marker")
	}
}

func TestRetryFinishesTheBuildWhenTheStateWriteFailed(t *testing.T) {
	requireGit(t)
	tr := newTree(t)
	fake := scriptFakeGo(t)
	fake.succeed(t)

	bin := filepath.Join(tr.install, Binary)
	writeFile(t, bin, "old binary bytes")
	tr.upstreamCommit(t, "upstream moves", map[string]string{"internal_service.go": "package service\n\nconst Version = \"2\"\n"})

	// A directory where writeState's temp file goes makes the state write fail.
	blocked := filepath.Join(tr.install, ".update-state.json.tmp")
	if err := os.Mkdir(blocked, 0o755); err != nil {
		t.Fatal(err)
	}
	res, err := Apply(context.Background(), buildOpts(tr), nil)
	if ue, ok := err.(*Error); !ok || ue.Reason != "state_unwritable" {
		t.Fatalf("want state_unwritable, got %v (result %+v)", err, res)
	}
	if got := readFile(t, bin); got != "old binary bytes" {
		t.Errorf("no build may happen while the state write is broken: %q", got)
	}

	// Clear the obstruction and retry: the owed build must happen, not a success report.
	if err := os.Remove(blocked); err != nil {
		t.Fatal(err)
	}
	res, err = Apply(context.Background(), buildOpts(tr), nil)
	if err != nil {
		t.Fatalf("retry failed: %v (result %+v)", err, res)
	}
	if !res.BinaryBuilt {
		t.Fatalf("retry result = %+v, want the owed build", res)
	}
	if got := readFile(t, bin); !strings.Contains(got, "fake binary") {
		t.Errorf("binary after retry = %q, want the compiled one", got)
	}
}

// TestAdoptBuildFailureIsFinishedByTheNextUpdate: an adoption that landed the source but
// failed its build leaves a work tree that has "nothing to pull" on the next click. The
// same marker has to make that click compile rather than report "already up to date".
func TestAdoptBuildFailureIsFinishedByTheNextUpdate(t *testing.T) {
	requireGit(t)
	tr, dir := newAdoptFixture(t)
	fake := scriptFakeGo(t)
	fake.failBuild(t)

	o := Options{Root: dir, Repo: tr.upstream, BinaryName: Binary}
	res, err := Adopt(context.Background(), o, nil)
	ue, ok := err.(*Error)
	if !ok || ue.Reason != "build_failed" {
		t.Fatalf("adopt: err=%v result=%+v, want build_failed", err, res)
	}
	snap, err := Status(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if !snap.BuildPending {
		t.Error("the adopted tree owes a build; Status must say so")
	}

	fake.succeed(t)
	res, err = Apply(context.Background(), o, nil)
	if err != nil {
		t.Fatalf("retry failed: %v (result %+v)", err, res)
	}
	if !res.BinaryBuilt {
		t.Fatalf("retry result = %+v, want the owed build", res)
	}
	if got := readFile(t, filepath.Join(dir, Binary)); !strings.Contains(got, "fake binary") {
		t.Errorf("binary after retry = %q, want the compiled one", got)
	}
	if buildPending(dir) {
		t.Error("a finished build must clear the pending marker")
	}
}

// TestRetryWithoutToolchainKeepsSayingSoAndThenBuilds: the no_toolchain message tells the
// operator "Install go, then click update once more to build the binary". Until the fix that retry answered
// success without compiling; the promise is only real if the click after installing go
// actually builds - and the clicks before it keep an honest refusal.
func TestRetryWithoutToolchainKeepsSayingSoAndThenBuilds(t *testing.T) {
	requireGit(t)
	tr := newTree(t)
	hideToolchain(t)

	bin := filepath.Join(tr.install, Binary)
	writeFile(t, bin, "old binary bytes")
	tr.upstreamCommit(t, "upstream moves", map[string]string{"internal_service.go": "package service\n\nconst Version = \"2\"\n"})

	if _, err := Apply(context.Background(), buildOpts(tr), nil); err == nil {
		t.Fatal("a tree without a toolchain must refuse the build, not report success")
	} else if ue, ok := err.(*Error); !ok || ue.Reason != "no_toolchain" {
		t.Fatalf("want no_toolchain, got %v", err)
	}
	// Still no go: the retry keeps refusing.
	if _, err := Apply(context.Background(), buildOpts(tr), nil); err == nil {
		t.Fatal("a retry without a toolchain must keep refusing, not report success")
	}

	// Toolchain installed: the same click now does what the message said it would.
	scriptFakeGo(t)
	res, err := Apply(context.Background(), buildOpts(tr), nil)
	if err != nil {
		t.Fatalf("retry with a toolchain failed: %v (result %+v)", err, res)
	}
	if !res.BinaryBuilt {
		t.Errorf("result = %+v, want the binary built on retry", res)
	}
	if got := readFile(t, bin); !strings.Contains(got, "fake binary") {
		t.Errorf("binary = %q, want the compiled one", got)
	}
}

func TestNewUpdateFinishesThePendingCommitBeforeAdvancing(t *testing.T) {
	requireGit(t)
	tr := newTree(t)
	fake := scriptFakeGo(t)
	fake.failBuild(t)

	bin := filepath.Join(tr.install, Binary)
	writeFile(t, bin, "original binary")
	tr.upstreamCommit(t, "second", map[string]string{"internal_service.go": "package service\n\nconst Version = \"2\"\n"})
	if _, err := Apply(context.Background(), buildOpts(tr), nil); err == nil {
		t.Fatal("the first build must fail")
	}
	second := mustGit(t, tr.install, "rev-parse", "--short", "HEAD")

	tr.upstreamCommit(t, "third", map[string]string{"internal_service.go": "package service\n\nconst Version = \"3\"\n"})
	third := mustGit(t, tr.upstream, "rev-parse", "--short", "HEAD")
	fake.succeed(t)
	res, err := Apply(context.Background(), buildOpts(tr), nil)
	if err != nil {
		t.Fatalf("second apply failed: %v (result %+v)", err, res)
	}
	if got := readFile(t, bin); !strings.Contains(got, third) {
		t.Fatalf("live binary = %q, want third commit %s", got, third)
	}
	if got := readFile(t, bin+".prev"); !strings.Contains(got, second) {
		t.Fatalf("rollback binary = %q, want second commit %s", got, second)
	}

	if _, err := Rollback(context.Background(), buildOpts(tr)); err != nil {
		t.Fatalf("rollback failed: %v", err)
	}
	if head := mustGit(t, tr.install, "rev-parse", "--short", "HEAD"); head != second {
		t.Fatalf("rollback source = %s, want %s", head, second)
	}
	if got := readFile(t, bin); !strings.Contains(got, second) {
		t.Fatalf("rollback binary = %q, want the same commit as source %s", got, second)
	}
}

func TestResidualPendingMarkerDoesNotReplaceTheRollbackBinary(t *testing.T) {
	requireGit(t)
	tr := newTree(t)
	scriptFakeGo(t)

	bin := filepath.Join(tr.install, Binary)
	writeFile(t, bin, "original binary")
	before := mustGit(t, tr.install, "rev-parse", "--short", "HEAD")
	tr.upstreamCommit(t, "second", map[string]string{"internal_service.go": "package service\n\nconst Version = \"2\"\n"})
	if _, err := Apply(context.Background(), buildOpts(tr), nil); err != nil {
		t.Fatal(err)
	}
	updated := mustGit(t, tr.install, "rev-parse", "--short", "HEAD")

	legacy, ok := readState(tr.install)
	if !ok {
		t.Fatal("successful update must leave rollback state")
	}
	legacy.BinaryCommit = ""
	legacy.BinarySHA256 = ""
	legacy.PreviousBinarySHA256 = ""
	if err := writeState(tr.install, legacy); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(buildPendingPath(tr.install), []byte("source is ahead of the binary\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Apply(context.Background(), buildOpts(tr), nil)
	if err != nil {
		t.Fatalf("retry with a residual marker failed: %v", err)
	}
	if !res.BinaryBuilt || !res.NeedsRestart {
		t.Fatalf("the already-installed binary must still be reported as awaiting restart: %+v", res)
	}
	if got := readFile(t, bin+".prev"); got != "original binary" {
		t.Fatalf("residual marker replaced rollback binary with %q", got)
	}

	if _, err := Rollback(context.Background(), buildOpts(tr)); err != nil {
		t.Fatalf("rollback failed: %v", err)
	}
	if head := mustGit(t, tr.install, "rev-parse", "--short", "HEAD"); head != before {
		t.Fatalf("rollback source = %s, want %s (updated was %s)", head, before, updated)
	}
	if got := readFile(t, bin); got != "original binary" {
		t.Fatalf("rollback binary = %q, want original binary", got)
	}
}

func TestLegacyPendingMarkerRefusesAHeadThatMovedAgain(t *testing.T) {
	requireGit(t)
	tr := newTree(t)
	scriptFakeGo(t)

	bin := filepath.Join(tr.install, Binary)
	writeFile(t, bin, "original binary")
	tr.upstreamCommit(t, "second", map[string]string{"internal_service.go": "package service\n\nconst Version = \"2\"\n"})
	if _, err := Apply(context.Background(), buildOpts(tr), nil); err != nil {
		t.Fatal(err)
	}
	rollbackBinary := readFile(t, bin+".prev")
	legacy, ok := readState(tr.install)
	if !ok {
		t.Fatal("successful update must leave rollback state")
	}
	legacy.BinaryCommit = ""
	legacy.BinarySHA256 = ""
	legacy.PreviousBinarySHA256 = ""
	if err := writeState(tr.install, legacy); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(buildPendingPath(tr.install), []byte("source is ahead of the binary\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(tr.install, "local.go"), "package service\n")
	mustGit(t, tr.install, "add", "local.go")
	mustGit(t, tr.install, "-c", "user.email=t@example.com", "-c", "user.name=T", "commit", "-q", "-m", "local move after update")
	if _, err := Apply(context.Background(), buildOpts(tr), nil); err == nil {
		t.Fatal("a legacy marker must not be reinterpreted after HEAD moved")
	} else if ue, ok := err.(*Error); !ok || ue.Reason != "build_state_mismatch" {
		t.Fatalf("want build_state_mismatch, got %v", err)
	}
	if got := readFile(t, bin+".prev"); got != rollbackBinary {
		t.Fatalf("refused migration changed rollback binary: %q", got)
	}
}

// A crash between the version write and the state write leaves the new number in the live
// config with no rollback record beside it. The marker is written *before* the config, so it
// still carries the pair - which is what lets the retry rebuild that record and the rollback
// put the old number back next to the old code.
func TestRetryRebuildsTheRollbackRecordTheCrashNeverWrote(t *testing.T) {
	requireGit(t)
	tr := newTree(t)
	fake := scriptFakeGo(t)
	fake.failBuild(t)

	bin := filepath.Join(tr.install, Binary)
	writeFile(t, bin, "original binary")
	writeFile(t, filepath.Join(tr.install, "config.yaml"), "version: \"v1.0.0\"\nserver:\n  port: 8088\n")
	tr.upstreamCommit(t, "release: bump version", map[string]string{
		"config.example.yaml": "version: \"v1.1.0\"\n",
		"internal_service.go": "package service\n\nconst Version = \"2\"\n",
	})
	if _, err := Apply(context.Background(), buildOpts(tr), nil); err == nil {
		t.Fatal("the first build must fail")
	}
	pending, present, err := readBuildPending(tr.install)
	if err != nil || !present {
		t.Fatalf("marker = %+v present=%v err=%v", pending, present, err)
	}
	if pending.VersionBefore != "v1.0.0" || pending.VersionAfter != "v1.1.0" {
		t.Fatalf("the marker must carry the version pair before the config is written: %+v", pending)
	}

	// The crash: the config already carries the new number, the state file never landed.
	if err := os.Remove(statePath(tr.install)); err != nil {
		t.Fatal(err)
	}
	fake.succeed(t)
	if _, err := Apply(context.Background(), buildOpts(tr), nil); err != nil {
		t.Fatalf("retry failed: %v", err)
	}
	st, ok := readState(tr.install)
	if !ok || st.VersionBefore != "v1.0.0" || st.VersionAfter != "v1.1.0" {
		t.Fatalf("the retry must rebuild the rollback record from the marker: ok=%v state=%+v", ok, st)
	}
	if _, err := Rollback(context.Background(), buildOpts(tr)); err != nil {
		t.Fatalf("rollback failed: %v", err)
	}
	live := readFile(t, filepath.Join(tr.install, "config.yaml"))
	if !strings.Contains(live, `version: "v1.0.0"`) || !strings.Contains(live, "port: 8088") {
		t.Fatalf("live config after rollback = %q, want the old version back with the settings intact", live)
	}
}

func TestRetryRestoresProtectedContentWhenInterruptedBeforeMerge(t *testing.T) {
	requireGit(t)
	tr := newTree(t)
	scriptFakeGo(t)
	bin := filepath.Join(tr.install, Binary)
	writeFile(t, bin, "original binary")
	role := filepath.Join(tr.install, "roles", "shipped.yaml")
	writeFile(t, role, "name: shipped\ndescription: mine\n")
	tr.upstreamCommit(t, "second", map[string]string{
		"internal_service.go": "package service\n\nconst Version = \"2\"\n",
		"roles/shipped.yaml":  "name: shipped\ndescription: upstream\n",
	})

	snap, err := Check(context.Background(), buildOpts(tr))
	if err != nil || snap.CheckError != "" {
		t.Fatalf("check: %v, %s", err, snap.CheckError)
	}
	backupDir, kept, _, err := stashProtected(context.Background(), tr.install, snap.sourceRef())
	if err != nil {
		t.Fatal(err)
	}
	if err := removeProtected(tr.install, kept); err != nil {
		t.Fatal(err)
	}
	if fileExists(role) {
		t.Fatal("fixture must stop after the operator role was moved aside")
	}
	previous := mustGit(t, tr.install, "rev-parse", "HEAD")
	target := mustGit(t, tr.install, "rev-parse", snap.sourceRef())
	oldHash, err := fileSHA256(bin)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeBuildPending(tr.install, buildPendingState{
		Commit: target, PreviousCommit: previous, BackupDir: backupDir,
		KeptContent: kept, PreviousBinarySHA256: oldHash,
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := Apply(context.Background(), buildOpts(tr), nil); err != nil {
		t.Fatalf("retry failed: %v", err)
	}
	if got := readFile(t, role); !strings.Contains(got, "description: mine") {
		t.Fatalf("operator role was not restored: %q", got)
	}
}

func TestRetryAfterMergeRestoresOnlyUntouchedProtectedContent(t *testing.T) {
	for _, tc := range []struct {
		name       string
		afterMerge string
		want       string
	}{
		{name: "merge copy untouched", want: "description: mine before crash"},
		{name: "operator edited after crash", afterMerge: "name: shipped\ndescription: mine after crash\n", want: "description: mine after crash"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requireGit(t)
			tr := newTree(t)
			scriptFakeGo(t)
			bin := filepath.Join(tr.install, Binary)
			writeFile(t, bin, "original binary")
			role := filepath.Join(tr.install, "roles", "shipped.yaml")
			writeFile(t, role, "name: shipped\ndescription: mine before crash\n")
			tr.upstreamCommit(t, "second", map[string]string{
				"internal_service.go": "package service\n\nconst Version = \"2\"\n",
				"roles/shipped.yaml":  "name: shipped\ndescription: upstream\n",
			})

			snap, err := Check(context.Background(), buildOpts(tr))
			if err != nil || snap.CheckError != "" {
				t.Fatalf("check: %v, %s", err, snap.CheckError)
			}
			backupDir, kept, _, err := stashProtected(context.Background(), tr.install, snap.sourceRef())
			if err != nil {
				t.Fatal(err)
			}
			if err := removeProtected(tr.install, kept); err != nil {
				t.Fatal(err)
			}
			previous := mustGit(t, tr.install, "rev-parse", "HEAD")
			target := mustGit(t, tr.install, "rev-parse", snap.sourceRef())
			oldHash, err := fileSHA256(bin)
			if err != nil {
				t.Fatal(err)
			}
			if err := writeBuildPending(tr.install, buildPendingState{
				Commit: target, PreviousCommit: previous, BackupDir: backupDir,
				KeptContent: kept, PreviousBinarySHA256: oldHash,
			}); err != nil {
				t.Fatal(err)
			}
			mustGit(t, tr.install, "merge", "--ff-only", "--no-verify", snap.sourceRef())
			if tc.afterMerge != "" {
				writeFile(t, role, tc.afterMerge)
			}

			if _, err := Apply(context.Background(), buildOpts(tr), nil); err != nil {
				t.Fatalf("retry failed: %v", err)
			}
			if got := readFile(t, role); !strings.Contains(got, tc.want) {
				t.Fatalf("protected content after retry = %q, want %q", got, tc.want)
			}
		})
	}
}

// Legacy records cannot tell an installed rebuild from a binary still owed.
// A different build of the same source must never be promoted into .prev.
func TestLegacyPendingWithUnprovableBinaryPreservesRollback(t *testing.T) {
	requireGit(t)
	tr := newTree(t)
	scriptFakeGo(t)
	bin := filepath.Join(tr.install, Binary)
	writeFile(t, bin, "original binary")
	tr.upstreamCommit(t, "second", map[string]string{"internal_service.go": "package service\nconst Version = \"2\"\n"})
	if _, err := Apply(context.Background(), buildOpts(tr), nil); err != nil {
		t.Fatal(err)
	}
	st, _ := readState(tr.install)
	st.BinaryCommit, st.BinarySHA256, st.PreviousBinarySHA256 = "", "", ""
	if err := writeState(tr.install, st); err != nil {
		t.Fatal(err)
	}
	writeFile(t, bin, "different build of the updated commit")
	writeFile(t, buildPendingPath(tr.install), "source is ahead of the binary\n")
	head := mustGit(t, tr.install, "rev-parse", "HEAD")
	_, err := Apply(context.Background(), buildOpts(tr), nil)
	if ue, ok := err.(*Error); !ok || ue.Reason != "binary_state_unknown" {
		t.Fatalf("want binary_state_unknown, got %v", err)
	}
	if got := readFile(t, bin+".prev"); got != "original binary" {
		t.Fatalf("rollback binary overwritten: %q", got)
	}
	if got := readFile(t, bin); got != "different build of the updated commit" {
		t.Fatalf("live binary overwritten: %q", got)
	}
	if got := mustGit(t, tr.install, "rev-parse", "HEAD"); got != head {
		t.Fatal("refusal moved HEAD")
	}
	if !buildPending(tr.install) {
		t.Fatal("refusal cleared recovery marker")
	}
}

func TestLegacyMarkerUsesRecordedHashForADifferentInstalledBuild(t *testing.T) {
	requireGit(t)
	tr := newTree(t)
	scriptFakeGo(t)
	bin := filepath.Join(tr.install, Binary)
	writeFile(t, bin, "original binary")
	tr.upstreamCommit(t, "second", map[string]string{"internal_service.go": "package service\nconst Version = \"2\"\n"})
	if _, err := Apply(context.Background(), buildOpts(tr), nil); err != nil {
		t.Fatal(err)
	}
	writeFile(t, bin, "another valid build recorded by the updater")
	st, _ := readState(tr.install)
	var err error
	st.BinarySHA256, err = fileSHA256(bin)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeState(tr.install, st); err != nil {
		t.Fatal(err)
	}
	writeFile(t, buildPendingPath(tr.install), "source is ahead of the binary\n")
	if _, err := Apply(context.Background(), buildOpts(tr), nil); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, bin+".prev"); got != "original binary" {
		t.Fatalf("rollback binary overwritten: %q", got)
	}
	if _, err := Rollback(context.Background(), buildOpts(tr)); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, bin); got != "original binary" {
		t.Fatalf("rollback binary = %q", got)
	}
}

func TestInstallBinaryMissingStagingKeepsLiveAndPrevious(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, Binary)
	writeFile(t, bin, "live binary")
	writeFile(t, bin+".prev", "rollback binary")
	if _, err := installBinary(filepath.Join(dir, "missing-staging"), bin); err == nil {
		t.Fatal("missing staging must fail")
	}
	if got := readFile(t, bin); got != "live binary" {
		t.Fatalf("live binary lost: %q", got)
	}
	if got := readFile(t, bin+".prev"); got != "rollback binary" {
		t.Fatalf("previous binary lost: %q", got)
	}
}

func TestLegacyMarkerRefusesContradictoryState(t *testing.T) {
	for _, tc := range []struct {
		name, reason string
		badCommit    bool
	}{
		{name: "unrelated binary commit", reason: "bad_state", badCommit: true},
		{name: "changed binary hash", reason: "binary_changed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requireGit(t)
			tr := newTree(t)
			scriptFakeGo(t)
			bin := filepath.Join(tr.install, Binary)
			writeFile(t, bin, "original binary")
			tr.upstreamCommit(t, "second", map[string]string{"internal_service.go": "package service\nconst Version = \"2\"\n"})
			if _, err := Apply(context.Background(), buildOpts(tr), nil); err != nil {
				t.Fatal(err)
			}
			st, _ := readState(tr.install)
			if tc.badCommit {
				st.BinaryCommit = "not-a-commit"
			} else {
				st.BinarySHA256 = strings.Repeat("0", 64)
			}
			if err := writeState(tr.install, st); err != nil {
				t.Fatal(err)
			}
			writeFile(t, buildPendingPath(tr.install), "source is ahead of the binary\n")
			live := readFile(t, bin)
			stateBytes := readFile(t, statePath(tr.install))
			_, err := Apply(context.Background(), buildOpts(tr), nil)
			if ue, ok := err.(*Error); !ok || ue.Reason != tc.reason {
				t.Fatalf("want %s, got %v", tc.reason, err)
			}
			if got := readFile(t, bin); got != live {
				t.Fatal("refusal changed live binary")
			}
			if got := readFile(t, bin+".prev"); got != "original binary" {
				t.Fatal("refusal changed rollback binary")
			}
			if got := readFile(t, statePath(tr.install)); got != stateBytes {
				t.Fatal("refusal rewrote state")
			}
		})
	}
}
