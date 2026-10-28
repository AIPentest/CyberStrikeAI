package update

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const rollbackPendingFile = ".update-rollback-pending.json"

type rollbackPendingState struct {
	State          State    `json:"state"`
	BackupDir      string   `json:"backup_dir"`
	Kept           []string `json:"kept"`
	Deleted        []string `json:"deleted"`
	RestoreBinary  bool     `json:"restore_binary"`
	LiveSHA256     string   `json:"live_sha256"`
	PreviousSHA256 string   `json:"previous_sha256"`
}

func readRollbackPending(root string) (*rollbackPendingState, error) {
	data, err := os.ReadFile(filepath.Join(root, rollbackPendingFile))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var pending rollbackPendingState
	if err := decodeRecoveryJSON(data, &pending); err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(data, &fields)
	if _, ok := fields["restore_binary"]; !ok {
		return nil, fmt.Errorf("the rollback recovery record lacks the binary restore decision")
	}
	if err := validateRollbackEvidence(root, pending); err != nil {
		return nil, err
	}
	return &pending, nil
}

func writeRollbackPending(root string, pending rollbackPendingState) error {
	data, err := json.Marshal(pending)
	if err != nil {
		return err
	}
	path := filepath.Join(root, rollbackPendingFile)
	if err := os.WriteFile(path+".tmp", data, 0600); err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}

// finishRollback can run again after a reset or binary rename was interrupted.
// Hashes distinguish the old live binary from the already-restored one.
func finishRollback(ctx context.Context, opts Options, pending rollbackPendingState) (*Result, error) {
	root, err := opts.installRoot()
	if err != nil {
		return nil, err
	}
	bin := filepath.Join(root, opts.binaryName())
	if !pending.RestoreBinary && pending.LiveSHA256 == "" {
		if _, err := os.Lstat(bin); !os.IsNotExist(err) {
			return nil, &Error{Reason: "binary_changed", Message: "the recovery record requires that no binary exist, but that is not the case; the record is preserved"}
		}
	}
	if pending.RestoreBinary {
		current, currentErr := fileSHA256(bin)
		if currentErr != nil || current != pending.PreviousSHA256 {
			if currentErr == nil && current != pending.LiveSHA256 {
				return nil, &Error{Reason: "binary_changed", Message: "the installed binary changed after the rollback interruption; the current state is preserved"}
			}
			previous, previousErr := fileSHA256(bin + ".prev")
			if previousErr != nil || previous != pending.PreviousSHA256 {
				return nil, &Error{Reason: "binary_changed", Message: "the rollback backup does not match the recovery record; the current state is preserved"}
			}
			if err := restorePrevBinary(bin); err != nil {
				return nil, &Error{Reason: "swap_failed", Message: err.Error()}
			}
		}
	} else if pending.LiveSHA256 != "" {
		current, err := fileSHA256(bin)
		if err != nil || current != pending.LiveSHA256 {
			return nil, &Error{Reason: "binary_changed", Message: "the installed binary changed after the rollback interruption; the current state is preserved"}
		}
	}
	content := buildPendingState{BackupDir: pending.BackupDir, KeptContent: pending.Kept, DeletedContent: pending.Deleted}
	if err := restoreInterruptedProtected(ctx, root, content, true); err != nil {
		return nil, err
	}
	if err := clearBuildPending(root); err != nil {
		return nil, err
	}
	if err := restoreVersionAfterRollback(root, pending.State); err != nil {
		return nil, &Error{Reason: "version_restore_failed", Message: "source and binary were rolled back, but restoring the version failed; fix the config write and retry the rollback: " + err.Error()}
	}
	if err := os.Remove(statePath(root)); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err := os.Remove(filepath.Join(root, rollbackPendingFile)); err != nil {
		return nil, err
	}
	return &Result{FromCommit: shortHash(pending.State.UpdatedCommit), ToCommit: shortHash(pending.State.PreviousCommit), KeptContent: pending.Kept, BinaryPath: bin, BinaryBuilt: pending.RestoreBinary, BackupDir: pending.BackupDir, NeedsRestart: true}, nil
}
