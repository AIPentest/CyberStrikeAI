# WebShell Management

[Chinese](../zh-CN/webshell.md)

WebShell management stores authorized WebShell connections and allows command/file operations through the UI and Agent tools.

## Workflow

1. Add a connection.
2. Fill URL, parameter/password, and metadata.
3. Test connectivity.
4. Run read-only identification commands first.
5. Let AI assist only after selecting the correct connection.

Connections are stored in SQLite.

## Operation Tiers

| Tier | Operation | Risk | Guidance |
| --- | --- | --- | --- |
| Identify | `whoami`, `pwd`, OS version | low | may automate |
| Enumerate | dirs, processes, env vars | medium | constrain path/command |
| Read | config, logs, source | medium-high | human confirms sensitivity |
| Write/execute | write, run script, delete | high | human approval and rollback |

Having a WebShell does not make follow-up operations low risk.

## Naming

Use:

```text
<project>-<environment>-<target>-<privilege>-<date>
```

Example:

```text
acme-staging-web01-www-20260707
```

Avoid vague names like `test`, `shell1`, or `customer machine`.

## AI Guardrail Prompt

```text
Before using WebShell, confirm connection_id, target name, current directory, and privilege. Default to read-only commands. Any write, delete, upload, permission change, persistence, credential access, or internal probing requires purpose, impact, rollback plan, and approval.
```

## MCP Tools

Typical tools:

- `webshell_exec`
- `webshell_file_list`
- `webshell_file_read`
- `webshell_file_write`
- connection management tools

Do not put write/execute tools in a global allowlist.

## Source Anchors

- Handler: `internal/handler/webshell.go`
- Context: `internal/handler/webshell_context.go`
- Probe: `internal/handler/webshell_probe.go`
- Encoding/OS tests: `internal/handler/webshell_encoding_test.go`, `internal/handler/webshell_os_test.go`
- Tool registration: `internal/app/app.go`
