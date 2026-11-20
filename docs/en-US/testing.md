# Testing Guide

[中文](../zh-CN/testing.md)

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

## Docker build and test environment

From the repository root:

```bash
docker build -t cyberstrikeai-checks .
docker run --rm --network none cyberstrikeai-checks
```

The Dockerfile provides Go 1.25, Node.js 22, and the CGO compiler needed by SQLite. Building the image installs locked dependencies, checks and compiles the Pi runtime, and compiles all Go packages. Running it executes the existing Pi, frontend JavaScript, and Go test suites; it does not start the application server. Tests use local fixtures and need no AI API keys. Privileged cgroup fixtures and Java plugin tests require their separate CI environments.

To run a focused check, override the default command:

```bash
docker run --rm --network none cyberstrikeai-checks go test ./internal/config
```

Builds need network access to download base images and dependencies. Tests run against the source copied at build time; rebuild after editing it. `.dockerignore` excludes local configuration, credentials, runtime data, and generated files from the image.

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
