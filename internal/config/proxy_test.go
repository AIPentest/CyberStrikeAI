package config

import "testing"

func TestProxyValidation(t *testing.T) {
	for _, p := range []ProxyConfig{{}, {Mode: "manual"}, {Mode: "fixed", URL: "http://u:p@localhost:8080"}, {Mode: "pool", Pool: "[::1]:8080\nlocalhost:8081", MinSwitchInterval: 60}} {
		if err := p.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []ProxyConfig{{Mode: "bad"}, {Mode: "fixed"}, {Mode: "fixed", URL: "file:///secret"}, {Mode: "pool"}, {Mode: "pool", Pool: "http://host:80/path"}, {Mode: "pool", Source: "file:///secret"}, {MinSwitchInterval: -1}, {MaxAttempts: 11}} {
		if p.Validate() == nil {
			t.Fatalf("accepted invalid config: mode=%s", p.Mode)
		}
	}
}
