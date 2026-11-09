# Audit and Monitoring

[Chinese](../zh-CN/audit-and-monitoring.md)

CyberStrikeAI has separate observability streams:

- Audit: who performed platform management actions.
- Monitor: how tool executions ran.
- HITL logs: why a tool call was approved, edited, or rejected.
- Process details: how an Agent chained reasoning, tools, and outputs.

Use them together during review.

## Audit

Config:

```yaml
audit:
  enabled: true
  retention_days: 15
  max_detail_bytes: 8192
```

Endpoints:

- `GET /api/audit/meta`
- `GET /api/audit/summary`
- `GET /api/audit/logs`
- `GET /api/audit/logs/:id`
- `GET /api/audit/logs/export`

Watch for login failures, password changes, config updates, external MCP changes, WebShell/C2 actions, and HITL rejections.

## Tool Monitoring

Config:

```yaml
monitor:
  retention_days: 90
```

Endpoints:

- `GET /api/monitor`
- `GET /api/monitor/execution/:id`
- `POST /api/monitor/execution/:id/cancel`
- `GET /api/monitor/stats`
- `GET /api/monitor/calls-timeline`

Monitoring is for execution state, duration, cancellation, and result review. It is not a substitute for platform audit.

## Retention Guidance

Security-tool logs can include targets, paths, commands, and sensitive outputs. Longer retention is not always safer.

- Short engagements: 15-30 days.
- Continuous red-team platform: 90-180 days.
- Compliance archive: export and encrypt.

## Review Checklist

Weekly:

- failed logins and unusual IPs;
- config changes;
- long-running or frequently failing tools;
- external MCP state;
- DB size and disk.

After engagement:

- export required evidence;
- delete stale WebShell/C2 resources;
- clean uploads and temporary workspaces;
- archive reports, vulnerabilities, and project facts.

## Source Anchors

- Audit service: `internal/audit/service.go`
- Sanitization: `internal/audit/sanitize.go`
- Retention: `internal/audit/retention.go`
- Audit handler: `internal/handler/audit.go`
- Monitor: `internal/monitor/reconcile.go`
- Monitor handler: `internal/handler/monitor.go`
- HITL logs: `internal/handler/hitl_logs.go`
