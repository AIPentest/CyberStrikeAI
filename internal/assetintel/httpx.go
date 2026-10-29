package assetintel

import (
	"encoding/json"
	"strconv"
	"strings"
)

// httpxJSON 对应 httpx -json 的单行 JSON 对象，只取本包用到的字段。
type httpxJSON struct {
	URL       string   `json:"url"`
	Input     string   `json:"input"`
	Host      string   `json:"host"`
	Port      flexInt  `json:"port"`
	Scheme    string   `json:"scheme"`
	Title     string   `json:"title"`
	Webserver string   `json:"webserver"`
	Tech      []string `json:"tech"`
	IP        string   `json:"ip"`
}

// flexInt 兼容 port 为数字或字符串（部分版本/管道会写成 "443"）。
type flexInt int

// UnmarshalJSON 容错解析数字、数字字符串、null；无法解析时置 0 而不报错。
func (f *flexInt) UnmarshalJSON(data []byte) error {
	s := strings.TrimSpace(string(data))
	s = strings.Trim(s, `"`)
	if s == "" || strings.EqualFold(s, "null") {
		*f = 0
		return nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		*f = 0
		return nil
	}
	*f = flexInt(n)
	return nil
}

// parseHttpx 解析 httpx 输出，自动识别 JSON 行与纯文本行。
func parseHttpx(output string) []Host {
	var hosts []Host
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(stripANSI(line))
		if line == "" || isLogLine(line) || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "{") {
			if h, ok := parseHttpxJSONLine(line); ok {
				hosts = append(hosts, h)
			}
			// JSON 行解析失败直接跳过，避免把 {"url": 当主机名。
			continue
		}
		if h, ok := parseHttpxTextLine(line); ok {
			hosts = append(hosts, h)
		}
	}
	return finalize(toolHttpx, hosts)
}

// parseHttpxJSONLine 解析一行 httpx JSON。
func parseHttpxJSONLine(line string) (Host, bool) {
	var rec httpxJSON
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		return Host{}, false
	}

	scheme, host, port := "", "", 0
	if rec.URL != "" {
		scheme, host, port = parseURLParts(rec.URL)
	}
	if host == "" && rec.Input != "" {
		inputScheme, inputHost, inputPort := parseURLParts(rec.Input)
		if inputHost != "" {
			host = inputHost
			if scheme == "" {
				scheme = inputScheme
			}
			if port == 0 {
				port = inputPort
			}
		}
	}
	if host == "" {
		host = normalizeHostname(rec.Host)
	}
	if host == "" {
		return Host{}, false
	}
	if port == 0 && int(rec.Port) > 0 {
		port = int(rec.Port)
	}
	if scheme == "" {
		scheme = normalizeProtocol(rec.Scheme)
	}

	h := Host{
		Host:     host,
		IP:       normalizeIP(rec.IP),
		Port:     normalizePort(port),
		Protocol: scheme,
		Title:    firstLine(rec.Title),
	}
	// webserver 形如 "nginx" 或 "nginx/1.18.0 (Ubuntu)"，拆出版本进指纹。
	if server, version := splitProduct(rec.Webserver); server != "" {
		h.Server = server
		if version != "" {
			h.Technologies = addTechnology(h.Technologies, Technology{Name: server, Version: version})
		}
	}
	for _, rawTech := range rec.Tech {
		if tech, ok := parseTechnology(rawTech); ok {
			h.Technologies = addTechnology(h.Technologies, tech)
		}
	}
	return h, true
}

// parseHttpxTextLine 解析一行 httpx 纯文本，如：
//
//	https://a.example.com [200] [Some Title] [nginx] [PHP:7.4]
//	a.example.com:443 [200] [Some Title]
//	https://x.example.com [nginx]
//
// 各字段都靠 [ ] 分组，按出现顺序宽松识别：状态码 -> 标题 -> Server -> 指纹。
func parseHttpxTextLine(line string) (Host, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return Host{}, false
	}
	firstBracket := strings.IndexByte(line, '[')
	token := line
	var groups []string
	if firstBracket >= 0 {
		token = strings.TrimSpace(line[:firstBracket])
		groups = bracketGroups(line[firstBracket:])
	} else {
		// 无分组时可能带空格（日志/横幅），交由主机名校验过滤。
		if strings.ContainsAny(token, " \t") {
			return Host{}, false
		}
	}

	scheme, host, port := parseURLParts(token)
	if host == "" {
		return Host{}, false
	}

	h := Host{Host: host, Port: normalizePort(port), Protocol: scheme}

	// 第一组若为 3 位状态码则忽略；其余按 标题 -> webserver -> 指纹 识别。
	var rest []string
	for _, g := range groups {
		if isStatusCode(g) {
			continue
		}
		rest = append(rest, g)
	}
	serverSet := false
	if len(rest) > 0 && looksLikeServer(rest[0]) {
		h.Server = rest[0]
		serverSet = true
		rest = rest[1:]
	}
	if len(rest) > 0 {
		h.Title = firstLine(rest[0])
		rest = rest[1:]
	}
	if len(rest) > 0 && !serverSet {
		h.Server = rest[0]
		rest = rest[1:]
	}
	for _, group := range rest {
		for _, item := range strings.Split(group, ",") {
			if tech, ok := parseTechnology(item); ok {
				h.Technologies = addTechnology(h.Technologies, tech)
			}
		}
	}
	// Group 里若出现类似 "nginx" 的 Server，也拆版本。
	if name, version := splitProduct(h.Server); name != "" {
		h.Server = name
		if version != "" {
			h.Technologies = addTechnology(h.Technologies, Technology{Name: name, Version: version})
		}
	}
	return h, true
}

// bracketGroups 提取字符串中所有 [xxx] 的内容（忽略未闭合的尾部）。
func bracketGroups(s string) []string {
	var groups []string
	for {
		start := strings.IndexByte(s, '[')
		if start < 0 {
			return groups
		}
		end := strings.IndexByte(s[start:], ']')
		if end < 0 {
			return groups
		}
		groups = append(groups, strings.TrimSpace(s[start+1:start+end]))
		s = s[start+end+1:]
	}
}

// isStatusCode 判断是否为 HTTP 状态码。
func isStatusCode(s string) bool {
	if len(s) != 3 {
		return false
	}
	n, err := strconv.Atoi(s)
	return err == nil && n >= 100 && n <= 599
}

// looksLikeServer 判断单独一组文本更像 Server 头而不是标题。
func looksLikeServer(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" || strings.ContainsAny(s, " \t") {
		return false
	}
	known := []string{
		"nginx", "apache", "openresty", "cloudflare", "envoy", "caddy",
		"iis", "microsoft-iis", "tengine", "tomcat", "jetty", "gunicorn",
		"uvicorn", "lighttpd", "traefik", "haproxy", "varnish", "squid",
	}
	for _, k := range known {
		if strings.Contains(s, k) {
			return true
		}
	}
	return false
}
