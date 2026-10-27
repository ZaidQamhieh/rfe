#!/bin/sh
# End-to-end tests: the real app in a real WebKit window, driven through WebDriver (tauri-driver).
#
#   desktop/e2e/run.sh                 # cargo build first: the debug binary is used
#   SCALE=2 desktop/e2e/run.sh         # the same at 200% display scaling
#   SHOTS=1 desktop/e2e/run.sh         # also refresh the screenshots in desktop/docs
#
# It runs in a private desktop of its own: a private D-Bus session, an unlocked keyring with no
# password, and KWin's virtual compositor, with temporary data, config and runtime directories.
# No window appears on the display of whoever runs it, and nothing of theirs (keyring, settings,
# running compositor) is read or written. The environment variables that point at their display are
# removed.
#
# Safety: it runs as three stages chosen by RFE_E2E_STAGE (never by arguments: KWin starts its
# command with none). Each stage can only start the next one, so it cannot start itself. The launcher
# puts everything in a systemd scope capped at 4 GiB of memory, no swap, 300 tasks and 5 minutes, and
# stops the scope on exit so no compositor, bus or driver outlives the run.
#
# Needs: systemd-run (user manager), dbus-run-session, kwin_wayland, gnome-keyring-daemon, secret-tool, WebKitWebDriver,
# tauri-driver (cargo install tauri-driver --locked), node, a built agent (RFE_AGENT_BIN) and the
# app (RFE_DESKTOP_BIN, default desktop/src-tauri/target/debug/rfe-desktop).
set -eu

here=$(cd "$(dirname "$0")" && pwd)
stage=${RFE_E2E_STAGE:-launcher}

case "$stage" in
inside)
  # Running inside the virtual desktop, started by the session stage below.
  # Through a file: the daemon keeps the pipe of a command substitution open and would hang it.
  # RFE_E2E_NO_KEYRING=1 leaves the session with no Secret Service at all, for e2e/no-keystore.test.mjs
  # (run it alone with RFE_E2E_ONLY=no-keystore); the other tests need the keyring.
  if [ "${RFE_E2E_NO_KEYRING:-}" != "1" ]; then
    gnome-keyring-daemon --start --components=secrets </dev/null >"$XDG_RUNTIME_DIR/keyring.env" 2>"$XDG_RUNTIME_DIR/keyring.err"
    eval "$(cat "$XDG_RUNTIME_DIR/keyring.env")"
    export GNOME_KEYRING_CONTROL SSH_AUTH_SOCK
  fi
  export GDK_BACKEND=wayland WAYLAND_DISPLAY=rfe-e2e WEBKIT_DISABLE_DMABUF_RENDERER=1
  # The accessibility bus, so the app's AT-SPI tree can be read as a screen reader reads it
  # (e2e/a11y.test.mjs); without it the app logs an "atk-bridge" warning and nothing else changes.
  if [ -x /usr/lib/at-spi-bus-launcher ]; then
    /usr/lib/at-spi-bus-launcher --launch-immediately >"$XDG_RUNTIME_DIR/atspi.log" 2>&1 &
    sleep 0.5
    [ -x /usr/lib/at-spi2-registryd ] && /usr/lib/at-spi2-registryd --use-gnome-session >>"$XDG_RUNTIME_DIR/atspi.log" 2>&1 &
    export NO_AT_BRIDGE=0 GTK_A11Y=atspi
    sleep 0.5
  fi
  tauri-driver --port 4444 >"$XDG_RUNTIME_DIR/driver.log" 2>&1 &
  driver=$!
  waited=0
  while ! (exec 3<>/dev/tcp/127.0.0.1/4444) 2>/dev/null; do
    waited=$((waited + 1))
    if [ "$waited" -gt 100 ]; then echo "tauri-driver did not start" >&2; break; fi
    sleep 0.1
  done
  status=0
  node --test --test-concurrency=1 "$here"/${RFE_E2E_ONLY:-*}.test.mjs || status=$?
  kill "$driver" 2>/dev/null || true
  echo "$status" >"$XDG_RUNTIME_DIR/e2e-status"
  exit 0
  ;;

