package openai

import (
	"cyberstrike-ai/internal/config"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// customHeaderHTTPClient does not mutate shared clients. Custom headers can
// contain credentials under arbitrary names, so none may follow a cross-origin
// redirect (including HTTPS downgrades). Checking only in the transport is too
// late: net/http also copies private headers into redirected requests.
func customHeaderHTTPClient(base *http.Client, cfg *config.OpenAIConfig) *http.Client {
	if base == nil {
		base = http.DefaultClient
	}
	if cfg == nil || len(cfg.Headers) == 0 {
		return base
	}
	cloned := *base
	previous := base.CheckRedirect
	cloned.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if previous != nil {
			if err := previous(req, via); err != nil {
				return err
			}
		} else if len(via) >= 10 {
			return fmt.Errorf("stopped after 10 redirects")
		}
		if len(via) == 0 || !sameHeaderOrigin(req.URL, via[0].URL) {
			return fmt.Errorf("cross-origin redirect blocked for model custom headers")
		}
		return nil
	}
	return &cloned
}

func sameHeaderOrigin(a, b *url.URL) bool {
	if a == nil || b == nil {
		return false
	}
	port := func(u *url.URL) string {
		if p := u.Port(); p != "" {
			return p
		}
		if strings.EqualFold(u.Scheme, "https") {
			return "443"
		}
		if strings.EqualFold(u.Scheme, "http") {
			return "80"
		}
		return ""
	}
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Hostname(), b.Hostname()) && port(a) == port(b)
}
