package assetintel

import (
	"net"
	"strings"
)

// parseSubfinder 解析 subfinder 输出：每行一个子域名（也容忍 URL、IP、通配符）。
func parseSubfinder(output string) []Host {
	var hosts []Host
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(stripANSI(line))
		if line == "" || isLogLine(line) {
			continue
		}
		// 注释行。
		if strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") || strings.HasPrefix(line, ";") {
			continue
		}
		// subfinder 偶尔输出通配符结果。
		line = strings.TrimPrefix(line, "*.")
		host := normalizeHostname(line)
		if host == "" {
			continue
		}
		// 子域名至少应含一个点；IPv4/IPv6 单独放行，避免把横幅文字当主机。
		if net.ParseIP(host) == nil && !strings.Contains(host, ".") {
			continue
		}
		hosts = append(hosts, Host{Host: host})
	}
	return finalize(toolSubfinder, hosts)
}
