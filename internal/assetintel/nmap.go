package assetintel

import (
	"encoding/xml"
	"regexp"
	"strings"
)

// ---- nmap XML (-oX) 结构 ----

type nmapXMLRun struct {
	Hosts []nmapXMLHost `xml:"host"`
}

type nmapXMLHost struct {
	Addresses []nmapXMLAddress  `xml:"address"`
	Hostnames []nmapXMLHostname `xml:"hostnames>hostname"`
	Ports     []nmapXMLPort     `xml:"ports>port"`
}

type nmapXMLAddress struct {
	Addr     string `xml:"addr,attr"`
	AddrType string `xml:"addrtype,attr"`
}

type nmapXMLHostname struct {
	Name string `xml:"name,attr"`
	Type string `xml:"type,attr"`
}

type nmapXMLPort struct {
	Protocol string          `xml:"protocol,attr"`
	PortID   int             `xml:"portid,attr"`
	State    nmapXMLState    `xml:"state"`
	Service  nmapXMLService  `xml:"service"`
	Scripts  []nmapXMLScript `xml:"script"`
}

type nmapXMLState struct {
	State string `xml:"state,attr"`
}

type nmapXMLService struct {
	Name    string `xml:"name,attr"`
	Product string `xml:"product,attr"`
	Version string `xml:"version,attr"`
	OSType  string `xml:"ostype,attr"`
}

type nmapXMLScript struct {
	ID     string `xml:"id,attr"`
	Output string `xml:"output,attr"`
}

// parseNmap 解析 nmap 输出，XML 优先，失败时回退纯文本表格。
func parseNmap(output string) []Host {
	if strings.Contains(output, "<nmaprun") {
		if hosts, ok := parseNmapXML(output); ok {
			return finalize(toolNmap, hosts)
		}
	}
	return finalize(toolNmap, parseNmapText(output))
}

// parseNmapXML 解析 -oX 输出。ok=false 表示 XML 不可用（调用方回退文本解析）。
func parseNmapXML(output string) ([]Host, bool) {
	start := strings.Index(output, "<nmaprun")
	if start < 0 {
		return nil, false
	}
	data := output[start:]
	if end := strings.Index(data, "</nmaprun>"); end >= 0 {
		data = data[:end+len("</nmaprun>")]
	}
	var run nmapXMLRun
	if err := xml.Unmarshal([]byte(data), &run); err != nil {
		return nil, false
	}

	var hosts []Host
	for _, nh := range run.Hosts {
		ipv4, ipv6 := "", ""
		for _, addr := range nh.Addresses {
			switch strings.ToLower(strings.TrimSpace(addr.AddrType)) {
			case "ipv4":
				if ipv4 == "" {
					ipv4 = normalizeIP(addr.Addr)
				}
			case "ipv6":
				if ipv6 == "" {
					ipv6 = normalizeIP(addr.Addr)
				}
			}
		}
		ip := ipv4
		if ip == "" {
			ip = ipv6
		}

		hostname := ""
		for _, hn := range nh.Hostnames {
			if strings.EqualFold(hn.Type, "user") && hn.Name != "" {
				hostname = hn.Name
				break
			}
		}
		if hostname == "" {
			for _, hn := range nh.Hostnames {
				if strings.EqualFold(hn.Type, "PTR") && hn.Name != "" {
					hostname = hn.Name
					break
				}
			}
		}
		if hostname == "" && len(nh.Hostnames) > 0 {
			hostname = nh.Hostnames[0].Name
		}
		host := normalizeHostname(hostname)
		if host == "" {
			host = ip
		}
		if host == "" {
			continue
		}

		for _, port := range nh.Ports {
			if !strings.EqualFold(strings.TrimSpace(port.State.State), "open") {
				continue
			}
			portID := normalizePort(port.PortID)
			if portID == 0 {
				continue
			}
			service := normalizeServiceName(port.Service.Name)
			h := Host{
				Host:     host,
				IP:       ip,
				Port:     portID,
				Protocol: nmapProtocol(service, portID, port.Protocol),
				Service:  service,
			}
			if product := strings.TrimSpace(port.Service.Product); product != "" {
				h.Server = product
				h.Technologies = addTechnology(h.Technologies, Technology{
					Name:    product,
					Version: normalizeVersion(port.Service.Version),
				})
			}
			if os := strings.TrimSpace(port.Service.OSType); os != "" {
				if tech, ok := parseTechnology(os); ok {
					h.Technologies = addTechnology(h.Technologies, tech)
				}
			}
			// http-title 脚本可作为页面标题。
			for _, script := range port.Scripts {
				if strings.EqualFold(strings.TrimSpace(script.ID), "http-title") && h.Title == "" {
					h.Title = firstLine(script.Output)
				}
			}
			hosts = append(hosts, h)
		}
	}
	return hosts, true
}

