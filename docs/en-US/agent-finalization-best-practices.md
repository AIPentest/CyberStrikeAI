# Native Agent finalization and task acceptance

Pi `agent_settled` ends automatic model work for the current run. Go still decides whether the business result can be finalized.

`internal/agentfinalizer` keeps runtime failure, cancellation and iteration-limit outcomes separate from candidate assistant text. Pending tool executions, empty responses and missing required execution evidence prevent completion. Specialist output must be checked against execution receipts; `exit.final_result` does not bypass finalization.

Implementation: `internal/agentfinalizer/decision.go` and `internal/multiagent/pi_runner.go`. See [Pi SDK lifecycle](https://github.com/earendil-works/pi/blob/v1.5.2/packages/coding-agent/docs/sdk.md).
