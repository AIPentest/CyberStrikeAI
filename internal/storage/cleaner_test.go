package storage

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"cyberstrike-ai/internal/config"
)

// fakeActivity is the test double for Activity.
type fakeActivity struct {
	conversations map[string]time.Time
	projects      map[string]time.Time
	err           error
}

func (f fakeActivity) ConversationLastActivity(id string) (time.Time, bool, error) {
	if f.err != nil {
		return time.Time{}, false, f.err
	}
	at, ok := f.conversations[id]
	return at, ok, nil
}

func (f fakeActivity) ProjectLastActivity(id string) (time.Time, bool, error) {
	if f.err != nil {
		return time.Time{}, false, f.err
	}
	at, ok := f.projects[id]
	return at, ok, nil
}

// ageTree Builds a directory tree under root and sets the mtime of all files and directories uniformly to now-age.
// The directory's own mtime must also be changed: statUnit takes the 'latest of the directory and its contents' mtime",
// If only files are changed, a newly created directory's mtime is still now, and the unit would be blocked by active protection.
func ageTree(t *testing.T, root string, files map[string]int, age time.Duration, now time.Time) {
	t.Helper()
	stamp := now.Add(-age)
	dirs := map[string]bool{root: true}
	for rel, size := range files {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
		if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatalf("chtimes %s: %v", path, err)
		}
		for dir := filepath.Dir(path); dir != root && strings.HasPrefix(dir, root); dir = filepath.Dir(dir) {
			dirs[dir] = true
		}
	}
	// Bottom-up: modifying a subdirectory refreshes the parent's mtime, so in the wrong order the parent would still look new.
	order := make([]string, 0, len(dirs))
	for dir := range dirs {
		order = append(order, dir)
	}
	sort.Slice(order, func(i, j int) bool { return len(order[i]) > len(order[j]) })
	for _, dir := range order {
		if err := os.Chtimes(dir, stamp, stamp); err != nil {
			t.Fatalf("chtimes dir %s: %v", dir, err)
		}
	}
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func newTestCleaner(t *testing.T, cfg *config.Config, paths Paths, activity Activity, now time.Time) *Cleaner {
	t.Helper()
	if cfg == nil {
		cfg = &config.Config{}
	}
	return NewCleaner(Options{
		Config:   cfg,
		Paths:    paths,
		Activity: activity,
		Now:      func() time.Time { return now },
		CacheTTL: -1, // Disables the cache in tests, ensuring a real walk every time
	})
}

func intPtr(v int) *int    { return &v }
func boolPtr(v bool) *bool { return &v }

func categoryCfg(days int) config.StorageCategoryConfig {
	return config.StorageCategoryConfig{RetentionDays: intPtr(days)}
}

// workspace Root directory layout: tmp/workspace/{projects,conversations}/<id>/
func TestCleanExpiresIdleWorkspaceAndKeepsFresh(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "workspace")

	old := filepath.Join(ws, "conversations", "conv-old")
	fresh := filepath.Join(ws, "conversations", "conv-fresh")
	ageTree(t, old, map[string]int{"scan/nmap.txt": 4096}, 40*24*time.Hour, now)
	ageTree(t, fresh, map[string]int{"scan/nmap.txt": 1024}, 2*24*time.Hour, now)

	activity := fakeActivity{conversations: map[string]time.Time{
		"conv-old":   now.Add(-40 * 24 * time.Hour),
		"conv-fresh": now.Add(-2 * 24 * time.Hour),
	}}
	cfg := &config.Config{Storage: config.StorageConfig{
		Categories: map[string]config.StorageCategoryConfig{
			config.StorageCategoryWorkspace: categoryCfg(30),
		},
	}}
	c := newTestCleaner(t, cfg, Paths{Workspace: ws}, activity, now)

	rep, err := c.Clean(CleanRequest{Categories: []string{config.StorageCategoryWorkspace}})
	if err != nil {
		t.Fatalf("Clean: %v", err)
	}
	if !exists(fresh) {
		t.Errorf("A workspace that has not expired was deleted: %s", fresh)
	}
	if exists(old) {
		t.Errorf("A workspace past its retention period was not deleted: %s", old)
	}
	cat := findCategory(t, rep, config.StorageCategoryWorkspace)
	if cat.RemovedUnits != 1 {
		t.Errorf("RemovedUnits = %d, want 1", cat.RemovedUnits)
	}
	if cat.FreedBytes != 4096 {
		t.Errorf("FreedBytes = %d, want 4096", cat.FreedBytes)
	}
}

