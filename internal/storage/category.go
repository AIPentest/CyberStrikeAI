// Package storage 负责枚举、评估并清理运行空间产生的磁盘垃圾。
//
// 设计约束（误删活跃任务数据的代价远高于省下磁盘，故以下均为硬约束）：
//   - 每个类别只在固定根目录下的固定层级枚举删除单元，不做无界递归删除；
//   - 删除单元必须通过 confined 校验，杜绝路径逃逸；
//   - 目录清理采用「原子改名 + RemoveAll」，进程崩溃只留下带标记的残骸，下一轮补删；
//   - 会话型单元在删除前查询数据库最近活动时间，活跃会话一律跳过；查询失败时保守跳过（fail closed）。
package storage

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"cyberstrike-ai/internal/config"
)

// deletionMarkerSuffix 标记「已判定删除、正在移除」的目录。
// 沿用 Prometheus TSDB 的做法：改名是原子的，崩溃后不会留下半删状态的原始目录名。
const deletionMarkerSuffix = ".tmp-for-deletion"

// Scope 描述删除单元与会话/项目的绑定关系。
type Scope int

const (
	// ScopeNone 与会话无关，仅按保留期判定（日志、checkpoint、C2 产物）。
	ScopeNone Scope = iota
	// ScopeConversation 目录名即会话 ID，可查询会话是否仍存在及最近活动时间。
	ScopeConversation
	// ScopeProject 目录名即项目 ID。
	ScopeProject
)

// Paths 汇总各类别根目录。
// 由 app 层按既有解析规则注入（与 database.SetEinoConversationDirs 使用同一批值），
// 避免本包重复推导路径导致清理目录与实际写入目录不一致。
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

// Unit 是一个可独立删除的清理单元（目录或文件）。
type Unit struct {
	Path    string
	Session string // Scope 非 ScopeNone 时为对应 ID，否则为空
	Scope   Scope
	IsDir   bool
	ModTime time.Time
	Size    int64
}

// Age 返回单元相对 now 的闲置时长；未来时间戳（时钟异常）按 0 处理。
func (u Unit) Age(now time.Time) time.Duration {
	if u.ModTime.IsZero() {
		return 0
	}
	if age := now.Sub(u.ModTime); age > 0 {
		return age
	}
	return 0
}

// scanner 枚举某个根目录下的全部删除单元。
type scanner func(root string) ([]Unit, error)

// category 是一个具名清理任务，对应 Gitea 的 [cron.*] 子任务模型。
type category struct {
	key        string
	label      string
	hint       string
	root       func(Paths) string
	scan       scanner
	pruneEmpty bool // 删除单元后回收空掉的父目录（chat_uploads 的日期层）
}

// CategoryInfo 是一个清理类别的静态元信息，供 API 与前端展示。
type CategoryInfo struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Hint  string `json:"hint"`
}

// DescribeCategories 按注册表顺序返回全部类别元信息。
func DescribeCategories() []CategoryInfo {
	all := categories()
	out := make([]CategoryInfo, 0, len(all))
	for _, cat := range all {
		out = append(out, CategoryInfo{Key: cat.key, Label: cat.label, Hint: cat.hint})
	}
	return out
}

