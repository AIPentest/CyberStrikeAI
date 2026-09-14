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

// fakeActivity 是 Activity 的测试替身。
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

// ageTree 在 root 下建一棵目录树，并把全部文件与目录的 mtime 统一改成 now-age。
// 目录自身的 mtime 也要改：statUnit 取「目录与其内容的最新 mtime」，
// 只改文件的话新建目录的 mtime 仍是当下，单元会被活跃保护挡住。
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
	// 自底向上：改动子目录会刷新父目录 mtime，顺序反了父目录仍是新的。
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
		CacheTTL: -1, // 测试里禁用缓存，保证每次都真实遍历
	})
}

func intPtr(v int) *int    { return &v }
func boolPtr(v bool) *bool { return &v }

func categoryCfg(days int) config.StorageCategoryConfig {
	return config.StorageCategoryConfig{RetentionDays: intPtr(days)}
}

// workspace 根目录布局：tmp/workspace/{projects,conversations}/<id>/
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
		t.Errorf("未过期的工作区被删除: %s", fresh)
	}
	if exists(old) {
		t.Errorf("超过保留期的工作区未被删除: %s", old)
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
		t.Fatal("dry-run 删除了文件")
	}
	if rep.Totals.ReclaimableBytes != 2048 {
		t.Errorf("ReclaimableBytes = %d, want 2048", rep.Totals.ReclaimableBytes)
	}
	if rep.Totals.RemovedUnits != 0 {
		t.Errorf("dry-run 不应有 RemovedUnits，got %d", rep.Totals.RemovedUnits)
	}
}

// 会话已从数据库消失 → 孤儿目录，按较短的 orphan_grace_days 回收。
func TestOrphanReclaimedEvenWhenRetentionIsZero(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "workspace")
	orphan := filepath.Join(ws, "conversations", "conv-gone")
	ageTree(t, orphan, map[string]int{"a.txt": 10}, 5*24*time.Hour, now)

	// retention_days: 0 表示不按保留期清理，但孤儿目录仍应回收。
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
		t.Error("孤儿目录未被回收")
	}
}

// 会话仍存在且最近有活动 → 即使目录 mtime 很旧也必须保护。
func TestActiveSessionIsProtected(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "workspace")
	dir := filepath.Join(ws, "conversations", "conv-busy")
	ageTree(t, dir, map[string]int{"a.txt": 10}, 40*24*time.Hour, now)

	activity := fakeActivity{conversations: map[string]time.Time{
		"conv-busy": now.Add(-10 * time.Minute), // 10 分钟前还在跑
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
		t.Fatal("活跃会话的工作区被删除")
	}
	if rep.Totals.SkippedActive != 1 {
		t.Errorf("SkippedActive = %d, want 1", rep.Totals.SkippedActive)
	}
}

// 活跃状态查询失败时必须保守跳过：宁可少删，不可误删正在跑的任务数据。
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
		t.Fatal("查询失败时不应删除任何数据")
	}
}

// 指向目录的符号链接不会被任何 scanner 当作删除单元：
// os.ReadDir 的 DirEntry.IsDir() 对符号链接返回 false。
// 这是更安全的行为 —— 工作区里被塞进一个指向 /etc 的软链时，
// 既不会跟随它，也不会把它当成会话目录处理。
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
		t.Skipf("symlink 不可用: %v", err)
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
		t.Error("符号链接不应被当作删除单元移除")
	}
	if !exists(victim) {
		t.Fatal("符号链接目标被删除，发生路径逃逸")
	}
	if rep.Totals.Units != 0 {
		t.Errorf("Units = %d, want 0（符号链接不计入）", rep.Totals.Units)
	}
}

// 上一轮清理崩溃留下的标记目录必须被无条件补删，
// 否则它的 mtime 是刚改名的时间，会被活跃保护永久挡住。
func TestLeftoverDeletionMarkerIsReclaimed(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "workspace")
	marker := filepath.Join(ws, "conversations", "conv-crash"+deletionMarkerSuffix)
	// mtime 就是「刚刚」，模拟崩溃后立即重跑。
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
		t.Error("崩溃残留的标记目录未被补删")
	}
}

// chat_uploads 是 root/<日期>/<会话> 三层布局，删完会话目录后空的日期目录也要回收。
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
		t.Error("过期上传目录未被删除")
	}
	if exists(dateDir) {
		t.Error("空的日期目录未被回收")
	}
	if !exists(uploads) {
		t.Error("类别根目录不应被删除")
	}
	// Clean 未指定类别时会返回全部已注册类别，必须按 key 取，不能依赖下标。
	cat := findCategory(t, rep, config.StorageCategoryChatUploads)
	if cat.RemovedEmptyDirs != 1 {
		t.Errorf("RemovedEmptyDirs = %d, want 1", cat.RemovedEmptyDirs)
	}
	if cat.RemovedUnits != 1 {
		t.Errorf("RemovedUnits = %d, want 1", cat.RemovedUnits)
	}
}

// findCategory 按 key 取报表条目；缺失时直接失败，避免用错下标断言到别的类别。
func findCategory(t *testing.T, rep *Report, key string) CategoryReport {
	t.Helper()
	for _, cat := range rep.Categories {
		if cat.Key == key {
			return cat
		}
	}
	t.Fatalf("报表中缺少类别 %s", key)
	return CategoryReport{}
}

