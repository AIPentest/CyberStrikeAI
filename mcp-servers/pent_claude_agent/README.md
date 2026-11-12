# Pent Claude Agent MCP

[Chinese](README_CN.md)

AI-powered **penetration testing engineer** MCP server. CyberStrikeAI can command it to run pentest tasks, analyze vulnerabilities, and perform security diagnostics. The agent runs a Claude-based AI internally and can be configured with its own MCP servers and tools.

## Tools

| Tool | Description |
|------|-------------|
| `pent_claude_run_pentest_task` | Run a penetration testing task. The agent executes independently and returns results. |
| `pent_claude_analyze_vulnerability` | Analyze vulnerability information and provide remediation suggestions. |
| `pent_agent_execute` | Execute a task. The agent chooses appropriate tools and methods. |
| `pent_agent_diagnose` | Diagnose a target (URL, IP, domain) for security assessment. |
| `pent_claude_status` | Get the current status of pent_claude_agent. |

## Requirements

- Python 3.10+
- `mcp`, `claude-agent-sdk`, `pyyaml` (included if using the project venv; otherwise: `pip install mcp claude-agent-sdk pyyaml`)

## Configuration

The agent uses `pent_claude_agent_config.yaml` in this directory by default. You can override via:

- `--config /path/to/config.yaml` when starting the MCP server
- Environment variable `PENT_CLAUDE_AGENT_CONFIG`

Config options (see `pent_claude_agent_config.yaml`):

- `cwd`: Working directory for the agent
- `allowed_tools`: Tools the agent can use (Read, Write, Bash, Grep, Glob, etc.)
- `mcp_servers`: MCP servers the agent can use (e.g. reverse_shell)
- `env`: Environment variables (API keys, etc.)
- `system_prompt`: Role and behavior definition

Path placeholders: `${PROJECT_ROOT}` = CyberStrikeAI root, `${SCRIPT_DIR}` = this script's directory.

## Setup in CyberStrikeAI

1. **Paths**  
   Example: project root `/path/to/CyberStrikeAI-main`  
   Script: `/path/to/CyberStrikeAI-main/mcp-servers/pent_claude_agent/mcp_pent_claude_agent.py`

2. **Web UI** → **Settings** → **External MCP** → **Add External MCP**. Paste JSON (replace paths with yours):

```json
{
  "pent-claude-agent": {
    "command": "/path/to/CyberStrikeAI-main/venv/bin/python3",
    "args": [
      "/path/to/CyberStrikeAI-main/mcp-servers/pent_claude_agent/mcp_pent_claude_agent.py",
      "--config",
      "/path/to/CyberStrikeAI-main/mcp-servers/pent_claude_agent/pent_claude_agent_config.yaml"
    ],
    "description": "Penetration testing engineer: run pentest tasks, analyze vulnerabilities, get status",
    "timeout": 300,
    "external_mcp_enable": true
  }
}
```

   - `command`: Prefer the project **venv** Python; or use system `python3`.
   - `args`: **Must be absolute path** to `mcp_pent_claude_agent.py`. Add `--config` and config path if needed.
   - `timeout`: 300 recommended (pentest tasks can be long).
   - Save, then click **Start** for this MCP to use the tools in chat.

3. **Typical workflow**
   - CyberStrikeAI calls `pent_claude_run_pentest_task("Scan target 192.168.1.1 for open ports")`.
   - pent_claude_agent starts a Claude agent internally, which may use Bash, nmap, etc.
   - Results are returned to CyberStrikeAI.

## Run locally (optional)

```bash
# From project root, with venv
./venv/bin/python mcp-servers/pent_claude_agent/mcp_pent_claude_agent.py
```

The process talks MCP over stdio; CyberStrikeAI starts it the same way when using External MCP.

## Security

- Use only in authorized, isolated test environments.
- API keys in config should be kept secure; prefer environment variables for production.
