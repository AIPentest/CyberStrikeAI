---
name: zero-day-discovery
description: >-
  0day autonomous discovery engine: variant analysis / patch gap / differential testing / Fuzzing / taint inference / N-day weaponization / hunter mindset. Use when public vulns not found and need to discover 0day or weaponize N-day.
metadata:
  tags: [penetration-testing, penetration-testing, red-team]
---

## 0day autonomous discovery engine (dig it yourself when the whole web has no findable vulnerability)

```
Core shift: from "matching known vulnerability databases" → "understanding how the code/protocol works, inferring where it will break". A 0day is not luck, it is method.
Five main roads:
  1. Variant analysis (highest yield): take a CVE patch → distill the vulnerability pattern → grep the pattern across the whole repository at other locations → what the patch did not cover = a 0day
  2. Patch gap: read the patch's filtering logic; blacklists can almost always be bypassed (a missing encoding / an equivalent function / an alias slipped in) → a new CVE
  3. Differential testing: two components disagree about the same input (WAF vs backend / validator vs executor) → smuggling / SSRF bypass
  4. Fuzzing: write a harness (wrapping the function that handles untrusted input) + build corpora/dictionaries + crash triage (reproducible / controllable / exploitability)
     AFL++/libFuzzer (coverage-guided, finds memory corruption) boofuzz (protocols) radamsa (black-box) restler (REST API)
  5. Taint inference (strongest with source in hand): a source (parameter/Header/deserialization field) reaching a sink (exec/SQL/template) without an effective sanitizer = a 0day
     CodeQL write a query to automatically hunt reachable data flows / Semgrep / Joern
N-day weaponization (advisory out but no PoC anywhere): from the patch diff reverse-rebuild the exploit (bindiff/diaphora binary comparison,
  the vendor's regression test is often the PoC embryo) → tune it on a local test range → hit the target. Maximize the vulnerability window — one of a red team's most valuable capabilities.
Hunter mindset (look at any code/endpoint/protocol, interrogate it layer by layer; every "yes" is a 0day candidate):
  Trust boundary: is the input assumed trusted? Under what circumstance does that fail? | State and timing: can state change between two steps (TOCTOU)? Can you scramble the order?
  Parse and normalization: how many parses? Does normalize come before or after validation? | Boundary extremes: negative/0/very large/type confusion/encoding/null bytes?
  Implied capability: what does this feature give me "by the way"? | Uniqueness: is the ID/token predictable? Is the "secret" really secret?
Turn a component into an "assumption list" and break them one by one: file upload implicitly assumes "only images are uploaded / the extension is trustworthy / the filename contains no path / the content is just data" → break each = a vulnerability, combine = a chain.
0day verification (a higher bar than a CVE): reproducible (minimal PoC) + root cause clear (which line / which assumption) + impact provable (actual read/write/execution) + ruling out false positives.
```
