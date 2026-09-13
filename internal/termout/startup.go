package termout

import (
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
)

// StartupWebUIOptions configures the startup Web UI banner.
type StartupWebUIOptions struct {
	Scheme       string
	Host         string
	Port         int
	SelfSigned   bool
	HTTPRedirect bool
}

// PrintConfigCreated prints a short notice when config.yaml is bootstrapped.
func PrintConfigCreated() {
	s := New(os.Stdout)
	s.Println("")
	s.Println(s.Green("✔ ") + s.Bold("已创建 config.yaml") + s.Dim("（来自 config.example.yaml）"))
	s.BlankLine()
}

// startupHosts 返回横幅应展示的访问地址。通配地址（含空 host）展开为回环地址
// 加本机非回环 IPv4；显式 host 原样展示。横幅此前硬编码 127.0.0.1，导致
// 绑定 0.0.0.0 的用户误以为 server.host 配置未生效（issue #301）。
func startupHosts(host string) []string {
	host = strings.TrimSpace(host)
	if host != "" && host != "0.0.0.0" && host != "::" && host != "[::]" {
		return []string{host}
	}
	hosts := []string{"127.0.0.1"}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return hosts
	}
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok || ipNet.IP.IsLoopback() || ipNet.IP.To4() == nil {
			continue
		}
		ip := ipNet.IP.String()
		dup := false
		for _, h := range hosts {
			if h == ip {
				dup = true
				break
			}
		}
		if !dup {
			hosts = append(hosts, ip)
		}
	}
	return hosts
}

// PrintStartupWebUI prints a colored startup banner for the Web UI.
func PrintStartupWebUI(opts StartupWebUIOptions) {
	printStartupWebUI(os.Stdout, opts)
}

func printStartupWebUI(out io.Writer, opts StartupWebUIOptions) {
	s := New(out)
	scheme := opts.Scheme
	if scheme == "" {
		scheme = "http"
	}
	port := opts.Port
	if port <= 0 {
		port = 8080
	}
	hosts := startupHosts(opts.Host)
	urlFor := func(h string) string {
		return scheme + "://" + net.JoinHostPort(h, strconv.Itoa(port)) + "/"
	}

	s.BlankLine()
	s.Println(s.Bold(s.Cyan("CYBERSTRIKE AI")) + s.Dim("  /  secure workspace"))
	s.Println(s.Dim(strings.Repeat("─", 60)))
	s.Println(s.Green("● ONLINE") + "   " + s.Bold(s.White(urlFor(hosts[0]))))
	for _, h := range hosts[1:] {
		s.Println(s.Dim("  Network  ") + s.Bold(s.White(urlFor(h))))
	}
	if opts.SelfSigned {
		s.Println(s.Dim("  TLS      ") + s.Yellow("self-signed") + s.Dim(" · accept the browser warning once"))
	}
	if opts.HTTPRedirect {
		s.Println(s.Dim("  Redirect ") + fmt.Sprintf("http://%s/ → HTTPS", net.JoinHostPort(hosts[0], strconv.Itoa(port))))
	}
	s.BlankLine()
}

// PrintBootstrapAdminCredentials prints the initial admin password banner.
func PrintBootstrapAdminCredentials(password string) {
	password = strings.TrimSpace(password)
	if password == "" {
		return
	}

	s := New(os.Stdout)
	s.Println(s.Bold(s.Yellow("ADMIN SETUP REQUIRED")))
	s.Println(s.Dim(strings.Repeat("─", 60)))
	s.Println(s.Dim("  Username  ") + s.Bold(s.White("admin")))
	s.Println(s.Dim("  Password  ") + s.Bold(s.Yellow(password)))
	s.BlankLine()
	s.Println(s.Yellow("  ! ") + s.White("Store this password securely. It is shown only once."))
	s.Println(s.Dim("    Change it in Settings immediately after signing in."))
	s.BlankLine()
}
