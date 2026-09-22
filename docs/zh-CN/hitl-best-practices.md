# 人机协同（HITL）最佳实践

[English](../en-US/hitl-best-practices.md)

人机协同用于在 Agent 执行工具前做审批拦截。它适合控制高风险操作、保留审计痕迹，并在人工审计压力过大时让审计 Agent 接管常规审批。

## 配置入口

Web 端进入 **系统设置 → 人机协同**，可配置：

- 全局默认审批方：`human` 或 `audit_agent`
- 审批引擎：`hitl.audit_backend`（`openai` 或 `typesafe`）
- 审计 Agent 专用模型：`hitl.audit_model`
- 已决策审计日志保留天数
- 免审批工具白名单：`hitl.tool_whitelist`
- 审批模式与审查编辑模式的审计提示词

对应的 `config.yaml` 示例：

```yaml
hitl:
  default_reviewer: human
  audit_backend: openai
  audit_model:
    provider: ""
    base_url: ""
    api_key: ""
    model: "" # 可填小模型；留空复用默认 AI 通道的模型
  retention_days: 90
  tool_whitelist: [read_file, ls, glob, grep, tool_search, get_project_fact, list_project_facts, search_project_facts, list_vulnerabilities, get_vulnerability, get_asset, query_assets, list_knowledge_risk_types, get_tool_execution, wait_tool_execution, batch_task_list, batch_task_get, manage_webshell_list, c2_event, c2_file]
```

`audit_backend` 为二选一：`openai`（默认）走兼容协议聊天模型，用提示词输出 JSON；`typesafe` 走 TypeSafe Jev。自定义审批策略会作为 `operatorPolicy` 编进结构化问题，内置破坏性规则仍是硬底线。Jev 不能改参，审查编辑模式下也只返回通过/拒绝。内置默认提示词与 Jev 问题重复，不会再复制进 state。

`audit_model` 在 openai 后端可以只填一部分，空字段继承默认 AI 通道。typesafe 后端的 `api_key` 必填且**不会**复用主模型密钥；`base_url` 留空为 `https://api.typesafe.ai`，`model` 留空为 `jev-latest`。

## 推荐审批策略

### 1. 默认人工，逐步放权

刚开始建议：

- `default_reviewer: human`
- 仅把明显只读工具加入 `tool_whitelist`
- 对写文件、执行命令、C2 任务、WebShell 操作保持人工审批

运行一段时间后，观察审计日志，把重复、低风险、误报少的工具加入白名单。

### 2. 人工审不过来时，用小模型接管常规审批

当待审批积压明显时，可以切换为：

```yaml
hitl:
  default_reviewer: audit_agent
  audit_model:
    model: "your-small-reviewer-model"
```

建议让小模型处理：

- 只读查询
- 信息收集
- 端口与服务扫描
- 目录枚举
- 无破坏性的验证命令

仍建议人工处理：

- 删除、覆盖、清空数据
- 修改权限、密码、账号
- 持久化、横向移动、C2 高风险任务
- 对生产目标的写入操作

### 3. 用提示词定义组织策略

审计 Agent 的提示词应该写成策略，而不是泛泛地说“谨慎审批”。建议明确：

- 默认放行哪些低风险操作
- 必须拒绝哪些破坏性操作
- 哪些情况需要人工升级
- 审查编辑模式下允许怎样收窄参数

示例策略片段：

```text
常规信息收集、只读查询、端口扫描默认 approve。
涉及删除文件、清空数据库、修改账号权限、写入持久化后门、停止关键服务时必须 reject。
若目标范围超出用户授权范围，应 reject。
审查编辑模式下，可将路径、目标、命令参数收窄后 approve，但不得扩大攻击面。
```

OpenAI 协议后端把这段文字当聊天提示词。TypeSafe Jev 把它当作 `operatorPolicy` 编进结构化问题；内置破坏性规则仍是硬底线，Jev 不会改参。留空或等于内置默认时，Jev 只用内置问题，不再把长提示词复制进 state。

### 4. 白名单只放稳定低风险工具

白名单工具会跳过审批，因此要保守维护。推荐放：

- `read_file`
- `ls`
- `glob`
- `grep`
- `tool_search`
- 项目与漏洞查询：`get_project_fact`、`list_project_facts`、`search_project_facts`、`list_vulnerabilities`、`get_vulnerability`
- 资产与知识元数据查询：`get_asset`、`query_assets`、`list_knowledge_risk_types`
- 执行与任务状态查询：`get_tool_execution`、`wait_tool_execution`、`batch_task_list`、`batch_task_get`
- 本地管理元数据查询：`manage_webshell_list`、`c2_event`、`c2_file`

上述内置 MCP 查询仍受 RBAC 和资源范围约束；白名单只跳过 HITL 审批，不扩大访问权限。`list_dir` 仅在实际工具名为该值时有效；Eino 文件系统的目录列表工具名是 `ls`。

不建议直接全局白名单：

- 任意 shell 执行工具
- 文件写入/删除工具
- C2 任务工具
- WebShell 命令执行工具
- 会向目标或外部服务发起请求的“只读”工具，例如 `webshell_file_read`、`webshell_file_list` 和 `search_knowledge_base`
- 同一工具名同时包含读写 action 的复合工具，例如 `c2_session`、`c2_listener`、`c2_profile`、`c2_task_manage`

## 模式选择

| 模式 | 适用场景 |
|------|----------|
| 关闭 | 本地实验、完全信任工具链 |
| 审批模式 | 只需要通过/拒绝 |
| 审查编辑 | 希望审计 Agent 收窄参数后放行 |

如果你已经配置了小模型审计，推荐从 **审批模式** 开始。只有当你希望 AI 自动收窄路径、目标范围或命令参数时，再开启 **审查编辑**。

## 运维建议

- 定期查看 **人机协同 → 审计日志**，调整白名单和提示词。
- 高风险环境下保持 `default_reviewer: human`，只让审计 Agent 辅助给出建议。
- 小模型审批失败时默认保守拒绝，这是预期行为。
- 修改 `hitl.audit_model` 后先在页面点击 **测试审计模型**。
- 对生产、客户、真实业务系统操作前，应保留人工最终确认。
