// Package storage Responsible for enumerating, evaluating, and cleaning up the disk garbage produced by the runtime workspace.
//
// Design constraints (the cost of wrongly deleting an active task's data far exceeds the disk saved, so the following are all hard constraints):
// - Each category enumerates deletion units only at a fixed level under a fixed root directory, with no unbounded recursive deletion;
// - A deletion unit must pass the confined check, eliminating path escape;
// - Directory cleanup uses 'atomic rename' + RemoveAll", A process crash only leaves marked remnants, which the next round deletes;
// - A session-type unit queries the database for the last activity time before deletion, always skipping active sessions; on query failure it skips conservatively (fail closed).
package storage

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"cyberstrike-ai/internal/config"
)

// deletionMarkerSuffix Marks a directory as 'decided for deletion, being removed'.
// Follows the Prometheus TSDB approach: renaming is atomic, so a crash never leaves the original directory name in a half-deleted state.
const deletionMarkerSuffix = ".tmp-for-deletion"

// Scope Describes the binding between a deletion unit and a session/project.
type Scope int

const (
	// ScopeNone Unrelated to sessions, judged only by retention period (logs, checkpoints, C2 artifacts).
	ScopeNone Scope = iota
	// ScopeConversation The directory name is the session ID, which can be queried for whether the session still exists and its last activity time.
	ScopeConversation
	// ScopeProject The directory name is the project ID.
	ScopeProject
)

// Paths Aggregates the root directories of all categories.
// Injected by the app layer following the existing parsing rules (using the same set of values as database.SetEinoConversationDirs),
// Avoids this package re-deriving paths and causing the cleanup directory to differ from the actual write directory.
type Paths struct {
	Workspace            string
	Reduction            string
	ConversationArtifact string
	Plantask             string
	C2                   string
	ChatUploads          string
	WorkflowCheckpoints  string
	DiagnosticLogs       string
}

// Unit is an independently deletable cleanup unit (a directory or file).
type Unit struct {
	Path    string
	Session string // Scope The corresponding ID when not ScopeNone, otherwise empty
	Scope   Scope
	IsDir   bool
	ModTime time.Time
	Size    int64
}

// Age Returns the unit's idle duration relative to now; a future timestamp (clock anomaly) is treated as 0.
func (u Unit) Age(now time.Time) time.Duration {
	if u.ModTime.IsZero() {
		return 0
	}
	if age := now.Sub(u.ModTime); age > 0 {
		return age
	}
	return 0
}

// scanner Enumerates all deletion units under a given root directory.
type scanner func(root string) ([]Unit, error)

// category is a named cleanup task, corresponding to Gitea's [cron.*] subtask model.
type category struct {
	key        string
	label      string
	hint       string
	root       func(Paths) string
	scan       scanner
	pruneEmpty bool // After deleting a unit it reclaims the now-empty parent directory (the date level of chat_uploads)
}

// CategoryInfo is the static metadata of a cleanup category, for the API and frontend to display.
type CategoryInfo struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Hint  string `json:"hint"`
}

// DescribeCategories Returns the metadata of all categories in registry order.
func DescribeCategories() []CategoryInfo {
	all := categories()
	out := make([]CategoryInfo, 0, len(all))
	for _, cat := range all {
		out = append(out, CategoryInfo{Key: cat.key, Label: cat.label, Hint: cat.hint})
	}
	return out
}

