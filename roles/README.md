# Role configuration file notes

This directory contains all role configuration files; each role defines the AI's behavior mode and available tools.

## Creating a new role

To create a new role, create a YAML file in the `roles/` directory with the following format:

**Option 1: Explicitly specify the tools list (recommended)**
```yaml
name: Role name
description: Role description
user_prompt: User prompt (prepended to user messages, to guide the AI's behavior)
icon: "icon (optional)"
tools:
    # Add the tools you need...
    # ⚠️ Important: recommended to include these core built-in MCP tools (vulnerability and knowledge base)
    - record_vulnerability
    - list_knowledge_risk_types
    - search_knowledge_base
enabled: true
```

**Option 2: Do not set the tools field (use all enabled tools)**
```yaml
name: Role name
description: Role description
user_prompt: User prompt (prepended to user messages, to guide the AI's behavior)
icon: "icon (optional)"
# Without the tools field, all tools enabled in MCP management are used by default
enabled: true
```

## ⚠️ Important reminder: core built-in MCP tools

**If you set the `tools` field, be sure to include the following tools in the list (at least these three):**

1. **`record_vulnerability`** - Vulnerability management tool, used for recording discovered vulnerabilities
2. **`list_knowledge_risk_types`** - Knowledge base tool, lists available risk types
3. **`search_knowledge_base`** - Knowledge base tool, searches knowledge base content

Add other built-in or external tools such as WebShell, batch tasks, etc. as needed (per what is enabled in MCP management).

**Skills (skill packs)**: in **multi-agent / Eino** sessions, the built-in `skill` tool loads packages under `skills_dir` as needed; there is no binding relationship with the role YAML.

**Note**: If you do not set the `tools` field, the system uses all tools enabled in MCP management by default. To explicitly control a role's available tools, it is recommended to set the `tools` field explicitly.

## Role configuration field notes

- **name**: Role name (required)
- **description**: Role description (required)
- **user_prompt**: User prompt, prepended to user messages, used to guide the AI to adopt specific testing methods and attention points (optional)
- **icon**: Role icon, supports Unicode emoji (optional)
- **tools**: Tool list, specifying the tools available to this role (optional)
  - **If you do not set `tools`**: all tools enabled in **MCP management** are selected by default
  - **If you set `tools`**: only the tools specified in the list are used (recommended to include the core built-in tools above at minimum)
- **enabled**: Whether this role is enabled (required, true/false)

## Examples

See the other role files in this directory, such as `penetration-testing.yaml`, `web-application-scanning.yaml`.
