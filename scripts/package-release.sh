#!/usr/bin/env bash
# Packages one release artefact for the current platform.
#
# Output in dist/:
#   deepseek-cpa_<version>_<goos>_<goarch>.zip  (library at the zip root)
#   checksums.txt                              (sha256, appended per platform)
#
# Usage: scripts/package-release.sh <version>
set -euo pipefail

cd "$(dirname "$0")/.."

version="${1:?usage: scripts/package-release.sh <version>}"
plugin_id="deepseek-cpa"

goos="$(go env GOOS)"
goarch="$(go env GOARCH)"
target="${goos}/${goarch}"

if ! awk '!/^#/ && NF {print $1}' platforms.txt | grep -qx "$target"; then
  echo "unsupported release platform ${target}; platforms.txt lists:" >&2
  awk '!/^#/ && NF {print "  " $1}' platforms.txt >&2
  exit 1
fi

case "$goos" in
  linux) extension="so" ;;
  darwin) extension="dylib" ;;
  *) echo "no dynamic library extension known for ${goos}" >&2; exit 1 ;;
esac

stage="dist/stage-${goos}-${goarch}"
archive="${plugin_id}_${version}_${goos}_${goarch}.zip"

rm -rf "$stage"
mkdir -p "$stage" dist

# The version the plugin reports at registration comes from the release tag.
CGO_ENABLED=1 go build -buildmode=c-shared -trimpath \
  -ldflags "-X github.com/xspeed1989/cpa-plugin-deepseek.pluginVersion=${version}" \
  -o "${stage}/${plugin_id}.${extension}" ./cmd/deepseekcpa

# c-shared also writes a C header next to the library; the plugin store expects
# the archive root to hold the library and nothing else.
rm -f "${stage}/${plugin_id}.h"

( cd "$stage" && zip -q -X "../${archive}" "${plugin_id}.${extension}" )

if command -v sha256sum >/dev/null 2>&1; then
  ( cd dist && sha256sum "$archive" >> checksums.txt )
else
  ( cd dist && shasum -a 256 "$archive" >> checksums.txt )
fi

echo "packaged dist/${archive}"
