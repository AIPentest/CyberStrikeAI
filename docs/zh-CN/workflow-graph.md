# CyberStrikeAI 工作流使用说明

[English](../en-US/workflow-graph.md)

本文档说明 **工作流（Workflow）** 的完整使用方式：如何在画布上搭建流程、配置各类型节点、在节点之间传递数据，以及如何将流程绑定到角色并自动运行。

---

## 一、在哪里使用工作流

1. 登录 CyberStrikeAI Web 端  
2. 左侧导航进入 **工作流**  
3. 在左侧列表选择已有流程，或新建流程  
4. 在中央画布拖拽、连线、配置节点  
5. 填写流程 **ID**、**名称**、**描述** 后点击 **保存**

保存后的流程可在 **角色管理** 中绑定到某个角色。绑定后，用户与该角色对话时会按流程图自动执行（`workflow_policy: auto`）。

---

## 二、画布基本操作

| 操作 | 说明 |
|------|------|
| 添加节点 | 点击画布上方节点类型按钮（开始、工具、Agent、条件、审批、输出、结束） |
| 连线 | 点击 **连线**，依次点击源节点和目标节点；再次点击 **连线** 退出连线模式 |
| 选中元素 | 单击节点或连线，右侧显示 **节点属性** |
| 删除选中 | 点击 **删除选中** 删除当前节点或连线 |
| 自动布局 | 点击 **自动布局** 整理节点位置 |
| 试运行 | 点击 **试运行** 使用安全 dry-run 验证数据流；工具、Agent、审批不会真实执行 |
| 删除流程 | 点击 **删除** 删除整个流程定义 |

**硬性规则：** 每个流程至少包含 **1 个开始节点** 和 **1 个输出节点**；开始节点不能有入边，输出 / 结束节点不能有出边。保存时前端和后端都会执行严格校验。

---

## 三、自然语言生成草稿

工作流页面提供 **用自然语言创建** 入口。用户描述一句安全作业目标后，平台会生成一个可编辑草稿，并返回结构化审计结果：

- 只生成草稿，不会自动保存、试运行或真实执行工具。
- 服务端接口为 `POST /api/workflows/generate-draft`，权限为 `workflow:write`。
- 生成结果包含 `graph`、`meta`、`capabilities`、`audit` 和 `stats`，前端应用到画布后仍走现有保存校验。
- 高风险语义（执行脚本、隔离、封禁、删除、利用等）会标记 `high_risk`，并默认插入 HITL 审批或 `requires_human_confirmation`。
- 工具能力按已有工具列表匹配；未匹配到时降级为 Agent 草稿，并在 `audit.missing_fields` / `audit.assumptions` 中提示需要补配置。
- 如果服务端生成不可用，前端会使用本地确定性兜底生成器，继续给出可编辑草稿和风险提示。

示例请求：

