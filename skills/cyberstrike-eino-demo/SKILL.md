---
name: cyberstrike-eino-demo
description: A fully-outfitted demo skill package: SKILL.md + scripts/, references/, assets/ and other optional directories; validates Eino skill and in-package HTTP paths (authorized security testing and teaching only).
---

# CyberStrike × Eino fully-outfitted skill demo

This package follows [Agent Skills](https://platform.claude.com/docs/en/agents-and-tools/agent-skills/overview): **`SKILL.md` serves as the manifest + main description** (no separate `SKILL.yaml`). The same directory may hold **`scripts/`**, **`references/`**, **`assets/`** and any other subdirectories (as long as paths are safe and the package depth / file-count limits are not touched), read by **`ListPackageFiles` / `resource_path`** and Eino's native tools. Additional notes live in `FORMS.md` and `REFERENCE.md`.

## Overview

Used for one-shot verification of:

- The HTTP `GET /api/skills` list (`script_count`, `file_count`, `progressive` etc. are derived/scanned results)
- `GET /api/skills/cyberstrike-eino-demo?depth=summary|full`
- `section=` matching a short id of a **`##` heading** or an ASCII heading in `SKILL.md` (e.g. `## Payload samples` usually corresponds to `section=payload`)
- Reading package-relative-path resources via the ADK **`skill`** tool inside multi-agent (plus optional native file tools)
- Eino `FilesystemSkillsRetriever` retrieving package summaries, `##` sections, and script entries

**Hard requirement**: every test must have written authorization and stay within the agreed scope and time window.

## Authorized-testing workflow

1. **Scope confirmation**: domains / IPs, endpoint list, forbidden actions (DoS, database pulls, etc.).
2. **Baseline recording**: read-only probing of the agreed assets, saving timestamps and raw request/response summaries.
3. **Classified testing**: split tasks by vulnerability class; double-confirm the authorization boundary before high-risk operations.
4. **Evidence and reporting**: each finding accompanied by reproduction steps, impact, and a fix recommendation; redact sensitive data.
5. **Wrap-up**: delete temporary accounts, clean up test data, hand over the report.

## Payload samples

The following are **teaching placeholders**; real testing must substitute target context and never use them against unauthorized systems:

- SQLi probing (error-based): `"'` (observe whether it triggers a database error message leak)
- Reflected XSS (harmless): `<script>alert(1)</script>` → in a test range it should be encoded or CSP-intercepted
- Path traversal (read-only verification): `....//....//etc/passwd` (only in an authorized file-read scenario)

See `scripts/payloads.txt` for the detailed list.

## references/ and assets/

Used to verify that subdirectories other than `scripts/` are treated the same:

| Path | Purpose |
|------|---------|
| `references/citations.md` | Citations plus HTTP `resource_path` test notes |
| `assets/README.txt` | Placeholder resource (can be swapped with a real binary to test the read-file size limit) |

## Recommended toolchain

| Stage | Example tools |
|-------|---------------|
| Proxy and replay | Burp Suite, mitmproxy |
| Scanning and directories | ffuf, nuclei (lower concurrency to stay within authorization) |
| Vulnerability verification | self-written PoCs, official CLIs (sqlmap etc.) only within the authorized scope |
| Recording | Markdown + JSON snippet templates (see `scripts/report-snippet.json`) |

## Checklist and verification

- [ ] Written authorization and test window saved
- [ ] Files under `scripts/` consistent with body references
- [ ] Web or `GET /api/skills?...` can be cross-checked against the index; inside a multi-agent session, use the **`skill`** tool to load per-package and save tokens
- [ ] When detail is needed, pull the full text via the **`skill`** tool, or HTTP `depth=full`, `section=<heading or short id>`
- [ ] When raw script text is needed, use the native file tools or HTTP `resource_path=scripts/check-env.sh`
- [ ] `resource_path=references/citations.md` and `resource_path=assets/README.txt` are both readable
