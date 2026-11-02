package assetintel

import (
	"reflect"
	"testing"
)

// tech 构造期望的技术指纹，减少表驱动用例的噪音。
func tech(name, version string) Technology {
	return Technology{Name: name, Version: version}
}

// parseCase 一条 Parse 用例。
type parseCase struct {
	name   string
	tool   string
	output string
	want   []Host
}

// runParseCases 执行表驱动 Parse 用例并做全字段断言。
func runParseCases(t *testing.T, cases []parseCase) {
	t.Helper()
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse(tc.tool, nil, tc.output)
			if err != nil {
				t.Fatalf("Parse 返回错误: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("解析结果不符\n got: %#v\nwant: %#v", got, tc.want)
			}
		})
	}
}

func TestSupports(t *testing.T) {
	supported := []string{
		"subfinder", "HTTPX", "naabu", "nmap", " nmap ",
		"/usr/local/bin/httpx", `C:\tools\nmap.exe`, "Subfinder",
		"fscan", "masscan", "rustscan", "amass", "wafw00f",
		"katana", "gau", "waybackurls",
		"/usr/local/bin/fscan", `C:\tools\wafw00f.exe`, "WaybackURLs",
	}
	for _, tool := range supported {
		if !Supports(tool) {
			t.Errorf("Supports(%q) = false，期望 true", tool)
		}
	}
	unsupported := []string{"", "nikto", "nuclei", "subfinder2", "httpx-extra", "sqlmap"}
	for _, tool := range unsupported {
		if Supports(tool) {
			t.Errorf("Supports(%q) = true，期望 false", tool)
		}
	}
}

func TestParseUnknownTool(t *testing.T) {
	hosts, err := Parse("nikto", nil, "1.2.3.4:80")
	if err != nil {
		t.Fatalf("Parse 未知工具不应返回错误: %v", err)
	}
	if hosts != nil {
		t.Fatalf("Parse 未知工具应返回 nil，得到 %#v", hosts)
	}
}

