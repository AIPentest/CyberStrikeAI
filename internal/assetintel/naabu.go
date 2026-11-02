package assetintel

import (
	"encoding/json"
	"net"
	"strings"
)

// naabuJSON 对应 naabu -json 的单行对象。
type naabuJSON struct {
	Host string  `json:"host"`
	IP   string  `json:"ip"`
	Port flexInt `json:"port"`
}

// parseNaabu 解析 naabu 输出：JSON 行或纯文本 host:port。
func parseNaabu(output string) []Host {
	var hosts []Host
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(stripANSI(line))
		if line == "" || isLogLine(line) || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "{") {
			if h, ok := parseNaabuJSONLine(line); ok {
				hosts = append(hosts, h)
			}
			continue
		}
		host, port := splitHostPort(line)
		host = normalizeHostname(host)
		if host == "" {
			continue
		}
		// 纯文本行必须是域名或 IP，避免把 "naabu v2.1" 之类的横幅当主机。
		if net.ParseIP(host) == nil && !strings.Contains(host, ".") {
			continue
		}
		hosts = append(hosts, Host{Host: host, Port: normalizePort(port)})
	}
	return finalize(toolNaabu, hosts)
}

// parseNaabuJSONLine 解析一行 naabu JSON。
func parseNaabuJSONLine(line string) (Host, bool) {
	var rec naabuJSON
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		return Host{}, false
	}
	ip := normalizeIP(rec.IP)
	host := normalizeHostname(rec.Host)
	if host == "" {
		host = ip
	}
	if host == "" {
		return Host{}, false
	}
	return Host{Host: host, IP: ip, Port: normalizePort(int(rec.Port))}, true
}
