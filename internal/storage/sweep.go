package storage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"
)

// ErrInvalidSweepTarget indicates the sweep request did not name a concrete
// conversation or project to wipe.
var ErrInvalidSweepTarget = errors.New("sweep target requires scope conversation|project and a non-empty id")

// SweepTargetRequest describes an on-demand wipe of a single engagement's
// artifacts.
type SweepTargetRequest struct {
	// Scope is ScopeConversation or ScopeProject.
	Scope Scope
	// ID is the conversation or project ID whose artifacts are removed.
	ID string
	// DryRun true only counts; real deletion requires DryRun=false.
	DryRun bool
	// Trigger is recorded for logging/audit (e.g. "manual", "post-test").
	Trigger string
}

// SweepTarget immediately removes every artifact belonging to one conversation
// or project across all storage categories, ignoring retention age and the
// active-session grace period. It is the "I'm done with this test, wipe its
// files now" operation, so it bypasses the time-based protections that the
// scheduled Clean honors.
//
// Safety: deletion is irreversible, so callers must gate real deletion
// (DryRun=false) behind an explicit confirmation. Path confinement is enforced
// per unit via confined(), and the id is rejected if it contains any path
// separator or traversal, so a crafted id can never escape a category root.
func (c *Cleaner) SweepTarget(req SweepTargetRequest) (*Report, error) {
	id := strings.TrimSpace(req.ID)
	if id == "" || (req.Scope != ScopeConversation && req.Scope != ScopeProject) {
		return nil, ErrInvalidSweepTarget
	}
	// Reject any path-ish id up front; confined() is the second line of defense.
	if strings.ContainsAny(id, `/\`) || id == "." || id == ".." || strings.Contains(id, "..") {
		return nil, ErrInvalidSweepTarget
	}

	trigger := strings.TrimSpace(req.Trigger)
	if trigger == "" {
		trigger = "sweep"
	}

	if !c.running.CompareAndSwap(false, true) {
		return nil, ErrCleanupInProgress
	}
	defer c.running.Store(false)

	startedAt := c.now()
	rep := &Report{DryRun: req.DryRun, Trigger: trigger, StartedAt: startedAt}

	for _, cat := range categories() {
		root := c.rootOf(cat)
		cr := CategoryReport{
			Key:           cat.key,
			Label:         cat.label,
			Hint:          cat.hint,
			Root:          root,
			Enabled:       c.storageConfig().CategoryEnabled(cat.key),
			RetentionDays: c.storageConfig().CategoryRetentionDays(cat.key),
		}
		if root == "" {
			cr.Missing = true
			rep.Categories = append(rep.Categories, cr)
			continue
		}
		if _, err := os.Stat(root); err != nil {
			cr.Missing = true
			rep.Categories = append(rep.Categories, cr)
			continue
		}

		units, err := cat.scan(root)
		if err != nil {
			cr.Errors = append(cr.Errors, err.Error())
			rep.Categories = append(rep.Categories, cr)
			continue
		}

		for _, u := range units {
			// Only this engagement's own units; ScopeNone categories (logs,
			// C2, checkpoints) carry no session id and are never touched here.
			if u.Scope != req.Scope || u.Session != id {
				continue
			}
			cr.Units++
			cr.Bytes += u.Size
			if !confined(root, u.Path) {
				cr.SkippedUnsafe++
				continue
			}
			cr.ReclaimableUnits++
			cr.ReclaimableBytes += u.Size
			if req.DryRun {
				continue
			}
			if rmErr := removeUnit(u); rmErr != nil {
				cr.Errors = append(cr.Errors, fmt.Sprintf("%s: %v", filepath.Base(u.Path), rmErr))
				continue
			}
			cr.RemovedUnits++
			cr.FreedBytes += u.Size
		}

		if cat.pruneEmpty && cr.RemovedUnits > 0 {
			cr.RemovedEmptyDirs = pruneEmptyDirs(root, 2)
		}
		rep.Categories = append(rep.Categories, cr)
	}

	if rep.Categories == nil {
		rep.Categories = []CategoryReport{}
	}
	for _, cr := range rep.Categories {
		rep.Totals.Units += cr.Units
		rep.Totals.Bytes += cr.Bytes
		rep.Totals.ReclaimableUnits += cr.ReclaimableUnits
		rep.Totals.ReclaimableBytes += cr.ReclaimableBytes
		rep.Totals.RemovedUnits += cr.RemovedUnits
		rep.Totals.FreedBytes += cr.FreedBytes
		rep.Totals.Errors += len(cr.Errors)
	}
	rep.Filesystem = c.filesystem()
	rep.DurationMS = time.Since(startedAt).Milliseconds()

	if !req.DryRun && c.logger != nil {
		c.logger.Info("storage sweep-target executed",
			zap.String("scope", scopeName(req.Scope)),
			zap.String("id", id),
			zap.String("trigger", trigger),
			zap.Int("removed_units", rep.Totals.RemovedUnits),
			zap.Int64("freed_bytes", rep.Totals.FreedBytes))
	}
	c.invalidateCache()
	return rep, nil
}

// WipeAll removes every runtime artifact under every storage category root,
// ignoring retention age, session scope, and active-session protection. It is
// the ephemeral "leave nothing behind" wipe, intended to run on shutdown when
// ephemeral mode is enabled, so a finished run persists no files on disk.
//
// Safety: path confinement is still enforced per unit via confined(), so only
// paths genuinely under a category root are removed; the roots themselves are
// never deleted. Deletion is irreversible — callers gate real deletion
// (DryRun=false) behind explicit opt-in (ephemeral.wipe_on_exit).
func (c *Cleaner) WipeAll(dryRun bool) (*Report, error) {
	if !c.running.CompareAndSwap(false, true) {
		return nil, ErrCleanupInProgress
	}
	defer c.running.Store(false)

	startedAt := c.now()
	rep := &Report{DryRun: dryRun, Trigger: "ephemeral-wipe", StartedAt: startedAt}

	for _, cat := range categories() {
		root := c.rootOf(cat)
		cr := CategoryReport{
			Key:   cat.key,
			Label: cat.label,
			Hint:  cat.hint,
			Root:  root,
		}
		if root == "" {
			cr.Missing = true
			rep.Categories = append(rep.Categories, cr)
			continue
		}
		if _, err := os.Stat(root); err != nil {
			cr.Missing = true
			rep.Categories = append(rep.Categories, cr)
			continue
		}
		units, err := cat.scan(root)
		if err != nil {
			cr.Errors = append(cr.Errors, err.Error())
			rep.Categories = append(rep.Categories, cr)
			continue
		}
		for _, u := range units {
			cr.Units++
			cr.Bytes += u.Size
			if !confined(root, u.Path) {
				cr.SkippedUnsafe++
				continue
			}
			cr.ReclaimableUnits++
			cr.ReclaimableBytes += u.Size
			if dryRun {
				continue
			}
			if rmErr := removeUnit(u); rmErr != nil {
				cr.Errors = append(cr.Errors, fmt.Sprintf("%s: %v", filepath.Base(u.Path), rmErr))
				continue
			}
			cr.RemovedUnits++
			cr.FreedBytes += u.Size
		}
		if cat.pruneEmpty && cr.RemovedUnits > 0 {
			cr.RemovedEmptyDirs = pruneEmptyDirs(root, 2)
		}
		rep.Categories = append(rep.Categories, cr)
	}

	if rep.Categories == nil {
		rep.Categories = []CategoryReport{}
	}
	for _, cr := range rep.Categories {
		rep.Totals.Units += cr.Units
		rep.Totals.Bytes += cr.Bytes
		rep.Totals.ReclaimableUnits += cr.ReclaimableUnits
		rep.Totals.ReclaimableBytes += cr.ReclaimableBytes
		rep.Totals.RemovedUnits += cr.RemovedUnits
		rep.Totals.FreedBytes += cr.FreedBytes
		rep.Totals.Errors += len(cr.Errors)
	}
	rep.Filesystem = c.filesystem()
	rep.DurationMS = time.Since(startedAt).Milliseconds()

	if !dryRun && c.logger != nil {
		c.logger.Info("storage ephemeral wipe executed",
			zap.Int("removed_units", rep.Totals.RemovedUnits),
			zap.Int64("freed_bytes", rep.Totals.FreedBytes))
	}
	c.invalidateCache()
	return rep, nil
}

func scopeName(s Scope) string {
	switch s {
	case ScopeConversation:
		return "conversation"
	case ScopeProject:
		return "project"
	default:
		return "none"
	}
}
