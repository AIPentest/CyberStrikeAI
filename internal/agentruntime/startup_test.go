package agentruntime

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestStartupPreparesWorkerAfterGoOnlyUpgrade(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node not installed")
	}
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("npm not installed")
	}
	// This is the install state left by an old updater: new sources and a new
	// Go executable, with no dist, node_modules or previously published worker.
	t.Chdir(t.TempDir())
	root := filepath.Join("runtime", "pi")
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o700); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"package.json":      `{"name":"startup-fixture","version":"1.0.0","scripts":{"build":"node -e \"require('fs').mkdirSync('dist');require('fs').copyFileSync('src/worker.js','dist/worker.js')\""}}`,
		"package-lock.json": `{"name":"startup-fixture","version":"1.0.0","lockfileVersion":3,"packages":{"":{"name":"startup-fixture","version":"1.0.0"}}}`,
		"tsconfig.json":     `{}`,
		"src/worker.js":     `console.log(JSON.stringify({type:'ready',version:1,sdkVersion:'1.5.2'}));process.stdin.resume();`,
	}
	for path, body := range files {
		if err := os.WriteFile(filepath.Join(root, path), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ResolveWorker(""); err == nil {
		t.Fatal("fixture unexpectedly has a worker before startup")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := EnsurePiRuntime(ctx, node, ""); err != nil {
		t.Fatal(err)
	}
	worker, err := ResolveWorker("")
	if err != nil {
		t.Fatal(err)
	}
	// Restart is offline: the matching bundle requires no npm invocation.
	t.Setenv("PATH", t.TempDir())
	if err := EnsurePiRuntime(ctx, node, ""); err != nil {
		t.Fatalf("prepared installation required npm again: %v", err)
	}
	if err := os.WriteFile(worker, []byte(`process.exit(1)`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := EnsurePiRuntime(ctx, node, ""); err == nil {
		t.Fatal("broken worker passed readiness")
	}
}

func TestStartupFailsForMissingRuntime(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := EnsurePiRuntime(context.Background(), "node", ""); err == nil {
		t.Fatal("binary-only install must not report readiness")
	}
	if err := EnsurePiRuntime(context.Background(), "node", "missing-worker.js"); err == nil {
		t.Fatal("missing configured worker silently fell back")
	}
}
