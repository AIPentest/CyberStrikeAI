// Package update makes "bring this installation up to the newest commit of the
// repository it updates from" one action: fetch, move the working tree, rebuild the
// binary, swap it in.
//
// The update source is one thing: a repository address. It defaults to the official
// repository and can be pointed at a mirror, a fork or a second-development repository
// with one config field (config.yaml update.repo); the branch that is followed is that
// repository's own default branch, so there is nothing else to configure and nothing to
// guess. The platform has to answer "is there anything new" and "take it" with nobody
// typing git or go.
//
// Two properties hold the design down:
//
//   - Nothing destructive happens by default. Local source edits, or a tree that has
//     diverged from the incoming history, stop with the file names and commit counts in
//     the error instead of being reset to match.
//   - Content the operator owns is not ours to overwrite. roles/, skills/, tools/,
//     agents/, bundles/, knowledge_base/, data/, config.yaml and friends are put aside
//     and restored on top of the merge, so an update moves code while the roles and
//     skills created from the console last week survive it. The result names what was
//     kept rather than quietly dropping it on the floor.
package update

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Binary is the default executable name an update replaces.
const Binary = "cyberstrike-ai"

// DefaultRepoURL is where an installation updates from unless config.yaml says
// otherwise. Keeping the official repository as the default is what makes the feature
// work with no configuration at all; forks and mirrors change this one value.
const DefaultRepoURL = "https://github.com/AIPentest/CyberStrikeAI.git"

// urlRefNamespace is where a fetch parks its remote-tracking ref. A fixed name no
// remote can shadow keeps every installation reading the same shape.
const urlRefNamespace = "update-source"

// maxIncoming caps how many commits a status page lists; the count is reported
// separately so "37 new commits, showing 50" is impossible.
const maxIncoming = 50

// namePattern guards remote and branch names before they reach git's argv. Git treats a
// leading dash as an option, and an argument like this one comes from the working tree
// rather than from a request - but the same validator is used for both, so there is one
// rule instead of one that is stricter in the case nobody attacks.
var namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/@-]*$`)

// urlSchemePattern is the allowlist of fetch schemes. git also accepts "ext::" URLs, which
// run the command written after the colons, so this is an allowlist rather than "whatever
// git accepts"; every other "::" form is rejected outright.
var urlSchemePattern = regexp.MustCompile(`^(https?|ssh|git|file)://`)

// maxRemoteURLLength caps what a config file can put on a git command line.
const maxRemoteURLLength = 512

