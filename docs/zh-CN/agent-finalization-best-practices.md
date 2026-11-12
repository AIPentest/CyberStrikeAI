# Agent 最终回复治理最佳实践

[返回中文文档](README.md) | [English](../en-US/agent-finalization-best-practices.md)

调研日期：2026-07-28

本文聚焦一个具体问题：Agent 在工具调用、推理、计划或子代理协作尚未真正完成时，输出了一段“像结论”的自然语言，前端或编排层把它当作最终回复展示。结论先说清楚：成熟 Agent 系统不会用“最近一段 assistant 文本”判断任务完成，而是用运行时状态、工具状态、验证结果和显式终态事件共同决定是否 final。

## 一、核心结论

1. **最终回复是运行时事件，不是自然语言内容。**
   “已拿到”“下一步”“Huge breakthrough”这类文本只能作为候选观察或进展，不能作为完成信号。

2. **过程面和交付面必须隔离。**
   `thinking`、`reasoning_chain`、`planning`、`response_delta`、子代理回复、工具输出都属于过程面；只有通过 final gate 的 `response` / `final` 事件才能写入主消息气泡和 `messages.content`。

3. **复杂任务需要 verifier，而不是更长 prompt。**
   Prompt 可以提醒模型谨慎，但最终完成必须由代码层判断：是否仍有待执行工具、后台 execution、未完成计划步骤、未验证证据、未记录事实/漏洞、未清理或未说明不可清理。

4. **不同 agent 模式不同，但 final 治理原则一致。**
   单代理、Deep、Plan-Execute、Supervisor 都需要 final gate。区别只是 gate 的证据来源不同：单代理看工具轨迹，Deep 还要看子代理结果，Plan-Execute 要看 Replanner 的终止判断，Supervisor 要看 `exit` 与 supervisor 汇总。

## 二、成熟 Agent 的公开做法

| 系统 | 公开做法 | 对 final 治理的启发 |
|---|---|---|
| Codex | OpenAI 的 Codex prompting guide 建议不要在 prompt 中强行要求 upfront plan、preamble 或 status updates，因为这可能导致 rollout 未完成就停止。 | 不要把“模型自己说的阶段性计划/状态”当完成依据；agent harness 应负责执行循环和收尾。 |
| Claude Code | Claude Code 提供 `PreToolUse`、`PostToolUse`、`Stop` 等 hooks；`PostToolUse` 明确发生在工具成功执行之后。 | 生命周期事件比自然语言可靠。验证、审计、阻断应挂在确定的阶段边界上。 |
| Claude Code Subagents | 子代理有独立上下文、自定义系统提示、特定工具权限和独立权限；子代理适合隔离大量检索/日志/文件读取。 | 子代理输出是证据材料，不是主任务最终结论；主代理必须汇总、验收、再 final。 |
| Claude Code Plan Mode | Plan mode 先读文件并产出计划，获得批准前不编辑。 | 计划与执行是不同状态；计划完成不等于任务完成。 |
| Cursor Plan Mode | Cursor Plan Mode 会研究代码库、询问澄清问题、生成可审查计划，并等待用户确认后再构建。 | UI 层把 plan/review/build 拆开，用户不会把计划误认为最终交付。 |
| OpenCode | OpenCode 把 Build、Plan、Review、Debug、Docs 等 agent 分成不同工具权限与用途，Plan agent 只分析规划不做修改。 | 用 agent 能力边界降低误触发：能规划的 agent 不等于能执行完成。 |
| Eino ADK | Eino ADK 提供事件驱动输出、Runner 回调、中断、checkpoint，以及 Supervisor、Plan-Execute 等协作原语。Plan-Execute 由 Planner、Executor、Replanner 协作。 | 当前项目选型方向正确；需要把事件驱动能力进一步固化为 finalization contract。 |

主要参考：

- OpenAI Codex Prompting Guide: https://developers.openai.com/cookbook/examples/gpt-5/codex_prompting_guide
- Claude Code Hooks: https://docs.anthropic.com/en/docs/claude-code/hooks
- Claude Code Subagents: https://docs.anthropic.com/en/docs/claude-code/sub-agents
- Claude Code Common Workflows: https://docs.anthropic.com/en/docs/claude-code/common-workflows
- Cursor Agent Best Practices: https://cursor.com/blog/agent-best-practices
- OpenCode Agents: https://opencode.ai/docs/agents/
- CloudWeGo Eino ADK: https://www.cloudwego.io/docs/eino/core_modules/eino_adk/
- CloudWeGo Eino ADK Patterns: https://www.cloudwego.io/docs/eino/overview/eino_adk0_1/

