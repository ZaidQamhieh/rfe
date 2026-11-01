#!/usr/bin/env bash
# Regenerates the download table in README.md (between the `downloads:start` and `downloads:end` markers) from the
# newest full releases of the public repo: app `v*`, agent `agent-v*`, desktop `desktop-v*`. Release asset names carry
# the version, so run this after each release and commit the README; the next promotion carries it to the public repo.
#
#   tools/download-table.sh [repo]        # default ZaidQamhieh/rfe
set -euo pipefail

repo="${1:-ZaidQamhieh/rfe}"
readme="$(git -C "$(dirname "$0")/.." rev-parse --show-toplevel)/README.md"

releases="$(gh release list -R "$repo" --limit 100 --json tagName,isPrerelease,isDraft,publishedAt)"
newest() { # newest non-draft, non-prerelease tag matching the regex
  jq -r --arg re "$1" '[.[] | select((.isDraft or .isPrerelease | not) and (.tagName | test($re)))]
    | sort_by(.publishedAt) | last | .tagName // empty' <<<"$releases"
}
app_tag="$(newest '^v[0-9]')"
agent_tag="$(newest '^agent-v[0-9]')"
desktop_tag="$(newest '^desktop-v[0-9]')"
for t in app_tag agent_tag desktop_tag; do
  [[ -n "${!t}" ]] || { echo "no full release found for ${t%_tag} in $repo" >&2; exit 1; }
done

assets() { gh release view "$1" -R "$repo" --json assets -q '.assets[].name'; }
app_assets="$(assets "$app_tag")"
agent_assets="$(assets "$agent_tag")"
desktop_assets="$(assets "$desktop_tag")"

# link <tag> <asset list> <regex> <label>: an HTML link to the first matching asset, or nothing
link() {
  local name
  name="$(rg -m1 -e "$3" <<<"$2" || true)"
  [[ -n "$name" ]] && printf '<a href="https://github.com/%s/releases/download/%s/%s">%s</a>' "$repo" "$1" "$name" "$4"
  return 0
}
join() { # joins the non-empty arguments with " · "
  local out="" part
  for part in "$@"; do [[ -n "$part" ]] && out="${out:+$out · }$part"; done
  printf '%s' "$out"
}
agent() { link "$agent_tag" "$agent_assets" "$1" "${2:-Agent}"; }
desktop() { link "$desktop_tag" "$desktop_assets" "$1" "${2:-Desktop}"; }

cell() { printf '<td align="center">%s</td>' "$1"; }
row() { # row <platform> <cell for amd64> <cell for arm64>
  printf '<tr>\n<th align="left">%s</th>\n%s\n%s\n</tr>\n' "$1" "$(cell "$2")" "$(cell "$3")"
}
linux() { # linux <arch: amd64|arm64> <agent ext> <desktop regex for amd64>
  local d=""
  [[ "$1" == amd64 ]] && d="$(desktop "$3")"
  join "$(agent "linux-$1\.$2$")" "$d"
}

apk="$(link "$app_tag" "$app_assets" '\.apk$' 'Universal APK') (also runs on 32-bit ARMv7 phones)"
table="$(
  cat <<EOF
<table>
<tr>
<th>Platform</th><th>x86-64 (64-bit)</th><th>AArch64 (ARM64)</th>
</tr>
<tr>
<th align="left">Android</th>
<td align="center" colspan="2">$apk</td>
</tr>
EOF
  row Windows "$(agent 'windows-amd64\.zip$')" "$(agent 'windows-arm64\.zip$')"
  row macOS "$(agent 'darwin-amd64\.tar\.gz$')" "$(agent 'darwin-arm64\.tar\.gz$')"
  row 'Debian / Ubuntu' "$(linux amd64 deb '_amd64\.deb$')" "$(linux arm64 deb)"
  row 'Fedora / openSUSE' "$(linux amd64 rpm '\.x86_64\.rpm$')" "$(linux arm64 rpm)"
  row 'Other Linux' "$(linux amd64 'tar\.gz' '_amd64\.AppImage$')" "$(linux arm64 'tar\.gz')"
  printf '</table>\n'
  printf '\nApp %s · Agent %s · Desktop %s. ' "${app_tag#v}" "${agent_tag#agent-v}" "${desktop_tag#desktop-v}"
  printf 'Each release lists its `SHA256SUMS`; older versions are on the [releases page](https://github.com/%s/releases).\n' "$repo"
)"

tmp="$(mktemp)"
TABLE="$table" awk '
  /<!-- downloads:start -->/ { print; print ""; print ENVIRON["TABLE"]; print ""; skip = 1; found++; next }
  /<!-- downloads:end -->/ { skip = 0 }
  !skip
  END { if (found != 1) exit 2 }
' "$readme" >"$tmp" || { rm -f "$tmp"; echo "README.md needs exactly one downloads:start/end marker pair" >&2; exit 1; }
cat "$tmp" >"$readme" && rm -f "$tmp"
echo "README.md: app $app_tag, agent $agent_tag, desktop $desktop_tag"
