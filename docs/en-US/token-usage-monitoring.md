# Token usage monitoring: research and implementation

[中文](../zh-CN/token-usage-monitoring.md)

Research date: 2026-10-11.

## Research findings

| Reference | Official capability | Design adopted by this project |
| --- | --- | --- |
| [ZCode usage statistics](https://zcode.z.ai/cn/docs/usage-stats) | Separates local session statistics from remote coding-plan quotas; supports date ranges, daily trends, and model shares | A local usage view with 7/30/90-day ranges, daily trends, model shares, and details; initially designed as an expandable dashboard panel |
| [DeepSeek context caching](https://api-docs.deepseek.com/guides/kv_cache/) | Response usage distinguishes input cache hits and misses with `prompt_cache_hit_tokens` / `prompt_cache_miss_tokens` | Map cache hits to the standard OpenAI cache-read field; do not map misses to cache writes |
| [Eino v0.9.21](https://github.com/cloudwego/eino/releases/tag/v0.9.21), [schema PR #1309](https://github.com/cloudwego/eino/pull/1309) | The stable release adds `PromptTokenDetails.CacheWriteTokens`, retaining cache reads, input, output, and reasoning fields | Carry the stable standard fields through streaming merges, run summaries, the database, and UI |
| [Eino-ext PR #1015](https://github.com/cloudwego/eino-ext/pull/1015) | Model adapters forward cache-write counts | Upgrade agenticclaude to 0.1.7, agenticopenai to 0.2.4, and the corresponding OpenAI ACL, rather than upgrading only the framework |
| [Eino callbacks and tracing](https://www.cloudwego.io/docs/eino/quick_start/chapter_06_callback_and_trace/) | Component callbacks can collect model lifecycle, usage, and duration data | Retain existing run-event collection; covering every internal call would require a separate unified model-call ledger |

Versions were checked through Go module resolution and local source inspection; the 0.10 alpha was not adopted. Raw DeepSeek fields do not automatically enter Eino's standard cache-read count, so the project's existing HTTP compatibility layer supplies the mapping for ordinary JSON and SSE streaming responses. Existing standard `cached_tokens` takes precedence. Unknown fields are preserved as RawMessage to avoid loss of numeric precision through floating-point conversion.

## Metric definitions

- `promptTokens`: all input reported by the model.
- `completionTokens`: all output, including reasoning counted as output by the provider.
- `totalTokens`: the provider's total; falls back to input plus output when absent.
- `cachedTokens`: the cache-read portion of input.
- `cacheWriteTokens`: the cache-write portion of input.
- `reasoningTokens`: the reasoning portion of output.
- Cache reads, cache writes, and reasoning are breakdowns and must not be added to the total again. The cache-read share of input is `cachedTokens / promptTokens`; a dash is shown when there is no input.
- A streaming model output may report cumulative counts multiple times. The merge retains the maximum for each field and adds the output to the run summary only once when it ends. Usage already received can be retained even if the output subsequently fails.

DeepSeek cache misses are not equivalent to Claude cache writes. Text length cannot establish billing-grade counts, and an absent field does not prove that the provider used no cache. New fields on older records default to 0, meaning that the breakdown was not recorded.

## Entry points and data flow

Click the dashboard **Token Usage** card to open **System Settings → Token Usage**, or open the section directly from the settings submenu. The dashboard card shows total usage for the last seven days; its subtitle shows only the cache-hit rate (cache-read tokens / input tokens). Call counts and daily usage remain on the detail page. The detail page supports the current project scope, 7/30/90-day ranges, eight metrics, up to 20 model-ranking entries grouped by the run's configured model, daily trends and full daily details, and up to 20 recent runs. It loads independently, supports manual refresh and request cancellation, and does not refresh the whole dashboard when the range changes. Reloading the page retains the settings section.

The detail page separates four primary metrics (total, input, output, and cache-read share of input) from four secondary metrics (calls, cache reads, cache writes, and reasoning). Trends switch between total tokens, model calls, and cache-hit rate. Hover over or keyboard-focus a bar to see the date and exact value; switching chart metrics does not request the API again. The cache donut separates cache reads from remaining input instead of adding cache or reasoning to the total. Model details include share bars; daily numeric details are collapsed by default; recent runs distinguish complete and partial records. Metric definitions can be expanded.

Daily charts use the server dates returned by the API and fill only gaps between returned dates; they do not guess the query endpoint from the browser timezone. Charts and wide tables scroll horizontally within their own areas on small screens. No records and unavailable statistics have distinct empty and unavailable states.

### UI research basis (2026-10-12)

| Reference | Design adopted |
| --- | --- |
| [DeepSeek Context Caching](https://api-docs.deepseek.com/guides/kv_cache/) | Its explicit cache-hit/miss input definitions inform the cache composition view; the ratio uses tokens, not request counts |
| [LiteLLM Usage](https://docs.litellm.ai/docs/proxy/ui/chat_ui) | Separate range selection, core metrics, and usage logs; retain the existing data scope without adding costs or success rates that cannot be measured accurately |
| [Langfuse Custom Dashboards](https://langfuse.com/docs/metrics/features/custom-dashboards) | Separate summaries, time trends, and model distribution into readable areas, with switchable trend metrics |

Full DeepSeek and GLM console pages were not publicly readable during that research. The visual design therefore used verifiable open-source dashboards and does not claim to reproduce their current authenticated interfaces. The project adds no provider billing, budget, or savings estimates.

Run events → streaming/non-streaming usage accumulation → `eino_usage_summary` → `process_details` and `model_token_usage` → existing `/api/usage/tokens` and conversation usage endpoints → dashboard and chat summaries.

`model_token_usage.cache_write_tokens INTEGER NOT NULL DEFAULT 0` was added. Existing declarative SQLite schema reconciliation adds the column at startup; older databases need no manual migration. Existing event-ID idempotency, foreign-key deletion relationships, and historical backfills remain in effect. Daily grouping uses the server's local timezone, matching the dashboard's “Today” boundary.

Provider model names are rendered as text nodes. Tables scroll locally on mobile, and labels support Chinese, English, and Russian. Chat-turn tooltips, the main conversation timeline, and WebShell timelines also include cache writes.

## Current scope and future work

Statistics use recorded Agent output events, including sub-agents in the existing collection path. They are **not a site-wide API request ledger**. Compaction, retrieval embeddings, hidden model calls, and requests without usage data are not fully covered. Consumption whose final usage was lost through request cancellation cannot be reconstructed.

Model rankings group by the model name configured for the run. A failover run may mix multiple models, so rankings cannot be used for per-model billing. Each recent entry is a run summary, not an individual HTTP request. Model call counts include only outputs with nonempty usage.

Costs and remaining plan quotas are not displayed. Custom gateways and subscription plans have different pricing and billing rules; multiplying tokens by a fixed price cannot reliably represent cost. Complete cost management would first require a per-request ledger covering call origin, actual provider and model, success/error/cancellation, duration, TTFT, whether usage was returned, price version, and currency. It should cover main/sub-agents, compaction, retries, and embeddings, with separate provider quota integrations.

## Validation

The Chinese guide records these implementation-time checks; this translation does not represent a new execution of them:

- `go test ./...`: the full repository passed.
- `node --test web/static/js/*.test.cjs`: 295 tests passed.
- Regression coverage was added for cache-write persistence, grouping, recent entries, adding the column to older schemas, user visibility, streaming deduplication, and total-token fallback.
- DeepSeek JSON/SSE cache-field conversion, standard-field precedence, preservation of error/termination chunks, and very large number precision passed validation.
- Browser checks used synthetic data for desktop, 390px mobile, dark mode, model-name escaping, empty data, and unavailable API states. No real run database or paid model was accessed.
