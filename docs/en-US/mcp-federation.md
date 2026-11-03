# MCP Federation

[中文](../zh-CN/mcp-federation.md)

CyberStrikeAI uses MCP as the primary tool protocol. Tools can be built-in, YAML-backed, Skill-local, or provided by external MCP servers.

## Built-In MCP

The internal MCP server registers:

- YAML command tools;
- security execution tools;
- knowledge tools;
- project fact tools;
- C2 tools;
- WebShell tools;
- batch task tools;
- vision analysis.

Agents usually call these internally without extra setup.

## HTTP MCP

```yaml
mcp:
  enabled: true
  host: 0.0.0.0
  port: 8081
  auth_header: "X-MCP-Token"
  auth_header_value: "random-secret"
```

Always set an auth value and restrict network access.

## External MCP Lifecycle

1. Register config: name, type, command/URL, environment.
2. Start connection: stdio process or HTTP/SSE client.
3. Pull tool list: names, descriptions, schemas.
4. Expose to Agent: affected by role, tool_search, HITL.
5. Execute: validate args, call, monitor.
6. Recover: handle process/network failure.
7. Stop/delete: remove runtime and config.

Debug by locating the failed step.

## Tool Naming

Good names are stable, specific, and action-object oriented:

```text
burp_send_to_repeater
asset_lookup_domain
cloud_list_public_buckets
```

Avoid:

```text
run
execute
scan
tool1
```

Specific names improve tool_search and reduce misuse.

## Security Review

Before connecting an external MCP, ask:

- Can it read/write local files?
- Can it execute commands?
- What network does it access?
- Does it send data to third parties?
- Are tool descriptions trustworthy?
- Can output contain prompt injection?
- Should it run under a separate OS user or container?

## Source Anchors

- External manager: `internal/mcp/external_manager.go`
- Recovery: `internal/mcp/connection_recovery.go`
- Tool adapter: `internal/einomcp/mcp_tools.go`
- Handler: `internal/handler/external_mcp.go`
- Invoke notification: `internal/einomcp/tool_invoke_notify.go`

## Parallel Search MCP

[Parallel Search MCP](parallel-search.md) provides an opt-in, keyless public web research configuration example.
