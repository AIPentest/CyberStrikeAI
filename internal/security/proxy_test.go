package security

import (
	"cyberstrike-ai/internal/config"
	"go.uber.org/zap"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestProxySnapshotIsolation(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://existing:8080")
	e := NewExecutor(&config.SecurityConfig{}, nil, zap.NewNop())
	e.SetProxyConfig(config.ProxyConfig{Mode: "fixed", URL: "http://private:secret@localhost:8081"})
	snapshot := e.proxySnapshot("http-framework-test")
	if snapshot == "" || e.proxySnapshot("nmap") != "" || e.proxySnapshot("exec") != "" {
		t.Fatal("wrong routing scope")
	}
	e.SetProxyConfig(config.ProxyConfig{Mode: "fixed", URL: "http://new:8082"})
	cmd := exec.Command("unused")
	applyHTTPProxySnapshot(cmd, snapshot)
	if !strings.Contains(strings.Join(cmd.Env, "\n"), "private:secret") || strings.Contains(strings.Join(cmd.Args, " "), "secret") {
		t.Fatal("snapshot lost or leaked to argv")
	}
	if os.Getenv("HTTP_PROXY") != "http://existing:8080" {
		t.Fatal("global proxy changed")
	}
	other := exec.Command("unused")
	applyHTTPProxySnapshot(other, e.proxySnapshot("nmap"))
	if strings.Contains(strings.Join(other.Env, "\n"), "CSAI_HTTP_PROXY_CONFIG=") {
		t.Fatal("policy leaked to other tool")
	}
}
