# Testing Guide

[Chinese](../zh-CN/testing.md)

Testing CyberStrikeAI means more than running Go tests. Agent, MCP, HITL, C2, WebShell, and frontend streaming all have different failure modes.

## Commands

```bash
go test ./internal/...
go test ./cmd/...
go build -o cyberstrike-ai ./cmd/server
```

Run focused packages when working locally:

```bash
go test ./internal/multiagent
go test ./internal/handler
go test ./internal/security
```

## Test Pyramid

| Layer | Goal | Example |
| --- | --- | --- |
| Unit | pure logic | expressions, chunking, sanitization |
| Handler | HTTP behavior | validation, auth, status codes |
| Integration | module cooperation | external MCP, KB indexing, HITL |
| Smoke | user path | login, chat, tools, settings |
| Authorized lab | high-risk features | C2, WebShell, terminal |

Do not use end-to-end manual testing as a substitute for unit tests, or unit tests as a substitute for high-risk lab validation.

## Regression Focus

Expand testing when changing:

- `internal/handler/config.go`: model, KB, MCP, C2, robot apply paths;
- `internal/multiagent/`: streaming, tool calls, summarization, retry, HITL;
- `internal/security/`: auth, shell, timeout, no-output;
- `internal/database/`: old data compatibility;
- `web/static/js/chat.js`: chat, process details, attack chain, groups.

## Test Data

Avoid real customer data. Prepare:

- small Markdown KB sample;
- fake local MCP server;
- controlled local HTTP target;
- harmless WebShell simulator;
- temporary SQLite DB.

## Failure Cases

Cover:

- model API 401/429/500;
- MCP startup failure;
- tool timeout;
- HITL rejection;
- interrupted KB indexing;
- unwritable database;
- WebShell non-200 response;
- C2 disabled endpoint access.

## Source Anchors

Existing tests live across:

- `internal/handler/*_test.go`
- `internal/multiagent/*_test.go`
- `internal/workflow/*_test.go`
- `internal/knowledge/*_test.go`
- `internal/security/*_test.go`
- `internal/mcp/*_test.go`
- `internal/c2/*_test.go`
