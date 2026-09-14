# API Reference

[中文](../zh-CN/api-reference.md)

CyberStrikeAI exposes built-in OpenAPI docs:

```text
/api-docs
GET /api/openapi/spec
```

The OpenAPI spec is protected to avoid exposing the API surface to unauthenticated users.

## Authentication

Login:

```http
POST /api/auth/login
Content-Type: application/json

{"password":"your-password"}
```

The auth middleware accepts token from:

1. `Authorization: Bearer <token>`
2. `Authorization: <token>`
3. `?token=<token>`
4. `auth_token` cookie

Prefer `Authorization: Bearer` for scripts. Query tokens can leak through logs.

## Agent APIs

Single-agent:

- `POST /api/eino-agent`
- `POST /api/eino-agent/stream`

Multi-agent:

- `POST /api/multi-agent`
- `POST /api/multi-agent/stream`

`orchestration` may be `deep`, `plan_execute`, or `supervisor`.

Common request body fields:

| Field | Meaning |
| --- | --- |
| `message` | User message, required. |
| `conversationId` | Continue an existing conversation; empty creates a new one. |
| `projectId` | Project for a new conversation; empty may follow `config.project.default_project_id`. |
| `role` | Use a named role. |
| `aiChannelId` | Select a channel from `ai.channels`; empty follows `ai.default_channel`. |
| `reasoning` | Per-session reasoning override, controlled by the channel's `reasoning.allow_client_reasoning`. |
| `hitl` | Per-session human-in-the-loop settings. |

## SSE Notes

Streaming endpoints are long-lived. Clients should:

- handle `error` events;
- wait for `done`;
- avoid blindly replaying destructive requests;
- disable proxy buffering;
- pass `conversationId` when continuing a conversation.

## File Management Sources

The file management page and `GET /api/chat-uploads` group conversation-related files by source. Directory names still use project IDs or conversation IDs for stability, while the UI prefers project names or conversation titles and keeps the full ID available in tooltips or copied paths.

| Source | `source` | Typical directory | Meaning | Mutability |
| --- | --- | --- | --- | --- |
| Workspace files | `workspace` | `tmp/workspace/projects/<projectId>/...`, `tmp/workspace/conversations/<conversationId>/...` | The Agent workspace for downloaded files, analysis scripts, intermediate results, and generated CSV/XLSX/Markdown files. If an AI-generated file is missing from the UI, check this source first. | Read-only listing; supports copy path, download, and export. |
| Conversation artifacts | `conversation_artifact` | `data/conversation_artifacts/<conversationId>/...` | Conversation-scoped deliverables or archived artifacts such as summaries, reports, or middleware-generated artifacts. | Read-only listing; supports copy path, download, and export. |
| Tool outputs | `reduction` | `tmp/reduction/projects/<projectId>/...`, `tmp/reduction/conversations/<conversationId>/...` | Persisted full tool outputs, scan raw data, or outputs saved before truncation. Useful for reviewing long command or scan results. | Read-only listing; supports copy path, download, and export. |
| Chat uploads | `upload` | `chat_uploads/<date>/<conversationId>/...` | Files manually uploaded in chat or from the file management page. Copy the server absolute path into chat when the AI should reference a file. | Supports upload, mkdir, text edit, rename, delete, copy path, download, and export. |

Related endpoints:

- `GET /api/chat-uploads`: list files filtered by source, project, conversation, or filename.
- `GET /api/chat-uploads/path`: resolve a file-management relative path or internal virtual path to a server absolute path for copy actions.
- `GET /api/chat-uploads/download`: download a file.
- `GET /api/chat-uploads/export`: export the current filtered result as a ZIP.
- `POST /api/chat-uploads`: upload into the chat uploads directory.

## Asset Management and Bulk Import

Asset endpoints:

- `GET /api/assets`: list and filter assets;
- `GET /api/assets/selection`: resolve cross-page selection from the current filters, up to 10,000 rows;
- `GET /api/assets/stats`: retrieve statistics; `days` accepts only `7`, `30`, or `90`;
- `POST /api/assets/import`: create or deduplicate and update up to 100,000 assets;
- `POST /api/assets/scan-links`: record up to 10,000 scan links;
- `PUT /api/assets/bulk`: atomically update up to 10,000 assets;
- `PUT /api/assets/project-binding`: bind up to 10,000 asset IDs to a project;
- `POST /api/assets/batch-delete`: atomically delete up to 10,000 assets;
- `POST /api/assets/merge`: merge 2-100 duplicate assets with a shared identity;
- `PUT /api/assets/:id`: update an asset;
- `DELETE /api/assets/:id`: delete an asset.

`GET /api/assets` and `GET /api/assets/selection` share filters and sorting. `selection` ignores pagination and returns all matching rows, up to 10,000:

| Category | Parameters |
| --- | --- |
| Pagination (list only) | `page`, `page_size` (maximum: 100) |
| Common | `q`, `status`, `project_id`, `risk_level`, `min_vulnerabilities`, `max_vulnerabilities` |
| Target and source | `host`, `ip`, `domain`, `port`, `protocol`, `source`, `tag` |
| Responsibility and business | `responsible_person`, `department`, `business_system`, `environment`, `criticality` |
| Location | `country`, `province`, `city` |
| Scan | `scan_state=never|scanned`, `scan_overdue_days`, `last_scan_before`, `last_scan_after` |
| Discovery time | `first_seen_before`, `first_seen_after`, `last_seen_before`, `last_seen_after` |
| Sort | `sort_by`, `sort_order=asc|desc` |

