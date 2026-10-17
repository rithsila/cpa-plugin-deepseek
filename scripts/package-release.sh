#!/usr/bin/env bash
# Packages one release artefact for one platform.
#
# Output in dist/:
#   deepseek-cpa_<version>_<goos>_<goarch>.zip  (library at the zip root)
#   checksums.txt                              (sha256, appended per platform)
#
# Usage: scripts/package-release.sh <version> [<goos>/<goarch>]
#
# The optional target lets one runner build both architectures of its platform,
# which macOS needs: the arm64 runner also produces the amd64 artefact.
set -euo pipefail

cd "$(dirname "$0")/.."
source scripts/platforms.sh

version="${1:?usage: scripts/package-release.sh <version> [<goos>/<goarch>]}"
plugin_id="deepseek-cpa"

target="${2:-$(go env GOOS)/$(go env GOARCH)}"
require_platform "$target"
export GOOS="${target%%/*}"
export GOARCH="${target##*/}"
extension="$(require_library_extension "$GOOS")"

stage="dist/stage-${GOOS}-${GOARCH}"
archive="${plugin_id}_${version}_${GOOS}_${GOARCH}.zip"

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