// categories Returns all categories in a fixed order; categories with an empty root are skipped by the caller.
func categories() []category {
	return []category{
		{
			key:   config.StorageCategoryWorkspace,
			label: "Agent Workspace",
			hint:  "Agent The working directory for download and scan artifacts (tmp/workspace), split into per-project/session directories.",
			root:  func(p Paths) string { return p.Workspace },
			scan:  scanScopedDirs,
		},
		{
			key:   config.StorageCategoryReduction,
			label: "Tool-output cache",
			hint:  "Truncated files written to disk for over-long tool output (tmp/reduction), one file per execution, which is purely derived data.",
			root:  func(p Paths) string { return p.Reduction },
			scan:  scanScopedDirs,
		},
		{
			key:   config.StorageCategoryConversationArtifact,
			label: "Session artifacts",
			hint:  "Summary records and the over-long user-input ledger (data/conversation_artifacts).",
			root:  func(p Paths) string { return p.ConversationArtifact },
			scan:  func(root string) ([]Unit, error) { return scanSessionDirs(root, ScopeConversation) },
		},
		{
			key:   config.StorageCategoryPlantask,
			label: "Scheduled-task board",
			hint:  "Eino The multi-agent plan-board JSON (skills/.eino/plantask), which is purely derived data.",
			root:  func(p Paths) string { return p.Plantask },
			scan:  func(root string) ([]Unit, error) { return scanSessionDirs(root, ScopeConversation) },
		},
		{
			key:   config.StorageCategoryC2Artifacts,
			label: "C2 Artifacts",
			hint:  "C2 Returned screenshots, uploads, delivered files, and generated payload binaries (tmp/c2). After cleanup, the corresponding payload download links become invalid.",
			root:  func(p Paths) string { return p.C2 },
			scan: func(root string) ([]Unit, error) {
				return scanSubdirFiles(root, "results", "uploads", "downstream", "payloads")
			},
		},
		{
			key:        config.StorageCategoryChatUploads,
			label:      "Conversation-uploaded files",
			hint:       "Attachments uploaded by the user in a conversation (chat_uploads/date/session).",
			root:       func(p Paths) string { return p.ChatUploads },
			scan:       scanDatedSessionDirs,
			pruneEmpty: true,
		},
		{
			key:   config.StorageCategoryWorkflowCheckpoints,
			label: "Workflow checkpoints",
			hint:  "Workflow-run checkpoint files (data/workflow-checkpoints), used only to resume interrupted runs.",
			root:  func(p Paths) string { return p.WorkflowCheckpoints },
			scan: func(root string) ([]Unit, error) {
				return scanPatternFiles(root, "*.ckpt", "*.ckpt.tmp")
			},
		},
		{
			key:   config.StorageCategoryDiagnosticLogs,
			label: "Diagnostic logs",
			hint:  "Diagnostic logs rotated daily (log/diagnostic-*.log).",
			root:  func(p Paths) string { return p.DiagnosticLogs },
			scan: func(root string) ([]Unit, error) {
				return scanPatternFiles(root, "diagnostic-*.log")
			},
		},
	}
}

// chatUploadsPlaceholderConvs is the placeholder directory name used when there is no session ID yet at upload time,
// Cannot be used as a session ID to query the database; treated as non-session type (judged only by retention period).
var chatUploadsPlaceholderConvs = map[string]bool{"_new": true, "_manual": true}

// scanScopedDirs Enumerates root/projects/<id> and root/conversations/<id>.
func scanScopedDirs(root string) ([]Unit, error) {
	scopes := []struct {
		dir   string
		scope Scope
	}{{"projects", ScopeProject}, {"conversations", ScopeConversation}}

	var units []Unit
	for _, s := range scopes {
		base := filepath.Join(root, s.dir)
		entries, err := os.ReadDir(base)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			u, err := statUnit(filepath.Join(base, e.Name()), e.Name(), s.scope)
			if err != nil {
				continue
			}
			units = append(units, u)
		}
	}
	return units, nil
}

// scanSessionDirs Enumerates root/<id>, where the directory name is the session ID.
func scanSessionDirs(root string, scope Scope) ([]Unit, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var units []Unit
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		u, err := statUnit(filepath.Join(root, e.Name()), e.Name(), scope)
		if err != nil {
			continue
		}
		units = append(units, u)
	}
	return units, nil
}

