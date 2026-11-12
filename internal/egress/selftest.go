package egress

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// DefaultEchoEndpoints are plain-text "what is my IP" services queried by the
// self-test. Each must return the caller's public IP as the response body.
// Multiple are listed so a single one being down does not fail the check.
var DefaultEchoEndpoints = []string{
	"https://api.ipify.org",
	"https://ifconfig.me/ip",
	"https://icanhazip.com",
}

// LeakReport is the result of a self-test comparing the IP seen through the
// gateway against the real outbound IP.
type LeakReport struct {
	GatewayEnabled bool   // whether the gateway was active during the check
	Mode           Mode   // routing mode used
	ExitIP         string // public IP observed through the gateway
	DirectIP       string // public IP observed on a direct connection (real IP)
	Leaking        bool   // true when ExitIP == DirectIP (or exit IP unavailable under fail-open)
	Checked        bool   // true when both IPs were resolved and compared
	Detail         string // human-readable summary
}

// CheckExitIP returns the public IP address as seen through the gateway. This
// is the IP the outside world attributes to the platform's traffic. Endpoints
// defaults to DefaultEchoEndpoints when nil.
func (g *Gateway) CheckExitIP(ctx context.Context, endpoints []string) (string, error) {
	if len(endpoints) == 0 {
		endpoints = DefaultEchoEndpoints
	}
	client := g.HTTPClient(20 * time.Second)
	return fetchIP(ctx, client, endpoints)
}

// VerifyNoLeak fetches the public IP both through the gateway and on a direct
// connection, then reports whether the real IP is leaking. It is the
// pre-flight check to run before any sensitive task: if Leaking is true, the
// platform's traffic is not actually anonymized.
func (g *Gateway) VerifyNoLeak(ctx context.Context, endpoints []string) (*LeakReport, error) {
	rep := &LeakReport{GatewayEnabled: g.Enabled(), Mode: g.Mode()}

	if !g.Enabled() {
		rep.Leaking = true
		rep.Detail = "egress gateway is disabled: all traffic uses the real IP"
		return rep, nil
	}

	// Direct IP: always via a bare transport, bypassing the gateway on purpose
	// so we know the real address to compare against.
	directClient := &http.Client{
		Timeout:   20 * time.Second,
		Transport: &http.Transport{DialContext: (&net.Dialer{Timeout: 15 * time.Second}).DialContext},
	}
	directIP, directErr := fetchIP(ctx, directClient, endpoints)
	rep.DirectIP = directIP

	exitIP, exitErr := g.CheckExitIP(ctx, endpoints)
	rep.ExitIP = exitIP

	switch {
	case exitErr != nil && g.FailClosed():
		// Under fail-closed, inability to reach the proxy is the safe state:
		// nothing goes out, so nothing leaks.
		rep.Leaking = false
		rep.Detail = fmt.Sprintf("exit IP unavailable (fail-closed, no traffic leaves): %v", exitErr)
		return rep, nil
	case exitErr != nil:
		rep.Leaking = true
		rep.Detail = fmt.Sprintf("could not confirm exit IP through gateway (fail-open risk): %v", exitErr)
		return rep, nil
	case directErr != nil:
		// We have an exit IP but no baseline; cannot prove non-leak, but the
		// gateway path is working.
		rep.Detail = fmt.Sprintf("exit IP %s confirmed; direct baseline unavailable: %v", exitIP, directErr)
		return rep, nil
	}

	rep.Checked = true
	rep.Leaking = strings.TrimSpace(exitIP) == strings.TrimSpace(directIP)
	if rep.Leaking {
		rep.Detail = fmt.Sprintf("LEAK: traffic exits on the real IP %s despite gateway mode %s", exitIP, g.Mode())
	} else {
		rep.Detail = fmt.Sprintf("OK: exit IP %s differs from real IP %s", exitIP, directIP)
	}
	return rep, nil
}

func fetchIP(ctx context.Context, client *http.Client, endpoints []string) (string, error) {
	var lastErr error
	for _, ep := range endpoints {
		ip, err := fetchOne(ctx, client, ep)
		if err != nil {
			lastErr = err
			continue
		}
		if ip != "" {
			return ip, nil
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no endpoint returned an IP")
	}
	return "", lastErr
}

func fetchOne(ctx context.Context, client *http.Client, endpoint string) (string, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 128))
	if err != nil {
		return "", err
	}
	candidate := strings.TrimSpace(string(body))
	if net.ParseIP(candidate) == nil {
		return "", fmt.Errorf("endpoint %s returned non-IP %q", endpoint, candidate)
	}
	return candidate, nil
}
