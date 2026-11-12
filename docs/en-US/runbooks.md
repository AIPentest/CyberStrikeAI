# Runbooks

[Chinese](../zh-CN/runbooks.md)

Runbooks are task-oriented procedures you can follow during real operations.

## Runbook 1: Production Instance from Zero to Ready

Use for first-time internal or production red-team deployment.

For local or temporary verification, start with the bundled script:

```bash
chmod +x run.sh && ./run.sh
```

After it is verified, decide whether to move to systemd plus reverse proxy for long-running deployment.

### Preconditions

- Host is managed as an asset.
- Access path is decided: internal network, VPN, bastion, or reverse proxy.
- Model API key and model are available.
- C2, WebShell, and external MCP policy is decided.

### Steps

1. Prepare directory:

```bash
mkdir -p /opt/CyberStrikeAI
```

2. Place binary and resources:

```text
cyberstrike-ai
web/
tools/
roles/
skills/
agents/
docs/
config.yaml
```

3. Set baseline config:

```yaml
auth:
  session_duration_hours: 12
server:
  host: 127.0.0.1
  port: 8080
  tls_enabled: false
audit:
  enabled: true
c2:
  enabled: false
```

4. Configure HTTPS at the reverse proxy and restrict source IPs.
5. Run with systemd.
6. Login and test the model.
7. Check tools and audit logs.
8. Create backup policy.

### Acceptance

- `/api/auth/validate` succeeds after login.
- Model test passes.
- Tools load.
- Audit shows login.
- C2 is disabled when not needed.

## Runbook 2: Connect External MCP

### Preconditions

- MCP service is trusted.
- You know whether it can read/write files, execute commands, or access networks.
- Transport is chosen: stdio, HTTP, or SSE.

### Steps

1. Add service in External MCP page.
2. For stdio, configure command, args, cwd, and env.
3. For HTTP/SSE, configure URL and auth.
4. Start service.
5. Check `/api/external-mcp/stats`.
6. Confirm tools and schemas.
7. Execute one low-risk tool call.
8. Keep high-risk tools out of global allowlist.

### Acceptance

- MCP status is running.
- Tool schemas are visible.
- Agent can find tools through `tool_search`.
- Monitor records tool execution.
- Audit records config change.

## Runbook 3: Enable and Tune Knowledge Base

### Steps

1. Enable config:

```yaml
knowledge:
  enabled: true
  base_path: knowledge_base
  retrieval:
    top_k: 5
    similarity_threshold: 0.4
```

2. Put Markdown files under `knowledge_base/`.
3. Scan directory.
4. Rebuild index.
5. Prepare 5-10 fixed test queries.
6. Search and record hits.
7. Tune threshold, top_k, chunking, and document titles.

### Acceptance

- Index status is complete.
- Common queries hit correct docs.
- Agent consults KB when uncertain.
- Retrieval logs show query and hit docs.

## Runbook 4: Authorized Web Test Workflow

1. Create project and record scope.
2. Start conversation and bind project.
3. Choose minimal role.
4. State target, time window, and prohibited actions.
5. Start with read-only recon.
6. Record useful leads as project facts.
7. Use HITL for risky validation.
8. Save confirmed issues to vulnerability management.
9. Generate attack-chain/report material.
10. Clean uploads, workspace, and unnecessary execution logs.

Acceptance:

- Each vulnerability has evidence, impact, reproduction, and fix.
- Risky actions have HITL records.
- Project facts reconstruct the path.
- Report excludes unrelated sensitive data.

## Runbook 5: C2 Cleanup After Exercise

1. Stop all listeners.
2. List sessions and confirm no authorized session remains active.
3. Export required task results.
4. Delete or archive payloads.
5. Delete stale tasks, events, and files.
6. Review C2 audit trail.
7. Write key results to project facts or report.
8. Set `c2.enabled: false` unless continuously needed.

Acceptance:

- No running listener.
- No pending task.
- Payloads are not publicly downloadable.
- Audit/report explains the lifecycle.

## Runbook 6: Agent Does Not Call a Tool

Check in order:

1. Role includes the tool.
2. Tool appears in `/api/config/tools`.
3. `tool_search` is not hiding it.
4. Tool name and description are clear.
5. HITL is not pending.
6. Agent is not in final summarization phase.
7. Sub-agent does not have a narrower tool list.

Fix:

- add tool to role;
- improve `short_description`;
- add to `tool_search_always_visible_tools`;
- prompt when to use it;
- inspect process details and monitor records.