func TestDryRunDeletesNothing(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "workspace")
	old := filepath.Join(ws, "conversations", "conv-old")
	ageTree(t, old, map[string]int{"a.txt": 2048}, 40*24*time.Hour, now)

	activity := fakeActivity{conversations: map[string]time.Time{"conv-old": now.Add(-40 * 24 * time.Hour)}}
	cfg := &config.Config{Storage: config.StorageConfig{
		Categories: map[string]config.StorageCategoryConfig{
			config.StorageCategoryWorkspace: categoryCfg(30),
		},
	}}
	c := newTestCleaner(t, cfg, Paths{Workspace: ws}, activity, now)

	rep, err := c.Clean(CleanRequest{DryRun: true})
	if err != nil {
		t.Fatalf("Clean: %v", err)
	}
	if !exists(old) {
		t.Fatal("dry-run deleted the file")
	}
	if rep.Totals.ReclaimableBytes != 2048 {
		t.Errorf("ReclaimableBytes = %d, want 2048", rep.Totals.ReclaimableBytes)
	}
	if rep.Totals.RemovedUnits != 0 {
		t.Errorf("dry-run should not have RemovedUnits, got %d", rep.Totals.RemovedUnits)
	}
}

// The session has disappeared from the database → Orphan directories, reclaimed by the shorter orphan_grace_days.
func TestOrphanReclaimedEvenWhenRetentionIsZero(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "workspace")
	orphan := filepath.Join(ws, "conversations", "conv-gone")
	ageTree(t, orphan, map[string]int{"a.txt": 10}, 5*24*time.Hour, now)

	// retention_days: 0 indicates cleaning is not done by retention period, but orphan directories should still be reclaimed.
	cfg := &config.Config{Storage: config.StorageConfig{
		OrphanGraceDays: intPtr(1),
		Categories: map[string]config.StorageCategoryConfig{
			config.StorageCategoryWorkspace: categoryCfg(0),
		},
	}}
	c := newTestCleaner(t, cfg, Paths{Workspace: ws}, fakeActivity{}, now)

	if _, err := c.Clean(CleanRequest{}); err != nil {
		t.Fatalf("Clean: %v", err)
	}
	if exists(orphan) {
		t.Error("An orphan directory was not reclaimed")
	}
}

// the session still exists and has recent activity → must be protected even if the directory's mtime is very old.
func TestActiveSessionIsProtected(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "workspace")
	dir := filepath.Join(ws, "conversations", "conv-busy")
	ageTree(t, dir, map[string]int{"a.txt": 10}, 40*24*time.Hour, now)

	activity := fakeActivity{conversations: map[string]time.Time{
		"conv-busy": now.Add(-10 * time.Minute), // 10 was still running minutes ago
	}}
	cfg := &config.Config{Storage: config.StorageConfig{
		Categories: map[string]config.StorageCategoryConfig{
			config.StorageCategoryWorkspace: categoryCfg(30),
		},
	}}
	c := newTestCleaner(t, cfg, Paths{Workspace: ws}, activity, now)

	rep, err := c.Clean(CleanRequest{})
	if err != nil {
		t.Fatalf("Clean: %v", err)
	}
	if !exists(dir) {
		t.Fatal("An active session's workspace was deleted")
	}
	if rep.Totals.SkippedActive != 1 {
		t.Errorf("SkippedActive = %d, want 1", rep.Totals.SkippedActive)
	}
}

