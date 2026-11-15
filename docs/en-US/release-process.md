# Release Process

[中文](../zh-CN/release-process.md)

Use this guide for maintainers and operators preparing upgrades or releases.

## Pre-Release Checklist

- README and docs updated.
- `config.yaml` sample includes new fields.
- OpenAPI includes new endpoints.
- i18n updated when frontend text changed.
- Security docs updated for high-risk capabilities.

## Release Risk Tiers

| Change | Risk | Must test |
| --- | --- | --- |
| Docs/assets | low | links/rendering |
| Frontend | medium | login, page states, API errors |
| Handler/API | medium | OpenAPI, auth, errors |
| Config struct | high | old config compatibility, ApplyConfig |
| DB schema | high | old DB migration, rollback |
| Agent/MCP/HITL | high | tools, approvals, streaming |
| C2/WebShell/Terminal | critical | authorized lab, audit, disable switch |

Release notes should call out risk, not just features.

## Config Compatibility

New fields should:

- have safe defaults;
- allow old configs to start;
- be documented in sample `config.yaml`;
- not cause Web settings to delete unknown fields;
- be tested via restart and hot-apply paths.

Avoid default-enabling high-risk capabilities.

## Database Changes

SQLite migrations must be:

- compatible with old versions;
- idempotent after interruption;
- careful with nullable/default fields;
- mindful of large indexes and locks;
- documented with backup instructions.

## Build and Test

```bash
go test ./internal/...
go test ./cmd/...
bash scripts/build-pi-runtime.sh
go build -o cyberstrike-ai ./cmd/server
```

Manual smoke:

```text
login -> model test -> new chat -> tools -> HITL -> KB -> external MCP -> C2 enable/disable
```

## Rollback

Restore binary/code, `config.yaml`, and `data/` together. If a new version changed DB schema, replacing only the binary is not a reliable rollback.
