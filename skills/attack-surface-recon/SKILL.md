---
name: attack-surface-recon
description: >-
  Recon/attack surface mapping: passive whois/amass/crt.sh/FOFA/Shodan, active subfinder/httpx/naabu/katana/nuclei, DNS geo/CDN/Nginx catch-all/BT Panel/UniApp fingerprinting. The first action at kickoff, insights go into the project blackboard. Use when starting recon, asset mapping, fingerprinting, or CDN/DNS bypass discovery.
metadata:
  tags: [penetration-testing, penetration-testing, red-team]
---

## Recon / Attack Surface Mapping

```
=== Recon/attack surface mapping (first action at kickoff, 60% of wartime work happens here, do not charge bare-handed into exploitation; ports/fingerprints etc. go immediately into upsert_project_fact) ===
Passive first (does not touch the target, all to execute):
  terminal: whois {domain} | grep -iE 'org|name|email|registrant'
  terminal: amass intel -asn {ASN} -d {domain}
  browser_navigate: https://crt.sh/?q=%.{domain} (certificate transparency to mine subdomains)
  terminal: curl -s "https://crt.sh/?q=%.{domain}&output=json" | python3 -c "import sys,json;[print(x['name_value']) for x in json.load(sys.stdin)]" | sort -u
  terminal: dig +short {domain} @114.114.114.114; dig +short {domain} @8.8.8.8 (comparison differences = CDN)
  terminal: curl -s "https://web.archive.org/cdx/search/cdx?url=*.{domain}/*&output=text&fl=original&collapse=urlkey" | head -100
  browser_navigate: https://fofa.info/result?qbase64=$(echo -n 'domain="{domain}"' | base64)
  browser_navigate: https://www.shodan.io/search?query=hostname:{domain}
Active pipeline (execute step by step):
  terminal: subfinder -d {domain} -silent | tee subs.txt; amass enum -d {domain} -passive >> subs.txt; sort -u subs.txt -o subs.txt
  terminal: cat subs.txt | dnsx -silent | tee alive_subs.txt
  terminal: cat alive_subs.txt | httpx -silent -title -status-code -tech-detect -cdn | tee httpx_out.txt
  terminal: naabu -l alive_subs.txt -top-ports 1000 -silent | tee ports.txt; nmap -sCV -iL <(head -20 alive_subs.txt) -oN nmap_out.txt
  terminal: katana -list alive_subs.txt -silent -d 3 | tee urls.txt; gau {domain} >> urls.txt
  terminal: cat urls.txt | grep -iE '\.(js|json)$' | httpx -silent -mc 200 | while read u; do curl -s "$u" | grep -oiE '(api|secret|key|token|password|aws|endpoint)[^"]*'; done
  terminal: ffuf -u https://{target}/FUZZ -w /usr/share/seclists/Discovery/Web-Content/raft-medium-directories.txt -mc 200,301,302,403 -fs {catch-all-size}
  terminal: nuclei -l alive_subs.txt -t /root/nuclei-templates/ -severity medium,high,critical -o nuclei_results.txt
  → httpx output identifies a framework/version → immediately trigger the `component-vuln-intel` search sequence
🚨DNS geo restriction bypass: domestic CDNs (987dns/dnspod/Alibaba Cloud) often return 0.0.0.0 to overseas DNS; resolve via 114.114.114.114 to get the real IP. Multi-DNS comparison: dig @1.1.1.1 vs @114.114.114.114 vs @8.8.8.8; differences are CDN geo policy. The real IP hides in the domestic DNS results.
🚨CDN response-filtering bypass (GCCDN/PWS etc.): the CDN returns 502 for specific URL patterns (admin paths/actuator) instead of passing through the backend response:
  Fingerprinting: Server: PWS/x.x.x.x | Via: 1.1 PS-XXX-XXXX:N (W) | X-Px: ms CS-XXX-XXXXnone(origin) | wildcard *.gccdn.net CNAME
  502 ≠ not existing: 502 = the backend response is filtered by the CDN, 404 = the path genuinely does not exist, 403 = the CDN route is rejected. Use 502/404/403 differences to enumerate live endpoints
  Path traversal confirmation: /api/v1/v/..;/..;/actuator/env → 502 (exists but filtered) vs /api/v1/v/..;/..;/xxxx → 404 (Tomcat-native does not exist)
  CDN node multi-port: scan the CDN IP's ports 3000-9200 (8000/8080/9090 often proxy the same backend but rules may differ, 9200 may be a standalone nginx)
  Bypass attempt matrix (all attempts in this session failed, recorded to avoid repetition): URL encoding/double encoding/case change/semicolon suffix/TE space smuggling/HTTP pipelining/Accept-Encoding/Range
  Effective information extraction: even with 502, a POST to actuator/env may still be executed by the backend (blind attribute write) → needs a companion refresh trigger (if refresh 404, the chain breaks)
🚨Nginx catch-all trap: PHP CMS with rewrite over all routes → all paths return 200 with the same size = catch-all (not that the file exists!). Identification: compare 3 random paths, identical size = catch-all. Test truly live paths with the .php suffix (nginx `location ~ \.php$` goes straight to FastCGI; nonexistent returns a true 404).
🚨Pure-FTPd corrupted puredb ≠ authentication bypass: "Unable to read the indexed puredb file" = the virtual user db is broken; all usernames (including anonymous) trigger a 421 disconnect. PAM system-user authentication also goes through puredb first → all fail. Not an attack surface, skip.
🚨Multiple sites on one IP, lateral pivot: reverse lookup the IP (SecurityTrails/Microstep/VirusTotal) to find other domains on the same server → among them a weak site (standalone nginx config returns a true 404 = enumerable) breaks through → the BT Panel manages all sites uniformly = lateral to all sites.
Fingerprint priority: as soon as httpx/wappalyzer identifies a framework version → immediately switch to `component-vuln-intel` for online intelligence. Map the attack surface clear first, then Reason to pick the fattest edge; do not dive head-first into the first hole you see.
🚨DNS wildcard CDN identification: all subdomains (including nonexistent random subdomains) resolve to the CDN = wildcard DNS (*.domain → CDN CNAME). At that point enumerating subdomains is pointless; all IPs are CDN nodes.
  Verification: dig random123456.target.com → if it also has an A record pointing to a known CDN IP = wildcard. The real origin IP must be obtained from other channels (historical DNS/certificate transparency/other domains on the same server/mail headers/SSRF)
🚨BT Panel identification: the Cookie name is a 32-hex-char hash (e.g. 789d6d2de16419a1e8bbfea926c8996e) + 302 redirect to /login + nginx reverse proxy + "entry validation failed" page = BT Panel random secure entry
  Windows version: IIS+nginx coexist, panel on port 8888, phpMyAdmin on port 888 (possibly), /index.html may leak site configuration (reverse-proxy target URL/site name)
  Attack: entry-path brute force (8 random alphanumeric chars) | historical CVEs (API key unauth/phar deserialization) | panel default FTP/MySQL account reuse
🚨Nginx 404 differential fingerprinting: when the nginx catch-all rewrite masks real paths, compare response sizes — a true nginx 404 (146B) = the file exists on disk but the location block denies; catch-all (large page) = the route does not match. Batch-probe CMS-signature paths (/caches/configs/, /phpcms/modules/, /statics/ etc.) and the response sizes alone confirm the CMS type. See references/nginx-404-differential-fingerprinting.md
🚨UniApp/DCloud APK reverse quick path: assets/apps/__UNI__*/www/app-service.js = the entire business logic (obfuscated JS); assets/zlsioh.dat = encrypted configuration (API domains); lib/arm64-v8a/libirzlemnr.so = the decryption library. Decode the JS obfuscation with RC4+base64 (extract the a0G array + the a0m decoder); strings search in the .so for 16-char random strings = possible keys, search the `x-number.number.number.numberx` format = hardcoded IPs (usually DCloud HTTPDNS infrastructure, not the target API). See references/uniapp-dcloud-apk-reversing.md
```
