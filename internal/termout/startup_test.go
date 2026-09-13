package termout

import (
	"bytes"
	"net"
	"strings"
	"testing"
)

func TestStartupHostsExplicitHost(t *testing.T) {
	for _, host := range []string{"192.168.1.5", "127.0.0.1", "10.0.0.8"} {
		got := startupHosts(host)
		if len(got) != 1 || got[0] != host {
			t.Fatalf("startupHosts(%q) = %v, want [%q]", host, got, host)
		}
	}
}

func TestStartupHostsWildcardExpandsToLocalAddresses(t *testing.T) {
	for _, host := range []string{"", "0.0.0.0", "::", "[::]"} {
		got := startupHosts(host)
		if len(got) == 0 || got[0] != "127.0.0.1" {
			t.Fatalf("startupHosts(%q) = %v, want first entry 127.0.0.1", host, got)
		}
		seen := map[string]bool{}
		for _, h := range got {
			if seen[h] {
				t.Fatalf("startupHosts(%q) contains duplicate %q", host, h)
			}
			seen[h] = true
			ip := net.ParseIP(h)
			if ip == nil {
				t.Fatalf("startupHosts(%q) returned non-IP %q", host, h)
			}
			if ip.IsLoopback() && h != "127.0.0.1" {
				t.Fatalf("startupHosts(%q) returned unexpected loopback %q", host, h)
			}
		}
	}
}

func TestPrintStartupWebUIReflectsConfiguredHost(t *testing.T) {
	var buf bytes.Buffer
	printStartupWebUI(&buf, StartupWebUIOptions{
		Scheme: "http",
		Host:   "192.168.1.5",
		Port:   8080,
	})
	out := buf.String()
	if !strings.Contains(out, "http://192.168.1.5:8080/") {
		t.Fatalf("banner should show configured host, got:\n%s", out)
	}
	if strings.Contains(out, "127.0.0.1") {
		t.Fatalf("banner should not fall back to 127.0.0.1 for explicit host, got:\n%s", out)
	}
}

func TestPrintStartupWebUIWildcardShowsLoopbackFirst(t *testing.T) {
	var buf bytes.Buffer
	printStartupWebUI(&buf, StartupWebUIOptions{
		Scheme: "https",
		Host:   "0.0.0.0",
		Port:   8443,
	})
	out := buf.String()
	if !strings.Contains(out, "https://127.0.0.1:8443/") {
		t.Fatalf("wildcard banner should include loopback URL, got:\n%s", out)
	}
	for _, h := range startupHosts("0.0.0.0")[1:] {
		if !strings.Contains(out, "https://"+h+":8443/") {
			t.Fatalf("wildcard banner should include network URL for %s, got:\n%s", h, out)
		}
	}
}

func TestPrintStartupWebUIIPv6HostBracketed(t *testing.T) {
	var buf bytes.Buffer
	printStartupWebUI(&buf, StartupWebUIOptions{
		Scheme: "http",
		Host:   "::1",
		Port:   8080,
	})
	if !strings.Contains(buf.String(), "http://[::1]:8080/") {
		t.Fatalf("IPv6 host should be bracketed in URL, got:\n%s", buf.String())
	}
}

func TestPrintStartupWebUIRedirectUsesConfiguredHost(t *testing.T) {
	var buf bytes.Buffer
	printStartupWebUI(&buf, StartupWebUIOptions{
		Scheme:       "https",
		Host:         "10.1.2.3",
		Port:         8080,
		HTTPRedirect: true,
	})
	out := buf.String()
	if !strings.Contains(out, "http://10.1.2.3:8080/") || !strings.Contains(out, "https://10.1.2.3:8080/") {
		t.Fatalf("redirect line should use configured host, got:\n%s", out)
	}
}

func TestDisplayWidthEmoji(t *testing.T) {
	if got := displayWidth("🚀"); got != 2 {
		t.Fatalf("displayWidth(emoji) = %d, want 2", got)
	}
	if got := displayWidth("ab"); got != 2 {
		t.Fatalf("displayWidth(ab) = %d, want 2", got)
	}
}

func TestDisplayWidthIgnoresANSI(t *testing.T) {
	s := New(nil)
	colored := s.Bold("admin")
	if got := displayWidth(colored); got != 5 {
		t.Fatalf("displayWidth colored = %d, want 5", got)
	}
}

func TestPadRightDisplay(t *testing.T) {
	got := padRightDisplay("pwd", 10)
	if displayWidth(got) != 10 {
		t.Fatalf("padded width = %d, want 10", displayWidth(got))
	}
}

func TestColorDisabledWithoutTTY(t *testing.T) {
	s := New(nil)
	if s.enabled {
		t.Fatal("expected colors disabled for nil writer")
	}
	if got := s.Cyan("x"); got != "x" {
		t.Fatalf("Cyan without TTY = %q, want plain text", got)
	}
}

func TestPrintBootstrapAdminCredentialsEmpty(t *testing.T) {
	PrintBootstrapAdminCredentials("   ")
}

func TestPrintStartupWebUIOptions(t *testing.T) {
	PrintStartupWebUI(StartupWebUIOptions{
		Scheme:       "https",
		Port:         8080,
		SelfSigned:   true,
		HTTPRedirect: true,
	})
}

func TestBoxRowAlignedWidth(t *testing.T) {
	s := New(nil)
	rows := []string{
		s.Bold("CyberStrikeAI") + s.White(" is ready"),
		s.Dim("Web UI   ") + s.Bold("https://127.0.0.1:8080/"),
	}
	inner := maxDisplayWidth(rows...)
	for _, row := range rows {
		line := s.boxRow(inner, row)
		if !strings.Contains(line, "│") {
			t.Fatalf("box row missing border: %q", line)
		}
	}
}

func TestMaxDisplayWidth(t *testing.T) {
	short := "abc"
	long := "https://127.0.0.1:8080/"
	if got := maxDisplayWidth(short, long); got != displayWidth(long) {
		t.Fatalf("maxDisplayWidth = %d, want %d", got, displayWidth(long))
	}
}
