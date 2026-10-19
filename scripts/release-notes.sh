#!/bin/sh
# Deterministic release notes for the GitLab Release job.
# The Duo release-notes flow drafts a separate set. See docs/AUTOPILOT.md.
set -eu

tag="${CI_COMMIT_TAG:-unreleased}"
printf '%s\n' "# helpdrift ${tag}" ""
printf '%s\n' "Notes generated from git history by scripts/release-notes.sh." ""
printf '%s\n' "## Changes" ""

if ! command -v git >/dev/null 2>&1 || ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  printf '%s\n' "- Git history was not available in this job."
  exit 0
fi

range=""
if git rev-parse --verify "HEAD^" >/dev/null 2>&1; then
  if prev=$(git describe --tags --abbrev=0 "HEAD^" 2>/dev/null); then
    range="${prev}..HEAD"
  fi
fi

if [ -n "$range" ]; then
  git log --pretty='- %s' "$range" | tr -d '`'
else
  git log --pretty='- %s' -n 20 | tr -d '`'
fi
printf '\n%s\n' "Binaries for this version are in the generic package registry."
