# Deep assessments / 深度测评

「深度测评」是独立于聊天的后台测评工作台。新建草稿后配置目标、授权范围与排除项、已有材料、模型渠道、工具白名单、并发数、时间和 Token 预算，再启动任务。

## 使用

1. 在侧边栏打开「深度测评」，点击「新建测评」。可关联现有项目；模型渠道复用系统设置，不接收 API key。
2. 选择本次可用工具。默认沿用全局 HITL 的开关和免审批白名单；也可选「每次工具调用人工批准」。全局 `review_edit` 在此工作台按人工批准处理；如需修改参数，拒绝本次调用，暂停并补充方向后重试。工作台审批由人工完成。
3. 启动后，规划器读取目标、已完成步骤及结构化证据，提出至多四个小任务。Worker 在所选工具范围内执行，记录 MCP execution ID 和结果，再把新观察交还规划器。依赖项完成后才可执行后继步骤。
4. 「执行计划」显示进度及待审批工具的完整参数；「探索链路」显示目标、步骤依赖及证据；「运行日志」保留工具参数、输出和状态迁移。可暂停、补充线索、继续或停止。
5. 「证据与发现」可将观察保存至项目事实、资产库，或登记为待复核漏洞。项目事实为 `tentative`，候选漏洞先以 `info` 登记。操作需要对应模块的写权限，并检查关联项目的写权限。稳定的来源 ID 避免重复登记漏洞。
6. 随时下载 Markdown 报告，包含状态、预算、各方向结果、执行证据及未完成项。中断或预算耗尽也能交付已有结果。

范围文本会传给每轮规划和执行；它不是任意 Shell / 外部工具的网络隔离沙箱。确定性限制由所选工具白名单、实时 RBAC、MCP 调用拦截和所配置的审批策略提供。工具具体行为仍应符合所填写的授权范围。

## 长程推进机制

- 规划器和 Worker 独立调度：某个 Worker 完成后可立即释放执行槽推进依赖步骤；运行中保存了带证据的检查点，也会唤醒规划，不等待同批慢任务；规划模型响应慢时，其余已就绪的工作仍可继续。
- 规划器跨轮保存 `strategy` 路线；用带原因的 `retire` 淘汰走不通的待执行/受阻方向。运行中或等待审批的步骤不可淘汰。被淘汰分支显示为 `skipped`，原结果、证据、原因和覆盖缺口继续保留在页面及报告中。
- 上下文保留全部方向的简短索引，并优先提供最新观察。Worker 每轮刷新任务态势，较早的完整工具交互可从活动上下文移出，避免旧输出挤掉新发现。
- 内置 `assessment_memory` 支持本任务内关键词/步骤检索、按事件 ID 和字符偏移分页读取。每次最多返回 12,000 字符；工具结果单条最多留存 128,000 字符，超过时标记截断。更大原始输出使用 MCP 自身的输出文件机制。跨任务事件 ID 无法读取；跨 Worker 证据引用需实际读取并核验成功执行来源。
- 内置 `assessment_checkpoint` 原子保存中途进展和证据，不执行外部动作。停止、超时或异常恢复保留已保存的进展。记忆与检查点工具不需要外部动作审批。
- 模型返回格式错误或引用不成立时，最多进行两次无工具的结构化修复，仍计入 Token 预算；不因修复重新执行外部命令。预算预留被其他 Worker 暂占时等待结算；已经返回并付费的最终结果会保存。

## 生命周期与恢复

- 状态：`draft` → `running` / `awaiting_approval` → `completed`、`blocked`、`exhausted`、`failed`、`paused` 或 `stopped`。
- 前端只查询持久化状态与增量事件。离开页面、断开浏览器、删除聊天不会取消测评，也不会创建隐藏聊天。
- 暂停与停止先取消本轮执行，再回收本地进程和 MCP 异步 worker。未确认的远程取消会作为失败/缺口保留，不能当作成功结束。
- 服务启动时把遗留活跃任务恢复为暂停；当时正在执行的步骤标记为 `blocked`。它们可能已有部分外部效果，必须检查日志并明确重试。未执行的审批请求会保留，不自动批准或重放。
- 同一任务的重复启动被拒绝；单进程最多四个活跃测评，每个任务最多四个 Worker、100 个步骤、每步最多 20 个执行轮次（含最后的收尾轮，每次结构化输出的格式修复另有两次上限且计入预算）、50 轮规划。此实现面向单服务进程，不提供分布式调度。
- 删除活跃任务被拒绝。停止并收尾后可删除任务及其事件；已发布的事实、资产、漏洞保留来源信息。

## 预算及证据

