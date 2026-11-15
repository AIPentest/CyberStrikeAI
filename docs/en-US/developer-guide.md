# Developer Guide

[中文](../zh-CN/developer-guide.md)

This guide is for contributors extending CyberStrikeAI. The project is a Go single-service application with a static frontend, SQLite persistence, Agent/MCP orchestration, and optional high-risk security subsystems.

## Project Layout

```text
cmd/server/              service entrypoint
internal/app/            app wiring, routes, MCP tool registration
internal/handler/        HTTP handlers
internal/database/       SQLite access
internal/security/       auth, rate limits, shell execution
internal/mcp/            MCP server and external MCP manager
internal/multiagent/     Pi single-agent, multi-agent, middleware
internal/workflow/       graph orchestration runtime
internal/knowledge/      indexing and retrieval
internal/c2/             built-in C2
internal/project/        project fact blackboard
web/static/              frontend JS/CSS/assets
web/templates/           HTML templates
tools/                   YAML command tools
roles/                   role YAML
agents/                  multi-agent Markdown definitions
skills/                  Agent Skills
docs/                    documentation
```

## Development Startup

```bash
go run ./cmd/server --config config.yaml
```

The frontend is static. Most JS/CSS/template changes only require a browser refresh.

## One-Click Update: Letting an Installation Update Its Own Source

On a deployed machine the routine is "my tree moved forward by a few dozen commits -> bring this
installation up -> rebuild -> swap the binary -> restart". This used to be only `upgrade.sh`, which
pointed at one fixed upstream repository, so for anyone running a fork "upgrade" meant "overwrite
yourself with somebody else's code". The capability now lives in the platform (`internal/update`) and
the console page, the REST API and the CLI share one implementation, so the three surfaces cannot
disagree about why an update was refused.

### Three entry points

| entry | how it is used |
|---|---|
| console | System settings -> One-click update (`#system-update`, the old deep link still works). **Update** starts a job the page polls. Supported installations switch to the newly built binary by default, and the page reloads once the service comes back. A rollback button sits below. |
| REST | `GET /api/system/update` (state on disk, no network), `POST /api/system/update/check` (fetch, then report the gap), `POST /api/system/update/apply` (`202` with `job_id`, polled through `GET /api/system/update/job`), `POST /api/system/update/rollback` |
| CLI | `./cyberstrike-ai -check-update`, `./cyberstrike-ai -update`, `./cyberstrike-ai -update-rollback`. The install root is the directory holding `--config`, or the current directory when `--config` was not given |

Permissions are `update:read` (GET) and `update:apply` (the four mutating endpoints). `update:apply`
additionally requires a global (`all` scope) session: one machine has one source tree, and an
`assigned`/`own` session must not be able to move the code everybody else is running.

### Where the code comes from

From **the update source repository**: one address, the official repository
`https://github.com/AIPentest/CyberStrikeAI.git` by default, and the update follows that repository's
**own default branch** (nothing about branches is configured, and "main" is not hardcoded), so an
installation with no configuration at all still has exactly one answer to "where does an update come
from". `upgrade.sh` is now a thin shell over the same implementation: when this directory is a git
work tree it just calls `./cyberstrike-ai -update`.

### Changing the source (the update section of config.yaml)

The update source is a single field: a repository address. Empty means the official repository; to
follow your own fork, a mirror (when GitHub is hard to reach), or somebody else's repository, this is
the one place to change:

```yaml
update:
  repo: https://github.com/AIPentest/CyberStrikeAI.git  # one repository address; empty/absent = the official repository
```

The console's "Update source" block edits and saves this address (saving it empty goes back to the
official repository). Saving validates the address against an allowlist: https/http/ssh/git/file plus
local absolute paths - git's `ext::` transport executes commands and is rejected outright.

### Connecting a non-git installation (the tarball kind)

