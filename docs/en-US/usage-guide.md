# DarkStorm — Usage Guide

Complete walkthrough: from a clean install to day-to-day operation, with every
command-line flag and the main configuration options explained.

> Authorized use only. Run DarkStorm against systems you own or have explicit
> written permission to test.

---

## 1. What it is

DarkStorm is an AI-native security operations platform (Go backend + web panel).
It connects planning, tool execution, human oversight, evidence, and replay in
one auditable workspace: Eino-powered agents, MCP-native tools, RAG knowledge,
visual workflows, C2, WebShell management, and attack-chain modeling.

---

## 2. Requirements

| Component | Version / note |
|-----------|----------------|
| OS | Linux (Kali recommended), macOS, or Windows via WSL2 |
| Go | 1.25.0+ (to build from source) |
| Python | 3.x (optional — only for Python-based tool recipes; see `requirements.txt`) |
| Disk | A few GB for the binary, database, and runtime workspace |
| Network | Outbound HTTPS to your LLM provider (or route it through the egress gateway) |

An LLM API key is required for the agent features (OpenAI-compatible or Claude).

---

## 3. Installation

### Option A — one-click script (recommended)

```bash
git clone https://github.com/Ed1s0nZ/CyberStrikeAI.git darkstorm
cd darkstorm
./run.sh
```

`run.sh` checks Go (and Python), builds `./cmd/server`, prepares `config.yaml`
from the example on first run, and starts the service. It sets `GOPROXY` and a
PyPI mirror for the duration of the script only.

### Option B — manual build

```bash
git clone https://github.com/Ed1s0nZ/CyberStrikeAI.git darkstorm
cd darkstorm
cp config.example.yaml config.yaml      # then edit it (see §6)
go build -o darkstorm ./cmd/server
./darkstorm -http -config config.yaml
```

### Option C — Kali Linux on WSL2 (Windows 11 host)

In PowerShell:

```powershell
wsl --install -d kali-linux
wsl --set-default-version 2
```

Then inside Kali:

```bash
sudo apt update && sudo apt full-upgrade -y
sudo apt install -y golang git           # or install Go 1.25+ manually
git clone https://github.com/Ed1s0nZ/CyberStrikeAI.git darkstorm
cd darkstorm && cp config.example.yaml config.yaml
go build -o darkstorm ./cmd/server
./darkstorm -http
```

WSL2 forwards `localhost`, so open the panel from your Windows browser at the
address the server prints (default `http://localhost:8080`). For privacy
hardening (hide your real IP through Tor), see
[Egress privacy gateway](egress-privacy.md).

---

## 4. First start & logging in

On the **first** start (empty database) DarkStorm bootstraps the built-in admin
account and prints its credentials **once** to the console:

```
ADMIN SETUP REQUIRED
────────────────────────────────────────────
 Username  admin
 Password  <24-char generated password>
 ! Store this password securely. It is shown only once.
```

- **Username:** `admin` (fixed)
- **Password:** randomly generated, shown only at first boot, then stored
  hashed in the RBAC database (`data/conversations.db`).

Sign in at the panel, then **change the password immediately** under
**System settings**.

### Lost or need to change the password

```bash
./darkstorm -reset-admin-password
```

Interactive prompt (hidden input), minimum 8 characters, asked twice. The
database must already exist (start the service once first).

---

## 5. Command-line flags

```bash
./darkstorm [flags]
```

| Flag | Effect |
|------|--------|
| `-config <path>` | Path to the config file (default `config.yaml`). |
| `-http` | Force plain HTTP, overriding TLS in the config. Good for local/WSL. |
| `-https` | Enable HTTPS; uses an in-memory self-signed cert when none is configured. |
| `-reset-admin-password` | Interactively reset the admin password, then exit. |
| `-check-process-isolation` | Probe task containment/cleanup, then exit (diagnostics). |
| `-check-update` | Report whether this install's repo has anything newer, then exit. |
| `-update` | Fast-forward to the repo's remote, rebuild the binary, then exit. |
| `-update-rollback` | Undo the last `-update` (restore the previous commit and binary). |

---

## 6. Configuration (`config.yaml`)

Copy `config.example.yaml` to `config.yaml` and edit. The example file documents
every option inline; the key sections are below.

### 6.1 Server

```yaml
server:
  host: "0.0.0.0"
  port: 8080
  cors_allowed_origins: []       # e.g. ["https://panel.example.com"]
  tls_enabled: false
  tls_cert_path: ""
  tls_key_path: ""
  tls_auto_self_sign: false      # generate a self-signed cert when no cert/key given
  # tls_http_redirect: true      # redirect HTTP -> HTTPS when TLS is on
```

### 6.2 LLM provider (AI / OpenAI)

```yaml
openai:
  provider: "openai"             # "openai" (default) or "claude"
  api_key: "sk-..."              # your LLM API key
  base_url: "https://api.openai.com/v1"   # or any OpenAI-compatible endpoint
  model: "gpt-4o"                # or claude-* when provider: claude
  max_total_tokens: 0            # 0 = model default
  max_completion_tokens: 0
  # headers: {}                  # custom request headers (advanced)
  # reasoning: {}                # reasoning-model options
```

Use `provider: claude` with a `claude-*` model to go through the native
Anthropic Messages API.

### 6.3 Agent, security, auth

```yaml
agent:
  max_iterations: 12000          # global loop ceiling
  tool_timeout_minutes: 10
  workspace_root_dir: ""         # empty => tmp/workspace (per project/conversation)

security:
  # command/tool execution policy, rate limits, RBAC — see the example file

auth:
  session_duration_hours: 24     # login session lifetime
```

