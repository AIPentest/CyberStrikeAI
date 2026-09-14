# API Recipes

[中文](../zh-CN/api-recipes.md)

Common API workflows for scripts and plugins. Use `/api-docs` and `/api/openapi/spec` for complete schemas.

## Recipe 1: Login and Validate

```bash
curl -k https://127.0.0.1:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"password":"<password>"}'
```

Use:

```text
Authorization: Bearer <token>
```

Validate:

```bash
curl -k https://127.0.0.1:8080/api/auth/validate \
  -H "Authorization: Bearer <token>"
```

## Recipe 2: Create Conversation and Send Message

Simplest path: call Agent without pre-creating an empty conversation.

```bash
curl -k https://127.0.0.1:8080/api/eino-agent \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"message":"Run authorized basic read-only recon against 127.0.0.1"}'
```

If you need an empty conversation first:

```bash
curl -k https://127.0.0.1:8080/api/conversations \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"title":"Web Test"}'
```

Then pass `conversationId` to the Agent request.

## Recipe 3: Stream Agent Output

```bash
curl -k -N https://127.0.0.1:8080/api/eino-agent/stream \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"message":"Summarize current project facts and propose read-only next steps"}'
```

Notes:

- `-N` disables curl buffering.
- reverse proxy buffering must also be disabled.
- wait for `done`.

## Recipe 4: Multi-Agent

```bash
curl -k -N https://127.0.0.1:8080/api/multi-agent/stream \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "message":"Run a staged authorized Web security test; plan first, execute read-only steps",
    "orchestration":"plan_execute"
  }'
```

Options:

- `deep`
- `plan_execute`
- `supervisor`

## Recipe 5: Upload Attachment

```bash
curl -k https://127.0.0.1:8080/api/chat-uploads \
  -H "Authorization: Bearer <token>" \
  -F "file=@./request.txt"
```

Upload large files and reference them in messages instead of pasting raw content.

## Recipe 6: Create Vulnerability

```bash
curl -k https://127.0.0.1:8080/api/vulnerabilities \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "title":"Example SQL Injection",
    "severity":"high",
    "target":"https://example.com/item?id=1",
    "description":"Parameter id has verified SQL injection",
    "evidence":"read-only validation output...",
    "remediation":"Use parameterized queries"
  }'
```

Check OpenAPI for exact fields.

## Recipe 7: Search Knowledge Base

```bash
curl -k https://127.0.0.1:8080/api/knowledge/search \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "query":"How to infer SQL injection column count",
    "riskType":"SQL Injection",
    "topK":5,
    "threshold":0.4
  }'
```

If empty, check categories first.

## Recipe 8: External MCP Status

```bash
curl -k https://127.0.0.1:8080/api/external-mcp/stats \
  -H "Authorization: Bearer <token>"
```

If service is running but Agent cannot find tools, check role constraints and `tool_search`.

## Recipe 9: Tool Schema

```bash
curl -k https://127.0.0.1:8080/api/config/tools/nmap/schema \
  -H "Authorization: Bearer <token>"
```

Scripts should build args from schema rather than guessing field names.

## Recipe 10: Export Audit Logs

```bash
curl -k "https://127.0.0.1:8080/api/audit/logs/export" \
  -H "Authorization: Bearer <token>" \
  -o audit.csv
```

Exported logs may contain sensitive operational data. Store encrypted.

## Recipe 11: Bulk Import Assets

Create `assets.json`:

```json
{
  "source": "api-import",
  "source_query": "cmdb-export-2026-07",
  "assets": [
    {
      "domain": "app.example.com",
      "port": 443,
      "protocol": "https",
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

Submit it:

```bash
curl -k https://127.0.0.1:8080/api/assets/import \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  --data-binary @assets.json
```

Example response:

```json
{"created":2,"updated":0,"skipped":0}
```

Notes:

- The caller needs `asset:write`.
- Each asset requires at least one of `host`, `ip`, or `domain`.
- One request supports up to 100,000 assets. For large payloads, use a file with `--data-binary` instead of embedding JSON in the command line.
- An existing “IP + domain + port + protocol” (or host when both IP and domain are missing) is merged and counted in `updated`.
- To work from XLSX/CSV, use **Asset Inventory → Bulk Import** in the Web UI. The API itself accepts JSON rather than multipart files.