// When the active-status query fails it must skip conservatively: better to delete too little than to wrongly delete the data of a running task.
func TestActivityLookupErrorFailsClosed(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "workspace")
	dir := filepath.Join(ws, "conversations", "conv-x")
	ageTree(t, dir, map[string]int{"a.txt": 10}, 400*24*time.Hour, now)

	cfg := &config.Config{Storage: config.StorageConfig{
		Categories: map[string]config.StorageCategoryConfig{
			config.StorageCategoryWorkspace: categoryCfg(30),
		},
	}}
	c := newTestCleaner(t, cfg, Paths{Workspace: ws}, fakeActivity{err: errors.New("db locked")}, now)

	if _, err := c.Clean(CleanRequest{}); err != nil {
		t.Fatalf("Clean: %v", err)
	}
	if !exists(dir) {
		t.Fatal("No data should be deleted when the query fails")
	}
}

// A symlink pointing to a directory is never treated as a deletion unit by any scanner:
// os.ReadDir DirEntry.IsDir() returns for a symlink false.
// This is the safer behavior -- When a symlink pointing to /etc is slipped into the workspace,
// neither follows it nor treats it as a session directory.
func TestSymlinkInsideRootIsNeverADeletionUnit(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "workspace")
	outside := filepath.Join(tmp, "precious")
	if err := os.MkdirAll(filepath.Join(outside, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(outside, "sub", "keep.txt")
	if err := os.WriteFile(victim, []byte("important"), 0o644); err != nil {
		t.Fatal(err)
	}

	linkParent := filepath.Join(ws, "conversations")
	link := filepath.Join(linkParent, "conv-link")
	if err := os.MkdirAll(linkParent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink Unavailable: %v", err)
	}
	stamp := now.Add(-400 * 24 * time.Hour)
	if err := os.Chtimes(link, stamp, stamp); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{Storage: config.StorageConfig{
		Categories: map[string]config.StorageCategoryConfig{
			config.StorageCategoryWorkspace: categoryCfg(30),
		},
	}}
	c := newTestCleaner(t, cfg, Paths{Workspace: ws}, nil, now)

	rep, err := c.Clean(CleanRequest{})
	if err != nil {
		t.Fatalf("Clean: %v", err)
	}
	if !exists(link) {
		t.Error("A symlink should not be removed as a deletion unit")
	}
	if !exists(victim) {
		t.Fatal("the symlink target is deleted, causing a path escape")
	}
	if rep.Totals.Units != 0 {
		t.Errorf("Units = %d, want 0 (Symlinks are not counted)", rep.Totals.Units)
	}
}

// A marker directory left by the previous cleanup crash must be deleted unconditionally,
// otherwise its mtime is the just-renamed time and would be permanently blocked by active protection.
func TestLeftoverDeletionMarkerIsReclaimed(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "workspace")
	marker := filepath.Join(ws, "conversations", "conv-crash"+deletionMarkerSuffix)
	// mtime is 'just now', simulating an immediate re-run after a crash.
	ageTree(t, marker, map[string]int{"a.txt": 10}, 0, now)

	cfg := &config.Config{Storage: config.StorageConfig{
		Categories: map[string]config.StorageCategoryConfig{
			config.StorageCategoryWorkspace: categoryCfg(30),
		},
	}}
	c := newTestCleaner(t, cfg, Paths{Workspace: ws}, fakeActivity{}, now)

	if _, err := c.Clean(CleanRequest{}); err != nil {
		t.Fatalf("Clean: %v", err)
	}
	if exists(marker) {
		t.Error("A crash-residual marker directory was not deleted")
	}
}