// 占位目录 _new / _manual 不是会话 ID，不能拿去查数据库，按非会话型处理。
func TestChatUploadsPlaceholderDirIsNotSessionScoped(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	tmp := t.TempDir()
	uploads := filepath.Join(tmp, "chat_uploads")
	placeholder := filepath.Join(uploads, "2026-06-01", "_new")
	ageTree(t, placeholder, map[string]int{"a.png": 10}, 120*24*time.Hour, now)

	// Activity 对任何查询都报错：若占位目录被当成会话，会因 fail-closed 而被保留。
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
		t.Error("占位目录应按保留期清理，而不是走会话查询")
	}
}

// glob 型类别：只删匹配的文件，同目录下的其他文件不受影响。
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
		t.Error("过期诊断日志未被删除")
	}
	if !exists(keepName) {
		t.Error("不匹配 glob 的文件被误删")
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

	// Inspect 仍应展示被关闭的类别，让管理员看到可回收量后再决定是否开启。
	inspected := c.Inspect(true)
	var found bool
	for _, cat := range inspected.Categories {
		if cat.Key == config.StorageCategoryWorkspace {
			found = true
			if cat.Enabled {
				t.Error("类别应为 disabled")
			}
			if cat.ReclaimableUnits != 1 {
				t.Errorf("ReclaimableUnits = %d, want 1", cat.ReclaimableUnits)
			}
		}
	}
	if !found {
		t.Fatal("Inspect 未返回 workspace 类别")
	}

	if _, err := c.Clean(CleanRequest{}); err != nil {
		t.Fatalf("Clean: %v", err)
	}
	if !exists(dir) {
		t.Error("被关闭的类别不应被清理")
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

	// 手动占住执行位，模拟「已有一轮在跑」。
	if !c.running.CompareAndSwap(false, true) {
		t.Fatal("无法占用执行位")
	}
	if _, err := c.Clean(CleanRequest{}); !errors.Is(err, ErrCleanupInProgress) {
		t.Errorf("err = %v, want ErrCleanupInProgress", err)
	}
	c.running.Store(false)

	if _, err := c.Clean(CleanRequest{}); err != nil {
		t.Fatalf("释放后应可再次清理: %v", err)
	}
}

// 并发触发清理时只允许一轮真正执行，其余应立即得到 ErrCleanupInProgress 而不是排队删除。
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
			t.Errorf("意外错误: %v", err)
		}
	}
	if inProgress == 0 {
		t.Log("提示：本轮所有 goroutine 都串行完成了，未观察到并发冲突（非失败）")
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
		t.Error("空 root 不应通过校验")
	}
}

// 根目录不存在时不应报错，只标记 Missing —— 系统尚未产生该类垃圾是正常状态。
func TestMissingRootIsNotAnError(t *testing.T) {
	now := time.Now()
	c := newTestCleaner(t, nil, Paths{Workspace: filepath.Join(t.TempDir(), "never-created")}, nil, now)

	rep := c.Inspect(true)
	if len(rep.Categories) != len(config.StorageCategoryOrder) {
		t.Fatalf("Categories = %d, want %d", len(rep.Categories), len(config.StorageCategoryOrder))
	}
	for _, cat := range rep.Categories {
		if !cat.Missing {
			t.Errorf("类别 %s 的根目录不存在/未配置，应标记 Missing", cat.Key)
		}
		if cat.Units != 0 || len(cat.Errors) != 0 {
			t.Errorf("类别 %s 不应有统计或错误: units=%d errors=%v", cat.Key, cat.Units, cat.Errors)
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
		t.Fatalf("首次统计 Units = %d, want 1", first.Totals.Units)
	}
	// 缓存生效期间新增目录不应被看到。
	ageTree(t, filepath.Join(ws, "conversations", "conv-b"), map[string]int{"b.txt": 10}, 400*24*time.Hour, now)
	if cached := c.Inspect(false); cached.Totals.Units != 1 {
		t.Errorf("缓存期内 Units = %d, want 1", cached.Totals.Units)
	}
	if refreshed := c.Inspect(true); refreshed.Totals.Units != 2 {
		t.Errorf("refresh 后 Units = %d, want 2", refreshed.Totals.Units)
	}
	// 清理后缓存必须失效，否则状态页仍显示已删除的内容。
	if _, err := c.Clean(CleanRequest{}); err != nil {
		t.Fatalf("Clean: %v", err)
	}
	if after := c.Inspect(false); after.Totals.Units != 0 {
		t.Errorf("清理后 Units = %d, want 0", after.Totals.Units)
	}
}

func TestRetentionLoopDisabledByDefault(t *testing.T) {
	s := NewService(nil, &config.Config{}, nil)
	if s.AutoCleanEnabled() {
		t.Error("auto_clean 默认必须为关闭")
	}
	// 关闭时 PurgeExpired 是 no-op，不应 panic。
	s.PurgeExpired()

	enabled := &config.Config{Storage: config.StorageConfig{AutoClean: boolPtr(true)}}
	if !NewService(nil, enabled, nil).AutoCleanEnabled() {
		t.Error("显式 true 时应开启")
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