// scanDatedSessionDirs Enumerates root/<YYYY-MM-DD>/<session ID|placeholder name>.
func scanDatedSessionDirs(root string) ([]Unit, error) {
	dates, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var units []Unit
	for _, d := range dates {
		if !d.IsDir() {
			continue
		}
		dateDir := filepath.Join(root, d.Name())
		convs, err := os.ReadDir(dateDir)
		if err != nil {
			continue
		}
		for _, cd := range convs {
			if !cd.IsDir() {
				// Loose files landing directly under a date directory: cleaned as non-session-type files.
				if u, err := statUnit(filepath.Join(dateDir, cd.Name()), "", ScopeNone); err == nil {
					units = append(units, u)
				}
				continue
			}
			session, scope := cd.Name(), ScopeConversation
			if chatUploadsPlaceholderConvs[session] {
				session, scope = "", ScopeNone
			}
			if u, err := statUnit(filepath.Join(dateDir, cd.Name()), session, scope); err == nil {
				units = append(units, u)
			}
		}
	}
	return units, nil
}

// scanSubdirFiles Enumerates files under root/<subdir>/ (non-recursively), used for C2 artifacts.
func scanSubdirFiles(root string, subdirs ...string) ([]Unit, error) {
	var units []Unit
	for _, sub := range subdirs {
		base := filepath.Join(root, sub)
		entries, err := os.ReadDir(base)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if u, err := statUnit(filepath.Join(base, e.Name()), "", ScopeNone); err == nil {
				units = append(units, u)
			}
		}
	}
	return units, nil
}

// scanPatternFiles Enumerates files under root that match any glob.
func scanPatternFiles(root string, patterns ...string) ([]Unit, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var units []Unit
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		matched := false
		for _, p := range patterns {
			if ok, _ := filepath.Match(p, e.Name()); ok {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		if u, err := statUnit(filepath.Join(root, e.Name()), "", ScopeNone); err == nil {
			units = append(units, u)
		}
	}
	return units, nil
}

// statUnit Collects a unit's size and latest modification time.
// Use Lstat: a symlink is counted as the link itself and never followed to its target (otherwise data outside the working directory would be counted, or even deleted).
func statUnit(path, session string, scope Scope) (Unit, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return Unit{}, err
	}
	u := Unit{
		Path:    path,
		Session: session,
		Scope:   scope,
		IsDir:   info.IsDir(),
		ModTime: info.ModTime(),
		Size:    info.Size(),
	}
	if u.IsDir {
		size, newest := dirStats(path)
		u.Size = size
		if newest.After(u.ModTime) {
			u.ModTime = newest
		}
	}
	if scope == ScopeNone {
		u.Session = ""
	}
	return u, nil
}

// dirStats Aggregates a directory's byte usage and the latest modification time within it.
// filepath.WalkDir Does not follow symlinks, and when a single subtree is unreadable it skips on a best-effort basis without interrupting the overall statistics.
func dirStats(root string) (int64, time.Time) {
	var size int64
	var newest time.Time
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d == nil {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return nil
		}
		if !d.IsDir() {
			size += info.Size()
		}
		if mt := info.ModTime(); mt.After(newest) {
			newest = mt
		}
		return nil
	})
	return size, newest
}

// confined Verifies that candidate is truly inside root, to prevent any form of path escape.
func confined(root, candidate string) bool {
	if root == "" || candidate == "" {
		return false
	}
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	if rel == "." || rel == ".." || filepath.IsAbs(rel) {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// pruneEmptyDirs Reclaims empty directories bottom-up, descending at most depth levels.
// chat_uploads After deleting the session directories, empty date directories remain and must be reclaimed too.
// Deletes only descendant directories that are 'truly empty'; root itself is never deleted.
func pruneEmptyDirs(root string, depth int) int {
	if depth <= 0 {
		return 0
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return 0
	}
	removed := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sub := filepath.Join(root, e.Name())
		removed += pruneEmptyDirs(sub, depth-1)
		if isEmptyDir(sub) && os.Remove(sub) == nil {
			removed++
		}
	}
	return removed
}

func isEmptyDir(path string) bool {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}
	return len(entries) == 0
}
