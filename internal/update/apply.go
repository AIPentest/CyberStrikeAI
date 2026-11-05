package update

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// stateFile records what the last update moved, so a rollback has a target that is not
// guessed at. It lives in the install tree and is deliberately not in git: it describes
// this machine's installation.
const stateFile = ".update-state.json"

// State is the rollback contract of one installation.
type State struct {
	PreviousCommit string `json:"previous_commit"`
	UpdatedCommit  string `json:"updated_commit"`
	BackupDir      string `json:"backup_dir"`
	UpdatedAt      string `json:"updated_at"`
	// BinaryCommit says which source commit produced the live binary. Before a build it is
	// PreviousCommit; after a successful swap it is UpdatedCommit. Old state files omit it
	// and keep the legacy assumption that their .prev binary must be restored.
	BinaryCommit         string `json:"binary_commit,omitempty"`
	BinarySHA256         string `json:"binary_sha256,omitempty"`
	PreviousBinarySHA256 string `json:"previous_binary_sha256,omitempty"`
	PreviousBinaryAbsent bool   `json:"previous_binary_absent,omitempty"`
	// VersionBefore/VersionAfter record a version-line change this update made, so a
	// rollback can put the old number back next to the old code. Absent for updates that
	// touched no version (old state files simply have no such fields).
	VersionBefore string `json:"version_before,omitempty"`
	VersionAfter  string `json:"version_after,omitempty"`
}

func statePath(root string) string { return filepath.Join(root, stateFile) }

// buildPendingFile records "the source has moved (or was moved), but the binary has not
// followed": a failed build, or a machine with no Go toolchain. It is written the moment
// the tree moves and removed only once the new binary is in place, so a retry on an
// already-current tree still knows there is a compile to finish. A file of its own rather
// than a State field because an adoption owes a build too, and an adoption has no state
// (there is no previous commit to roll back to).
const buildPendingFile = ".update-build-pending"

type buildPendingState struct {
	Commit               string   `json:"commit"`
	PreviousCommit       string   `json:"previous_commit,omitempty"`
	BackupDir            string   `json:"backup_dir,omitempty"`
	KeptContent          []string `json:"kept_content,omitempty"`
	DeletedContent       []string `json:"deleted_content,omitempty"`
	VersionBefore        string   `json:"version_before,omitempty"`
	VersionAfter         string   `json:"version_after,omitempty"`
	BinarySHA256         string   `json:"binary_sha256,omitempty"`
	PreviousBinarySHA256 string   `json:"previous_binary_sha256,omitempty"`
	AdoptBranch          string   `json:"adopt_branch,omitempty"`
	OverwrittenContent   []string `json:"overwritten_content,omitempty"`
	PreviousBinaryAbsent bool     `json:"previous_binary_absent,omitempty"`
	Legacy               bool     `json:"-"`
}

func buildPendingPath(root string) string { return filepath.Join(root, buildPendingFile) }

func readBuildPending(root string) (buildPendingState, bool, error) {
	var st buildPendingState
	data, err := os.ReadFile(buildPendingPath(root))
	if os.IsNotExist(err) {
		return st, false, nil
	}
	if err != nil {
		return st, true, err
	}
	if err := decodeRecoveryJSON(data, &st); err != nil {
		if strings.TrimSpace(string(data)) == "source is ahead of the binary" {
			if _, _, stateErr := recoveryState(root); stateErr != nil {
				return st, true, stateErr
			}
			return buildPendingState{Legacy: true}, true, nil
		}
		return buildPendingState{}, true, fmt.Errorf("corrupted pending-build state: %w", err)
	}
	if err := validateBuildEvidence(root, st); err != nil {
		return st, true, err
	}
	return st, true, nil
}

func writeBuildPending(root string, st buildPendingState) error {
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	tmp := buildPendingPath(root) + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, buildPendingPath(root))
}

