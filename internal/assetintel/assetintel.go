// Package assetintel 将第三方信息收集工具（子域名、端口扫描、HTTP 探测、
// URL 收集、WAF 识别等）的原始输出清洗成结构化的主机记录。
//
// 本包刻意保持“纯函数”：只依赖标准库与 golang.org/x/net/publicsuffix，
// 不访问数据库、网络或进程全局状态，可独立编译、独立单测。
package assetintel

import (
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/publicsuffix"
)

// Technology 一项技术指纹。
type Technology struct {
	Name    string // 如 "nginx"、"PHP"、"jQuery"
	Version string // 如 "1.22.0"，没有则空
}

// Host 从工具输出中清洗出的一条主机/服务记录。
type Host struct {
	Host         string       // 原始主机名或 IP（小写、去协议去端口）
	IP           string       // 纯 IP，未知为空
	Port         int          // 端口，未知为 0
	Domain       string       // 根域名（eTLD+1，如 a.b.example.com -> example.com），未知为空
	Protocol     string       // http/https/ssh/tcp 等，未知为空
	Title        string       // HTTP 页面标题
	Server       string       // HTTP Server 头（如 nginx）
	Service      string       // 服务名（nmap -sV 的 service，如 ssh/http/mysql）
	Technologies []Technology // 技术指纹栈
	Source       string       // 工具名，如 "httpx"
}

// 规范化的工具名（Source 统一使用这一份）。
const (
	toolSubfinder   = "subfinder"
	toolHttpx       = "httpx"
	toolNaabu       = "naabu"
	toolNmap        = "nmap"
	toolFscan       = "fscan"
	toolMasscan     = "masscan"
	toolRustscan    = "rustscan"
	toolAmass       = "amass"
	toolWafw00f     = "wafw00f"
	toolKatana      = "katana"
	toolGau         = "gau"
	toolWaybackurls = "waybackurls"
)

// Supports 判断该工具的输出是否由本包处理。
// 大小写不敏感，允许传入可执行文件路径（如 /usr/local/bin/httpx、nmap.exe）。
func Supports(tool string) bool {
	return normalizeTool(tool) != ""
}

// Parse 解析一次工具执行的输出。无法识别时返回 (nil, nil)，不返回错误。
//
// 单个工具的输出即使夹杂日志、提示或畸形行，也只跳过该行，不会整体失败。
func Parse(tool string, args map[string]interface{}, output string) ([]Host, error) {
	_ = args // 目前各工具输出格式可由内容自识别，无需额外参数
	name := normalizeTool(tool)
	switch name {
	case toolSubfinder:
		return parseSubfinder(output), nil
	case toolHttpx:
		return parseHttpx(output), nil
	case toolNaabu:
		return parseNaabu(output), nil
	case toolNmap:
		return parseNmap(output), nil
	case toolFscan:
		return parseFscan(output), nil
	case toolMasscan:
		return parseMasscan(output), nil
	case toolRustscan:
		return parseRustscan(output), nil
	case toolAmass:
		return parseAmass(output), nil
	case toolWafw00f:
		return parseWafw00f(output), nil
	case toolKatana, toolGau, toolWaybackurls:
		return parseURLList(name, output), nil
	default:
		return nil, nil
	}
}