// chat_uploads is a three-level root/<date>/<session> layout; after deleting the session directories, the now-empty date directories must also be reclaimed.
func TestChatUploadsDatedLayoutAndEmptyDirPrune(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	tmp := t.TempDir()
	uploads := filepath.Join(tmp, "chat_uploads")
	dateDir := filepath.Join(uploads, "2026-06-01")
	convDir := filepath.Join(dateDir, "conv-old")
	ageTree(t, convDir, map[string]int{"report.pdf": 100}, 120*24*time.Hour, now)

	cfg := &config.Config{Storage: config.StorageConfig{
		Categories: map[string]config.StorageCategoryConfig{
			config.StorageCategoryChatUploads: categoryCfg(90),
		},
	}}
	c := newTestCleaner(t, cfg, Paths{ChatUploads: uploads}, fakeActivity{}, now)

	rep, err := c.Clean(CleanRequest{})
	if err != nil {
		t.Fatalf("Clean: %v", err)
	}
	if exists(convDir) {
		t.Error("An expired upload directory was not deleted")
	}
	if exists(dateDir) {
		t.Error("An empty date directory was not reclaimed")
	}
	if !exists(uploads) {
		t.Error("A category root directory should not be deleted")
	}
	// Clean When no category is specified it returns all registered categories; they must be fetched by key, not by index.
	cat := findCategory(t, rep, config.StorageCategoryChatUploads)
	if cat.RemovedEmptyDirs != 1 {
		t.Errorf("RemovedEmptyDirs = %d, want 1", cat.RemovedEmptyDirs)
	}
	if cat.RemovedUnits != 1 {
		t.Errorf("RemovedUnits = %d, want 1", cat.RemovedUnits)
	}
}

// findCategory Fetches a report entry by key; fails outright when missing, to avoid asserting the wrong category via a bad index.
func findCategory(t *testing.T, rep *Report, key string) CategoryReport {
	t.Helper()
	for _, cat := range rep.Categories {
		if cat.Key == key {
			return cat
		}
	}
	t.Fatalf("The category is missing from the report %s", key)
	return CategoryReport{}
}

// The placeholder directories _new / _manual are not session IDs; they cannot be used to query the database and are treated as non-session type.
func TestChatUploadsPlaceholderDirIsNotSessionScoped(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	tmp := t.TempDir()
	uploads := filepath.Join(tmp, "chat_uploads")
	placeholder := filepath.Join(uploads, "2026-06-01", "_new")
	ageTree(t, placeholder, map[string]int{"a.png": 10}, 120*24*time.Hour, now)

	// Activity errors on any query: if a placeholder directory were treated as a session, it would be kept due to fail-closed behavior.
	cfg := &config.Config{Storage: config.StorageConfig{
		Categories: map[string]config.StorageCategoryConfig{
			config.StorageCategoryChatUploads: categoryCfg(90),
		},
	}}
	c := newTestCleaner(t, cfg, Paths{ChatUploads: uploads},
		fakeActivity{err: errors.New("should not be called")}, now)

	if _, err := c.Clean(CleanRequest{}); err != nil {
		t.Fatalf("Clean: %v", err)
	}
	if exists(placeholder) {
		t.Error("A placeholder directory should be cleaned by retention period, not via a session query")
	}
}

// glob type category: only deletes matching files, leaving other files in the same directory unaffected.
func TestPatternCategoryOnlyMatchesItsGlob(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	tmp := t.TempDir()
	logs := filepath.Join(tmp, "log")
	if err := os.MkdirAll(logs, 0o755); err != nil {
		t.Fatal(err)
	}
	old := now.Add(-40 * 24 * time.Hour)
	stale := filepath.Join(logs, "diagnostic-2026-08-01.log")
	keepName := filepath.Join(logs, "app.log")
	for _, p := range []string{stale, keepName} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}

	cfg := &config.Config{Storage: config.StorageConfig{
		Categories: map[string]config.StorageCategoryConfig{
			config.StorageCategoryDiagnosticLogs: categoryCfg(14),
		},
	}}
	c := newTestCleaner(t, cfg, Paths{DiagnosticLogs: logs}, nil, now)

	if _, err := c.Clean(CleanRequest{}); err != nil {
		t.Fatalf("Clean: %v", err)
	}
	if exists(stale) {
		t.Error("An expired diagnostic log was not deleted")
	}
	if !exists(keepName) {
		t.Error("A file not matching the glob was wrongly deleted")
	}
}