Time parameters accept RFC3339 or `YYYY-MM-DD`. Supported `sort_by` values are `last_seen_at`, `last_scan_at`, `first_seen_at`, `created_at`, `updated_at`, `host`, `port`, `risk_level`, and `vulnerability_count`.

`POST /api/assets/import` accepts JSON, not an XLSX/CSV upload. The Web UI parses the template in the browser, previews it, and converts valid rows to this request:

```http
POST /api/assets/import
Authorization: Bearer <token>
Content-Type: application/json

{
  "source": "manual-import",
  "source_query": "asset-import-2026-07.xlsx",
  "assets": [
    {
      "host": "https://app.example.com:443",
      "domain": "app.example.com",
      "port": 443,
      "protocol": "https",
      "title": "Example App",
      "server": "nginx",
      "project_id": "<project-id>",
      "responsible_person": "Alice",
      "department": "Security",
      "business_system": "Customer Portal",
      "environment": "production",
      "criticality": "critical",
      "tags": ["production", "internet"],
      "status": "active"
    },
    {
      "ip": "192.0.2.10",
      "port": 22,
      "protocol": "ssh",
      "status": "active"
    }
  ]
}
```

Request rules:

- `assets` must contain between 1 and 100,000 entries;
- at least one of `host`, `ip`, or `domain` must be non-empty for each asset;
- `port` must be between `0` and `65535`;
- `status` must be `active` or `inactive`;
- `environment` may be empty or `production`, `staging`, `testing`, `development`, or `other`;
- `criticality` may be empty or `critical`, `high`, `medium`, or `low`;
- an asset may have up to 30 tags, each no longer than 64 characters;
- a non-empty `project_id` must reference a project accessible to the caller;
- the caller needs `asset:write`;
- the server deduplicates by “IP + domain + port + protocol” (or host when both IP and domain are missing) and processes the request in one transaction.

Successful response:

```json
{
  "created": 120,
  "updated": 8,
  "skipped": 2
}
```

`created` counts new records, `updated` counts deduplicated merges, and `skipped` counts empty or inaccessible existing records. Validation errors return `400` with the failing asset position in `error`; inaccessible projects return `403`. See [Asset Management](asset-management.md#import-from-a-spreadsheet) for the template and UI workflow.

Bulk edit example:

```http
PUT /api/assets/bulk
Content-Type: application/json

{
  "asset_ids": ["<asset-id-1>", "<asset-id-2>"],
  "responsible_person": "Alice",
  "department": "Security",
  "environment": "production",
  "criticality": "high",
  "add_tags": ["internet-facing"],
  "remove_tags": ["untriaged"]
}
```

All patch fields are optional; omitted fields retain their current values. `add_tags` and `remove_tags` are deduplicated inside the transaction. Bulk edit, project binding, and batch deletion validate access to every requested asset first, so a missing or inaccessible ID fails the entire operation.

Duplicate merge example:

```http
POST /api/assets/merge
Content-Type: application/json

{
  "asset_ids": ["<primary-id>", "<duplicate-id>"],
  "primary_id": "<primary-id>"
}
```

Every record being removed must share a domain, IP address, or Host with the primary asset. Existing primary values win, empty fields are filled from the other records, and tags are unioned. The caller needs permission to update the primary and delete the other assets.

## Stability Tiers

| API type | Stability | Recommendation |
| --- | --- | --- |
| `/api/auth/*` | high | safe to integrate |
| `/api/eino-agent*` | high | preferred chat entry |
| `/api/openapi/spec` | high | client generation |
| `/api/assets/*` | high | asset management and bulk import |
| `/api/config*` | medium | admin automation only |
| `/api/c2/*`, `/api/webshell/*` | medium | high-risk, restrict access |
| frontend private calls | low | avoid plugin dependency |

## Common Areas

- Conversations: `/api/conversations`
- Projects/facts: `/api/projects`
- Assets and bulk import: `/api/assets`
- Vulnerabilities: `/api/vulnerabilities`
- Knowledge: `/api/knowledge/*`
- Roles: `/api/roles`
- Skills: `/api/skills`
- External MCP: `/api/external-mcp`
- Monitoring: `/api/monitor`
- Audit: `/api/audit`
- C2: `/api/c2`
- WebShell: `/api/webshell`

## Curl Example

```bash
curl -k https://127.0.0.1:8080/api/conversations \
  -H "Authorization: Bearer <token>"
```

```bash
curl -k https://127.0.0.1:8080/api/eino-agent \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"message":"Run authorized basic recon against 127.0.0.1; avoid high-risk actions."}'
```

## Source Anchors

- Routes: `internal/app/app.go`
- Auth middleware: `internal/security/auth_middleware.go`
- OpenAPI: `internal/handler/openapi.go`
- Single-agent: `internal/handler/eino_single_agent.go`
- Multi-agent: `internal/handler/multi_agent.go`
- Asset endpoints: `internal/handler/asset.go`
- Asset storage and deduplication: `internal/database/asset.go`
