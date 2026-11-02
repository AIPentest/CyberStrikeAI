package assetintel

import (
	"regexp"
	"strings"
)

// masscan 默认把结果打到 stdout，常见两种行：
//
//	Discovered open port 443/tcp on 1.2.3.4
//	open tcp 80 1.2.3.4 1690000000
var (
	masscanDiscoveredRe = regexp.MustCompile(`(?i)Discovered open port\s+(\d{1,5})/(tcp|udp)\s+on\s+(\S+)`)
	masscanOpenRe       = regexp.MustCompile(`^open\s+(tcp|udp)\s+(\d{1,5})\s+(\S+)`)
)

// parseMasscan 解析 masscan 输出，提取 ip/port/协议。
func parseMasscan(output string) []Host {
	var hosts []Host
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(stripANSI(line))
		if line == "" {
			continue
		}
		if m := masscanDiscoveredRe.FindStringSubmatch(line); m != nil {
			appendMasscan(&hosts, m[1], m[2], m[3])
			continue
		}
		if m := masscanOpenRe.FindStringSubmatch(line); m != nil {
			appendMasscan(&hosts, m[2], m[1], m[3])
		}
	}
	return finalize(toolMasscan, hosts)
}

// appendMasscan 校验并追加一条 masscan 结果。
func appendMasscan(hosts *[]Host, portRaw, transport, hostRaw string) {
	port := normalizePort(parsePortToken(portRaw))
	// 去掉 grepable 行尾可能带的逗号/分号；不能动 "."，IPv4 需要它。
	host := normalizeHostname(strings.TrimRight(hostRaw, ",;"))
	if port == 0 || host == "" {
		return
	}
	*hosts = append(*hosts, Host{
		Host:     host,
		Port:     port,
		Protocol: normalizeProtocol(transport),
	})
}
