#!/usr/bin/env bash
# Rewrite the Markdown format command in Go source only.
# docs/ stays on the old command so `helpdrift check` reports drift.
# The old command is assembled from two literals so this script is not
# itself a copy of the claim the articles make.
# Restore with: git checkout -- cmd/helpdrift/main.go
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

old="helpdrift check --format ""md"
new="${old%md}markdown"

changed=0
while IFS= read -r -d '' f; do
  if grep -q -F "$old" "$f"; then
    # sed uses | so the command's slashes do not need escaping.
    sed -i "s|${old}|${new}|g" "$f"
    echo "updated ${f#./}"
    changed=$((changed + 1))
  fi
done < <(find . -name '*.go' -not -path './testdata/*' -not -path './.git/*' -print0)

if [ "$changed" -eq 0 ]; then
  echo "demo-rename: no Go source contained the Markdown format command" >&2
  exit 1
fi

echo "demo-rename: ${changed} Go file(s) now say --format markdown. Docs were not changed."
echo "Next: helpdrift check --base HEAD --docs 'docs/**/*.md' --fail-on-findings"
