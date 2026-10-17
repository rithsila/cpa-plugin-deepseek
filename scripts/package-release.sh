#!/usr/bin/env bash
# Packages one release artefact for one platform.
#
# Output in dist/:
#   deepseek-cpa_<version>_<goos>_<goarch>.zip  (library at the zip root)
#   checksums.txt                              (sha256, appended per platform)
#
# Usage: scripts/package-release.sh <version> [<goos>/<goarch>]
#
# Archiving and hashing go through python so the script runs on Linux, macOS and
# Windows runners alike; the runner's own zip/sha256sum may not exist.
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
library="${stage}/${plugin_id}.${extension}"
archive="${plugin_id}_${version}_${GOOS}_${GOARCH}.zip"

rm -rf "$stage"
mkdir -p "$stage" dist

# The version the plugin reports at registration comes from the release tag.
CGO_ENABLED=1 go build -buildmode=c-shared -trimpath \
  -ldflags "-X github.com/xspeed1989/cpa-plugin-deepseek.pluginVersion=${version}" \
  -o "$library" ./cmd/deepseekcpa

# c-shared also writes a C header next to the library; the plugin store expects
# the archive root to hold the library and nothing else.
rm -f "${stage}/${plugin_id}.h" "${stage}/${plugin_id}.hpp"

PY="$(python_bin)"
"$PY" - "$library" "dist/${archive}" <<'PY'
import os
import sys
import zipfile

library, archive = sys.argv[1], sys.argv[2]
with zipfile.ZipFile(archive, "w", zipfile.ZIP_DEFLATED) as bundle:
    bundle.write(library, arcname=os.path.basename(library))
PY

"$PY" - "dist/${archive}" "dist/checksums.txt" <<'PY'
import hashlib
import os
import sys

archive, checksums = sys.argv[1], sys.argv[2]
digest = hashlib.sha256(open(archive, "rb").read()).hexdigest()
with open(checksums, "a", encoding="utf-8") as out:
    out.write(f"{digest}  {os.path.basename(archive)}\n")
PY

echo "packaged dist/${archive}"
