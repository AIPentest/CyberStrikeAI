package project

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func sanitizeWorkspacePathSegment(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "default"
	}
	s = strings.ReplaceAll(s, string(filepath.Separator), "-")
	s = strings.ReplaceAll(s, "/", "-")
	s = strings.ReplaceAll(s, "\\", "-")
	s = strings.ReplaceAll(s, "..", "__")
	if len(s) > 180 {
		s = s[:180]
	}
	return s
}

// WorkspaceRootDir returns the relative workspace root for downloads and local analysis.
// Project-bound sessions share projects/<id>/; otherwise conversations/<id>/.
func WorkspaceRootDir(configuredBase, projectID, conversationID string) string {
	base := strings.TrimSpace(configuredBase)
	if base == "" {
		base = filepath.Join("tmp", "workspace")
	}
	if pid := strings.TrimSpace(projectID); pid != "" {
		return filepath.Join(base, "projects", sanitizeWorkspacePathSegment(pid))
	}
	conv := strings.TrimSpace(conversationID)
	if conv == "" {
		conv = "default"
	}
	return filepath.Join(base, "conversations", sanitizeWorkspacePathSegment(conv))
}

// EnsureWorkspace creates the workspace directory and returns its absolute path.
func EnsureWorkspace(root string) (string, error) {
	abs, err := filepath.Abs(strings.TrimSpace(root))
	if err != nil {
		return "", fmt.Errorf("workspace abs: %w", err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return "", fmt.Errorf("workspace mkdir: %w", err)
	}
	return abs, nil
}

// BuildWorkspaceBlock instructs the agent to use the session workspace instead of /tmp.
func BuildWorkspaceBlock(absPath string) string {
	absPath = strings.TrimSpace(absPath)
	if absPath == "" {
		return ""
	}
	return fmt.Sprintf(`## The session working directory (for downloads and local analysis)

**You must use the following directory** to save files downloaded with curl/wget, temporary HTML/JS, and as the search scope for read_file/glob/grep:
`+"`%s`"+`

- When a project is bound, this directory is shared by sessions of the same project; after a restart or reopening the conversation you should still search existing work first. Successful scripts and dependencies are saved in `+"`artifacts/`"+` under it; call `+"`upsert_project_fact(category=note, fact_key=artifact/<Name>)`"+` to register the relative path, invocation method, and verification evidence.
- You **must not** use the system `+"`/tmp`"+` or another global temporary directory (multiple projects/sessions would leak files into each other).
- Download example: `+"`curl -o '%s/page.html' 'https://target/'`"+`; with exec you can then set `+"`workdir`"+` to that directory.
- Before reading downloaded artifacts or temporary analysis files, search with glob/grep/read_file **confined to that directory**, and do not do blind `+"`/tmp`"+` searching.
- When the user asks about the "current directory" or "project root directory", or about the application's own files, interpret it against the service process's current working directory first; do not mistake an empty session working directory for the project root.`, absPath, absPath)
}