func clearBuildPending(root string) error {
	if err := os.Remove(buildPendingPath(root)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func buildPending(root string) bool { return fileExists(buildPendingPath(root)) }

func readState(root string) (State, bool) {
	var st State
	data, err := os.ReadFile(statePath(root))
	if err != nil {
		return st, false
	}
	if err := json.Unmarshal(data, &st); err != nil || st.PreviousCommit == "" || st.UpdatedCommit == "" {
		return st, false
	}
	return st, true
}

func writeState(root string, st State) error {
	st.UpdatedAt = time.Now().Format(time.RFC3339)
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := statePath(root) + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, statePath(root))
}

// Step is one progress line. A build takes minutes, so Apply reports where it is rather
// than appearing to hang; the console renders these directly.
type Step struct {
	Phase   string `json:"phase"`
	Message string `json:"message"`
	At      string `json:"at"`
}

// Result is what one update attempt produced. Everything the operator would otherwise
// have to go and inspect by hand is in here.
type Result struct {
	FromCommit   string `json:"fromCommit"`
	ToCommit     string `json:"toCommit"`
	Commits      int    `json:"commits"`
	FilesTouched int    `json:"filesTouched"`
	// KeptContent names the operator-owned files that were restored over the update.
	// Upstream may well have changed them; saying which ones is the difference between
	// "my roles disappeared" and "my roles are still mine, and these upstream files did
	// not land".
	KeptContent []string `json:"keptContent"`
	// Overwritten names local files whose content the target repository's version replaced
	// during an adoption; copies are kept under <BackupDir>/overwritten/ so nothing is lost
	// without a way back.
	Overwritten []string `json:"overwritten,omitempty"`
	// Adopted marks a result that comes from connecting a plain directory to a source
	// rather than from moving an existing work tree.
	Adopted      bool   `json:"adopted,omitempty"`
	BinaryPath   string `json:"binaryPath"`
	BinaryBuilt  bool   `json:"binaryBuilt"`
	PrevBinary   string `json:"prevBinary"`
	BackupDir    string `json:"backupDir"`
	NeedsRestart bool   `json:"needsRestart"`
	Duration     string `json:"duration"`
}

// Error is a refusal with the information needed to act on it. Callers render the
// Message; Details is for the page to list file names or commits.
type Error struct {
	Reason  string   `json:"reason"`
	Message string   `json:"message"`
	Items   []string `json:"items,omitempty"`
}

func (e *Error) Error() string { return e.Message }

// Apply carries an installation to the tip of the branch it tracks, then rebuilds and
// swaps the binary. It is all-or-nothing up to the point of the working-tree move: a
// failed build leaves the tree updated (source and previous binary are both recoverable),
// while a refused precondition leaves nothing touched at all.
func Apply(ctx context.Context, opts Options, onStep func(Step)) (*Result, error) {
	release, err := LockInstall(opts)
	if err != nil {
		return nil, err
	}
	defer release()
	if onStep == nil {
		onStep = func(Step) {}
	}
	step := func(phase, msg string) { onStep(Step{Phase: phase, Message: msg, At: time.Now().Format(time.RFC3339)}) }

	root, rootErr := opts.installRoot()
	if rootErr != nil {
		return nil, rootErr
	}
	if pending, err := readRollbackPending(root); err != nil || pending != nil {
		return nil, &Error{Reason: "rollback_pending", Message: "the previous rollback has not finished; retry the rollback first, then update"}
	}
	if err := recoverAdoption(ctx, opts); err != nil {
		return nil, err
	}
	snap, err := check(ctx, opts)
	if err != nil {
		return nil, err
	}
	start := time.Now()

	// A refusal is not the end of the timeline: the page renders these lines, so the first
	// one says what was looked at even when the answer is "nothing to do".
	step("preflight", fmt.Sprintf("checking install directory %s (local branch %s / update source %s / commit %s)", snap.Root, snap.Branch, snap.sourceLabel(), snap.Commit))

	if !snap.Installed {
		return nil, &Error{Reason: "not_a_repo", Message: "this directory is not a git work tree; automatic update is unavailable"}
	}
	if snap.CheckError != "" {
		return nil, &Error{Reason: "check_failed", Message: "update check failed: " + snap.CheckError}
	}
	if len(snap.BlockingChanges) > 0 {
		items := make([]string, 0, len(snap.BlockingChanges))
		for _, c := range snap.BlockingChanges {
			items = append(items, fmt.Sprintf("%s (%s)", c.Path, c.Status))
		}
		return nil, &Error{
			Reason:  "local_source_edits",
			Message: fmt.Sprintf("%d source files carry local edits; the automatic update will not overwrite them. Commit or revert these files before updating.", len(items)),
			Items:   items,
		}
	}
	if snap.Diverged {
		return nil, &Error{
			Reason:  "diverged",
			Message: fmt.Sprintf("local is %d commits ahead and %d behind: this is a merge rather than a download; resolve it manually and retry.", snap.Ahead, snap.Behind),
		}
	}

	res := &Result{
		FromCommit: snap.Commit, ToCommit: snap.Commit,
		BinaryPath: filepath.Join(snap.Root, opts.binaryName()),
	}
	if buildPending(snap.Root) && opts.binaryName() != "none" {
		if st, ok := readState(snap.Root); ok {
			res.BackupDir = st.BackupDir
		}
		if err := finishPendingBuild(ctx, snap, opts, res, step); err != nil {
			res.Duration = time.Since(start).Round(time.Millisecond).String()
			return res, err
		}
	}
	if !snap.UpdateAvailable {
		// Nothing to move - but the installation's own bookkeeping still gets aligned: a
		// version left over from a tree that was updated before this behaviour existed (or
		// from a hand edit) is corrected here instead of waiting for the next commit.
		versionBefore, versionAfter, versionErr := syncVersionToConfig(snap.Root)
		versionStep(step, versionBefore, versionAfter, versionErr)
		res.Duration = time.Since(start).Round(time.Millisecond).String()
		return res, nil
	}

	step("fetch", fmt.Sprintf("%s (default branch %s) has %d new commits: %s → %s", snap.sourceLabel(), snap.TargetBranch, snap.Behind, snap.Commit, snap.RemoteCommit))

	// Put operator-owned content aside, and clear the paths the incoming change would
	// write to, so the fast-forward cannot be refused by "your local changes would be
	// overwritten". Nothing here deletes a file the operator made: every path is copied
	// out first and copied back after.
	root = snap.Root
	ref := snap.sourceRef()
	backupDir, kept, deleted, err := stashProtected(ctx, root, ref)
	if err != nil {
		return nil, err
	}
	step("protect", fmt.Sprintf("staged %d local content files (roles/skills/tools/data/config); restored once the update finishes", len(kept)))

	previousCommit, err := gitCmd(ctx, root, "rev-parse", "HEAD")
	if err != nil {
		restoreProtected(backupDir, kept)
		return nil, err
	}
	updatedCommit, err := gitCmd(ctx, root, "rev-parse", ref)
	if err != nil {
		restoreProtected(backupDir, kept)
		return nil, err
	}
	bin := filepath.Join(root, opts.binaryName())
	pending := buildPendingState{
		Commit:         updatedCommit,
		PreviousCommit: previousCommit,
		BackupDir:      backupDir,
		KeptContent:    kept,
		DeletedContent: deleted,
	}
	pending.PreviousBinaryAbsent = !fileExists(bin)
	if opts.binaryName() != "none" && fileExists(bin) {
		pending.PreviousBinarySHA256, err = fileSHA256(bin)
		if err != nil {
			restoreProtected(backupDir, kept)
			return nil, err
		}
	}
	if opts.binaryName() != "none" {
		if err := writeBuildPending(root, pending); err != nil {
			return nil, &Error{Reason: "state_unwritable", Message: "failed to write the pending-build state: " + err.Error()}
		}
	}
	if err := removeProtected(root, kept); err != nil {
		if restoreErr := restoreProtected(backupDir, kept); restoreErr != nil {
			return nil, &Error{Reason: "backup_failed", Message: fmt.Sprintf("%v; restoring the local content also failed: %v", err, restoreErr)}
		}
		_ = clearBuildPending(root)
		return nil, err
	}

	if out, err := gitCmd(ctx, root, "merge", "--ff-only", "--no-verify", ref); err != nil {
		if restoreErr := restoreProtected(backupDir, kept); restoreErr != nil {
			return nil, &Error{Reason: "backup_failed", Message: fmt.Sprintf("fast-forward merge failed; restoring the local content also failed: %v", restoreErr)}
		}
		_ = clearBuildPending(root)
		return nil, &Error{Reason: "merge_failed", Message: fmt.Sprintf("fast-forward merge failed: %v\n%s", err, strings.TrimSpace(out))}
	}
	// The merge wrote upstream's version of the content paths; the operator's copies win
	// them back, which is what "update the code, keep my work" means.
	if err := restoreProtected(backupDir, kept); err != nil {
		return nil, err
	}

	if err := restoreDeleted(root, deleted); err != nil {
		return nil, err
	}

	files, _ := gitRaw(ctx, root, "diff", "--name-only", "-z", previousCommit+".."+updatedCommit)
	res.FromCommit = snap.Commit
	res.ToCommit = snap.RemoteCommit
	res.Commits = snap.Behind
	res.KeptContent = kept
	res.BackupDir = backupDir
	res.FilesTouched = len(nulList(files))
	res.BinaryPath = bin

	// The new code carries its own release version; the live config (which no merge ever
	// touches) gets it now, so the page header and the static cache buster follow the code.
	// The pair is recorded in the marker first: a crash during the write below would
	// otherwise leave the new number on disk with nothing saying what it replaced.
	pending.VersionBefore, pending.VersionAfter = plannedVersionChange(root)
	if opts.binaryName() != "none" {
		if err := writeBuildPending(root, pending); err != nil {
			return res, &Error{Reason: "state_unwritable", Message: "failed to update the pending-build state: " + err.Error()}
		}
	}
	versionBefore, versionAfter, versionErr := syncVersionToConfig(root)
	versionStep(step, versionBefore, versionAfter, versionErr)

	state := State{
		PreviousCommit: previousCommit,
		UpdatedCommit:  updatedCommit,
		BackupDir:      backupDir,
		VersionBefore:  versionBefore,
		VersionAfter:   versionAfter,
	}
	if opts.binaryName() != "none" {
		state.BinaryCommit = previousCommit
		state.BinarySHA256 = pending.PreviousBinarySHA256
		state.PreviousBinarySHA256 = pending.PreviousBinarySHA256
		state.PreviousBinaryAbsent = pending.PreviousBinaryAbsent
	}
	if err := writeState(root, state); err != nil {
		return res, &Error{Reason: "state_unwritable", Message: "failed to write the update state file; rollback will be unavailable: " + err.Error()}
	}

	if opts.binaryName() == "none" {
		step("build", "rebuild skipped as requested; the source is updated, and an external build process produces the binary after restart")
		res.NeedsRestart = true
		res.Duration = time.Since(start).Round(time.Millisecond).String()
		return res, nil
	}
	if !snap.CanBuild {
		res.NeedsRestart = true
		res.Duration = time.Since(start).Round(time.Millisecond).String()
		return res, &Error{
			Reason:  "no_toolchain",
			Message: "the source is updated, but this machine has no Go toolchain and cannot build automatically. Install go and click update once more to get the binary.",
		}
	}
	if err := buildAndSwap(ctx, root, opts, res, step, updatedCommit, &state, pending); err != nil {
		res.Duration = time.Since(start).Round(time.Millisecond).String()
		return res, err
	}
	step("done", fmt.Sprintf("updated %d commits and swapped in the binary: %s → %s", res.Commits, res.FromCommit, res.ToCommit))
	res.Duration = time.Since(start).Round(time.Millisecond).String()
	return res, nil
}

// finishPendingBuild compiles the binary a previous update left missing. The marker carries
// the target commit and hashes around the swap, so a retry can distinguish "still owed"
// from "the swap finished and only marker cleanup failed" without overwriting .prev.
func finishPendingBuild(ctx context.Context, snap *Snapshot, opts Options, res *Result, step func(string, string)) error {
	pending, present, err := readBuildPending(snap.Root)
	if err != nil {
		return &Error{Reason: "state_unreadable", Message: "failed to read the pending-build state: " + err.Error()}
	}
	if !present {
		return nil
	}
	head, err := gitCmd(ctx, snap.Root, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	bin := filepath.Join(snap.Root, opts.binaryName())
	state, hasState := readState(snap.Root)
	if pending.Legacy && hasState {
		updated, resolveErr := gitCmd(ctx, snap.Root, "rev-parse", state.UpdatedCommit)
		if resolveErr != nil {
			return &Error{Reason: "bad_state", Message: "the update commit named by the old pending-build marker does not exist: " + state.UpdatedCommit}
		}
		if updated != head {
			return &Error{Reason: "build_state_mismatch", Message: fmt.Sprintf("the old pending-build marker belongs to %s, but the current source is already %s; refusing to reinterpret the rollback record", shortHash(updated), shortHash(head))}
		}
		pending.Commit = updated
		pending.PreviousCommit = state.PreviousCommit
		pending.BackupDir = state.BackupDir
		pending.VersionBefore = state.VersionBefore
		pending.VersionAfter = state.VersionAfter
		pending.PreviousBinarySHA256 = state.PreviousBinarySHA256
		pending.PreviousBinaryAbsent = state.PreviousBinaryAbsent
		if state.BinaryCommit != "" {
			binaryCommit, resolveErr := gitCmd(ctx, snap.Root, "rev-parse", state.BinaryCommit)
			previous, previousErr := gitCmd(ctx, snap.Root, "rev-parse", state.PreviousCommit)
			if resolveErr != nil || previousErr != nil || (binaryCommit != updated && binaryCommit != previous) {
				return &Error{Reason: "bad_state", Message: "the old recovery record's binary commit does not belong to this update; the current state is preserved"}
			}
			if state.BinarySHA256 != "" {
				currentHash, hashErr := fileSHA256(bin)
				if hashErr != nil || currentHash != state.BinarySHA256 {
					return &Error{Reason: "binary_changed", Message: "the current binary does not match the old recovery record; the current state is preserved"}
				}
			}
			if binaryCommit == updated {
				pending.BinarySHA256 = state.BinarySHA256
			} else {
				pending.PreviousBinarySHA256 = state.BinarySHA256
			}
		}
	}

	if pending.Commit == "" {
		if hasState && state.BinaryCommit != "" && state.BinarySHA256 != "" {
			binaryCommit, commitErr := gitCmd(ctx, snap.Root, "rev-parse", state.BinaryCommit)
			binaryHash, hashErr := fileSHA256(bin)
			if commitErr == nil && hashErr == nil && binaryCommit == head && binaryHash == state.BinarySHA256 {
				if err := clearBuildPending(snap.Root); err != nil {
					return &Error{Reason: "state_unwritable", Message: "the binary is already the current version, but the pending-build marker cannot be cleared: " + err.Error()}
				}
				res.BinaryBuilt = true
				if fileExists(bin + ".prev") {
					res.PrevBinary = bin + ".prev"
				}
				res.NeedsRestart = true
				return nil
			}
		}
		if revision := binaryRevision(bin); revision != "" {
			resolved, resolveErr := gitCmd(ctx, snap.Root, "rev-parse", revision)
			if resolveErr == nil && resolved == head {
				if err := clearBuildPending(snap.Root); err != nil {
					return &Error{Reason: "state_unwritable", Message: "the binary is already the current version, but the old pending-build marker cannot be cleared: " + err.Error()}
				}
				res.BinaryBuilt = true
				if fileExists(bin + ".prev") {
					res.PrevBinary = bin + ".prev"
				}
				res.NeedsRestart = true
				return nil
			}
		}
		pending.Commit = head
	}

	target, err := gitCmd(ctx, snap.Root, "rev-parse", pending.Commit)
	if err != nil {
		return &Error{Reason: "bad_state", Message: "the commit named in the pending-build state does not exist: " + pending.Commit}
	}
	fileEvidence := pending
	if target != head {
		// Before the source move only the original binary/absence is valid.
		fileEvidence.BinarySHA256 = ""
	}
	if err := validateBuildFiles(bin, fileEvidence); err != nil {
		return &Error{Reason: "bad_state", Message: "the recovery evidence is insufficient; the binary and the record are preserved: " + err.Error()}
	}
	if target != head {
		if pending.PreviousCommit != "" {
			previous, previousErr := gitCmd(ctx, snap.Root, "rev-parse", pending.PreviousCommit)
			if previousErr == nil && previous == head {
				if err := restoreInterruptedProtected(ctx, snap.Root, pending, false); err != nil {
					return err
				}
				if err := clearBuildPending(snap.Root); err != nil {
					return &Error{Reason: "state_unwritable", Message: "local content restored, but the pending-build marker cannot be cleared: " + err.Error()}
				}
				return nil
			}
		}
		return &Error{Reason: "build_state_mismatch", Message: fmt.Sprintf("the pending-build target is %s, but the current source is %s; refusing to overwrite the binary with the wrong version", shortHash(target), shortHash(head))}
	}
	if err := restoreInterruptedProtected(ctx, snap.Root, pending, true); err != nil {
		return err
	}
	if len(pending.KeptContent) > 0 {
		res.KeptContent = append([]string(nil), pending.KeptContent...)
		res.BackupDir = pending.BackupDir
	}
	if pending.BinarySHA256 != "" && fileExists(bin) {
		currentHash, hashErr := fileSHA256(bin)
		if hashErr == nil && currentHash == pending.BinarySHA256 {
			if pending.PreviousCommit != "" {
				state = State{
					PreviousCommit:       pending.PreviousCommit,
					UpdatedCommit:        target,
					BackupDir:            pending.BackupDir,
					BinaryCommit:         target,
					BinarySHA256:         currentHash,
					PreviousBinarySHA256: pending.PreviousBinarySHA256,
					PreviousBinaryAbsent: pending.PreviousBinaryAbsent,
					VersionBefore:        pending.VersionBefore,
					VersionAfter:         pending.VersionAfter,
				}
				if err := writeState(snap.Root, state); err != nil {
					return &Error{Reason: "state_unwritable", Message: "the binary is swapped in, but the rollback state cannot be appended: " + err.Error()}
				}
			}
			if err := clearBuildPending(snap.Root); err != nil {
				return &Error{Reason: "state_unwritable", Message: "the binary is swapped in, but the pending-build marker cannot be cleared: " + err.Error()}
			}
			res.BinaryBuilt = true
			if fileExists(bin + ".prev") {
				res.PrevBinary = bin + ".prev"
			}
			res.NeedsRestart = true
			return nil
		}
	}

	if !snap.CanBuild {
		return &Error{
			Reason:  "no_toolchain",
			Message: fmt.Sprintf("the source is in place (%s) but the binary is still old: this machine has no Go toolchain and cannot rebuild. Install go and click update once more to get the binary.", shortHash(head)),
		}
	}
	var targetState *State
	if pending.PreviousCommit != "" {
		state = State{
			PreviousCommit:       pending.PreviousCommit,
			UpdatedCommit:        target,
			BackupDir:            pending.BackupDir,
			BinaryCommit:         pending.PreviousCommit,
			BinarySHA256:         pending.PreviousBinarySHA256,
			PreviousBinarySHA256: pending.PreviousBinarySHA256,
			PreviousBinaryAbsent: pending.PreviousBinaryAbsent,
			VersionBefore:        pending.VersionBefore,
			VersionAfter:         pending.VersionAfter,
		}
		targetState = &state
	} else if hasState {
		targetState = &state
	}
	if err := buildAndSwap(ctx, snap.Root, opts, res, step, target, targetState, pending); err != nil {
		return err
	}
	step("done", fmt.Sprintf("the source is in place (%s); this run supplied the binary", shortHash(head)))
	return nil
}

// buildAndSwap compiles the tree and moves the new binary into place, keeping the previous
// one as .prev. The marker records the compiled file's hash before the rename; after a crash
// a retry can prove whether the swap happened instead of doing it twice.
func buildAndSwap(ctx context.Context, root string, opts Options, res *Result, step func(string, string), commit string, state *State, pending buildPendingState) error {
	step("build", "starting the binary build (the first run downloads dependencies and may take several minutes)")
	if err := verifyBuildSource(ctx, root, commit); err != nil {
		return err
	}
	staging := filepath.Join(root, ".update-staging")
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return &Error{Reason: "staging_failed", Message: err.Error()}
	}
	defer os.RemoveAll(staging)

	newBin := filepath.Join(staging, opts.binaryName())
	if err := build(ctx, root, newBin); err != nil {
		return &Error{Reason: "build_failed", Message: "build failed; the binary stays at its previous version:\n" + err.Error()}
	}
	if err := verifyBuildSource(ctx, root, commit); err != nil {
		return err
	}
	newHash, err := fileSHA256(newBin)
	if err != nil {
		return &Error{Reason: "build_failed", Message: "failed to verify the new binary: " + err.Error()}
	}
	bin := filepath.Join(root, opts.binaryName())
	if err := validateBuildFiles(bin, pending); err != nil {
		return &Error{Reason: "binary_changed", Message: "the recovery evidence no longer matches after the build; the current state is preserved: " + err.Error()}
	}
	if pending.Legacy && fileExists(bin) && fileExists(bin+".prev") {
		currentHash, currentErr := fileSHA256(bin)
		previousHash, previousErr := fileSHA256(bin + ".prev")
		if currentErr == nil && previousErr == nil && currentHash == newHash {
			if pending.PreviousBinarySHA256 != "" && previousHash != pending.PreviousBinarySHA256 {
				return &Error{Reason: "binary_changed", Message: "the old binary backup does not match the recovery record; the current state is preserved, refusing to rewrite the rollback record"}
			}
			res.BinaryBuilt = true
			res.PrevBinary = bin + ".prev"
			res.NeedsRestart = true
			if state != nil {
				state.BinaryCommit = commit
				state.BinarySHA256 = currentHash
				state.PreviousBinarySHA256 = previousHash
				if err := writeState(root, *state); err != nil {
					return &Error{Reason: "state_unwritable", Message: "the binary is already the current version, but the rollback state cannot be updated: " + err.Error()}
				}
			}
			if err := clearBuildPending(root); err != nil {
				return &Error{Reason: "state_unwritable", Message: "the binary is already the current version, but the old pending-build marker cannot be cleared: " + err.Error()}
			}
			return nil
		}
	}
	// A legacy marker without hashes is ambiguous when a rebuild differs from the
	// installed binary. Only a clean VCS stamp for the previous commit can prove
	// that another swap will preserve the right rollback binary.
	if (pending.Legacy || pending.PreviousCommit != "") && pending.PreviousBinarySHA256 == "" && !pending.PreviousBinaryAbsent {
		revision := binaryRevision(bin)
		previous, resolveErr := gitCmd(ctx, root, "rev-parse", pending.PreviousCommit)
		if revision == "" || resolveErr != nil || revision != previous {
			return &Error{Reason: "binary_state_unknown", Message: "the old recovery record lacks the binary hash; cannot confirm the current binary belongs to the pre-update version. The current binary and .prev are preserved; inspect them manually before resuing"}
		}
		hash, err := fileSHA256(bin)
		if err != nil {
			return &Error{Reason: "binary_state_unknown", Message: "failed to verify the pre-update binary: " + err.Error()}
		}
		pending.PreviousBinarySHA256 = hash
	}
	if pending.PreviousBinarySHA256 != "" && fileExists(bin) {
		currentHash, hashErr := fileSHA256(bin)
		if hashErr != nil || currentHash != pending.PreviousBinarySHA256 {
			return &Error{Reason: "binary_changed", Message: "the installed binary changed during the build; refusing to overwrite. Make sure no other update is running"}
		}
	}
	pending.Commit = commit
	pending.BinarySHA256 = newHash
	if err := writeBuildPending(root, pending); err != nil {
		return &Error{Reason: "state_unwritable", Message: "failed to record the binary awaiting swap: " + err.Error()}
	}
	prev, err := installBinary(newBin, bin)
	if err != nil {
		return &Error{Reason: "swap_failed", Message: err.Error()}
	}
	res.BinaryBuilt = true
	res.PrevBinary = prev
	res.NeedsRestart = true
	if state != nil {
		state.BinaryCommit = commit
		state.BinarySHA256 = newHash
		state.PreviousBinarySHA256 = pending.PreviousBinarySHA256
		state.PreviousBinaryAbsent = pending.PreviousBinaryAbsent
		if err := writeState(root, *state); err != nil {
			return &Error{Reason: "state_unwritable", Message: "the binary is swapped in, but the rollback state cannot be updated: " + err.Error()}
		}
	}
	if err := clearBuildPending(root); err != nil {
		return &Error{Reason: "state_unwritable", Message: "the binary is swapped in, but the pending-build marker cannot be cleared: " + err.Error()}
	}
	return nil
}

// stashProtected copies every operator-owned file that stands in the way of the
// fast-forward into backupDir. The caller persists the recovery record before removing
// these paths, so a crash can never strand content without saying where its copy lives.
func stashProtected(ctx context.Context, root, ref string) (backupDir string, kept, deleted []string, err error) {
	ts := time.Now().Format("20060102_150405")
	backupDir = ""

	tracked, _, changeErr := localChanges(ctx, root)
	if changeErr != nil {
		return "", nil, nil, changeErr
	}
	incoming, err := gitRaw(ctx, root, "diff", "--name-only", "-z", "HEAD.."+ref)
	if err != nil {
		return "", nil, nil, &Error{Reason: "diff_failed", Message: err.Error()}
	}
	willWrite := map[string]bool{}
	for _, f := range nulList(incoming) {
		willWrite[filepath.ToSlash(f)] = true
	}

	if err := validateIncomingFiles(ctx, root, willWrite); err != nil {
		return "", nil, nil, err
	}

	// A locally modified content file would refuse the merge; an untracked content file
	// whose path upstream now adds would also refuse it. Both are put aside.
	targets := map[string]bool{}
	for _, c := range tracked {
		if c.Protected && willWrite[c.Path] {
			if c.Status == "deleted" {
				deleted = append(deleted, c.Path)
			} else {
				targets[c.Path] = true
			}
		}
	}
	for _, p := range untrackedUnder(ctx, root, willWrite) {
		targets[p] = true
	}

	sort.Strings(deleted)
	for p := range targets {
		if err := validateFilePath(root, p); err != nil {
			return backupDir, kept, deleted, err
		}
		abs := filepath.Join(root, p)
		info, statErr := os.Stat(abs)
		if statErr != nil || !info.Mode().IsRegular() {
			return backupDir, kept, deleted, &Error{Reason: "backup_failed", Message: "local files changed before the backup; update not performed: " + p}
		}
		if backupDir == "" {
			backupDir, err = newBackupDir(root, ts)
			if err != nil {
				return "", nil, deleted, err
			}
		}
		if err := copyFile(abs, filepath.Join(backupDir, p), info.Mode()); err != nil {
			return backupDir, kept, deleted, &Error{Reason: "backup_failed", Message: fmt.Sprintf("backup of %s failed: %v", p, err)}
		}
		kept = append(kept, p)
	}
	if len(kept) > 0 {
		sort.Strings(kept)
		return backupDir, kept, deleted, nil
	}
	// Nothing to put aside: don't leave an empty backup directory behind.
	return "", nil, deleted, nil
}

func removeProtected(root string, kept []string) error {
	for _, rel := range kept {
		if err := os.Remove(filepath.Join(root, filepath.FromSlash(rel))); err != nil && !os.IsNotExist(err) {
			return &Error{Reason: "backup_failed", Message: fmt.Sprintf("failed to move %s aside: %v", rel, err)}
		}
	}
	return nil
}

// stashProtectedEdits copies the operator's local edits to protected files out of the
// tree before a rollback resets it. Unlike an update's stash it leaves the working copy
// alone - the reset is what replaces those files - because reset --hard does not know
// which local changes are the update's and which are the operator's. A file deleted by
// hand carries no content to copy, so its name is recorded instead and the deletion is
// re-applied after the reset.
func stashProtectedEdits(ctx context.Context, root, target string, changed []Change) (backupDir string, kept, deleted []string, err error) {
	ts := time.Now().Format("20060102_150405")
	files := map[string]bool{}
	for _, c := range changed {
		if !c.Protected {
			continue
		}
		if c.Status == "deleted" {
			deleted = append(deleted, c.Path)
			continue
		}
		files[c.Path] = true
	}

	incoming, diffErr := gitRaw(ctx, root, "diff", "--name-only", "-z", "HEAD.."+target)
	if diffErr != nil {
		return "", nil, nil, &Error{Reason: "diff_failed", Message: diffErr.Error()}
	}
	willWrite := map[string]bool{}
	for _, p := range nulList(incoming) {
		willWrite[filepath.ToSlash(p)] = true
	}
	if err := validateIncomingFiles(ctx, root, willWrite); err != nil {
		return "", nil, nil, err
	}
	for _, p := range untrackedUnder(ctx, root, willWrite) {
		files[p] = true
	}

	for p := range files {
		if err := validateFilePath(root, p); err != nil {
			return backupDir, kept, deleted, err
		}
		abs := filepath.Join(root, filepath.FromSlash(p))
		info, statErr := os.Stat(abs)
		if statErr != nil || !info.Mode().IsRegular() {
			return backupDir, kept, deleted, &Error{Reason: "backup_failed", Message: "local files changed before the backup; rollback not performed: " + p}
		}
		if backupDir == "" {
			backupDir, err = newBackupDir(root, "rollback_"+ts)
			if err != nil {
				return "", nil, deleted, err
			}
		}
		if err := copyFile(abs, filepath.Join(backupDir, filepath.FromSlash(p)), info.Mode()); err != nil {
			return backupDir, kept, deleted, &Error{Reason: "backup_failed", Message: fmt.Sprintf("backup of %s failed: %v", p, err)}
		}
		kept = append(kept, p)
	}
	sort.Strings(kept)
	sort.Strings(deleted)
	return backupDir, kept, deleted, nil
}

// restoreProtected puts the named operator-owned copies back over whatever the merge
// wrote. The list is the one stashProtected returned: an adoption parks replaced
// non-protected files in the same backup directory (under overwritten/), and those must
// NOT be restored over the target repository's version.
func restoreProtected(backupDir string, kept []string) error {
	if backupDir == "" {
		return nil
	}
	root := filepath.Join(filepath.Dir(backupDir), "..")
	for _, rel := range kept {
		abs := filepath.Join(backupDir, filepath.FromSlash(rel))
		info, err := os.Stat(abs)
		if err != nil {
			return err
		}
		if err := copyFile(abs, filepath.Join(root, filepath.FromSlash(rel)), info.Mode()); err != nil {
			return err
		}
	}
	return nil
}

func restoreInterruptedProtected(ctx context.Context, root string, pending buildPendingState, merged bool) error {
	if merged {
		// Reapply only untouched copies written by the merge. A file recreated
		// by the operator after the crash belongs to them.
		for _, rel := range pending.DeletedContent {
			workHash, workErr := gitCmd(ctx, root, "hash-object", "--", rel)
			headHash, headErr := gitCmd(ctx, root, "rev-parse", "HEAD:"+rel)
			if workErr == nil && headErr == nil && workHash == headHash {
				if err := restoreDeleted(root, []string{rel}); err != nil {
					return err
				}
			}
		}
	}
	if pending.BackupDir == "" || len(pending.KeptContent) == 0 {
		return nil
	}
	for _, rel := range pending.KeptContent {
		rel = filepath.ToSlash(rel)
		dst := filepath.Join(root, filepath.FromSlash(rel))
		restore := false
		if !fileExists(dst) {
			if !merged {
				restore = true
			} else {
				_, headErr := gitCmd(ctx, root, "rev-parse", "HEAD:"+rel)
				restore = headErr != nil
			}
		} else if merged {
			workHash, workErr := gitCmd(ctx, root, "hash-object", "--", rel)
			headHash, headErr := gitCmd(ctx, root, "rev-parse", "HEAD:"+rel)
			restore = workErr == nil && headErr == nil && workHash == headHash
		}
		if !restore {
			continue
		}
		src := filepath.Join(pending.BackupDir, filepath.FromSlash(rel))
		info, err := os.Stat(src)
		if err != nil {
			return &Error{Reason: "backup_missing", Message: fmt.Sprintf("the backup of the content awaiting restoration does not exist: %s", rel)}
		}
		if err := copyFile(src, dst, info.Mode()); err != nil {
			return &Error{Reason: "backup_failed", Message: fmt.Sprintf("failed to restore %s: %v", rel, err)}
		}
	}
	return nil
}

// untrackedUnder lists untracked files under the given repository paths - the operator's
// own additions, which must be backed up but never deleted by an update.
func untrackedUnder(ctx context.Context, root string, willWrite map[string]bool) []string {
	// With no exclude patterns ls-files includes ignored files too. git merge
	// may overwrite an ignored config or database without an untracked warning.
	out, err := gitRaw(ctx, root, "ls-files", "--others", "-z")
	if err != nil {
		return nil
	}
	var list []string
	for _, entry := range nulList(out) {
		p := filepath.ToSlash(entry)
		if willWrite[p] && Protected(p) {
			list = append(list, p)
		}
	}
	return list
}

// Rollback returns an installation to the commit it came from, using the binary kept
// before the swap. It refuses unless HEAD is still exactly what the update wrote, so it
// can never discard work done after that update.
func Rollback(ctx context.Context, opts Options) (*Result, error) {
	release, err := LockInstall(opts)
	if err != nil {
		return nil, err
	}
	defer release()
	root, err := opts.installRoot()
	if err != nil {
		return nil, err
	}

	interrupted, err := readRollbackPending(root)
	if err != nil {
		return nil, &Error{Reason: "state_unreadable", Message: "failed to read the rollback recovery record: " + err.Error()}
	}
	if interrupted != nil {
		if err := repositoryRoot(ctx, root); err != nil {
			return nil, err
		}
		head, err := gitCmd(ctx, root, "rev-parse", "HEAD")
		if err != nil {
			return nil, err
		}
		if head == interrupted.State.PreviousCommit {
			return finishRollback(ctx, opts, *interrupted)
		}
		if head != interrupted.State.UpdatedCommit {
			return nil, &Error{Reason: "moved_since_update", Message: "HEAD changed after the rollback interruption; refusing to overwrite later commits"}
		}
	}
	st, ok, stateErr := recoveryState(root)
	if stateErr != nil {
		return nil, &Error{Reason: "state_unreadable", Message: "failed to read the rollback state: " + stateErr.Error()}
	}
	if !ok {
		return nil, &Error{Reason: "no_state", Message: "no update record available to roll back"}
	}
	if err := repositoryRoot(ctx, root); err != nil {
		return nil, err
	}
	bin := filepath.Join(root, opts.binaryName())
	restoreBinary := true
	if st.BinaryCommit != "" {
		binaryCommit, resolveErr := gitCmd(ctx, root, "rev-parse", st.BinaryCommit)
		if resolveErr != nil {
			return nil, &Error{Reason: "bad_state", Message: "the binary commit in the update record does not exist: " + st.BinaryCommit}
		}
		previousCommit, resolveErr := gitCmd(ctx, root, "rev-parse", st.PreviousCommit)
		if resolveErr != nil {
			return nil, &Error{Reason: "bad_state", Message: "the rollback commit in the update record does not exist: " + st.PreviousCommit}
		}
		updatedCommit, resolveErr := gitCmd(ctx, root, "rev-parse", st.UpdatedCommit)
		if resolveErr != nil {
			return nil, &Error{Reason: "bad_state", Message: "the update commit in the update record does not exist: " + st.UpdatedCommit}
		}
		switch binaryCommit {
		case previousCommit:
			restoreBinary = false
			if st.BinarySHA256 != "" {
				got, hashErr := fileSHA256(bin)
				if hashErr != nil || got != st.BinarySHA256 {
					return nil, &Error{Reason: "binary_changed", Message: "the current binary does not match the pre-update record; refusing to roll the source back when the versions cannot be paired"}
				}
			}
		case updatedCommit:
			restoreBinary = true
		default:
			return nil, &Error{Reason: "bad_state", Message: "the binary commit in the update record does not belong to this update"}
		}
	}
	if restoreBinary {
		if st.PreviousBinaryAbsent {
			return nil, &Error{Reason: "no_binary", Message: "there was no binary before this update; the .prev another update left cannot be used to roll back"}
		}
		if !fileExists(bin + ".prev") {
			return nil, &Error{Reason: "no_binary", Message: "the previous update left no old binary; the binary cannot be rolled back (the source can still be handled with git)"}
		}
		if st.PreviousBinarySHA256 != "" {
			got, hashErr := fileSHA256(bin + ".prev")
			if hashErr != nil || got != st.PreviousBinarySHA256 {
				return nil, &Error{Reason: "binary_changed", Message: "the rollback binary does not match the update record; refusing to take the source back to a version that cannot be paired"}
			}
		}
	}
	head, err := gitCmd(ctx, root, "rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	updated, err := gitCmd(ctx, root, "rev-parse", st.UpdatedCommit)
	if err != nil {
		return nil, &Error{Reason: "bad_state", Message: "the commit in the update record does not exist in this repository: " + st.UpdatedCommit}
	}
	if head != updated {
		return nil, &Error{
			Reason:  "moved_since_update",
			Message: fmt.Sprintf("HEAD has changed since that update (it is now %s); the rollback would only undo to the pre-update commit, so it is refused.", shortHash(head)),
		}
	}

	start := time.Now()
	changes, blocking, changeErr := localChanges(ctx, root)
	if changeErr != nil {
		return nil, changeErr
	}
	if len(blocking) > 0 {
		items := make([]string, 0, len(blocking))
		for _, c := range blocking {
			items = append(items, c.Path)
		}
		return nil, &Error{Reason: "local_source_edits", Message: "there are local source edits the rollback would overwrite", Items: items}
	}

	// Content the operator owns is not the rollback's to discard either: reset --hard takes
	// every local change to tracked files, so protected files that differ right now - edits
	// made before the update and edits made after it alike - are copied aside first and set
	// back on top of the old commit. An untracked file at a path the old commit tracked is
	// included too: reset would otherwise overwrite a role recreated after upstream deleted it.
	backupDir, kept, deleted, err := stashProtectedEdits(ctx, root, st.PreviousCommit, changes)
	if err != nil {
		return nil, err
	}

	pending := rollbackPendingState{State: st, BackupDir: backupDir, Kept: kept, Deleted: deleted, RestoreBinary: restoreBinary}
	if fileExists(bin) {
		pending.LiveSHA256, err = fileSHA256(bin)
		if err != nil {
			return nil, err
		}
	}
	if restoreBinary {
		pending.PreviousSHA256, err = fileSHA256(bin + ".prev")
		if err != nil {
			return nil, err
		}
	}
	if err := writeRollbackPending(root, pending); err != nil {
		return nil, &Error{Reason: "state_unwritable", Message: "failed to write the rollback recovery record; source and binary untouched: " + err.Error()}
	}
	if _, err := gitCmd(ctx, root, "reset", "--hard", "--quiet", st.PreviousCommit); err != nil {
		return nil, &Error{Reason: "reset_failed", Message: err.Error()}
	}
	res, err := finishRollback(ctx, opts, pending)
	if res != nil {
		res.Duration = time.Since(start).Round(time.Millisecond).String()
	}
	return res, err

}

func shortHash(h string) string {
	if len(h) > 8 {
		return h[:8]
	}
	return h
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func restoreDeleted(root string, deleted []string) error {
	for _, rel := range deleted {
		if err := os.Remove(filepath.Join(root, filepath.FromSlash(rel))); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func newBackupDir(root, prefix string) (string, error) {
	base := filepath.Join(root, ".update-backup")
	if err := os.MkdirAll(base, 0700); err != nil {
		return "", err
	}
	return os.MkdirTemp(base, prefix+"-")
}

func verifyBuildSource(ctx context.Context, root, commit string) error {
	head, err := gitCmd(ctx, root, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if head != commit {
		return &Error{Reason: "build_state_mismatch", Message: "the source commit changed during the build; refusing to swap in a binary that cannot be paired"}
	}
	_, blocking, err := localChanges(ctx, root)
	if err != nil {
		return err
	}
	if len(blocking) > 0 {
		return &Error{Reason: "local_source_edits", Message: "the source acquired local edits during the build; the binary and backups are preserved; resolve these edits first"}
	}
	return nil
}

func validateIncomingFiles(ctx context.Context, root string, paths map[string]bool) error {
	for rel := range paths {
		if err := validateFilePath(root, rel); err != nil {
			return err
		}
	}
	others, err := gitRaw(ctx, root, "ls-files", "--others", "-z")
	if err != nil {
		return err
	}
	for _, rel := range nulList(others) {
		if paths[rel] && !Protected(rel) {
			return &Error{Reason: "local_source_edits", Message: "the target commit would overwrite a local untracked source file; update refused: " + rel, Items: []string{rel}}
		}
	}
	return nil
}