// ValidRemoteURL reports whether s may be used as a fetch source. Local absolute paths are
// allowed on purpose: they are how this project's own trees point at each other in tests
// and on one machine.
func ValidRemoteURL(s string) bool {
	if s == "" || len(s) > maxRemoteURLLength {
		return false
	}
	if strings.HasPrefix(s, "-") {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	if strings.Contains(s, "::") {
		return false
	}
	if strings.HasPrefix(s, "/") || filepath.IsAbs(s) {
		return true
	}
	return urlSchemePattern.MatchString(s)
}

// protectedDirs are install-tree directories whose contents the operator may have
// changed through the console or by hand. An update never overwrites them.
var protectedDirs = []string{
	"roles/", "skills/", "tools/", "agents/", "bundles/", "knowledge_base/",
	"data/", "log/", "logs/", "tmp/", "venv/", ".venv/", "chat_uploads/", ".upgrade-backup/",
}

// protectedFiles are single files with the same rule - most importantly the live
// configuration, which is never in the repository in the first place.
var protectedFiles = []string{"config.yaml", "config.yml", "config.local.yaml", ".env"}

// Protected reports whether a repository-relative path belongs to the operator rather
// than to the product.
func Protected(rel string) bool {
	rel = filepath.ToSlash(rel)
	for _, f := range protectedFiles {
		if rel == f {
			return true
		}
	}
	for _, d := range protectedDirs {
		if strings.HasPrefix(rel, d) {
			return true
		}
	}
	return false
}

// Options describes which installation to act on. Root is the only required field; Repo
// is the update source (config.yaml update.repo) - a repository address to fetch from.
// Empty means the official repository, so an installation with no configuration at all
// still has exactly one answer to "where does an update come from".
type Options struct {
	Root string
	Repo string
	// BinaryName overrides the executable swapped after a successful update. Empty
	// means "cyberstrike-ai" in Root; "none" means leave the binary alone.
	BinaryName string
}

// repoURL is the effective update source. Trimmed here once so every caller sees the
// same decision: configured address if there is one, the official repository otherwise.
func (o Options) repoURL() string {
	if repo := strings.TrimSpace(o.Repo); repo != "" {
		return repo
	}
	return DefaultRepoURL
}

func (o Options) binaryName() string {
	if o.BinaryName == "" {
		return Binary
	}
	return o.BinaryName
}

// installRoot resolves the configured directory to an absolute path. A relative config path
// arrives as "." from filepath.Dir, and while git answers it correctly the page would show
// an installation rooted at "." - and an operator reading the refusal cannot tell which
// directory was actually inspected.
func (o Options) installRoot() (string, error) {
	root := strings.TrimSpace(o.Root)
	if root == "" {
		return "", fmt.Errorf("update: install root is required")
	}
	if abs, err := filepath.Abs(root); err == nil {
		return abs, nil
	}
	return root, nil
}

// Change is one locally modified or deleted tracked file.
type Change struct {
	Path      string `json:"path"`
	Status    string `json:"status"`
	Protected bool   `json:"protected"`
}

// Commit is one line of the incoming history.
type Commit struct {
	Commit  string `json:"commit"`
	Subject string `json:"subject"`
}

// Snapshot is everything a person needs to decide whether to press the button.
type Snapshot struct {
	Root      string `json:"root"`
	Installed bool   `json:"installed"`
	// Branch is the local branch the install tree is on, for display only.
	Branch string `json:"branch"`
	// Repo is the effective update source address, and RepoConfigured says whether it
	// came from config.yaml or is the built-in official repository.
	Repo           string `json:"repo"`
	RepoConfigured bool   `json:"repoConfigured"`
	// TargetBranch is the update source's own default branch, learned from the source
	// itself when the network answer arrives (empty until then).
	TargetBranch string `json:"targetBranch,omitempty"`
	Commit       string `json:"commit"`
	Subject      string `json:"subject"`
	CommittedAt  string `json:"committedAt"`

	Behind int `json:"behind"`
	Ahead  int `json:"ahead"`
	// Diverged means this tree has commits the incoming history does not. Updating then
	// is a merge decision, not a download, so Apply refuses it.
	Diverged bool `json:"diverged"`

	UpdateAvailable bool     `json:"updateAvailable"`
	RemoteCommit    string   `json:"remoteCommit"`
	RemoteSubject   string   `json:"remoteSubject"`
	Incoming        []Commit `json:"incoming"`
	IncomingTotal   int      `json:"incomingTotal"`

	LocalChanges []Change `json:"localChanges"`
	// BlockingChanges are local edits to product source. Content edits are not
	// blocking: they are preserved across the update.
	BlockingChanges []Change `json:"blockingChanges"`

	GoToolchain string `json:"goToolchain"`
	CanBuild    bool   `json:"canBuild"`

	HasBinary   bool `json:"hasBinary"`
	HasRollback bool `json:"hasRollback"`
	// RollbackTo is the commit the previous update came from, when there is one.
	RollbackTo string `json:"rollbackTo"`
	// BuildPending says the last update moved the source but never replaced the binary (a
	// failed compile, or a machine without a toolchain). Such a tree has nothing left to
	// pull and still owes a build, which is why this is visible without a network round
	// trip: the page has to offer the click the failure message told the operator to make.
	BuildPending    bool `json:"buildPending"`
	RollbackPending bool `json:"rollbackPending"`

	// CheckError carries a fetch or parse failure to the page instead of turning the
	// whole endpoint into a 500: "offline" and "your token expired" are answers, not
	// server faults.
	CheckError string `json:"checkError,omitempty"`
}

// git runs one git command in the install tree. Everything goes through argv, never
// through a shell, so a repository name with a metacharacter in it is inert.
func gitCmd(ctx context.Context, root string, args ...string) (string, error) {
	out, err := gitRaw(ctx, root, args...)
	return strings.TrimSpace(out), err
}

// gitRaw is the untrimmed form, and the difference is load-bearing: in a `--porcelain -z`
// record the first two bytes are the X and Y columns, and an unstaged modification is
// " M" - trimming the whole blob eats that leading space and shifts every path one byte
// to the left, so "internal/x.go" arrives as "nternal/x.go". Callers that parse the
// machine formats must use this, not gitCmd.
func gitRaw(ctx context.Context, root string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return string(out), fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(ee.Stderr)))
		}
		return string(out), fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

// nulList splits a `-z` machine-readable git output into its records.
func nulList(blob string) []string {
	var out []string
	for _, rec := range strings.Split(blob, "\x00") {
		if rec != "" {
			out = append(out, rec)
		}
	}
	return out
}

// ValidName reports whether s can safely reach git's argv as a branch name.
func ValidName(s string) bool { return namePattern.MatchString(s) }

