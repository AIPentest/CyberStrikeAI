package egress

import (
	"os"
	"strings"
	"testing"
)

func TestProxychainsSupportedDisabled(t *testing.T) {
	g, _ := New(Config{Enabled: false})
	if g.ProxychainsSupported() {
		t.Fatal("disabled gateway must not support proxychains wrapping")
	}
}

func TestProxychainsSupportedOffMode(t *testing.T) {
	g, _ := New(Config{Enabled: true, Mode: ModeTor, ProxychainsMode: "off"})
	if g.ProxychainsSupported() {
		t.Fatal("proxychains_mode=off must disable wrapping")
	}
}

func TestProxychainsConfContentSocks5WithDNS(t *testing.T) {
	g, _ := New(Config{Enabled: true, Mode: ModeTor, DNSThroughProxy: true})
	conf, err := g.proxychainsConfContent()
	if err != nil {
		t.Fatalf("conf: %v", err)
	}
	if !strings.Contains(conf, "proxy_dns") {
		t.Error("expected proxy_dns when DNSThroughProxy is set")
	}
	if !strings.Contains(conf, "socks5 127.0.0.1 9050") {
		t.Errorf("expected tor socks5 line, got:\n%s", conf)
	}
	if !strings.Contains(conf, "strict_chain") {
		t.Error("expected strict_chain")
	}
}

func TestProxychainsConfContentSocks5NoDNS(t *testing.T) {
	g, _ := New(Config{Enabled: true, Mode: ModeSOCKS5, Address: "10.0.0.5:1080"})
	conf, _ := g.proxychainsConfContent()
	if strings.Contains(conf, "proxy_dns") {
		t.Error("proxy_dns should be absent without DNSThroughProxy")
	}
	if !strings.Contains(conf, "socks5 10.0.0.5 1080") {
		t.Errorf("expected socks5 line, got:\n%s", conf)
	}
}

func TestProxychainsConfContentHTTPWithAuth(t *testing.T) {
	g, _ := New(Config{Enabled: true, Mode: ModeHTTP, Address: "gw:3128", Username: "u", Password: "p"})
	conf, _ := g.proxychainsConfContent()
	if !strings.Contains(conf, "http gw 3128 u p") {
		t.Errorf("expected authed http proxy line, got:\n%s", conf)
	}
}

func TestWrapToolCommandPassthroughWhenUnsupported(t *testing.T) {
	// No proxychains binary is required: a disabled gateway must return the
	// command unchanged so execution is never broken.
	g, _ := New(Config{Enabled: false})
	name, args := g.WrapToolCommand("nmap", []string{"-sT", "-Pn", "target"})
	if name != "nmap" || len(args) != 3 {
		t.Fatalf("expected unchanged command, got %s %v", name, args)
	}
}

func TestProxychainsConfPathWritesFile(t *testing.T) {
	g, _ := New(Config{Enabled: true, Mode: ModeTor, DNSThroughProxy: true})
	path, err := g.ProxychainsConfPath()
	if err != nil {
		t.Fatalf("ProxychainsConfPath: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read conf: %v", err)
	}
	if !strings.Contains(string(data), "[ProxyList]") {
		t.Error("written conf missing [ProxyList]")
	}
	// Second call returns the same cached path.
	path2, _ := g.ProxychainsConfPath()
	if path2 != path {
		t.Errorf("conf path not cached: %s vs %s", path, path2)
	}
}
