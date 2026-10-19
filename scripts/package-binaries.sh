#!/bin/sh
# Cross-compile helpdrift. In GitLab CI, upload each binary to the generic
# package registry with the predefined CI_JOB_TOKEN.
# https://docs.gitlab.com/user/packages/generic_packages/
set -eu

cd "$(dirname "$0")/.."

if [ -n "${CI_COMMIT_TAG:-}" ]; then
  version="$CI_COMMIT_TAG"
elif [ -n "${1:-}" ]; then
  version="$1"
else
  version="0.0.0-${CI_COMMIT_SHORT_SHA:-local}"
fi

mkdir -p dist
rm -f dist/helpdrift-*

for pair in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64; do
  os="${pair%%/*}"
  arch="${pair#*/}"
  name="helpdrift-${os}-${arch}"
  if [ "$os" = "windows" ]; then
    name="${name}.exe"
  fi
  echo "building ${name}"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
    go build -trimpath -ldflags "-s -w -X main.version=${version}" \
    -o "dist/${name}" ./cmd/helpdrift
done

if [ -z "${CI_JOB_TOKEN:-}" ] || [ -z "${CI_API_V4_URL:-}" ] || [ -z "${CI_PROJECT_ID:-}" ]; then
  echo "package: built dist/ for ${version} (no CI package registry environment, skipping upload)"
  exit 0
fi

for f in dist/helpdrift-*; do
  base=$(basename "$f")
  url="${CI_API_V4_URL}/projects/${CI_PROJECT_ID}/packages/generic/helpdrift/${version}/${base}"
  echo "uploading ${base} as helpdrift ${version}"
  curl --fail --show-error --location --retry 3 --retry-delay 2 \
    --header "JOB-TOKEN: ${CI_JOB_TOKEN}" \
    --upload-file "$f" \
    "$url"
  echo
done
