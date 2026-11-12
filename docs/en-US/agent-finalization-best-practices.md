# Agent final-response governance best practices

[English documentation](README.md) | [中文](../zh-CN/agent-finalization-best-practices.md)

Research date: 2026-07-28.

This guide addresses a specific failure: an Agent produces natural language that sounds conclusive before tool calls, reasoning, planning, or sub-agent collaboration have actually finished, and the frontend or orchestrator displays it as the final response. Task completion should be determined by runtime state, tool state, verification results, and explicit terminal events together, rather than by the most recent assistant text.

## 1. Core conclusions

1. **A final response is a runtime event, not natural-language content.**
   Phrases such as “obtained,” “next step,” or “Huge breakthrough” are candidate observations or progress, not completion signals.

2. **Separate execution details from delivery.**
   `thinking`, `reasoning_chain`, `planning`, `response_delta`, sub-agent replies, and tool output belong to execution details. Only `response` / `final` events that pass the final gate may populate the main message bubble and `messages.content`.

3. **Complex tasks need a verifier, not a longer prompt.**
   Prompts can encourage caution, but code must decide whether tools or background executions are pending, required plan steps remain incomplete, evidence is unverified, facts/vulnerabilities are unrecorded, or cleanup is unfinished without explanation.

4. **Agent modes differ, but finalization principles are shared.**
   Single-agent, Deep, Plan-Execute, and Supervisor modes all need a final gate. Evidence sources differ: single-agent mode checks tool traces; Deep also checks sub-agent results; Plan-Execute checks the Replanner's termination decision; Supervisor checks `exit` and the supervisor's synthesis.

## 2. Public practices in established Agent systems

The following summarizes the practices surveyed on the research date, not a new verification of external products.

| System | Public practice | Implication for finalization |
| --- | --- | --- |
| Codex | OpenAI's Codex prompting guide advises against forcing upfront plans, preambles, or status updates because these can stop a rollout before completion | Model-written plans/status are not completion evidence; the harness owns the execution loop and finalization |
| Claude Code | Provides `PreToolUse`, `PostToolUse`, and `Stop` hooks; `PostToolUse` occurs after successful tool execution | Lifecycle events are more reliable than prose; attach verification, auditing, and blocking to deterministic boundaries |
| Claude Code Subagents | Sub-agents have separate contexts, custom system prompts, specific tool access, and permissions; suitable for isolating extensive retrieval/log/file reads | Sub-agent output is evidence material, not the parent task's final conclusion; the parent must synthesize and verify it |
| Claude Code Plan Mode | Reads files and produces a plan without editing before approval | Planning and execution are distinct states; a completed plan is not a completed task |
| Cursor Plan Mode | Studies the codebase, asks clarifying questions, generates a reviewable plan, and waits for confirmation before building | Separating plan/review/build helps users distinguish plans from delivered results |
| OpenCode | Separates agents such as Build, Plan, Review, Debug, and Docs by tools and purpose; Plan analyzes without making changes | Capability boundaries reduce accidental transitions: planning capability does not imply completed execution |
| Eino ADK | Provides event-driven output, Runner callbacks, interrupts, checkpoints, and collaboration primitives including Supervisor and Plan-Execute; the latter uses Planner, Executor, and Replanner | These primitives fit the project; event-driven behavior needs an explicit finalization contract |

Primary references from the source guide:

