package egress

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCleanupTempFilesRemovesGeneratedConf(t *testing.T) {
	// Generate a proxychains.conf under the gateway temp dir.
	g, _ := New(Config{Enabled: true, Mode: ModeTor, DNSThroughProxy: true})
	path, err := g.ProxychainsConfPath()
	if err != nil {
		t.Fatalf("ProxychainsConfPath: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("conf not written: %v", err)
	}

	if err := CleanupTempFiles(); err != nil {
		t.Fatalf("CleanupTempFiles: %v", err)
	}
	if _, err := os.Stat(TempDir()); !os.IsNotExist(err) {
		t.Fatalf("temp dir still present after cleanup: %v", err)
	}
	// Idempotent: a second call on an absent dir is not an error.
	if err := CleanupTempFiles(); err != nil {
		t.Fatalf("second CleanupTempFiles should be nil, got %v", err)
	}
}

func TestTempDirIsConfined(t *testing.T) {
	if filepath.Base(TempDir()) != tempDirName {
		t.Fatalf("TempDir() basename = %q, want %q", filepath.Base(TempDir()), tempDirName)
	}
}