## 三、通用最佳实践

### 1. 建立 Finalization Contract

所有执行入口统一产出一个结构化收尾对象，只有它允许触发最终回复。

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

硬规则：

- `Finalizable=false` 时禁止发送 `response` 终态事件。
- `Status=in_progress` 时只能发 `progress`、`planning`、`tool_*`、`reasoning_chain` 等过程事件。
- `FinalText` 不能为空，但非空不代表可以 final。
- `PendingToolRuns`、`PendingPlanSteps`、`PendingApprovals` 任一非空时不能 `completed`。
- `EvidenceVerified=false` 时不能把候选输出写成已验证结论。

### 2. 固定 SSE 事件语义

推荐事件分层：

| 事件 | 展示位置 | 可否写 `messages.content` | 说明 |
|---|---|---:|---|
| `progress` | 任务状态/时间线 | 否 | 简短进度 |
| `planning` | 执行详情 | 否 | 主代理计划、阶段性判断 |
| `reasoning_chain` / `thinking` | 执行详情 | 否 | 推理/思考摘要 |
| `tool_call` / `tool_result` | 执行详情 | 否 | 工具事件 |
| `eino_agent_reply` | 执行详情 | 否 | 子代理返回材料 |
| `finalization_check` | 执行详情 | 否 | verifier 结果 |
| `finalization_auto_continue` | 执行详情 | 否 | verifier 触发的工程续跑，`contextInjection=false` |
| `response` | 主消息气泡 | 是 | 只能在 `data.finalized=true` 时使用 |
| `done` | 关闭流 | 否 | 仅表示流结束，不表示任务成功 |
| `error` / `cancelled` | 主消息气泡或系统提示 | 是，终态失败类 | 必须带原因 |

### 3. 把“最终候选”与“最终回复”分开

模型可以输出候选结论，但候选结论必须先进入 `final_candidate` 或 `planning`，再由 verifier 决定是否提升：

```text
assistant text
  -> candidate
  -> finalization gate
  -> response(finalized=true)
```

不要这样做：

```text
assistant text
  -> response
```

### 4. Stop-time Verification

借鉴 Claude Code hook 思路，在 agent run 停止时做一次确定性检查：

- 所有工具调用都有对应 tool result。
- 后台 execution 都处于 terminal 状态，或被明确登记为仍在运行且任务状态为 `in_progress` / `blocked`。
- Plan-Execute 没有未执行的 required step。
- Supervisor 没有未汇总的子代理结果。
- 在 evidence-required 策略下，至少存在可查询到的 completed 工具执行证据。

### 5. 子代理输出只作证据

子代理返回不能直接成为用户最终回复。主代理必须完成：

- 去重和冲突合并。
- 证据强度排序。
- 不确定性标注。
- 范围边界确认。
- 用户可读交付。

### 6. Prompt 只做软约束，代码做硬约束

Prompt 中可以写：

```text
Interim observations must be marked as progress, not final.
Do not produce a final answer until verification is complete.
```

但真正决定 final 的必须是后端字段和状态机。否则模型只要生成一段像最终结论的自然语言，UI 仍可能误判。

## 四、CyberStrikeAI 当前落地状态

当前项目已经具备一套显式 final gate：

- [internal/agentfinalizer/decision.go](../../internal/agentfinalizer/decision.go) 是唯一的最终回复决策契约。
- [internal/handler/finalization_helpers.go](../../internal/handler/finalization_helpers.go) 负责把决策结果写入 `process_details`，并且只有 `Finalizable=true` 时才调用 `UpdateAssistantMessageFinalize`。
- [internal/handler/eino_single_agent.go](../../internal/handler/eino_single_agent.go)、[internal/handler/multi_agent.go](../../internal/handler/multi_agent.go)、[internal/handler/workflow_integration.go](../../internal/handler/workflow_integration.go)、[internal/handler/batch_queue_executor.go](../../internal/handler/batch_queue_executor.go) 均已在收尾处接入 finalizer。
- [web/static/js/monitor.js](../../web/static/js/monitor.js) 只把 `data.finalized === true` 的 `response` 当最终回复；未最终化文本会显示为最终回复检查未通过。
- [web/static/js/webshell.js](../../web/static/js/webshell.js) 将流式正文标记为候选输出，只有 `response(finalized=true)` 才切换为完成态。
- [internal/agentfinalizer/decision_test.go](../../internal/agentfinalizer/decision_test.go) 覆盖 pending tool、HITL、空输出、证据策略要求但缺执行证据、失败证据不能支撑最终化、完成态证据可 final 等回归场景。
- [internal/handler/finalization_auto_continue.go](../../internal/handler/finalization_auto_continue.go) 在缺 completed 执行证据时最多自动续跑 2 段；续跑只恢复已有模型轨迹，不向 agent 注入新的 user/system 文案。

