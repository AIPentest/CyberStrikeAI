// Package tooloutput spills oversized tool stdout/results to local files under
// the reduction cache tree (tmp/reduction/...), so agents can read_file the
// full text after context truncation.
package tooloutput

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	defaultRootDir = "tmp/reduction"
	readFileHint   = "read_file"
)

// SpillOpts scopes where a trunc file is written (mirrors reduction RootDir layout).
type SpillOpts struct {
	RootDir        string // reduction_root_dir or empty → tmp/reduction
	ProjectID      string
	ConversationID string
	ExecutionID    string // preferred file name; empty → uuid
}

// SessionRoot returns the conversation/project-scoped reduction cache root.
func SessionRoot(configuredBase, projectID, conversationID string) string {
	base := strings.TrimSpace(configuredBase)
	if base == "" {
		base = defaultRootDir
	}
	if pid := strings.TrimSpace(projectID); pid != "" {
		return filepath.Join(base, "projects", sanitizeSegment(pid))
	}
	conv := strings.TrimSpace(conversationID)
	if conv == "" {
		conv = "default"
	}
	return filepath.Join(base, "conversations", sanitizeSegment(conv))
}

// WriteTruncFile writes full content under {sessionRoot}/trunc/{id} and returns
// an absolute path suitable for read_file.
func WriteTruncFile(opts SpillOpts, content string) (string, error) {
	session := SessionRoot(opts.RootDir, opts.ProjectID, opts.ConversationID)
	id := strings.TrimSpace(opts.ExecutionID)
	if id == "" {
		id = uuid.NewString()
	}
	id = sanitizeSegment(id)
	dir := filepath.Join(session, "trunc")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir tool output trunc dir: %w", err)
	}
	path := filepath.Join(dir, id)
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return "", fmt.Errorf("write tool output trunc file: %w", err)
	}
	return path, nil
}

// BoundWithSpill truncates full text into a <persisted-output> notice after
// spilling the original to disk. The returned string is always ≤ maxBytes when
// maxBytes > 0. On spill failure it falls back to a prefix + marker (no path).
func BoundWithSpill(full string, maxBytes int, opts SpillOpts) string {
	if maxBytes <= 0 || len(full) <= maxBytes {
		return full
	}
	path, err := WriteTruncFile(opts, full)
	if err != nil {
		return boundPrefixOnly(full, maxBytes, len(full), "")
	}
	return FormatPersistedOutput(full, path, maxBytes)
}

// FormatPersistedOutput builds a reduction-compatible notice with head/tail
// previews that fits in maxBytes.
func FormatPersistedOutput(full, filePath string, maxBytes int) string {
	return formatPersisted(len(full), filePath, full, maxBytes)
}

// FormatPersistedFromFile builds the notice using previews read from an already
// spilled file (streaming collectors that never kept the full string in memory).
func FormatPersistedFromFile(filePath string, originalSize, maxBytes int) string {
	previewSrc := ""
	if data, err := os.ReadFile(filePath); err == nil {
		previewSrc = string(data)
		if originalSize <= 0 {
			originalSize = len(data)
		}
	}
	return formatPersisted(originalSize, filePath, previewSrc, maxBytes)
}