An installation that started as an unpacked Release archive has no `.git`; it can be connected from
the same page (the default official repository works without any configuration): the preview fetches in a throwaway repository and first lists
**the local files the target would replace** and **the operator content that will be kept**; on
confirm the directory becomes a git work tree (`git init`, origin added, the target branch checked
out), every replaced file is kept under `.update-backup/<unique-backup>/overwritten/`, operator content
is stashed and restored as usual, and the binary is rebuilt. From then on it is a normal
installation with one-click update and rollback - but note there is no git history before the
connection, so the connection itself has no earlier commit to roll back to; the first real update
writes the first rollback point.

### The version number follows the code

The repository's `config.example.yaml` carries the release version this code was cut from
(upstream bumps it in the release commit), while your `config.yaml` is operator data that no
update ever touches. So after a successful merge the update writes the new code's version back
into `config.yaml`'s `version` field (that one line only, temp file plus atomic rename; the job
log records the old and the new number). After a restart the header badge and the static
assets' `?v=` both match the code again - which is also why the frontend cache invalidates
itself on upgrade. A rollback puts the number back too (unless somebody hand-edited it in the
meantime), and connecting a tarball install syncs it as well.

### The four refusals

| situation | reason | meaning and what to do |
|---|---|---|
| product **source** modified locally (a path outside the protected list) | `local_source_edits` | An update is a download, not a conflict decision. The file names are listed; commit or restore them first |
| branch **diverged** from its remote (ahead and behind at once) | `diverged` | That is a merge decision; the counts are reported so you can resolve it by hand |
| no Go toolchain on this box | `no_toolchain` | **The source still updates**; only the binary is not swapped. Install `go` and press update again to finish |
| this directory is not a git work tree | `not_a_repo` (`installed: false` in the status) | A tarball installation has no remote to fast-forward; update it through the release package |

`apply` is asynchronous, so it always answers `202` first and the refusals above arrive in the polled
`job.failure` (`reason` / `message` / `items`). `rollback` is synchronous and maps refusals to status
codes (`local_source_edits`, `diverged`, `no_state`, `moved_since_update`, `no_binary` -> 409, anything
else -> 400). Other failures (`merge_failed`, `build_failed`, `swap_failed`) carry git's or the
compiler's own output; a failed build leaves the previous binary in place while the source has already
moved and the state file is written, so it stays rollback-able.

Check, update, adoption, rollback and restart share an operating-system file lock in the install
directory (`.update-lock`). Wait for the other operation when `update_busy` is reported. The system
releases the lock when its process exits; do not delete the lock file to bypass it.

### How operator content survives

`roles/ skills/ tools/ agents/ knowledge_base/ data/ log/ venv/ config.yaml .env` belong to the
**operator** (`update.Protected` is the judge). Before the merge, only those of those paths the
update actually writes to are copied into `.update-backup/<unique-backup>/` and removed from the tree -
your extra directories that upstream never touched are not disturbed - and they are put back
afterwards, which is what "update the code, keep my work" means. The result names every file in
`keptContent` instead of quietly dropping it: where both you and upstream changed the same file, the
version that lands is yours, and the result says so.

### Rollback

`./cyberstrike-ai -update-rollback`, or the button on the page: `git reset --hard` back to the commit
the update came from, the binary kept as `cyberstrike-ai.prev` is put back when that update swapped
one, and `.update-state.json` is removed. An update whose build failed never swapped a binary, so its
rollback moves the source only - the binary on disk already belongs to the commit it returns to. It
refuses when there is no update record (`no_state`), a swap is owed but no `cyberstrike-ai.prev` is
left (`no_binary`), a recorded commit is not in this repository (`bad_state`), the binary on disk is
not the one the record pairs with that commit (`binary_changed`), **HEAD has moved since that update**
(`moved_since_update` - a rollback undoes the update, not the work done after it), or source is
modified locally (`local_source_edits`).

Rollback writes `.update-rollback-pending.json` before changing source or binaries. Run rollback
again after an interruption to finish recovery; the console reports `rollbackPending` and pauses new
updates. Keep the recovery record, backup directory and `.prev` file. If recorded binary hashes do not
match the files on disk, recovery refuses to overwrite them.

### Switching to the new binary

