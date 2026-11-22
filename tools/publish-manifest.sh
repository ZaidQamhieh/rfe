#!/usr/bin/env bash
# Signs and publishes the host-update manifest for a finished host release (host-vX.Y.Z) on the public repo.
# Run it on the owner's machine after the release is published: the private key never reaches CI.
#   tools/publish-manifest.sh 1.2.0 [owner/repo]
# The agents read RFE_UPDATE_MANIFEST_URL from tools/update-keys.env: the fixed asset of the rolling release
# "host-manifest" (never Latest, so the app updater is unaffected).
set -euo pipefail
version="${1:?usage: publish-manifest.sh <version> [owner/repo]}"
repo="${2:-ZaidQamhieh/rfe}"
key="${RFE_UPDATE_SIGNING_KEY:-$HOME/.rfe-keystore/update-signing/primary.key}"
[[ "$version" == *-* ]] && { echo "refusing a pre-release ($version): agents must not be offered it" >&2; exit 1; }
[[ -f "$key" ]] || { echo "signing key not found: $key" >&2; exit 1; }
root=$(cd "$(dirname "$0")/.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

gh release download "host-v$version" -R "$repo" -p 'agent-v*' -D "$work/bundles"
(cd "$root/agent" && go build -o "$work/rfe-sign" ./cmd/rfe-sign)

args=()
for f in "$work"/bundles/agent-v"$version"-*; do
  base=$(basename "$f"); stem=${base#agent-v"$version"-}; stem=${stem%%.*}   # <os>-<arch>
  args+=("${stem%%-*}/${stem#*-}=$f")
done
[[ ${#args[@]} -eq 6 ]] || { echo "expected 6 agent bundles, found ${#args[@]}" >&2; exit 1; }

"$work/rfe-sign" -key "$key" -version "$version" \
  -base-url "https://github.com/$repo/releases/download/host-v$version" -out "$work/out" "${args[@]}"

gh release view host-manifest -R "$repo" >/dev/null 2>&1 || gh release create host-manifest -R "$repo" \
  --title "Host update manifest" --notes "Signed manifest read by rfe update check. Replaced on every host release." --latest=false
gh release upload host-manifest -R "$repo" --clobber "$work/out/manifest.json" "$work/out/manifest.json.sig"
echo "published https://github.com/$repo/releases/download/host-manifest/manifest.json"
