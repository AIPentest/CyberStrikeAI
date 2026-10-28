---
name: initial-access-phishing
description: >-
  Initial access/phishing/social engineering: credential spraying, AiTM, device code, OAuth consent phishing, payloads, vishing. Use when needing initial access, phishing, AiTM, device code, or social engineering.
metadata:
  tags: [penetration-testing, penetration-testing, red-team]
---

## Initial Access / Phishing / Social Engineering

```
=== Initial access/phishing/social engineering (the first jump from outside to inside, the beachhead before the beachhead) ===
Credential spraying: weak passwords (Season+Year!/Company123) sprayed across all accounts (1-2 attempts per account to avoid lockout) | Sources: leak databases/OSINT email formats (f.last@)/default credentials
AiTM phishing (bypasses MFA): evilginx3/Modlishka reverse-proxies the real site → man-in-the-middle steals session cookies + tokens (bypassing MFA) | phishlet configures domain + certificate
Device-code phishing (Azure/M365): device code flow → induce the victim to input the code → get access/refresh token (no password/MFA needed) | TokenTactics/AADInternals
Phishing payloads: gophish to send emails + landing page | Payloads lnk/iso/macro/HTA/OneNote | Bypass the gateway: password-protected zip/cloud-drive link/HTML smuggling
OAuth consent phishing (illicit consent): a malicious App requests excessive scopes (Mail.Read/offline_access) → the victim consents → long-term access token
Social engineering prework: OSINT organizational structure/vendors/password habits (LinkedIn/recruitment/GitHub) | pretext excuse (IT support/vendor/HR) | vishing (deepfake voice)
→ As soon as initial access lands, switch to `redteam-opsec` (do not get caught by EDR the moment you walk in); the capabilities obtained go into `capability-primitive-search` to assemble the chain
```
