package app

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"cyberstrike-ai/internal/config"
)

func TestMainServerTLSReusesGeneratedIdentity(t *testing.T) {
	t.Parallel()
	configDir := t.TempDir()
	settings := config.ServerConfig{TLSAutoSelfSign: true}
	_, first, _, _, err := prepareMainServerTLS(&settings, configDir)
	if err != nil {
		t.Fatal(err)
	}
	_, next, _, _, err := prepareMainServerTLS(&settings, configDir)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Certificates[0].Certificate[0], next.Certificates[0].Certificate[0]) {
		t.Fatal("server restart changed its self-signed certificate")
	}

	bundlePath := filepath.Join(configDir, "data", "tls", "self-signed.pem")
	info, err := os.Stat(bundlePath)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		t.Fatalf("TLS private key is accessible by other users: %v", info.Mode().Perm())
	}

	brokenBundle := []byte("incomplete certificate bundle")
	if err := os.WriteFile(bundlePath, brokenBundle, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := prepareMainServerTLS(&settings, configDir); err == nil {
		t.Fatal("invalid certificate bundle was accepted")
	}
	bundle, err := os.ReadFile(bundlePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bundle, brokenBundle) {
		t.Fatal("startup silently replaced the certificate bundle")
	}
}
