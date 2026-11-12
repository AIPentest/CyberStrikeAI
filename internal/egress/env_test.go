package egress

import (
	"strings"
	"testing"
)

func TestProxyEnvDisabledReturnsNil(t *testing.T) {
	g, _ := New(Config{Enabled: false})
	if env := g.ProxyEnv(); env != nil {
		t.Fatalf("disabled gateway ProxyEnv = %v, want nil", env)
	}
}

func TestProxyEnvSocks5hWhenDNSThroughProxy(t *testing.T) {
	g, _ := New(Config{Enabled: true, Mode: ModeSOCKS5, Address: "127.0.0.1:9050", DNSThroughProxy: true})
	env := g.ProxyEnv()
	joined := strings.Join(env, "\n")
	if !strings.Contains(joined, "ALL_PROXY=socks5h://127.0.0.1:9050") {
		t.Fatalf("expected socks5h ALL_PROXY, got:\n%s", joined)
	}
	if !strings.Contains(joined, "all_proxy=socks5h://127.0.0.1:9050") {
		t.Fatalf("expected lowercase all_proxy, got:\n%s", joined)
	}
}

func TestProxyEnvSocks5WithoutRemoteDNS(t *testing.T) {
	g, _ := New(Config{Enabled: true, Mode: ModeSOCKS5, Address: "127.0.0.1:9050"})
	joined := strings.Join(g.ProxyEnv(), "\n")
	if !strings.Contains(joined, "socks5://127.0.0.1:9050") || strings.Contains(joined, "socks5h") {
		t.Fatalf("expected plain socks5 scheme, got:\n%s", joined)
	}
}

func TestProxyEnvHTTPWithAuth(t *testing.T) {
	g, _ := New(Config{Enabled: true, Mode: ModeHTTP, Address: "gw:3128", Username: "u", Password: "p"})
	joined := strings.Join(g.ProxyEnv(), "\n")
	if !strings.Contains(joined, "HTTP_PROXY=http://u:p@gw:3128") {
		t.Fatalf("expected authed http proxy url, got:\n%s", joined)
	}
}
