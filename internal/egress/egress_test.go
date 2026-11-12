package egress

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestNewDisabledIsPassthrough(t *testing.T) {
	g, err := New(Config{Enabled: false})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if g.Enabled() {
		t.Fatal("disabled gateway reports enabled")
	}
	if g.Mode() != ModeDirect {
		t.Fatalf("mode = %q, want direct", g.Mode())
	}
}

func TestNewTorDefaultsAddress(t *testing.T) {
	g, err := New(Config{Enabled: true, Mode: ModeTor})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if g.addr != DefaultTorAddress {
		t.Fatalf("tor address = %q, want %q", g.addr, DefaultTorAddress)
	}
	if !g.Enabled() {
		t.Fatal("tor gateway should be enabled")
	}
}

func TestNewUnknownModeFails(t *testing.T) {
	if _, err := New(Config{Enabled: true, Mode: "carrier-pigeon", Address: "x:1"}); err == nil {
		t.Fatal("expected error for unknown mode")
	}
}

func TestNewHTTPModeSetsProxyURL(t *testing.T) {
	g, err := New(Config{Enabled: true, Mode: ModeHTTP, Address: "127.0.0.1:8080", Username: "u", Password: "p"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if g.proxyURL == nil || g.proxyURL.Host != "127.0.0.1:8080" {
		t.Fatalf("proxyURL not set correctly: %+v", g.proxyURL)
	}
	if g.proxyURL.User == nil {
		t.Fatal("proxy credentials not set")
	}
}

func TestNewSocks5RequiresAddress(t *testing.T) {
	if _, err := New(Config{Enabled: true, Mode: ModeSOCKS5}); err == nil {
		t.Fatal("expected error when socks5 address is empty")
	}
}

func TestDisabledDialContextReachesListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err == nil {
			c.Close()
		}
	}()

	g, _ := New(Config{Enabled: false})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, err := g.DialContext(ctx, "tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("direct dial failed: %v", err)
	}
	conn.Close()
}

func TestFailClosedBlocksWhenProxyDown(t *testing.T) {
	// Point at a port with nothing listening so the proxy dial is refused.
	g, err := New(Config{
		Enabled:    true,
		Mode:       ModeSOCKS5,
		Address:    "127.0.0.1:1",
		FailClosed: true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err = g.DialContext(ctx, "tcp", "example.com:443")
	if err == nil {
		t.Fatal("expected fail-closed error, got nil (possible real-IP leak path)")
	}
	if !errors.Is(err, ErrEgressBlocked) {
		t.Fatalf("error = %v, want ErrEgressBlocked", err)
	}
}

func TestTransportNeverNil(t *testing.T) {
	for _, g := range []*Gateway{
		mustGW(t, Config{Enabled: false}),
		mustGW(t, Config{Enabled: true, Mode: ModeTor}),
		mustGW(t, Config{Enabled: true, Mode: ModeHTTP, Address: "127.0.0.1:8080"}),
	} {
		tr := g.Transport()
		if tr == nil || tr.DialContext == nil {
			t.Fatal("transport or its DialContext is nil")
		}
	}
}

func mustGW(t *testing.T, c Config) *Gateway {
	t.Helper()
	g, err := New(c)
	if err != nil {
		t.Fatalf("New(%+v): %v", c, err)
	}
	return g
}