- 任务时间预算使用运行期间的墙钟时间，**包含审批等待**；暂停期间不扣时。服务异常重启时，按原截止时间结算，随后暂停等待人工继续。
- 所有并发 Worker 共用持久化 Token 计数。请求前以序列化输入字节数、协议余量及输出上限做保守预留，响应后按提供商实际用量结算。提供商不返回 usage 或请求中断时保留保守计费；崩溃时未结算的预留也会记入已用预算。
- Token 预算控制是否允许下一次请求，不是提供商的账单硬上限；供应商隐藏用量、内部重试等可能影响最终账单。轮数/时长/Token 耗尽后停止新请求并保留部分结果。
- Worker 必须使用本步骤成功执行、或经任务内记忆读取并核验来源的真实 ID 支持事实、资产和候选发现；错误、被拦截或虚构的 ID 不得支持成功。仅规划完成或模型文字声明不能使任务完成。报告保留受阻方向，候选漏洞仍需人工复核。
- 规划上下文仅包含有界的任务结果与证据，不把全部执行轨迹不断拼入一个长聊天。

## API

所有端点均需登录。`assessment:read/write/delete` 控制测评资源，支持 owner/assigned/all 范围。启动另需 `agent:execute`；每次启动/继续绑定实际操作者身份，工具执行前重新解析其当前权限（协作者不会继承任务所有者的更高权限），继续经过原有 MCP authorizer 与 Tool Guard。审批另需 `hitl:write`。

| 方法 | 路径 | 作用 |
|---|---|---|
| GET | `/api/assessments/options` | 可选模型渠道和独立工具，不返回凭据 |
| GET / POST | `/api/assessments` | 分页列表（limit/offset）/ 创建草稿 |
| GET / DELETE | `/api/assessments/:id` | 查询 / 删除非活跃任务 |
| POST | `/api/assessments/:id/control/:action` | `start`、`resume`、`pause`、`stop`、`steer` |
| POST | `/api/assessments/:id/steps/:stepId/decision` | `{ "approved": true/false, "approval_id": "当前步骤返回的编号" }`，仅批准该次调用 |
| POST | `/api/assessments/:id/steps/:stepId/retry` | 明确重试受阻步骤，不自动重复外部操作 |
| GET | `/api/assessments/:id/events?after=0` | 按 ID 增量读取，单页最多 200 条且约 1 MiB |
| POST | `/api/assessments/:id/publish` | `{ "step_id": "…", "index": 0, "kind": "fact|asset|vulnerability", "revision": "展示证据的版本" }` |
| GET | `/api/assessments/:id/report` | 下载包含部分结果的 Markdown 报告 |

创建示例（工具名应从 options 中选择）：

```json
{
  "name": "本地测试环境测评",
  "goal": "检查指定本地测试应用并交付证据和覆盖缺口",
  "scope": "仅限本地隔离测试夹具，排除其他资产",
  "material": "测试说明及可用材料路径",
  "model": "default",
  "tools": ["selected-tool-name"],
  "approval_mode": "policy",
  "concurrency": 2,
  "token_budget": 200000,
  "remaining_seconds": 7200,
  "max_rounds": 12
}
```

新表 `assessments` / `assessment_events` 由现有增量 schema reconcile 创建；不修改既有聊天、项目或漏洞数据。私有模型 transcript 存储在任务 payload 中，对外任务查询不返回 transcript。数据库和备份应按现有运行数据要求管理。

## 验证

- `go test -race ./internal/assessment`：审批、暂停取消、恢复、并发预留、状态原子性、依赖验证、伪造证据拒绝、聊天隔离。
- `go test ./internal/handler -run '^TestAssessment'`：资源隔离、执行/审批权限、项目写范围、候选发布及去重。
- `go test ./internal/multiagent -run TestBackgroundCompletion`：本地模拟 OpenAI/Claude 服务，验证原生工具调用和 Token 用量。
- `node scripts/assessment-browser-smoke.mjs`：使用本地夹具 API 和真实前端模块，验证创建/选择、审批、图、XSS 转义、轮询保留输入、390px 布局；不调用模型或外部目标。可用 `CHROME_PATH` 指定 Chromium/Chrome。

本模块在 CSAI 的现有 Go / 前端结构内实现，没有复制 ARTEX 源码或引入其运行服务。

### 长程回归场景

`internal/assessment/longrun_test.go` 覆盖慢 Worker、慢规划器、替代路线与退休分支、多轮依赖链、历史长输出尾部检索、跨 Worker 证据、检查点恢复、格式修复和预算边界。均为确定性本地夹具，用于验证机制和避免回归；这些测试不等同于真实模型的漏洞发现率、成本或长时稳定性对标。

安全边界、已修复问题和回归场景见 [深度测评安全复查](DEEP_ASSESSMENT_SECURITY_REVIEW.md)。审批编号和证据版本为必填字段，旧客户端应刷新页面后提交；过期请求返回 409。
