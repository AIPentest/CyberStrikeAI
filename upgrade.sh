#!/usr/bin/env bash
set -euo pipefail

# CyberStrikeAI upgrade entry point.
#
# Two installation kinds, decided at runtime from this directory itself:
#
#  1. git work tree (the normal case):
#     the update is delegated to the platform binary, `./cyberstrike-ai -update`, which
#     fetches the configured update source (config.yaml update.repo; empty = the official
#     repository, and one field points it at a fork or a mirror) and follows that
#     repository's own default branch, fast-forwards, rebuilds with `go build` and swaps
#     the binary in atomically. The CLI reads the same config field the console page
#     saves, so the keyboard and the button update from the same place.
#     Refusals (local source edits, diverged history) come from that same implementation
#     and name the files/commits involved.
#
#  2. Plain directory (Release tarball install, no git): falls back to downloading a
#     Release tarball and synchronizing it with `rsync --delete`. Here the source
#     repository must be named, so it is taken from --repo, else the GITHUB_REPO
#     environment variable, and only if neither is set from the built-in default - with a
#     warning that says out loud which repository the code is coming from.
#
# Default preserves:
# - config.yaml
# - data/
# - venv/ (disabled with --no-venv)
# - tools/ (user extensions; never overwritten by upgrade)
# - roles/, skills/, agents/
# - the rollback points of an earlier one-click update:
#   .update-backup/, .update-state.json, .update-build-pending, cyberstrike-ai.prev

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT_DIR"

BINARY_NAME="cyberstrike-ai"
CONFIG_FILE="$ROOT_DIR/config.yaml"
DATA_DIR="$ROOT_DIR/data"
VENV_DIR="$ROOT_DIR/venv"
KNOWLEDGE_BASE_DIR="$ROOT_DIR/knowledge_base"

BACKUP_BASE_DIR="$ROOT_DIR/.upgrade-backup"
UPGRADE_TMP_DIR=""

# Built-in fallback, used ONLY when this directory is not a git work tree and neither
# --repo nor GITHUB_REPO was given. See resolve_repo().
DEFAULT_GITHUB_REPO="AIPentest/CyberStrikeAI"
GITHUB_REPO="${GITHUB_REPO:-}"
REPO_ARG=""

TAG=""
PRESERVE_VENV=1
STOP_SERVICE=1
FORCE_STOP=0
YES=0
CHECK_ONLY=0

# Set by resolve_install_kind(): "git" or "tarball".
INSTALL_KIND=""

usage() {
  cat <<'EOF'
Usage:
  ./upgrade.sh [--check] [--yes] [--no-venv] [--no-stop] [--force-stop]
                [--tag vX.Y.Z] [--repo <owner/name>]

What it does depends on the installation kind (detected automatically):

  git work tree     Updates from the configured source repository (config.yaml
                    update.repo; empty = the official repository) by calling
                    ./cyberstrike-ai -update (fetch, fast-forward, go build, swap binary).
                    --tag/--repo are ignored on this path. Your own roles/skills/tools/
                    agents/data/config are put aside and restored by the platform;
                    local source edits or diverged history stop the update instead of
                    being overwritten.
  plain directory   (Release tarball install, no git) Downloads the Release tarball and
                    syncs it in with rsync --delete. --repo / GITHUB_REPO apply here.

Options:
  --check             Report only, change nothing: never stops the service, never writes.
                      git tree: ./cyberstrike-ai -check-update, or
                      go run ./cmd/server -check-update while there is no binary yet.
                      plain directory: prints the newest Release tag and the version
                      recorded in config.yaml.
  --repo <owner/name> Release repository to read (tarball installs only). Takes precedence
                      over $GITHUB_REPO. Without either, the built-in default is used and
                      the script warns about it.
  --tag <tag>         Specify GitHub Release tag (e.g. v1.3.28).
                      If omitted, the script uses the latest release.
  --no-venv           Do not preserve venv/ (Python deps will be re-installed).
  --no-stop           Do not stop the running service, and do not start it afterwards
                      (use this under systemd or any supervisor; restart the unit yourself).
  --force-stop        If no process matching current directory is found, also stop
                      any cyberstrike-ai processes (use with caution).
  --yes               Do not ask for confirmation.

Rollback:
  ./cyberstrike-ai -update-rollback
  Undoes the last one-click update (back to the commit and the binary kept before it).
EOF
}

