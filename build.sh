#!/usr/bin/env bash
# Builds the plugin for the current platform into dist/.
#
# This is the developer build. Release artefacts come from
# scripts/package-release.sh, which also validates the platform against
# platforms.txt.
set -euo pipefail

cd "$(dirname "$0")"

goos="$(go env GOOS)"
goarch="$(go env GOARCH)"
target="${goos}/${goarch}"

if ! awk '!/^#/ && NF {print $1}' platforms.txt | grep -qx "$target"; then
  echo "unsupported platform ${target}; platforms.txt lists:" >&2
  awk '!/^#/ && NF {print "  " $1}' platforms.txt >&2
  exit 1
fi

case "$goos" in
  linux) extension="so" ;;
  darwin) extension="dylib" ;;
  *) echo "no dynamic library extension known for ${goos}" >&2; exit 1 ;;
esac

mkdir -p dist
CGO_ENABLED=1 go build -buildmode=c-shared -trimpath \
  -o "dist/deepseek-cpa.${goos}.${goarch}" ./cmd/deepseekcpa

echo "built dist/deepseek-cpa.${goos}.${goarch}"
echo "install as <cliproxyapi>/plugins/deepseek-cpa.${extension} (the plugin id comes from the file name)"
