# CyberStrikeAI Workflows Guide

[Chinese](../zh-CN/workflow-graph.md)

This document explains how to use **Workflows**: building workflows on the canvas, configuring node types, passing data between nodes, and binding a workflow to a role for automatic execution.

---

## 1. Where to find Workflows

1. Log in to the CyberStrikeAI web UI.  
2. Open **Workflows** in the left sidebar.  
3. Select an existing workflow from the list, or create a new one.  
4. Drag nodes, draw edges, and configure properties on the canvas.  
5. Fill in **ID**, **Name**, and **Description**, then click **Save**.

Saved workflows can be bound to a role under **Role Management**. When `workflow_policy` is `auto`, chatting with that role runs the bound graph automatically.

---

## 2. Canvas basics

| Action | Description |
|--------|-------------|
| Add node | Click a node type button above the canvas (Start, Tool, Agent, Condition, HITL, Output, End) |
| Connect | Click **Connect**, then click source and target nodes; click **Connect** again to exit connect mode |
| Select | Click a node or edge; properties appear in the right panel |
| Delete selected | Remove the current node or edge |
| Auto layout | Rearrange node positions |
| Dry run | Safely simulate data flow; Tool, Agent, and HITL nodes are not executed for real |
| Delete workflow | Remove the entire workflow definition |

**Hard requirements:** Every workflow needs at least **one Start node** and **one Output node**. Start nodes must not have incoming edges; Output / End nodes must not have outgoing edges. Both frontend and backend run strict validation before save.

---

## 3. Natural-language Draft Generation

The Workflows page provides a **Create from natural language** entry point. After a user describes a security operations goal, CyberStrikeAI generates an editable draft and returns a structured audit result:

- Draft generation does not save, dry-run, or execute tools automatically.
- The server endpoint is `POST /api/workflows/generate-draft`, protected by `workflow:write`.
- The response includes `graph`, `meta`, `capabilities`, `audit`, and `stats`; after applying the draft to the canvas, normal save validation still runs.
- High-risk language such as executing scripts, isolating hosts, blocking, deleting, or exploitation is marked as `high_risk` and defaults to HITL approval or `requires_human_confirmation`.
- Tool capabilities are matched against the available tool list; unmatched capabilities fall back to Agent draft nodes and are surfaced in `audit.missing_fields` / `audit.assumptions`.
- If server generation is unavailable, the frontend uses a local deterministic fallback and still returns an editable draft with risk notes.

Example request:

```http
POST /api/workflows/generate-draft
Content-Type: application/json

{
  "prompt": "Scan target assets for open ports, create a vulnerability task when critical ports are found, ask the owner for approval, and output a report",
  "options": {
    "include_objective": true,
    "allow_schedule": false,
    "allow_high_risk": false
  },
  "available_tools": [
    { "key": "nmap", "name": "nmap", "enabled": true }
  ]
}
```

---

## 4. Execution model (read this before configuring)

The engine executes the workflow as a **directed graph**, starting from the **Start** node and following edges to downstream nodes.

During a run, the engine keeps internal state. Template expressions `{{...}}` read from that state:

| Internal state | Template prefix | Meaning |
|----------------|-----------------|---------|
| `inputs` | `{{inputs.xxx}}` | Workflow inputs at start (user message, conversation ID, etc.) |
| `lastOutput` | `{{previous.xxx}}` | Output of the **most recently executed** node |
| `outputs` | `{{outputs.xxx}}` | Global **named variable pool** (written by nodes with an output key) |
| `nodeOutputs` | `{{nodeId.xxx}}` | Full output object of a specific node ID |
| `metrics` | available in run details | Node duration, tool call count, and usage/cost metrics when reported |

### 3.1 What is `previous`?

`{{previous.output}}` is the `output` field of the **immediately preceding executed node**.

- After every node finishes, the engine updates `lastOutput`.
- It is **not** “the node drawn upstream on the canvas”; it is **the previous step in actual execution order**.

Example:

```text
Start → Agent A → Agent B
```

For Agent B, `{{previous.output}}` = Agent A’s output.

With a condition in between:

```text
Start → Agent A → Condition → Agent B
```

For Agent B, `{{previous.output}}` = the **condition node** output (`true` / `false`), **not** Agent A’s result.

