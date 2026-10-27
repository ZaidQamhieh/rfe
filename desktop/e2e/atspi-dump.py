#!/usr/bin/env python3
"""Prints the AT-SPI tree of the running app as `depth role | name`, the way a screen reader sees it.

Used by e2e/a11y.test.mjs inside the private session (it needs that session's accessibility bus).
Usage: atspi-dump.py [application name substring]
"""
import sys

import gi

gi.require_version("Atspi", "2.0")
from gi.repository import Atspi  # noqa: E402

want = (sys.argv[1] if len(sys.argv) > 1 else "rfe").lower()


def text_of(node):
    """Static text has no accessible name; a screen reader reads its text."""
    try:
        iface = node.get_text_iface()
        if iface is not None:
            return iface.get_text(0, iface.get_character_count()).replace("\n", " ").strip()
    except Exception:
        pass
    return ""


def walk(node, depth, out):
    try:
        role = node.get_role_name()
    except Exception:
        return
    try:
        name = node.get_name() or ""
    except Exception:
        name = ""
    if not name:
        name = text_of(node)
    try:
        showing = node.get_state_set().contains(Atspi.StateType.SHOWING)
    except Exception:
        showing = False
    out.append((depth, role, name, showing))
    try:
        count = node.get_child_count()
    except Exception:
        return
    for i in range(count):
        try:
            child = node.get_child_at_index(i)
        except Exception:
            continue
        if child is not None:
            walk(child, depth + 1, out)


desktop = Atspi.get_desktop(0)
found = False
for i in range(desktop.get_child_count()):
    app = desktop.get_child_at_index(i)
    if app is None or want not in (app.get_name() or "").lower():
        continue
    found = True
    rows = []
    walk(app, 0, rows)
    for depth, role, name, showing in rows:
        print(f"{depth}\t{role}\t{int(showing)}\t{name}")
if not found:
    print("no application matching " + want, file=sys.stderr)
    sys.exit(3)
