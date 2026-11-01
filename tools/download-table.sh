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

row() { # row <label> <arch: amd64|arm64> [android cell html]
  local a="$1" arch="$2" rpm_arch
  [[ "$arch" == amd64 ]] && rpm_arch=x86_64 || rpm_arch=aarch64
  printf '<tr>\n<td>%s</td>\n' "$a"
  [[ -n "${3:-}" ]] && printf '%s\n' "$3"
  printf '<td align="center">%s</td>\n' "$(agent "windows-$arch\.zip$")"
  printf '<td align="center">%s</td>\n' "$(agent "darwin-$arch\.tar\.gz$")"
  printf '<td align="center">%s</td>\n' "$(join "$(agent "linux-$arch\.deb$")" "$(desktop "_$arch\.deb$")")"
  printf '<td align="center">%s</td>\n' "$(join "$(agent "linux-$arch\.rpm$")" "$(desktop "\.$rpm_arch\.rpm$")")"
  printf '<td align="center">%s</td>\n' "$(join "$(agent "linux-$arch\.tar\.gz$")" "$(desktop "_$arch\.AppImage$")")"
  printf '</tr>\n'
}

apk="$(link "$app_tag" "$app_assets" '\.apk$' 'Universal APK')"
table="$(
  cat <<EOF
<table>
<tr>
<th>Architecture</th><th>Android</th><th>Windows</th><th>macOS</th><th>Debian / Ubuntu</th><th>Fedora / openSUSE</th><th>Other Linux</th>
</tr>
EOF
  row 'x86-64 (64-bit)' amd64 "<td align=\"center\" rowspan=\"3\">$apk</td>"
  row 'AArch64 (ARM64)' arm64
  printf '<tr>\n<td>ARMv7 (32-bit)</td>\n<td></td><td></td><td></td><td></td><td></td>\n</tr>\n</table>\n'
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
