package agentruntime

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSourceAddressedWorkerRejectsStaleBundleAndSupportsRollback(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	runtimeRoot := filepath.Join(root, "runtime", "pi")
	if err := os.MkdirAll(filepath.Join(runtimeRoot, "src"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"package.json", "package-lock.json", "tsconfig.json"} {
		if err := os.WriteFile(filepath.Join(runtimeRoot, name), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	source := filepath.Join(runtimeRoot, "src", "worker.ts")
	os.WriteFile(source, []byte("version one"), 0o600)
	hash, err := SourceFingerprint(runtimeRoot)
	if err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(runtimeRoot, ".builds", hash)
	os.MkdirAll(filepath.Join(bundle, "dist"), 0o700)
	os.WriteFile(filepath.Join(bundle, "dist", "worker.js"), []byte("worker"), 0o600)
	if _, err := ResolveWorker(""); err == nil {
		t.Fatal("unpublished bundle was selected")
	}
	os.WriteFile(filepath.Join(bundle, ".ready"), []byte(hash), 0o600)
	worker, err := ResolveWorker("")
	if err != nil {
		t.Fatal(err)
	}
	if worker != filepath.Join(bundle, "dist", "worker.js") {
		t.Fatalf("wrong worker: %s", worker)
	}
	os.WriteFile(source, []byte("version two"), 0o600)
	if _, err := ResolveWorker(""); err == nil {
		t.Fatal("stale source bundle was selected")
	}
	os.WriteFile(source, []byte("version one"), 0o600)
	if restored, err := ResolveWorker(""); err != nil || restored != worker {
		t.Fatalf("rollback lost original bundle: %s %v", restored, err)
	}
}