// Status reads the install tree without touching the network.
func Status(ctx context.Context, opts Options) (*Snapshot, error) {
	root, err := opts.installRoot()
	if err != nil {
		return nil, err
	}
	snap := &Snapshot{
		Root:            root,
		Repo:            opts.repoURL(),
		RepoConfigured:  strings.TrimSpace(opts.Repo) != "",
		Incoming:        []Commit{},
		LocalChanges:    []Change{},
		BlockingChanges: []Change{},
	}
	if !ValidRemoteURL(snap.Repo) {
		snap.CheckError = "the configured update source repository address is invalid (only https/http/ssh/git/file:// or an absolute local path are allowed)"
		return snap, nil
	}

	if _, err := gitCmd(ctx, root, "rev-parse", "--is-inside-work-tree"); err != nil {
		// Not a git tree: the page still has to say something useful, so this is a
		// state rather than an error.
		snap.Installed = false
		snap.CheckError = "this directory is not a git work tree: configure the update source repository and adopt first, or update by release package"
		snap.GoToolchain, snap.CanBuild = toolchain(ctx, root)
		snap.HasBinary = fileExists(filepath.Join(root, opts.binaryName()))
		return snap, nil
	}
	snap.Installed = true
	if err := repositoryRoot(ctx, root); err != nil {
		snap.CheckError = err.Error()
		return snap, nil
	}

	for _, q := range []struct {
		args []string
		dest *string
	}{
		{[]string{"rev-parse", "--abbrev-ref", "HEAD"}, &snap.Branch},
		{[]string{"rev-parse", "--short", "HEAD"}, &snap.Commit},
		{[]string{"log", "-1", "--format=%s"}, &snap.Subject},
		{[]string{"log", "-1", "--format=%ci"}, &snap.CommittedAt},
	} {
		if v, err := gitCmd(ctx, root, q.args...); err == nil {
			*q.dest = v
		}
	}

	var changeErr error
	snap.LocalChanges, snap.BlockingChanges, changeErr = localChanges(ctx, root)
	if changeErr != nil {
		snap.CheckError = "failed to check local changes: " + changeErr.Error()
		return snap, nil
	}
	snap.GoToolchain, snap.CanBuild = toolchain(ctx, root)
	bin := filepath.Join(root, opts.binaryName())
	snap.HasBinary = fileExists(bin)
	snap.BuildPending = buildPending(root)
	if st, ok := readState(root); ok {
		binaryAlreadyMatchesTarget := st.BinaryCommit != "" && st.BinaryCommit == st.PreviousCommit
		snap.HasRollback = st.UpdatedCommit != "" && !st.PreviousBinaryAbsent && (binaryAlreadyMatchesTarget || fileExists(bin+".prev"))
		snap.RollbackTo = st.PreviousCommit
	}
	if pending, err := readRollbackPending(root); err != nil {
		snap.CheckError = "the rollback recovery record cannot be read: " + err.Error()
	} else if pending != nil {
		snap.HasRollback = true
		snap.RollbackPending = true
		snap.RollbackTo = pending.State.PreviousCommit
	}
	return snap, nil
}

// sourceRef is the remote-tracking ref a status or an update compares against.
func (s *Snapshot) sourceRef() string { return urlRefNamespace + "/" + s.TargetBranch }

// sourceLabel is the human-facing name of where an update would come from.
func (s *Snapshot) sourceLabel() string { return s.Repo }

// localChanges lists modified and deleted tracked files, and splits them by who owns
// the path.
func localChanges(ctx context.Context, root string) (all, blocking []Change, err error) {
	out, err := gitRaw(ctx, root, "status", "--porcelain=v1", "-z", "--no-renames", "--untracked-files=no")
	if err != nil {
		return all, blocking, err
	}
	for _, entry := range nulList(out) {
		if len(entry) < 4 {
			continue
		}
		status, path := entry[:2], filepath.ToSlash(entry[3:])
		if status == "??" || strings.HasPrefix(status, "!") {
			continue
		}
		c := Change{Path: path, Status: changeVerb(status), Protected: Protected(path)}
		all = append(all, c)
		if !c.Protected {
			blocking = append(blocking, c)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Path < all[j].Path })
	sort.Slice(blocking, func(i, j int) bool { return blocking[i].Path < blocking[j].Path })
	return all, blocking, nil
}

func changeVerb(status string) string {
	switch {
	case strings.Contains(status, "D"):
		return "deleted"
	case strings.Contains(status, "A"):
		return "added"
	default:
		return "modified"
	}
}

