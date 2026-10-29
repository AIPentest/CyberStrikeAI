package assetintel

import (
	"regexp"
	"strings"
)

// rustscan 非 greppable 输出 "Open 1.2.3.4:443"，
// greppable 输出汇总行 "1.2.3.4 -> [80,443]"。
var (
	rustscanOpenRe    = regexp.MustCompile(`(?i)^Open\s+(\S+):(\d{1,5})\s*$`)
	rustscanSummaryRe = regexp.MustCompile(`^(\S+)\s*->\s*\[([0-9,\s]*)\]`)
)

// parseRustscan 解析 rustscan 输出，过滤横幅与日志行。
func parseRustscan(output string) []Host {
	var hosts []Host
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(stripANSI(line))
		if line == "" {
			continue
		}
		if m := rustscanOpenRe.FindStringSubmatch(line); m != nil {
			if h, ok := rustscanHost(m[1], m[2]); ok {
				hosts = append(hosts, h)
			}
			continue
		}
		if m := rustscanSummaryRe.FindStringSubmatch(line); m != nil {
			host := normalizeHostname(m[1])
			if host == "" {
				continue
			}
			for _, part := range strings.Split(m[2], ",") {
				if h, ok := rustscanHost(host, strings.TrimSpace(part)); ok {
					hosts = append(hosts, h)
				}
			}
		}
	}
	return finalize(toolRustscan, hosts)
}

// rustscanHost 组装一条 rustscan 端口记录。
func rustscanHost(hostRaw, portRaw string) (Host, bool) {
	host := normalizeHostname(hostRaw)
	port := normalizePort(parsePortToken(portRaw))
	if host == "" || port == 0 {
		return Host{}, false
	}
	return Host{Host: host, Port: port}, true
}
