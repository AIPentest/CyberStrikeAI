package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"cyberstrike-ai/internal/config"
	"github.com/gin-gonic/gin"
)

func TestOIDCDefaultDiscoveryURL(t *testing.T) {
	var issuer string
	var requestedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(discoveryDocument{
			Issuer:                issuer,
			AuthorizationEndpoint: issuer + "/authorize",
			TokenEndpoint:         issuer + "/token",
			JWKSURI:               issuer + "/jwks",
		})
	}))
	defer server.Close()
	issuer = server.URL

	h := NewOIDCHandler(config.OIDCConfig{AllowHTTP: true, Issuer: issuer}, nil)
	d, err := h.discovery(context.Background())
	if err != nil {
		t.Fatalf("default discovery failed: %v", err)
	}
	if requestedPath != "/.well-known/openid-configuration" || d.Issuer != issuer {
		t.Fatalf("unexpected discovery path %q or issuer %q", requestedPath, d.Issuer)
	}
}

func TestOIDCCustomDiscoveryURL(t *testing.T) {
	const issuer = "http://issuer.example.test"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/oauth2/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(discoveryDocument{
			Issuer:                issuer,
			AuthorizationEndpoint: issuer + "/authorize",
			TokenEndpoint:         issuer + "/token",
			JWKSURI:               issuer + "/jwks",
		})
	}))
	defer server.Close()

	h := NewOIDCHandler(config.OIDCConfig{
		AllowHTTP:    true,
		Issuer:       issuer,
		DiscoveryURL: server.URL + "/api/oauth2/.well-known/openid-configuration",
	}, nil)
	d, err := h.discovery(context.Background())
	if err != nil {
		t.Fatalf("custom discovery failed: %v", err)
	}
	if d.Issuer != issuer {
		t.Fatalf("unexpected issuer: %s", d.Issuer)
	}
}

func TestOIDCRejectsHTTPDiscoveryByDefault(t *testing.T) {
	h := NewOIDCHandler(config.OIDCConfig{
		Issuer:       "https://issuer.example.test",
		DiscoveryURL: "http://issuer.example.test/.well-known/openid-configuration",
	}, nil)
	if _, err := h.discovery(context.Background()); err == nil {
		t.Fatal("expected HTTP discovery to be rejected")
	}
}

func TestOIDCConfigLoginLabel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewOIDCHandler(config.OIDCConfig{Enabled: true, LoginLabel: "Company SSO"}, nil)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/auth/oidc/config", nil)
	h.Config(c)
	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", w.Code)
	}
	var response struct {
		Enabled bool   `json:"enabled"`
		Label   string `json:"login_label"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Enabled || response.Label != "Company SSO" {
		t.Fatalf("unexpected config response: %+v", response)
	}
}

func TestOIDCCookieSecureBasedOnRedirect(t *testing.T) {
	for _, tc := range []struct {
		uri    string
		secure bool
	}{
		{"https://example.test/api/auth/oidc/callback", true},
		{"http://localhost:8082/api/auth/oidc/callback", false},
	} {
		h := NewOIDCHandler(config.OIDCConfig{RedirectURI: tc.uri}, nil)
		if h.callbackCookieSecure() != tc.secure {
			t.Errorf("%s: expected secure=%t", tc.uri, tc.secure)
		}
	}
}

func TestOIDCConfigDefaultLoginLabel(t *testing.T) {
	h := NewOIDCHandler(config.OIDCConfig{Enabled: true}, nil)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/auth/oidc/config", nil)
	h.Config(c)
	var response struct {
		Label string `json:"login_label"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Label != "Sign in with SSO" {
		t.Fatalf("unexpected default label: %q", response.Label)
	}
}
