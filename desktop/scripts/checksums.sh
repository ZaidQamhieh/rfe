#!/bin/sh
# Writes SHA256SUMS for every file in a directory (names relative to it, sorted), so a release
# can publish the sums next to its files.
#
#   desktop/scripts/checksums.sh dist/
set -eu

if [ "$#" -ne 1 ] || [ ! -d "$1" ]; then
  echo "usage: $0 <directory>" >&2
  exit 2
fi
cd "$1"
rm -f SHA256SUMS
files=$(find . -maxdepth 1 -type f ! -name SHA256SUMS | sed 's#^\./##' | LC_ALL=C sort)
if [ -z "$files" ]; then
  echo "$0: no files in $1" >&2
  exit 1
fi
# shellcheck disable=SC2086 # the names come from find and contain no newline
printf '%s\n' "$files" | while IFS= read -r f; do sha256sum -- "$f"; done > SHA256SUMS
echo "wrote $(wc -l < SHA256SUMS) sums to $1/SHA256SUMS"
