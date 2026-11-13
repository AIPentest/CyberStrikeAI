# Pi single and multi-agent execution

Pi is the only runtime. Go owns business state, tool permissions, HITL, execution receipts and finalization. The SDK owns model loops, sessions, compaction and streaming. See [Pi runtime](pi-runtime.md).

Canonical single-agent endpoints are `/api/agent`, `/api/agent/stream`, and `/api/agent/runs`; the mode is `single`. Multi-agent endpoints retain `/api/multi-agent` and support `deep`, `plan_execute`, and `supervisor`. Older route/mode identifiers are input aliases only and cannot select another engine.

Deep delegates with `task`; Plan-Execute records a plan and delegates bounded executor steps; Supervisor selects specialists and finishes with `exit`. Markdown definitions under `agents/` remain supported. Child tool catalogs may narrow, never widen, their parent's allowed catalog.

`multi_agent.skills` configures skill loading and native host tools. `multi_agent.runtime` contains discovery, task-board, retry, failover and output-budget settings. Large catalogs use native `tool_search`; TaskCreate/Update/Get/List and write_todos remain compatible with the conversation progress UI.

Steering enters at a tool-batch boundary; cancellation stops inference and host tools. Uncertain effects after a crash are retained as unresolved results. Runtime completion does not bypass execution-evidence checks.
