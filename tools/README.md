# Tool Configuration Guide

## Overview

Each tool has its own configuration file under the `tools/` directory. This keeps tool definitions clear, easy to maintain, and manageable. The system automatically loads all `.yaml` and `.yml` files in `tools/`.

## Configuration File Format

Each tool configuration file is a YAML file. The table below lists supported top-level fields and whether they are required. Check each item before submitting:

| Field | Required | Type | Description |
|-------|----------|------|-------------|
| `name` | ✅ | string | Unique tool identifier; use lowercase letters, digits, and hyphens. |
| `command` | ✅ | string | Command or script to run; must be on system PATH or an absolute path. |
| `enabled` | ✅ | bool | Whether to register with MCP; set to `false` to skip the tool. |
| `description` | ✅ | string | Full description, multi-line Markdown, for AI and `resources/read` queries. |
| `short_description` | Optional | string | 20–50 character summary for tool lists and lower token usage; defaults to start of `description` if omitted. |
| `args` | Optional | string[] | Fixed arguments prepended to the command line; often used for default scan modes. |
| `parameters` | Optional | array | Runtime parameter list; see **Parameter Definition** below. |
| `arg_mapping` | Optional | string | Parameter mapping mode (`auto`/`manual`/`template`); default `auto`; only set if needed. |

> If a field is wrong or a required field is missing, the loader skips that tool and logs a warning; other tools are unaffected.

## Tool Descriptions

### Short Description (`short_description`)

- **Purpose**: Used in tool lists to reduce tokens sent to the model.
- **Guideline**: One sentence (20–50 characters) describing the tool’s main use.
- **Example**: `"Network scanner for discovering hosts, open ports, and services"`

### Detailed Description (`description`)

Use multi-line text and include:

1. **Capabilities**: What the tool does.
2. **Usage scenarios**: When to use it.
3. **Warnings**: Caveats and safety notes.
4. **Examples**: Optional usage examples.

**Notes**:
- Tool lists use `short_description` when present.
- If `short_description` is missing, the system uses the first line or first 100 characters of `description`.
- Full descriptions are available via MCP `resources/read` (URI: `tool://tool_name`).

This reduces token usage, especially with many tools (e.g. 100+).

## Parameter Definition

Each parameter can include:

- `name`: Parameter name.
- `type`: One of string, int, bool, array.
- `description`: Full description (multi-line supported).
- `required`: Whether it is required (true/false).
- `default`: Default value.
- `flag`: CLI flag (e.g. `-u`, `--url`, `-p`).
- `position`: Zero-based index for positional arguments.
- `format`: One of `"flag"`, `"positional"`, `"combined"`, `"template"`.
- `template`: Template string when `format` is `"template"`.
- `options`: Allowed values for enums.

### Parameter Formats

- **`flag`**: Flag plus value, e.g. `--flag value` or `-f value`
  - Example: `flag: "-u"` → `-u http://example.com`

- **`positional`**: Added in order by position.
  - Example: `position: 0` → first positional argument.

- **`combined`**: Single token `--flag=value`.
  - Example: `flag: "--level"`, `format: "combined"` → `--level=3`

- **`template`**: Custom template.
  - Example: `template: "{flag} {value}"` → custom format.

### Special Parameters

#### `additional_args`

Used to pass extra CLI options not defined in the parameter list. The value is split on spaces into multiple arguments.

**Use cases:**
- Advanced tool options.
- Options not in the schema.
- Complex argument combinations.

**Example:**
```yaml
- name: "additional_args"
  type: "string"
  description: "Extra CLI arguments; separate multiple options with spaces"
  required: false
  format: "positional"
```

**Usage:**
- `additional_args: "--script vuln -O"` → `["--script", "vuln", "-O"]`
- `additional_args: "-T4 --max-retries 3"` → `["-T4", "--max-retries", "3"]`

**Notes:**
- Split by spaces; quoted parts are preserved.
- Ensure valid syntax to avoid command injection.
- Appended at the end of the command.

#### `scan_type` (tool-specific)

Some tools (e.g. `nmap`) support `scan_type` to override the default scan arguments.

**Example (nmap):**
```yaml
- name: "scan_type"
  type: "string"
  description: "Scan type options; overrides default scan arguments"
  required: false
  format: "positional"
```

**Usage:**
- `scan_type: "-sV -sC"` → version and script scan.
- `scan_type: "-A"` → aggressive scan.

