# Upgrading from the Flutter app to the React Native app

The React Native app (`mobile/`) uses the same package name (`com.zqamhieh.remote_file_explorer`) and
the same release signing key as the Flutter app, so Android installs it over the Flutter app as an
ordinary update. The agent and the API contract (`protocol/openapi.yaml`) are unchanged.

## Upgrade

Install the new APK over the old app (Settings → About → Update, or `adb install -r`). Nothing has
to be uninstalled and nothing has to be paired again.

What carries over, read in place from the old app's storage:

- Paired computers, with their tokens, pinned certificate fingerprints and the phone's device key.
  The secrets are not copied; the new app opens the same secure storage the old app wrote.
- Settings (theme, density, sort, visibility rules), favorites, bookmarks, offline pins and sync rules.

A computer whose token or pin cannot be read is listed, refuses to connect, and says to pair it
again. The app never falls back to an unpinned connection.

Needs Android 7.0 (API 24) or newer. The version code must stay higher than the installed build
(81 for the first React Native release).

## Roll back

Android refuses to install an older version code over a newer one, so there is no in-place downgrade.
To go back to the Flutter app:

1. Uninstall the React Native app (this deletes its data).
2. Install the Flutter APK (v1.42.5 or newer).
3. Pair each computer again (`rfe pair`, or Ask to pair).

Paired devices from before stay in the agent's list; pairing again from the same phone reuses its row.
Revoke or remove the old entries with `rfe revoke <id>` or `rfe remove <id>` if the phone
is gone for good.

The agent needs no change in either direction. An agent built from this repository serves both apps.

## What the first release was checked against

See `docs/rn-migration/ledger.md`: the upgrade over a real Flutter 1.42.5 install on a Galaxy S23 Ultra,
the API 24 and API 36 emulators, a Galaxy A53, two routed virtual networks, the pinned-certificate
refusal, and the security review.
