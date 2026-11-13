# Skills 目录（Agent Skills / Pi）

- 每个技能为**子目录**，根上必须有 **`SKILL.md`**（YAML front matter：`name`、`description` + Markdown 正文），见 [agentskills.io](https://agentskills.io/specification.md)。
- **目录名须与 `name` 一致**。
- **运行时加载**：在 **Pi 单代理或多代理** 会话中由 原生 **`skill` 工具**渐进披露（系统提示中列出各 skill 的 name/description，模型再调用 **`skill`** 工具拉取 `SKILL.md` 全文）。可选开启 **`multi_agent.skills.filesystem_tools`**，使用与本机相同的 `read_file` / `execute` 等访问包内脚本与资源。
- **Web 管理**：HTTP `/api/skills/*` 仍用于列表、编辑、上传包内文件（实现为 `internal/skillpackage`，非 MCP）。
- **运行时**：单代理与多代理均由原生 **`skill`** 工具按需加载。