### 6.4 Egress privacy gateway — hide your real IP

Route all outbound traffic through Tor / a SOCKS5 / HTTP proxy. See the full
guide in [egress-privacy.md](egress-privacy.md).

```yaml
egress:
  enabled: true
  mode: tor                      # socks5 | tor | http | https | direct
  address: "127.0.0.1:9050"
  fail_closed: true              # block traffic if the proxy is down (no leak)
  dns_through_proxy: true        # resolve DNS at the proxy (no DNS leak)
  proxychains_mode: auto         # wrap raw-socket tools (nmap) via proxychains
```

For nmap through the gateway use a TCP connect scan so no raw packets leak:
`nmap -sT -Pn -n <target>`.

### 6.5 Ephemeral mode — leave no trace

```yaml
ephemeral:
  enabled: true
  wipe_on_exit: true             # on shutdown, delete ALL runtime artifacts
  zero_retention: true           # aggressive storage policy while running
```

### 6.6 Storage cleanup (retention)

```yaml
storage:
  auto_clean: true               # background cleanup on
  interval_minutes: 60           # poll interval (floor 5)
  orphan_grace_days: 1
  active_grace_hours: 24         # never touch recently-active sessions
  # categories: {}               # per-category retention_days overrides
```

On-demand wipe of one engagement's artifacts (irreversible; needs confirmation):

```bash
# preview only
curl -X POST http://localhost:8080/api/storage/sweep \
  -H 'Content-Type: application/json' \
  -d '{"scope":"conversation","id":"<conversation-id>"}'

# actually delete
curl -X POST http://localhost:8080/api/storage/sweep \
  -H 'Content-Type: application/json' \
  -d '{"scope":"conversation","id":"<conversation-id>","dry_run":false,"confirm":true}'
```

### 6.7 Roles, skills, agents, C2, robots

```yaml
roles_dir: "roles"               # role YAMLs (Default, Penetration-Testing, ...)
skills_dir: "skills"             # skill packages (SKILL.md folders)
agents_dir: "agents"             # multi-agent sub-agent markdown definitions
c2: {}                           # built-in C2 toggle (on by default)
robots: {}                       # WeChat / WeCom / DingTalk / Lark / Telegram bots
```

---

## 7. Using the panel

After signing in, the left navigation groups the workspace:

- **Dashboard** — runtime status, vulnerability stats, tool usage, capabilities.
- **Chat** — drive the agent in natural language; it plans and runs tools with
  human-in-the-loop approval for sensitive actions.
- **Security / Projects / Asset Management / Vulnerabilities / Tasks** — scope
  an engagement, track assets and findings.
- **Workflows** — visual, repeatable task flows.
- **WebShell Management** — manage webshell connections to authorized targets.
- **C2** — listeners, sessions, payloads.
- **MCP** — the Model-Context-Protocol tool registry (internal + external).
- **Knowledge** — RAG knowledge base.
- **Skills / Agents / Roles** — capability packages, sub-agents, and operator
  personas (each role is a YAML in `roles/`).
- **Platform Access / System settings** — RBAC users, retention, bots, and
  the Storage Cleanup page.

Pick a **Role** (e.g. *Penetration Testing*, *Information Gathering*,
*Digital Forensics*) to set the agent's system prompt and tool posture for the
task at hand.

---

## 8. Human-in-the-loop (HITL)

Sensitive tool calls (running a command, visiting a URL, modifying a file, using
the browser) pause for approval in the panel. Approve, deny, or auto-allow by
policy. Decisions are audited. Configure under `hitl:` / `tool_guard:` in the
config and the HITL pages in the UI.

---

## 9. Privacy & cleanup checklist

Before a sensitive engagement:

1. Enable the **egress gateway** (`egress.enabled: true`, `fail_closed: true`)
   and confirm the startup **leak self-test** passes in the log.
2. Optionally enable **ephemeral mode** (`wipe_on_exit: true`) so nothing
   persists after you stop the server.
3. For an OS-level guarantee, add the firewall kill-switch from
   [egress-privacy.md](egress-privacy.md).
4. When a test is done, use **Storage → sweep** (or let `wipe_on_exit` handle it)
   to remove that engagement's artifacts.

---

## 10. Updating

```bash
./darkstorm -check-update     # is there anything newer?
./darkstorm -update           # fast-forward + rebuild
./darkstorm -update-rollback  # undo the last update
```

The update source is the repository in `update.repo` (defaults to the official
repo); point it at your own fork or a mirror if needed.

---

## 11. Troubleshooting

| Symptom | Fix |
|---------|-----|
| Server won't start, template panic | Ensure you are on a build that includes the template-delimiter fix; rebuild from source. |
| Can't log in | Grab the admin password from the first-boot log, or run `-reset-admin-password`. |
| Outbound blocked while egress is on | That's `fail_closed` doing its job — the proxy is unreachable; start Tor/your proxy. |
| nmap leaks the real IP | Use `nmap -sT -Pn -n` (raw-packet scans bypass proxychains). |
| "no space left on device" | Delete runtime artifacts (Storage Cleanup / sweep) or enable ephemeral mode. |
| LLM calls fail | Check `openai.api_key` / `base_url` / `model`; verify network (or egress) reaches the provider. |

---

## See also

- [Egress privacy gateway](egress-privacy.md) — hide your real IP (Tor/proxy, fail-closed, proxychains).
- `config.example.yaml` — every configuration option with inline comments.
