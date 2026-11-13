# Pi token usage monitoring

Pi reports usage; Go accumulates completed model turns and emits `agent_usage_summary` into the existing database and timeline. Local statistics are not a provider billing statement and may not cover every auxiliary HTTP request.

Prompt tokens are Pi input + cacheRead + cacheWrite. Completion tokens are output, and total is prompt + completion. Cache details are already included and must not be added again. Model-call counts reflect observed turns. Missing reasoning/cache details mean unreported data, not proven zero use.

The dashboard and Settings usage view retain period, model and project filters. APIs are `GET /api/usage/tokens` and `GET /api/conversations/:id/token-usage`. Existing usage rows and historical timeline backfill remain compatible.

Streaming updates carry deltas; complete messages are committed at message end. Implementation: `internal/multiagent/pi_runner.go` and `internal/database/model_token_usage.go`. See [Pi AI](https://github.com/earendil-works/pi/tree/v1.5.2/packages/ai).
