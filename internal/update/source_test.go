package update

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// The update source is one address (config.yaml update.repo): the official repository by
// default, changeable in one field to a mirror, a fork or a second-development
// repository. The branch that is followed is the source's own default branch, so nothing
// about branches needs configuring. These tests keep that path honest: the configured
// address must be what git is pointed at, the default must be the official repository,
// the followed branch must come from the source itself, and git's ext:: transport (which
// executes commands) must never get through.

func TestValidRemoteURL(t *testing.T) {
	yes := []string{
		"https://github.com/Sycun/CyberStrikeAI.git",
		"http://example.com/x.git",
		"ssh://git@example.com/x.git",
		"git://example.com/x.git",
		"file:///srv/repos/x.git",
		"/Volumes/code/CyberStrikeAI/开发版-CyberStrikeAI",
	}
	no := []string{
		"",
		"-u./evil",
		"ext::sh -c whoami",
		"ext::touch /tmp/x",
		"a\nb",
		"javascript:alert(1)",
		"ftp://example.com/x",
		" https://github.com/x/y.git",
		strings.Repeat("a", 600),
	}
	for _, s := range yes {
		if !ValidRemoteURL(s) {
			t.Errorf("%q must be accepted as a fetch source", s)
		}
	}
	for _, s := range no {
		if ValidRemoteURL(s) {
			t.Errorf("%q must be rejected before it reaches git", s)
		}
	}
}

func TestDefaultRepoIsTheOfficialRepository(t *testing.T) {
	requireGit(t)
	tr := newTree(t)

	snap, err := Status(context.Background(), Options{Root: tr.install})
	if err != nil {
		t.Fatal(err)
	}
	if snap.Repo != DefaultRepoURL || snap.RepoConfigured {
		t.Fatalf("repo = %q configured=%v, want the official default %q without configuration",
			snap.Repo, snap.RepoConfigured, DefaultRepoURL)
	}
	if DefaultRepoURL != "https://github.com/AIPentest/CyberStrikeAI.git" {
		t.Errorf("DefaultRepoURL = %q, want the official repository", DefaultRepoURL)
	}
}

func TestCheckAndApplyFromTheConfiguredRepo(t *testing.T) {
	requireGit(t)
	tr := newTree(t)
	// No remote at all: the source is purely the configured address.
	mustGit(t, tr.install, "remote", "remove", "origin")

	urlOpts := Options{Root: tr.install, Repo: tr.upstream, BinaryName: "none"}

	snap, err := Status(context.Background(), urlOpts)
	if err != nil {
		t.Fatal(err)
	}
	if !snap.Installed || snap.Repo != tr.upstream || !snap.RepoConfigured {
		t.Fatalf("source must resolve to the configured address: installed=%v repo=%q configured=%v",
			snap.Installed, snap.Repo, snap.RepoConfigured)
	}
	if snap.Branch != "main" {
		t.Fatalf("local branch = %q, want main", snap.Branch)
	}

	tr.upstreamCommit(t, "second: bump service", map[string]string{
		"internal_service.go": "package service\n\nconst Version = \"2\"\n",
	})
	snap, err = Check(context.Background(), urlOpts)
	if err != nil {
		t.Fatal(err)
	}
	if snap.CheckError != "" {
		t.Fatalf("check failed: %s", snap.CheckError)
	}
	if !snap.UpdateAvailable || snap.Behind != 1 {
		t.Fatalf("behind=%d available=%v, want 1/true", snap.Behind, snap.UpdateAvailable)
	}
	if snap.TargetBranch != "main" {
		t.Fatalf("targetBranch = %q, want the source's own default branch", snap.TargetBranch)
	}

	res, err := Apply(context.Background(), urlOpts, nil)
	if err != nil {
		t.Fatalf("apply failed: %v\nresult: %+v", err, res)
	}
	if res.Commits != 1 {
		t.Fatalf("res = %+v, want one commit moved", res)
	}
	if got := readFile(t, filepath.Join(tr.install, "internal_service.go")); !strings.Contains(got, "Version = \"2\"") {
		t.Fatalf("code was not updated: %q", got)
	}
	// The configured source parks its ref in a namespace of its own; it must not fabricate a
	// remote the operator never configured.
	if remotes := strings.TrimSpace(mustGit(t, tr.install, "remote")); remotes != "" {
		t.Fatalf("a configured repo must not create a remote, got %q", remotes)
	}
}

func TestSourceFollowsTheRepositoriesDefaultBranch(t *testing.T) {
	requireGit(t)
	tr := newTree(t)
	// The source publishes a default branch that is not "main": the update follows what
	// the source itself names, so nothing about branches needs configuring.
	mustGit(t, tr.upstream, "checkout", "-q", "-b", "stable")
	tr.upstreamCommit(t, "stable: bump service", map[string]string{
		"internal_service.go": "package service\n\nconst Version = \"2\"\n",
	})

	snap, err := Check(context.Background(), Options{Root: tr.install, Repo: tr.upstream, BinaryName: "none"})
	if err != nil {
		t.Fatal(err)
	}
	if snap.CheckError != "" {
		t.Fatalf("check failed: %s", snap.CheckError)
	}
	if snap.TargetBranch != "stable" {
		t.Fatalf("targetBranch = %q, want the source's default branch stable", snap.TargetBranch)
	}
	if !snap.UpdateAvailable || snap.Behind != 1 {
		t.Fatalf("behind=%d available=%v, want 1/true against the source's default branch", snap.Behind, snap.UpdateAvailable)
	}

	res, err := Apply(context.Background(), Options{Root: tr.install, Repo: tr.upstream, BinaryName: "none"}, nil)
	if err != nil {
		t.Fatalf("apply failed: %v\nresult: %+v", err, res)
	}
	if got := readFile(t, filepath.Join(tr.install, "internal_service.go")); !strings.Contains(got, "Version = \"2\"") {
		t.Fatalf("code was not updated from the source's default branch: %q", got)
	}
}

func TestInvalidRemoteURLIsAReadableStateNotAnError(t *testing.T) {
	requireGit(t)
	tr := newTree(t)
	snap, err := Status(context.Background(), Options{Root: tr.install, Repo: "ext::sh -c true"})
	if err != nil {
		t.Fatalf("a bad configured source is a state the page can show, not a fault: %v", err)
	}
	if !strings.Contains(snap.CheckError, "invalid") {
		t.Fatalf("checkError = %q, want the illegality named", snap.CheckError)
	}
}
