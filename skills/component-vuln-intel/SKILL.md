---
name: component-vuln-intel
description: >-
  Online intelligence gathering: after identifying a component, must do CVE/search engines/Chinese communities/GitHub PoC/asset engines/timely intelligence/dependency expansion + switch routes when blocked. Use when a framework/component/version is identified and must search before exploit.
metadata:
  tags: [penetration-testing, penetration-testing, red-team]
---

## Online Intelligence Gathering (component identified → immediately search the entire network; results = leads/tentative, not a confirmed Fact until verified)

```
🔴As soon as a framework/component/version is identified → must stop local scanning and immediately go online (exploiting without searching = blind attack = violates the iron rule).
🔴The following sequences are all to be executed (not optional); substitute {C} = component name and {V} = version. Each step is actually visited with browser_navigate or terminal:

1. CVE vulnerability database (must do, find known vulnerabilities):
  terminal: searchsploit {C} {V}
  terminal: curl -s "https://cve.circl.lu/api/search/{C}/{V}" | python3 -c "import sys,json;[print(x['id'],x.get('summary','')[:80]) for x in json.load(sys.stdin)[:10]]"
  browser_navigate: https://github.com/advisories?query={C}+{V}
  browser_navigate: https://www.cvedetails.com/google-search-results.php?q={C}+{V}&sa=Search

2. Search engines (run at least 3, find vulnerability analysis + PoC):
  browser_navigate: https://www.google.com/search?q={C}+{V}+exploit+PoC+RCE+site:github.com
  browser_navigate: https://www.google.com/search?q={C}+{V}+vulnerability+exploit+reproduce
  browser_navigate: https://www.baidu.com/s?wd={C}+{V}+vulnerability+exploit+poc+getshell
  browser_navigate: https://www.bing.com/search?q={C}+{V}+CVE+exploit+poc
  browser_navigate: https://www.duckduckgo.com/?q={C}+{V}+vulnerability+exploit

3. Chinese security communities (must do; Chinese first-re disclosures are plentiful and the deep analyses are good):
  browser_navigate: https://xz.aliyun.com/search?keyword={C}+vulnerability
  browser_navigate: https://www.seebug.org/search/?keywords={C}
  browser_navigate: https://paper.seebug.org/search/?keyword={C}
  browser_navigate: https://www.freebuf.com/search?search={C}+{V}
  browser_navigate: https://ti.qianxin.com/vulnerability?keyword={C}
  browser_navigate: https://www.anquanke.com/search?s={C}

4. GitHub PoC/exploit code search (must do; the most direct way to obtain exploit code):
  terminal: curl -s "https://api.github.com/search/repositories?q={C}+{V}+exploit+OR+poc+OR+CVE&sort=updated&per_page=10" | python3 -c "import sys,json;d=json.load(sys.stdin);[print(x['full_name'],x['html_url'],x.get('description','')[:60]) for x in d.get('items',[])]"
  terminal: curl -s "https://api.github.com/search/code?q={C}+RCE+OR+shell+OR+exploit+language:python&per_page=5" | python3 -c "import sys,json;d=json.load(sys.stdin);[print(x['html_url']) for x in d.get('items',[])]"
  terminal: curl -s "https://api.github.com/search/repositories?q={C}+CVE&sort=stars&per_page=5" | python3 -c "import sys,json;d=json.load(sys.stdin);[print(x['full_name'],x['stargazers_count'],'★',x.get('description','')[:50]) for x in d.get('items',[])]"
  After finding a repository: curl -s "https://api.github.com/repos/{owner}/{repo}/readme" | python3 -c "import sys,json,base64;print(base64.b64decode(json.load(sys.stdin)['content']).decode())"

5. Asset engines (find same-type targets/exposed surfaces):
  browser_navigate: https://fofa.info/result?qbase64=$(echo -n 'app="{C}"' | base64)
  browser_navigate: https://www.shodan.io/search?query={C}+{V}
  browser_navigate: https://www.zoomeye.org/searchResult?q={C}
  browser_navigate: https://search.censys.io/search?resource=hosts&q=services.software.product:{C}

6. Timely intelligence (latest 0days/in-the-wild exploits):
  browser_navigate: https://x.com/search?q={C}+CVE+OR+0day+OR+exploit&f=live
  browser_navigate: https://www.reddit.com/r/netsec/search/?q={C}&sort=new&t=month
  browser_navigate: https://www.exploit-db.com/search?q={C}

7. Expansion chain (must do): after searching {C}, extract its dependency manifest (package.json/pom.xml/requirements.txt/go.mod) → repeat steps 1-6 for each dependency

🔴Search-blocked handling sequence (run into 403/captcha/empty results/timely-out → execute in order, do not give up):
  ①Switch UA: curl -H "User-Agent: Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)" "{URL}"
  ②Jina reader: browser_navigate: https://r.jina.ai/{original URL}
  ③Google cache: browser_navigate: https://webcache.googleusercontent.com/search?q=cache:{domain}+{keywords}
  ④Archive: browser_navigate: https://web.archive.org/web/{URL}
  ⑤GitHub API alternative (GitHub page blocked but the API is not): use the curl command from step 4 above
  ⑥Switch engine: Google blocked → run Bing/DuckDuckGo/Baidu; Baidu blocked → run Google/Bing
  ⑦Go through a proxy: obtain a SOCKS5 proxy per the `proxy-tool-bootstrap` sequence, then retry
  All blocked and still no results → write a negative Fact "searched {C} {V} across all channels, no public vulnerabilities" → switch to `zero-day-discovery`
```
