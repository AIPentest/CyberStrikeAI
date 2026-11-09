# Security Hardening

[Chinese](../zh-CN/security-hardening.md)

This checklist covers pre-production and continuous hardening for CyberStrikeAI.

## Before Going Live

- Change the initial `admin` password from the Web UI after first login.
- Use HTTPS or a trusted reverse proxy.
- Restrict access by IP, VPN, or bastion.
- Enable `audit.enabled`.
- Set `c2.enabled: false` when C2 is not required.
- Do not expose standalone HTTP MCP without strong auth and network isolation.
- Connect only trusted external MCP services.
- Back up `config.yaml`, `data/`, and custom resource directories.

## Reverse Proxy Baseline

```nginx
client_max_body_size 200m;
proxy_buffering off;
proxy_http_version 1.1;
proxy_set_header Host $host;
proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
proxy_set_header X-Forwarded-Proto https;
proxy_set_header Upgrade $http_upgrade;
proxy_set_header Connection "upgrade";
```

Recommended security headers:

```nginx
add_header X-Content-Type-Options nosniff;
add_header Referrer-Policy no-referrer;
add_header X-Frame-Options DENY;
```

## HITL Allowlist Baseline

Minimal allowlist:

```yaml
hitl:
  tool_whitelist:
    - read_file
    - glob
    - grep
    - tool_search
```

Do not globally allowlist:

- `execute`;
- WebShell write/execute tools;
- C2 task/payload tools;
- high-risk external MCP tools;
- delete, write, upload, persistence tools.

## File Permissions

```bash
chmod 600 config.yaml
chmod 700 data
```

Run under a dedicated OS user. Avoid root unless explicitly required.

## External MCP Review

Before connecting:

- Can it execute commands?
- Can it read/write local files?
- Does it send data to third parties?
- Does it authenticate?
- Can output contain untrusted model/web content?
- Should it run in a container or separate user?

After connecting:

- keep high-risk tools out of allowlist;
- review tool list changes;
- audit config changes.

## C2 and WebShell

C2:

- disabled by default;
- enabled only during authorized window;
- listener ports separated from admin UI;
- cleanup payloads, sessions, tasks, and events.

WebShell:

- authorized targets only;
- clear naming;
- write/delete/execute requires approval;
- delete connections after project end.

## Retention

Suggested:

- audit: 30-90 days;
- monitor: 90-180 days;
- uploads: clean after project;
- C2/WebShell outputs: keep only report evidence;
- knowledge base: no real credentials or customer secrets.

## Periodic Review

Weekly:

- failed logins and unusual IPs;
- config changes;
- external MCP changes;
- long-running tools;
- unexpected C2 enablement;
- stale WebShell connections;
- disk and DB size.

Project closeout:

- clean temp workspaces;
- delete unnecessary uploads;
- archive evidence;
- delete stale WebShell/C2 resources;
- export audit records.