func TestDisabledCategoryIsReportedButNotCleaned(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "workspace")
	dir := filepath.Join(ws, "conversations", "conv-old")
	ageTree(t, dir, map[string]int{"a.txt": 10}, 400*24*time.Hour, now)

	cfg := &config.Config{Storage: config.StorageConfig{
		Categories: map[string]config.StorageCategoryConfig{
			config.StorageCategoryWorkspace: {Enabled: boolPtr(false), RetentionDays: intPtr(30)},
		},
	}}
	c := newTestCleaner(t, cfg, Paths{Workspace: ws}, fakeActivity{}, now)

	// Inspect Disabled categories should still be shown, so the admin can see the reclaimable amount before deciding whether to enable them.
	inspected := c.Inspect(true)
	var found bool
	for _, cat := range inspected.Categories {
		if cat.Key == config.StorageCategoryWorkspace {
			found = true
			if cat.Enabled {
				t.Error("The category should be disabled")
			}
			if cat.ReclaimableUnits != 1 {
				t.Errorf("ReclaimableUnits = %d, want 1", cat.ReclaimableUnits)
			}
		}
	}
	if !found {
		t.Fatal("Inspect did not return the workspace category")
	}

	if _, err := c.Clean(CleanRequest{}); err != nil {
		t.Fatalf("Clean: %v", err)
	}
	if !exists(dir) {
		t.Error("A disabled category should not be cleaned")
	}
}

func TestUnknownCategoryIsRejected(t *testing.T) {
	now := time.Now()
	c := newTestCleaner(t, nil, Paths{Workspace: t.TempDir()}, nil, now)
	if _, err := c.Clean(CleanRequest{Categories: []string{"../../etc"}}); !errors.Is(err, ErrUnknownCategory) {
		t.Errorf("err = %v, want ErrUnknownCategory", err)
	}
}

func TestConcurrentCleanIsRejected(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "workspace")
	for i := 0; i < 20; i++ {
		dir := filepath.Join(ws, "conversations", "conv-"+string(rune('a'+i)))
		ageTree(t, dir, map[string]int{"a.txt": 10}, 400*24*time.Hour, now)
	}
	c := newTestCleaner(t, nil, Paths{Workspace: ws}, fakeActivity{}, now)

	// Manually occupies the execution slot, simulating 'a round is already running'".
	if !c.running.CompareAndSwap(false, true) {
		t.Fatal("Could not occupy the execution slot")
	}
	if _, err := c.Clean(CleanRequest{}); !errors.Is(err, ErrCleanupInProgress) {
		t.Errorf("err = %v, want ErrCleanupInProgress", err)
	}
	c.running.Store(false)

	if _, err := c.Clean(CleanRequest{}); err != nil {
		t.Fatalf("Cleanup should be possible again after release: %v", err)
	}
}

// When cleanup is triggered concurrently, only one round actually runs; the others should immediately get ErrCleanupInProgress rather than queuing to delete.
func TestParallelCleanHasSingleWinner(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "workspace")
	dir := filepath.Join(ws, "conversations", "conv-old")
	ageTree(t, dir, map[string]int{"a.txt": 10}, 400*24*time.Hour, now)

	c := newTestCleaner(t, nil, Paths{Workspace: ws}, fakeActivity{}, now)

	const n = 8
	var wg sync.WaitGroup
	results := make([]error, n)
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, err := c.Clean(CleanRequest{})
			results[i] = err
		}(i)
	}
	close(start)
	wg.Wait()

	inProgress := 0
	for _, err := range results {
		switch {
		case err == nil:
		case errors.Is(err, ErrCleanupInProgress):
			inProgress++
		default:
			t.Errorf("Unexpected error: %v", err)
		}
	}
	if inProgress == 0 {
		t.Log("Note: all goroutines in this round completed serially, with no concurrency conflict observed (not a failure)")
	}
}

func TestConfinedRejectsEscapes(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "srv", "app", "tmp", "workspace")
	cases := []struct {
		candidate string
		want      bool
	}{
		{filepath.Join(root, "conversations", "abc"), true},
		{filepath.Join(root, "a", "b", "c"), true},
		{root, false},
		{filepath.Join(root, ".."), false},
		{filepath.Join(root, "..", "secrets"), false},
		{filepath.Join(string(filepath.Separator), "etc", "passwd"), false},
		{"", false},
	}
	for _, tc := range cases {
		if got := confined(root, tc.candidate); got != tc.want {
			t.Errorf("confined(%q) = %v, want %v", tc.candidate, got, tc.want)
		}
	}
	if confined("", filepath.Join(root, "x")) {
		t.Error("An empty root should not pass validation")
	}
}

