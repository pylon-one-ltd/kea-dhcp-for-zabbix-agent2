#!/usr/bin/env bash
# Prints the CHANGELOG.md section for one version, without its heading.
# Usage: changelog-section.sh 1.2.0 [CHANGELOG.md]
# Fails if the section is missing or empty, so a release can't go out without notes.
set -euo pipefail

version="${1#v}"
file="${2:-CHANGELOG.md}"

notes="$(awk -v v="$version" '
  /^## \[/ {
    if (found) exit
    if (index($0, "## [" v "]") == 1) { found = 1; next }
  }
  found { print }
' "$file")"

# Trim leading and trailing blank lines.
notes="$(printf '%s\n' "$notes" | sed -e '/./,$!d' | sed -e ':a' -e '/^\n*$/{$d;N;ba' -e '}')"

if [ -z "$notes" ]; then
  echo "No '## [$version]' section with content in $file" >&2
  exit 1
fi

printf '%s\n' "$notes"
