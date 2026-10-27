#!/bin/sh
# Installs the RFE agent and/or the desktop app from GitHub releases on any Linux distro, with no root needed
# (setcap is opt-in). Every download is checked against the release's SHA256SUMS before anything is installed.
#
#   curl -fsSL https://raw.githubusercontent.com/ZaidQamhieh/rfe/master/tools/install.sh | sh
#   tools/install.sh [agent|desktop|all] [--agent-version X.Y.Z] [--desktop-version X.Y.Z] [--bin-dir DIR]
#                    [--share-root DIR] [--no-service] [--setcap] [--uninstall]
#
# agent: rfe + rfe-indexd + rfe-thumbd + rfe-sidecars.txt go into the bin dir together (the agent refuses
#        sidecars that do not match the manifest). A first install then runs `rfe setup`: it shares one
#        folder (default ~/RFE Files), starts the systemd user service and prints a pairing QR. An upgrade runs
#        `rfe install`, which restarts the service.
# desktop: the AppImage goes into the bin dir, with a launcher entry and icon under ~/.local/share so it shows up
#        in the app menu and search.
set -eu

REPO=${RFE_REPO:-ZaidQamhieh/rfe}
what=all agent_version="" desktop_version="" bin_dir=${XDG_BIN_HOME:-$HOME/.local/bin}
service=1 setcap=0 uninstall=0 share_root=""
data_dir=${XDG_DATA_HOME:-$HOME/.local/share}

die() { echo "install.sh: $*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "$1 is required"; }

while [ $# -gt 0 ]; do
  case $1 in
    agent|desktop|all) what=$1 ;;
    --agent-version) agent_version=${2:?}; shift ;;
    --desktop-version) desktop_version=${2:?}; shift ;;
    --bin-dir) bin_dir=${2:?}; shift ;;
    --share-root) share_root=${2:?}; shift ;;
    --no-service) service=0 ;;
    --setcap) setcap=1 ;;
    --uninstall) uninstall=1 ;;
    -h|--help) sed -n '2,15p' "$0"; exit 0 ;;
    *) die "unknown argument: $1" ;;
  esac
  shift
done

[ "$(uname -s)" = Linux ] || die "Linux only"
case $(uname -m) in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) die "unsupported CPU: $(uname -m)" ;;
esac

apps_dir=$data_dir/applications
icon_dir=$data_dir/icons/hicolor/128x128/apps
appimage=$bin_dir/rfe-desktop.AppImage
desktop_file=$apps_dir/app.rfe.desktop.desktop

refresh_menu() {
  command -v update-desktop-database >/dev/null 2>&1 && update-desktop-database "$apps_dir" 2>/dev/null || true
  command -v gtk-update-icon-cache >/dev/null 2>&1 && gtk-update-icon-cache -q -t "$data_dir/icons/hicolor" 2>/dev/null || true
}

