# Eino 多代理改造说明（DeepAgent）

本文档记录 **Eino 单代理（ADK）** 与 **多 Agent（CloudWeGo Eino `adk/prebuilt`）** 的改造范围、进度与后续事项。原生 ReAct 执行路径已移除。

## 总体结论

- **改造已可用于生产试验**：流式对话、MCP 工具桥接、配置开关、前端模式切换均已落地。
- **入口策略**：**单代理** 走 `/api/eino-agent/stream`；多代理走 `/api/multi-agent/stream`，请求体 **`orchestration`** 指定编排。模式定位按 Eino ADK 最佳实践区分：**Deep** 适合复杂安全测试与 task 子代理协作；**Plan-Execute** 适合目标明确的规划 → 执行 → 重规划闭环；**Supervisor** 适合多个专业子代理动态分派的专家路由场景。机器人默认 `robot_default_agent_mode: eino_single`；批量队列默认 `eino_single`，多代理模式需 `multi_agent.enabled`。

## 已完成项

| 项 | 说明 |
|----|------|
| 依赖与代理 | `go.mod` 直接依赖 `github.com/cloudwego/eino`、`eino-ext/.../openai`；`go.mod` 注释指导 **GOPROXY**（如 `https://goproxy.cn,direct`）。 |
| 配置 | `config.yaml` → `agent.max_iterations` 为全局 ReAct 上限（主/子代理统一）；`multi_agent`：`enabled`、`robot_use_multi_agent`、`sub_agents`（含可选 `bind_role`）、`eino_skills`、`eino_middleware` 等；结构体见 `internal/config/config.go`。 |
| Markdown 子代理 / 主代理 | 在 `agents_dir` 下放 `*.md`。**子代理**：供 Deep `task` 与 `supervisor` `transfer`。**主代理（按模式分离）**：`orchestrator.md`（或 `kind: orchestrator` 的**单个**其他 .md）→ **Deep**；固定名 `orchestrator-plan-execute.md` → **plan_execute**；固定名 `orchestrator-supervisor.md` → **supervisor**。正文优先于 YAML：`multi_agent.orchestrator_instruction`、`orchestrator_instruction_plan_execute`、`orchestrator_instruction_supervisor`；plan_execute / supervisor **不会**回退到 Deep 的 `orchestrator_instruction`。皆空时 plan_execute / supervisor 使用代码内置默认提示。管理：**Agents → Agent管理**；API：`/api/multi-agent/markdown-agents*`。 |
| MCP 桥 | `internal/einomcp`：`ToolsFromDefinitions` + 会话 ID 持有者，执行走 `Agent.ExecuteMCPToolForConversation`。 |
| 编排 | `internal/multiagent/runner.go`：单代理、Deep 主代理与子代理、Supervisor 主代理与子代理均使用 `TypedChatModelAgent[*schema.AgenticMessage]` / `deep.NewTyped[*schema.AgenticMessage]`，再经 adapter 接回现有 `adk.Runner` / TurnLoop / SSE 边界；`plan_execute` 的 Executor 也使用 Agentic typed agent，经 adapter 挂入 Eino 官方 `planexecute.Config` 的经典外层契约。 |
| HTTP | `POST /api/multi-agent`（非流式）、`POST /api/multi-agent/stream`（SSE）；路由**常注册**，是否可用由运行时 `multi_agent.enabled` 决定（流式未启用时 SSE 内 `error` + `done`）。 |
| 会话准备 | `internal/handler/multi_agent_prepare.go`：`prepareMultiAgentSession`（含 **WebShell** `CreateConversationWithWebshell`、工具白名单与单代理一致）。 |
| 单 Agent | `internal/agent` 为 MCP/工具层（`ToolsForRole`、`ExecuteMCPToolForConversation`）；单代理编排走 `RunEinoSingleChatModelAgent`（`/api/eino-agent*`）。 |
| 前端 | 主聊天 / WebShell：**Eino 单代理**（`/api/eino-agent/stream`）与 **Deep / Plan-Execute / Supervisor**（`/api/multi-agent/stream` + `orchestration`）；`multi_agent.enabled` 控制多代理选项是否展示；设置页已暴露 Eino 原生模型 retry/failover 参数，运行态时间线展示 `eino_model_retry` / `eino_model_failover` / `eino_usage_summary`。 |
| 流式兼容 | Eino 单/多代理与 Web UI 共用 `handleStreamEvent`：`conversation`、`progress`、`response_start` / `response_delta`、`thinking` / `thinking_stream_*`、`tool_*`、`response`、`done` 等。 |
| 批量任务 | 队列 `agentMode` 为 `deep` / `plan_execute` / `supervisor` 时子任务带对应 `orchestration` 调用 `RunDeepAgent`；旧值 `multi` 与「`agentMode` 为空且 `batch_use_multi_agent: true`」均按 `deep`。 |
| 配置 API | `GET /api/config` 返回 `multi_agent` 标量与 Eino middleware 可运营字段（含用户输入预算、`model_retry_*`、`model_failover_*`、常驻工具白名单）；`PUT /api/config` 可更新这些字段且不覆盖 `sub_agents`。 |
| OpenAPI | 多代理路径说明已更新（流式未启用为 SSE 错误事件）。 |
| 机器人 | `ProcessMessageForRobot` 按 `robot_default_agent_mode`（默认 `eino_single`）调用 `RunEinoSingleChatModelAgent` 或 `RunDeepAgent`。 |
| 预置编排 | 聊天 / WebShell：`POST /api/multi-agent*` 请求体 `orchestration`：`deep` \| `plan_execute` \| `supervisor`（缺省 `deep`）。`deep` 使用 task 子代理协作；`plan_execute` 不构建 YAML/Markdown 子代理；`plan_execute_loop_max_iterations` 仍来自配置；`supervisor` 至少需一个子代理，只有一个子代理时会提示其专家路由空间有限。 |
| Eino 中间件 | `multi_agent.eino_middleware`（可选）：`patchtoolcalls`（默认开）、`toolsearch`（按阈值拆分 MCP 工具列表）、`plantask`（需 `eino_skills`）、`reduction`（大工具输出截断/落盘）、`checkpoint_dir`（Runner 断点）、`model_retry_*` / `model_failover_channels`（单代理、Deep、Supervisor 与 `plan_execute` Executor 均走 Eino 原生 AgenticModel retry/failover）、`deep_output_key` / `task_tool_description_prefix`（Deep 与 supervisor 主代理共享其中模型容错与 OutputKey）。Agentic 路径的 patchtoolcalls / toolsearch / plantask / reduction / filesystem / skill / summarization tail 均使用 Eino v0.9.14 官方 typed middleware。**`plan_execute`**：Executor 使用 Agentic typed agent，经 adapter 保持官方 Plan/UserInput/ExecutedSteps session contract；Planner/Replanner 仅 summarization tail + prompt 预算截断，不跑 MCP 工具链，因当前 Eino 官方 Planner/Replanner 构造仍是经典 ChatModel 接口。 |
| AgenticMessage 边界 | `internal/multiagent/eino_agentic_message.go` 提供 `schema.Message` ↔ `schema.AgenticMessage` 的文本、reasoning、函数工具调用、函数工具结果映射；`internal/multiagent/eino_agentic_event_adapter.go` 将 `TypedAgentEvent[*schema.AgenticMessage]` 转成现有 SSE/MCP drain 消费的 `AgentEvent[*schema.Message]`，覆盖 assistant/tool result/stream/error，并保证 Agentic 流式 `FunctionToolResult` 的 tool name / call id 可被现有 `einoToolResultEventHandler` 回放、持久化与展示；`internal/multiagent/eino_agentic_agent_adapter.go` 将 `TypedAgent[*schema.AgenticMessage]` 包装成现有 `adk.Agent` / `adk.ResumableAgent`，使 checkpoint resume、Runner、TurnLoop 与 SSE 边界无需重写；`internal/multiagent/eino_agentic_chat_model_agent.go` 组装 `TypedChatModelAgent[*schema.AgenticMessage]`；`internal/multiagent/eino_single_runner.go`、Deep、Supervisor 生产路径已切到 AgenticModel；`internal/multiagent/eino_model_resilience.go` 已接入 `agenticopenai.ChatModel` 工厂、Agentic 原生 ModelRetry 与 ModelFailover 配置，OpenAI/OpenAI-compatible 配置可实际构建 `model.AgenticModel`；`internal/multiagent/eino_middleware.go` 已提供 Agentic 官方泛型 patchtoolcalls / toolsearch / plantask / reduction prepender；`internal/multiagent/eino_agentic_summarize.go` 已把项目领域化摘要策略接入 Eino 原生 `summarization.NewTyped[*schema.AgenticMessage]`；`internal/multiagent/eino_agentic_chat_model_tail_middleware.go` 提供 protocol-neutral 的 Agentic typed tail（system 合并、continuation 去重、typed summarization、model-facing trace、model-output guard）。 |
| TurnLoop 接入桥 | 主聊天 Eino 单代理 / 多代理流式路径已通过 `WithAgentTurnLoopInterruptRegistrar` 启用 Eino 原生 `adk.TurnLoop` bridge：`internal/multiagent/eino_run_trace.go` 为每次 Eino run 生成统一 `runId`，自动注入所有 Eino progress data，并让 `einoobserve` callbacks 复用同一 id；`internal/multiagent/eino_turn_loop_runtime.go` 封装会话 item、`PushInterruptContinue`（`WithPreemptTimeout(AnySafePoint, ...)`）、idle stop 与 checkpoint 参数；`internal/multiagent/eino_turn_loop_iterator_starter.go` 统一 runtime 创建、旧 registrar 清理、interrupt/cancel registrar 绑定、idle stop 与 TurnLoop exit 转发；`internal/multiagent/eino_runner_iterator_starter.go` 统一 Runner fallback 的原生 cancel option、runtime cancel registrar、fresh run checkpoint id 与 resume cancel option；`internal/multiagent/eino_checkpoint_runtime.go` 统一 checkpoint_dir 规整、store 创建、checkpoint id 与启停日志；`internal/multiagent/eino_checkpoint_resume_handler.go` 统一 Runner checkpoint preflight、resume 进度事件、resume 调用与失败回退；`internal/multiagent/eino_initial_iterator_start_handler.go` 统一 checkpoint resume 后的 Runner / TurnLoop fresh start 选择与 TurnLoop takeover 进度事件；`internal/multiagent/eino_turn_loop_event_bridge.go` 将 TurnLoop `OnAgentEvents` 显式转发给现有 SSE/MCP drain，并吞掉 preempt 产生的框架级 cancel；`internal/multiagent/eino_run_runtime_session.go` 统一 Runner/TurnLoop 启动、checkpoint resume、fresh restart、native cancel registrar、run recovery、stream error、completion/cancellation cleanup 与 final result builder；`internal/multiagent/eino_run_error_handler.go` 统一不可重试 run error 的 pending flush、cancel fallback、timeout、iteration limit 与 error progress；`internal/multiagent/eino_run_recovery_handler.go` 统一 Eino 原生 retry、context overflow、run-level transient retry 与 fatal fallback 的恢复策略分派；`internal/multiagent/eino_stream_error_handler.go` 统一 assistant stream recv error 的 interrupt-continue、`eino_stream_error`、retry 与 partial result 分派；`internal/multiagent/eino_run_cancellation_handler.go` 统一 iter ctx cancel / interrupt-continue 时的 pending flush、进度事件与 partial result；`internal/multiagent/eino_run_completion_handler.go` 统一正常完成时 orphan pending flush 与 checkpoint cleanup；`internal/multiagent/eino_run_event_drain.go` 统一 Runner/TurnLoop 共享的 runMessages、assistantOutput、pending tool calls、stream id、tool-result / assistant-stream / materialized-message handlers 装配；`internal/multiagent/eino_run_result_builder.go` 统一 partial/final `RunResult` 构建、model-facing trace 持久化、plan_execute executor 输出优先级与 exit fallback；`internal/multiagent/eino_pending_tool_calls.go` 抽出 pending tool-call drain state；`internal/multiagent/eino_tool_result_progress_emitter.go` 统一 `tool_result` 的 pending 关联、去重、background wait 展示、execute stdout 抑制与 filesystem monitor 更新；`internal/multiagent/eino_tool_result_event_handler.go` 统一 ADK 流式/非流式 tool result 到 runMessages 与 `tool_result` 事件的适配；`internal/multiagent/eino_assistant_stream_event_handler.go` 统一 ADK 流式 assistant 输出到主助手、子代理回复、reasoning 与 tool-call fragments 的 drain；`internal/multiagent/eino_materialized_message_event_handler.go` 统一 ADK 非流式 message 到 runMessages、reasoning、主/子回复、tool-call 与 tool-result drain；`internal/multiagent/eino_stream_tool_call_completion_handler.go` 统一流式 assistant tool-call fragments 的合并、pending 标记与 runMessages 持久化；`internal/multiagent/eino_execute_stdout_suppressor.go` 抽出 execute 输出重复抑制状态；`internal/multiagent/eino_message_stream_receiver.go` 统一 schema.Message stream 的 ctx cancel / EOF / nil chunk / recv error 处理；`internal/multiagent/eino_main_response_stream_emitter.go` 统一主助手 `response_start/response_delta` 与 SSE accumulated 字段；`internal/multiagent/eino_main_assistant_stream_handler.go` 统一主助手流式正文缓冲、execute 复述去重、最终输出记录与 runMessages 追加；`internal/multiagent/eino_main_assistant_complete_handler.go` 统一主助手非流式正文的 execute 复述去重、UI 发射与最终输出记录；`internal/multiagent/eino_sub_agent_reply_emitter.go` 统一子代理 `eino_agent_reply(_stream_*)` 事件；`internal/multiagent/eino_reasoning_stream_emitter.go` 统一 reasoning chain 流式/非流式事件与 Claude signature 展示过滤；`internal/multiagent/eino_assistant_output_accumulator.go` 统一最后助手输出与 plan_execute executor 展示文本记录；`internal/multiagent/eino_run_message_accumulator.go` 统一事件流累积消息，作为异常重试/partial result 的 Runner/TurnLoop 共享上下文来源；`internal/multiagent/eino_run_progress_tracker.go` 收敛主/子代理轮次、工具调用去重与 pending 标记状态；`internal/multiagent/model_output_recovery_compat.go` 仅保留旧 recovery marker 的历史兼容拦截，默认链路不再预改写模型工具调用；`internal/multiagent/eino_context_overflow_retry.go` 统一上下文超限后的单次激进压缩、restart 上下文选择、SSE 事件与结构化日志；`internal/multiagent/eino_transient_run_retry_handler.go` 统一 run-level 临时错误退避、pending flush、`eino_run_retry` 事件与重试耗尽日志；`internal/multiagent/eino_native_model_retry_progress.go` 统一 Eino 原生 `WillRetryError` 的 `eino_model_retry` 事件与结构化日志。“中断并继续”会优先 push 用户补充进 TurnLoop，未注册或 push 失败时回退到 Eino 原生 cancel + trace 续跑链路。context overflow 压缩、run-level transient retry 的 fresh restart 入口已统一为 TurnLoop-aware，避免异常续跑退回普通 Runner。 |