// normalizeTool 归一化工具名，无法识别返回空串。
func normalizeTool(tool string) string {
	t := strings.ToLower(strings.TrimSpace(stripANSI(tool)))
	if t == "" {
		return ""
	}
	// 去掉路径（/ 或 \）与 Windows 的 .exe 后缀。
	if i := strings.LastIndexAny(t, `/\`); i >= 0 {
		t = t[i+1:]
	}
	t = strings.TrimSpace(strings.TrimSuffix(t, ".exe"))
	t = strings.TrimSuffix(t, ".bat")
	switch t {
	case toolSubfinder, toolHttpx, toolNaabu, toolNmap,
		toolFscan, toolMasscan, toolRustscan, toolAmass,
		toolWafw00f, toolKatana, toolGau, toolWaybackurls:
		return t
	default:
		return ""
	}
}

// finalize 对一批 Host 做统一清洗、去重与来源标注。
func finalize(tool string, hosts []Host) []Host {
	src := normalizeTool(tool)
	out := make([]Host, 0, len(hosts))
	index := make(map[string]int, len(hosts))
	for _, h := range hosts {
		h.Source = src
		h = cleanHost(h)
		if h.Host == "" {
			continue
		}
		key := dedupKey(h)
		if i, ok := index[key]; ok {
			out[i] = mergeHost(out[i], h)
			continue
		}
		index[key] = len(out)
		out = append(out, h)
	}
	// 没有解析到任何主机时返回 nil，方便调用方区分“没结果”和“空切片”。
	if len(out) == 0 {
		return nil
	}
	return out
}

// dedupKey 同一批结果按 (host, port, protocol) 去重。
func dedupKey(h Host) string {
	return strings.ToLower(h.Host) + "|" + strconv.Itoa(h.Port) + "|" + strings.ToLower(h.Protocol)
}

// cleanHost 补齐/纠正一条记录的字段：主机名、IP、端口、协议、域名、指纹去重。
func cleanHost(h Host) Host {
	host := normalizeHostname(h.Host)
	if host == "" {
		host = normalizeHostname(h.IP)
	}
	if host == "" {
		return Host{}
	}
	h.Host = host
	h.IP = normalizeIP(h.IP)
	if h.IP == "" {
		h.IP = normalizeIP(host)
	}
	h.Port = normalizePort(h.Port)
	h.Protocol = normalizeProtocol(h.Protocol)
	if h.Protocol == "" {
		h.Protocol = guessProtocol(h.Port)
	}
	// URL 未写端口时按 scheme 补默认端口，避免资产层级出现“端口 0”节点。
	if h.Port == 0 {
		h.Port = defaultPortForScheme(h.Protocol)
	}
	if h.Domain == "" {
		h.Domain = domainOf(host)
	}
	h.Title = firstLine(h.Title)
	h.Server = strings.TrimSpace(stripANSI(h.Server))
	h.Service = strings.ToLower(strings.TrimSpace(stripANSI(h.Service)))
	if h.Service == "unknown" || h.Service == "tcpwrapped" {
		h.Service = ""
	}
	h.Technologies = normalizeTechnologies(h.Technologies)
	h = applyServerTechnology(h)
	return h
}

// mergeHost 合并两条同键记录，保留信息更全的那条并补齐缺失字段。
func mergeHost(a, b Host) Host {
	if richness(b) > richness(a) {
		a, b = b, a
	}
	if a.IP == "" {
		a.IP = b.IP
	}
	if a.Port == 0 {
		a.Port = b.Port
	}
	if a.Domain == "" {
		a.Domain = b.Domain
	}
	if a.Protocol == "" {
		a.Protocol = b.Protocol
	}
	if a.Title == "" {
		a.Title = b.Title
	}
	if a.Server == "" {
		a.Server = b.Server
	}
	if a.Service == "" {
		a.Service = b.Service
	}
	if a.Host == "" {
		a.Host = b.Host
	}
	for _, t := range b.Technologies {
		a.Technologies = addTechnology(a.Technologies, t)
	}
	return applyServerTechnology(a)
}

// richness 粗略衡量一条记录的信息量，用于去重时决定保留哪条。
func richness(h Host) int {
	n := 0
	if h.IP != "" {
		n += 2
	}
	if h.Port > 0 {
		n++
	}
	if h.Domain != "" {
		n++
	}
	if h.Protocol != "" {
		n++
	}
	if h.Title != "" {
		n += 2
	}
	if h.Server != "" {
		n += 2
	}
	if h.Service != "" {
		n += 2
	}
	n += len(h.Technologies)
	return n
}

// normalizeHostname 统一主机名：小写、去空白、去协议/端口/路径、去结尾点、去 IPv6 方括号。
// 非法主机名返回空串。
func normalizeHostname(raw string) string {
	s := strings.TrimSpace(stripANSI(raw))
	if s == "" {
		return ""
	}
	// URL 形态优先按 URL 解析。
	if strings.Contains(s, "://") {
		_, host, _ := parseURLParts(s)
		return host
	}
	if strings.HasPrefix(s, "//") {
		_, host, _ := parseURLParts("http:" + s)
		return host
	}
	// 去 userinfo（user@host）。
	if i := strings.LastIndexByte(s, '@'); i >= 0 {
		s = s[i+1:]
	}
	// 去路径 / 查询串。
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	// 去端口：IPv6 先处理方括号，普通主机名交给 SplitHostPort。
	if strings.HasPrefix(s, "[") {
		if end := strings.IndexByte(s, ']'); end > 0 {
			s = s[1:end]
		}
	} else if net.ParseIP(s) == nil {
		if host, _, err := net.SplitHostPort(s); err == nil {
			s = host
		}
	}
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimSuffix(s, ".")
	if !validHostname(s) {
		return ""
	}
	return s
}

// validHostname 校验主机名（IP 直接放行）。
func validHostname(s string) bool {
	if s == "" || len(s) > 253 {
		return false
	}
	if net.ParseIP(s) != nil {
		return true
	}
	for _, label := range strings.Split(s, ".") {
		if label == "" || len(label) > 63 {
			return false
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
				continue
			}
			return false
		}
	}
	return true
}

// normalizeIP 校验并规范化 IP，非法返回空串。
func normalizeIP(raw string) string {
	s := strings.TrimSpace(stripANSI(raw))
	s = strings.TrimSuffix(strings.TrimPrefix(s, "["), "]")
	ip := net.ParseIP(s)
	if ip == nil {
		return ""
	}
	return ip.String()
}

// normalizePort 把端口收敛到 1-65535，非法返回 0。
func normalizePort(port int) int {
	if port < 1 || port > 65535 {
		return 0
	}
	return port
}

// parsePortToken 解析端口文本，非法返回 0。
func parsePortToken(raw string) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0
	}
	return normalizePort(n)
}

// normalizeProtocol 统一协议写法。
func normalizeProtocol(protocol string) string {
	p := strings.ToLower(strings.TrimSpace(stripANSI(protocol)))
	p = strings.TrimSuffix(p, "://")
	switch p {
	case "", "unknown":
		return ""
	default:
		return p
	}
}

// guessProtocol 按端口猜协议。
func guessProtocol(port int) string {
	switch port {
	case 80, 8080, 8000, 8888:
		return "http"
	case 443, 8443:
		return "https"
	case 22:
		return "ssh"
	case 21:
		return "ftp"
	case 3306:
		return "mysql"
	case 6379:
		return "redis"
	case 5432:
		return "postgres"
	case 27017:
		return "mongodb"
	default:
		return ""
	}
}

// defaultPortForScheme 返回 scheme 的默认端口；未知 scheme 返回 0（保持“端口未知”）。
func defaultPortForScheme(protocol string) int {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "https":
		return 443
	case "http":
		return 80
	case "ftp":
		return 21
	default:
		return 0
	}
}

// parsePlainHostLine 从“每行一个主机名/URL/IP”的输出中提取主机；
// 跳过空行、日志、注释与不像主机的行（子域名至少含一个点）。
func parsePlainHostLine(line string) string {
	line = strings.TrimSpace(stripANSI(line))
	if line == "" || isLogLine(line) {
		return ""
	}
	if strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") || strings.HasPrefix(line, ";") {
		return ""
	}
	// 通配符结果只保留主机部分。
	line = strings.TrimPrefix(line, "*.")
	host := normalizeHostname(line)
	if host == "" {
		return ""
	}
	if net.ParseIP(host) == nil && !strings.Contains(host, ".") {
		return ""
	}
	return host
}

// domainOf 计算 eTLD+1；IP、内网名或计算失败返回空串。
func domainOf(host string) string {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "" || net.ParseIP(host) != nil {
		return ""
	}
	domain, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil {
		return ""
	}
	return strings.TrimSuffix(strings.ToLower(domain), ".")
}

// normalizeTechnologies 清洗指纹列表：去空、规范化版本、同名同版本去重。
func normalizeTechnologies(in []Technology) []Technology {
	var out []Technology
	for _, t := range in {
		name := strings.TrimSpace(stripANSI(t.Name))
		if name == "" {
			continue
		}
		version := normalizeVersion(t.Version)
		if version != "" && !looksVersion(version) {
			version = ""
		}
		out = addTechnology(out, Technology{Name: name, Version: version})
	}
	return out
}

// addTechnology 同名同版本去重后追加。
func addTechnology(list []Technology, t Technology) []Technology {
	if strings.TrimSpace(t.Name) == "" {
		return list
	}
	for _, existing := range list {
		if strings.EqualFold(existing.Name, t.Name) && strings.EqualFold(existing.Version, t.Version) {
			return list
		}
	}
	return append(list, t)
}

// applyServerTechnology 让 Server 也出现在 Technologies 里；同名指纹已存在则跳过，
// 避免 httpx 的 webserver 与 tech 重复。
func applyServerTechnology(h Host) Host {
	if h.Server == "" {
		return h
	}
	for _, t := range h.Technologies {
		if strings.EqualFold(t.Name, h.Server) {
			return h
		}
	}
	h.Technologies = append(h.Technologies, Technology{Name: h.Server})
	return h
}

// parseTechnology 解析一条指纹文本，支持 "Nginx:1.18.0" 形式。
func parseTechnology(raw string) (Technology, bool) {
	s := strings.TrimSpace(stripANSI(raw))
	s = strings.Trim(s, " \t,;")
	if s == "" {
		return Technology{}, false
	}
	name, version := s, ""
	if i := strings.Index(s, ":"); i > 0 {
		name = strings.TrimSpace(s[:i])
		if v := strings.TrimSpace(s[i+1:]); looksVersion(v) {
			version = normalizeVersion(v)
		}
	}
	name = strings.TrimSpace(name)
	if name == "" || strings.EqualFold(name, "unknown") {
		return Technology{}, false
	}
	return Technology{Name: name, Version: version}, true
}

// splitProduct 将 "nginx/1.18.0 (Ubuntu)" 这类 Server 头拆成名称与版本。
func splitProduct(raw string) (name, version string) {
	s := strings.TrimSpace(stripANSI(raw))
	if s == "" {
		return "", ""
	}
	name = s
	if i := strings.Index(s, "/"); i > 0 {
		name = strings.TrimSpace(s[:i])
		rest := strings.TrimSpace(s[i+1:])
		if j := strings.IndexAny(rest, " \t("); j >= 0 {
			rest = rest[:j]
		}
		if looksVersion(rest) {
			version = normalizeVersion(rest)
		}
	} else if fields := strings.Fields(s); len(fields) > 1 && looksVersion(fields[1]) {
		name = fields[0]
		version = normalizeVersion(fields[1])
	}
	return strings.TrimSpace(name), version
}

// looksVersion 判断文本是否像版本号（可带前缀 v）。
func looksVersion(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	if s[0] == 'v' || s[0] == 'V' {
		s = s[1:]
	}
	if s == "" {
		return false
	}
	return s[0] >= '0' && s[0] <= '9'
}

// normalizeVersion 规范化版本号文本。
func normalizeVersion(s string) string {
	s = strings.TrimSpace(stripANSI(s))
	s = strings.Trim(s, "[]()")
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if (s[0] == 'v' || s[0] == 'V') && len(s) > 1 && s[1] >= '0' && s[1] <= '9' {
		s = s[1:]
	}
	return s
}

// splitHostPort 从 "host:port" / "[v6]:port" / "host" 中拆出主机与端口。
func splitHostPort(raw string) (host string, port int) {
	s := strings.TrimSpace(stripANSI(raw))
	if s == "" {
		return "", 0
	}
	// 去掉 scheme 与路径，只保留 authority。
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	s = strings.TrimPrefix(s, "//")
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	if strings.HasPrefix(s, "[") {
		if end := strings.IndexByte(s, ']'); end > 0 {
			host = s[1:end]
			if rest := s[end+1:]; strings.HasPrefix(rest, ":") {
				port = parsePortToken(rest[1:])
			}
			return host, port
		}
	}
	if h, p, err := net.SplitHostPort(s); err == nil {
		return h, parsePortToken(p)
	}
	return s, 0
}

// parseURLParts 解析 URL / host:port，返回 scheme、主机名、端口。
func parseURLParts(raw string) (scheme, host string, port int) {
	s := strings.TrimSpace(stripANSI(raw))
	if s == "" {
		return "", "", 0
	}
	if !strings.Contains(s, "://") && !strings.HasPrefix(s, "//") {
		h, p := splitHostPort(s)
		return "", normalizeHostname(h), p
	}
	u, err := url.Parse(s)
	if err != nil || u.Hostname() == "" {
		h, p := splitHostPort(s)
		return "", normalizeHostname(h), p
	}
	return strings.ToLower(strings.TrimSpace(u.Scheme)), normalizeHostname(u.Hostname()), parsePortToken(u.Port())
}

// firstLine 取首行并去空白，用于标题、脚本文本等。
func firstLine(s string) string {
	s = strings.TrimSpace(stripANSI(s))
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// isLogLine 判断是否工具日志行，如 "[INF] ..."。
func isLogLine(line string) bool {
	l := strings.ToUpper(strings.TrimSpace(line))
	if !strings.HasPrefix(l, "[") {
		return false
	}
	tags := []string{
		"[INF]", "[WRN]", "[ERR]", "[DBG]", "[FTL]", "[TRC]", "[SUC]",
		"[VRB]", "[VER]", "[INFO]", "[WARN]", "[ERROR]", "[DEBUG]", "[FATAL]",
	}
	for _, tag := range tags {
		if strings.HasPrefix(l, tag) {
			return true
		}
	}
	return false
}

// ansiRe 匹配 ANSI 转义序列（工具输出偶有着色）。
var ansiRe = regexp.MustCompile("\x1b\\[[0-9;?]*[ -/]*[@-~]")

// stripANSI 去掉 ANSI 转义序列。
func stripANSI(s string) string {
	if strings.IndexByte(s, 0x1b) < 0 {
		return s
	}
	return ansiRe.ReplaceAllString(s, "")
}