- [OpenAI Codex Prompting Guide](https://developers.openai.com/cookbook/examples/gpt-5/codex_prompting_guide)
- [Claude Code Hooks](https://docs.anthropic.com/en/docs/claude-code/hooks)
- [Claude Code Subagents](https://docs.anthropic.com/en/docs/claude-code/sub-agents)
- [Claude Code Common Workflows](https://docs.anthropic.com/en/docs/claude-code/common-workflows)
- [Cursor Agent Best Practices](https://cursor.com/blog/agent-best-practices)
- [OpenCode Agents](https://opencode.ai/docs/agents/)
- [CloudWeGo Eino ADK](https://www.cloudwego.io/docs/eino/core_modules/eino_adk/)
- [CloudWeGo Eino ADK Patterns](https://www.cloudwego.io/docs/eino/overview/eino_adk0_1/)

## 3. General best practices

### 1. Establish a Finalization Contract

Every execution entry point should produce one structured finalization object. Only that object may authorize a final response. The following is a conceptual contract:

```go
type FinalizationDecision struct {
    Status             string   // in_progress | completed | blocked | failed | cancelled
    Finalizable        bool
    CompletionReason   string   // verified | user_cancelled | timeout | blocked | failed
    FinalText          string
    EvidenceVerified   bool
    EvidenceRefs       []string
    PendingToolRuns    []string
    PendingPlanSteps   []string
    PendingApprovals   []string
    MissingChecks      []string
}
```

Hard rules:

- When `Finalizable=false`, do not send a `response` as a successful terminal event. The project's blocked-response envelope is described below.
- When `Status=in_progress`, use process events such as `progress`, `planning`, `tool_*`, and `reasoning_chain`.
- `FinalText` must not be empty, but nonempty text alone does not authorize finalization.
- Any nonempty `PendingToolRuns`, `PendingPlanSteps`, or `PendingApprovals` prevents `completed`.
- When `EvidenceVerified=false`, do not present candidate output as a verified conclusion.

### 2. Define SSE event semantics

Recommended event layers:

| Event | Display location | May write `messages.content`? | Meaning |
| --- | --- | --- | --- |
| `progress` | Task state/timeline | No | Short progress update |
| `planning` | Execution details | No | Main-agent plan or interim judgment |
| `reasoning_chain` / `thinking` | Execution details | No | Reasoning/thinking summary |
| `tool_call` / `tool_result` | Execution details | No | Tool events |
| `eino_agent_reply` | Execution details | No | Material returned by a sub-agent |
| `finalization_check` | Execution details | No | Verifier result |
| `finalization_auto_continue` | Execution details | No | Verifier-triggered continuation, `contextInjection=false` |
| `response` | Main message bubble | Yes | Final-response treatment requires `data.finalized=true` |
| `done` | Stream closure | No | The stream ended; it does not establish task success |
| `error` / `cancelled` | Main message bubble or system notice | Yes, as terminal failure | Must include a reason |

### 3. Separate final candidates from final responses

The model can produce candidate conclusions. First route them into `final_candidate` or `planning`; the verifier then decides whether to promote them:

```text
assistant text
  -> candidate
  -> finalization gate
  -> response(finalized=true)
```

Avoid direct promotion:

```text
assistant text
  -> response
```

### 4. Verify at stop time

Following the lifecycle-hook approach, run deterministic checks when an agent run stops:

- Every tool call has a corresponding tool result.
- Background executions are terminal, or explicitly registered as still running with task state `in_progress` / `blocked`.
- Plan-Execute has no unexecuted required steps.
- Supervisor has no unsynthesized sub-agent results.
- An evidence-required policy has at least one queryable completed tool execution.

### 5. Treat sub-agent output as evidence

A sub-agent reply cannot directly become the user-facing final response. The main agent must:

- Deduplicate and reconcile conflicts.
- Rank evidence strength.
- Identify uncertainty.
- Check scope boundaries.
- Produce a readable delivery for the user.

### 6. Use prompts for guidance and code for enforcement

A prompt may say:

```text
Interim observations must be marked as progress, not final.
Do not produce a final answer until verification is complete.
```

Backend fields and state machines must still decide finalization. Otherwise, any conclusive-sounding model text could mislead the UI.

## 4. Current CyberStrikeAI implementation

The project has an explicit final gate:

- [internal/agentfinalizer/decision.go](../../internal/agentfinalizer/decision.go) is the single final-response decision contract.
- [internal/handler/finalization_helpers.go](../../internal/handler/finalization_helpers.go) writes decisions to `process_details` and calls `UpdateAssistantMessageFinalize` only when `Finalizable=true`.
- [internal/handler/eino_single_agent.go](../../internal/handler/eino_single_agent.go), [internal/handler/multi_agent.go](../../internal/handler/multi_agent.go), [internal/handler/workflow_integration.go](../../internal/handler/workflow_integration.go), and [internal/handler/batch_queue_executor.go](../../internal/handler/batch_queue_executor.go) invoke the finalizer at completion.
- [web/static/js/monitor.js](../../web/static/js/monitor.js) treats a `response` as final only when `data.finalized === true`. Nonfinalized text is shown as a failed finalization check.
- [web/static/js/webshell.js](../../web/static/js/webshell.js) marks streaming text as candidate output and switches to completed only on `response(finalized=true)`.
- [internal/agentfinalizer/decision_test.go](../../internal/agentfinalizer/decision_test.go) covers pending tools, HITL, empty output, missing required execution evidence, failed evidence that cannot justify finalization, and completed evidence that can.
- [internal/handler/finalization_auto_continue.go](../../internal/handler/finalization_auto_continue.go) automatically continues up to two segments when completed execution evidence is missing. It restores existing model traces without injecting new user/system text.

Core rules of the implemented contract:

1. **Model prose is only a candidate.**
   `RunResult.Response` must pass through `agentfinalizer.Decide` before becoming a final response.

2. **Every `response` carries terminal-state fields.**
   At minimum: `finalized`, `finalizable`, `status`, `completionReason`, `evidenceVerified`, `evidenceRefs`, `pendingExecutionIds`, and `missingChecks`.

3. **Unfinished tools block finalization.**
   Remaining `queued/running` executions produce `in_progress/pending_tool_executions`.

4. **Execution evidence is declared through structured policy.**
   The backend does not infer execution intent from user prose, assistant replies, or agent-mode names. Chat requests explicitly declare `finalization.requireExecutionEvidence`; execution entry points such as WebShell, Workflow, batch tasks, and robots pass policy at the call site. When required, at least one queryable `completed` tool execution must exist. Failed/cancelled records alone cannot justify finalization.

5. **Missing evidence triggers continuation before blocking.**
   Eino single-agent and main multi-agent paths automatically continue from existing traces on `missing_execution_evidence`, without injecting extra context. They record blocked only if evidence is still missing at the continuation limit.

6. **HITL and empty output do not finalize.**
   Workflows awaiting human approval, empty assistant text, and Eino empty-output placeholders produce blocked messages instead of success summaries.

## 5. Architecture for this project

The implemented flow is:

```text
Agent / Eino ADK events
  -> event normalizer
  -> process_details
  -> finalization verifier
  -> response(finalized=true)
  -> messages.content
```

### 1. A shared backend Finalizer

Responsibilities:

- Accept `RunResult` / candidate text, `mcpExecutionIds`, conversation and assistant-message IDs, HITL state, and orchestration mode.
- Query tool execution states in the database, distinguishing pending, completed, failed, and cancelled evidence.
- Return `FinalizationDecision`.
- Check state and evidence without invoking high-risk tools.

### 2. Terminal fields in RunResult

[internal/multiagent/runner.go](../../internal/multiagent/runner.go) includes terminal-state fields:

```go
type RunResult struct {
    Response             string
    MCPExecutionIDs      []string
    LastAgentTraceInput  string
    LastAgentTraceOutput string

    Finalized           bool
    Status              string
    CompletionReason    string
    EvidenceVerified    bool
    EvidenceRefs        []string
    PendingExecutionIDs []string
    MissingChecks       []string
}
```

### 3. Conditions for sending `response`

Single-agent, multi-agent, workflow, and batch completion paths use the same pattern. These snippets illustrate control flow rather than standalone compilable code:

```go
decision := h.finalizeAgentRunForDelivery(...)
if !decision.Finalizable {
    sendEvent("finalization_check", "Final-response requirements have not been met", decision)
    sendEvent("response", finalizationBlockedMessage(decision), finalizationResponsePayload(decision, extra))
    return
}

sendEvent("response", decision.FinalText, finalizationResponsePayload(decision, extra))
```

### 4. The frontend trusts only `finalized=true`

The `case 'response'` branch in [web/static/js/monitor.js](../../web/static/js/monitor.js) enforces the check:

```js
const responseFinalized = isFinalizedResponseData(responseData);
const bubbleText = responseFinalized
  ? resolvedResponseText
  : (event.message || 'Final-response requirements have not been met; no success conclusion is available.');
markAssistantFinalizationState(assistantIdFinal, responseData);
```

The same principle applies to WebShell. `response_delta` can preview live text labeled as output from an ongoing execution; only the final `response(finalized=true)` shows completion.

### 5. Final gates by mode

| Mode | Final candidate producer | Finalization decision | Required checks |
| --- | --- | --- | --- |
| Eino single-agent | Last assistant text from the agent | Finalizer | No pending tools; complete evidence references; terminal task state |
| Deep | Main-agent synthesis | Finalizer | Sub-agent results synthesized; sub-agent text cannot directly finalize; terminal tool states |
| Plan-Execute | Synthesis after Replanner ends | Replanner + Finalizer | Executor step output cannot finalize; plan steps completed or explicitly blocked |
| Supervisor | Supervisor `exit` / synthesis | Supervisor + Finalizer | Transfers returned; no unhandled specialist results; supervisor owns the final synthesis |

### 6. Evidence gates for security testing

For evidence-required scenarios such as security testing, WebShell, batch validation, Workflow, and multi-agent execution, final delivery should satisfy at least these governance requirements:

- Identify the target and authorization scope.
- Provide reviewable evidence references, such as tool execution IDs, request/response summaries, screenshot paths, command-output summaries, and fact/vulnerability record IDs.
- Validate identity or impact instead of relying only on marker text.
- Record results in the project blackboard or vulnerability store, or explicitly state that no project is bound and recording is unavailable.
- Clean up, roll back, or cancel high-risk actions, or explain why cleanup was not performed.
- Do not implicitly treat still-running scans, commands, WebShell, or C2 tasks as completed.

These are governance rules, not a requirement to expose sensitive exploitation details in final reports. Evidence summaries and internal references are sufficient.

## 6. Implementation status and future improvements

### P0: Prevent false finalization (implemented)

1. `FinalizationDecision` is in place.
2. Main Agent SSE `response` events carry fields such as `data.finalized/finalizable/status/completionReason`.
3. `monitor.js` and `webshell.js` distinguish candidate output from final responses using `finalized=true`.
4. `RunResult.Response` retains its compatible field name, but promotion is controlled by the finalizer. A future split into `CandidateResponse` / `FinalResponse` could reduce misuse.
5. Plan-Execute, Deep, Supervisor, and Eino Single use the unified handler completion gate.

### P1: Strengthen the evidence chain (partly implemented)

1. `mcp_execution:<id>` is the basic evidence reference.
2. `finalization_check` events expose pending executions and missing checks.
3. Execution entry points enable explicit evidence policy. When policy requires evidence but completed tool evidence is missing, the Eino main path continues without context injection and blocks only after reaching the limit.
4. Future work: finer-grained evidence references for `record_vulnerability`, `upsert_project_fact`, and blackboard records.
5. Future work: final report templates with conclusions, evidence, risks/uncertainty, and next actions.

### P2: Experience and observability (future work)

1. Display `in_progress / verifying / finalizing / completed / blocked` on task cards.
2. Add finalizer logs and metrics: false-block rate, missing-evidence types, and pending-tool counts.
3. Offer a “Continue verification” button that builds the next input from `FinalizationDecision.MissingChecks`.

## 7. Suggested acceptance tests

Include at least these regressions:

1. **Reasoning text cannot finalize.**
   Simulate a conclusive-sounding candidate in `reasoning_chain` without completed tool evidence in the current turn. The main bubble should show ongoing or blocked state, not success.

2. **Interim main-agent output cannot finalize.**
   Simulate “continue verification next” in `response_start/delta`. It should enter only timeline `planning`.

3. **Unfinished background tools cannot finalize.**
   A tool returns `execution_id` with state `running`. Even with a model summary, the run remains `in_progress`.

4. **Plan-Execute Executor output cannot finalize.**
   The Executor reports a successful breakthrough before the Replanner ends. `messages.content` must not finalize.

5. **Supervisor sub-agent output cannot finalize.**
   A sub-agent returns a definite conclusion before Supervisor `exit`. It should enter only `eino_agent_reply`.

6. **Final events require finalized metadata.**
   A legacy `response` without `finalized=true` should place candidate content only in details/warnings and show blocked state in the main message, without a successful final bubble.

7. **Failure and cancellation may be terminal.**
   `error` / `cancelled` may update the assistant message, but `completionReason` must be `failed` / `user_cancelled`, never successful completion.

## 8. Recommended defaults

Recommended policies for CyberStrikeAI:

```text
eino_single: suitable for lightweight tasks, with the final gate enabled
deep: recommended default for complex security testing
plan_execute: recommended for clear goals needing strict plan-execute-replan control
supervisor: use for routing among specialists, not as the general default
```

The governing rule is:

```text
messages.content may come only from FinalizationDecision.FinalText;
process_details may show the entire process;
the frontend treats only response(finalized=true) as a final response.
```
