# Pi runtime

CyberStrikeAI uses Pi 1.5.2 as its only runtime. Go owns business state, permissions, MCP execution, evidence, and delivery checks; an isolated Node worker hosts the Pi SDK.

Prefer public APIs from the pinned Pi SDK. Add host or worker logic where project permissions, business records, or compatibility require it.

## Build

Node.js >=22.19 and the Go version in go.mod are required:

```sh
bash scripts/build-pi-runtime.sh
go build -o cyberstrike-ai ./cmd/server
```

The launcher and self-update build prepare the worker. The new server also prepares a missing matching bundle at startup and checks the worker handshake before accepting requests. This covers the first upgrade from Eino, whose old updater only builds Go. Preparation requires Node.js >=22.19, npm and access to the locked npm dependencies; failures stop startup with an actionable error. An existing matching bundle starts without npm or dependency downloads. Dependency installation uses the lockfile with lifecycle scripts disabled. Complete bundles are published under `runtime/pi/.builds/<source fingerprint>`; running workers keep their original dependencies, and source rollback selects the corresponding old bundle. Retain the runtime sources and matching bundle when deploying, or rebuild on the destination. A Go binary alone is insufficient for Pi.

After pulling a worker fix into a source installation, run `bash scripts/build-pi-runtime.sh` in that installation before restarting the service. If `agent_runtime.worker_path` is set, make sure it points to the rebuilt worker. Keep existing session files: the worker corrects interrupted OpenAI-compatible tool batches when preparing model requests.

## Configuration

`agent_runtime.engine` accepts only `pi` (also the default). The former engine and dependencies have been removed. Optional `node_path`, `worker_path`, and `session_dir` control deployment.

The model worker uses HTTP/1.1 and respects the service's `HTTP_PROXY`, `HTTPS_PROXY`, and `NO_PROXY` environment variables. Model redirects stay on the original origin to protect credentials.

Deleting a conversation turn removes all of its persisted Pi mode/child sessions and advances a database revision; the next request imports the remaining database history. A running conversation must be stopped before deleting a turn. Session roots are retained in the database across configuration changes and restarts so deletion cleans both old and new locations.

Pi's `SessionManager` opens `session.jsonl` directly for new conversations. Existing conversations keep using the file referenced by their `current-session` pointer, preserving saved evidence paths. If rolled back to an older worker, it rebuilds the context for new-format sessions from the history supplied by the host without loading their Pi compaction records.

Chat agents, specialists and workflow Agent nodes use Pi. User steering goes through Pi's native queue. Large catalogs can use native tool search. Conversation task boards remain compatible with the existing UI.

Workflow now uses a native DAG and versioned JSON checkpoints. Old Eino Workflow binary checkpoints cannot be resumed by this version; finish those runs before upgrading, or export their material and start new Pi tasks. Knowledge retrieval and indexing use native domain types and HTTP embeddings. Old configuration keys are read as data migration only; saving emits the native skills/runtime keys. Legacy URL aliases call the same Pi handlers and cannot select a different engine.

See the [Chinese implementation and validation guide](../zh-CN/pi-runtime.md) for protocol, recovery and test details. Local fixtures establish functional behavior, not live-provider reliability, refusal-rate improvements or production acceptance.