// Check asks the update source what it has and reports the gap. The two source calls are
// the only network operations in this package, and a failure is a readable answer rather
// than a fault: the page has to distinguish "up to date" from "could not look".
func Check(ctx context.Context, opts Options) (*Snapshot, error) {
	release, err := LockInstall(opts)
	if err != nil {
		return nil, err
	}
	defer release()
	return check(ctx, opts)
}

func check(ctx context.Context, opts Options) (*Snapshot, error) {
	snap, err := Status(ctx, opts)
	if err != nil {
		return nil, err
	}
	if !snap.Installed || snap.CheckError != "" {
		return snap, nil
	}
	root := snap.Root

	// The branch to follow is the source repository's own default branch, so the
	// operator never names one and a fork whose default differs from main still works.
	target, err := resolveDefaultBranch(ctx, root, snap.Repo)
	if err != nil {
		snap.CheckError = err.Error()
		return snap, nil
	}
	snap.TargetBranch = target

	// --recurse-submodules=no and --prune keep the fetch to this repository's own
	// history. A branch-scoped fetch means a source with many refs cannot turn the
	// update check into a full clone.
	if _, err := gitCmd(ctx, root, "fetch", "--quiet", "--no-tags", "--prune", "--recurse-submodules=no",
		snap.Repo, "+refs/heads/"+target+":refs/remotes/"+urlRefNamespace+"/"+target); err != nil {
		snap.CheckError = err.Error()
		return snap, nil
	}

	ref := snap.sourceRef()
	if v, err := gitCmd(ctx, root, "rev-parse", "--short", ref); err == nil {
		snap.RemoteCommit = v
	} else {
		snap.CheckError = err.Error()
		return snap, nil
	}
	snap.RemoteSubject, _ = gitCmd(ctx, root, "log", "-1", "--format=%s", ref)

	counts, err := gitCmd(ctx, root, "rev-list", "--left-right", "--count", "HEAD..."+ref)
	if err != nil {
		snap.CheckError = err.Error()
		return snap, nil
	}
	var ahead, behind int
	if _, err := fmt.Sscanf(counts, "%d %d", &ahead, &behind); err != nil {
		snap.CheckError = "failed to parse the update commit divergence: " + err.Error()
		return snap, nil
	}
	snap.Ahead, snap.Behind = ahead, behind
	snap.Diverged = ahead > 0 && behind > 0
	snap.UpdateAvailable = snap.Behind > 0
	snap.IncomingTotal = snap.Behind

	logs, err := gitCmd(ctx, root, "log", "--format=%h\x1f%s", "-n", fmt.Sprint(maxIncoming), "HEAD.."+ref)
	if err == nil {
		for _, line := range strings.Split(logs, "\n") {
			if line == "" {
				continue
			}
			parts := strings.SplitN(line, "\x1f", 2)
			c := Commit{Commit: parts[0]}
			if len(parts) > 1 {
				c.Subject = parts[1]
			}
			snap.Incoming = append(snap.Incoming, c)
		}
	}
	return snap, nil
}

// resolveDefaultBranch asks the source repository which branch it publishes as its
// default. It is the one naming decision in the update path, and the source makes it,
// so nothing needs configuring and a source whose default is not "main" still works.
func resolveDefaultBranch(ctx context.Context, root, source string) (string, error) {
	out, err := gitCmd(ctx, root, "ls-remote", "--symref", source, "HEAD")
	if err != nil {
		return "", fmt.Errorf("cannot reach the update source repository %s: %s", source, strings.TrimSpace(err.Error()))
	}
	branch := parseDefaultBranch(out)
	if branch == "" {
		return "", fmt.Errorf("cannot determine the default branch of the update source repository: %s", source)
	}
	return branch, nil
}

// parseDefaultBranch reads "ref: refs/heads/<name>\tHEAD" out of ls-remote --symref output.
func parseDefaultBranch(out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "ref: refs/heads/") {
			continue
		}
		rest := strings.TrimPrefix(line, "ref: refs/heads/")
		if i := strings.IndexAny(rest, "\t "); i >= 0 {
			rest = rest[:i]
		}
		if ValidName(rest) {
			return rest
		}
	}
	return ""
}

// Git accepts any descendant of a work tree, but file backups and binary swaps
// are rooted at the installation. Refuse descendants before touching that tree.
func repositoryRoot(ctx context.Context, root string) error {
	top, err := gitCmd(ctx, root, "rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}
	actual, err := os.Stat(root)
	if err != nil {
		return err
	}
	expected, err := os.Stat(top)
	if err != nil {
		return err
	}
	if !os.SameFile(actual, expected) {
		return &Error{Reason: "bad_root", Message: "updates may only operate on the git repository root; use: " + top}
	}
	return nil
}
