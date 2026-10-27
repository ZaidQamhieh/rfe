#!/bin/sh
# Runs after install and after every upgrade (an upgrade replaces the file, which drops the capability).
# The capability lets the agent also listen on port 443; without it the agent keeps its high port.
if command -v setcap >/dev/null 2>&1; then
  setcap cap_net_bind_service=+ep /usr/bin/rfe 2>/dev/null || true
fi
echo 'Enable the agent for your user:  systemctl --user enable --now rfe'
echo 'Then pair a phone:               rfe pair'
exit 0
