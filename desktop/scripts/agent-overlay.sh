#!/usr/bin/env bash
# Stages a linux agent archive (from tools/package-agent.sh: rfe, rfe-indexd, rfe-thumbd, rfe-sidecars.txt) and
# writes a Tauri config overlay that puts the agent into the desktop .deb and .rpm, so one package installs both:
# the four files in /usr/bin (the agent refuses sidecars that do not match the manifest), the systemd user unit,
# and the agent's post-install step (port-443 capability). The package takes over the agent-only package `rfe`
# (and the pre-1.0 `rfe-agent`). The AppImage stays the app alone: it cannot install a service.
#
#   desktop/scripts/agent-overlay.sh <agent-linux-amd64.tar.gz> <stage_dir>
#   (cd desktop/src-tauri && tauri build --config <stage_dir>/overlay.json)
#
# Prints the overlay path. tauri.conf.json stays the plain app; `--config` merges the overlay into it.
set -euo pipefail
if [[ $# -ne 2 ]]; then
  echo "usage: $0 <agent-linux-amd64.tar.gz> <stage_dir>" >&2
  exit 2
fi
archive=$1 stage=$2
root=$(cd "$(dirname "$0")/../.." && pwd)
mkdir -p "$stage"
stage=$(cd "$stage" && pwd)
tar -C "$stage" -xzf "$archive"
for f in rfe rfe-indexd rfe-thumbd rfe-sidecars.txt; do
  [[ -f "$stage/$f" ]] || { echo "$archive is missing $f" >&2; exit 1; }
done

python3 - "$stage" "$root" <<'PY'
import json, sys
stage, root = sys.argv[1], sys.argv[2]
files = {f"/usr/bin/{n}": f"{stage}/{n}" for n in ("rfe", "rfe-indexd", "rfe-thumbd", "rfe-sidecars.txt")}
files["/usr/lib/systemd/user/rfe.service"] = f"{root}/packaging/agent/rfe.service"
post = f"{root}/packaging/agent/postinstall.sh"
overlay = {"bundle": {"linux": {
    "deb": {"files": files, "provides": ["rfe"], "conflicts": ["rfe", "rfe-agent"],
            "replaces": ["rfe", "rfe-agent"], "postInstallScript": post},
    "rpm": {"files": files, "provides": ["rfe"], "obsoletes": ["rfe", "rfe-agent"],
            "postInstallScript": post},
}}}
with open(f"{stage}/overlay.json", "w") as fh:
    json.dump(overlay, fh, indent=2)
PY
echo "$stage/overlay.json"