If a node has **multiple upstream nodes**, `previous` is built by that node’s **join strategy** first:

| Join strategy | Meaning | Use case |
|---------------|---------|----------|
| `all_merge` | Merge all upstream outputs; `previous.output` is an array | Default; aggregate multiple results |
| `last_by_canvas` | Use the last upstream output by canvas order | Explicitly use one branch |
| `first_non_empty` | Use the first non-empty output | Fallback chains |
| `fail_fast` | Stop the node if any upstream failed | Critical gates, approval prechecks, safety checks |

### 3.2 What is `outputs`?

`outputs` is a **named variable registry** maintained by the engine during execution.

When an Agent, Tool, or Output node sets an **Output variable name** (`output_key`), the result is stored as:

```text
outputs["your_variable_name"] = node_output
```

Any downstream node can then reference it via `{{outputs.variable_name}}`, even if other nodes sit in between.

Example:

- Agent A **Output variable name**: `agent_result1`
- Agent B **Input source**: `{{outputs.agent_result1}}`

Agent B still receives Agent A’s output even when a condition node lies between them.

### 3.3 When to use `previous` vs `outputs`

| Scenario | Recommended |
|----------|-------------|
| Two nodes are **directly connected**; you only need the last step | `{{previous.output}}` |
| Other nodes sit in between (condition, tool, HITL, etc.) | `{{outputs.variable_name}}` |
| Reference output from an **earlier** node | `{{outputs.variable_name}}` or `{{nodeId.output}}` |
| Condition should test an Agent’s output | `{{outputs.variable_name}} != ""` |
| Read the original user input | `{{inputs.message}}` |

**Rule of thumb:**

- `previous` = last step (chained, adjacent)
- `outputs` = by name (cross-node, look back)

---

## 5. Template syntax

### 4.1 Basic format

```text
{{path.to.value}}
```

Allowed characters in paths: letters, digits, underscore, dot, hyphen. Examples:

```text
{{previous.output}}
{{outputs.agent_result1}}
{{inputs.message}}
{{inputs.conversationId}}
{{previous.matched}}
{{node-abc123.output}}
```

### 4.2 Available paths

| Path | Description |
|------|-------------|
| `{{inputs.message}}` | User message (Start node input) |
| `{{inputs.conversationId}}` | Conversation ID |
| `{{inputs.projectId}}` | Project ID |
| `{{previous.output}}` | Primary output of the previous node |
| `{{previous.matched}}` | Match result of the previous condition node (`true` / `false`) |
| `{{outputs.variable_name}}` | Named output registered by a node |
| `{{nodeId.output}}` | `output` field of the node with that ID |
| `{{previous.kind}}` | Previous node output kind, e.g. `agent` / `tool` / `condition` |
| `{{previous.status}}` | Previous node status, e.g. `completed` / `failed` / `simulated` |

Node outputs keep compatibility fields such as `output` and `matched`, and also include a structured envelope:

```json
{
  "kind": "agent",
  "node_id": "node-2",
  "node_type": "agent",
  "status": "completed",
  "output": "..."
}
```

### 4.3 Condition expressions

Condition nodes and edge conditions support comparisons, text matching, regex, logical operators, and safe JSONPath/JQ path reads:

```text
{{outputs.agent_result1}} != ""
{{previous.output}} == "ok"
{{outputs.count}} >= 100
{{previous.output}} contains "success"
{{previous.output}} matches "^ok"
{{outputs.risk_score}} >= 8 && {{previous.output}} != ""
jsonpath({{previous.output}}, "$.status") == "ok"
jq({{outputs.scan}}, ".severity") == "high"
```

Rules:

- Operators: `==`, `!=`, `>`, `>=`, `<`, `<=`
- `contains` checks substrings; `matches` checks regular expressions
- Simple `&&` / `||` is supported
- `jsonpath(value, "$.path")` and `jq(value, ".path")` support a **safe path-only subset**; no arbitrary script execution
- Leading/trailing spaces and quotes are trimmed before comparison
- Without a comparator, non-empty values that are not `false`, `0`, or `null` are treated as true
- Expressions, regexes, and JSONPath/JQ paths are statically validated before save