当前契约的核心规则：

1. **模型自然语言只是 candidate。**
   `RunResult.Response` 不能直接升级为最终回复，必须经过 `agentfinalizer.Decide`。

2. **所有 `response` 事件必须携带终态字段。**
   至少包含 `finalized`、`finalizable`、`status`、`completionReason`、`evidenceVerified`、`evidenceRefs`、`pendingExecutionIds`、`missingChecks`。

3. **未完成工具会阻断 final。**
   `queued/running` 工具执行仍存在时，决策结果为 `in_progress/pending_tool_executions`。

4. **执行证据必须由结构化策略声明。**
   后端不从用户自然语言、助手回复或 agent mode 名称中推断执行意图。聊天请求通过 `finalization.requireExecutionEvidence` 显式声明；WebShell、Workflow、批量、机器人等执行入口由调用点显式传入 policy。policy 要求证据时，至少需要一个可查询到的 `completed` 工具执行记录；只有 failed/cancelled 记录不能支撑最终化。

5. **缺执行证据先工程续跑，再阻断。**
   Eino 单代理和 Eino 多代理主链路在 `missing_execution_evidence` 时会先通过已有 trace 自动续跑，不注入额外上下文；达到续跑上限后仍缺证据才写入 blocked。

6. **HITL 和空输出不会 final。**
   workflow 等待人工确认、空 assistant 文本、Eino 空输出占位均会写入阻断文案，而不是成功总结。

## 五、贴合当前项目的推荐架构

当前采用的链路是：

```text
Agent / Eino ADK events
  -> event normalizer
  -> process_details
  -> finalization verifier
  -> response(finalized=true)
  -> messages.content
```

### 1. 后端统一 Finalizer

职责：

- 接收 `RunResult` / 候选文本、`mcpExecutionIds`、会话与助手消息 ID、HITL 状态、编排模式。
- 通过数据库查询工具执行状态，识别 pending、completed、failed、cancelled 等证据状态。
- 返回 `FinalizationDecision`。
- 不调用高风险工具，只做状态和证据检查。

### 2. RunResult 终态字段

[internal/multiagent/runner.go](../../internal/multiagent/runner.go) 已扩展终态字段：

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

### 3. 发送 `response` 的条件

在单代理、多代理、工作流、批处理收尾处统一执行：

```go
decision := h.finalizeAgentRunForDelivery(...)
if !decision.Finalizable {
    sendEvent("finalization_check", "任务尚未达到最终回复条件", decision)
    sendEvent("response", finalizationBlockedMessage(decision), finalizationResponsePayload(decision, extra))
    return
}

sendEvent("response", decision.FinalText, finalizationResponsePayload(decision, extra))
```

### 4. 前端只信 `finalized=true`

在 [web/static/js/monitor.js](../../web/static/js/monitor.js) 的 `case 'response'` 中执行硬判断：

```js
const responseFinalized = isFinalizedResponseData(responseData);
const bubbleText = responseFinalized
  ? resolvedResponseText
  : (event.message || '任务尚未达到最终回复条件，暂不生成成功结论。');
markAssistantFinalizationState(assistantIdFinal, responseData);
```

WebShell 侧同理：`response_delta` 可以用于实时预览，但 UI 文案应标记为“执行中输出”，只有最终 `response(finalized=true)` 才显示为完成态。

### 5. 各模式 final gate

| 模式 | 谁可以产出最终候选 | 谁决定 final | 必须检查 |
|---|---|---|---|
| Eino 单代理 | 单代理最后助手文本 | Finalizer | 无 pending tool、证据引用完整、任务状态 terminal |
| Deep | 主代理汇总文本 | Finalizer | 子代理结果已汇总；子代理文本不能直接 final；工具状态 terminal |
| Plan-Execute | Replanner 结束后的汇总文本 | Replanner + Finalizer | Executor 单步输出不能 final；计划步骤完成或明确 blocked |
| Supervisor | Supervisor 的 `exit` / 汇总文本 | Supervisor + Finalizer | transfer 已返回；无未处理专家结果；最终由 supervisor 统一口径 |

