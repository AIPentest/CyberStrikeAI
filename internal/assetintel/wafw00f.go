package assetintel

import (
	"regexp"
	"strings"
)

// wafw00f 典型输出（stdout 混有 ASCII 横幅与统计）：
//
//	[*] Checking https://shop.example.com
//	[+] The site https://shop.example.com is behind Cloudflare (Cloudflare Inc.) WAF.
var (
	wafw00fSiteRe  = regexp.MustCompile(`(?i)\bthe site\s+(\S+)\s+is behind\s+(.+?)\s+waf\b`)
	wafw00fCheckRe = regexp.MustCompile(`(?i)\bchecking\s+(\S+)`)
)

// parseWafw00f 解析 wafw00f 输出，提取被检测主机与 WAF 指纹。
func parseWafw00f(output string) []Host {
	var hosts []Host
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(stripANSI(line))
		if line == "" {
			continue
		}
		// "The site <url> is behind <WAF> WAF."：同时得到主机与 WAF 指纹。
		if m := wafw00fSiteRe.FindStringSubmatch(line); m != nil {
			scheme, host, port := parseURLParts(cleanURLToken(m[1]))
			if host == "" {
				continue
			}
			h := Host{Host: host, Port: normalizePort(port), Protocol: scheme}
			if name := cleanWAFName(m[2]); name != "" {
				h.Technologies = addTechnology(h.Technologies, Technology{Name: name})
			}
			hosts = append(hosts, h)
			continue
		}
		// "Checking <url>"：先记录主机，后续结果行按同键合并补指纹。
		if m := wafw00fCheckRe.FindStringSubmatch(line); m != nil {
			scheme, host, port := parseURLParts(cleanURLToken(m[1]))
			if host == "" {
				continue
			}
			hosts = append(hosts, Host{Host: host, Port: normalizePort(port), Protocol: scheme})
		}
	}
	return finalize(toolWafw00f, hosts)
}

// cleanURLToken 去掉行尾标点，避免把句子标点带进 URL。
func cleanURLToken(raw string) string {
	return strings.TrimRight(strings.TrimSpace(raw), ".,;:)")
}

// cleanWAFName 从 "Cloudflare (Cloudflare Inc.)" 这类文本中取 WAF 名称。
func cleanWAFName(raw string) string {
	s := strings.TrimRight(strings.TrimSpace(raw), ".,;")
	if i := strings.Index(s, " ("); i > 0 {
		s = strings.TrimSpace(s[:i])
	}
	switch strings.ToLower(s) {
	case "", "a", "an", "the", "unknown", "waf":
		return ""
	default:
		return s
	}
}
