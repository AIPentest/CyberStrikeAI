# Pi runtime

CyberStrikeAI uses Pi 1.5.2 as its only runtime. Go owns business state, permissions, MCP execution, evidence, and delivery checks; an isolated Node worker hosts the Pi SDK.

## Build

Node.js >=22.19 and the Go version in go.mod are required:

```sh
bash scripts/build-pi-runtime.sh
go build -o cyberstrike-ai ./cmd/server
```

The launcher and self-update build prepare the worker. Dependency installation uses the lockfile with lifecycle scripts disabled. Complete bundles are published under `runtime/pi/.builds/<source fingerprint>`; running workers keep their original dependencies, and source rollback selects the corresponding old bundle. Retain the runtime sources and matching bundle when deploying, or rebuild on the destination. A Go binary alone is insufficient for Pi.

After pulling a worker fix into a source installation, run `bash scripts/build-pi-runtime.sh` in that installation before restarting the service. If `agent_runtime.worker_path` is set, make sure it points to the rebuilt worker. Keep existing session files: the worker corrects interrupted OpenAI-compatible tool batches when preparing model requests.

## Configuration

`agent_runtime.engine` accepts only `pi` (also the default). The former engine and dependencies have been removed. Optional `node_path`, `worker_path`, and `session_dir` control deployment.

The model worker uses HTTP/1.1 and respects the service's `HTTP_PROXY`, `HTTPS_PROXY`, and `NO_PROXY` environment variables. Model redirects stay on the original origin to protect credentials.

Chat agents, specialists and workflow Agent nodes use Pi. Large catalogs can use native tool search. Conversation task boards remain compatible with the existing UI.

Workflow now uses a native DAG and versioned JSON checkpoints. Old Eino Workflow binary checkpoints cannot be resumed by this version; finish those runs before upgrading, or export their material and start new Pi tasks. Knowledge retrieval and indexing use native domain types and HTTP embeddings. Old configuration keys are read as data migration only; saving emits the native skills/runtime keys. Legacy URL aliases call the same Pi handlers and cannot select a different engine.

See the [Chinese implementation and validation guide](../zh-CN/pi-runtime.md) for protocol, recovery and test details. Local fixtures establish functional behavior, not live-provider reliability, refusal-rate improvements or production acceptance.