### 4.4 Nested field binding

Field bindings can read ordinary fields such as `output` or `message`, and also JSONPath/JQ-style paths:

| Binding | Meaning |
|---------|---------|
| `from=previous, field=$.status` | Read `status` from previous output |
| `from=outputs, field=$.scan.severity` | Read a nested field from named outputs |
| `from=node-1, field=.output.items[0]` | Read an array element from a specific node output |

---

## 6. Node types and configuration

### 5.1 Start

Workflow entry point; injects user input into `inputs`.

| Field | Description | Default |
|-------|-------------|---------|
| Input keys | Comma-separated input key names | `message, conversationId, projectId` |

Start node output includes: `output`, `message`, `conversationId`, `projectId`.

### 5.2 Agent

Runs an LLM Agent task. Supports multiple modes.

| Field | Description | Default |
|-------|-------------|---------|
| Agent mode | `eino_single` / `deep` / `plan_execute` / `supervisor` | `eino_single` |
| Input source | Template for upstream data | `{{previous.output}}` |
| Node instruction | Task description for this node | empty |
| Output variable name | Key written into `outputs` | `agent_result` |
| Join strategy | How to build `previous` when multiple upstreams enter this node | `all_merge` |

**Message assembly:**

- Instruction only → send instruction to the Agent  
- Input source only → “Continue based on upstream output: …”  
- Both → combined “upstream input + node instruction”

After execution:

- `previous.output` becomes this node’s response text  
- If **Output variable name** is set, the value is also stored in `outputs[variable_name]`
- In the Eino graph, the Agent node is split into `prepare → execute → finalize` for clearer trace and future checkpointing

### 5.3 Tool

Calls an enabled MCP tool.

| Field | Description | Default |
|-------|-------------|---------|
| MCP tool | Tool name (required) | — |
| Argument template | JSON with `{{...}}` templates | `{}` |
| Timeout (seconds) | Optional | empty |
| Join strategy | How to build `previous` when multiple upstreams enter this node | `all_merge` |

Example argument template:

```json
{"target": "{{inputs.message}}", "port": "443"}
```

If an output variable name is configured, the tool result is written to `outputs`.

### 5.4 Condition

Evaluates an expression and outputs `matched` (`true` / `false`).

| Field | Description | Default |
|-------|-------------|---------|
| Expression | Supports `{{...}}` and `==` / `!=` | `{{previous.output}} != ""` |
| Join strategy | How to build `previous` when multiple upstreams enter this node | `all_merge` |

**Branching rules:**

- The **first outgoing edge** defaults to the **“yes”** branch (`matched == true`)
- The **second outgoing edge** defaults to the **“no”** branch (`matched == false`)
- Edge labels such as `yes` / `no` (or `true` / `false`) help identify branches
- A third or later edge needs a custom **edge condition**

Edge condition examples (select an edge, configure in the right panel):

```text
{{previous.matched}} == "true"
{{previous.matched}} == "false"
```

### 5.5 HITL (human-in-the-loop)

Human approval checkpoint. The run pauses before this node through Eino interrupt/checkpoint and resumes after approval via API or the monitor panel.

| Field | Description | Default |
|-------|-------------|---------|
| Prompt | Supports templates | `Please approve before continuing` |
| Prompt binding | If prompt text is empty, read approval text from a bound field | `previous.output` |
| Reviewer | `human` / `audit_agent` | `human` |
| Join strategy | How to build `previous` when multiple upstreams enter this node | `all_merge` |

Pending HITL metadata records:

- `checkpointId`
- interrupt `beforeNodes`
- resume target / address / path
- resume payload schema (`approved`, `comment`)

### 5.6 Output

Writes the final workflow result into `outputs` for summary and chat display.

| Field | Description | Default |
|-------|-------------|---------|
| Output variable name | Required key for the final result | `result` |
| Variable source | Template deciding what to write | `{{previous.output}}` |
| Static output value | Optional; overrides variable source when set | empty |
| Join strategy | How to build `previous` when multiple upstreams enter this node | `all_merge` |

**Note:** Output nodes are workflow exits and must not have outgoing edges.

### 5.7 End

Optional node for an end summary template (less common in role-bound flows).

