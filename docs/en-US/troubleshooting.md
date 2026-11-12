# Troubleshooting

[Chinese](../zh-CN/troubleshooting.md)

Debug by layer. Do not change random config before locating the failing layer.

## Diagnostic Order

1. Process: is the service alive, any panic?
2. Network: port, HTTPS, reverse proxy, browser console.
3. Auth: does `/api/auth/validate` return 200?
4. Config: can `/api/config` be read and applied?
5. Model: does model test pass?
6. Tools: do tool list and schemas look right?
7. Database: is `data/` writable, any lock?
8. Subsystem: KB, MCP, C2, WebShell minimal action.

## Minimal Commands

```bash
lsof -i :8080
curl -k -I https://127.0.0.1:8080/
curl -k -I https://127.0.0.1:8080/static/logo.png
ls -lh data/
```

If a reverse proxy is involved, test both proxy address and upstream address.

## Common Issues

Page inaccessible:

- wrong protocol, especially HTTPS vs HTTP;
- self-signed cert warning;
- port occupied;
- reverse proxy loop.

Login fails:

- wrong RBAC user password;
- config not applied/restarted;
- stale cookie;
- audit throttling repeated failures.

### Recover a forgotten `admin` password

If another administrator with `rbac:write` is available, reset the password under **Platform permissions → User management**.

If no administrator session is available, the built-in `admin` account can be recovered on the server. Change to the project root and run:

```bash
./run.sh --reset-admin-password
```

Enter and confirm the new password when prompted. The script hides input and stores a bcrypt hash. If the service is running, restart it afterward to invalidate existing login sessions.

If `run.sh` is not available, run the command below manually. Enter and confirm the new password when prompted:

```bash
HASH=$(htpasswd -nBC 10 '' | cut -d: -f2 | tr -d '\n') && sqlite3 data/conversations.db "UPDATE rbac_users SET password_hash='$HASH', updated_at=CURRENT_TIMESTAMP WHERE id='admin' AND username='admin' AND is_builtin=1; SELECT changes();"
```

Output `1` means that the row was updated. The command requires `sqlite3` and `htpasswd`. If `database.path` in `config.yaml` is not the default, replace `data/conversations.db`. Password input is hidden and is not written to shell history.

Model fails:

- selected AI channel does not exist; empty selection follows `ai.default_channel`;
- wrong `ai.channels.<id>.base_url` path;
- invalid API key;
- model unavailable;
- reasoning fields unsupported by gateway. Try `ai.channels.<id>.reasoning.mode: off`.

Streaming stalls:

- proxy buffers SSE;
- model gateway timeout;
- context too large;
- browser/network interruption.

Tool fails:

- real command not installed;
- YAML schema wrong;
- HITL rejected or pending;
- timeout or no-output timeout.

Knowledge base empty:

- `knowledge.enabled` false;
- scan/index not run;
- embedding API failed;
- threshold or risk type too strict.

C2 returns 503:

- expected when `c2.enabled: false`.

## Common Misdiagnoses

- "Model is broken": HITL is waiting.
- "Tool missing": tool_search hides it from current context.
- "Knowledge base useless": index not rebuilt or risk type too narrow.
- "Config saved but ineffective": listener/TLS changes need restart.
- "Robot silent": platform callback or signature config wrong.

## Issue Template

```text
Version:
Startup method:
Access path:
Relevant config:
Steps:
Expected:
Actual:
Server logs:
Browser console:
API response:
```
