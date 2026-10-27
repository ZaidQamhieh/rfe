#!/bin/sh
# Runs a command inside a private D-Bus session with a real Secret Service (gnome-keyring), so
# tests can use the real OS keystore on a headless machine or in CI.
#
#   desktop/scripts/with-keyring.sh cargo test --locked --test keystore -- --ignored
#   desktop/scripts/with-keyring.sh --locked cargo test --locked --test keystore_locked -- --ignored
#
# By default the keyring is unlocked and has no password. With --locked it exists, holds a
# password nobody supplies, and stays locked, which is what a session that has not been unlocked
# looks like.
#
# Everything lives in a temporary XDG_DATA_HOME and XDG_RUNTIME_DIR: the keyring files and the
# running keyring daemon of whoever runs this are neither read nor written (a D-Bus session
# started without that can still activate the user's own keyring), and the daemon dies with the
# session. No display is passed on, so the keyring can never open a prompt window on the desktop
# of whoever runs this.
# Needs dbus-run-session, gnome-keyring-daemon and secret-tool (libsecret-tools).
set -eu

locked=0
if [ "${1:-}" = "--locked" ]; then
  locked=1
  shift
fi
if [ "$#" -eq 0 ]; then
  echo "usage: $0 [--locked] <command> [args...]" >&2
  exit 2
fi
for tool in dbus-run-session gnome-keyring-daemon secret-tool; do
  command -v "$tool" >/dev/null 2>&1 || { echo "$0: $tool not found" >&2; exit 127; }
done

XDG_DATA_HOME=$(mktemp -d)
XDG_RUNTIME_DIR=$(mktemp -d)
chmod 700 "$XDG_RUNTIME_DIR"
export XDG_DATA_HOME XDG_RUNTIME_DIR
unset DISPLAY WAYLAND_DISPLAY
trap 'rm -rf "$XDG_DATA_HOME" "$XDG_RUNTIME_DIR"' EXIT INT TERM

if [ "$locked" -eq 1 ]; then
  # Session one creates the default keyring under a password and ends, taking its daemon with it.
  printf pw >"$XDG_RUNTIME_DIR/pw"
  dbus-run-session -- sh -c '
    eval "$(gnome-keyring-daemon --unlock --components=secrets <"$XDG_RUNTIME_DIR/pw")"
    # An item the locked tests can ask for: reading an existing item is what needs the unlock.
    printf x | secret-tool store --label=rfe-locked-test service rfe-desktop-locked-test username probe application rust-keyring
  '
  rm -f "$XDG_RUNTIME_DIR/pw"
  sleep 1
  # Session two starts the daemon without the password: the keyring is there and locked.
  dbus-run-session -- sh -c '
    eval "$(gnome-keyring-daemon --start --components=secrets)"
    export GNOME_KEYRING_CONTROL SSH_AUTH_SOCK
    "$@"
  ' sh "$@"
else
  # A keyring with a blank password is a plain file that gnome-keyring opens unlocked, with no
  # prompt and no password to pass. (The daemon cannot create one without a prompt.)
  mkdir -p "$XDG_DATA_HOME/keyrings"
  printf 'Default_keyring' >"$XDG_DATA_HOME/keyrings/default"
  printf '[keyring]\ndisplay-name=Default keyring\nctime=0\nmtime=0\nlock-on-idle=false\nlock-after=false\n\n' \
    >"$XDG_DATA_HOME/keyrings/Default_keyring.keyring"
  chmod 600 "$XDG_DATA_HOME/keyrings/default" "$XDG_DATA_HOME/keyrings/Default_keyring.keyring"
  dbus-run-session -- sh -c '
    eval "$(gnome-keyring-daemon --start --components=secrets)"
    export GNOME_KEYRING_CONTROL SSH_AUTH_SOCK
    # Touch the keyring once now so the first test does not race the daemon loading it.
    printf x | secret-tool store --label=rfe-prime rfe prime
    secret-tool clear rfe prime
    "$@"
  ' sh "$@"
fi
