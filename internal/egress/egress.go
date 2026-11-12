// Package egress provides a single, auditable chokepoint for all outbound
// network traffic so the operator's real IP address is never exposed while
// the platform is active.
//
// Every outbound HTTP client, raw dialer, and (later) spawned external tool
// is meant to route through a Gateway built here. The Gateway sends traffic
// through a configured SOCKS5 or HTTP(S) proxy — typically Tor, a VPN
// gateway, or a proxy pool — and, when fail-closed is enabled, refuses to
// dial at all if that upstream proxy is unreachable rather than silently
// falling back to a direct connection that would leak the real address.
//
// The package deliberately has no dependency on internal/config: the
// application layer translates the user configuration into egress.Config and
// wires the resulting Gateway into the subsystems that make outbound calls.
package egress

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	xproxy "golang.org/x/net/proxy"
)

// Mode selects how outbound traffic is routed.
type Mode string

const (
	// ModeDirect disables anonymization: traffic goes out on the real
	// interface. Used when egress is not enabled.
	ModeDirect Mode = "direct"
	// ModeSOCKS5 routes every dial through a SOCKS5 proxy. This is the
	// universal path: it carries any TCP stream and, when DNSThroughProxy
	// is set, resolves hostnames at the proxy so DNS never leaks.
	ModeSOCKS5 Mode = "socks5"
	// ModeTor is an alias for SOCKS5 that defaults to Tor's local port
	// (127.0.0.1:9050) when no address is given.
	ModeTor Mode = "tor"
	// ModeHTTP routes through an HTTP proxy (CONNECT for raw dials).
	ModeHTTP Mode = "http"
	// ModeHTTPS routes through an HTTPS proxy (CONNECT over TLS to the proxy).
	ModeHTTPS Mode = "https"
)

// DefaultTorAddress is used when Mode is "tor" and no address is configured.
const DefaultTorAddress = "127.0.0.1:9050"

// ErrEgressBlocked is returned when a dial is refused because the egress
// gateway is active and fail-closed but the upstream proxy cannot be used.
// Callers should treat this as "the request did not and must not go out",
// never as a reason to retry on a direct connection.
var ErrEgressBlocked = errors.New("egress: outbound blocked to prevent real-IP leak (fail-closed)")

// Config is the self-contained egress configuration consumed by New.
type Config struct {
	// Enabled turns the anonymizing gateway on. When false, the Gateway is a
	// transparent passthrough (direct dials) and FailClosed is ignored.
	Enabled bool
	// Mode selects the proxy protocol (socks5/tor/http/https/direct).
	Mode Mode
	// Address is the proxy endpoint as host:port.
	Address string
	// Username/Password are optional proxy credentials.
	Username string
	Password string
	// FailClosed, when true, refuses any outbound dial if the proxy is
	// unreachable instead of falling back to a direct connection. This is the
	// kill-switch: no request ever leaves on the real IP.
	FailClosed bool
	// DNSThroughProxy, for SOCKS5/Tor, asks the proxy to resolve hostnames so
	// DNS queries do not leak to the local resolver / ISP.
	DNSThroughProxy bool
	// DialTimeout bounds how long a single dial (including to the proxy) may
	// take. Zero uses a sane default.
	DialTimeout time.Duration
	// ProxychainsMode controls whether spawned external tools (nmap and other
	// raw-socket tools that ignore proxy env vars) are wrapped with
	// proxychains: "auto"/"" wraps when a proxychains binary is present,
	// "off" never wraps. Only meaningful for socks5/tor/http/https modes.
	ProxychainsMode string
}

// Gateway is the outbound chokepoint. It is safe for concurrent use.
type Gateway struct {
	cfg      Config
	mode     Mode
	addr     string
	proxyURL *url.URL      // set for http/https modes
	forward  xproxy.Dialer // set for socks5/tor modes
	fwdCtx   xproxy.ContextDialer
	enabled  bool
	mu       sync.RWMutex

	// proxychains config caching (generated once, reused for every wrap)
	pcOnce sync.Once
	pcPath string
	pcErr  error
}

