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
```

| Field | Meaning |
| --- | --- |
| `ai.default_channel` | Default channel ID for new conversations and requests without an explicit channel. |
| `ai.channels.<id>` | Channel config. IDs are normalized to lowercase letters, digits, and hyphens. |
| `name` | Display name in the Web UI; falls back to the ID. |
| `provider` | `openai_compatible` or `claude`. OpenAI-compatible channels map to runtime `openai`; Claude channels use Eino's native Anthropic Messages API component. |
| `base_url/api_key/model` | Required. Base URL usually includes a version path such as `/v1`. |
| `max_total_tokens` | Shared context budget for compression, attack-chain generation, multi-agent summaries, and similar paths. |
| `max_completion_tokens` | Per-response output cap; default is used when empty. |
| `context_window` | Native model context window in tokens. When set, the runtime clamps `max_total_tokens` to it; 0/omitted means unknown. |
| `context_profile` | `auto` (default) or `local`. `local` targets 32K–256K local models: the budget is 60% of the window (capped at 128K), while summary reserve, tool-output truncation/clearing, user-message and ledger caps are tightened, and `tool_search` with a small always-visible tool set is enabled. It only tightens numeric middleware settings and enables tool_search/reduction where needed. |
| `reasoning` | Default reasoning fields for the channel. Gateway support varies; try `mode: off` first when a provider rejects requests. |

### Local Model Context (32K–256K)

A local model's effective context is usually much smaller than its advertised window, and the “model maximum” can differ from the runtime window. Recommended setup:

1. **Confirm the runtime window**: click **Auto-detect** next to the context window field in **System Settings → Basic Settings → AI Channel Configuration**. The probe checks Ollama `/api/ps`/`/api/show`, llama.cpp `/props`, LM Studio `/api/v0/models`, then vLLM/OpenAI-compatible `/v1/models`; reverse-proxy subpaths such as `https://host/ollama/v1` are supported. The result also shows the effective budget for the current profile. Enter the server's actual value if detection fails.
2. **Enable the local profile**: set `context_profile: local`. The budget becomes 60% of the window and compaction triggers earlier; tool-output truncation is roughly window/6 (8–32 KB), historical tool-result clearing is roughly 35% of the budget, and always-visible tools shrink to a core set; backend builtins also join the `tool_search` dynamic pool, and the rest are unlocked on demand. Token estimates are inflated by 10% to absorb tokenizer differences between local models and tiktoken. The profile also switches the output cap to the more widely accepted `max_tokens` field, because many local servers do not recognize `max_completion_tokens`; the same applies to attack-chain, FOFA query, workflow draft, and HITL audit calls.
3. **Do not plan around a nominal 1M window**: Qwen3.5-class models ship with 262,144 native context and can be extended to 1M with YaRN, but local VRAM limits and context rot degrade quality. For tool-using agents, keep the effective budget around 64K–128K.
4. **Ollama requires `num_ctx`**: Ollama's `/v1` compatibility endpoint ignores `num_ctx` in requests and picks a default window from VRAM (roughly 4K/32K/256K). Oversized prompts are truncated from the beginning, dropping the system prompt and tool definitions first. Use `OLLAMA_CONTEXT_LENGTH=65536 ollama serve` or a Modelfile `PARAMETER num_ctx 65536`, then confirm `CONTEXT` in `ollama ps` after restart.
5. **KV cache and prefix caching**:
   - llama.cpp: `--ctx-size 65536 --cache-type-k q8_0 --cache-type-v q8_0 -fa`; `--cache-prompt` is on by default. `--parallel` splits the context across slots.
   - vLLM: `--max-model-len 65536 --enable-prefix-caching --kv-cache-dtype fp8`; use `--max-num-seqs` to bound concurrent KV usage.
   - Ollama: `OLLAMA_KV_CACHE_TYPE=q8_0`, `OLLAMA_NUM_PARALLEL=1`; larger windows consume more KV cache.
   - LM Studio: set Context Length explicitly in the model settings and enable KV-cache quantization when available.
6. **Thinking models**: Qwen3.5-class models enable thinking by default, which burns context inside tool loops. Disable it per channel with `reasoning.extra_request_fields.chat_template_kwargs.enable_thinking: false` (supported by vLLM and llama.cpp with `--jinja`). `reasoning.mode: off` only clears OpenAI/DeepSeek reasoning fields and may not disable the local chat template's thinking.
7. **Minimum viable window**: an agent with 90+ tools is unreliable below 16K. 32K depends on `tool_search` and `context_profile: local`; 64K and above is closer to a cloud-model experience.

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
- `hitl.audit_backend` chooses `openai` (default) or `typesafe` (TypeSafe Jev).
- `hitl.audit_model` can inherit from the resolved default AI channel when `audit_backend` is `openai`. TypeSafe keys are never inherited.
- `hitl.audit_agent_prompt` is a chat system prompt on `openai`, and a Jev `operatorPolicy` overlay on `typesafe`. The built-in default prompt is not copied into Jev state.
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

## Runtime storage cleanup

```yaml
storage:
  auto_clean: false
  interval_minutes: 60
  orphan_grace_days: 1
  active_grace_hours: 24
  categories:
    workspace:
      enabled: true
      retention_days: 30
```

Reclaims disk space used by runtime artifacts. Manage it from **System settings → Storage cleanup**, or trigger it with `POST /api/storage/cleanup`.

- `auto_clean` is off by default: upgrading never deletes existing data behind an administrator's back. Manual preview and cleanup still work while it is off.
- `interval_minutes` is the background sweep interval (floor 5). `orphan_grace_days` is the minimum age before a directory whose conversation/project no longer exists is reclaimed. Sessions active within `active_grace_hours` are always skipped.
- A category `retention_days` of 0 disables age-based cleanup, but **orphaned directories are still reclaimed** — a directory whose session is gone has no retention value.
- Deletion is irreversible. The API requires `dry_run=false` together with `confirm=true` for a real cleanup; omitting `dry_run` is treated as a preview.

| Category | Default retention | Target directory |
| --- | --- | --- |
| `workspace` | 30 days | agent workspaces, `tmp/workspace` |
| `reduction` | 7 days | oversized tool-output spill, `tmp/reduction` |
| `conversation_artifacts` | 30 days | summaries and user-input ledger, `data/conversation_artifacts` |
| `plantask` | 30 days | multi-agent plan boards, `skills/.eino/plantask` |
| `c2_artifacts` | 30 days | C2 results/uploads/downstream/payloads, `tmp/c2` (cleaning a payload invalidates its download link) |
| `chat_uploads` | 90 days | conversation attachments, `chat_uploads` |
| `workflow_checkpoints` | 7 days | workflow run checkpoints, `data/workflow-checkpoints` |
| `diagnostic_logs` | 14 days | diagnostic logs, `log/diagnostic-*.log` |

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
