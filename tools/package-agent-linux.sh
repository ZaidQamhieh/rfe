#!/usr/bin/env bash
# Turns a linux agent archive (from tools/package-agent.sh) into a .deb and an .rpm with nfpm. The agent, both
# sidecars and rfe-sidecars.txt go into /usr/bin together (the agent refuses sidecars that do not match the
# manifest), with a systemd user unit and a post-install step that sets the port-443 capability.
#
#   tools/package-agent-linux.sh <version> <amd64|arm64> <archive.tar.gz> <out_dir>
#
# Writes <out_dir>/agent-v<version>-linux-<arch>.deb and .rpm. Needs nfpm on PATH.
set -euo pipefail
if [[ $# -ne 4 ]]; then
  echo "usage: $0 <version> <amd64|arm64> <archive.tar.gz> <out_dir>" >&2
  exit 2
fi
version=$1 arch=$2 archive=$3 out=$4
case "$arch" in amd64|arm64) ;; *) echo "unsupported arch: $arch" >&2; exit 2 ;; esac
root=$(cd "$(dirname "$0")/.." && pwd)
case "$archive" in /*) ;; *) archive="$PWD/$archive" ;; esac
mkdir -p "$out"
out=$(cd "$out" && pwd)
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT
tar -C "$stage" -xzf "$archive"
for f in rfe rfe-indexd rfe-thumbd rfe-sidecars.txt; do
  [[ -f "$stage/$f" ]] || { echo "archive is missing $f" >&2; exit 1; }
done

cat > "$stage/nfpm.yaml" <<YAML
name: rfe
arch: $arch
platform: linux
version: $version
version_schema: semver
maintainer: RFE <noreply@users.noreply.github.com>
description: RFE host agent with its Rust sidecars
homepage: https://github.com/ZaidQamhieh/rfe
license: GPL-3.0-only
# Before 1.0.0 the package was called rfe-agent.
replaces: [rfe-agent]
conflicts: [rfe-agent]
contents:
  - {src: $stage/rfe, dst: /usr/bin/rfe, file_info: {mode: 0755}}
  - {src: $stage/rfe-indexd, dst: /usr/bin/rfe-indexd, file_info: {mode: 0755}}
  - {src: $stage/rfe-thumbd, dst: /usr/bin/rfe-thumbd, file_info: {mode: 0755}}
  - {src: $stage/rfe-sidecars.txt, dst: /usr/bin/rfe-sidecars.txt, file_info: {mode: 0644}}
  - {src: $root/packaging/agent/rfe.service, dst: /usr/lib/systemd/user/rfe.service, file_info: {mode: 0644}}
scripts:
  postinstall: $root/packaging/agent/postinstall.sh
YAML

for fmt in deb rpm; do
  nfpm package --config "$stage/nfpm.yaml" --packager "$fmt" --target "$stage/pkg.$fmt" >/dev/null
  mv "$stage/pkg.$fmt" "$out/agent-v$version-linux-$arch.$fmt"
  echo "$out/agent-v$version-linux-$arch.$fmt"
done
