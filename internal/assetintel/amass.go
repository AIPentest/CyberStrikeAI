package assetintel

import (
	"encoding/json"
	"strings"
)

// amassJSON 对应 `amass enum -json` 的单行对象，只用 name/domain/addresses。
type amassJSON struct {
	Name      string             `json:"name"`
	Domain    string             `json:"domain"`
	Addresses []amassJSONAddress `json:"addresses"`
}

type amassJSONAddress struct {
	IP string `json:"ip"`
}

// parseAmass 解析 amass 输出：默认每行一个子域名，也兼容 -json 行与日志混排。
func parseAmass(output string) []Host {
	var hosts []Host
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(stripANSI(line))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "{") {
			if h, ok := parseAmassJSONLine(line); ok {
				hosts = append(hosts, h)
			}
			continue
		}
		if host := parsePlainHostLine(line); host != "" {
			hosts = append(hosts, Host{Host: host})
		}
	}
	return finalize(toolAmass, hosts)
}

// parseAmassJSONLine 解析一行 amass JSON。
func parseAmassJSONLine(line string) (Host, bool) {
	var rec amassJSON
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		return Host{}, false
	}
	name := rec.Name
	if strings.TrimSpace(name) == "" {
		name = rec.Domain
	}
	host := parsePlainHostLine(name)
	if host == "" {
		return Host{}, false
	}
	h := Host{Host: host}
	for _, addr := range rec.Addresses {
		if ip := normalizeIP(addr.IP); ip != "" {
			h.IP = ip
			break
		}
	}
	return h, true
}
