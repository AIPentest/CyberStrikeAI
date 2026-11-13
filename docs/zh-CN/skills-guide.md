# Skills 指南

Skills 用于给 Agent 提供可按需加载的专题能力、流程说明、模板和参考资料。它适合承载稳定方法论，而不是一次性任务输入。

## 目录结构

默认目录：

```yaml
skills_dir: skills
```

推荐结构：

```text
skills/
  api-security-testing/
    SKILL.md
  ssrf-testing/
    SKILL.md
  cyberstrike-pi-demo/
    SKILL.md
    REFERENCE.md
    assets/
```

每个 Skill 至少包含 `SKILL.md`。

## SKILL.md

`SKILL.md` 使用 YAML front matter：

```markdown
---
name: ssrf-testing
description: SSRF 漏洞识别、验证、绕过和修复建议流程
---

# SSRF Testing

当任务涉及服务端请求伪造、URL 回调、云元数据访问或内网探测时使用本技能。
```

`description` 很重要，Agent 会根据它判断何时加载。

## 渐进式披露

Pi Skills 支持按需加载。配置：

```yaml
multi_agent:
  skills:
    disable: false
    filesystem_tools: true
    skill_tool_name: skill
```

Agent 初始只看到技能名称和描述，真正需要时再调用 `skill` 读取详情，减少上下文占用。

## 适合写成 Skill 的内容

- 某类漏洞测试流程。
- 安全审计 checklist。
- 报告模板。
- 工具组合方法。
- 内部规范。
- 常见误报判断。

不适合：

- 临时目标信息。
- API Key、密码、Cookie。
- 经常变化的扫描结果。
- 大量无结构原始日志。

## 附属文件

Skill 可以带附属文件，如 `REFERENCE.md`、模板、字典或示例。`SKILL.md` 中应说明何时读取这些文件。

建议：

- 主文件保持短而清晰。
- 参考资料按主题拆分。
- 大文件只在必要时读取。

## 与角色绑定

角色可以提示 Agent 使用某类 Skill；Skill 也可以通过页面管理和角色形成绑定关系。建议：

- 通用技能保持不绑定，按描述自动触发。
- 高风险技能绑定到专用角色。
- 同类技能不要描述过度重叠。

## 开发建议

Skill 内容结构：

1. 触发场景。
2. 目标和边界。
3. 操作步骤。
4. 工具建议。
5. 输出格式。
6. 风险和禁止事项。
7. 参考资料。

写法要让 Agent 能执行，而不是只给人阅读。

## 排错

Skill 没被使用：

- `description` 过窄或过模糊。
- 任务没有触发关键词。
- `multi_agent.skills.disable: true`。
- Skill 文件 front matter 格式错误。

Skill 读取太多：

- 拆分附属文件。
- 在 `SKILL.md` 中明确“只有在需要 X 时读取 Y”。
- 删除重复内容。

## Skill 设计深水区

Skill 的核心价值不是“让 Agent 知道一个概念”，而是让 Agent 在正确时机拿到一套可执行的程序。写 Skill 时要特别关注触发条件和退出条件。

推荐结构：

```markdown
## When to use
明确触发场景。

## Preconditions
需要用户提供什么、目标必须满足什么。

## Procedure
按步骤执行，每步说明工具、输入和判断标准。

## Stop conditions
什么情况下停止、升级审批或转人工。

## Output
最终结果格式。
```

## 反模式

| 反模式 | 后果 | 改法 |
| --- | --- | --- |
| 描述过泛：`用于安全测试` | 几乎所有任务都触发 | 写具体漏洞、场景、信号 |
| 内容像百科 | Agent 不知道下一步做什么 | 改成流程和决策树 |
| 把敏感配置写进 Skill | 泄露和误用 | 用运行时配置或用户输入 |
| 一个 Skill 装所有内容 | 读取成本高，召回混乱 | 按漏洞/任务拆分 |
| 没有停止条件 | Agent 可能持续扩大范围 | 写明何时停止和审批 |

## Skill 与知识库的区别

- Skill：指导 Agent 怎么做，强调流程。
- 知识库：提供事实、案例和参考，强调检索。

例如 SSRF：

- Skill 写“如何测试 SSRF、如何判定、何时停止”。
- 知识库写“云厂商 metadata 地址、历史绕过、修复方案”。

## 本地文件工具风险

`filesystem_tools: true` 会暴露读写和执行能力。它对开发和自动化很有用，但也是安全边界。生产环境建议：

- 配合 `workspace_root_dir` 限制工作目录。
- 对写入和执行动作使用 HITL。
- 不把 `execute` 加入全局白名单。
- Skill 中明确禁止读写授权范围外文件。

## 源码锚点

- Skill 包校验：`internal/skillpackage/validate.go`
- Skill 服务：`internal/skillpackage/service.go`
- Pi Skills 接入：`internal/multiagent/pi_local_tools.go`
- Skills Handler：`internal/handler/skills.go`
