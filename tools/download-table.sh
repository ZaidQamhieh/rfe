#!/usr/bin/env bash
# Regenerates the download table in README.md (between the `downloads:start` and `downloads:end` markers).
# The links point at the fixed names of the rolling "downloads" release (tools/publish-stable-assets.sh, run by the
# release workflows), so they always serve the newest version and the README never needs a per-release edit. Run this
# only when the table layout changes.
#
#   tools/download-table.sh [repo]        # default ZaidQamhieh/rfe
set -euo pipefail

repo="${1:-ZaidQamhieh/rfe}"
readme="$(git -C "$(dirname "$0")/.." rev-parse --show-toplevel)/README.md"
base="https://github.com/$repo/releases/download/downloads"

link() { printf '<a href="%s/%s">%s</a>' "$base" "$1" "$2"; } # link <file> <label>
cell() { printf '<td align="center">%s</td>' "$1"; }
row() { # row <platform> <cell for amd64> <cell for arm64>
  printf '<tr>\n<th align="left">%s</th>\n%s\n%s\n</tr>\n' "$1" "$(cell "$2")" "$(cell "$3")"
}
agent() { link "rfe-agent-$1" Agent; }
both() { printf '%s · %s' "$(agent "$1")" "$(link "$2" Desktop)"; } # both <agent file suffix> <desktop file>

table="$(
  cat <<EOF
<table>
<tr>
<th>Platform</th><th>x86-64 (64-bit)</th><th>AArch64 (ARM64)</th>
</tr>
<tr>
<th align="left">Android</th>
<td align="center" colspan="2">$(link rfe.apk 'Universal APK') (also runs on 32-bit ARMv7 phones)</td>
</tr>
EOF
  row Windows "$(agent windows-amd64.zip)" "$(agent windows-arm64.zip)"
  row macOS "$(agent darwin-amd64.tar.gz)" "$(agent darwin-arm64.tar.gz)"
  row 'Debian / Ubuntu' "$(both linux-amd64.deb RFE-Desktop_amd64.deb)" "$(agent linux-arm64.deb)"
  row 'Fedora / openSUSE' "$(both linux-amd64.rpm RFE-Desktop.x86_64.rpm)" "$(agent linux-arm64.rpm)"
  row 'Other Linux' "$(both linux-amd64.tar.gz RFE-Desktop_amd64.AppImage)" "$(agent linux-arm64.tar.gz)"
  printf '</table>\n'
  printf '\nThese links always serve the newest release. Each versioned release lists its `SHA256SUMS`; older versions are on the [releases page](https://github.com/%s/releases).\n' "$repo"
)"

tmp="$(mktemp)"
TABLE="$table" awk '
  /<!-- downloads:start -->/ { print; print ""; print ENVIRON["TABLE"]; print ""; skip = 1; found++; next }
  /<!-- downloads:end -->/ { skip = 0 }
  !skip
  END { if (found != 1) exit 2 }
' "$readme" >"$tmp" || { rm -f "$tmp"; echo "README.md needs exactly one downloads:start/end marker pair" >&2; exit 1; }
cat "$tmp" >"$readme" && rm -f "$tmp"
echo "README.md: download table now uses the fixed names under $base"