// New builds a Gateway from cfg. It validates the configuration and, for
// SOCKS5/Tor, constructs the proxy dialer up front so a bad address fails
// loudly at startup rather than on the first request.
func New(cfg Config) (*Gateway, error) {
	g := &Gateway{cfg: cfg, enabled: cfg.Enabled}

	if !cfg.Enabled {
		g.mode = ModeDirect
		return g, nil
	}

	g.mode = Mode(strings.ToLower(strings.TrimSpace(string(cfg.Mode))))
	g.addr = strings.TrimSpace(cfg.Address)

	switch g.mode {
	case ModeTor:
		if g.addr == "" {
			g.addr = DefaultTorAddress
		}
		fallthrough
	case ModeSOCKS5:
		if g.addr == "" {
			return nil, fmt.Errorf("egress: socks5 mode requires an address")
		}
		var auth *xproxy.Auth
		if cfg.Username != "" || cfg.Password != "" {
			auth = &xproxy.Auth{User: cfg.Username, Password: cfg.Password}
		}
		fwd, err := xproxy.SOCKS5("tcp", g.addr, auth, xproxy.Direct)
		if err != nil {
			return nil, fmt.Errorf("egress: build socks5 dialer: %w", err)
		}
		g.forward = fwd
		if cd, ok := fwd.(xproxy.ContextDialer); ok {
			g.fwdCtx = cd
		}
	case ModeHTTP, ModeHTTPS:
		if g.addr == "" {
			return nil, fmt.Errorf("egress: %s mode requires an address", g.mode)
		}
		scheme := "http"
		if g.mode == ModeHTTPS {
			scheme = "https"
		}
		u := &url.URL{Scheme: scheme, Host: g.addr}
		if cfg.Username != "" {
			u.User = url.UserPassword(cfg.Username, cfg.Password)
		}
		g.proxyURL = u
	case ModeDirect, "":
		// Enabled but direct: treat as disabled passthrough.
		g.enabled = false
		g.mode = ModeDirect
	default:
		return nil, fmt.Errorf("egress: unknown mode %q", cfg.Mode)
	}

	return g, nil
}

