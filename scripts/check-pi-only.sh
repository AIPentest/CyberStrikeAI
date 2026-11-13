#!/usr/bin/env bash
set -euo pipefail
project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$project_root"
pi_module_list="$(go list -m all)"
pi_package_list="$(go list -deps ./...)"
if grep -E 'github.com/(cloudwego/eino|eino-contrib)' <<< "$pi_module_list
$pi_package_list"; then
    echo 'Retired Eino dependency found.' >&2
    exit 1
fi
echo 'Pi-only check passed: no Eino modules or compiled package dependencies.'
