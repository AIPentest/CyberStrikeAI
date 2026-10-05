# Parallel Search MCP

[中文](../zh-CN/parallel-search.md)

Use Parallel Search MCP to research public documentation and advisories from a CyberStrikeAI conversation. The free, keyless endpoint provides `web_search` for queries and `web_fetch` for relevant excerpts from specific pages. It uses the existing [External MCP federation](mcp-federation.md) over Streamable HTTP.

## Configuration

The [YAML example](../examples/parallel-search.yaml) is a configuration fragment, not a replacement for your application config. Copy only `parallel-search` into `external_mcp.servers` in your own `config.yaml`, preserving existing servers and settings. It starts disabled. To opt in, set `disabled: false` and restart CyberStrikeAI. File edits are applied at startup; this example does not change default tools or saved provider choices.

No API key, Authorization header, environment variable, local MCP process, or additional package is needed. The `User-Agent` identifies the CyberStrikeAI MCP client. `timeout: 60` bounds HTTP requests. The existing Agent tool timeout, execution monitoring, cancellation, role restrictions, tool enable controls, and HITL policy still apply.

To stop using it, use Settings → External MCP → Stop, or set `disabled: true` in your config and restart. Stop applies to the current connection immediately. To keep the server disabled across restarts, use `disabled: true`; the loader enables a server when this field is false or absent. If you edit an already connected server through the UI, stop and start it to reconnect with the new settings.

## Workflow

1. Enable the server and confirm its connected status in Settings → External MCP.
2. Confirm `web_search` and `web_fetch` appear in the tool list. Their tool keys are `parallel-search::web_search` and `parallel-search::web_fetch`; include these keys if your role restricts tools.
3. Ask: “Find the official OWASP guidance on preventing SQL injection. Summarize parameterized queries and cite the source.”
4. Use the search excerpts for the answer. Fetch a specific source only when those excerpts are insufficient. Check cited sources before relying on the answer.
5. Inspect MCP monitoring for execution results or errors. Tool discovery, enabled tools, and approval depend on your current role and policy.

Queries, requested URLs, and fetched content go to Parallel. Use public information; do not include credentials, private reports, or internal target details. Remote pages and tool descriptions are untrusted input. This configuration grants no local shell or file access and adds no automatic approvals. See the [security model](security-model.md) and [HITL guide](hitl-best-practices.md).

## Troubleshooting and validation

- No tools: check `disabled`, server status, role tool keys, and per-tool enable settings.
- Connection error: check HTTPS access to `search.parallel.ai` and use `type: http`, not `sse`.
- Rate limit or remote error: inspect the tool result, wait before retrying, or disable the server. Free access can be rate limited.
- Timeout: inspect the HTTP timeout and Agent tool timeout; avoid requesting full pages when excerpts suffice.

The local test uses a fake HTTP MCP server and exercises the example through the config loader, external manager, Agent tool selection, and Eino tool bridge, then produces a sourced final answer through the ADK runner with a deterministic model fixture:

```bash
go test ./internal/einomcp -run TestParallelSearchExample -count=1
```

An optional live check makes public search and fetch requests without a Parallel key:

```bash
CYBERSTRIKE_PARALLEL_LIVE=1 go test ./internal/einomcp -run TestParallelSearchLive -count=1 -v
```

The live check requires network access and can fail when the service is unavailable or rate limited. It does not call your configured AI provider.

## Source anchors

- [Config loader and server fields](../../internal/config/config.go)
- [HTTP MCP client](../../internal/mcp/client_sdk.go)
- [External manager and execution monitoring](../../internal/mcp/external_manager.go)
- [Agent tool selection](../../internal/agent/agent.go)
- [Eino MCP bridge](../../internal/einomcp/mcp_tools.go)
