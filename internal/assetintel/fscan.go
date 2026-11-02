package assetintel

import (
	"net"
	"regexp"
	"strings"
)

// fscan 输出按行混杂日志、端口结果、Web 结果与漏洞命中：
//
//	[*] 1.2.3.4:445 open ms17-010
//	[+] http://1.2.3.4 200 后台
//	[+] 1.2.3.4 MS17-010
var (
	// fscanPortRe 匹配去掉前缀后的 "host:port state banner"。
	fscanPortRe = regexp.MustCompile(`^(\S+):(\d{1,5})\s+(open|closed|filtered)\b\s*(.*)$`)
	// fscanWebRe 匹配去掉前缀后的 "http(s)://url [status] [title]"。
	fscanWebRe = regexp.MustCompile(`^(https?://\S+)(?:\s+(\d{3})\b)?\s*(.*)$`)
	// fscanVulnRe 匹配 "[+] 1.2.3.4 MS17-010" 这类只有 IP 与漏洞名的命中行。
	fscanVulnRe = regexp.MustCompile(`^(\d{1,3}(?:\.\d{1,3}){3})\s+\S`)
)

// parseFscan 解析 fscan 输出，逐行容错，只取能识别的行。
func parseFscan(output string) []Host {
	var hosts []Host
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(stripANSI(line))
		if line == "" || isLogLine(line) {
			continue
		}
		rest := stripFscanTag(line)
		if rest == "" {
			continue
		}
		// 1) Web 结果行：URL（可选状态码与标题）。
		if strings.HasPrefix(rest, "http://") || strings.HasPrefix(rest, "https://") {
			if h, ok := parseFscanWeb(rest); ok {
				hosts = append(hosts, h)
			}
			continue
		}
		// 2) 端口结果行：host:port open [banner]。
		if m := fscanPortRe.FindStringSubmatch(rest); m != nil {
			if !strings.EqualFold(m[3], "open") {
				continue
			}
			host := normalizeHostname(m[1])
			port := normalizePort(parsePortToken(m[2]))
			if host == "" || port == 0 {
				continue
			}
			hosts = append(hosts, Host{
				Host:    host,
				Port:    port,
				Service: fscanService(m[4], port),
			})
			continue
		}
		// 3) 漏洞命中行：只取主机，忽略漏洞名等噪音。
		if m := fscanVulnRe.FindStringSubmatch(rest); m != nil && net.ParseIP(m[1]) != nil {
			hosts = append(hosts, Host{Host: m[1]})
		}
	}
	return finalize(toolFscan, hosts)
}

// stripFscanTag 去掉 "[*]"/"[+]"/"[-]" 前缀，返回剩余内容。
func stripFscanTag(line string) string {
	if !strings.HasPrefix(line, "[") {
		return strings.TrimSpace(line)
	}
	end := strings.IndexByte(line, ']')
	if end <= 0 {
		return strings.TrimSpace(line)
	}
	return strings.TrimSpace(line[end+1:])
}

// parseFscanWeb 解析 fscan 的 Web 结果行。
func parseFscanWeb(rest string) (Host, bool) {
	m := fscanWebRe.FindStringSubmatch(rest)
	if m == nil {
		return Host{}, false
	}
	scheme, host, port := parseURLParts(m[1])
	if host == "" {
		return Host{}, false
	}
	h := Host{Host: host, Port: normalizePort(port), Protocol: scheme}
	if title := strings.Trim(strings.TrimSpace(m[3]), "[]"); title != "" {
		h.Title = title
	}
	return h, true
}

// fscanService 从 banner 关键字或端口推断服务名。
func fscanService(banner string, port int) string {
	lower := strings.ToLower(strings.TrimSpace(banner))
	for _, keyword := range fscanServiceKeywords {
		if strings.Contains(lower, keyword) {
			return keyword
		}
	}
	return serviceByPort(port)
}

// fscanServiceKeywords banner 中可识别的服务关键字（长词在前，避免 "https" 命中 "http"）。
var fscanServiceKeywords = []string{
	"https", "http", "ssh", "smb", "mssql", "mysql", "redis", "memcached",
	"mongodb", "postgresql", "postgres", "oracle", "rdp", "vnc", "ftp",
	"telnet", "ldap", "smtp", "imap", "pop3", "dns", "snmp", "rsync",
	"nfs", "docker", "kafka", "zookeeper", "elasticsearch", "rabbitmq",
	"cassandra", "ipmi", "weblogic", "tomcat", "nginx", "apache", "socks",
	"netbios", "mqtt",
}

// fscanPortService 常见端口到服务名的映射。
var fscanPortService = map[int]string{
	21: "ftp", 22: "ssh", 23: "telnet", 25: "smtp", 53: "dns",
	80: "http", 81: "http", 110: "pop3", 111: "rpcbind", 135: "msrpc",
	139: "netbios-ssn", 143: "imap", 161: "snmp", 389: "ldap",
	443: "https", 445: "smb", 465: "smtps", 512: "exec", 513: "login",
	514: "shell", 554: "rtsp", 587: "smtp", 623: "ipmi", 636: "ldaps",
	873: "rsync", 993: "imaps", 995: "pop3s", 1080: "socks", 1099: "rmi",
	1433: "mssql", 1521: "oracle", 1723: "pptp", 1883: "mqtt",
	2049: "nfs", 2181: "zookeeper", 2375: "docker", 2376: "docker",
	2379: "etcd", 2380: "etcd", 3000: "http", 3128: "squid",
	3268: "ldap", 3306: "mysql", 3389: "rdp", 4369: "epmd",
	5000: "http", 5432: "postgres", 5672: "amqp", 5900: "vnc",
	5984: "couchdb", 6379: "redis", 6443: "kubernetes", 7001: "weblogic",
	8000: "http", 8009: "ajp", 8080: "http", 8081: "http",
	8086: "influxdb", 8088: "http", 8090: "http", 8161: "activemq",
	8180: "http", 8443: "https", 8500: "consul", 8834: "nessus",
	8848: "nacos", 8880: "http", 8888: "http", 9000: "http",
	9001: "http", 9042: "cassandra", 9080: "http", 9090: "http",
	9092: "kafka", 9160: "cassandra", 9200: "elasticsearch",
	9300: "elasticsearch", 9418: "git", 9443: "https", 9999: "http",
	10000: "webmin", 10250: "kubelet", 11211: "memcached",
	15672: "rabbitmq", 27017: "mongodb", 27018: "mongodb",
	50070: "hdfs", 61616: "activemq",
}

// serviceByPort 查表得到端口对应服务名，未收录返回空串。
func serviceByPort(port int) string {
	return fscanPortService[port]
}