log() { printf "%s\n" "$*"; }
info() { log "[INFO]  $*"; }
warn() { log "[WARN]  $*"; }
err() { log "[ERROR] $*"; }

have_cmd() { command -v "$1" >/dev/null 2>&1; }

http_get() {
  # $1: url
  if have_cmd curl; then
    # If GITHUB_TOKEN is provided, use it for api.github.com to avoid low rate limits.
    if [[ -n "${GITHUB_TOKEN:-}" && "$1" == https://api.github.com/* ]]; then
      # Do not use `-f` so we can parse GitHub error JSON bodies and show `message`.
      curl -sSL -H "Authorization: Bearer ${GITHUB_TOKEN}" "$1"
    else
      # Do not use `-f` so we can parse GitHub error JSON bodies and show `message`.
      curl -sSL "$1"
    fi
  elif have_cmd wget; then
    wget -qO- "$1"
  else
    err "curl or wget is required to download GitHub releases. Please install one of them."
    exit 1
  fi
}

physical_path() {
  # $1: existing directory; symlinks resolved, so paths reported by git compare cleanly
  # (macOS /tmp and /var are symlinks).
  ( cd "$1" 2>/dev/null && pwd -P ) || true
}

is_git_work_tree() {
  # $1: directory
  have_cmd git && git -C "$1" rev-parse --is-inside-work-tree >/dev/null 2>&1
}

resolve_install_kind() {
  if ! have_cmd git; then
    INSTALL_KIND="tarball"
    info "git is not installed: treating this directory as a tarball install (Release download)."
    return 0
  fi

  if ! is_git_work_tree "$ROOT_DIR"; then
    INSTALL_KIND="tarball"
    info "This directory is not a git work tree: using the Release tarball path."
    return 0
  fi

  local raw_top top_phys root_phys
  raw_top="$(git -C "$ROOT_DIR" rev-parse --show-toplevel 2>/dev/null || true)"
  if [[ -n "$raw_top" ]]; then
    top_phys="$(physical_path "$raw_top")"
    root_phys="$(physical_path "$ROOT_DIR")"
    if [[ -n "$top_phys" && "$top_phys" != "$root_phys" ]]; then
      err "This script lives in ${ROOT_DIR}, which is a subdirectory of the repository root ${raw_top}."
      err "One-click update only ever moves a repository root, so run the upgrade from ${raw_top}."
      exit 1
    fi
  fi

  INSTALL_KIND="git"
}

show_git_source() {
  # Say out loud where the code is coming from: that is the whole point of this path.
  # The source itself is one field (config.yaml update.repo, empty = the official
  # repository) and the binary is the one that reads it - `-check-update` prints the
  # exact repository and branch that would land, without changing anything.
  local branch
  branch="$(git -C "$ROOT_DIR" rev-parse --abbrev-ref HEAD 2>/dev/null || true)"
  info "Install directory: ${ROOT_DIR}"
  info "Local branch: ${branch:-unknown}"
  info "Update source: config.yaml update.repo (empty = the official repository); ./${BINARY_NAME} -check-update prints it exactly."
}

explain_no_go() {
  err "No compiled ${BINARY_NAME} binary in ${ROOT_DIR} and no Go toolchain on PATH."
  err "One of the two is required: an existing binary rebuilds itself, a source-only"
  err "checkout needs Go to produce the first binary."
  err "Install Go (this module requires Go 1.25 or newer):"
  err "  download:  https://go.dev/dl/"
  err "  macOS:     brew install go"
  err "  Debian/Ubuntu: sudo apt install golang-go   (or the official tarball in /usr/local/go)"
  err "Then re-run: ./upgrade.sh"
}

stop_service() {
  # Try to stop the service that is running from the current project directory.
  # If nothing is found and --force-stop is enabled, stop all cyberstrike-ai processes.
  if [[ "$STOP_SERVICE" -ne 1 ]]; then
    return 0
  fi

  local pids=""
  if have_cmd pgrep; then
    # Prefer matches where the command line contains the current project path.
    pids="$(pgrep -f "${ROOT_DIR}.*${BINARY_NAME}" || true)"
    if [[ -z "$pids" && "$FORCE_STOP" -eq 1 ]]; then
      warn "No ${BINARY_NAME} process found under the current directory. Will try to force-stop all matching ${BINARY_NAME} processes."
      pids="$(pgrep -f "${BINARY_NAME}" || true)"
    fi
  fi

  if [[ -z "$pids" ]]; then
    info "No ${BINARY_NAME} process detected (or no matching process). Skipping stop step."
    return 0
  fi

  warn "Detected running PID(s): ${pids}"
  for pid in $pids; do
    if kill -0 "$pid" 2>/dev/null; then
      info "Sending SIGTERM to PID=${pid}..."
      kill -TERM "$pid" 2>/dev/null || true
    fi
  done

  # Wait for exit
  local deadline=$((SECONDS + 20))
  while [[ $SECONDS -lt $deadline ]]; do
    local alive=0
    for pid in $pids; do
      if kill -0 "$pid" 2>/dev/null; then
        alive=1
        break
      fi
    done
    if [[ "$alive" -eq 0 ]]; then
      info "Service stopped."
      return 0
    fi
    sleep 1
  done

  warn "Timed out waiting for processes to exit. Still running PID(s): ${pids} (may still hold file handles)."
  return 0
}

start_service() {
  if [[ "$STOP_SERVICE" -ne 1 ]]; then
    info "--no-stop: the service was left untouched. Restart it yourself (for example: systemctl restart cyberstrikeai)."
    return 0
  fi
  if [[ ! -f "$ROOT_DIR/run.sh" ]]; then
    warn "run.sh not found: starting the service is left to you."
    return 0
  fi
  info "Starting service..."
  chmod +x ./run.sh
  ./run.sh
}

confirm_or_exit() {
  if [[ "$YES" -eq 1 ]]; then
    return 0
  fi

  if [[ ! -t 0 ]]; then
    err "Non-interactive terminal detected. Please add --yes to continue."
    exit 1
  fi

  if [[ "$INSTALL_KIND" == "git" ]]; then
    warn "About to update this installation from its configured update source:"
    info " - git fetch + fast-forward of ${ROOT_DIR}"
    info "   (refused with file names if source files are modified locally, or if the"
    info "    local history has diverged from the source - that is a merge decision, not a download)"
    info " - go build, then the binary is swapped; the previous one is kept as ${BINARY_NAME}.prev"
    info " - your content (roles/skills/tools/agents/knowledge_base/data/config.yaml)"
    info "   is put aside before the merge and restored after it; the result lists what was kept"
    info " - rollback stays available: .update-backup/<timestamp>/ + .update-state.json"
    info " - Stop service: ${STOP_SERVICE}"
  else
    warn "About to perform upgrade:"
    info " - Source repository: ${GITHUB_REPO:-unset} (this directory is not a git work tree)"
    info " - Preserve config.yaml: yes"
    info " - Preserve data/: yes"
    if [[ "$PRESERVE_VENV" -eq 1 ]]; then
      info " - Preserve venv/: yes"
    else
      info " - Preserve venv/: no (will remove old venv and re-install deps)"
    fi
    info " - Preserve tools/: yes (always)"
    info " - Preserve roles/skills/agents: yes (always)"
    info " - Preserve rollback points (.update-backup/, .update-state.json, .update-build-pending, ${BINARY_NAME}.prev): yes"
    info " - Stop service: ${STOP_SERVICE}"
  fi

  echo ""
  read -r -p "Continue? (y/N) " ans
  if [[ "${ans:-N}" != "y" && "${ans:-N}" != "Y" ]]; then
    err "Cancelled."
    exit 1
  fi
}

run_platform_update_cli() {
  # $1: '-check-update' or '-update'. Same implementation the console page drives, so a
  # keyboard session and the API cannot disagree about what an update means.
  local flag="$1"
  if [[ ! -f "$ROOT_DIR/$BINARY_NAME" ]]; then
    err "Expected ${BINARY_NAME} in ${ROOT_DIR} but it is not there."
    err "Run ./upgrade.sh again (it builds the binary first) or use --check."
    return 1
  fi
  if [[ ! -x "$ROOT_DIR/$BINARY_NAME" ]]; then
    chmod +x "./$BINARY_NAME"
  fi
  info "Running: ./${BINARY_NAME} ${flag}"
  local rc=0
  ./"$BINARY_NAME" "$flag" || rc=$?
  return "$rc"
}

report_update_refusal() {
  # $1: exit code. The platform already printed the exact reason and the file names above;
  # this only says what it means for the tree, because the two outcomes look the same.
  warn "${BINARY_NAME} exited with code $1. Read the reason printed above."
  info " - Local source edits / diverged branch: nothing was moved. Commit or restore your"
  info "   own source changes (or merge by hand), then run ./upgrade.sh again."
  info " - No Go toolchain: the source DID move and your content was kept; the binary is"
  info "   still the previous one. Install Go and run ./upgrade.sh again to finish."
  info " - Undo the last successful update: ./${BINARY_NAME} -update-rollback"
}

build_first_binary() {
  info "No ${BINARY_NAME} binary yet: building one before updating (./cmd/server, first run may download modules)."
  if ! have_cmd go; then
    explain_no_go
    exit 1
  fi
  if ! go build -o "$BINARY_NAME" ./cmd/server; then
    err "The first build failed, so the platform has no binary to update itself with."
    err "Fix the build (see the compiler output above) and run ./upgrade.sh again."
    exit 1
  fi
  if [[ ! -f "$BINARY_NAME" ]]; then
    err "go build reported success but produced no ${BINARY_NAME} in ${ROOT_DIR}."
    exit 1
  fi
  chmod +x "./$BINARY_NAME"
}

check_git_install() {
  # Read-only path: never stops or starts the service.
  if [[ -f "$ROOT_DIR/$BINARY_NAME" ]]; then
    local rc=0
    run_platform_update_cli "-check-update" || rc=$?
    if [[ "$rc" -ne 0 ]]; then
      report_update_refusal "$rc"
    fi
    return "$rc"
  fi
  if have_cmd go; then
    info "No ${BINARY_NAME} binary yet, so the check is asked of the source tree: go run compiles into the build cache and leaves this directory untouched."
    local rc=0
    go run ./cmd/server -check-update || rc=$?
    if [[ "$rc" -ne 0 ]]; then
      report_update_refusal "$rc"
    fi
    return "$rc"
  fi
  explain_no_go
  exit 1
}

update_git_install() {
  confirm_or_exit

  stop_service

  if [[ ! -f "$ROOT_DIR/$BINARY_NAME" ]]; then
    build_first_binary
  fi
  if ! have_cmd go; then
    warn "No Go toolchain on PATH: the update will move the source and keep your content, but it cannot rebuild the binary."
    warn "Install Go and run ./upgrade.sh again to swap in the new binary."
  fi

  # A fast-forward may rewrite this script as well; harmless, the running copy is already
  # open, and the next invocation reads the updated file.
  local rc=0
  run_platform_update_cli "-update" || rc=$?
  if [[ "$rc" -ne 0 ]]; then
    report_update_refusal "$rc"
    return "$rc"
  fi

  info "Source and binary are in place (the running process is still the old one until it is restarted)."
  start_service
}

ensure_git_style_env() {
  # No hard requirement; just a sanity check.
  if [[ ! -f "$CONFIG_FILE" ]]; then
    err "Could not find ${CONFIG_FILE}. Please verify you are in the correct project directory."
    exit 1
  fi
}

resolve_repo() {
  # Tarball path only: without a repository of our own, the source has to be named, and
  # naming it silently is how a fork gets overwritten. --repo beats GITHUB_REPO, and the
  # built-in default is a last resort that says so.
  if [[ -n "$REPO_ARG" ]]; then
    GITHUB_REPO="$REPO_ARG"
  fi
  if [[ -z "${GITHUB_REPO:-}" ]]; then
    GITHUB_REPO="$DEFAULT_GITHUB_REPO"
    warn "Neither --repo nor GITHUB_REPO was given, so the built-in default repository is used."
  fi
  if [[ ! "$GITHUB_REPO" =~ ^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$ ]]; then
    err "Invalid repository: ${GITHUB_REPO} (expected the form owner/name)."
    exit 1
  fi
  warn "Not a git work tree: code will be downloaded from https://github.com/${GITHUB_REPO} (a Release tarball), not from this directory's own remote."
  warn "If this installation is meant to track your fork, make it a git clone; ./upgrade.sh then pulls from the remote it already has."
}

backup_dir_tgz() {
  # $1: label, $2: path
  local label="$1"
  local path="$2"
  if [[ -e "$path" ]]; then
    info "Backing up ${label} -> ${BACKUP_BASE_DIR}/$(basename "$path").tgz"
    tar -czf "${BACKUP_BASE_DIR}/$(basename "$path").tgz" -C "$ROOT_DIR" "$(basename "$path")"
  fi
}

backup_config() {
  if [[ -f "$CONFIG_FILE" ]]; then
    cp -a "$CONFIG_FILE" "${BACKUP_BASE_DIR}/config.yaml"
  fi
}

resolve_tag() {
  if [[ -n "$TAG" ]]; then
    info "Using specified tag: $TAG"
    return 0
  fi

  local api_url="https://api.github.com/repos/${GITHUB_REPO}/releases/latest"
  info "Fetching latest Release..."
  local json
  json="$(http_get "$api_url")"
  TAG="$(printf '%s' "$json" | python3 - <<'PY'
import json, sys
data=json.loads(sys.stdin.read() or "{}")
print(data.get("tag_name",""))
PY
)"

  if [[ -z "$TAG" ]]; then
    local msg
    msg="$(printf '%s' "$json" | python3 -c "import sys,json; d=json.loads(sys.stdin.read() or '{}'); print(d.get('message',''))" 2>/dev/null || true)"

    # Fallback: try query releases list (sometimes latest endpoint returns error JSON without tag_name).
    local fallback_url="https://api.github.com/repos/${GITHUB_REPO}/releases?per_page=1"
    info "Fallback to: ${fallback_url}"
    local fallback_json
    fallback_json="$(http_get "$fallback_url" 2>/dev/null || true)"
    local fallback_tag
    fallback_tag="$(printf '%s' "$fallback_json" | python3 -c "import sys,json; d=json.loads(sys.stdin.read() or '[]'); print(d[0].get('tag_name','') if isinstance(d,list) and d else '')" 2>/dev/null || true)"

    if [[ -n "$fallback_tag" ]]; then
      TAG="$fallback_tag"
      info "Latest Release tag (fallback): $TAG"
      return 0
    fi

    local snippet
    snippet="$(printf '%s' "$json" | python3 -c "import sys; s=sys.stdin.read(); print(s[:300].replace('\\n',' '))" 2>/dev/null || true)"

    if [[ -n "$msg" ]]; then
      err "Failed to fetch latest tag: ${msg}"
    else
      err "Failed to fetch latest tag."
    fi
    if [[ -n "$snippet" ]]; then
      err "API response snippet: ${snippet}"
    fi
    err "Please try using --tag to specify the version, or set export GITHUB_TOKEN=\"...\"."
    exit 1
  fi
  info "Latest Release tag: $TAG"
}

check_tarball_install() {
  # Nothing to fast-forward without a repository of our own: report the newest published
  # tag against the version recorded in config.yaml, and change no file.
  resolve_tag
  local current=""
  if [[ -f "$CONFIG_FILE" ]]; then
    current="$(grep -m1 -E '^[[:space:]]*version[[:space:]]*:' "$CONFIG_FILE" 2>/dev/null | cut -d: -f2- | tr -d ' "' || true)"
  fi
  info "Newest Release tag of ${GITHUB_REPO}: ${TAG}"
  info "Version recorded in config.yaml: ${current:-unknown}"
  info "Report only: nothing was stopped, downloaded or written. Run ./upgrade.sh without --check to apply."
}

update_config_version() {
  # Replace config.yaml's top-level version with the specified tag. The line matching
  # mirrors the Go writer (internal/config.WriteVersion): top-level key only, in every
  # spelling YAML accepts (version / "version" / 'version'), first occurrence only - a
  # nested key or a later duplicate belongs to the file's own structure and must not be
  # rewritten. A value the line cannot replace safely (block scalar / multi-line scalar)
  # leaves the file alone on purpose: a stale version number is a display problem, a
  # config that no longer loads is an installation that no longer starts.
  local new_tag="$1"
  python3 - "$CONFIG_FILE" "$new_tag" <<'PY'
import os, re, stat, sys, tempfile

path = sys.argv[1]
tag = sys.argv[2]
# Same value rule as the Go writer (internal/config.versionValuePattern): what lands here
# is a live config, and a tag with a quote or newline in it must never reach it.
if not re.match(r"^[A-Za-z0-9][A-Za-z0-9._+-]*$", tag):
    sys.stderr.write("Version %r is invalid (only letters, digits, and . _ + - are allowed); not written to config.yaml\n" % tag)
    sys.exit(0)
with open(path, "r", encoding="utf-8") as f:
    lines = f.readlines()

key = re.compile(r"^(['\"]?version['\"]?)\s*:(.*)$")
replaced = False
refused = False
out = []
for i, line in enumerate(lines):
    if not replaced:
        m = key.match(line)
        if m:
            value = m.group(2).strip()
            nxt = lines[i + 1] if i + 1 < len(lines) else ""
            # Without a YAML parser, only replace a complete scalar on one
            # line. An open quote or flow/anchor value may continue below.
            simple_value = re.fullmatch(r"(?:[A-Za-z0-9._+-]+|'[^'\n]*'|\"[^\"\\\n]*\")?(?:[ \t]+#.*)?", value)
            if simple_value is None or (value == "" and nxt[:1] in (" ", "\t")):
                sys.stderr.write("Version not written: config.yaml's version uses a multi-line/block scalar form that cannot be replaced in place (original file unchanged)\n")
                refused = True
                break
            out.append('version: "%s"\n' % tag)
            replaced = True
            continue
    out.append(line)

if not refused:
    if not replaced:
        insert_at = 0
        for i, line in enumerate(lines):
            first = line.rstrip("\r\n").lstrip("\ufeff")
            rest = first[3:] if first.startswith("---") else None
            if rest is not None and (not rest.strip(" \t") or rest[:1] in (" ", "\t") and rest.lstrip(" \t").startswith("#")):
                insert_at = i + 1
                if not line.endswith(("\n", "\r")):
                    out[i] += "\n"
                break
            trimmed = first.strip()
            if trimmed and not trimmed.startswith("#") and not first.startswith("%"):
                # Only a block mapping at column zero can accept a new key.
                if not re.match(r"^[A-Za-z_][A-Za-z0-9_-]*[ \t]*:", first):
                    sys.stderr.write("Version not written: cannot safely insert a field into this YAML document; original file unchanged\n")
                    sys.exit(0)
                break
        out.insert(insert_at, 'version: "%s"\n' % tag)

    original_mode = stat.S_IMODE(os.stat(path).st_mode)
    tmp_fd, tmp = tempfile.mkstemp(prefix=os.path.basename(path) + ".", suffix=".tmp", dir=os.path.dirname(os.path.abspath(path)))
    try:
        with os.fdopen(tmp_fd, "w", encoding="utf-8") as f:
            f.writelines(out)
            f.flush()
            os.fchmod(f.fileno(), original_mode)
        os.replace(tmp, path)
    except BaseException:
        try:
            os.unlink(tmp)
        except FileNotFoundError:
            pass
        raise
PY
}

sync_code() {
  local tmp_dir="$1"
  local new_src_dir="$2"

  # rsync sync: overwrite files from the new version and delete removed files.
  # Preserve user data/config (and optional directories).

  if ! have_cmd rsync; then
    err "rsync not found. This script depends on rsync for safe synchronization. Please install it and retry."
    exit 1
  fi

  local -a rsync_excludes
  rsync_excludes+=( "--exclude=.upgrade-backup/" )
  rsync_excludes+=( "--exclude=config.yaml" "--exclude=config.yml" "--exclude=config.local.yaml" "--exclude=.env" )
  rsync_excludes+=( "--exclude=data/" )
  rsync_excludes+=( "--exclude=tmp/" "--exclude=chat_uploads/" "--exclude=log/" "--exclude=logs/" "--exclude=bundles/" "--exclude=.venv/" )

  if [[ "$PRESERVE_VENV" -eq 1 ]]; then
    rsync_excludes+=( "--exclude=venv/" )
  fi

  # knowledge_base may not be referenced in config, but many users treat it as the knowledge files directory.
  if [[ -d "$KNOWLEDGE_BASE_DIR" ]]; then
    rsync_excludes+=( "--exclude=knowledge_base/" )
  fi

  # User tool extensions: never replace or delete during upgrade.
  rsync_excludes+=( "--exclude=tools/" )
  rsync_excludes+=( "--exclude=roles/" )
  rsync_excludes+=( "--exclude=skills/" )
  # Markdown agents are operator content too, and the platform's own update protects the
  # same set.
  rsync_excludes+=( "--exclude=agents/" )

  # Rollback points written by a one-click update: an rsync --delete that ate them would
  # leave the installation with no way back - and eating .update-build-pending would make it
  # forget a compile it still owes, so the next click reports "already up to date".
  rsync_excludes+=( "--exclude=.update-backup/" )
  rsync_excludes+=( "--exclude=.update-staging/" )
  rsync_excludes+=( "--exclude=.update-state.json" "--exclude=.update-lock" "--exclude=.update-rollback-pending.json" "--exclude=.update-rollback-pending.json.tmp" )
  rsync_excludes+=( "--exclude=.update-state.json.tmp" )
  rsync_excludes+=( "--exclude=.update-build-pending" )
  rsync_excludes+=( "--exclude=.update-build-pending.tmp" )
  rsync_excludes+=( "--exclude=${BINARY_NAME}.prev" )

  # Ensure this upgrade script itself is not deleted.
  rsync_excludes+=( "--exclude=upgrade.sh" )

  # shellcheck disable=SC2068
  info "Syncing code into current directory (preserving data/config; using rsync --delete)..."
  rsync -a --delete \
    ${rsync_excludes[@]} \
    "${new_src_dir}/" "${ROOT_DIR}/"
}

update_tarball_install() {
  ensure_git_style_env
  confirm_or_exit

  stop_service

  resolve_tag

  local ts
  ts="$(date +"%Y%m%d_%H%M%S")"
  BACKUP_BASE_DIR="${BACKUP_BASE_DIR}/${ts}"
  mkdir -p "$BACKUP_BASE_DIR"

  info "Starting backup into: $BACKUP_BASE_DIR"
  backup_config
  backup_dir_tgz "data" "$DATA_DIR"
  if [[ "$PRESERVE_VENV" -eq 1 ]]; then
    backup_dir_tgz "venv" "$VENV_DIR"
  else
    if [[ -d "$VENV_DIR" ]]; then
      warn "With --no-venv: removing old venv/ (run.sh will re-install Python deps after upgrade)."
      rm -rf "$VENV_DIR"
    fi
  fi
  if [[ -d "$KNOWLEDGE_BASE_DIR" ]]; then
    backup_dir_tgz "knowledge_base" "$KNOWLEDGE_BASE_DIR"
  fi
  if [[ -d "$ROOT_DIR/tools" ]]; then
    backup_dir_tgz "tools" "$ROOT_DIR/tools"
  fi
  if [[ -d "$ROOT_DIR/roles" ]]; then
    backup_dir_tgz "roles" "$ROOT_DIR/roles"
  fi
  if [[ -d "$ROOT_DIR/skills" ]]; then
    backup_dir_tgz "skills" "$ROOT_DIR/skills"
  fi
  if [[ -d "$ROOT_DIR/agents" ]]; then
    backup_dir_tgz "agents" "$ROOT_DIR/agents"
  fi

  UPGRADE_TMP_DIR="$(mktemp -d)"
  trap 'rm -rf "${UPGRADE_TMP_DIR:-}" >/dev/null 2>&1 || true' EXIT

  local tarball="${UPGRADE_TMP_DIR}/source.tar.gz"
  local url="https://github.com/${GITHUB_REPO}/archive/refs/tags/${TAG}.tar.gz"
  info "Downloading source package: ${url}"
  http_get "$url" >"$tarball"

  info "Extracting source package..."
  tar -xzf "$tarball" -C "$UPGRADE_TMP_DIR"

  # GitHub tarball usually creates a top-level directory.
  local extracted_dir
  extracted_dir="$(ls -d "${UPGRADE_TMP_DIR}"/*/ 2>/dev/null | head -n 1 || true)"
  if [[ -z "$extracted_dir" || ! -f "${extracted_dir}/run.sh" ]]; then
    err "run.sh not found in the extracted directory. Please check network/download contents."
    exit 1
  fi

  sync_code "$UPGRADE_TMP_DIR" "$extracted_dir"

  # Update config.yaml version display
  if [[ -f "$CONFIG_FILE" ]]; then
    info "Updating config.yaml version field to: $TAG"
    update_config_version "$TAG"
  fi

  info "Upgrade complete."
  start_service
}

main() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --check)
        CHECK_ONLY=1
        shift 1
        ;;
      --repo)
        REPO_ARG="${2:-}"
        shift 2
        ;;
      --tag)
        TAG="${2:-}"
        shift 2
        ;;
      --no-venv)
        PRESERVE_VENV=0
        shift 1
        ;;
      --no-stop)
        STOP_SERVICE=0
        shift 1
        ;;
      --force-stop)
        FORCE_STOP=1
        shift 1
        ;;
      --yes)
        YES=1
        shift 1
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      *)
        err "Unknown parameter: $1"
        usage
        exit 1
        ;;
    esac
  done

  resolve_install_kind

  if [[ "$INSTALL_KIND" == "git" ]]; then
    if [[ -n "$TAG" ]]; then
      warn "--tag names a GitHub Release and is ignored here: this directory fast-forwards to its own remote, not to a tag."
    fi
    if [[ -n "$REPO_ARG" ]]; then
      warn "--repo names a GitHub Release repository and is ignored here: the remote is the one this directory already tracks."
    fi
    show_git_source
    if [[ "$CHECK_ONLY" -eq 1 ]]; then
      check_git_install
      return 0
    fi
    update_git_install
    return 0
  fi

  # Tarball install: the source has to be named, and naming it out loud comes first.
  resolve_repo
  if [[ "$CHECK_ONLY" -eq 1 ]]; then
    check_tarball_install
    return 0
  fi
  update_tarball_install
}

main "$@"
