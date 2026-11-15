# Pi 单代理与多代理

CyberStrikeAI 仅使用 Pi 运行时。Go 负责业务、权限、工具执行与结果验收；Pi SDK 负责模型循环、会话、流式事件和压缩。部署与配置见 [Pi 运行时](pi-runtime.md)。

## 入口与模式

- 单代理：`POST /api/agent`、`POST /api/agent/stream`，模式值 `single`。
- 多代理：`POST /api/multi-agent`、`POST /api/multi-agent/stream`，模式 `deep`、`plan_execute`、`supervisor`。
- 后台回执：`POST /api/agent/runs`、`POST /api/multi-agent/runs`。
- 机器人、批量任务及工作流 Agent 节点复用同一 Pi 调度入口。

旧 `/api/eino-agent` 路由和 `eino_single` 模式只作为输入兼容，执行仍进入 Pi；不存在旧引擎或切换选项。

## 分工

| 模式 | 行为 |
| --- | --- |
| single | 一个 Pi 会话调用被允许的工具完成当前工作 |
| deep | 主代理通过 task 委派专家，核对返回结果并综合交付 |
| plan_execute | 维护计划，逐步委派 executor，根据结果调整后续步骤，受规划轮数与 Agent 轮数限制 |
| supervisor | 按专家职责委派 task／transfer_to_agent，汇总后通过 exit 交付 |

主代理与专家定义继续位于 `agents/` 的 Markdown 文件。主代理提示文件为 `orchestrator.md`、`orchestrator-plan-execute.md`、`orchestrator-supervisor.md`。子代理绑定角色可以缩小工具范围，不得扩大主代理的工具集合。

## 工具与能力

工具执行统一回到 Go。MCP 和本机文件／Shell 工具经过相应权限和 HITL；执行记录与最终证据关联。工具较多时，Pi 原生 tool_search 先检索元数据，再按需加载工具定义。

`multi_agent.skills` 控制 skill 与本机工具；`multi_agent.runtime` 保存工具检索、任务板、模型重试、备用渠道及输出预算设置。旧配置键在读取时迁移，保存采用新键。TaskCreate／TaskUpdate／TaskGet／TaskList 与 write_todos 使用现有对话任务板。

## 生命周期

- 补充指令通过 steering 在工具批次后的安全点进入后续推理。
- 立即取消同时中止模型请求和当前宿主工具。
- 中断后的未提交工具结果标为不确定，继续前核对执行记录，不能自动当作成功或直接重放。
- 完成状态由宿主结果验收决定。达到迭代上限、存在待完成工具或缺少执行证据时，不得升级为任务完成。
- Pi 会话和压缩记录落盘；已绑定项目的自动工作摘要另存为 tentative 的续接记录。
