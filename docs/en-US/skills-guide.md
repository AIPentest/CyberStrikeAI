# Skills Guide

[中文](../zh-CN/skills-guide.md)

Skills provide reusable procedures, checklists, templates, and references that Agents can load when needed. A Skill should be an executable procedure, not an encyclopedia page.

## Structure

```text
skills/
  ssrf-testing/
    SKILL.md
    REFERENCE.md
```

`SKILL.md` front matter:

```markdown
---
name: ssrf-testing
description: SSRF identification, validation, bypass, and remediation workflow
---
```

The description determines when the Agent loads it.

## Recommended Sections

```markdown
## When to use
## Preconditions
## Procedure
## Stop conditions
## Output
```

Stop conditions matter: they tell the Agent when to escalate, ask for approval, or stop expanding scope.

## Anti-Patterns

| Anti-pattern | Result | Fix |
| --- | --- | --- |
| Description too broad | triggers too often | make it scenario-specific |
| Encyclopedia content | Agent lacks next step | write procedures and decisions |
| Secrets in Skill | leakage/misuse | use runtime config or user input |
| One huge Skill | costly and noisy | split by task/vulnerability |
| No stop condition | scope creep | define approval/stop rules |

## Skill vs Knowledge Base

- Skill: how to do something.
- Knowledge base: facts, references, cases.

For SSRF, a Skill describes the test procedure; the KB stores metadata addresses, bypass cases, and remediation references.

## Local Tool Risk

`filesystem_tools: true` exposes local read/write/execute capability. In production:

- constrain workspace;
- require HITL for write/execute;
- do not globally allowlist `execute`;
- make Skills explicitly avoid out-of-scope files.

## Source Anchors

- Validation: `internal/skillpackage/validate.go`
- Service: `internal/skillpackage/service.go`
- Pi Skills: `internal/multiagent/pi_local_tools.go`
- Handler: `internal/handler/skills.go`
