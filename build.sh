#!/usr/bin/env bash
# Builds the plugin for the current platform into dist/.
#
# This is the developer build. Release artefacts come from
# scripts/package-release.sh, which packages the archive and checksum.
set -euo pipefail

cd "$(dirname "$0")"
source scripts/platforms.sh

target="$(go env GOOS)/$(go env GOARCH)"
require_platform "$target"
extension="$(require_library_extension "${target%%/*}")"

mkdir -p dist
CGO_ENABLED=1 go build -buildmode=c-shared -trimpath \
  -o "dist/deepseek-cpa.${target%%/*}.${target##*/}" ./cmd/deepseekcpa

echo "built dist/deepseek-cpa.${target%%/*}.${target##*/}"
echo "install as <cliproxyapi>/plugins/deepseek-cpa.${extension} (the plugin id comes from the file name)"