func formatPersisted(originalSize int, filePath, previewSrc string, maxBytes int) string {
	if maxBytes <= 0 {
		maxBytes = 12000
	}
	// Always keep the absolute path readable for read_file, even under tight budgets.
	minimal := fmt.Sprintf(
		"<persisted-output>\nOutput too large (%d). Full output saved to: %s\nUse %s to read.\n</persisted-output>",
		originalSize, filePath, readFileHint,
	)
	if len(minimal) > maxBytes {
		core := fmt.Sprintf("<persisted-output>Full output saved to: %s</persisted-output>", filePath)
		if len(core) <= maxBytes {
			return core
		}
		// Path longer than budget: keep as much of the path as possible after a short prefix.
		prefix := "<persisted-output>Full output saved to: "
		suffix := "</persisted-output>"
		room := maxBytes - len(prefix) - len(suffix)
		if room <= 0 {
			return clampPrefix(core, maxBytes)
		}
		return prefix + clampSuffix(filePath, room) + suffix
	}

	previewBudget := maxBytes - len(minimal) + 32 // approximate room beyond minimal shell
	if previewBudget > 4000 {
		previewBudget = 4000
	}
	if previewBudget < 0 {
		previewBudget = 0
	}
	for previewBudget >= 0 {
		half := previewBudget / 2
		head := clampPrefix(previewSrc, half)
		tail := clampSuffix(previewSrc, previewBudget-half)
		notice := fmt.Sprintf(
			"<persisted-output>\nOutput too large (%d). Full output saved to: %s\nUse %s with offset/limit to read parts of the file.\nPreview (first %d):\n%s\n\nPreview (last %d):\n%s\n\n</persisted-output>",
			originalSize, filePath, readFileHint, len(head), head, len(tail), tail,
		)
		if len(notice) <= maxBytes {
			return notice
		}
		if previewBudget == 0 {
			return minimal
		}
		previewBudget = previewBudget * 3 / 4
	}
	return minimal
}

func boundPrefixOnly(full string, maxBytes, originalSize int, filePath string) string {
	marker := fmt.Sprintf("\n\n...[tool output truncated: original %d bytes, kept %d bytes]...", originalSize, maxBytes)
	if filePath != "" {
		marker = fmt.Sprintf("\n\n...[tool output truncated: original %d bytes, kept %d bytes; full output: %s]...", originalSize, maxBytes, filePath)
	}
	budget := maxBytes - len(marker)
	if budget < 0 {
		return clampPrefix(marker, maxBytes)
	}
	return clampPrefix(full, budget) + marker
}

func clampPrefix(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func clampSuffix(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	start := len(s) - n
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	return s[start:]
}

func sanitizeSegment(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "default"
	}
	// Execution IDs may be model/provider generated and can contain characters
	// that Windows does not allow in a file name (notably `|`).
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '-'
		}
		return r
	}, s)
	s = strings.ReplaceAll(s, "..", "__")
	s = strings.TrimRight(s, " .")
	if s == "" {
		return "default"
	}
	windowsName := strings.ToUpper(s)
	if dot := strings.IndexByte(windowsName, '.'); dot >= 0 {
		windowsName = windowsName[:dot]
	}
	switch windowsName {
	case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		s = "_" + s
	}
	if len(s) > 180 {
		s = s[:180]
	}
	return s
}

// Tee writes every byte to a trunc file while callers keep only a bounded
// in-memory prefix. Safe for concurrent stdout/stderr writers.
type Tee struct {
	mu   sync.Mutex
	opts SpillOpts
	file *os.File
	path string
	err  error
	open bool
}

// NewTee prepares a lazy spill file (created on first Write).
func NewTee(opts SpillOpts) *Tee {
	return &Tee{opts: opts}
}

// Write appends to the spill file, creating it on first use.
func (t *Tee) Write(p []byte) (int, error) {
	if t == nil {
		return len(p), nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := t.ensureOpenLocked(); err != nil {
		return len(p), nil // best-effort: never fail the tool pipe
	}
	if t.file == nil {
		return len(p), nil
	}
	_, _ = t.file.Write(p)
	return len(p), nil
}

func (t *Tee) ensureOpenLocked() error {
	if t.open || t.err != nil {
		return t.err
	}
	t.open = true
	session := SessionRoot(t.opts.RootDir, t.opts.ProjectID, t.opts.ConversationID)
	id := strings.TrimSpace(t.opts.ExecutionID)
	if id == "" {
		id = uuid.NewString()
	}
	id = sanitizeSegment(id)
	dir := filepath.Join(session, "trunc")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.err = err
		return err
	}
	path := filepath.Join(dir, id)
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		t.err = err
		return err
	}
	t.file = f
	t.path = path
	return nil
}

// Path returns the absolute spill path after any Write (may be empty if unused/failed).
func (t *Tee) Path() string {
	if t == nil {
		return ""
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.path
}

// Close flushes and closes the spill file.
func (t *Tee) Close() error {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.file == nil {
		return nil
	}
	err := t.file.Close()
	t.file = nil
	return err
}
