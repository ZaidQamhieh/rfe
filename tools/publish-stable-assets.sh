#!/usr/bin/env bash
# Copies a finished full release's download files to the rolling release "downloads" under fixed names (no version in
# the name), so the README links https://github.com/<repo>/releases/download/downloads/<name> always serve the newest.
# The rolling release is never Latest, so the app updater (releases/latest) is unaffected. Run by release.yml and
# host-release.yml after they publish a full release; also safe to run by hand to backfill.
#
#   tools/publish-stable-assets.sh <tag>            # GH_REPO (or -R) picks the repo; default ZaidQamhieh/rfe
#
# Names: rfe.apk, rfe-agent-<os>-<arch>.<ext>, RFE-Desktop_amd64.<ext>, RFE-Desktop.x86_64.rpm. Everything else
# (SHA256SUMS, sbom) stays only on the versioned release.
set -euo pipefail
tag="${1:?usage: publish-stable-assets.sh <tag>}"
repo="${GH_REPO:-ZaidQamhieh/rfe}"
[[ "$tag" == *-rc* ]] && { echo "refusing a pre-release ($tag)" >&2; exit 1; }

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
mkdir "$work/in" "$work/out"
gh release download "$tag" -R "$repo" -D "$work/in" \
  -p '*.apk' -p 'agent-v*' -p 'host-v*' -p 'RFE-Desktop*'

stable() { # stable <asset name>: the fixed name, or nothing for a file that is not a download
  case "$1" in
    rfe-[0-9]*.apk) echo rfe.apk ;;
    agent-v[0-9]*) sed -E 's/^agent-v[0-9.]+-/rfe-agent-/' <<<"$1" ;;
    host-v[0-9]*) sed -E 's/^host-v[0-9.]+-/rfe-agent-/' <<<"$1" ;;
    RFE-Desktop_[0-9]*) sed -E 's/^RFE-Desktop_[0-9.]+_/RFE-Desktop_/' <<<"$1" ;;
    RFE-Desktop-[0-9]*) sed -E 's/^RFE-Desktop-[0-9.]+-[0-9]+\./RFE-Desktop./' <<<"$1" ;;
  esac
}
count=0
for f in "$work"/in/*; do
  [[ -f "$f" ]] || continue
  name="$(stable "$(basename "$f")")"
  [[ -n "$name" ]] || continue
  cp "$f" "$work/out/$name"
  count=$((count + 1))
done
[[ $count -gt 0 ]] || { echo "no download files found in $tag" >&2; exit 1; }

gh release view downloads -R "$repo" >/dev/null 2>&1 || gh release create downloads -R "$repo" \
  --title "Latest downloads" --latest=false \
  --notes "Fixed-name copies of the newest release files, replaced on every full release. Each versioned release lists its SHA256SUMS."
gh release upload downloads -R "$repo" --clobber "$work"/out/*
echo "published $count files from $tag to https://github.com/$repo/releases/tag/downloads"
