# Skills Directory (Agent Skills / Eino)

- Each skill is a **subdirectory** and must contain a **`SKILL.md`** at its root (YAML front matter: `name`, `description` + Markdown body), see [agentskills.io](https://agentskills.io/specification.md).
- **The directory name must match `name`**.
- **Runtime loading**: within an **Eino DeepAgent (multi-agent)** session, the ADK **`skill` middleware** discloses them progressively (the system prompt lists each skill's name/description, and the model then calls the **`skill`** tool to fetch the full `SKILL.md`). Optionally enable **`multi_agent.eino_skills.filesystem_tools`**, which uses the same `read_file` / `execute` etc. as the host machine to access bundled scripts and resources.
- **Web management**: HTTP `/api/skills/*` is still used to list, edit, and upload files within a package (implemented as `internal/skillpackage`, not MCP).
- **Runtime**: loaded progressively by the ADK **`skill`** tool within multi-agent (DeepAgent) sessions; the single-agent MCP loop does not include Skills, so you need to enable multi-agent or the later single-agent Eino path.