// categories 按固定顺序返回全部类别；root 为空的类别会被调用方跳过。
func categories() []category {
	return []category{
		{
			key:   config.StorageCategoryWorkspace,
			label: "Agent workspace",
			hint:  "Working directory for agent downloads and scan products (tmp/workspace), split per project/conversation.",
			root:  func(p Paths) string { return p.Workspace },
			scan:  scanScopedDirs,
		},
		{
			key:   config.StorageCategoryReduction,
			label: "Tool output cache",
			hint:  "Truncated files where over-long tool output is persisted (tmp/reduction); one file per execution, purely derived data.",
			root:  func(p Paths) string { return p.Reduction },
			scan:  scanScopedDirs,
		},
		{
			key:   config.StorageCategoryConversationArtifact,
			label: "Conversation artifacts",
			hint:  "Summary records and the over-long user input ledger (data/conversation_artifacts).",
			root:  func(p Paths) string { return p.ConversationArtifact },
			scan:  func(root string) ([]Unit, error) { return scanSessionDirs(root, ScopeConversation) },
		},
		{
			key:   config.StorageCategoryPlantask,
			label: "Plan-task board",
			hint:  "Eino multi-agent plan-board JSON (skills/.eino/plantask), purely derived data.",
			root:  func(p Paths) string { return p.Plantask },
			scan:  func(root string) ([]Unit, error) { return scanSessionDirs(root, ScopeConversation) },
		},
		{
			key:   config.StorageCategoryC2Artifacts,
			label: "C2 artifacts",
			hint:  "C2 retrieved screenshots, uploads, delivered files and generated payload binaries (tmp/c2). After cleanup the corresponding payload download links become invalid.",
			root:  func(p Paths) string { return p.C2 },
			scan: func(root string) ([]Unit, error) {
				return scanSubdirFiles(root, "results", "uploads", "downstream", "payloads")
			},
		},
		{
			key:        config.StorageCategoryChatUploads,
			label:      "Conversation uploaded files",
			hint:       "Attachments the user uploaded in a conversation (chat_uploads/date/conversation).",
			root:       func(p Paths) string { return p.ChatUploads },
			scan:       scanDatedSessionDirs,
			pruneEmpty: true,
		},
		{
			key:   config.StorageCategoryWorkflowCheckpoints,
			label: "Workflow checkpoints",
			hint:  "Workflow run breakpoint files (data/workflow-checkpoints); used only to resume interrupted runs.",
			root:  func(p Paths) string { return p.WorkflowCheckpoints },
			scan: func(root string) ([]Unit, error) {
				return scanPatternFiles(root, "*.ckpt", "*.ckpt.tmp")
			},
		},
		{
			key:   config.StorageCategoryDiagnosticLogs,
			label: "Diagnostic logs",
			hint:  "Diagnostic logs rotated by day (log/diagnostic-*.log).",
			root:  func(p Paths) string { return p.DiagnosticLogs },
			scan: func(root string) ([]Unit, error) {
				return scanPatternFiles(root, "diagnostic-*.log")
			},
		},
	}
}

// chatUploadsPlaceholderConvs 是上传时还没有会话 ID 的占位目录名，
// 不能当作会话 ID 去数据库查询，按非会话型处理（仅按保留期判定）。
var chatUploadsPlaceholderConvs = map[string]bool{"_new": true, "_manual": true}

// scanScopedDirs 枚举 root/projects/<id> 与 root/conversations/<id>。
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

// scanSessionDirs 枚举 root/<id>，目录名即会话 ID。
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

// scanDatedSessionDirs 枚举 root/<YYYY-MM-DD>/<会话ID|占位名>。
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
				// 直接落在日期目录下的散文件：按非会话型文件清理。
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

// scanSubdirFiles 枚举 root/<subdir>/ 下的文件（不递归），用于 C2 产物。
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

// scanPatternFiles 枚举 root 下匹配任一 glob 的文件。
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

// statUnit 采集单元的大小与最近修改时间。
// 使用 Lstat：符号链接只统计链接自身，绝不跟随到目标（否则会把工作目录外的数据算进来甚至删掉）。
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

// dirStats 汇总目录占用字节与其中最新的修改时间。
// filepath.WalkDir 不跟随符号链接，且单个子树不可读时按尽力而为跳过，不中断整体统计。
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

// confined 校验 candidate 确实位于 root 之内，防止任何形式的路径逃逸。
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

// pruneEmptyDirs 自底向上回收空目录，最多下探 depth 层。
// chat_uploads 删完会话目录后会留下空的日期目录，需要一并回收。
// 只删除「确实为空」的子孙目录，root 自身永不删除。
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
