package egress

import (
	"fmt"
	"net/url"
)

// ProxyEnv returns the environment-variable assignments ("KEY=value") that
// steer child processes (curl, wget, go, python-requests, and any tool that
// honors the standard proxy variables) through the gateway's proxy. It returns
// nil when the gateway is disabled so callers append nothing.
//
// Scope and limits: this covers tools that read HTTP_PROXY/HTTPS_PROXY/
// ALL_PROXY. It does NOT capture tools that open raw sockets and ignore those
// variables (nmap's scan engine, masscan, custom implants). Those require a
// transparent wrapper such as proxychains and are handled separately; env
// injection is the broad first layer, not a complete guarantee for every tool.
func (g *Gateway) ProxyEnv() []string {
	if g == nil || !g.Enabled() {
		return nil
	}

	var proxyURL string
	switch g.mode {
	case ModeSOCKS5, ModeTor:
		// socks5h asks the proxy to resolve DNS when DNSThroughProxy is set,
		// so hostnames are not leaked to the local resolver.
		scheme := "socks5"
		if g.cfg.DNSThroughProxy {
			scheme = "socks5h"
		}
		proxyURL = buildProxyURL(scheme, g.addr, g.cfg.Username, g.cfg.Password)
	case ModeHTTP:
		proxyURL = buildProxyURL("http", g.addr, g.cfg.Username, g.cfg.Password)
	case ModeHTTPS:
		proxyURL = buildProxyURL("https", g.addr, g.cfg.Username, g.cfg.Password)
	default:
		return nil
	}

	// Set both lower- and upper-case forms: some tools read one, some the other.
	return []string{
		"HTTP_PROXY=" + proxyURL,
		"HTTPS_PROXY=" + proxyURL,
		"ALL_PROXY=" + proxyURL,
		"http_proxy=" + proxyURL,
		"https_proxy=" + proxyURL,
		"all_proxy=" + proxyURL,
	}
}

func buildProxyURL(scheme, addr, user, pass string) string {
	if user == "" && pass == "" {
		return fmt.Sprintf("%s://%s", scheme, addr)
	}
	u := url.URL{Scheme: scheme, Host: addr, User: url.UserPassword(user, pass)}
	return u.String()
}