## 进行中 / 待办（ backlog ）

| 优先级 | 项 | 说明 |
|--------|----|------|
| P1 | **plan_execute Planner/Replanner typed 化跟进** | Executor 已切到 Agentic typed agent；当前 Eino 官方 Planner/Replanner 构造仍接收经典 ChatModel，因此规划/重规划侧保留经典消息契约。待官方提供 typed planner/replanner 或迁移到自研 typed plan-execute root 后再统一切换。 |
| P2 | **观测与计费成本** | run 级 `runId` 已统一注入 Eino SSE/progress 事件并复用于 callbacks；`eino_run_usage_accumulator.go` 已从 `schema.ResponseMeta.Usage` 聚合 run 级 model calls、prompt/completion/total、cached 与 reasoning tokens，final/partial result 前通过 `eino_usage_summary` 发到前端时间线。后续可继续补模型单价与 cost 字段。 |
| P3 | **测试** | 增加 `internal/multiagent` 与 einomcp 的集成测试（mock model 或录屏回放）。 |

## 关键文件索引

- `internal/multiagent/runner.go` — DeepAgent / plan_execute / supervisor 组装与事件循环  
- `internal/multiagent/eino_orchestration.go` — PlanExecute 根节点与 Agentic Executor 中间件栈（`buildPlanExecuteAgenticExecutorHandlers`）  
- `internal/handler/multi_agent.go` — SSE 与（同步）HTTP  
- `internal/handler/multi_agent_prepare.go` — 会话准备（含 WebShell）  
- `internal/einomcp/` — MCP → Eino Tool  
- `config.yaml` — `multi_agent` 示例块  
- `web/static/js/chat.js` — 模式选择与 stream URL  
- `web/static/js/webshell.js` — WebShell AI 流式 URL 与主聊天模式对齐  
- `web/static/js/settings.js` — 多代理标量、Eino 模型 retry/failover 设置保存  

