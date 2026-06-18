#!/usr/bin/env bash
# Print the CHANGELOG.md section body for a given version, for use as GitHub
# Release notes. The version may be "v2.6.2" or "2.6.2" (a leading v is stripped).
# Prints nothing (exit 0) if the section is absent — callers should fall back to
# GoReleaser's auto-generated notes in that case.
#
#   scripts/changelog-section.sh v2.6.2 [CHANGELOG.md]
set -euo pipefail

version="${1:?usage: changelog-section.sh <version> [changelog-file]}"
version="${version#v}"
changelog="${2:-CHANGELOG.md}"

# Capture lines between "## [<version>]" and the next "## [" heading (exclusive),
# then trim leading and trailing blank lines.
awk -v ver="$version" '
  index($0, "## [" ver "]") == 1 { capture = 1; next }
  capture && index($0, "## [") == 1 { exit }
  capture { lines[++n] = $0 }
  END {
    s = 1;  while (s <= n && lines[s] ~ /^[[:space:]]*$/) s++
    e = n;  while (e >= s && lines[e] ~ /^[[:space:]]*$/) e--
    for (i = s; i <= e; i++) print lines[i]
  }
' "$changelog"