```http
POST /api/workflows/generate-draft
Content-Type: application/json

{
  "prompt": "对目标资产做端口扫描，如果发现高危端口就创建漏洞任务并通知负责人审批，最后输出报告",
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

## 四、执行模型（先理解再配置）

工作流按 **有向图** 执行，引擎从 **开始** 节点出发，沿连线依次运行下游节点。

每次运行会维护一份内部状态，模板变量 `{{...}}` 从这里取值：

| 内部状态 | 模板前缀 | 含义 |
|----------|----------|------|
| `inputs` | `{{inputs.xxx}}` | 流程启动时的输入（用户消息、会话 ID 等） |
| `lastOutput` | `{{previous.xxx}}` | **上一个刚执行完** 的节点的输出 |
| `outputs` | `{{outputs.xxx}}` | 全局 **命名变量池**（由节点的「输出变量名」写入） |
| `nodeOutputs` | `{{节点ID.xxx}}` | 指定节点 ID 的完整输出对象 |
| `metrics` | 运行详情中查看 | 节点耗时、工具调用数、可收集到的 token / cost 等指标 |

### 3.1 `previous` 是什么？

`{{previous.output}}` 表示 **紧邻的上一个执行节点** 的 `output` 字段。

- 每执行完一个节点，引擎都会更新 `lastOutput`
- **不是**「画布上画线的上游」，而是 **实际执行顺序上的上一步**

示例：

```text
开始 → Agent A → Agent B
```

Agent B 的 `{{previous.output}}` = Agent A 的输出。

但若中间有条件节点：

```text
开始 → Agent A → 条件 → Agent B
```

Agent B 的 `{{previous.output}}` = **条件节点** 的输出（`true` / `false`），**不是** Agent A 的结果。

如果一个节点有 **多个上游节点** 同时连入，`previous` 会先按该节点的 **汇聚策略** 生成：

| 汇聚策略 | 含义 | 适合场景 |
|----------|------|----------|
| `all_merge` | 合并所有上游输出，`previous.output` 为数组 | 默认推荐，综合多路结果 |
| `last_by_canvas` | 按画布顺序取最后一个上游输出 | 明确只采用一路结果 |
| `first_non_empty` | 取第一个非空输出 | 多路兜底 |
| `fail_fast` | 任一上游失败则中止当前节点 | 关键链路、审批前置、安全检查 |

### 3.2 `outputs` 是什么？

`outputs` 是引擎在运行过程中维护的 **命名变量注册表**。

当 Agent、工具、输出 等节点配置了 **输出变量名**（字段 `output_key`）后，节点执行成功会把结果写入：

```text
outputs["你填的变量名"] = 节点输出内容
```

之后 **任意下游节点** 都可以通过 `{{outputs.变量名}}` 引用，不要求两个节点直接相连。

示例：

- Agent A 的 **输出变量名** 填 `agent_result1`
- Agent B 的 **输入来源** 填 `{{outputs.agent_result1}}`

即使 A 和 B 之间隔着条件节点，B 仍能拿到 A 的输出。

### 3.3 什么时候用 `previous`，什么时候用 `outputs`？

| 场景 | 推荐写法 |
|------|----------|
| 两个节点 **直连**，只取上一步结果 | `{{previous.output}}` |
| 中间有其他节点（条件、工具、审批等） | `{{outputs.变量名}}` |
| 需要引用 **更早** 的某个节点结果 | `{{outputs.变量名}}` 或 `{{节点ID.output}}` |
| 条件判断要基于某 Agent 的输出 | `{{outputs.变量名}} != ""` |
| 读取用户最初输入 | `{{inputs.message}}` |

**记忆口诀：**

- `previous` = 上一步（链式、紧邻）
- `outputs` = 按名字取（跨节点、可回溯）

---

## 五、模板语法

### 4.1 基本格式

```text
{{变量路径}}
```

支持字母、数字、下划线、点、连字符，例如：

```text
{{previous.output}}
{{outputs.agent_result1}}
{{inputs.message}}
{{inputs.conversationId}}
{{previous.matched}}
{{node-abc123.output}}
```

### 4.2 可用路径一览

| 路径 | 说明 |
|------|------|
| `{{inputs.message}}` | 用户消息（开始节点输入） |
| `{{inputs.conversationId}}` | 会话 ID |
| `{{inputs.projectId}}` | 项目 ID |
| `{{previous.output}}` | 上一节点主输出 |
| `{{previous.matched}}` | 上一条件节点的匹配结果（`true` / `false`） |
| `{{outputs.变量名}}` | 某节点注册过的命名输出 |
| `{{节点ID.output}}` | 指定节点 ID 的 `output` 字段 |
| `{{previous.kind}}` | 上一节点输出类型，如 `agent` / `tool` / `condition` |
| `{{previous.status}}` | 上一节点状态，如 `completed` / `failed` / `simulated` |

节点输出会保留兼容字段（如 `output`、`matched`），同时带有结构化字段：

```json
{
  "kind": "agent",
  "node_id": "node-2",
  "node_type": "agent",
  "status": "completed",
  "output": "..."
}
```

### 4.3 条件表达式

条件节点和连线条件支持比较、文本匹配、正则、逻辑组合与安全 JSONPath/JQ 路径读取：

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

规则：

- 支持 `==`、`!=`、`>`、`>=`、`<`、`<=`
- 支持 `contains` 子串匹配与 `matches` 正则匹配
- 支持简单 `&&` / `||`
- 支持 `jsonpath(value, "$.path")` 与 `jq(value, ".path")` 的**安全路径子集**，仅做字段读取，不执行任意脚本
- 比较两侧会自动去掉首尾空格和引号
- 无比较符时，非空且不为 `false` / `0` / `null` 视为真
- 保存时会静态校验表达式格式、JSONPath/JQ 路径和正则语法

### 4.4 嵌套字段绑定

节点的字段绑定除 `output`、`message` 等普通字段外，也支持 JSONPath/JQ 风格路径：

| 绑定配置 | 含义 |
|----------|------|
| `from=previous, field=$.status` | 从上一节点输出对象读取 `status` |
| `from=outputs, field=$.scan.severity` | 从命名输出中读取嵌套字段 |
| `from=node-1, field=.output.items[0]` | 从指定节点输出读取数组元素 |

---

## 六、节点类型与配置

### 5.1 开始（start）

流程入口，将用户输入注入 `inputs`。

| 字段 | 说明 | 默认值 |
|------|------|--------|
| 输入变量 | 逗号分隔的输入键名 | `message, conversationId, projectId` |

开始节点输出包含：`output`、`message`、`conversationId`、`projectId`。

### 5.2 Agent（agent）

调用大模型 Agent 处理任务，支持多种运行模式。

| 字段 | 说明 | 默认值 |
|------|------|--------|
| Agent 模式 | `single` / `deep` / `plan_execute` / `supervisor` | `single` |
| 输入来源 | 上游数据的模板表达式 | `{{previous.output}}` |
| 节点指令 | 本节点要完成的任务描述 | 空 |
| 输出变量名 | 写入 `outputs` 的键名 | `agent_result` |
| 汇聚策略 | 多上游进入本节点时如何生成 `previous` | `all_merge` |

**消息拼装规则：**

- 仅填 **节点指令**：直接把指令发给 Agent  
- 仅填 **输入来源**：生成「请基于上游节点输出继续处理：…」  
- 两者都填：合并为「上游输入 + 节点指令」

Agent 节点执行后：

- `previous.output` 更新为本节点响应文本  
- 若配置了 **输出变量名**，同时写入 `outputs[输出变量名]`
- Agent 节点通过原生 DAG 调度 Pi 运行时；节点状态与检查点由 Go 保存

### 5.3 工具（tool）

调用已启用的 MCP 工具。

| 字段 | 说明 | 默认值 |
|------|------|--------|
| MCP 工具 | 工具名称（必填） | — |
| 参数模板 | JSON，支持 `{{...}}` 模板 | `{}` |
| 超时秒数 | 可选 | 空 |
| 汇聚策略 | 多上游进入本节点时如何生成 `previous` | `all_merge` |

示例参数模板：

```json
{"target": "{{inputs.message}}", "port": "443"}
```

若配置了 **输出变量名**，工具返回结果会写入 `outputs`。

### 5.4 条件（condition）

根据表达式计算分支，输出 `matched`（`true` / `false`）。

| 字段 | 说明 | 默认值 |
|------|------|--------|
| 条件表达式 | 支持 `{{...}}` 与 `==` / `!=` | `{{previous.output}} != ""` |
| 汇聚策略 | 多上游进入本节点时如何生成 `previous` | `all_merge` |

**分支规则：**

- 从条件节点连出的 **第一条线** 默认为 **「是」** 分支（`matched == true`）
- **第二条线** 默认为 **「否」** 分支（`matched == false`）
- 连线标签可写 `是` / `否`（或 `yes` / `no`、`true` / `false`）辅助识别
- 第三条及以后的出边需在 **连线条件** 中自定义表达式

连线条件示例（选中连线后在右侧配置）：

```text
{{previous.matched}} == "true"
{{previous.matched}} == "false"
```

### 5.5 审批（hitl）

人工确认检查点。流程运行到该节点前会通过原生 Workflow 检查点暂停，等待 API 或监控面板审批后恢复。

| 字段 | 说明 | 默认值 |
|------|------|--------|
| 审批提示 | 支持模板 | `请审批该步骤是否继续执行` |
| 提示字段绑定 | 留空审批提示时，从绑定字段读取说明 | `previous.output` |
| 审批方 | `human` / `audit_agent` | `human` |
| 汇聚策略 | 多上游进入本节点时如何生成 `previous` | `all_merge` |

HITL 等待信息会记录：

- `checkpointId`
- interrupt `beforeNodes`
- resume target / address / path
- resume payload schema（`approved`、`comment`）

### 5.6 输出（output）

将流程最终结果写入 `outputs`，供结束摘要和对话展示使用。

| 字段 | 说明 | 默认值 |
|------|------|--------|
| 输出变量名 | 必填，最终结果的键名 | `result` |
| 变量来源 | 模板表达式，决定写入的值 | `{{previous.output}}` |
| 固定输出值 | 可选，填写后覆盖变量来源 | 空 |
| 汇聚策略 | 多上游进入本节点时如何生成 `previous` | `all_merge` |

**注意：** 输出节点是流程的「出口」，不应再有出边。

### 5.7 结束（end）

可选节点，用于生成结束摘要模板（角色绑定流程中较少单独使用）。

| 字段 | 说明 | 默认值 |
|------|------|--------|
| 结束摘要模板 | 支持 `{{outputs.xxx}}` | `{{outputs.result}}` |
| 汇聚策略 | 多上游进入本节点时如何生成 `previous` | `all_merge` |

---

## 七、连线配置

选中 **连线** 后，右侧可配置 **连线条件**。

| 场景 | 示例 |
|------|------|
| 普通节点后的过滤 | `{{previous.output}} == "ok"` |
| 条件节点「是」分支 | `{{previous.matched}} == "true"` |
| 条件节点「否」分支 | `{{previous.matched}} == "false"` |

若不填连线条件：

- 非条件节点：连线始终放行  
- 条件节点：按出边顺序自动分配是/否分支

---

## 八、完整示例：跨条件节点传递 Agent 输出

### 7.1 流程结构

```text
开始 → Agent（生成初始值）→ 条件 → Agent（加工）→ 输出
                              ↘ 否 → 输出
