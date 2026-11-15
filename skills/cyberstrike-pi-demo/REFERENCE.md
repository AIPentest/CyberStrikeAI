# API / 参考（示例）

## HTTP `/api/skills/{id}`

- `resource_path`：包内相对路径，例如 `FORMS.md`、`scripts/payloads.txt`。
- `depth=summary`：仅摘要与目录，适合首轮检索。

## 多代理运行时

- 使用 Pi **`skill`** 工具按技能包渐进加载；可选开启 `multi_agent.skills.filesystem_tools` 访问包内文件。

## 链接

- [Agent Skills 概览](https://platform.claude.com/docs/en/agents-and-tools/agent-skills/overview)
