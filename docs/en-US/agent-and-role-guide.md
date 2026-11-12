# Agent and Role Guide

[Chinese](../zh-CN/agent-and-role-guide.md)

Agent behavior is shaped by roles, Markdown sub-agents, Skills, tool visibility, and HITL policy.

## Responsibility Boundaries

| Resource | Purpose | Not for |
| --- | --- | --- |
| Role | identity, tone, tool boundary, authorization rules | large reference material |
| Agent Markdown | multi-agent specialization, handoff format, local strategy | one-off facts |
| Skill | reusable procedures, checklists, templates, references | permission control |

Authorization boundaries belong in roles and HITL first, not only in Skills.

## Modes

| Mode | Good for | Poor fit |
| --- | --- | --- |
| `eino_single` | short tasks, interactive analysis | large multi-stage work |
| `deep` | dynamic task decomposition | strict sequential workflows |
| `plan_execute` | plan, execute, replan loops | frequent user interruption |
| `supervisor` | expert routing | vague or too many sub-agents |

Start with `eino_single`; use `plan_execute` for structured projects; use `deep` or `supervisor` when specialist agents matter.

## Markdown Sub-Agent

Example:

```yaml
---
name: Vulnerability Triage
id: vulnerability-triage
description: Validate, classify, and summarize vulnerability evidence
tools:
  - nmap
  - nuclei
bind_role: Comprehensive Vulnerability Scanning
max_iterations: 200
---
```

The body should define scope, tool order, output format, and prohibited actions.

## Tool Visibility

With `tool_search`, the model initially sees only a subset of tools:

- visible in UI does not mean visible in current model context;
- `tool_search_always_visible_tools` are easier to call;
- clear tool descriptions improve search hits;
- sub-agent tool constraints still matter.

When a tool is not used, check role tools, sub-agent tools, tool_search config, and description.

## Output Format

Sub-agents should return structured results:

```markdown
## Conclusion
## Evidence
- Tool:
- Key output:
- Confidence:
## Risks
## Suggested next step
```

This helps the orchestrator continue and supports reporting.

## Source Anchors

- Markdown Agent parser: `internal/agents/markdown.go`
- Multi-agent preparation: `internal/handler/multi_agent_prepare.go`
- Orchestration: `internal/multiagent/eino_orchestration.go`
- Tool search middleware: `internal/multiagent/eino_middleware.go`