```

### 7.2 节点配置

**Agent 1（第一个 Agent）**

| 字段 | 值 |
|------|-----|
| 节点指令 | 只输出 `123333333` |
| 输出变量名 | `agent_result1` |

**条件**

| 字段 | 值 |
|------|-----|
| 条件表达式 | `{{outputs.agent_result1}} != ""` |

**Agent 2（第二个 Agent）**

| 字段 | 值 |
|------|-----|
| 输入来源 | `{{outputs.agent_result1}}` |
| 节点指令 | 在输入基础上加 100，然后输出 |
| 输出变量名 | `agent_result` |

**输出**

| 字段 | 值 |
|------|-----|
| 输出变量名 | `result` |
| 变量来源 | `{{outputs.agent_result}}` |

### 7.3 常见错误

| 错误配置 | 原因 |
|----------|------|
| Agent 2 输入来源写 `{{previous.output}}` | `previous` 指向条件节点，得到的是 `true`/`false`，不是 Agent 1 的文本 |
| 未给 Agent 1 填输出变量名 | `outputs.agent_result1` 不存在，下游取到空值 |
| 条件表达式写 `{{previous.output}}` | 判断的是开始节点或上一节点的输出，而非 Agent 1 的命名变量 |

---

## 九、绑定角色并运行

### 8.1 在角色管理中绑定

1. 进入 **角色管理**，编辑或新建角色  
2. 选择绑定的 **工作流** ID  
3. 策略设为 `auto`（默认：有 `workflow_id` 时自动执行）  
4. 保存角色

也可在角色 YAML 中直接配置：

```yaml
name: 工作流测试
workflow_id: "1233"
workflow_version: latest
workflow_policy: auto
```

### 8.2 运行效果

用户选择该角色并发送消息后：

1. 引擎加载对应 `graph_json` 并按图执行  
2. 对话页可看到 `workflow_start`、`workflow_node_start`、Agent 推理等进度事件  
3. 流程结束后返回摘要，列出 `outputs` 中所有命名输出

若未配置输出节点或条件未命中，`outputs` 可能为空，摘要会提示检查输出节点与分支。

---

## 十、调试、试运行与复盘

### 9.1 安全试运行（dry-run）

画布工具栏点击 **试运行**，输入一条测试消息即可模拟执行流程。

dry-run 的安全边界：

- `start` / `condition` / `output` / `end` 会按真实逻辑计算
- `tool` 不会真实调用 MCP，只返回 `[dry-run] tool call skipped`
- `agent` 不会真实调用模型，只返回 `[dry-run] agent execution skipped`
- `hitl` 不会暂停，只模拟通过

相关 API：

```http
POST /api/workflows/dry-run
```

请求体：

```json
{
  "graph": { "nodes": [], "edges": [], "config": {} },
  "inputs": { "message": "ping" }
}
```

响应包含：

- `outputs`
- `nodeOutputs`
- `trace`
- `metrics`
- `replayScript`

### 9.2 运行详情与 replay

运行后可查询完整节点执行轨迹：

```http
GET /api/workflows/runs/{runId}
```

返回 `run` 与 `nodeRuns`，每个节点记录包含：

- input 快照
- output 快照
- status / error
- started_at / finished_at
- `duration_ms`

复盘接口：

```http
GET /api/workflows/runs/{runId}/replay
```

该接口只根据已保存的 `nodeRuns` 生成步骤，不会重新执行工具或 Agent。

### 9.3 指标（metrics）

工作流会尽量累计：

- `node_count`
- `duration_ms`
- `tool_call_count`
- Agent progress 中可收集到的 `prompt_tokens` / `completion_tokens` / `total_tokens` / `cost`

token 与成本是否存在取决于底层模型/Agent 事件是否上报 usage。

---

## 十一、保存前校验规则

保存时系统会自动检查：

| 规则 | 说明 |
|------|------|
| 必须有开始节点 | 至少 1 个 `start` |
| 必须有输出节点 | 至少 1 个 `output`，且填写输出变量名 |
| 连线合法 | 源/目标节点存在，不能自环 |
| 开始节点无入边 | 开始节点不能被指向 |
| 输出 / 结束节点无出边 | 输出 / 结束节点后不应再连线 |
| 非开始节点必须有入边 | 避免孤岛节点 |
| 非输出 / 结束节点必须有出边 | 避免执行到死路 |
| 无环路 | Workflow 编排必须是 DAG |
| 可达性 | 所有节点必须能从开始节点到达，并能最终到达 output/end |
| 工具节点 | 必须选择 MCP 工具；参数 JSON 必须合法；超时必须为正整数 |
| Agent 节点 | 必须填写节点指令或输入绑定；必须填写输出变量名 |
| 条件节点 | 必须填写表达式；需要 1～2 条出边；分支必须标记是/否且不能重复 |
| 连线条件 | 表达式、正则、JSONPath/JQ 路径必须通过静态校验 |
| 汇聚策略 | 必须是 `all_merge` / `last_by_canvas` / `first_non_empty` / `fail_fast` |

---

## 十二、排错指南

| 现象 | 可能原因 | 处理建议 |
|------|----------|----------|
| 下游拿到空值 | 上游未配置输出变量名 | 给上游 Agent/工具填 **输出变量名**，下游用 `{{outputs.xxx}}` |
| 下游拿到 `true`/`false` | 误用 `{{previous.output}}`，上一步是条件节点 | 改用 `{{outputs.xxx}}` |
| 条件总走「否」 | 表达式与真实输出格式不一致 | 检查 Agent 输出是否带引号、换行；用 `!= ""` 先验证 |
| 流程无最终输出 | 未命中输出节点所在分支 | 检查条件分支连线；确保至少一条路径到达 **输出** 节点 |
| 角色对话未跑流程 | 角色未绑定或未启用 | 确认 `workflow_id`、`workflow_policy: auto`、流程 `enabled: true` |
| 工具节点失败 | 参数 JSON 不合法或工具未启用 | 检查参数模板；在 MCP 中启用对应工具 |
| 保存失败提示分支非法 | 条件节点出边未标记是/否或重复 | 选中连线，设置条件分支为 `true` 或 `false` |
| 多上游结果不符合预期 | 汇聚策略不合适 | 根据场景改为 `all_merge` / `first_non_empty` / `last_by_canvas` / `fail_fast` |
| 嵌套字段取不到 | JSONPath/JQ 路径不符合安全子集 | 使用 `$.a.b[0]` 或 `.a.b[0]`，不要用通配符/递归/表达式 |

---

## 十三、最佳实践

1. **命名规范**：为每个需要被引用的节点设置有意义的输出变量名，如 `scan_result`、`parsed_targets`，避免都叫 `agent_result`。  
2. **跨节点传参优先用 `outputs`**：只要中间可能插入条件、工具、审批节点，就应用命名变量。  
3. **`previous` 仅用于直连**：A → B 且无中间节点时，`{{previous.output}}` 最简洁。  
4. **条件判断引用源数据**：判断 Agent 输出时用 `{{outputs.xxx}}`，不要用 `{{previous.output}}`（除非条件紧跟在目标 Agent 之后）。  
5. **每条路径都要有出口**：确保「是」「否」分支最终都能到达 **输出** 节点（或你期望的终点）。  
6. **多上游节点显式选择汇聚策略**：综合结果用 `all_merge`，兜底用 `first_non_empty`，关键链路用 `fail_fast`。  
7. **嵌套 JSON 用 JSONPath/JQ 安全路径**：例如 `jsonpath({{previous.output}}, "$.status") == "ok"`。  
8. **保存前先 dry-run**：用简单消息验证数据传递和分支，再绑定角色真实执行。

---

## 十四、相关代码位置（开发者参考）

| 模块 | 路径 |
|------|------|
| 执行引擎 | `internal/workflow/runner.go` |
| 原生 DAG / checkpoint / HITL | `internal/workflow/native_runtime.go` |
| 图校验 | `internal/workflow/validation.go` |
| 表达式 / JSONPath / 汇聚 | `internal/workflow/expression.go`、`jsonpath.go`、`join.go` |
| dry-run / replay 数据 | `internal/workflow/dry_run.go`、`internal/handler/workflow_run.go` |
| 画布前端 | `web/static/js/workflows.js` |
| 流程 API | `internal/handler/workflow.go` |
| 角色绑定 | `internal/config/config.go`（`workflow_id` 字段） |
