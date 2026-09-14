# Configuration Reference

[中文](../zh-CN/configuration.md)

The main configuration file is `config.yaml`. Many fields are editable through the Web settings page, but not every field has the same hot-apply behavior.

## Core Sections

```yaml
server:
  host: 0.0.0.0
  port: 8080
  tls_enabled: true
  # Optional: other trusted Web integrations; Chromium extensions need no entry.
  # cors_allowed_origins:
  #   - https://trusted-integration.example
auth:
  session_duration_hours: 12
ai:
  default_channel: openai-main
  channels:
    openai-main:
      name: OpenAI Main
      provider: openai_compatible
      base_url: https://api.openai.com/v1
      api_key: sk-...
      model: gpt-4.1
agent:
  max_iterations: 12000
  tool_timeout_minutes: 60
```

Change the initial `admin` password from the Web UI after first login. Use HTTPS or a trusted reverse proxy in any shared environment.

Valid Chromium `chrome-extension://<32-character-extension-id>` origins are recognized automatically. The extension must still obtain host permission and authenticate with a password and Bearer token. `server.cors_allowed_origins` remains available as an exact allowlist for other trusted Web integrations; wildcards are not accepted, and changing it requires a restart.

## AI Channels

`ai` is the recommended model configuration entry. In the Web UI, use **System Settings → Basic Settings → AI Channel Configuration**. Saving that form writes `ai.default_channel` and `ai.channels`. The legacy `openai` field remains as a backward-compatible runtime field; on load, CyberStrikeAI ensures a default channel exists and synchronizes the resolved `ai.default_channel` into runtime `openai`.

```yaml
ai:
  default_channel: openai-main
  channels:
    openai-main:
      name: OpenAI Main
      provider: openai_compatible
      base_url: https://api.openai.com/v1
      api_key: sk-...
      model: gpt-4.1
      max_total_tokens: 120000
      max_completion_tokens: 16384
      reasoning:
        mode: on
        effort: high
        allow_client_reasoning: true
        profile: openai_compat
    claude-main:
      name: Claude Main
      provider: claude
      base_url: https://api.anthropic.com/v1
      api_key: sk-ant-...
      model: claude-sonnet-4-5
    orcarouter:
      name: OrcaRouter
      provider: orcarouter
      base_url: https://api.orcarouter.ai/v1
      api_key: sk-orca-...
      model: orcarouter/auto
```

| Field | Meaning |
| --- | --- |
| `ai.default_channel` | Default channel ID for new conversations and requests without an explicit channel. |
| `ai.channels.<id>` | Channel config. IDs are normalized to lowercase letters, digits, and hyphens. |
| `name` | Display name in the Web UI; falls back to the ID. |
| `provider` | `openai_compatible`, `claude`, or `orcarouter`. OpenAI-compatible channels map to runtime `openai`; Claude channels use Eino's native Anthropic Messages API component; OrcaRouter is an OpenAI-compatible gateway (`https://api.orcarouter.ai/v1`) whose model IDs such as `orcarouter/auto` are resolved by the gateway to the best available backend model. |
| `base_url/api_key/model` | Required. Base URL usually includes a version path such as `/v1`. |
| `max_total_tokens` | Shared context budget for compression, attack-chain generation, multi-agent summaries, and similar paths. |
| `max_completion_tokens` | Per-response output cap; default is used when empty. |
| `reasoning` | Default reasoning fields for the channel. Gateway support varies; try `mode: off` first when a provider rejects requests. |

The chat page reads saved channels into the “AI Channel” selector. A non-empty request `aiChannelId` selects a channel for that run/session without sending API credentials through the prompt path. Empty `aiChannelId` follows `ai.default_channel`.

Common Web UI operations:

- Add: click `+`, fill required fields, then save.
- Set default: select a channel, click **Set as default**, then save/apply.
- Copy: duplicate the current form, useful for the same provider with a different model.
- Delete: keep at least one channel; the default channel is protected from bulk delete.
- Probe: use **Test connection** or **Bulk probe** to validate API key, Base URL, and model.

## Hot-Apply Boundaries

`POST /api/config/apply` coordinates model config, tool description mode, MCP tool registration, knowledge components, robot restarts, and C2 runtime reconciliation. It does not make every field instantly effective.

| Section | Usually hot-applies | Extra action |
| --- | --- | --- |
| `ai.default_channel` / `ai.channels` | new requests use the resolved default or selected channel | running streams keep their current state; reload config for the frontend channel list |
| `openai` | compatibility field, usually synchronized from the default AI channel | prefer maintaining new config in `ai.channels` |
| `agent.max_iterations` | new tasks | existing tasks continue |
| `hitl.tool_whitelist` | new approval checks | pending approvals are not re-decided |
| `knowledge.enabled` | initializes/updates components | scan and index are still required |
| `knowledge.embedding` | updates retriever/indexer config | rebuild index for existing vectors |
| `robots` | restarts long-lived connections | platform callback settings must still match |
| `c2.enabled` | reconciles C2 runtime | verify existing listeners/sessions manually |
| `server.port/tls` | usually needs process restart | listener settings are not ordinary hot state |

## Fallback Relationships

- `vision.api_key/base_url/provider` can inherit from the resolved default AI channel.
- `hitl.audit_model` can inherit from the resolved default AI channel.
- `knowledge.embedding.base_url/api_key` can inherit from model settings.
- rerank config can inherit from embedding/openai.
- `database.knowledge_db_path` can be separate or reuse the main DB.

When debugging, inspect both the child config and the fallback parent.

## Recommended Values

| Field | Conservative | Aggressive | Decide by |
| --- | --- | --- | --- |
| `agent.tool_timeout_minutes` | 10-30 | 60+ | long scanners |
| `shell_no_output_timeout_seconds` | 300-600 | 1200+ | quiet tools |
| `knowledge.indexing.batch_size` | 5-10 | 20+ | embedding API limits |
| `knowledge.indexing.rate_limit_delay_ms` | 300-800 | 0-100 | 429 frequency |
| `retrieval.top_k` | 3-5 | 8-12 | context budget |
| `similarity_threshold` | 0.35-0.45 | 0.5+ | recall vs precision |
| `audit.retention_days` | 15-30 | 90+ | compliance and disk |

## Change Template

Before changing config, write down:

```text
Purpose:
Sections:
Expected impact:
Rollback:
Validation endpoints:
```

After changing, validate the specific subsystem rather than trusting the save message.

## Source Anchors

- Config structs: `internal/config/config.go`
- Env expansion: `internal/config/envexpand.go`
- Config API and apply: `internal/handler/config.go`
- Route registration: `internal/app/app.go`
- C2 reconciliation: `internal/app/c2_lifecycle.go`

## Diagnostic logs

Alongside `log.output` (controlled by `log.level`), warnings and errors are saved as JSON Lines in `log/diagnostic-YYYY-MM-DD.log`, using the server’s local date. This independent warn-and-above output preserves existing context, caller information, and error stack traces; ordinary info/debug records are excluded and no extra request bodies or tool output are collected.

Configure `log.diagnostic_dir` (default `log`, relative to the working directory), `log.diagnostic_retention_days` (default 14, including today; nonpositive values use the default), or `log.diagnostic_disabled: true` to disable it. Restart after changing these settings. Files are created only when a diagnostic record is written; the first write each day removes expired files matching `diagnostic-YYYY-MM-DD.log`. Cleanup does not run while no diagnostic records are written. Write failures are reported to stderr without interrupting the primary log output.