### 6. 安全测试场景的证据 gate

安全测试、WebShell、批量验证、Workflow 和多代理执行等 evidence-required 场景，最终回复必须至少满足：

- 有明确目标和授权范围标识。
- 有可复核证据引用，例如工具 execution id、请求/响应摘要、截图路径、命令输出摘要、事实/漏洞记录 ID。
- 有身份或影响验证结果，而不是只凭 marker 文本判断。
- 已记录到项目黑板或漏洞库，或明确说明未绑定项目导致无法记录。
- 高风险动作已清理、回滚、取消，或明确说明未执行清理的原因。
- 仍在运行的扫描/命令/WebShell/C2 任务不能被隐式当作完成。

注意：这里的 gate 是治理规则，不要求最终报告暴露敏感利用细节；可以只给证据摘要和内部引用。

## 六、落地状态与后续增强

### P0：先修“误 final”（已落地）

1. 已引入 `FinalizationDecision`。
2. 主要 agent SSE `response` 事件已携带 `data.finalized/finalizable/status/completionReason` 等字段。
3. 前端 `monitor.js` 和 `webshell.js` 已按 `finalized=true` 区分候选输出和最终回复。
4. `RunResult.Response` 仍保留兼容字段名，但语义已由 finalizer 统一提升；后续可再拆成 `CandidateResponse` / `FinalResponse`，减少误用空间。
5. Plan-Execute / Deep / Supervisor / Eino Single 等模式均通过统一 handler 收尾 gate。

### P1：补证据链（部分落地）

1. 已用 `mcp_execution:<id>` 作为基础 evidence refs。
2. `finalization_check` 事件已展示 pending execution 与 missing checks。
3. 执行入口已启用显式 execution evidence policy；Eino 主链路在 policy 要求证据且缺少 completed 工具证据时先无注入续跑，达到上限后才阻断 final。
4. 后续建议：为 `record_vulnerability`、`upsert_project_fact`、项目黑板记录建立更细粒度 evidence refs。
5. 后续建议：最终报告模板固定包含“结论、证据、风险/不确定性、后续动作”。

### P2：体验和观测（后续增强）

1. 在任务卡片展示 `in_progress / verifying / finalizing / completed / blocked`。
2. 为 finalizer 加日志和指标：误拦截率、缺失证据类型、pending tool 数量。
3. 支持“继续验证”按钮，从 `FinalizationDecision.MissingChecks` 自动生成下一轮输入。

## 七、验收测试建议

至少加入这些回归测试：

1. **推理文本不 final**
   模拟 `reasoning_chain` 里出现看似完成的候选结论，但本轮没有 completed 工具执行证据；预期主消息气泡不显示成功结论，只显示执行中或阻断态。

2. **主代理阶段性输出不 final**
   模拟 `response_start/delta` 输出“下一步继续验证”；预期只进入 timeline `planning`。

3. **未完成后台工具不 final**
   工具返回 `execution_id` 且状态 `running`；即使模型给出总结，也只能 `in_progress`。

4. **Plan-Execute Executor 输出不 final**
   Executor 输出“突破成功”，但 Replanner 未结束；预期不触发 `messages.content` finalize。

5. **Supervisor 子代理输出不 final**
   子代理返回确定结论，Supervisor 未 `exit`；预期只进入 `eino_agent_reply`。

6. **最终事件必须带 finalized**
   前端收到旧格式 `response` 无 `finalized=true`；预期候选内容只进入详情/警告，主消息显示阻断态，不创建成功最终气泡。

7. **失败和取消可终态**
   `error` / `cancelled` 仍可更新助手消息，但 `completionReason` 必须是 `failed` / `user_cancelled`，不能伪装为成功完成。

## 八、推荐默认策略

对 CyberStrikeAI，建议默认策略是：

```text
eino_single：轻量任务可用，但 final gate 必须开启
deep：复杂安全测试默认推荐
plan_execute：目标明确、需要严格“规划-执行-重规划”的任务推荐
supervisor：多专家路由任务使用，不作为默认泛化模式
```

最终治理一句话：

```text
messages.content 只能来自 FinalizationDecision.FinalText；
process_details 可以展示所有过程；
前端只能把 response(finalized=true) 当最终回复。
```
