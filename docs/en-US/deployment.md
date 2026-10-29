# Deployment Guide

[中文](../zh-CN/deployment.md)

CyberStrikeAI can run as a local testing tool, an internal team service, or a production red-team platform. Treat it as a high-privilege security system: it can execute commands, call MCP tools, manage WebShell connections, and optionally run C2 listeners.

## Prerequisites

- Go for source runs and binary builds.
- Python for some MCP servers and tool scripts.
- SQLite files under `data/`; no external DB is required by default.
- Actual security tools installed in PATH. YAML files under `tools/` only describe commands.
- At least one `ai.channels` entry. Use `provider: openai_compatible` for OpenAI-compatible endpoints, or `provider: claude` for Eino's native Claude component.

Important persistent paths:

```text
config.yaml
data/
tools/
roles/
skills/
agents/
knowledge_base/
chat_uploads/
```

Back these up before upgrades.

## Startup Modes

Local quick start:

```bash
chmod +x run.sh && ./run.sh
```

`run.sh` is the most common startup path for local use, development, small temporary internal deployments, and quick post-upgrade verification.

For long-running service, boot-time startup, managed logs, and crash recovery, prefer a binary managed by systemd.

Source run:

```bash
go run ./cmd/server --config config.yaml
```

Binary build:

```bash
go build -o cyberstrike-ai ./cmd/server
./cyberstrike-ai --config config.yaml
```

The binary still needs `web/templates`, `web/static`, and the runtime resource directories.

## HTTPS and Reverse Proxy

For local testing, self-signed HTTPS is acceptable:

```yaml
server:
  tls_enabled: true
  tls_auto_self_sign: true
```

For production, use real certificates or terminate TLS at a reverse proxy. If the proxy terminates TLS and forwards HTTP to the app, avoid enabling app-side TLS on the same upstream unless `proxy_pass` uses HTTPS.

Nginx must not buffer SSE:

```nginx
proxy_buffering off;
proxy_http_version 1.1;
proxy_set_header Upgrade $http_upgrade;
proxy_set_header Connection "upgrade";
```

## Site rendering service (optional)

Site screenshots need a separate browser rendering service. It is **not part of the main
service**: the main service usually runs as root, and rendering untrusted target sites from it
would be too risky. The browser therefore lives in its own non-root process, and the main service
calls it over `127.0.0.1:8099`.

The source and systemd unit ship in `scripts/shot-service/` inside the repository.

```bash
# 1) Install dependencies as the service's normal user (never root)
mkdir -p ~/shot-service && cd ~/shot-service
npm init -y
npm i playwright --no-audit --no-fund
npx playwright install chromium      # about 120MB

# 2) Deploy the service code
cp <repo>/scripts/shot-service/server.js ~/shot-service/

# 3) Screenshot directory: create as root, chown to the browser user
sudo mkdir -p <repo>/data/screenshots
sudo chown <service-user>:<service-user> <repo>/data/screenshots

# 4) Install and start the unit (adjust User and paths)
sudo cp <repo>/scripts/shot-service/cyberstrike-shot.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now cyberstrike-shot
systemctl is-active cyberstrike-shot
curl -s http://127.0.0.1:8099/health     # expect {"ok":true,...}
```

Configuration: [configuration reference → Site screenshots](configuration.md). The main service
probes `/health` at startup; when the renderer is unreachable it disables automatic captures
instead of failing, and other features are unaffected.

> The renderer really visits target sites—use it only within authorized scope and lower the
> capture rate against production targets. The browser runs as a non-root user and the systemd
> unit sets `NoNewPrivileges`, `PrivateTmp`, and `ProtectSystem=full`.

## Upgrading

**Prefer the platform's own one-click update** (console System settings -> One-click update,
`POST /api/system/update/apply`, or `./cyberstrike-ai -update` at a keyboard). It pulls the newest
commit of the **update source repository**'s default branch (the official repository
`https://github.com/AIPentest/CyberStrikeAI.git` by default; one setting points it at your own fork
or a mirror): fetch, fast-forward, `go build`, atomic binary swap (the old binary is kept as
`cyberstrike-ai.prev`), while operator content - `roles/ skills/ tools/ agents/ knowledge_base/
data/ config.yaml` - is put aside and restored, and the result names every file it kept. When source
is modified locally or the local history has diverged from the source it refuses and says why
instead of forcing. See [the developer guide](developer-guide.md) for the full semantics.

When a supervisor is detected (systemd/launchd startup markers in the environment), the "exit after
updating" tick is pre-ticked: the process stands down after a successful update, the supervisor brings
the new binary back, and the page reloads itself once the service answers (sessions live in memory, so
one fresh login follows). Forgot the tick, or swapped the binary from the CLI? The console carries a
persistent banner with **Restart now** - no shell login needed.

