# Security Model

[Chinese](../zh-CN/security-model.md)

CyberStrikeAI is not a generic chatbot. It is a high-privilege security automation system with command execution, MCP tools, WebShell management, optional C2, batch tasks, and multi-agent orchestration.

## Trust Boundaries

Main actors:

- Web user: can chat, change settings, manage resources, and trigger tools.
- Agent: selects tools based on role, context, and middleware.
- MCP tools: may access files, run commands, call services, or touch targets.
- External MCP: third-party local or remote tool providers.
- Robot callbacks: platform-authenticated message ingress outside Web login.

Anyone who can log into the Web UI should be treated as an operator of the instance.

## Threat Model

| Threat | Path | Impact | Controls |
| --- | --- | --- | --- |
| Password leak | login, then use terminal/WebShell/C2 | platform takeover | strong password, HTTPS, internal network, audit |
| Prompt injection | target content instructs Agent to misuse tools | unauthorized actions | role boundaries, HITL, least tools |
| Malicious MCP | external tool lies or has side effects | host/target impact | trusted MCP only, isolation |
| Tool YAML tampering | command template changed | malicious execution | file permissions, review |
| C2 misuse | payload or task against unauthorized target | legal and business risk | disabled by default, approvals |
| WebShell misuse | destructive command on business host | outage/data loss | naming, read-only first, HITL |
| DB leak | copy `data/*.db` or uploads | sensitive target data | permissions, encrypted backups |

## HITL Is Not Magic

HITL sees a tool name, arguments, and context. It does not always see real-world impact. Be conservative when:

- a harmless-looking command wraps `bash -c` or base64;
- the MCP tool description is untrusted;
- WebShell target identity is vague;
- C2 payload delivery happens outside the platform;
- a read-only tool can still create traffic or side effects.

Audit Agent is useful for routine checks, not for replacing humans on destructive operations.

## Data Minimization

Avoid long-term storage of:

- real customer credentials;
- raw production data;
- long-lived cookies;
- unrelated scan output;
- stale WebShell or C2 sessions.

Project closeout should include cleanup of uploads, WebShell connections, C2 payloads, temporary workspaces, and bulky execution logs.

## Production Baseline

- Strong password and HTTPS.
- Internal/VPN/proxy restricted access.
- `audit.enabled: true`.
- Random `mcp.auth_header_value` when HTTP MCP is exposed.
- `c2.enabled: false` unless required.
- Minimal external MCP.
- No high-risk tools in global allowlist.

## Source Anchors

- Sessions: `internal/security/auth_manager.go`
- Auth middleware: `internal/security/auth_middleware.go`
- Rate limiting: `internal/security/ratelimit.go`
- Shell execution: `internal/security/executor.go`
- HITL execution: `internal/handler/hitl_execution.go`
- Audit service: `internal/audit/service.go`
