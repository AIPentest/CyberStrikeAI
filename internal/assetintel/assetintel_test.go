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
	}
	for _, tool := range supported {
		if !Supports(tool) {
			t.Errorf("Supports(%q) = false，期望 true", tool)
		}
	}
	unsupported := []string{"", "masscan", "nuclei", "subfinder2", "httpx-extra", "sqlmap"}
	for _, tool := range unsupported {
		if Supports(tool) {
			t.Errorf("Supports(%q) = true，期望 false", tool)
		}
	}
}

func TestParseUnknownTool(t *testing.T) {
	hosts, err := Parse("masscan", nil, "1.2.3.4:80")
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
	for _, tool := range []string{"subfinder", "httpx", "naabu", "nmap"} {
		got, err := Parse(tool, nil, "")
		if err != nil {
			t.Fatalf("Parse(%q, 空输出) 返回错误: %v", tool, err)
		}
		if len(got) != 0 {
			t.Fatalf("Parse(%q, 空输出) 期望 0 条，得到 %#v", tool, got)
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
			want:   []Host{},
		},
		{
			name:   "空输出",
			tool:   "subfinder",
			output: "",
			want:   []Host{},
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
				Domain:   "example.com",
				Protocol: "https",
				Source:   "httpx",
			}},
		},
		{
			name:   "空输出",
			tool:   "httpx",
			output: "",
			want:   []Host{},
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
			want:   []Host{},
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
			want:   []Host{},
		},
		{
			name:   "纯文本无扫描目标行",
			tool:   "nmap",
			output: "80/tcp open http nginx\n[INF] nothing here\n",
			want:   []Host{},
		},
		{
			name:   "空输出",
			tool:   "nmap",
			output: "",
			want:   []Host{},
		},
	})
}
