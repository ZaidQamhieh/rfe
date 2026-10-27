#!/usr/bin/env bash
# Builds the agent and both Rust sidecars from this checkout and installs them together (with the manifest the agent
# verifies) into a directory, default ~/.local/bin. It does not restart the service or touch capabilities.
#
#   tools/install-agent-local.sh [version] [dest_dir]
#
# Afterwards (Linux): sudo setcap cap_net_bind_service=+ep <dest>/rfe; systemctl --user restart rfe;
# rfe status shows `sidecars: ... verified`.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
version=${1:-0.0.0-local.$(git -C "$root" rev-parse --short HEAD)}
dest=${2:-$HOME/.local/bin}
goos=$(go env GOOS) goarch=$(go env GOARCH)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

(cd "$root/agent-rs" && RFE_RELEASE_VERSION="$version" cargo build --release --locked -p rfe-indexd -p rfe-thumbd)
"$root/tools/package-agent.sh" "$goos" "$goarch" "$version" "$root/agent-rs/target/release" "$tmp/pkg.tar.gz" >/dev/null
mkdir -p "$dest" "$tmp/x"
tar -C "$tmp/x" -xzf "$tmp/pkg.tar.gz"
# Stage the whole set beside the targets first, then rename each into place, so an interrupted run leaves no
# half-written file; the old rfe is kept as rfe.bak.
if [[ -f "$dest/rfe" ]]; then
  cp -f "$dest/rfe" "$dest/.rfe.bak.new"
  mv -f "$dest/.rfe.bak.new" "$dest/rfe.bak"
fi
for f in "$tmp"/x/*; do
  install -m 755 "$f" "$dest/.$(basename "$f").new"
done
for f in "$tmp"/x/*; do
  mv -f "$dest/.$(basename "$f").new" "$dest/$(basename "$f")"
done
echo "installed $version to $dest:"
ls -l "$dest"/rfe "$dest"/rfe-indexd "$dest"/rfe-thumbd "$dest"/rfe-sidecars.txt
