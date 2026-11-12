package egress

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestVerifyNoLeakDisabledReportsLeak(t *testing.T) {
	g, _ := New(Config{Enabled: false})
	rep, err := g.VerifyNoLeak(context.Background(), nil)
	if err != nil {
		t.Fatalf("VerifyNoLeak: %v", err)
	}
	if !rep.Leaking {
		t.Fatal("disabled gateway should report Leaking=true")
	}
	if rep.GatewayEnabled {
		t.Fatal("GatewayEnabled should be false")
	}
}

func TestCheckExitIPParsesEcho(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("203.0.113.7\n"))
	}))
	defer srv.Close()

	// Disabled gateway dials the test server directly; this exercises the
	// echo-parsing path without needing a real proxy.
	g, _ := New(Config{Enabled: false})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ip, err := g.CheckExitIP(ctx, []string{srv.URL})
	if err != nil {
		t.Fatalf("CheckExitIP: %v", err)
	}
	if ip != "203.0.113.7" {
		t.Fatalf("ip = %q, want 203.0.113.7", ip)
	}
}

func TestCheckExitIPRejectsNonIP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not-an-ip"))
	}))
	defer srv.Close()

	g, _ := New(Config{Enabled: false})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := g.CheckExitIP(ctx, []string{srv.URL}); err == nil {
		t.Fatal("expected error for non-IP echo body")
	}
}
