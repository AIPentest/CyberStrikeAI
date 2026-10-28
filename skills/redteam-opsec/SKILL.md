---
name: redteam-opsec
description: >-
  OPSEC covert-operations discipline: IP blacklist bypass, rate/timing, traffic blending, minimal footprint, anti-forensics, progressive exposure. Use when maintaining stealth, bypassing IP bans, or planning covert red-team ops.
metadata:
  tags: [penetration-testing, penetration-testing, red-team]
---

## OPSEC / covert operations discipline (AV-evasion / stability / stealth)

```
Core principle: getting in ≠ staying in. Being detected zeroes out the operation. Before each action, ask "what does this step look like from the defender's perspective".
🚨IP blacklist bypass (first thing to try after being banned): X-Forwarded-For: <random IP> (when the CDN trusts this header it directly bypasses the application-layer blacklist; if it returns code:10010 / blacklist, try XFF/X-Real-IP/CF-Connecting-IP/True-Client-IP one by one)
  Verify: a normal request returns "blacklist" + your IP → add the XFF header and it returns a normal 200 → carry XFF on all subsequent requests
  Advanced: rotate the XFF value every N requests (so the new IP doesn't get banned too) | some CDNs trust only the first XFF value, some trust the last
Rate and timing: throttle scanning (nuclei -rl / nmap -T2 --max-rate / ffuf -p delay) to avoid WAF bans + IDS threshold alerts | high-risk actions at low frequency + random jitter | avoid business peak hours but also avoid deep night (blend with the target's normal activity to stay least visible)
Traffic blending: mimic normal business traffic (common UA/Referer/legitimate paths) | probe defenses before bringing heavy weapons (process lists / known EDR / SIEM agent signatures) → if present, prefer quiet manual techniques; only go automated and bulk when there is no monitoring
Minimal footprint: prefer in-memory execution that never touches disk (DDexec/memfd/reflective loading) | webshell with strong password + uncommon path + disguised functionality | tunnels over 443/DNS blending with common egress | delete tools once used (/dev/shm as an in-memory disk, leave no wreckage)
Anti-forensics: unset HISTFILE / set +o history on command history | selectively delete only your own log entries (a full truncate actually triggers alerts) | for timestamps, touch -r against a neighboring file to preserve mtime | never touch monitoring/audit services (stopping one is itself an alert)
Progressive exposure: passive recon (certificate transparency / passive DNS / search engines / asset engines) → confirm there is no strong monitoring → only then active scanning → exploitation and landing last. Anything obtainable from public intelligence, never touch on the target directly. With each escalation ask "is it worth the exposure".
> Stealth is not squeamishness; it is a red team's survival skill. One reckless full-port full-speed scan can zero out the entire operation.
```
