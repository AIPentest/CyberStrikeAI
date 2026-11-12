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
	return fmt.Sprintf(`## 会话工作目录（下载与本地分析）

**必须使用以下目录**保存 curl/wget 下载的文件、临时 HTML/JS，以及 read_file/glob/grep 的检索范围：
`+"`%s`"+`

- 此目录在绑定项目时由同项目会话共享，重启或重新打开对话后仍应先检索已有成果。成功脚本及依赖保存在 `+"`artifacts/`"+` 下，并用 `+"`upsert_project_fact(category=note, fact_key=artifact/<名称>)`"+` 登记相对路径、调用方式和验证证据。
- **禁止**使用系统 `+"`/tmp`"+` 或其它全局临时目录（多项目/多会话会互窜遗留文件）。
- 下载示例：`+"`curl -o '%s/page.html' 'https://target/'`"+`；exec 时可将 `+"`workdir`"+` 设为该目录。
- 读取下载产物或临时分析文件前，用 glob/grep/read_file **限定在该目录**下搜索，勿在 `+"`/tmp`"+` 盲目检索。
- 当用户询问“当前目录”“项目根目录”或应用自身文件时，优先按服务进程当前工作目录理解；不要把空的会话工作目录误当成项目根目录。`, absPath, absPath)
}
