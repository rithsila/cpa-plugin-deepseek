#!/usr/bin/env python3
"""Checks the release artefacts against the plugin-store contract.

Run after scripts/package-release.sh. Verifies the archive layout, the checksum
file format, and the registry entry, so a broken release fails here instead of
on the store.
"""
import hashlib
import json
import pathlib
import sys
import zipfile

ROOT = pathlib.Path(__file__).resolve().parent.parent
PLUGIN_ID = "deepseek-cpa"
REPOSITORY = "https://github.com/xspeed1989/cpa-plugin-deepseek"
EXTENSIONS = {"linux": "so", "freebsd": "so", "darwin": "dylib", "windows": "dll"}


def fail(message):
    print(f"FAIL: {message}")
    sys.exit(1)


def check_registry():
    registry = json.loads((ROOT / "registry.json").read_text())
    if registry.get("schema_version") != 1:
        fail(f"registry schema_version = {registry.get('schema_version')!r}, want 1")
    plugins = registry.get("plugins")
    if not isinstance(plugins, list) or len(plugins) != 1:
        fail("registry must describe exactly one plugin")
    entry = plugins[0]
    for field in ("id", "name", "description", "author", "repository"):
        if not str(entry.get(field, "")).strip():
            fail(f"registry entry is missing required field {field!r}")
    if entry["id"] != PLUGIN_ID:
        fail(f"registry id = {entry['id']!r}, want {PLUGIN_ID!r}")
    if entry["repository"] != REPOSITORY:
        fail(f"registry repository = {entry['repository']!r}, want {REPOSITORY!r}")
    print("registry.json ok")


def check_archive(path):
    goos = path.stem.split("_")[-2]
    expected = f"{PLUGIN_ID}.{EXTENSIONS[goos]}"
    with zipfile.ZipFile(path) as archive:
        names = archive.namelist()
    if names != [expected]:
        fail(f"{path.name} contains {names}, want exactly ['{expected}'] at the root")
    print(f"{path.name} ok (root entry {expected})")


def check_checksums(archives):
    checksums = ROOT / "dist" / "checksums.txt"
    if not checksums.exists():
        fail("dist/checksums.txt is missing")
    recorded = {}
    for line in checksums.read_text().splitlines():
        if not line.strip():
            continue
        parts = line.split("  ")
        if len(parts) != 2 or len(parts[0]) != 64:
            fail(f"checksums line is not '<sha256>  <file>': {line!r}")
        recorded[parts[1]] = parts[0]
    for path in archives:
        if path.name not in recorded:
            fail(f"{path.name} has no checksum entry")
        digest = hashlib.sha256(path.read_bytes()).hexdigest()
        if recorded[path.name] != digest:
            fail(f"{path.name} checksum does not match the file")
    print(f"checksums.txt ok ({len(recorded)} entries)")


def main():
    check_registry()
    archives = sorted((ROOT / "dist").glob(f"{PLUGIN_ID}_*_*.zip"))
    if not archives:
        fail("no release archive found in dist/; run scripts/package-release.sh first")
    for path in archives:
        check_archive(path)
    check_checksums(archives)
    print("release artefacts passed")
    return 0


if __name__ == "__main__":
    sys.exit(main())