// nmapTextReportRe 匹配 "Nmap scan report for <host> [(ip)]"。
var nmapTextReportRe = regexp.MustCompile(`^Nmap scan report for\s+(.+?)\s*$`)

// nmapTextPortRe 匹配 "80/tcp  open  http  nginx 1.18.0"。
var nmapTextPortRe = regexp.MustCompile(`^(\d{1,5})/(tcp|udp)\s+(\S+)(?:\s+(\S+))?(?:\s+(.*))?$`)

// parseNmapText 解析 nmap 纯文本表格，容错跳过日志与提示行。
func parseNmapText(output string) []Host {
	var hosts []Host
	curHost, curIP := "", ""
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(stripANSI(line))
		if line == "" {
			continue
		}
		if m := nmapTextReportRe.FindStringSubmatch(line); m != nil {
			curHost, curIP = parseNmapReportTarget(m[1])
			continue
		}
		m := nmapTextPortRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if !strings.EqualFold(m[3], "open") { // 只要 open
			continue
		}
		port := parsePortToken(m[1])
		if port == 0 {
			continue
		}
		host := curHost
		if host == "" {
			host = curIP
		}
		if host == "" {
			continue
		}
		service := normalizeServiceName(m[4])
		product, version := splitNmapVersion(m[5])
		h := Host{
			Host:     host,
			IP:       curIP,
			Port:     port,
			Protocol: nmapProtocol(service, port, m[2]),
			Service:  service,
			Server:   product,
		}
		if product != "" {
			h.Technologies = addTechnology(h.Technologies, Technology{Name: product, Version: version})
		}
		hosts = append(hosts, h)
	}
	return hosts
}

// parseNmapReportTarget 解析扫描目标行，兼容 "host (ip)" 与仅 "ip"/"host" 两种形式。
func parseNmapReportTarget(raw string) (host, ip string) {
	raw = strings.TrimSpace(raw)
	if i := strings.LastIndex(raw, " ("); i > 0 && strings.HasSuffix(raw, ")") {
		name := strings.TrimSpace(raw[:i])
		ip = normalizeIP(raw[i+2 : len(raw)-1])
		host = normalizeHostname(name)
		if host == "" {
			host = ip
		}
		return host, ip
	}
	if parsed := normalizeIP(raw); parsed != "" {
		return parsed, parsed
	}
	host = normalizeHostname(raw)
	return host, ""
}

// splitNmapVersion 从 "nginx 1.18.0" 这类版本列拆出产品与版本。
func splitNmapVersion(rest string) (product, version string) {
	fields := strings.Fields(strings.TrimSpace(rest))
	if len(fields) == 0 {
		return "", ""
	}
	product = strings.TrimSpace(fields[0])
	// 去掉 "(Unknown)" 之类的占位符。
	if strings.HasPrefix(product, "(") {
		product = ""
	}
	if len(fields) > 1 && looksVersion(fields[1]) {
		version = normalizeVersion(fields[1])
	}
	return product, version
}

// normalizeServiceName 统一 nmap service 名。
func normalizeServiceName(raw string) string {
	s := strings.ToLower(strings.TrimSpace(stripANSI(raw)))
	switch s {
	case "", "unknown", "tcpwrapped":
		return ""
	default:
		return s
	}
}

// nmapProtocol 依据 service 名、端口、传输层推断协议。
func nmapProtocol(service string, port int, transport string) string {
	if service != "" {
		// 兼容文本输出里的 "ssl/http" 写法。
		if strings.Contains(service, "/") {
			parts := strings.Split(service, "/")
			last := strings.TrimSpace(parts[len(parts)-1])
			if last == "http" && strings.Contains(service, "ssl") {
				return "https"
			}
			if last != "" {
				return last
			}
		}
		return service
	}
	if guessed := guessProtocol(port); guessed != "" {
		return guessed
	}
	return strings.ToLower(strings.TrimSpace(transport))
}
