#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")" && pwd)"
OUT="$ROOT/dist/cyberstrikeai-browser-extension.zip"
mkdir -p "$ROOT/dist"
rm -f "$OUT"
(cd "$ROOT" && zip -r "$OUT" . \
  -x './dist/*' -x './tests/*' -x './package.sh' -x '*/.DS_Store')
echo "[+] $OUT"
