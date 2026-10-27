#!/usr/bin/env python3
"""Stand-in for a desktop's org.freedesktop.Notifications service, for the live contract test only.

It "clicks" the action named by $RFE_STUB_ACTION (default approve) on every notification that offers it, the way
a person pressing the button would. Run it on a private session bus (dbus-run-session), never on the real one.
"""
import os
import sys

import dbus
import dbus.service
from dbus.mainloop.glib import DBusGMainLoop
from gi.repository import GLib

ACTION = os.environ.get("RFE_STUB_ACTION", "approve")


class Notifications(dbus.service.Object):
    def __init__(self, bus):
        super().__init__(bus, "/org/freedesktop/Notifications")
        self.next_id = 1

    @dbus.service.method("org.freedesktop.Notifications", in_signature="", out_signature="as")
    def GetCapabilities(self):
        return ["actions", "body"]

    @dbus.service.method("org.freedesktop.Notifications", in_signature="susssasa{sv}i", out_signature="u")
    def Notify(self, app, replaces, icon, summary, body, actions, hints, timeout):
        nid = self.next_id
        self.next_id += 1
        names = [str(a) for a in actions[0::2]]
        print(f"notify {nid}: {summary!r} actions={names}", flush=True)
        if ACTION in names:
            GLib.timeout_add(300, lambda: self.ActionInvoked(nid, ACTION) or False)
        return nid

    @dbus.service.method("org.freedesktop.Notifications", in_signature="u", out_signature="")
    def CloseNotification(self, nid):
        self.NotificationClosed(nid, 3)

    @dbus.service.method("org.freedesktop.Notifications", in_signature="", out_signature="ssss")
    def GetServerInformation(self):
        return ("rfe-test-stub", "rfe", "1", "1.2")

    @dbus.service.signal("org.freedesktop.Notifications", signature="uu")
    def NotificationClosed(self, nid, reason):
        pass

    @dbus.service.signal("org.freedesktop.Notifications", signature="us")
    def ActionInvoked(self, nid, action):
        pass


DBusGMainLoop(set_as_default=True)
bus = dbus.SessionBus()
name = dbus.service.BusName("org.freedesktop.Notifications", bus)
Notifications(bus)
print("stub ready", flush=True)
sys.stdout.flush()
GLib.MainLoop().run()
