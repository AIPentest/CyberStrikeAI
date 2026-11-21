package app

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestSPABuildArtifacts 校验前端构建产物已随仓库提交：
// 首页必须引用 /static/app/assets/ 下的哈希资源，且这些文件真实存在。
// 前端源码在 web/frontend，构建命令：npm run --prefix web/frontend build。
func TestSPABuildArtifacts(t *testing.T) {
	root := filepath.Join("..", "..", "web", "static", "app")
	indexPath := filepath.Join(root, "index.html")
	index, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("前端构建产物缺失 %s: %v", indexPath, err)
	}
	html := string(index)
	if !strings.Contains(html, `<div id="root">`) {
		t.Fatalf("index.html 缺少 React 挂载点: %s", indexPath)
	}

	assetPattern := regexp.MustCompile(`(?:src|href)="(/static/app/[^"]+)"`)
	matches := assetPattern.FindAllStringSubmatch(html, -1)
	if len(matches) == 0 {
		t.Fatalf("index.html 未引用任何构建资源: %s", indexPath)
	}
	staticRoot := filepath.Join("..", "..", "web", "static")
	for _, match := range matches {
		relative := strings.TrimPrefix(match[1], "/static/")
		if strings.HasPrefix(relative, "favicon") || strings.HasPrefix(relative, "site.webmanifest") {
			continue
		}
		assetPath := filepath.Join(staticRoot, filepath.FromSlash(relative))
		if _, err := os.Stat(assetPath); err != nil {
			t.Fatalf("index.html 引用的资源不存在: %s", assetPath)
		}
	}
}

// TestSPAEntryDoesNotDependOnLegacyTemplates 防止页面回退到 Go 模板渲染。
func TestSPAEntryDoesNotDependOnLegacyTemplates(t *testing.T) {
	source, err := os.ReadFile("app.go")
	if err != nil {
		t.Fatal(err)
	}
	code := string(source)
	if strings.Contains(code, "LoadHTMLGlob") {
		t.Fatal("app.go 仍在加载 Go 模板，前端应统一由 web/static/app 提供")
	}
	if !strings.Contains(code, `c.File("./web/static/app/index.html")`) {
		t.Fatal("app.go 未通过 c.File 提供前端入口")
	}
}
