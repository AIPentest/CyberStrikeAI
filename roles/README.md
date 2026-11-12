# Role configuration files

This directory contains all role configuration files. Each role defines the AI's behavior mode and available tools.

## Creating a new role

To create a new role, create a YAML file under the `roles/` directory in the following format:

**Option 1: Explicitly specify the tool list (recommended)**
```yaml
name: Role name
description: Role description
user_prompt: User prompt (prepended to the user message to guide the AI's behavior)
icon: "Icon (optional)"
tools:
    # Add the tools you need...
    # ⚠️ Important: it is recommended to include the following core built-in MCP tools (vulnerabilities and knowledge base)
    - record_vulnerability
    - list_knowledge_risk_types
    - search_knowledge_base
enabled: true
```

**Option 2: Do not set the tools field (use all enabled tools)**
```yaml
name: Role name
description: Role description
user_prompt: User prompt (prepended to the user message to guide the AI's behavior)
icon: "Icon (optional)"
# Without the tools field, all tools enabled in MCP management are used by default
enabled: true
```

## ⚠️ Important reminder: core built-in MCP tools

**If you set the `tools` field, be sure to include the following tools in the list (at least these three):**

1. **`record_vulnerability`** - Vulnerability management tool, used to record discovered vulnerabilities
2. **`list_knowledge_risk_types`** - Knowledge base tool, lists the available risk types
3. **`search_knowledge_base`** - Knowledge base tool, searches knowledge base content

As needed, you can also add WebShell, batch tasks, and other built-in or external tools (subject to those enabled in MCP management).

**Skills**: in **multi-agent / Eino** sessions, the built-in **`skill`** tool loads packages from `skills_dir` on demand; they are not bound to the role YAML.

**Note**: if you do not set the `tools` field, the system uses all tools enabled in MCP management by default. To control a role's available tools explicitly, it is recommended to set the `tools` field explicitly.

## Role configuration field reference

- **name**: Role name (required)
- **description**: Role description (required)
- **user_prompt**: User prompt, prepended to the user message to guide the AI toward specific testing methods and focus areas (optional)
- **icon**: Role icon, supports Unicode emoji (optional)
- **tools**: Tool list, specifies the tools available to this role (optional)
  - **If the `tools` field is not set**: all tools enabled in MCP management are selected by default
  - **If the `tools` field is set**: only the tools in the list are used (it is recommended to include at least the core built-in tools above)
- **enabled**: Whether to enable this role (required, true/false)

## Examples

Refer to the other role files in this directory, such as `Penetration-Testing.yaml`, `Web-Application-Scanning.yaml`, etc.