if [ "$uninstall" = 1 ]; then
  if [ "$what" != desktop ]; then
    # The unit names one binary; only remove it when that is the binary being removed.
    unit=${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user/rfe.service
    if [ -x "$bin_dir/rfe" ] && [ -f "$unit" ] && grep -qF "$bin_dir/rfe" "$unit"; then
      "$bin_dir/rfe" uninstall || true
    fi
    rm -f "$bin_dir/rfe" "$bin_dir/rfe.bak" "$bin_dir/rfe-indexd" "$bin_dir/rfe-thumbd" "$bin_dir/rfe-sidecars.txt"
  fi
  if [ "$what" != agent ]; then
    rm -f "$appimage" "$desktop_file" "$icon_dir/app.rfe.desktop.png"
    refresh_menu
  fi
  echo "removed (agent data and settings were left alone)"
  exit 0
fi

need curl
need tar
need sha256sum
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# Newest stable (no "-rc") release whose tag starts with the prefix.
latest_version() {
  curl -fsSL "https://api.github.com/repos/$REPO/releases?per_page=50" |
    sed -n 's/.*"tag_name": *"'"$1"'v\([0-9][0-9.]*\)".*/\1/p' | head -n 1
}

# Downloads <tag> asset <file> plus SHA256SUMS into $tmp and verifies the file.
fetch() {
  tag=$1 file=$2
  base=https://github.com/$REPO/releases/download/$tag
  curl -fsSL -o "$tmp/SHA256SUMS" "$base/SHA256SUMS" || die "cannot download $tag SHA256SUMS"
  echo "downloading $file"
  if [ -t 2 ]; then bar=--progress-bar; else bar=-sS; fi
  curl -fL $bar -o "$tmp/$file" "$base/$file" || die "cannot download $file"
  want=$(awk -v n="$file" '{ h=$1; $1=""; sub(/^ [ *]?/, ""); if ($0 == n) { print h; exit } }' "$tmp/SHA256SUMS")
  [ -n "$want" ] || die "$file is not listed in SHA256SUMS"
  got=$(sha256sum "$tmp/$file" | cut -d' ' -f1)
  [ "$got" = "$want" ] || die "$file checksum mismatch (expected $want, got $got)"
}

mkdir -p "$bin_dir"

install_agent() {
  v=${agent_version:-$(latest_version agent-)}
  [ -n "$v" ] || die "no agent release found"
  fetch "agent-v$v" "agent-v$v-linux-$arch.tar.gz"
  mkdir -p "$tmp/agent"
  tar -C "$tmp/agent" -xzf "$tmp/agent-v$v-linux-$arch.tar.gz"
  for f in rfe rfe-indexd rfe-thumbd rfe-sidecars.txt; do
    [ -f "$tmp/agent/$f" ] || die "archive is missing $f"
  done
  # Stage every file beside its target, then rename, so an interrupted run never leaves a mixed set.
  [ -f "$bin_dir/rfe" ] && cp -f "$bin_dir/rfe" "$bin_dir/rfe.bak"
  for f in rfe rfe-indexd rfe-thumbd rfe-sidecars.txt; do
    install -m 755 "$tmp/agent/$f" "$bin_dir/.$f.new"
  done
  for f in rfe rfe-indexd rfe-thumbd rfe-sidecars.txt; do
    mv -f "$bin_dir/.$f.new" "$bin_dir/$f"
  done
  echo "agent $v installed to $bin_dir"
  if [ "$setcap" = 1 ]; then
    need setcap
    sudo setcap cap_net_bind_service=+ep "$bin_dir/rfe" || echo "setcap failed; the agent keeps its high port"
  fi
  first_run=0
  # A pre-1.0 install keeps its data in ~/.rfe-agent; `rfe install` moves it and retires the old service.
  [ -z "${RFE_DATA_DIR:-}" ] && [ ! -f "$HOME/.rfe/agent.db" ] && [ ! -f "$HOME/.rfe-agent/agent.db" ] && first_run=1
  if [ "$first_run" = 1 ]; then
    # First install: share one folder (conservative default), start the login service and print a pairing QR.
    flags=""
    [ "$service" = 1 ] || flags=-no-install
    if [ "$service" = 1 ] && ! { command -v systemctl >/dev/null 2>&1 && systemctl --user show-environment >/dev/null 2>&1; }; then
      echo "systemd --user is not available, so the service is not installed"
      flags=-no-install
    fi
    # shellcheck disable=SC2086
    "$bin_dir/rfe" setup -root "${share_root:-$HOME/RFE Files}" $flags
    [ -z "$flags" ] || echo "start the agent yourself with: $bin_dir/rfe    then pair a phone with: $bin_dir/rfe pair"
  elif [ "$service" = 1 ]; then
    if command -v systemctl >/dev/null 2>&1 && systemctl --user show-environment >/dev/null 2>&1; then
      "$bin_dir/rfe" install
    else
      echo "systemd --user is not available; start the agent yourself with: $bin_dir/rfe"
    fi
  fi
  # The pre-1.0 binary's name; its service is gone once `rfe install` ran.
  rm -f "$bin_dir/rfe-agent" "$bin_dir/rfe-agent.bak"
  "$bin_dir/rfe" status 2>/dev/null | sed -n '/sidecars/p' || true
}

install_desktop() {
  [ "$arch" = amd64 ] || die "the desktop app is only built for x86_64"
  v=${desktop_version:-$(latest_version desktop-)}
  [ -n "$v" ] || die "no desktop release found"
  file=RFE-Desktop_${v}_amd64.AppImage
  fetch "desktop-v$v" "$file"
  install -m 755 "$tmp/$file" "$bin_dir/.rfe-desktop.new"
  mv -f "$bin_dir/.rfe-desktop.new" "$appimage"
  mkdir -p "$apps_dir" "$icon_dir"
  curl -fsSL -o "$icon_dir/app.rfe.desktop.png" \
    "https://raw.githubusercontent.com/$REPO/desktop-v$v/desktop/src-tauri/icons/128x128.png" ||
    echo "icon download failed; the launcher entry will have no icon"
  cat > "$desktop_file" <<EOF
[Desktop Entry]
Type=Application
Name=RFE Desktop
GenericName=Remote PC control
Comment=Pin an RFE agent and manage its paired devices
Keywords=rfe;agent;remote;pc;devices;pairing;fingerprint;
Exec=$appimage
Icon=app.rfe.desktop
Terminal=false
Categories=Utility;
StartupNotify=true
EOF
  refresh_menu
  echo "desktop $v installed: search for \"RFE Desktop\" in your app menu"
  command -v gnome-keyring-daemon >/dev/null 2>&1 || command -v keepassxc >/dev/null 2>&1 ||
    command -v kwalletd6 >/dev/null 2>&1 || command -v kwalletd5 >/dev/null 2>&1 ||
    echo "note: no Secret Service keyring found (gnome-keyring, KWallet or KeePassXC); the app cannot store secrets without one"
}

case $what in
  agent) install_agent ;;
  desktop) install_desktop ;;
  all) install_agent; install_desktop ;;
esac
case ":$PATH:" in *":$bin_dir:"*) ;; *) echo "note: $bin_dir is not on your PATH" ;; esac
