#!/bin/sh
# GitLab job images do not all ship git. helpdrift and the tests shell out to it.
set -eu

if command -v git >/dev/null 2>&1; then
  exit 0
fi
if command -v apt-get >/dev/null 2>&1; then
  apt-get update -qq
  apt-get install -y -qq git
  exit 0
fi
if command -v apk >/dev/null 2>&1; then
  apk add --no-cache git
  exit 0
fi
echo "git is not installed and no package manager was found" >&2
exit 1
