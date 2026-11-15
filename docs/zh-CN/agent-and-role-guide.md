# Agent 与角色指南

CyberStrikeAI 的 Agent 行为由三类资源共同决定：角色、子代理和 Skills。角色决定当前任务身份和可用工具；子代理决定多代理分工；Skills 提供可按需加载的专题知识与流程。

## 角色

角色文件位于 `roles/`，格式为 YAML。角色通常包含：

- 名称。
- 描述。
- 系统提示词。
- 可用工具列表。

设计原则：

- 专用角色只绑定必要工具。
- 提示词明确授权边界。
- 对高风险操作要求先说明影响并等待审批。
- 输出格式尽量稳定，便于报告和复盘。

示例方向：

- 信息收集。
- Web 应用扫描。
- API 安全测试。
- 云安全审计。
- 数字取证。
- 二进制分析。
- CTF。

## 单代理

单代理接口：

- `POST /api/agent`
- `POST /api/agent/stream`

适合：

- 快速问答。
- 单目标测试。
- 工具链较短的任务。
- 需要稳定上下文的交互式分析。

## 多代理模式

多代理接口：

- `POST /api/multi-agent`
- `POST /api/multi-agent/stream`

编排模式：

- `deep`：主代理拆解任务，按需调用子代理。
- `plan_execute`：先规划，再执行，必要时重规划。
- `supervisor`：主管代理根据进展转交不同子代理。

适合：

- 多阶段渗透测试。
- 大范围信息收集。
- 需要并行角色分工的分析。
- 长任务和批量任务。

## 子代理 Markdown

子代理位于 `agents/*.md`。Front matter 示例：

```yaml
---
name: Attack Surface Enumeration
id: attack-surface-enumeration
description: 枚举目标暴露面并整理可验证线索
tools:
  - subfinder
  - nmap
  - http-framework-test
bind_role: 信息收集
max_iterations: 300
---
```

正文写系统提示词。建议包含：

- 职责边界。
- 输入期望。
- 使用工具顺序。
- 输出格式。
- 禁止事项。

## 主代理

主代理可用：

- `agents/orchestrator.md`
- `agents/orchestrator-plan-execute.md`
- `agents/orchestrator-supervisor.md`

或在 front matter 中设置 `kind: orchestrator`。每种编排只应有一个主代理定义。

## 工具选择

工具选择顺序建议：

1. 角色绑定最小工具集。
2. 子代理按任务补充专用工具。
3. `tool_search` 动态解锁大量工具。
4. 高风险工具由 HITL 审批。

不要给所有角色默认绑定全部工具，否则上下文成本和误调用风险都会上升。

## 提示词建议

好的角色提示词应说明：

- 只在授权范围内行动。
- 先确认目标和约束。
- 对写入、删除、爆破、持久化、C2、WebShell 等操作请求审批。
- 输出可复核证据。
- 不确定时标注假设，不编造结果。

## 调试

如果 Agent 选错工具：

- 缩小角色工具列表。
- 增强工具 `short_description`。
- 开启或调整 `tool_search_always_visible_tools`。
- 在角色提示词中明确工具使用顺序。

如果多代理跑偏：

- 检查子代理描述是否过宽。
- 降低 `sub_agent_user_context_max_runes` 或明确任务输入。
- 优化 orchestrator 提示词。
- 查看过程详情和工具执行监控。

## 角色、子代理、Skill 的职责边界

三者经常混用，建议这样分工：

| 资源 | 解决的问题 | 不适合承载 |
| --- | --- | --- |
| Role | 当前对话的身份、语气、工具边界和授权规则 | 大量参考资料 |
| Agent Markdown | 多代理中的专业分工、交接格式和局部策略 | 一次性任务事实 |
| Skill | 可复用方法论、检查清单、模板和长参考资料 | 权限控制 |

如果把授权边界写进 Skill，而角色没有限制工具，Agent 仍可能在未加载 Skill 前选错工具。权限边界应优先放在 Role 和 HITL 中。

## 编排模式选择

| 模式 | 适合 | 不适合 |
| --- | --- | --- |
| `single` | 短任务、交互式分析、需要稳定上下文 | 多阶段大任务 |
| `deep` | 主代理动态拆分任务，子代理按需深入 | 需要严格步骤顺序的流程 |
| `plan_execute` | 有明确阶段、需要执行后复盘和重规划 | 用户频繁打断的即兴对话 |
| `supervisor` | 专家分工明确，需要主管路由 | 子代理定义含糊或过多 |

经验上，普通安全测试先用 `single`；复杂项目用 `plan_execute`；需要多个专业角色时用 `deep` 或 `supervisor`。

## 工具可见性如何影响行为

多代理里 `tool_search` 会让模型一开始只看见部分常驻工具。结果是：

- 工具页面显示可用，不代表模型当前上下文可见。
- `tool_search_always_visible_tools` 里的工具更容易被模型调用。
- 工具描述越清晰，越容易被搜索命中。
- 子代理自己的 `tools` 限制仍然很重要。

调试“Agent 为什么不用某工具”时，要同时检查角色工具、子代理工具、tool_search 配置和工具描述。

## 好的子代理输出格式

子代理不要只返回“已完成”。建议固定格式：

```markdown
## 结论

## 证据
- 命令/工具：
- 关键输出：
- 置信度：

## 风险

## 建议下一步
```

这样主代理才能继续编排，也方便攻击链和项目事实沉淀。

## 源码锚点

- Markdown Agent 解析：`internal/agents/markdown.go`
- 多代理准备：`internal/handler/multi_agent_prepare.go`
- 编排实现：`internal/multiagent/pi_runner.go`
- 工具搜索中间件：`internal/multiagent/pi_runner.go`
- 子代理上下文：`internal/multiagent/sub_agent_context_test.go`
