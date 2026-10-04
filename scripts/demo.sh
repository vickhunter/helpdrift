#!/usr/bin/env bash
# Builds a throwaway git repo from testdata/fixtures/demo (before -> after)
# and runs helpdrift against it. Usage: scripts/demo.sh [path/to/helpdrift]
set -euo pipefail

here="$(cd "$(dirname "$0")/.." && pwd)"
bin="${1:-$here/bin/helpdrift}"
case "$bin" in /*) ;; *) bin="$here/$bin" ;; esac
fixture="$here/testdata/fixtures/demo"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

g() { git -C "$work" -c user.name=demo -c user.email=demo@example.invalid -c commit.gpgsign=false "$@"; }

g init -q -b main
cp -R "$fixture/before/." "$work/"
g add -A && g commit -q -m "before"
find "$work" -mindepth 1 -maxdepth 1 ! -name .git -exec rm -rf {} +
cp -R "$fixture/after/." "$work/"
g add -A && g commit -q -m "rename export flag, button and billing route; raise Pro price"

echo "\$ helpdrift check --base HEAD~1 --head HEAD --docs 'help/**' --fail-on-findings"
set +e
"$bin" check --root "$work" --base HEAD~1 --head HEAD --docs 'help/**' --fail-on-findings
status=$?
set -e
echo "(exit status: $status)"