| Field | Description | Default |
|-------|-------------|---------|
| Result template | Supports `{{outputs.xxx}}` | `{{outputs.result}}` |
| Join strategy | How to build `previous` when multiple upstreams enter this node | `all_merge` |

---

## 7. Edge configuration

Select an **edge** to configure its **condition** in the right panel.

| Scenario | Example |
|----------|---------|
| Filter after a normal node | `{{previous.output}} == "ok"` |
| “Yes” branch from a condition | `{{previous.matched}} == "true"` |
| “No” branch from a condition | `{{previous.matched}} == "false"` |

If no edge condition is set:

- Non-condition nodes: edge is always allowed  
- Condition nodes: yes/no branches are assigned by edge order automatically

---

## 8. Full example: passing Agent output across a condition

### 7.1 Graph structure

```text
Start → Agent (initial value) → Condition → Agent (transform) → Output
                                    ↘ no → Output
```

### 7.2 Node configuration

**Agent 1**

| Field | Value |
|-------|-------|
| Node instruction | Output only `123333333` |
| Output variable name | `agent_result1` |

**Condition**

| Field | Value |
|-------|-------|
| Expression | `{{outputs.agent_result1}} != ""` |

**Agent 2**

| Field | Value |
|-------|-------|
| Input source | `{{outputs.agent_result1}}` |
| Node instruction | Add 100 to the input, then output |
| Output variable name | `agent_result` |

**Output**

| Field | Value |
|-------|-------|
| Output variable name | `result` |
| Variable source | `{{outputs.agent_result}}` |

### 7.3 Common mistakes

| Wrong config | Why it fails |
|--------------|--------------|
| Agent 2 input source = `{{previous.output}}` | `previous` points to the condition node → `true`/`false`, not Agent 1’s text |
| Agent 1 has no output variable name | `outputs.agent_result1` does not exist → empty downstream |
| Condition uses `{{previous.output}}` | Tests the wrong upstream value instead of Agent 1’s named output |

---

## 9. Bind to a role and run

### 8.1 Bind in Role Management

1. Open **Role Management**, edit or create a role.  
2. Select the workflow / graph ID to bind.  
3. Set policy to `auto` (default when `workflow_id` is set).  
4. Save the role.

You can also configure this in role YAML:

```yaml
name: workflow-test
workflow_id: "1233"
workflow_version: latest
workflow_policy: auto
```

### 8.2 Runtime behavior

When a user chats with that role:

1. The engine loads `graph_json` and executes the graph.  
2. The chat UI shows progress events (`workflow_start`, `workflow_node_start`, Agent reasoning, etc.).  
3. When finished, a summary lists all named entries in `outputs`.

If no Output node is reached or no branch matches, `outputs` may be empty and the summary will suggest checking the Output node and branches.

---

## 10. Debugging, dry-run, and replay

### 9.1 Safe dry-run

Click **Dry run** on the canvas toolbar and enter a test message to simulate the workflow.

Dry-run safety rules:

- `start` / `condition` / `output` / `end` use real logic
- `tool` does not call MCP; it returns `[dry-run] tool call skipped`
- `agent` does not call the model; it returns `[dry-run] agent execution skipped`
- `hitl` does not pause; it simulates approval

API:

```http
POST /api/workflows/dry-run
```

Request:

```json
{
  "graph": { "nodes": [], "edges": [], "config": {} },
  "inputs": { "message": "ping" }
}
```

Response includes:

- `outputs`
- `nodeOutputs`
- `trace`
- `metrics`
- `replayScript`

### 9.2 Run details and replay

Query full node execution traces after a run:

```http
GET /api/workflows/runs/{runId}
```

The response contains `run` and `nodeRuns`. Each node run records:

- input snapshot
- output snapshot
- status / error
- started_at / finished_at
- `duration_ms`

Replay API:

```http
GET /api/workflows/runs/{runId}/replay
```

This generates replay steps from saved `nodeRuns`; it does not re-execute tools or Agents.

### 9.3 Metrics

The workflow accumulates, when available:

- `node_count`
- `duration_ms`
- `tool_call_count`
- Agent progress usage such as `prompt_tokens` / `completion_tokens` / `total_tokens` / `cost`

Token and cost metrics depend on whether the underlying model/Agent events report usage.

