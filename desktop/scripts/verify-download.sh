#!/bin/sh
# Checks downloaded files against a SHA256SUMS file from a release. Each file is looked up by its
# name, so the files can be anywhere; one that is missing from the sums, or whose hash differs,
# fails.
#
#   desktop/scripts/verify-download.sh SHA256SUMS "RFE Desktop_1.0.0_amd64.deb" rfe-desktop.sbom.json
set -eu

if [ "$#" -lt 2 ]; then
  echo "usage: $0 <SHA256SUMS> <file>..." >&2
  exit 2
fi
sums=$1
shift
[ -f "$sums" ] || { echo "$0: $sums not found" >&2; exit 2; }

status=0
for file in "$@"; do
  name=$(basename -- "$file")
  if [ ! -f "$file" ]; then
    echo "$name: MISSING (no such file)" >&2
    status=1
    continue
  fi
  # A sums line is "<64 hex>  <name>" (two spaces, or " *" for binary mode).
  want=$(awk -v n="$name" '{ h=$1; $1=""; sub(/^ [ *]?/, ""); if ($0 == n) { print h; exit } }' "$sums")
  if [ -z "$want" ]; then
    echo "$name: NOT LISTED in $sums" >&2
    status=1
    continue
  fi
  got=$(sha256sum -- "$file" | cut -d' ' -f1)
  if [ "$got" = "$want" ]; then
    echo "$name: OK"
  else
    echo "$name: FAILED (expected $want, got $got)" >&2
    status=1
  fi
done
exit "$status"