## 版本记录

| 日期 | 说明 |
|------|------|
| 2026-03-22 | 首版：Eino DeepAgent + stream + 前端开关 + GOPROXY 脚本。 |
| 2026-03-22 | 补充：进度文档、`prepareMultiAgentSession` 抽取、WebShell 后端对齐、`POST /api/multi-agent`、OpenAPI `/api/multi-agent*` 条目。 |
| 2026-03-22 | 路由常注册、流式未启用 SSE 错误、`robot_use_multi_agent`、设置页持久化、WebShell/机器人多代理、`bind_role` 子代理 Skills/tools。 |
| 2026-03-22 | `tool_result.toolCallId`、`ReasoningContent`→思考流、`batch_use_multi_agent` 与批量队列 Eino 执行。 |
| 2026-03-22 | 流式工具事件：按稳定签名去重，避免每 chunk 刷屏与「未知工具」；最终回复去重相同段落；内置调度显示为 `task`。 |
| 2026-03-22 | `agents/*.md` 子代理定义、`agents_dir`、合并进 `RunDeepAgent`、前端 Agents 菜单与 CRUD API。 |
| 2026-03-22 | `orchestrator.md` / `kind: orchestrator` 主代理、列表主/子标记、与 `orchestrator_instruction` 优先级。 |
| 2026-04-19 | 主聊天「对话模式」：原生 ReAct 与 Deep / Plan-Execute / Supervisor；`POST /api/multi-agent*` 请求体 `orchestration` 与界面一致；`config.yaml` / 设置页不再维护预置编排字段（机器人/批量默认 `deep`）。 |
| 2026-04-21 | 移除角色 `skills` 与 `/api/roles/skills/list`；`bind_role` 仅继承 tools；Skills 仅通过 Eino `skill` 工具按需加载。 |
| 2026-07-06 | **最佳实践对齐**：Deep / Plan-Execute / Supervisor 改为中性适用场景描述；Supervisor 标为专家路由特定场景并收紧 transfer/exit 约束；plan_execute Executor 明确为遵循官方 session contract 的自定义 ChatModelAgent，保留 middleware 并补类型保护。 |
| 2026-07-02 | **plan_execute Executor 中间件对齐**：早期经典 `ExecPreMiddlewares` 与 Deep 主代理同源并补回归测试；后续已由 Agentic typed Executor 装配取代。 |
| 2026-06-02 | **移除原生 ReAct**：删除 `/api/agent-loop*` 执行入口与 `AgentLoopWithProgress`；统一 Eino ADK（单代理 `/api/eino-agent*`，多代理 `/api/multi-agent*`）；任务 cancel/tasks API 保留。 |
| 2026-08-14 | **Eino 原生模型容错运营化**：设置页、`/api/config` public/update、YAML 写回与回归测试补齐 `model_retry_*` / `model_failover_*` 配置链路。 |
| 2026-08-14 | **Agentic 原生摘要接入**：新增 `summarization.NewTyped[*schema.AgenticMessage]` builder 与 Agentic typed tail 插槽，保留项目摘要预算、transcript、用户意图 ledger、fact index 与 transient retry 策略。 |
| 2026-08-14 | **流式工具参数保护**：流式 tool-call fragments 合并后复用 model-output guard 阈值，危险 arguments 统一替换为 recovery marker，并同步到 UI `tool_call`、pending 跟踪与 runMessages。 |
| 2026-08-14 | **Agentic 流式工具结果回放**：`recvSchemaMessageStream` 聚合 tool name / call id，Agentic streaming `FunctionToolResult` 经 adapter 后可被现有 tool-result drain 完整展示、持久化并用于 MCP display 更新。 |
| 2026-08-14 | **本地 MCP 审计回归**：新增真实 `mcp.Server` 覆盖，验证 Eino ADK filesystem begin/finish 复用同一 execution id，并在 `tool_result` 后把模型可见正文更新回 MCP display result。 |
| 2026-08-14 | **Agentic 上下文压缩端到端**：新增 typed agent 测试，证明 `summarization.NewTyped[*schema.AgenticMessage]` 会先压缩历史，再把摘要、ledger 与 transcript 提示送入业务 AgenticModel。 |
| 2026-08-14 | **Agentic 主路径切换**：`RunEinoSingleChatModelAgent`、Deep 主/子代理、Supervisor 主/子代理与 plan_execute Executor 切到 `TypedChatModelAgent[*schema.AgenticMessage]` / `deep.NewTyped[*schema.AgenticMessage]`；官方 typed patchtoolcalls / toolsearch / plantask / reduction / filesystem / skill / summarization 与 Agentic retry/failover 进入生产装配；adapter 补 `adk.ResumableAgent`，保持 Runner checkpoint、TurnLoop、SSE 与 MCP drain 边界稳定。 |
| 2026-09-22 | **Agentic builtin action 状态对齐**：Eino v0.9.14 官方 `ExitTool` / `transfer_to_agent` 仍通过 `SendToolGenAction` 写 `typedState[*schema.Message]`。Agentic 主路径改为兼容 exit/transfer 的 typed state 写入，并拦截 HITL 拒绝 transfer 时的 returnDirectly 清理，避免工具执行报 `cannot find state with type ... AgenticMessage`。 |
