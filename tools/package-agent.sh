#!/usr/bin/env bash
# Builds the Go agent for one target, puts the matching Rust sidecars and their manifest next to it, and archives
# the set. The agent refuses a sidecar whose sha256 or version differs from rfe-sidecars.txt, so the files only
# work together: install and update them as one unit.
#
#   tools/package-agent.sh <goos> <goarch> <version> <sidecar_dir> <out_file>
#
# <sidecar_dir> holds rfe-indexd and rfe-thumbd (.exe on windows) built with RFE_RELEASE_VERSION=<version>.
# <out_file> ends in .tar.gz or .zip.
set -euo pipefail
if [[ $# -ne 5 ]]; then
  echo "usage: $0 <goos> <goarch> <version> <sidecar_dir> <out_file>" >&2
  exit 2
fi
goos=$1 goarch=$2 version=$3 sidecars=$4 out=$5
root=$(cd "$(dirname "$0")/.." && pwd)
case "$out" in /*) ;; *) out="$PWD/$out" ;; esac

ext=""
[[ "$goos" == windows ]] && ext=".exe"
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT

# Public update-signing keys (and optionally the manifest URL) are baked into the agent; see tools/update-keys.env.
set -a; . "$root/tools/update-keys.env"; set +a
hu=github.com/ZaidQamhieh/rfe/agent/internal/hostupdate
ldflags="-s -w -X main.version=${version} -X $hu.PrimaryPublicKey=$RFE_UPDATE_PRIMARY_PUBKEY -X $hu.SparePublicKey=$RFE_UPDATE_SPARE_PUBKEY"
[[ -n "${RFE_UPDATE_MANIFEST_URL:-}" ]] && ldflags="$ldflags -X $hu.ManifestURL=$RFE_UPDATE_MANIFEST_URL"
(cd "$root/agent" && CGO_ENABLED=0 GOOS=$goos GOARCH=$goarch \
  go build -trimpath -ldflags "$ldflags" -o "$stage/rfe$ext" ./cmd/agent)

# The supervising launcher ships beside the agent (not a sidecar: it is not in rfe-sidecars.txt).
(cd "$root/agent" && CGO_ENABLED=0 GOOS=$goos GOARCH=$goarch \
  go build -trimpath -ldflags "-s -w" -o "$stage/rfe-launcher$ext" ./cmd/rfe-launcher)

manifest="$stage/rfe-sidecars.txt"
printf 'rfe-sidecars 1\nversion %s\n' "$version" > "$manifest"
files=("rfe$ext" "rfe-launcher$ext")
for name in rfe-indexd rfe-thumbd; do
  src="$sidecars/$name$ext"
  [[ -f "$src" ]] || { echo "missing sidecar $src" >&2; exit 1; }
  cp "$src" "$stage/$name$ext"
  chmod 755 "$stage/$name$ext"
  printf 'sha256 %s %s\n' "$(sha256sum "$stage/$name$ext" | cut -d' ' -f1)" "$name$ext" >> "$manifest"
  files+=("$name$ext")
done
files+=(rfe-sidecars.txt)

rm -f "$out"
case "$out" in
  *.zip)
    if command -v zip >/dev/null; then
      (cd "$stage" && zip -q "$out" "${files[@]}")
    else
      (cd "$stage" && python3 -m zipfile -c "$out" "${files[@]}")
    fi ;;
  *.tar.gz) tar -C "$stage" -czf "$out" "${files[@]}" ;;
  *) echo "unsupported archive type: $out" >&2; exit 2 ;;
esac
echo "$out"
