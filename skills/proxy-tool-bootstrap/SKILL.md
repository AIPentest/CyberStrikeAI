---
name: proxy-tool-bootstrap
description: >-
  Self-proxy-finding + tool bootstrap: SOCKS5/HTTP/Tor route-switch sequences, tool self-bootstrapping in Python, self-generated dictionaries, OOB infrastructure. Use when blocked by 403/429/WAF/timeout, missing tools, or needing OOB confirmation.
metadata:
  tags: [penetration-testing, penetration-testing, red-team]
---

## Self-Proxy Finding + Tool Bootstrap (switch routes when intercepted, write your own when tools are missing)

```
🔴Proxy (receiving rejection/rate limiting/timeout → the first reaction is not to retry, it is to switch routes. Not switching routes = giving up = violates blackboard trigger 2 (see `pentest-blackboard`)):
  Execution sequence (in order; only advance to the next step when the previous one fails):
  ①Probe the target region: terminal: curl -s "http://ip-api.com/json/{target IP}" | python3 -c "import sys,json;d=json.load(sys.stdin);print(d['country'],d['countryCode'])"
  ②Obtain a SOCKS5 proxy for that region: terminal: curl -s "https://api.proxyscrape.com/v2/?request=displayproxies&protocol=socks5&country={countryCode}&timeout=5000" | head -20
  ③Verify the proxy can reach the target: terminal: curl --socks5 {PROXY_IP:PORT} --connect-timeout 8 -s -o /dev/null -w "%{http_code}" {target URL}
  ④200 = usable; immediately rerun the blocked request with this proxy; non-200 = switch to the next proxy and repeat ③
  ⑤All SOCKS5 failed → obtain an HTTP proxy: terminal: curl -s "https://api.proxyscrape.com/v2/?request=displayproxies&protocol=http&country={countryCode}&timeout=5000" | head -20
  ⑥HTTP proxy verification: terminal: curl --proxy http://{PROXY_IP:PORT} --connect-timeout 8 -s -o /dev/null -w "%{http_code}" {target URL}
  ⑦All proxies failed → Tor: terminal: curl --socks5 127.0.0.1:9050 --connect-timeout 15 {target URL}
  Tools take a uniform proxy parameter: curl --socks5 / sqlmap --proxy=socks5://{P} / nmap --proxies socks5://{P} / nuclei -proxy socks5://{P} / ffuf -x socks5://{P}
  Rotation strategy: on 429/403 immediately switch to the next proxy; actively switch every 20 requests (to avoid the new IP also being banned) | CloudFlare → proxy pool + request interval randomized 2-5s
  🚨HTTP proxy vs SOCKS5: an HTTP proxy inserts its own error pages (502/this page cannot be displayed) making it impossible to distinguish the target's actual response → for probing you must use SOCKS5 (--socks5)
    ProxyScrape SOCKS5: https://api.proxyscrape.com/v2/?request=displayproxies&protocol=socks5&country=CN,JP&timeout=5000
    Verify SOCKS5: curl --socks5 IP:PORT --connect-timeout 5 target | the HTTP proxy is only suitable for already-confirmed reachable targets for anonymity/rotation
Tool bootstrap (which X || implement it with Python):
  No nmap → socket port scan | No ffuf → requests directory brute force | No sqlmap → manual payload detection | No hydra → requests brute force | No nuclei → requests hitting known payloads
  Complex tools simulated with Python: crawler requests+bs4 / encode base64/hex / hash hashlib / encrypt pycryptodome / network sniff scapy
Dictionary self-generation: craft variants based on the target domain/company name | extract keywords from the web page | username + year + special-character combinations | service default credentials
OOB infrastructure (blind vulnerabilities all rely on it): interactsh-client to obtain an oast.fun domain | or a VPS python3 -m http.server/nc to watch call-backs | ngrok/cloudflared tunnels
  → only upon seeing an OOB call-back (DNS query/HTTP request) is it counted as confirmation → write a Fact
```
