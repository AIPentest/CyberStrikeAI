package app

import (
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"cyberstrike-ai/internal/config"

	"golang.org/x/net/http2"
)

func TestNewHTTPToHTTPSRedirectHandler(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		httpsPort  int
		host       string
		uri        string
		wantTarget string
	}{
		{
			name:       "non standard port",
			httpsPort:  8080,
			host:       "127.0.0.1:8080",
			uri:        "/login?next=/",
			wantTarget: "https://127.0.0.1:8080/login?next=/",
		},
		{
			name:       "standard port",
			httpsPort:  443,
			host:       "example.com:80",
			uri:        "/",
			wantTarget: "https://example.com/",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := newHTTPToHTTPSRedirectHandler(tt.httpsPort)
			req := httptest.NewRequest(http.MethodGet, "http://"+tt.host+tt.uri, nil)
			req.Host = tt.host
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusPermanentRedirect {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusPermanentRedirect)
			}
			if got := rec.Header().Get("Location"); got != tt.wantTarget {
				t.Fatalf("Location = %q, want %q", got, tt.wantTarget)
			}
		})
	}
}

func TestIsTLSHandshakeRecord(t *testing.T) {
	t.Parallel()
	if !isTLSHandshakeRecord(0x16) {
		t.Fatal("expected TLS handshake record")
	}
	if isTLSHandshakeRecord('G') {
		t.Fatal("GET should not be TLS")
	}
}

func TestServerHTTPRedirectEnabled(t *testing.T) {
	t.Parallel()
	disabled := false
	enabled := true
	if config.ServerHTTPRedirectEnabled(nil) {
		t.Fatal("nil config should disable redirect")
	}
	if !config.ServerHTTPRedirectEnabled(&config.ServerConfig{TLSEnabled: true}) {
		t.Fatal("HTTPS without explicit flag should enable redirect")
	}
	if config.ServerHTTPRedirectEnabled(&config.ServerConfig{TLSEnabled: true, TLSHTTPRedirect: &disabled}) {
		t.Fatal("explicit false should disable redirect")
	}
	if !config.ServerHTTPRedirectEnabled(&config.ServerConfig{TLSEnabled: true, TLSHTTPRedirect: &enabled}) {
		t.Fatal("explicit true should enable redirect")
	}
	if config.ServerHTTPRedirectEnabled(&config.ServerConfig{}) {
		t.Fatal("plain HTTP should not redirect")
	}
}

func TestMainServerMuxHTTPRedirectAndHTTPS(t *testing.T) {
	_, tlsConfig, _, _, err := prepareMainServerTLS(&config.ServerConfig{TLSAutoSelfSign: true}, t.TempDir())
	if err != nil {
		t.Fatalf("generate cert: %v", err)
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	})
	srv := &http.Server{Handler: handler, TLSConfig: tlsConfig}
	if err := http2.ConfigureServer(srv, &http2.Server{}); err != nil {
		t.Fatalf("configure http2: %v", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	mux := newMainServerMux(ln, srv, portFromListenAddr(ln.Addr().String()), nil)
	go func() { _ = mux.Serve() }()

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12},
		},
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	addr := ln.Addr().String()

	httpResp, err := client.Get("http://" + addr + "/")
	if err != nil {
		t.Fatalf("http get: %v", err)
	}
	_ = httpResp.Body.Close()
	if httpResp.StatusCode != http.StatusPermanentRedirect {
		t.Fatalf("http status = %d, want %d", httpResp.StatusCode, http.StatusPermanentRedirect)
	}
	if got := httpResp.Header.Get("Location"); got != "https://127.0.0.1:"+strconv.Itoa(portFromListenAddr(addr))+"/" {
		t.Fatalf("Location = %q", got)
	}

	httpsResp, err := client.Get("https://" + addr + "/")
	if err != nil {
		t.Fatalf("https get: %v", err)
	}
	defer httpsResp.Body.Close()
	if httpsResp.StatusCode != http.StatusOK {
		t.Fatalf("https status = %d, want %d", httpsResp.StatusCode, http.StatusOK)
	}
	body, _ := io.ReadAll(httpsResp.Body)
	if string(body) != "ok" {
		t.Fatalf("body = %q, want ok", body)
	}
}
