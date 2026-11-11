package openai

import (
	"context"
	"cyberstrike-ai/internal/config"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestCustomHeadersDoNotCrossRedirectOrigin(t *testing.T) {
	for _, kind := range []string{"eino", "builtin"} {
		t.Run(kind, func(t *testing.T) {
			targetCalls := 0
			dest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetCalls++; w.Write([]byte(`{}`)) }))
			defer dest.Close()
			for _, mode := range []string{"port", "host", "downgrade"} {
				t.Run(mode, func(t *testing.T) {
					target := dest.URL
					if mode == "host" {
						target = strings.Replace(target, "127.0.0.1", "localhost", 1)
					}
					handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target, 302) })
					var src *httptest.Server
					if mode == "downgrade" {
						src = httptest.NewTLSServer(handler)
					} else {
						src = httptest.NewServer(handler)
					}
					defer src.Close()
					cfg := &config.OpenAIConfig{BaseURL: src.URL, APIKey: "test-key", Headers: map[string]string{"Authorization": "Bearer test-private", "X-Private-Key": "private"}}
					var err error
					if kind == "eino" {
						var resp *http.Response
						resp, err = NewEinoHTTPClient(cfg, src.Client()).Get(src.URL)
						if resp != nil {
							resp.Body.Close()
						}
					} else {
						var out interface{}
						err = NewClient(cfg, src.Client(), nil).ChatCompletion(context.Background(), map[string]string{}, &out)
					}
					if err == nil || !strings.Contains(err.Error(), "cross-origin") {
						t.Fatalf("expected blocked redirect: %v", err)
					}
					if targetCalls != 0 {
						t.Fatalf("credentials could reach other origin: %d requests", targetCalls)
					}
				})
			}
		})
	}
}
func TestCustomHeadersSameOriginRedirectAndClientIsolation(t *testing.T) {
	finalCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/final" {
			http.Redirect(w, r, "/final", 307)
			return
		}
		finalCalls++
		if r.Header.Get("Authorization") != "Bearer custom" || r.Header.Get("X-Private-Key") != "private" {
			t.Error("same-origin headers lost")
		}
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	cfg := &config.OpenAIConfig{BaseURL: srv.URL, APIKey: "key", Headers: map[string]string{"Authorization": "Bearer custom", "X-Private-Key": "private"}}
	base := srv.Client()
	callbacks := 0
	base.CheckRedirect = func(req *http.Request, via []*http.Request) error { callbacks++; return nil }
	resp, err := NewEinoHTTPClient(cfg, base).Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	var out interface{}
	if err = NewClient(cfg, base, nil).ChatCompletion(context.Background(), map[string]string{}, &out); err != nil {
		t.Fatal(err)
	}
	if finalCalls != 2 || callbacks != 2 {
		t.Fatalf("calls=%d callbacks=%d", finalCalls, callbacks)
	}
	// Existing callbacks are preserved; they can decline a redirect without a request.
	base.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err = NewEinoHTTPClient(cfg, base).Get(srv.URL)
	if err != nil || resp.StatusCode != 307 {
		t.Fatalf("callback not preserved: %v", err)
	}
	resp.Body.Close()
	if customHeaderHTTPClient(base, nil) != base {
		t.Fatal("no-header client changed")
	}
	// RoundTrip must not mutate a request owned by the caller.
	req, _ := http.NewRequest("GET", srv.URL+"/final", nil)
	resp, err = NewEinoHTTPClient(cfg, base).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if req.Header.Get("Authorization") != "" {
		t.Fatal("request mutated")
	}
}
func TestHeaderRedirectPolicyLimitsAndCallbackMutation(t *testing.T) {
	cfg := &config.OpenAIConfig{Headers: map[string]string{"X-Private": "value"}}
	req, _ := http.NewRequest("GET", "https://example.com:443/next", nil)
	first, _ := http.NewRequest("GET", "https://example.com/start", nil)
	client := customHeaderHTTPClient(nil, cfg)
	if err := client.CheckRedirect(req, []*http.Request{first}); err != nil {
		t.Fatal(err)
	}
	if client.CheckRedirect(req, make([]*http.Request, 10)) == nil {
		t.Fatal("redirect loop limit lost")
	}
	base := &http.Client{CheckRedirect: func(r *http.Request, _ []*http.Request) error {
		r.URL, _ = url.Parse("https://other.invalid")
		return nil
	}}
	if customHeaderHTTPClient(base, cfg).CheckRedirect(req, []*http.Request{first}) == nil {
		t.Fatal("callback rewrote destination across boundary")
	}
	sentinel := fmt.Errorf("custom decline")
	base.CheckRedirect = func(*http.Request, []*http.Request) error { return sentinel }
	if customHeaderHTTPClient(base, cfg).CheckRedirect(req, nil) != sentinel {
		t.Fatal("custom error lost")
	}
}