**Notes:**
- If set, it replaces the tool’s default scan arguments.
- Multiple options separated by spaces.

### Parameter Description Guidelines

Parameter descriptions should include:

1. **Purpose**: What the parameter does.
2. **Format**: Expected format (e.g. URL, port range).
3. **Example values**: Concrete examples (list if several).
4. **Notes**: Permissions, performance, safety, etc.

**Style:**
- Use Markdown for readability.
- Use **bold** for important points.
- Use lists for multiple examples or options.
- Use code blocks for complex formats.

**Example:**
```yaml
description: |
  Target IP or domain. Can be a single IP, range, CIDR, or hostname.

  **Example values:**
  - Single IP: "192.168.1.1"
  - Range: "192.168.1.1-100"
  - CIDR: "192.168.1.0/24"
  - Domain: "example.com"

  **Notes:**
  - Format must be valid.
  - Required; cannot be empty.
```

## Parameter Types

### Boolean (`bool`)

- `true`: Add only the flag (e.g. `--flag`).
- `false`: Do not add the argument.
- Accepted: `true`/`false`, `1`/`0`, `"true"`/`"false"`.

**Example:**
```yaml
- name: "verbose"
  type: "bool"
  description: "Enable verbose output"
  required: false
  default: false
  flag: "-v"
  format: "flag"
```

### String (`string`)

General-purpose; any string value.

### Integer (`int` / `integer`)

For numbers (ports, levels, etc.).

**Example:**
```yaml
- name: "level"
  type: "int"
  description: "Test level, 1-5"
  required: false
  default: 3
  flag: "--level"
  format: "combined"  # --level=3
```

### Array (`array`)

Converted to a comma-separated string.

**Example:**
```yaml
- name: "ports"
  type: "array"
  item_type: "number"
  description: "Port list"
  required: false
  # Input: [80, 443, 8080]
  # Output: "80,443,8080"
```

## Examples

See existing configs under `tools/`:

- `nmap.yaml`: Network scanner (`scan_type` and `additional_args`).
- `sqlmap.yaml`: SQL injection (`additional_args`).
- `nikto.yaml`: Web server scanner.
- `dirb.yaml`: Directory scanner.
- `exec.yaml`: System command execution.

### Full Example: nmap

```yaml
name: "nmap"
command: "nmap"
args: ["-sT", "-sV", "-sC"]  # default scan type
enabled: true

short_description: "Network scanner for discovering hosts, open ports, and services"

description: |
  Network mapping and port scanning for hosts, services, and open ports.

  **Capabilities:**
  - Host discovery
  - Port scanning
  - Service/version detection
  - OS detection
  - NSE-based vulnerability checks

parameters:
  - name: "target"
    type: "string"
    description: "Target IP or domain"
    required: true
    position: 0
    format: "positional"

  - name: "ports"
    type: "string"
    description: "Port range, e.g. 1-1000"
    required: false
    flag: "-p"
    format: "flag"

  - name: "scan_type"
    type: "string"
    description: "Scan type options, e.g. '-sV -sC'"
    required: false
    format: "positional"

  - name: "additional_args"
    type: "string"
    description: "Extra nmap arguments, e.g. '--script vuln -O'"
    required: false
    format: "positional"
```

## Adding a New Tool

Create a new YAML file under `tools/`, e.g. `my_tool.yaml`:

```yaml
name: "my_tool"
command: "my-command"
args: ["--default-arg"]  # optional fixed args
enabled: true

# Short description (recommended) – for tool list, fewer tokens
short_description: "One-line summary of what the tool does"

# Full description – for docs and AI
description: |
  Full description; multi-line and Markdown supported.

  **Capabilities:**
  - Feature 1
  - Feature 2

  **Usage:**
  - Scenario 1
  - Scenario 2

  **Notes:**
  - Caveats
  - Permissions
  - Performance

parameters:
  - name: "target"
    type: "string"
    description: |
      Target parameter description.

      **Example values:**
      - "value1"
      - "value2"

      **Notes:**
      - Format and limits
    required: true
    position: 0
    format: "positional"

  - name: "option"
    type: "string"
    description: "Option parameter"
    required: false
    flag: "--option"
    format: "flag"

  - name: "verbose"
    type: "bool"
    description: "Verbose mode"
    required: false
    default: false
    flag: "-v"
    format: "flag"

  - name: "additional_args"
    type: "string"
    description: "Extra arguments; separate with spaces"
    required: false
    format: "positional"
```