func TestParseToolNameNormalization(t *testing.T) {
	got, err := Parse(`C:\tools\httpx.exe`, nil, "https://a.example.com [200] [T] [nginx]")
	if err != nil {
		t.Fatalf("Parse 返回错误: %v", err)
	}
	want := []Host{{
		Host:         "a.example.com",
		Port:         443,
		Domain:       "example.com",
		Protocol:     "https",
		Title:        "T",
		Server:       "nginx",
		Technologies: []Technology{tech("nginx", "")},
		Source:       "httpx",
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("工具名归一化失败\n got: %#v\nwant: %#v", got, want)
	}
}

func TestParseEmptyOutput(t *testing.T) {
	tools := []string{
		"subfinder", "httpx", "naabu", "nmap",
		"fscan", "masscan", "rustscan", "amass", "wafw00f",
		"katana", "gau", "waybackurls",
	}
	for _, tool := range tools {
		got, err := Parse(tool, nil, "")
		if err != nil {
			t.Fatalf("Parse(%q, 空输出) 返回错误: %v", tool, err)
		}
		if len(got) != 0 {
			t.Fatalf("Parse(%q, 空输出) 期望 0 条，得到 %#v", tool, got)
		}
		if got != nil {
			t.Fatalf("Parse(%q, 空输出) 期望 nil，得到 %#v", tool, got)
		}
	}
}

// ---- subfinder ----

func TestParseSubfinder(t *testing.T) {
	runParseCases(t, []parseCase{
		{
			name:   "普通列表与大小写/结尾点/IP",
			tool:   "subfinder",
			output: "a.example.com\nWWW.Example.COM.\nsub.example.co.uk\n1.2.3.4\n",
			want: []Host{
				{Host: "a.example.com", Domain: "example.com", Source: "subfinder"},
				{Host: "www.example.com", Domain: "example.com", Source: "subfinder"},
				{Host: "sub.example.co.uk", Domain: "example.co.uk", Source: "subfinder"},
				{Host: "1.2.3.4", IP: "1.2.3.4", Source: "subfinder"},
			},
		},
		{
			name: "日志/注释/通配符/URL/ANSI/垃圾行",
			tool: "subfinder",
			output: "[INF] Enumerating subdomains for example.com\n" +
				"[WRN] could not resolve foo\n" +
				"[ERR] timeout\n" +
				"[DBG] debug\n" +
				"# 注释行\n   \n" +
				"*.api.example.com\n" +
				"https://dev.example.com/login?x=1\n" +
				"not a host\n" +
				"-\n" +
				"\x1b[32mcolor.example.com\x1b[0m\n",
			want: []Host{
				{Host: "api.example.com", Domain: "example.com", Source: "subfinder"},
				{Host: "dev.example.com", Domain: "example.com", Source: "subfinder"},
				{Host: "color.example.com", Domain: "example.com", Source: "subfinder"},
			},
		},
		{
			name:   "同主机去重",
			tool:   "subfinder",
			output: "dup.example.com\nDUP.Example.COM.\ndup.example.com\n*.dup.example.com\n",
			want: []Host{
				{Host: "dup.example.com", Domain: "example.com", Source: "subfinder"},
			},
		},
		{
			name:   "全是垃圾行",
			tool:   "subfinder",
			output: "[INF] only logs\n\n--\n",
			want:   nil,
		},
		{
			name:   "空输出",
			tool:   "subfinder",
			output: "",
			want:   nil,
		},
	})
}

// ---- httpx ----

func TestParseHttpx(t *testing.T) {
	runParseCases(t, []parseCase{
		{
			name: "JSON 基本形态（URL+端口+tech 版本+webserver 去重）",
			tool: "httpx",
			output: `{"url":"https://App.Example.com:8443/login?x=1","input":"app.example.com",` +
				`"host":"app.example.com","port":8443,"scheme":"HTTPS","status_code":200,` +
				`"title":"Admin Login","webserver":"nginx/1.18.0 (Ubuntu)",` +
				`"tech":["Nginx:1.18.0","PHP:7.4.3","jQuery"],"cdn_name":"cloudflare","ip":"1.2.3.4"}` + "\n",
			want: []Host{{
				Host:         "app.example.com",
				IP:           "1.2.3.4",
				Port:         8443,
				Domain:       "example.com",
				Protocol:     "https",
				Title:        "Admin Login",
				Server:       "nginx",
				Technologies: []Technology{tech("nginx", "1.18.0"), tech("PHP", "7.4.3"), tech("jQuery", "")},
				Source:       "httpx",
			}},
		},
		{
			name: "JSON 脏数据/畸形行/同键合并/非法 IP",
			tool: "httpx",
			output: "[INF] httpx starting\n" +
				`{"url":"http://dup.example.com","host":"dup.example.com","port":80,"scheme":"http","title":"Dup"}` + "\n" +
				`{"url": broken` + "\n" +
				`{"url":"http://dup.example.com:80","host":"dup.example.com","port":80,"scheme":"http","ip":"5.6.7.8","webserver":"Apache/2.4.41","tech":["PHP:8.1"]}` + "\n" +
				"not a line\n" +
				`{"host":"bad.example.com","ip":"999.1.1.1","port":443,"scheme":"https"}` + "\n",
			want: []Host{
				{
					Host:         "dup.example.com",
					IP:           "5.6.7.8",
					Port:         80,
					Domain:       "example.com",
					Protocol:     "http",
					Title:        "Dup",
					Server:       "Apache",
					Technologies: []Technology{tech("Apache", "2.4.41"), tech("PHP", "8.1")},
					Source:       "httpx",
				},
				{
					Host:     "bad.example.com",
					Port:     443,
					Domain:   "example.com",
					Protocol: "https",
					Source:   "httpx",
				},
			},
		},
		{
			name: "JSON IPv6",
			tool: "httpx",
			output: `{"url":"https://[2001:db8::1]:443/","host":"[2001:db8::1]","ip":"2001:db8::1",` +
				`"port":"443","scheme":"https","title":"v6","webserver":"nginx"}` + "\n",
			want: []Host{{
				Host:         "2001:db8::1",
				IP:           "2001:db8::1",
				Port:         443,
				Protocol:     "https",
				Title:        "v6",
				Server:       "nginx",
				Technologies: []Technology{tech("nginx", "")},
				Source:       "httpx",
			}},
		},
		{
			name: "纯文本带状态码/标题/Server/指纹",
			tool: "httpx",
			output: "https://a.example.com [200] [Example Domain] [nginx] [PHP:7.4]\n" +
				"http://b.example.com:8080 [403] [] [Apache/2.4.41]\n" +
				"c.example.com:443 [200] [C]\n" +
				"[INF] log line\n",
			want: []Host{
				{
					Host:         "a.example.com",
					Port:         443,
					Domain:       "example.com",
					Protocol:     "https",
					Title:        "Example Domain",
					Server:       "nginx",
					Technologies: []Technology{tech("PHP", "7.4"), tech("nginx", "")},
					Source:       "httpx",
				},
				{
					Host:         "b.example.com",
					Port:         8080,
					Domain:       "example.com",
					Protocol:     "http",
					Server:       "Apache",
					Technologies: []Technology{tech("Apache", "2.4.41")},
					Source:       "httpx",
				},
				{
					Host:     "c.example.com",
					Port:     443,
					Domain:   "example.com",
					Protocol: "https",
					Title:    "C",
					Source:   "httpx",
				},
			},
		},
		{
			name:   "纯文本只有 URL 与 Server 组",
			tool:   "httpx",
			output: "https://x.example.com [nginx]\n",
			want: []Host{{
				Host:         "x.example.com",
				Port:         443,
				Domain:       "example.com",
				Protocol:     "https",
				Server:       "nginx",
				Technologies: []Technology{tech("nginx", "")},
				Source:       "httpx",
			}},
		},
		{
			name:   "纯文本只有 URL（无分组）",
			tool:   "httpx",
			output: "https://only.example.com/path?q=1\n",
			want: []Host{{
				Host:     "only.example.com",
				Port:     443,
				Domain:   "example.com",
				Protocol: "https",
				Source:   "httpx",
			}},
		},
		{
			name:   "空输出",
			tool:   "httpx",
			output: "",
			want:   nil,
		},
	})
}

// ---- naabu ----

func TestParseNaabu(t *testing.T) {
	runParseCases(t, []parseCase{
		{
			name: "JSON 行（含字符串端口/仅 IP）",
			tool: "naabu",
			output: "{\"host\":\"a.example.com\",\"ip\":\"1.2.3.4\",\"port\":443}\n" +
				"{\"host\":\"b.example.com\",\"ip\":\"1.2.3.5\",\"port\":\"80\"}\n" +
				"{\"host\":\"1.2.3.6\",\"port\":22}\n" +
				"[INF] started\n",
			want: []Host{
				{Host: "a.example.com", IP: "1.2.3.4", Port: 443, Domain: "example.com", Protocol: "https", Source: "naabu"},
				{Host: "b.example.com", IP: "1.2.3.5", Port: 80, Domain: "example.com", Protocol: "http", Source: "naabu"},
				{Host: "1.2.3.6", IP: "1.2.3.6", Port: 22, Protocol: "ssh", Source: "naabu"},
			},
		},
		{
			name: "纯文本 host:port（含 IPv6/非法端口/横幅）",
			tool: "naabu",
			output: "a.example.com:443\n" +
				"1.2.3.4:22\n" +
				"[2001:db8::1]:80\n" +
				"b.example.com:99999\n" +
				"naabu v2.1.0\n",
			want: []Host{
				{Host: "a.example.com", Port: 443, Domain: "example.com", Protocol: "https", Source: "naabu"},
				{Host: "1.2.3.4", IP: "1.2.3.4", Port: 22, Protocol: "ssh", Source: "naabu"},
				{Host: "2001:db8::1", IP: "2001:db8::1", Port: 80, Protocol: "http", Source: "naabu"},
				{Host: "b.example.com", Domain: "example.com", Source: "naabu"},
			},
		},
		{
			name:   "同键去重",
			tool:   "naabu",
			output: "{\"host\":\"dup.example.com\",\"ip\":\"1.2.3.4\",\"port\":80}\n{\"host\":\"DUP.example.com\",\"ip\":\"1.2.3.4\",\"port\":80}\n",
			want: []Host{
				{Host: "dup.example.com", IP: "1.2.3.4", Port: 80, Domain: "example.com", Protocol: "http", Source: "naabu"},
			},
		},
		{
			name:   "全是垃圾行",
			tool:   "naabu",
			output: "garbage\n---\n\n",
			want:   nil,
		},
	})
}

// ---- nmap ----

func TestParseNmap(t *testing.T) {
	runParseCases(t, []parseCase{
		{
			name: "XML（open 端口/主机名/产品版本/ostype/http-title/IPv6）",
			tool: "nmap",
			output: `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE nmaprun>
<nmaprun scanner="nmap" args="nmap -sV -oX - example.com" start="1690000000">
  <host starttime="1690000000" endtime="1690000010">
    <status state="up"/>
    <address addr="1.2.3.4" addrtype="ipv4"/>
    <hostnames>
      <hostname name="a.example.com" type="user"/>
      <hostname name="ptr.example.net" type="PTR"/>
    </hostnames>
    <ports>
      <port protocol="tcp" portid="80">
        <state state="open" reason="syn-ack"/>
        <service name="http" product="nginx" version="1.18.0" ostype="Linux" method="probed" conf="10"/>
        <script id="http-title" output="Example Domain"/>
      </port>
      <port protocol="tcp" portid="443">
        <state state="closed"/>
        <service name="https" product="nginx" version="1.18.0"/>
      </port>
      <port protocol="tcp" portid="22">
        <state state="open"/>
        <service name="ssh" product="OpenSSH" version="8.2p1"/>
      </port>
    </ports>
  </host>
  <host>
    <status state="up"/>
    <address addr="2001:db8::5" addrtype="ipv6"/>
    <hostnames/>
    <ports>
      <port protocol="tcp" portid="3306">
        <state state="open"/>
        <service name="mysql" product="MySQL" version="8.0.32"/>
      </port>
    </ports>
  </host>
</nmaprun>
`,
			want: []Host{
				{
					Host:         "a.example.com",
					IP:           "1.2.3.4",
					Port:         80,
					Domain:       "example.com",
					Protocol:     "http",
					Title:        "Example Domain",
					Server:       "nginx",
					Service:      "http",
					Technologies: []Technology{tech("nginx", "1.18.0"), tech("Linux", "")},
					Source:       "nmap",
				},
				{
					Host:         "a.example.com",
					IP:           "1.2.3.4",
					Port:         22,
					Domain:       "example.com",
					Protocol:     "ssh",
					Server:       "OpenSSH",
					Service:      "ssh",
					Technologies: []Technology{tech("OpenSSH", "8.2p1")},
					Source:       "nmap",
				},
				{
					Host:         "2001:db8::5",
					IP:           "2001:db8::5",
					Port:         3306,
					Protocol:     "mysql",
					Server:       "MySQL",
					Service:      "mysql",
					Technologies: []Technology{tech("MySQL", "8.0.32")},
					Source:       "nmap",
				},
			},
		},
		{
			name: "XML 前有日志、后有多余文本",
			tool: "nmap",
			output: "Starting Nmap 7.94 ( https://nmap.org ) at 2024-01-01 00:00 UTC\n" +
				"Warning: some warning\n" +
				`<nmaprun><host><address addr="9.9.9.9" addrtype="ipv4"/><hostnames/>` +
				`<ports><port protocol="tcp" portid="22"><state state="open"/><service name="ssh"/></port></ports>` +
				`</host></nmaprun>` + "\n" +
				"trailing junk\n",
			want: []Host{
				{Host: "9.9.9.9", IP: "9.9.9.9", Port: 22, Protocol: "ssh", Service: "ssh", Source: "nmap"},
			},
		},
		{
			name: "纯文本表格（多主机/关闭端口/版本列/无主机名 IP）",
			tool: "nmap",
			output: "Starting Nmap 7.94 ( https://nmap.org ) at 2024-01-01 00:00 UTC\n" +
				"Nmap scan report for a.example.com (1.2.3.4)\n" +
				"Host is up (0.012s latency).\n" +
				"PORT     STATE  SERVICE   VERSION\n" +
				"80/tcp   open   http      nginx 1.18.0\n" +
				"443/tcp  closed https\n" +
				"22/tcp   open   ssh       OpenSSH 8.2p1 Ubuntu 4ubuntu0.5 (Ubuntu Linux; protocol 2.0)\n" +
				"MAC Address: 00:11:22:33:44:55 (VMware)\n" +
				"\n" +
				"Nmap scan report for 5.6.7.8\n" +
				"PORT     STATE SERVICE\n" +
				"3306/tcp open  mysql   MySQL 8.0.32\n" +
				"Service detection performed.\n" +
				"\n" +
				"Nmap scan report for down.example.com (9.9.9.9)\n" +
				"Host is down\n" +
				"Nmap done: 3 IP addresses (2 hosts up) scanned in 10.00 seconds\n",
			want: []Host{
				{
					Host:         "a.example.com",
					IP:           "1.2.3.4",
					Port:         80,
					Domain:       "example.com",
					Protocol:     "http",
					Server:       "nginx",
					Service:      "http",
					Technologies: []Technology{tech("nginx", "1.18.0")},
					Source:       "nmap",
				},
				{
					Host:         "a.example.com",
					IP:           "1.2.3.4",
					Port:         22,
					Domain:       "example.com",
					Protocol:     "ssh",
					Server:       "OpenSSH",
					Service:      "ssh",
					Technologies: []Technology{tech("OpenSSH", "8.2p1")},
					Source:       "nmap",
				},
				{
					Host:         "5.6.7.8",
					IP:           "5.6.7.8",
					Port:         3306,
					Protocol:     "mysql",
					Server:       "MySQL",
					Service:      "mysql",
					Technologies: []Technology{tech("MySQL", "8.0.32")},
					Source:       "nmap",
				},
			},
		},
		{
			name: "纯文本 ssl/http 映射为 https",
			tool: "nmap",
			output: "Nmap scan report for s.example.com (10.0.0.1)\n" +
				"443/tcp open ssl/http nginx 1.18.0\n",
			want: []Host{
				{
					Host:         "s.example.com",
					IP:           "10.0.0.1",
					Port:         443,
					Domain:       "example.com",
					Protocol:     "https",
					Server:       "nginx",
					Service:      "ssl/http",
					Technologies: []Technology{tech("nginx", "1.18.0")},
					Source:       "nmap",
				},
			},
		},
		{
			name:   "残缺 XML 不 panic 且回退文本后为空",
			tool:   "nmap",
			output: "<nmaprun><host><address addr=\"1.2.3.4\"",
			want:   nil,
		},
		{
			name:   "纯文本无扫描目标行",
			tool:   "nmap",
			output: "80/tcp open http nginx\n[INF] nothing here\n",
			want:   nil,
		},
		{
			name:   "空输出",
			tool:   "nmap",
			output: "",
			want:   nil,
		},
	})
}

// ---- fscan ----

func TestParseFscan(t *testing.T) {
	runParseCases(t, []parseCase{
		{
			name: "端口/Web/漏洞混合行",
			tool: "fscan",
			output: "[*] 1.2.3.4:80 open\n" +
				"[+] 1.2.3.4:445 open ms17-010\n" +
				"[+] http://1.2.3.4 200\n" +
				"[+] 1.2.3.5 MS17-010\n",
			want: []Host{
				{Host: "1.2.3.4", IP: "1.2.3.4", Port: 80, Protocol: "http", Service: "http", Source: "fscan"},
				{Host: "1.2.3.4", IP: "1.2.3.4", Port: 445, Service: "smb", Source: "fscan"},
				{Host: "1.2.3.5", IP: "1.2.3.5", Source: "fscan"},
			},
		},
		{
			name: "日志/中文/ANSI/关闭端口/Web 标题",
			tool: "fscan",
			output: `[*] fscan 开始扫描
[INFO] 正在扫描 1.2.3.0/24
[+] 10.0.0.1:22 open ssh
[!] 10.0.0.2:3306 open mysql
[-] 10.0.0.3:21 closed
[*] 10.0.0.4:6379 open redis
[+] 10.0.0.5:8080 open http
[+] http://10.0.0.6:8081 200 后台管理系统
` + "\x1b[32m[+] 192.168.1.10:3389 open rdp\x1b[0m\n",
			want: []Host{
				{Host: "10.0.0.1", IP: "10.0.0.1", Port: 22, Protocol: "ssh", Service: "ssh", Source: "fscan"},
				{Host: "10.0.0.2", IP: "10.0.0.2", Port: 3306, Protocol: "mysql", Service: "mysql", Source: "fscan"},
				{Host: "10.0.0.4", IP: "10.0.0.4", Port: 6379, Protocol: "redis", Service: "redis", Source: "fscan"},
				{Host: "10.0.0.5", IP: "10.0.0.5", Port: 8080, Protocol: "http", Service: "http", Source: "fscan"},
				{Host: "10.0.0.6", IP: "10.0.0.6", Port: 8081, Protocol: "http", Title: "后台管理系统", Source: "fscan"},
				{Host: "192.168.1.10", IP: "192.168.1.10", Port: 3389, Service: "rdp", Source: "fscan"},
			},
		},
	})
}

// ---- masscan ----

func TestParseMasscan(t *testing.T) {
	runParseCases(t, []parseCase{
		{
			name: "Discovered open port 行",
			tool: "masscan",
			output: "Starting masscan 1.3.2 (http://bit.ly/14GZzcT) at 2024-01-01 00:00:00 GMT\n" +
				"Initiating SYN Stealth Scan\n" +
				"Discovered open port 443/tcp on 1.2.3.4\n" +
				"Discovered open port 80/tcp on 1.2.3.4\n" +
				"Banner on port 8080/tcp on 1.2.3.5: [http] nginx\n",
			want: []Host{
				{Host: "1.2.3.4", IP: "1.2.3.4", Port: 443, Protocol: "tcp", Source: "masscan"},
				{Host: "1.2.3.4", IP: "1.2.3.4", Port: 80, Protocol: "tcp", Source: "masscan"},
			},
		},
		{
			name: "grepable open 行/udp/异常端口",
			tool: "masscan",
			output: "open tcp 22 10.0.0.5 1690000000\n" +
				"open udp 53 10.0.0.6 1690000000\n" +
				"open tcp 8080 10.0.0.7 1690000000,\n" +
				"Discovered open port 99999/tcp on 10.0.0.8\n" +
				"Discovered open port 443/tcp on\n" +
				"garbage line\n",
			want: []Host{
				{Host: "10.0.0.5", IP: "10.0.0.5", Port: 22, Protocol: "tcp", Source: "masscan"},
				{Host: "10.0.0.6", IP: "10.0.0.6", Port: 53, Protocol: "udp", Source: "masscan"},
				{Host: "10.0.0.7", IP: "10.0.0.7", Port: 8080, Protocol: "tcp", Source: "masscan"},
			},
		},
	})
}

// ---- rustscan ----

func TestParseRustscan(t *testing.T) {
	runParseCases(t, []parseCase{
		{
			name: "非 greppable Open 行",
			tool: "rustscan",
			output: `.----. .-. .-. .----..---.  .----. .---.   .--.  .-. .-.
| {}  }| { } |{ {__ {_   _}{ {__  /  ___} / {} \ |  ` + "`" + `| |
[~] The config file is expected to be at "/home/kali/.rustscan.toml"
[~] Automatically increasing ulimit value to 5000.
Open 192.168.1.10:80
Open 192.168.1.10:443
Open 192.168.1.11:22
`,
			want: []Host{
				{Host: "192.168.1.10", IP: "192.168.1.10", Port: 80, Protocol: "http", Source: "rustscan"},
				{Host: "192.168.1.10", IP: "192.168.1.10", Port: 443, Protocol: "https", Source: "rustscan"},
				{Host: "192.168.1.11", IP: "192.168.1.11", Port: 22, Protocol: "ssh", Source: "rustscan"},
			},
		},
		{
			name: "greppable 汇总行 + 日志噪音",
			tool: "rustscan",
			output: "[~] File limit higher than batch size. Can increase with -b\n" +
				"192.168.1.20 -> [80,443,8080]\n" +
				"192.168.1.21 -> [3306]\n" +
				"Open 192.168.1.21:6379\n",
			want: []Host{
				{Host: "192.168.1.20", IP: "192.168.1.20", Port: 80, Protocol: "http", Source: "rustscan"},
				{Host: "192.168.1.20", IP: "192.168.1.20", Port: 443, Protocol: "https", Source: "rustscan"},
				{Host: "192.168.1.20", IP: "192.168.1.20", Port: 8080, Protocol: "http", Source: "rustscan"},
				{Host: "192.168.1.21", IP: "192.168.1.21", Port: 3306, Protocol: "mysql", Source: "rustscan"},
				{Host: "192.168.1.21", IP: "192.168.1.21", Port: 6379, Protocol: "redis", Source: "rustscan"},
			},
		},
	})
}

// ---- amass ----

func TestParseAmass(t *testing.T) {
	runParseCases(t, []parseCase{
		{
			name: "默认子域名行 + 日志噪音",
			tool: "amass",
			output: "[INF] Amass has started\n" +
				"[DBG] resolving sub.example.com\n" +
				"www.example.com\n" +
				"api.example.com\n" +
				"[WRN] something\n" +
				"https://dev.example.com/path\n" +
				"not a host\n",
			want: []Host{
				{Host: "www.example.com", Domain: "example.com", Source: "amass"},
				{Host: "api.example.com", Domain: "example.com", Source: "amass"},
				{Host: "dev.example.com", Domain: "example.com", Source: "amass"},
			},
		},
		{
			name: "JSON 行（name/addresses/domain 回退）+ 畸形行",
			tool: "amass",
			output: "{\"name\":\"vpn.example.com\",\"domain\":\"example.com\",\"addresses\":[{\"ip\":\"1.2.3.4\",\"asn\":123}]}\n" +
				"{\"name\":\"mail.example.com\",\"addresses\":[{\"ip\":\"2001:db8::10\"}]}\n" +
				"{\"name\":\"\",\"domain\":\"fallback.example.com\"}\n" +
				"{\"name\": broken\n" +
				"[INF] done\n",
			want: []Host{
				{Host: "vpn.example.com", IP: "1.2.3.4", Domain: "example.com", Source: "amass"},
				{Host: "mail.example.com", IP: "2001:db8::10", Domain: "example.com", Source: "amass"},
				{Host: "fallback.example.com", Domain: "example.com", Source: "amass"},
			},
		},
	})
}

// ---- wafw00f ----

func TestParseWafw00f(t *testing.T) {
	runParseCases(t, []parseCase{
		{
			name: "Cloudflare 识别",
			tool: "wafw00f",
			output: "[*] Checking https://shop.example.com\n" +
				"[+] The site https://shop.example.com is behind Cloudflare (Cloudflare Inc.) WAF.\n" +
				"[~] Number of requests: 10\n",
			want: []Host{
				{
					Host:         "shop.example.com",
					Port:         443,
					Domain:       "example.com",
					Protocol:     "https",
					Technologies: []Technology{tech("Cloudflare", "")},
					Source:       "wafw00f",
				},
			},
		},
		{
			name: "多目标/无 WAF/匿名 WAF/横幅噪音",
			tool: "wafw00f",
			output: "                  ______\n" +
				"                 /      \\\n" +
				"                (  WAFW00F  )\n" +
				"                 \\______/\n" +
				"[*] Checking http://plain.example.com\n" +
				"[-] No WAF detected by the generic detection\n" +
				"[*] Checking http://api.example.com:8080\n" +
				"[+] The site http://api.example.com:8080 is behind Akamai (Akamai Technologies) WAF.\n" +
				"[*] Checking https://blog.example.com\n" +
				"[+] The site https://blog.example.com is behind a WAF.\n",
			want: []Host{
				{Host: "plain.example.com", Port: 80, Domain: "example.com", Protocol: "http", Source: "wafw00f"},
				{
					Host:         "api.example.com",
					Port:         8080,
					Domain:       "example.com",
					Protocol:     "http",
					Technologies: []Technology{tech("Akamai", "")},
					Source:       "wafw00f",
				},
				{Host: "blog.example.com", Port: 443, Domain: "example.com", Protocol: "https", Source: "wafw00f"},
			},
		},
	})
}

// ---- katana / gau / waybackurls（URL 型） ----

func TestParseURLTools(t *testing.T) {
	runParseCases(t, []parseCase{
		{
			name: "katana 同主机多 URL 去重 + 显式端口",
			tool: "katana",
			output: "http://shop.example.com/\n" +
				"https://shop.example.com:8443/login?next=/admin\n" +
				"https://shop.example.com/login\n" +
				"https://shop.example.com/login?user=1\n" +
				"http://api.example.com/v1/users?id=1\n" +
				"[INF] crawled 5 URLs\n",
			want: []Host{
				{Host: "shop.example.com", Port: 80, Domain: "example.com", Protocol: "http", Source: "katana"},
				{Host: "shop.example.com", Port: 8443, Domain: "example.com", Protocol: "https", Source: "katana"},
				{Host: "shop.example.com", Port: 443, Domain: "example.com", Protocol: "https", Source: "katana"},
				{Host: "api.example.com", Port: 80, Domain: "example.com", Protocol: "http", Source: "katana"},
			},
		},
		{
			name:   "katana IPv6 URL + 非 URL 噪音",
			tool:   "katana",
			output: "[INF] started\nhttp://[2001:db8::1]:8080/index\nnot-a-url\nhttps://js.example.com/static/app.js\n",
			want: []Host{
				{Host: "2001:db8::1", IP: "2001:db8::1", Port: 8080, Protocol: "http", Source: "katana"},
				{Host: "js.example.com", Port: 443, Domain: "example.com", Protocol: "https", Source: "katana"},
			},
		},
		{
			name: "gau URL 去重",
			tool: "gau",
			output: "https://app.example.com/users?id=1\n" +
				"https://app.example.com:443/legacy\n" +
				"http://old.example.com/robots.txt\n" +
				"[INF] fetching\n" +
				"https://app.example.com/users?id=2\n",
			want: []Host{
				{Host: "app.example.com", Port: 443, Domain: "example.com", Protocol: "https", Source: "gau"},
				{Host: "old.example.com", Port: 80, Domain: "example.com", Protocol: "http", Source: "gau"},
			},
		},
		{
			name: "gau 日志/畸形行/同主机合并",
			tool: "gau",
			output: "[INF] fetching urls\n" +
				"https://gau.example.com:8443/a?x=1\n" +
				"not a url\n" +
				"https://gau.example.com/a\n" +
				"http://gau.example.com/b\n" +
				"https://gau.example.com/a?dup=1\n",
			want: []Host{
				{Host: "gau.example.com", Port: 8443, Domain: "example.com", Protocol: "https", Source: "gau"},
				{Host: "gau.example.com", Port: 443, Domain: "example.com", Protocol: "https", Source: "gau"},
				{Host: "gau.example.com", Port: 80, Domain: "example.com", Protocol: "http", Source: "gau"},
			},
		},
		{
			name: "waybackurls http/https/ftp",
			tool: "waybackurls",
			output: "http://legacy.example.com/index.php?id=1\n" +
				"https://legacy.example.com:8443/admin\n" +
				"ftp://files.example.com/pub/file.txt\n" +
				"http://legacy.example.com/\n",
			want: []Host{
				{Host: "legacy.example.com", Port: 80, Domain: "example.com", Protocol: "http", Source: "waybackurls"},
				{Host: "legacy.example.com", Port: 8443, Domain: "example.com", Protocol: "https", Source: "waybackurls"},
				{Host: "files.example.com", Port: 21, Domain: "example.com", Protocol: "ftp", Source: "waybackurls"},
			},
		},
		{
			name: "waybackurls 日志/畸形行/多端口",
			tool: "waybackurls",
			output: "[INF] fetching\n" +
				"https://wb.example.com/x\n" +
				"ftp://wb.example.com:2121/file\n" +
				"garbage\n" +
				"http://wb.example.com:8080/admin\n" +
				"https://wb.example.com/x?dup=1\n",
			want: []Host{
				{Host: "wb.example.com", Port: 443, Domain: "example.com", Protocol: "https", Source: "waybackurls"},
				{Host: "wb.example.com", Port: 2121, Domain: "example.com", Protocol: "ftp", Source: "waybackurls"},
				{Host: "wb.example.com", Port: 8080, Domain: "example.com", Protocol: "http", Source: "waybackurls"},
			},
		},
	})
}

// ---- 无显式端口时按 scheme 补默认端口 ----

func TestDefaultPortFromScheme(t *testing.T) {
	cases := []struct {
		name         string
		tool         string
		output       string
		wantPort     int
		wantProtocol string
	}{
		{"httpx https 无端口", "httpx", "https://a.example.com\n", 443, "https"},
		{"katana http 无端口", "katana", "http://a.example.com/x\n", 80, "http"},
		{"gau https 无端口", "gau", "https://a.example.com/x\n", 443, "https"},
		{"waybackurls ftp 无端口", "waybackurls", "ftp://files.example.com/pub/file\n", 21, "ftp"},
		{"显式端口优先", "katana", "https://a.example.com:8443/x\n", 8443, "https"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse(tc.tool, nil, tc.output)
			if err != nil {
				t.Fatalf("Parse 返回错误: %v", err)
			}
			if len(got) != 1 {
				t.Fatalf("期望 1 条，得到 %#v", got)
			}
			if got[0].Port != tc.wantPort {
				t.Errorf("Port = %d，期望 %d", got[0].Port, tc.wantPort)
			}
			if got[0].Protocol != tc.wantProtocol {
				t.Errorf("Protocol = %q，期望 %q", got[0].Protocol, tc.wantProtocol)
			}
		})
	}
}