// Enabled reports whether the gateway is actively anonymizing traffic.
func (g *Gateway) Enabled() bool {
	if g == nil {
		return false
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.enabled
}

// Mode reports the active routing mode.
func (g *Gateway) Mode() Mode {
	if g == nil {
		return ModeDirect
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.mode
}

// FailClosed reports whether direct fallback is forbidden.
func (g *Gateway) FailClosed() bool {
	return g != nil && g.cfg.FailClosed
}

func (g *Gateway) dialTimeout() time.Duration {
	if g.cfg.DialTimeout > 0 {
		return g.cfg.DialTimeout
	}
	return 30 * time.Second
}

// DialContext dials addr through the gateway. When the gateway is enabled it
// always routes through the proxy; if the proxy cannot be reached and
// fail-closed is on, it returns ErrEgressBlocked instead of a direct dial.
// When the gateway is disabled it dials directly (transparent passthrough).
func (g *Gateway) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	if g == nil || !g.Enabled() {
		d := &net.Dialer{Timeout: 30 * time.Second}
		return d.DialContext(ctx, network, addr)
	}

	switch g.mode {
	case ModeSOCKS5, ModeTor:
		conn, err := g.dialSOCKS(ctx, network, addr)
		if err != nil {
			return nil, g.wrapDialErr(err)
		}
		return conn, nil
	case ModeHTTP, ModeHTTPS:
		conn, err := g.dialHTTPConnect(ctx, addr)
		if err != nil {
			return nil, g.wrapDialErr(err)
		}
		return conn, nil
	default:
		// Enabled but no usable mode: never leak.
		if g.cfg.FailClosed {
			return nil, ErrEgressBlocked
		}
		d := &net.Dialer{Timeout: g.dialTimeout()}
		return d.DialContext(ctx, network, addr)
	}
}

// wrapDialErr converts an underlying proxy failure into ErrEgressBlocked when
// fail-closed is enabled, so callers never mistake it for a transient error
// that could be retried directly.
func (g *Gateway) wrapDialErr(err error) error {
	if g.cfg.FailClosed {
		return fmt.Errorf("%w: %v", ErrEgressBlocked, err)
	}
	return err
}

func (g *Gateway) dialSOCKS(ctx context.Context, network, addr string) (net.Conn, error) {
	if g.fwdCtx != nil {
		return g.fwdCtx.DialContext(ctx, network, addr)
	}
	// Fall back to the non-context dialer with a goroutine-bounded timeout.
	type res struct {
		conn net.Conn
		err  error
	}
	ch := make(chan res, 1)
	go func() {
		c, err := g.forward.Dial(network, addr)
		ch <- res{c, err}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-ch:
		return r.conn, r.err
	}
}

// dialHTTPConnect opens a tunnel to addr through an HTTP(S) proxy using the
// CONNECT method, so raw TCP streams (not just HTTP requests) are carried.
func (g *Gateway) dialHTTPConnect(ctx context.Context, addr string) (net.Conn, error) {
	d := &net.Dialer{Timeout: g.dialTimeout()}
	proxyConn, err := d.DialContext(ctx, "tcp", g.addr)
	if err != nil {
		return nil, fmt.Errorf("dial proxy %s: %w", g.addr, err)
	}

	if g.mode == ModeHTTPS {
		// Upgrade the hop to the proxy to TLS. Verification uses the proxy host.
		host, _, _ := net.SplitHostPort(g.addr)
		if host == "" {
			host = g.addr
		}
		proxyConn = tlsClientOnProxy(proxyConn, host)
		if tc, ok := proxyConn.(handshaker); ok {
			if err := tc.HandshakeContext(ctx); err != nil {
				proxyConn.Close()
				return nil, fmt.Errorf("tls handshake to proxy: %w", err)
			}
		}
	}

	req := &http.Request{
		Method: http.MethodConnect,
		URL:    &url.URL{Opaque: addr},
		Host:   addr,
		Header: make(http.Header),
	}
	if g.proxyURL != nil && g.proxyURL.User != nil {
		if pw, ok := g.proxyURL.User.Password(); ok {
			req.Header.Set("Proxy-Authorization", basicProxyAuth(g.proxyURL.User.Username(), pw))
		}
	}
	if err := req.Write(proxyConn); err != nil {
		proxyConn.Close()
		return nil, fmt.Errorf("write CONNECT: %w", err)
	}

	br := bufio.NewReader(proxyConn)
	resp, err := http.ReadResponse(br, req)
	if err != nil {
		proxyConn.Close()
		return nil, fmt.Errorf("read CONNECT response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		proxyConn.Close()
		return nil, fmt.Errorf("proxy CONNECT to %s refused: %s", addr, resp.Status)
	}
	return proxyConn, nil
}

// Transport returns an *http.Transport whose outbound traffic is routed
// through the gateway. For SOCKS5/Tor it installs the gateway DialContext;
// for HTTP(S) proxies it sets Proxy so the standard library drives CONNECT.
// When the gateway is disabled a plain transport is returned.
func (g *Gateway) Transport() *http.Transport {
	t := &http.Transport{
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   30 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	if g == nil || !g.Enabled() {
		t.DialContext = (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext
		return t
	}

	switch g.mode {
	case ModeHTTP, ModeHTTPS:
		pu := g.proxyURL
		t.Proxy = func(*http.Request) (*url.URL, error) { return pu, nil }
		// Even with Proxy set, pin the base dialer so a direct bypass is not
		// possible for non-proxied hops under fail-closed.
		t.DialContext = (&net.Dialer{Timeout: g.dialTimeout()}).DialContext
	default: // socks5 / tor
		t.DialContext = g.DialContext
	}
	return t
}

// HTTPClient returns an *http.Client wired to the gateway Transport.
func (g *Gateway) HTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: g.Transport()}
}

func basicProxyAuth(user, pass string) string {
	return "Basic " + basicAuthEncode(user+":"+pass)
}