Restart the service to load the new tool.

### Best Practices

1. **Parameter design**
   - Define common parameters explicitly so the AI can use them.
   - Use `additional_args` for advanced cases.
   - Provide clear descriptions and examples.

2. **Descriptions**
   - Use `short_description` to reduce tokens.
   - Keep `description` detailed for AI and docs.
   - Use Markdown for readability.

3. **Defaults**
   - Set sensible defaults for common parameters.
   - Booleans often default to `false`.
   - Numbers according to tool behavior.

4. **Validation**
   - Document format and constraints.
   - Give several example values.
   - Mention limits and caveats.

5. **Safety**
   - Add warnings for dangerous or privileged actions.
   - Document permission requirements.
   - Remind users to use only in authorized environments.

6. **Execution duration and timeout**
   - If a tool often runs very long (e.g. still “running” after 10–30 minutes), treat it as abnormal and:
     - Set **config.yaml** → `agent.tool_timeout_minutes` (default 10) so long runs are stopped and resources freed.
     - Increase it (e.g. 20, 30) only when longer runs are needed; avoid `0` (no limit).
     - Use “Stop task” on the task monitor to cancel the whole run.
     - Prefer tools that support cancellation or an internal timeout so they align with the global timeout.

## Disabling a Tool

Set `enabled: false` in the tool’s config, or remove/rename the file. Disabled tools are not listed and cannot be called by the AI.

## Tool Configuration Validation

On load, the system checks:

- ✅ Required fields: `name`, `command`, `enabled`.
- ✅ Parameter structure and types.

Invalid configs produce startup warnings but do not prevent the server from starting. Invalid tools are skipped; others still load.

## FAQ

### Q: How do I pass multiple parameter values?

A: Array parameters are turned into comma-separated strings. For multiple separate arguments, use `additional_args`.

### Q: How do I override a tool’s default arguments?

A: Some tools (e.g. `nmap`) support a `scan_type` parameter. Otherwise use `additional_args`.

### Q: A tool has been “running” for over 30 minutes. What should I do?

A: That usually means it’s stuck. You can:
1. Set `agent.tool_timeout_minutes` in **config.yaml** (default 10) so single tool runs are stopped after that many minutes.
2. Use “Stop task” on the task monitor to stop the run immediately.
3. If the tool legitimately needs more time, increase `tool_timeout_minutes` (avoid setting it to 0).

### Q: What if tool execution fails?

A: Check:
1. The tool is installed and on PATH.
2. The tool config is correct.
3. Parameter formats match what the tool expects.
4. Server logs for the exact error.

### Q: How can I test a tool configuration?

A: Use the config test utility:
```bash
go run cmd/test-config/main.go
```

### Q: How is parameter order controlled?

A: Use the `position` field for positional arguments. **Position 0** (e.g. gobuster’s `dir` subcommand) is placed right after the command, before any flag arguments, so CLIs that expect “subcommand + options” work. Other flags are added in the order they appear in `parameters`, then position 1, 2, …; `additional_args` is appended last.

## Tool Configuration Templates

### Basic template

```yaml
name: "tool_name"
command: "command"
enabled: true

short_description: "Short description (20–50 chars)"

description: |
  Full description: what it does, when to use it, and caveats.

parameters:
  - name: "target"
    type: "string"
    description: "Target parameter"
    required: true
    position: 0
    format: "positional"

  - name: "additional_args"
    type: "string"
    description: "Extra CLI arguments"
    required: false
    format: "positional"
```

### Template with flag parameters

```yaml
name: "tool_name"
command: "command"
enabled: true

short_description: "Short description"

description: |
  Full description.

parameters:
  - name: "target"
    type: "string"
    description: "Target"
    required: true
    flag: "-t"
    format: "flag"

  - name: "option"
    type: "bool"
    description: "Option"
    required: false
    default: false
    flag: "--option"
    format: "flag"

  - name: "level"
    type: "int"
    description: "Level"
    required: false
    default: 3
    flag: "--level"
    format: "combined"

  - name: "additional_args"
    type: "string"
    description: "Extra arguments"
    required: false
    format: "positional"
```

## Related Documents

- Main project README: see `README.md` in the project root.
- Tool list: all YAML configs under `tools/`.
- API: see the main README for API details.
