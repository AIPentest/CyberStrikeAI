#!/usr/bin/env bash
# RBAC login regressions for both shipped plugins. Requires Node.js and JDK 11+.
set -euo pipefail
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BURP_DIR="$ROOT_DIR/plugins/burp-suite/cyberstrikeai-burp-extension"
TEST_DIR="$(mktemp -d)"
trap 'rm -rf "$TEST_DIR"' EXIT
# Burp supplies this API at runtime; it is only needed here for compilation/UI tests.
API_JAR="${BURP_API_JAR:-$TEST_DIR/burp-extender-api.jar}"
if [[ ! -f "$API_JAR" ]]; then
  curl -fsSL https://repo.maven.apache.org/maven2/net/portswigger/burp/extender/burp-extender-api/2.3/burp-extender-api-2.3.jar -o "$API_JAR"
fi
node --test "$ROOT_DIR/plugins/browser-extension/cyberstrikeai-browser-extension/tests/auth.test.cjs"
find "$BURP_DIR/src/main/java" "$BURP_DIR/src/test/java" -name '*.java' -print0 |
  xargs -0 javac --release 11 -encoding UTF-8 -cp "$API_JAR" -d "$TEST_DIR/classes"
java -Djava.awt.headless=true -cp "$TEST_DIR/classes:$API_JAR" burp.AuthRegressionTest
