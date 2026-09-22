# Human-in-the-loop (HITL) Best Practices

[中文](../zh-CN/hitl-best-practices.md)

HITL reviews tool calls before an Agent executes them. Use it to control high-risk operations, keep an audit trail, and let an Audit Agent take over routine approvals when human reviewers cannot keep up.

## Where To Configure

Open **System Settings → Human-in-the-loop** in the web UI. You can configure:

- Global default reviewer: `human` or `audit_agent`
- Approval engine: `hitl.audit_backend` (`openai` or `typesafe`)
- Dedicated Audit Agent model: `hitl.audit_model`
- Resolved audit log retention days
- No-approval tool allowlist: `hitl.tool_whitelist`
- Audit prompts for approval mode and review-edit mode

Example `config.yaml`:

```yaml
hitl:
  default_reviewer: human
  audit_backend: openai
  audit_model:
    provider: ""
    base_url: ""
    api_key: ""
    model: "" # set a small model here; blank reuses the default AI channel model
  retention_days: 90
  tool_whitelist: [read_file, ls, glob, grep, tool_search, get_project_fact, list_project_facts, search_project_facts, list_vulnerabilities, get_vulnerability, get_asset, query_assets, list_knowledge_risk_types, get_tool_execution, wait_tool_execution, batch_task_list, batch_task_get, manage_webshell_list, c2_event, c2_file]
```

`audit_backend` is a choice of `openai` (default, chat-completions JSON from the prompt) or `typesafe` (TypeSafe Jev). Custom audit-strategy text is evaluated as structured `operatorPolicy` questions; built-in destructive rules remain a hard floor. Jev cannot rewrite arguments, including in review-edit mode. The built-in default prompt is already encoded as Jev questions and is not copied into state.

`audit_model` supports partial configuration on the OpenAI backend. Empty fields inherit from the resolved default AI channel. On the TypeSafe backend, `api_key` is required and is **not** inherited from the main model; blank `base_url` uses `https://api.typesafe.ai`, and blank `model` uses `jev-latest`.

## Recommended Approval Strategy

### 1. Start With Humans, Then Delegate Gradually

At the beginning, prefer:

- `default_reviewer: human`
- Only clearly read-only tools in `tool_whitelist`
- Human approval for file writes, command execution, C2 tasks, and WebShell operations

After observing audit logs, move repeated low-risk operations into the allowlist.

### 2. Use A Small Model When Humans Cannot Keep Up

When pending approvals start piling up, switch routine review to the Audit Agent:

```yaml
hitl:
  default_reviewer: audit_agent
  audit_model:
    model: "your-small-reviewer-model"
```

Good candidates for small-model review:

- Read-only queries
- Reconnaissance
- Port and service scans
- Directory enumeration
- Non-destructive validation commands

Keep human review for:

- Deleting, overwriting, or clearing data
- Modifying permissions, passwords, or accounts
- Persistence, lateral movement, and high-risk C2 tasks
- Writes against production targets

### 3. Encode Your Policy In The Prompt

The Audit Agent prompt should describe an operational policy, not just say “be careful.” Make it explicit:

- Which low-risk actions are normally approved
- Which destructive actions must be rejected
- Which cases require escalation to a human
- How review-edit mode may narrow arguments

Example policy snippet:

```text
Approve routine reconnaissance, read-only queries, and port scans by default.
Reject file deletion, database clearing, account or permission changes, persistence, and stopping critical services.
Reject actions outside the user-authorized target scope.
In review-edit mode, you may narrow paths, targets, or command arguments before approving, but must not expand the attack surface.
```

On the OpenAI backend this text is a chat system prompt. On TypeSafe Jev it becomes an `operatorPolicy` overlay evaluated as structured questions; built-in destructive rules remain a hard floor, and Jev will not rewrite arguments. If the text is empty or identical to the built-in default, Jev uses the built-in questions only and does not copy the long prompt into state.

### 4. Keep The Allowlist Conservative

Allowlisted tools skip approval, so keep the list stable and low-risk. Recommended examples:

- `read_file`
- `ls`
- `glob`
- `grep`
- `tool_search`
- Project and vulnerability reads: `get_project_fact`, `list_project_facts`, `search_project_facts`, `list_vulnerabilities`, `get_vulnerability`
- Asset and knowledge-metadata reads: `get_asset`, `query_assets`, `list_knowledge_risk_types`
- Execution and task-state reads: `get_tool_execution`, `wait_tool_execution`, `batch_task_list`, `batch_task_get`
- Local management-metadata reads: `manage_webshell_list`, `c2_event`, `c2_file`

These built-in MCP reads remain constrained by RBAC and resource scope; the allowlist only bypasses HITL approval and does not grant additional access. `list_dir` is effective only when that is the actual tool name; the Eino filesystem directory-listing tool is named `ls`.

Avoid globally allowlisting:

- Arbitrary shell execution tools
- File write/delete tools
- C2 task tools
- WebShell command execution tools
- Nominally read-only tools that send requests to a target or external service, such as `webshell_file_read`, `webshell_file_list`, and `search_knowledge_base`
- Multiplexed tools whose actions include both reads and writes, such as `c2_session`, `c2_listener`, `c2_profile`, and `c2_task_manage`

## Mode Selection

| Mode | Best for |
|------|----------|
| Off | Local labs or fully trusted toolchains |
| Approval | Approve/reject only |
| Review-edit | Let the Audit Agent narrow arguments before approval |

If you configured a small audit model, start with **Approval** mode. Use **Review-edit** only when you want the AI to safely narrow paths, target ranges, or command arguments.

## Operations Tips

- Review **Human-in-the-loop → Audit logs** regularly and tune allowlists/prompts.
- In high-risk environments, keep `default_reviewer: human` and use the Audit Agent only for recommendations.
- If the small-model reviewer fails, CyberStrikeAI rejects conservatively by default.
- After changing `hitl.audit_model`, click **Test audit model** in the settings page.
- For production, customer, or real business systems, keep a human as the final approver.