On macOS and Linux, when started from the `cyberstrike-ai` binary alongside its config, the console
sends `restart: true` as soon as you click **One-Click Update**. After a successful build and binary swap, the service closes its
application resources and executes the new binary with the same PID, arguments, environment and
working directory. Installations that cannot switch in place return `400` on an explicit restart
request. CLI updates and REST updates without `restart: true` only replace files on disk.

**A pending binary is a durable state, and it can be activated later.** The status endpoint compares
the binary identity captured at startup (size + nanosecond mtime) with what is on disk now: after an
update, a rollback or a CLI run swapped the file without a restart, `needsRestart` is true,
`binaryBuiltAt` says when it was built, and the console grows a persistent banner with **Restart now**
(`POST /api/system/update/restart`; `409 nothing_pending` when there is nothing to activate, `409`
while a job runs, `400` when unavailable). The console checks `canRestart` before requesting automatic activation.

**The page reloads automatically.** The console polls the session validation endpoint every two
seconds for the new boot marker. When both versions support session handoff, existing sessions keep
their original expiration and permissions are loaded from the current RBAC store. The first upgrade
from an older version may still require sign-in. Leaving the console stops the polling.

## Adding a Business Module

Do not add only a handler. A complete module usually needs:

1. Data model and SQLite migration.
2. Handler: parameters, errors, pagination/filtering.
3. Audit: management actions.
4. Monitor: long-running execution state.
5. MCP: whether Agents should call it.
6. HITL: approval boundary for MCP tools.
7. OpenAPI: update `/api/openapi/spec`.
8. Frontend: i18n, states, empty/error UI.
9. Tests: DB, handler, edge cases.
10. Docs: config, usage, troubleshooting, safety impact.

Missing one of these usually becomes a later usability or safety bug.

## Error Response Design

Prefer stable JSON:

```json
{
  "error": "machine_readable_code",
  "message": "human-readable explanation"
}
```

Frontend needs stable fields, users need actionable messages, and logs need detailed internal errors.

## Long-Running Tasks

For scanning, indexing, batch tasks, C2, or external operations, answer:

- Can it be cancelled?
- Can progress be queried?
- Can it be retried?
- Where is the result stored?
- Does state survive page refresh?
- Does it block the HTTP request?

If not, use task tables, event streams, or monitoring.

## Extending Tools

Prefer `tools/*.yaml` for command tools. Use Go built-in tools when the tool needs internal state or structured integration.

Built-in tools should define clear input schemas, handle timeouts and errors, and respect HITL for risky actions.

## Frontend Changes

Use existing helpers such as `apiFetch`, modal utilities, notifications, and i18n. Update both `web/static/i18n/zh-CN.json` and `web/static/i18n/en-US.json` for new visible text.

Avoid putting secrets or provider keys in frontend code.

## Test Priority

High-value tests:

- config hot-apply;
- HITL branches;
- shell timeout/no-output;
- external MCP recovery;
- KB indexing and post-processing;
- WebShell OS/encoding detection;
- SQLite migration compatibility.

## Source Anchors

- App wiring: `internal/app/app.go`
- Config apply: `internal/handler/config.go`
- OpenAPI: `internal/handler/openapi.go`
- Tool executor: `internal/security/executor.go`
- Skill package: `internal/skillpackage/`
- One-click update: `internal/update/` (status, apply, rollback), HTTP surface in
  `internal/handler/update.go`, CLI switches in `cmd/server/update_cli.go`, thin shell in
  `upgrade.sh`

### Recovery evidence and MCP activation edits

Self-update recovery validates JSON field names/types, duplicate keys, commit/hash relationships and the actual binary files before resuming filesystem changes. The plain-text legacy build marker remains supported, but any accompanying State is validated too. An ambiguous, incomplete or unsupported record is refused with the files and recovery evidence preserved. Do not delete a pending record just to bypass the refusal: first reconcile the source commit, live binary and previous binary/backup.

Editing an existing external MCP without activation fields preserves its current enabled/stopped state. Explicit `disabled` or `external_mcp_enable` fields change activation; `disabled: true` takes precedence. A new server defaults to enabled. Start/Stop also persist the corresponding disabled state, so editing or restarting the application does not silently enable a stopped server.
