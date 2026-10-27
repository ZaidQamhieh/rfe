#!/usr/bin/env python3
"""A small window that drags one file out through the compositor, as a file manager does.
Prints "motion X Y" for the pointer over it (so the test can find where the window is) and
"dropped" when the receiver took the file.  Usage: dragsrc.py FILE"""
import sys, gi
gi.require_version("Gtk", "3.0")
from gi.repository import Gtk, Gdk
path = sys.argv[1]
win = Gtk.Window(title="rfe-e2e-dragsource")
win.set_default_size(300, 200)
box = Gtk.EventBox()
box.add(Gtk.Label(label="drag me"))
box.add_events(Gdk.EventMask.POINTER_MOTION_MASK)
tl = Gtk.TargetList.new([])
tl.add(Gdk.Atom.intern("text/uri-list", False), 0, 0)
box.drag_source_set(Gdk.ModifierType.BUTTON1_MASK, [Gtk.TargetEntry.new("text/uri-list", 0, 0)], Gdk.DragAction.COPY)
box.connect("motion-notify-event", lambda w, e: (print("motion %d %d" % (e.x, e.y), flush=True), False)[1])
box.connect("drag-begin", lambda *a: print("drag-begin", flush=True))
box.connect("drag-data-get", lambda w, ctx, sel, info, t: sel.set_uris(["file://" + path]))
box.connect("drag-end", lambda *a: print("drag-end", flush=True))
box.connect("drag-data-delete", lambda *a: print("dropped", flush=True))
win.add(box)
win.connect("destroy", Gtk.main_quit)
win.show_all()
print("ready", flush=True)
Gtk.main()
