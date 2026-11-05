---
name: source-code-hunting
description: >-
  Source-code hunting: .git leaks, dangerous-function grep, JS RC4 deobfuscation, semgrep/CodeQL, trufflehog, patch diff, supply-chain/CI. Use when hunting source leaks, secrets, JS deobfuscation, or supply-chain issues.
metadata:
  tags: [penetration-testing, penetration-testing, red-team]
---

## Source-code hunting

```
=== Source-code hunting ===
.git leaks: git-dumper → git log -p --all (sensitive files already deleted) | .svn/.DS_Store/composer.lock
Dangerous-function grep: exec/system/eval/unserialize/pickle.loads/render/curl_exec + hardcoded keys (sk-/ghp_/BEGIN RSA)
JS obfuscation breaking (RC4 + base64 string-array pattern): 1) extract the string array (var a0G=[...]) 2) find the decoder function (a0m(idx,key) → RC4 decrypt + base64) 3) find the rotation IIFE (target offset) 4) rebuild the decoder in Node.js and bulk-decode all strings → obtain plaintext variable names / API paths / configuration
  UniApp signature: app-service.js (business logic, often 800KB+ obfuscated) + zlsioh.dat (encrypted configuration, decrypted by native .so) + dcloud_uniplugins.json (plugin manifest)
Static scanning: semgrep --config=auto for fast scanning / CodeQL build a database and write queries (taint solving + variant-analysis methodology, see `zero-day-discovery`)
Secrets deep-dig: trufflehog/gitleaks scan full git history + docker image layers + npm/PyPI tarballs + frontend bundles (--only-verified distinguishes live from dead keys)
Framework patch diff: clone adjacent versions and diff → what was fixed = where the vulnerability is | dependency chain: composer.json/npm audit/pip-audit
Supply-chain/CI: dependency confusion (squatting internal package names on a public registry) | GHA command injection ${{github.event.issue.title}} | self-hosted runner takeover | .npmrc/.pypirc credentials
```