session)
  for tool in dbus-run-session kwin_wayland gnome-keyring-daemon secret-tool WebKitWebDriver tauri-driver node; do
    command -v "$tool" >/dev/null 2>&1 || { echo "$0: $tool not found" >&2; exit 127; }
  done
  : "${RFE_AGENT_BIN:?set RFE_AGENT_BIN to a built rfe}"
  RFE_DESKTOP_BIN=${RFE_DESKTOP_BIN:-$here/../src-tauri/target/debug/rfe-desktop}
  [ -x "$RFE_DESKTOP_BIN" ] || { echo "$0: $RFE_DESKTOP_BIN not built (cargo build in desktop/src-tauri)" >&2; exit 1; }
  RFE_DESKTOP_BIN=$(cd "$(dirname "$RFE_DESKTOP_BIN")" && pwd)/$(basename "$RFE_DESKTOP_BIN")
  RFE_AGENT_BIN=$(cd "$(dirname "$RFE_AGENT_BIN")" && pwd)/$(basename "$RFE_AGENT_BIN")
  export RFE_DESKTOP_BIN RFE_AGENT_BIN

  base=$E2E_BASE
  XDG_RUNTIME_DIR=$base/run
  XDG_DATA_HOME=$base/data
  XDG_CONFIG_HOME=$base/config
  XDG_CACHE_HOME=$base/cache
  HOME=$base/home
  mkdir -p "$XDG_RUNTIME_DIR" "$XDG_DATA_HOME/keyrings" "$XDG_CONFIG_HOME" "$XDG_CACHE_HOME" "$HOME"
  chmod 700 "$XDG_RUNTIME_DIR"
  export XDG_RUNTIME_DIR XDG_DATA_HOME XDG_CONFIG_HOME XDG_CACHE_HOME HOME
  unset DISPLAY WAYLAND_DISPLAY QT_WAYLAND_RECONNECT

  # A keyring with a blank password is a plain file gnome-keyring opens unlocked (see with-keyring.sh).
  printf 'Default_keyring' >"$XDG_DATA_HOME/keyrings/default"
  printf '[keyring]\ndisplay-name=Default keyring\nctime=0\nmtime=0\nlock-on-idle=false\nlock-after=false\n\n' \
    >"$XDG_DATA_HOME/keyrings/Default_keyring.keyring"
  chmod 600 "$XDG_DATA_HOME/keyrings/default" "$XDG_DATA_HOME/keyrings/Default_keyring.keyring"

  E2E_APP_DATA=$XDG_DATA_HOME/app.rfe.desktop
  export E2E_APP_DATA
  if [ "${SHOTS:-}" = "1" ]; then E2E_SHOTS=$here/../docs; export E2E_SHOTS; fi

  # KWin runs its command with no arguments, so the next stage is passed in the environment. It stays
  # in the background; this stage leaves as soon as the tests wrote their status (or KWin died, or
  # five minutes passed) and the launcher then stops the whole scope.
  # With no keyring the session bus must not be able to start one on demand either: this machine's
  # gnome-keyring is dbus-activatable, and an activated one waits for a password prompt no one can see.
  # A session config whose only service folder is empty leaves "no Secret Service" truly empty.
  bus_config=()
  if [ "${RFE_E2E_NO_KEYRING:-}" = "1" ]; then
    mkdir -p "$XDG_RUNTIME_DIR/no-services"
    sed "s|<standard_session_servicedirs */>|<servicedir>$XDG_RUNTIME_DIR/no-services</servicedir>|" /usr/share/dbus-1/session.conf >"$XDG_RUNTIME_DIR/session-no-services.conf"
    bus_config=(--config-file="$XDG_RUNTIME_DIR/session-no-services.conf")
  fi
  # e2e/os-drop.test.mjs moves the pointer through KWin's fake-input protocol, which KWin only offers to authorised programs.
  export KWIN_WAYLAND_NO_PERMISSION_CHECKS=1
  RFE_E2E_STAGE=inside dbus-run-session "${bus_config[@]}" -- kwin_wayland --virtual --socket rfe-e2e --width 1280 --height 800 \
    --scale "${SCALE:-1}" --no-lockscreen --no-global-shortcuts -- "$here/run.sh" >"$XDG_RUNTIME_DIR/tests.log" 2>&1 &
  kwin=$!
  waited=0
  while [ ! -f "$XDG_RUNTIME_DIR/e2e-status" ] && kill -0 "$kwin" 2>/dev/null && [ "$waited" -lt 2800 ]; do
    waited=$((waited + 1))
    sleep 0.1
  done
  cat "$XDG_RUNTIME_DIR/tests.log" 2>/dev/null || true
  status=$(cat "$XDG_RUNTIME_DIR/e2e-status" 2>/dev/null || echo 1)
  if [ "$status" != "0" ]; then
    echo "--- tauri-driver log ---" >&2
    tail -20 "$XDG_RUNTIME_DIR/driver.log" >&2 || true
  fi
  exit "$status"
  ;;

launcher) ;;
*) echo "$0: unknown RFE_E2E_STAGE '$stage'" >&2; exit 2 ;;
esac

# Launcher: refuse to start beside a leftover run, then run the session stage in a capped scope.
command -v systemd-run >/dev/null 2>&1 || { echo "$0: systemd-run is required (memory cap)" >&2; exit 127; }
if pgrep -xa kwin_wayland 2>/dev/null | grep -q -- '--socket rfe-e2e'; then
  echo "$0: a previous e2e compositor is still running; stop it first" >&2
  exit 1
fi
unit=rfe-e2e-$$
E2E_BASE=$(mktemp -d)
export E2E_BASE
# Stop the scope (kills every leftover process), then remove the temporary desktop. The document portal
# may have mounted a FUSE view inside it; unmount that first.
trap 'systemctl --user stop "$unit.scope" >/dev/null 2>&1 || true
  fusermount3 -uz "$E2E_BASE/run/doc" >/dev/null 2>&1 || fusermount -uz "$E2E_BASE/run/doc" >/dev/null 2>&1 || true
  rm -rf "$E2E_BASE"' EXIT INT TERM
RFE_E2E_STAGE=session systemd-run --user --scope --quiet --unit="$unit" \
  -p MemoryMax="${E2E_MEMORY_MAX:-4G}" -p MemorySwapMax=0 -p TasksMax=300 -p RuntimeMaxSec=300 \
  "$here/run.sh"