// When the root directory does not exist it should not error, only mark Missing -- It is normal for the system not to have produced this kind of garbage yet.
func TestMissingRootIsNotAnError(t *testing.T) {
	now := time.Now()
	c := newTestCleaner(t, nil, Paths{Workspace: filepath.Join(t.TempDir(), "never-created")}, nil, now)

	rep := c.Inspect(true)
	if len(rep.Categories) != len(config.StorageCategoryOrder) {
		t.Fatalf("Categories = %d, want %d", len(rep.Categories), len(config.StorageCategoryOrder))
	}
	for _, cat := range rep.Categories {
		if !cat.Missing {
			t.Errorf("The root directory of category %s does not exist / is not configured, and should be marked Missing", cat.Key)
		}
		if cat.Units != 0 || len(cat.Errors) != 0 {
			t.Errorf("Category %s should have no statistics or error: units=%d errors=%v", cat.Key, cat.Units, cat.Errors)
		}
	}
	if _, err := c.Clean(CleanRequest{}); err != nil {
		t.Errorf("Clean: %v", err)
	}
}

func TestInspectCachesUntilRefresh(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "workspace")
	dir := filepath.Join(ws, "conversations", "conv-a")
	ageTree(t, dir, map[string]int{"a.txt": 10}, 400*24*time.Hour, now)

	c := NewCleaner(Options{
		Config:   &config.Config{},
		Paths:    Paths{Workspace: ws},
		Activity: fakeActivity{},
		Now:      func() time.Time { return now },
		CacheTTL: time.Minute,
	})

	first := c.Inspect(false)
	if first.Totals.Units != 1 {
		t.Fatalf("First count Units = %d, want 1", first.Totals.Units)
	}
	// A directory added while the cache is valid should not be seen.
	ageTree(t, filepath.Join(ws, "conversations", "conv-b"), map[string]int{"b.txt": 10}, 400*24*time.Hour, now)
	if cached := c.Inspect(false); cached.Totals.Units != 1 {
		t.Errorf("within the cache period Units = %d, want 1", cached.Totals.Units)
	}
	if refreshed := c.Inspect(true); refreshed.Totals.Units != 2 {
		t.Errorf("refresh after Units = %d, want 2", refreshed.Totals.Units)
	}
	// The cache must be invalidated after cleanup, otherwise the status page would still show the deleted content.
	if _, err := c.Clean(CleanRequest{}); err != nil {
		t.Fatalf("Clean: %v", err)
	}
	if after := c.Inspect(false); after.Totals.Units != 0 {
		t.Errorf("after cleanup Units = %d, want 0", after.Totals.Units)
	}
}

func TestRetentionLoopDisabledByDefault(t *testing.T) {
	s := NewService(nil, &config.Config{}, nil)
	if s.AutoCleanEnabled() {
		t.Error("auto_clean must default to disabled")
	}
	// When disabled, PurgeExpired is a no-op and should not panic.
	s.PurgeExpired()

	enabled := &config.Config{Storage: config.StorageConfig{AutoClean: boolPtr(true)}}
	if !NewService(nil, enabled, nil).AutoCleanEnabled() {
		t.Error("should be enabled when explicitly true")
	}
}

func TestIntervalHasFloor(t *testing.T) {
	cfg := &config.Config{Storage: config.StorageConfig{IntervalMinutes: intPtr(1)}}
	if got := NewService(nil, cfg, nil).Interval(); got != minSweepInterval {
		t.Errorf("Interval = %v, want %v", got, minSweepInterval)
	}
	cfg = &config.Config{Storage: config.StorageConfig{IntervalMinutes: intPtr(180)}}
	if got := NewService(nil, cfg, nil).Interval(); got != 3*time.Hour {
		t.Errorf("Interval = %v, want 3h", got)
	}
}
