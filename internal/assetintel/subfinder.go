package assetintel

import "strings"

// parseSubfinder 解析 subfinder 输出：每行一个子域名（也容忍 URL、IP、通配符）。
func parseSubfinder(output string) []Host {
	var hosts []Host
	for _, line := range strings.Split(output, "\n") {
		if host := parsePlainHostLine(line); host != "" {
			hosts = append(hosts, Host{Host: host})
		}
	}
	return finalize(toolSubfinder, hosts)
}
