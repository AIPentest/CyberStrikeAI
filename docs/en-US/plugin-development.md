# Plugin Development

[中文](../zh-CN/plugin-development.md)

Plugins live under `plugins/`. The repo ships two reference implementations: **Burp Suite extension** and **Chromium DevTools extension**. Integrations typically use HTTP APIs, MCP servers, or resource packs (tools, roles, Skills, agents).

## Layout

```text
plugins/
  README.md
  burp-suite/cyberstrikeai-burp-extension/
  browser-extension/cyberstrikeai-browser-extension/
```

## Plugin Layers

| Layer | Example | Benefit | Cost |
| --- | --- | --- | --- |
| API plugin | Burp / browser extension calling Agent Stream | simple UI integration | depends on API/auth |
| MCP plugin | exposes tools to Agent | Agent can call it | needs schema and safety design |
| Resource pack | ships tools/roles/skills/agents | simple and versionable | less interactive |

Do not start with MCP unless the Agent must actively call your capability. For “send this HTTP request to AI”, an API plugin is enough.

## Burp Suite Extension

Java extension under `plugins/burp-suite/cyberstrikeai-burp-extension/`. Typical flow: read HTTP from Burp → format prompt → call CyberStrikeAI SSE → show Progress/Final in a Burp tab.

Build: JDK + Gradle/Maven → `bash build-mvn.sh` → `dist/cyberstrikeai-burp-extension.jar`.

## Browser Extension (Chromium DevTools)

MV3 DevTools extension under `plugins/browser-extension/cyberstrikeai-browser-extension/`. Aligned with the Burp plugin: capture Network traffic → HTTP/1.1 prompt → SSE output. Full docs: `README.md` / `README.zh-CN.md` in that directory.

Load unpacked at `chrome://extensions/`, or `bash package.sh` → `dist/cyberstrikeai-browser-extension.zip`.

### Auth best practices (browser)

Server `POST /api/auth/login` returns `{ token, expires_at }`. There is **no refresh token** — do not assume silent renewal. Reference: `lib/auth-session.js`, `lib/api.js`, `panel/panel.js`.

| Practice | Description |
| --- | --- |
| Session storage | Store token + `expires_at` in `chrome.storage.session`; never persist password |
| Remaining time | Show `OK · 11h 30m left`; warn when <30min |
| Local check | Re-check `expires_at` + `GET /api/auth/validate` every 30s |
| Server probe | Immediate probe when DevTools panel becomes visible |
| Unreachable | Show warning; keep token during transient outage |
| 401/403 | Clear token and require re-authentication |
| Before Send | `ensureAuthReady()` before SSE |
| Permissions | `optional_host_permissions` — request origin on Validate |

After extension reload, close DevTools completely and reopen F12 (stale panel context).

### Data and performance (browser)

- Caps: 200 captures/tab, 20 tabs, 512KB progress/run.
- Default XHR/Fetch only; use pause toggle when not capturing.
- Truncate or summarize large bodies before sending to Agent.

## API Integration

- Login: `POST /api/auth/login`, then `GET /api/auth/validate`.
- Persist `expires_at`; re-login when expired (no silent refresh).
- Prefer `/api/eino-agent/stream` or `/api/multi-agent/stream` (SSE).
- Large files: `/api/chat-uploads`, then reference in message.
- Full spec: `/api-docs` or `/api/openapi/spec`.

## API Plugin Payload

Include:

- source tool and context;
- target URL, method, key headers;
- truncation policy for request/response bodies;
- user intent;
- authorization boundary.

Large responses should be uploaded or summarized, not pasted whole into the prompt.

## MCP Schema Design

Bad:

```json
{"cmd":{"type":"string"}}
```

Better:

```json
{
  "target_url": {"type":"string","description":"authorized target URL"},
  "scan_profile": {"type":"string","enum":["passive","active-safe"]},
  "max_requests": {"type":"integer","description":"request limit"}
}
```

Specific schemas make HITL and Agent behavior safer.

## Security Boundaries

Plugins should not bypass platform controls:

- no hidden destructive local commands;
- no plaintext long-lived credentials (password only for login; token in session storage);
- no default third-party data exfiltration;
- no dependency on browser state to bypass login;
- on 401/403, clear session and require re-auth — do not silently retry.

## Source Anchors

- Burp plugin: `plugins/burp-suite/cyberstrikeai-burp-extension/src/main/java/burp/`
- Browser extension: `plugins/browser-extension/cyberstrikeai-browser-extension/`
  - Auth: `lib/auth-session.js`, `lib/api.js`, `lib/storage.js`
  - UI: `panel/panel.js`
  - Capture: `devtools.js`, `background/service-worker.js`
- OpenAPI: `internal/handler/openapi.go`
- External MCP: `internal/handler/external_mcp.go`
- Web auth reference: `web/static/js/auth.js`
