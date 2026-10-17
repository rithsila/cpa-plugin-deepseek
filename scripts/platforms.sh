#!/usr/bin/env bash
# Shared platform-matrix helpers.
#
# platforms.txt is the only source of truth for which platforms ship and which
# runner builds each one. build.sh, scripts/package-release.sh and the release
# workflow all read it through this file, including the comment-stripping rule,
# so they cannot disagree about what a line means.

platforms_file() {
  printf '%s\n' "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/platforms.txt"
}

# platforms_entries prints "<goos>/<goarch> <runner> <toolchain>" for every line.
# A line is: everything after '#' is a comment, surrounding whitespace is
# trimmed, and the fields are the target, the runner and the toolchain setup.
platforms_entries() {
  awk '
    { sub(/#.*/, ""); gsub(/^[ \t\r]+|[ \t\r]+$/, ""); if (NF) print $1 " " $2 " " (NF >= 3 ? $3 : "native") }
  ' "$(platforms_file)"
}

platforms_targets() {
  platforms_entries | awk '{ print $1 }'
}

# require_platform <goos>/<goarch> exits when the target is not in the matrix.
require_platform() {
  local target="$1"
  if ! platforms_targets | grep -qx "$target"; then
    echo "unsupported platform ${target}; platforms.txt lists:" >&2
    platforms_targets | sed 's/^/  /' >&2
    exit 1
  fi
}

# library_extension <goos> prints the dynamic library suffix for that platform.
library_extension() {
  case "$1" in
    linux | freebsd) echo so ;;
    darwin) echo dylib ;;
    windows) echo dll ;;
    *) return 1 ;;
  esac
}

require_library_extension() {
  local goos="$1" extension
  if ! extension="$(library_extension "$goos")"; then
    echo "no dynamic library extension known for ${goos}" >&2
    exit 1
  fi
  printf '%s\n' "$extension"
}

# python_bin prints a usable python interpreter name for this runner.
python_bin() {
  if command -v python3 >/dev/null 2>&1; then echo python3; else echo python; fi
}