The update source is a single repository address in the `update` section of `config.yaml` (`repo`),
or right on the console page: the official repository, your own fork, somebody else's repository or a
mirror are all just this one setting; empty means the official repository. An installation unpacked
from a Release archive (no `.git`) can be connected from the same page (no configuration needed - the
default official repository applies): the preview lists the files the target would replace and the
operator content that is kept, replaced files are backed up under
`.update-backup/<unique-backup>/overwritten/`, and confirming turns the directory into a normal
installation with one-click updates.

`upgrade.sh` still works and is now a thin shell over that implementation, with two paths decided by
the installation kind:

- **git work tree**: calls `./cyberstrike-ai -update` directly (building
  `go build -o cyberstrike-ai ./cmd/server` first when there is no binary yet, and failing with
  instructions on installing Go when there is neither). `--check` inspects only, through
  `-check-update`.
- **not a git work tree** (Release tarball install): only here does it fall back to downloading a
  Release tarball and `rsync --delete`-ing it in. The source repository then comes from
  `--repo owner/name` or the `GITHUB_REPO` environment variable, and the built-in default is used only
  when neither is given - with a warning naming which repository the code is coming from.
  `config.yaml`, `data/`, `tools/`, `roles/`, `skills/`, `agents/`, `venv/` (skippable with
  `--no-venv`) and the rollback points `.update-backup/`, `.update-state.json`, `cyberstrike-ai.prev`
  are on the rsync keep list, so `--delete` cannot remove them.

Recommended flow either way:

1. Stop the service.
2. Back up `config.yaml`, `data/` and your custom directories.
3. Pull or replace the new code/binary.
4. Keep your own configuration and add fields the new `config.yaml` example introduced.
5. Start the service and check login, model test, tool list and knowledge-base status.

The one-click update performs step 3 (plus the rebuild); 1, 2, 4 and 5 stay operator decisions - a
database migration does not become compatible just because the binary changed. `upgrade.sh` suits
quick upgrades without compatibility risk; in production still back up first. Python dependencies:
`venv/` is created and managed by `run.sh`, and keeping it means an upgrade does not remove it - only
`--no-venv` deletes it and lets `run.sh` reinstall.

## Rollback

The one-click update leaves its own rollback point: `./cyberstrike-ai -update-rollback`, or the button
on the page, goes back to the commit before that update and puts `cyberstrike-ai.prev` back. It runs
only while HEAD is still the commit that update wrote, so work done afterwards is not discarded.
Content that was put aside also survives under `.update-backup/<unique-backup>/`.

Beyond that, roll back together:

- The previous binary or code.
- The pre-upgrade `config.yaml`.
- The pre-upgrade `data/`.

If the new version already wrote database schema changes, rolling back the binary alone may not be
enough; restore the whole backup.

## Deployment Decision Table

| Scenario | Recommended setup | Key settings | Avoid |
| --- | --- | --- | --- |
| Personal testing | `./run.sh` + self-signed HTTPS | `tls_auto_self_sign: true` | Public exposure |
| Internal team | Binary + systemd + internal HTTPS | strong password, audit, backup, IP restrictions | Shared weak password |
| Production red-team platform | Reverse proxy + dedicated OS user + log collection | real certs, proxy auth, C2 only when needed | Direct public admin UI |
| Chat/KB only | Disable C2 and unnecessary MCP | `c2.enabled: false` | All tools enabled by default |
| Tool automation | Isolated workspace + HITL | `workspace_root_dir`, `hitl`, `monitor` | Shell tools globally allowlisted |

## Acceptance Checklist

After startup:

1. Open `/` and verify no HTTP/HTTPS redirect loop.
2. Login and validate `/api/auth/validate`.
3. Run model test in settings.
4. Check tool list and schemas.
5. If KB is enabled, check index status.
6. If external MCP is enabled, verify connection and tool visibility.
7. If C2 is enabled, start and stop a test listener only in an authorized network.
8. Check audit logs for login and config activity.

## Runtime File Layers

- Replaceable: binary, `web/`, default docs/resources.
- Preserve: `config.yaml`, `data/`, custom tools/roles/skills/agents, `knowledge_base`, uploads.
- Cleanup candidates: checkpoints, temporary workspaces, stale payloads, old tool execution records.

## Source Anchors

- App wiring and routes: `internal/app/app.go`
- TLS bootstrap: `internal/app/main_server_tls.go`
- HTTP to HTTPS redirect: `internal/app/main_server_http_redirect.go`
- Config structs: `internal/config/config.go`
- Config apply: `internal/handler/config.go`
