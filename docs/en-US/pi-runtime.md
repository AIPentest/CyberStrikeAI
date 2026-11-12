# Pi runtime

CyberStrikeAI uses Pi 1.5.2 by default. Go owns business state, permissions, MCP execution, evidence, and delivery checks; an isolated Node worker hosts the Pi SDK.

## Build

Node.js >=22.19 and the Go version in go.mod are required:

```sh
bash scripts/build-pi-runtime.sh
go build -o cyberstrike-ai ./cmd/server
```

The launcher and self-update build prepare the worker. Dependency installation uses the lockfile with lifecycle scripts disabled. Complete bundles are published under `runtime/pi/.builds/<source fingerprint>`; running workers keep their original dependencies, and source rollback selects the corresponding old bundle. Retain the runtime sources and matching bundle when deploying, or rebuild on the destination. A Go binary alone is insufficient for Pi.

## Configuration

`agent_runtime.engine` defaults to `pi`; `eino` explicitly selects the legacy Agent fallback. Optional `node_path`, `worker_path`, and `session_dir` control deployment. Settings exposes runtime selection.

The guidance combines capability selection, main/child task anchors and evidence requirements. It does not ship DSH host patches or encrypted identity prompts. Attribution is in `runtime/pi/THIRD_PARTY_NOTICES.md`.

Chat agents, specialists and workflow Agent nodes use Pi. Large catalogs can use native tool search. Conversation task boards remain compatible with the existing UI.

Workflow now uses a native DAG and versioned JSON checkpoints. Old Eino Workflow binary checkpoints require their original application version; `engine: eino` is an Agent fallback, not a converter for old Workflow checkpoints. Knowledge retrieval and indexing use native domain types and HTTP embeddings. Legacy Eino dependencies are still retained for the explicit Agent fallback.

See the [Chinese implementation and validation guide](../zh-CN/pi-runtime.md) for protocol, recovery and test details. Local fixtures establish functional behavior, not live-provider reliability, refusal-rate improvements or production acceptance.
