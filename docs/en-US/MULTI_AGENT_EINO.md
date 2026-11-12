# Eino Multi-Agent Notes

[Chinese](../zh-CN/MULTI_AGENT_EINO.md)

CyberStrikeAI uses CloudWeGo Eino ADK for the current single-agent and multi-agent execution paths. The native legacy ReAct path has been removed.

## Entrypoints

- Single-agent: `/api/eino-agent` and `/api/eino-agent/stream`
- Multi-agent: `/api/multi-agent` and `/api/multi-agent/stream`

Multi-agent orchestration is selected by request body:

- `deep`
- `plan_execute`
- `supervisor`

Robots default to `robot_default_agent_mode`, and batch tasks can opt into multi-agent through config.

## Agent Definitions

Markdown agents live under `agents/`.

Typical files:

```text
agents/orchestrator.md
agents/orchestrator-plan-execute.md
agents/orchestrator-supervisor.md
agents/*.md
```

Front matter controls name, id, description, tools, bound role, max iterations, and optional orchestrator kind.

## Middleware

Important Eino middleware:

- tool search: exposes a small visible tool set and unlocks others on demand;
- patch tool calls: repairs interrupted histories;
- plan task: structured task board;
- reduction: truncates or persists large tool outputs;
- summarization: compresses long contexts;
- checkpoint: resume after crash/OOM.

These settings live under `multi_agent.eino_middleware`.

## Skills

Eino Skills support progressive disclosure. The Agent initially sees names and descriptions; details are loaded only when needed through the configured skill tool.

## Operational Notes

- Tool visibility is not the same as tool availability in the UI.
- Running streams keep their startup context even if config changes mid-run.
- Summarization can write transcripts under `data/conversation_artifacts/...`.
- High-risk tools should still be constrained by roles and HITL.

## Source Anchors

- Multi-agent handler: `internal/handler/multi_agent.go`
- Preparation: `internal/handler/multi_agent_prepare.go`
- Orchestration: `internal/multiagent/eino_orchestration.go`
- Run loop: `internal/multiagent/eino_adk_run_loop.go`
- Skills: `internal/multiagent/eino_skills.go`
- Middleware: `internal/multiagent/eino_middleware.go`
