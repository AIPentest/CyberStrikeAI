#!/usr/bin/env bash
# Demo only — not executed by CyberStrike server; safe to display when previewing the skill package.
set -euo pipefail
echo "cyberstrike-pi-demo / scripts/check-env.sh"
echo "Purpose: illustrate bundled script resource in a skill package."
echo "This script does not modify the system when shown in a preview or read-only context."
uname -s 2>/dev/null || echo "(uname unavailable in display context)"
