#!/usr/bin/env bash
# Builds the plugin dynamic library for the current platform into dist/.
set -euo pipefail

cd "$(dirname "$0")"

goos="$(go env GOOS)"
goarch="$(go env GOARCH)"

mkdir -p dist
CGO_ENABLED=1 go build -buildmode=c-shared -trimpath \
  -o "dist/deepseek-cpa.${goos}.${goarch}" ./cmd/deepseekcpa

echo "built dist/deepseek-cpa.${goos}.${goarch}"
echo "install as <cliproxyapi>/plugins/deepseek-cpa.so (plugin id comes from the file name)"