---

## 11. Validation before save

On save, the system checks:

| Rule | Description |
|------|-------------|
| Start node required | At least one `start` node |
| Output node required | At least one `output` node with an output variable name |
| Valid edges | Source and target exist; no self-loops |
| Start has no incoming edges | Start must not be targeted |
| Output / End has no outgoing edges | Nothing after Output / End |
| Non-start nodes must have incoming edges | Prevent orphan nodes |
| Non-output/end nodes must have outgoing edges | Prevent dead ends |
| No cycles | Workflow orchestration must be a DAG |
| Reachability | Every node must be reachable from Start and eventually reach output/end |
| Tool nodes | MCP tool required; argument JSON must be valid; timeout must be a positive integer |
| Agent nodes | Must have node instruction or input binding; output variable name required |
| Condition nodes | Expression required; 1–2 outgoing edges; branches must be yes/no and unique |
| Edge conditions | Expressions, regexes, and JSONPath/JQ paths must pass static validation |
| Join strategy | Must be `all_merge` / `last_by_canvas` / `first_non_empty` / `fail_fast` |

---

## 12. Troubleshooting

| Symptom | Likely cause | Fix |
|---------|--------------|-----|
| Downstream gets empty value | Upstream has no output variable name | Set **Output variable name** on upstream; use `{{outputs.xxx}}` downstream |
| Downstream gets `true`/`false` | Used `{{previous.output}}` while previous node is a condition | Use `{{outputs.xxx}}` instead |
| Condition always takes “no” | Expression does not match actual output format | Check Agent output for quotes/newlines; try `!= ""` first |
| No final output | Output node branch not reached | Verify condition wiring; ensure every path reaches an **Output** node |
| Role chat does not run workflow | Role not bound or disabled | Check `workflow_id`, `workflow_policy: auto`, workflow `enabled: true` |
| Tool node fails | Invalid JSON in arguments or tool disabled | Fix argument template; enable the tool in MCP settings |
| Save fails with invalid branch | Condition outgoing edges are not marked yes/no, or are duplicated | Select the edge and set branch to `true` or `false` |
| Multi-upstream result is unexpected | Join strategy does not match the workflow | Switch between `all_merge`, `first_non_empty`, `last_by_canvas`, and `fail_fast` |
| Nested field is empty | JSONPath/JQ path is outside the safe subset | Use `$.a.b[0]` or `.a.b[0]`; avoid wildcards, recursion, or expressions |

---

## 13. Best practices

1. **Meaningful names**: Use descriptive output variable names (`scan_result`, `parsed_targets`) instead of reusing `agent_result` everywhere.  
2. **Prefer `outputs` for cross-node data**: If a condition, tool, or HITL node might sit in between, use named variables.  
3. **Use `previous` only for direct links**: `A → B` with nothing in between is the ideal case for `{{previous.output}}`.  
4. **Conditions should reference source data**: When testing Agent output, use `{{outputs.xxx}}` unless the condition immediately follows that Agent.  
5. **Every path needs an exit**: Ensure both yes and no branches eventually reach an **Output** node (or your intended end).  
6. **Choose join strategy explicitly for multi-upstream nodes**: Use `all_merge` for aggregation, `first_non_empty` for fallback, and `fail_fast` for critical gates.  
7. **Use JSONPath/JQ safe paths for nested JSON**: e.g. `jsonpath({{previous.output}}, "$.status") == "ok"`.  
8. **Dry-run before real execution**: Validate data flow and branches with a simple message before binding the workflow to a role.

---

## 14. Code references (for developers)

| Module | Path |
|--------|------|
| Execution engine | `internal/workflow/runner.go` |
| Eino compile / checkpoint / HITL | `internal/workflow/eino_compile.go` |
| Graph validation | `internal/workflow/validation.go` |
| Expressions / JSONPath / joins | `internal/workflow/expression.go`, `jsonpath.go`, `join.go` |
| Dry-run / replay data | `internal/workflow/dry_run.go`, `internal/handler/workflow_run.go` |
| Canvas UI | `web/static/js/workflows.js` |
| Workflow API | `internal/handler/workflow.go` |
| Role binding | `internal/config/config.go` (`workflow_id` field) |
