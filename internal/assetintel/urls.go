package assetintel

import (
	"net"
	"strings"
)

// parseURLList 解析 katana/gau/waybackurls 的“一行一个 URL”输出。
// URL 显式端口优先，未写端口时由 cleanHost 按 scheme 补默认端口；
// 同一主机多条 URL 交给 finalize 按 (host,port,protocol) 去重合并。
func parseURLList(tool, output string) []Host {
	var hosts []Host
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(stripANSI(line))
		if line == "" || isLogLine(line) || strings.HasPrefix(line, "#") {
			continue
		}
		// URL 不应含空白，借此过滤日志/统计行。
		if strings.ContainsAny(line, " \t") {
			continue
		}
		scheme, host, port := parseURLParts(line)
		if host == "" {
			continue
		}
		// 无 scheme 的裸行只接受域名/IP，避免把单个单词当主机。
		if scheme == "" && net.ParseIP(host) == nil && !strings.Contains(host, ".") {
			continue
		}
		hosts = append(hosts, Host{
			Host:     host,
			Port:     normalizePort(port),
			Protocol: scheme,
		})
	}
	return finalize(tool, hosts)
}
