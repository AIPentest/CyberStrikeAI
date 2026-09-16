# Contributing Guide

[中文](../zh-CN/contributing-guide.md)

This guide defines baseline expectations when adding features, APIs, tools, frontend pages, or docs.

## Principles

- New features need documentation.
- New APIs need OpenAPI updates.
- New frontend text needs zh-CN and en-US i18n.
- New config must state hot-apply behavior.
- New high-risk tools must define HITL policy.
- New DB fields must be compatible with old databases.
- New long-running tasks need state, cancellation, or recovery strategy.

## New API Checklist

- Handler validates parameters.
- Error response has stable `error` and readable `message`.
- Endpoint is authenticated unless it is an explicit platform callback.
- Mutations write audit events.
- Long tasks write monitoring/task state.
- `internal/handler/openapi.go` updated.
- API docs or recipes updated.
- Handler tests added.

## New Config Checklist

- Field exists in `config.Config`.
- `config.yaml` sample has comments.
- Safe default when omitted.
- Old configs still start.
- Hot-apply behavior documented.
- Web settings do not delete unknown fields.
- Security docs updated if high-risk capability is affected.

## New Tool Checklist

For YAML tools and Go MCP tools:

- stable and specific tool name;
- searchable `short_description`;
- explicit input schema, not one raw `cmd`;
- readable and stable output;
- controlled timeout and error path;
- high-risk operation not globally allowlisted;
- docs explain use case and risk.

## New Frontend Page Checklist

- Reuse `apiFetch`, modal, notifications, and existing state patterns.
- Add all visible text to `zh-CN.json` and `en-US.json`.
- Include loading, empty, and error states.
- Confirm destructive/high-risk actions.
- Avoid overflow in long English labels.
- Browser console clean.

## DB Change Checklist

- Migration is idempotent.
- Old DB upgrades.
- Defaults are safe.
- Large indexes are deliberate.
- Empty DB and old DB tested.
- Release notes mention backup.

## High-Risk Capability Checklist

High-risk includes Shell, WebShell, C2, external MCP write/execute, credential access, and bulk scanning.

Answer:

- Who can call it?
- Does it require HITL?
- What is audited?
- How can it be cancelled?
- How is cleanup done?
- How can it be disabled?
- Is it off by default?

## Documentation Requirements

Each important feature should document:

- purpose;
- config;
- workflow;
- risk boundary;
- troubleshooting;
- source anchors.

Chinese and English docs must have matching filenames:

```text
docs/zh-CN/
docs/en-US/
```

Update:

- `docs/README.md`
- `docs/zh-CN/README.md`
- `docs/en-US/README.md`

Before submitting documentation changes, verify local links, fenced code blocks, bilingual filename parity, locale index coverage, and the Go version documented by the root READMEs. Keep versioned examples derived from authoritative files such as `go.mod` and `config.example.yaml` whenever possible.

## Review Focus

Prioritize:

- behavior regressions;
- security boundaries;
- old data compatibility;
- error handling;
- test gaps;
- docs and OpenAPI sync.
