# Pi token usage monitoring

Pi reports usage; Go accumulates completed model turns and emits `agent_usage_summary` into the existing database and timeline. Local statistics are not a provider billing statement and may not cover every auxiliary HTTP request.

Prompt tokens are Pi input + cacheRead + cacheWrite. Completion tokens are output, and total is prompt + completion. Cache details are already included and must not be added again. The usage view shows provider-reported reasoning tokens as a subset of completion tokens. Model-call counts reflect observed turns. Missing reasoning/cache details mean unreported data, not proven zero use. Durable assessments retain their own conservative budget ledger.

The dashboard and Settings usage view retain period, model and project filters. APIs are `GET /api/usage/tokens` and `GET /api/conversations/:id/token-usage`. Existing usage rows and historical timeline backfill remain compatible.

Streaming updates carry deltas; complete messages are committed at message end. Implementation: `internal/multiagent/pi_runner.go` and `internal/database/model_token_usage.go`. See [Pi AI](https://github.com/earendil-works/pi/tree/v1.5.2/packages/ai).

## DeepSeek

In the channel’s reasoning settings, use `off` for simple Q&A and format conversions or pair `auto` with `low` to keep reasoning on at a lower effort. The official API defaults to `high` when effort is unspecified and maps `medium` to `high`. The channel sends its per-request output limit as `max_tokens`, shared by reasoning and the visible answer. Compare output tokens, reasoning tokens and the cache hit rate for the same tasks at different effort levels. [DeepSeek Thinking Mode](https://api-docs.deepseek.com/guides/thinking_mode/) · [Chat Completions](https://api-docs.deepseek.com/api/create-chat-completion/)

The official API automatically caches reusable request prefixes. Pi orders MCP tool definitions by name so the same catalog introduces fewer prefix changes across requests. The provider reports the actual cache hits. [DeepSeek Context Caching](https://api-docs.deepseek.com/guides/kv_cache/)

Input savings activate in the background for the official DeepSeek endpoint, explicit `deepseek` / `deepseek_compat` reasoning profiles, and model names containing `deepseek` (case-insensitive). At 16 tools, native discovery keeps six catalog entries plus core tools and explicit always-visible tools available. Other builtins load on demand. With authorized file access, results over 8,000 bytes become head/tail previews backed by the full output. Tighter configured limits take precedence. Valid JSON tool results lose formatting whitespace, including when replaying older sessions. Strings and numeric lexemes retain their original text. Existing configuration-file overrides remain supported. Compare complete tasks, including discovery/readback calls and evidence quality, to measure actual savings.
