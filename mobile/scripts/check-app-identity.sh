#!/usr/bin/env bash
# Guards the release identity of the RN app: it must keep the Flutter app's package name and only ever move its
# versionCode forward, or Android will refuse to install it over the Flutter build. With --apk it also checks the
# signing certificate of a built APK against EXPECTED_SIGNING_SHA256 (the release key's SHA-256, colon-free hex).
set -euo pipefail
cd "$(dirname "$0")/.."

EXPECTED_PACKAGE="com.zqamhieh.remote_file_explorer"
FLUTTER_VERSION_CODE=80   # last Flutter release, app/pubspec.yaml 1.42.5+80

pkg=$(node -p "require('./app.json').expo.android.package")
code=$(node -p "require('./app.json').expo.android.versionCode")
[ "$pkg" = "$EXPECTED_PACKAGE" ] || { echo "app.json android.package is '$pkg', expected '$EXPECTED_PACKAGE'"; exit 1; }
[ "$code" -gt "$FLUTTER_VERSION_CODE" ] || { echo "android.versionCode $code must exceed the Flutter build's $FLUTTER_VERSION_CODE"; exit 1; }
echo "package $pkg, versionCode $code: ok"

if [ "${1:-}" = "--apk" ]; then
  apk="${2:?usage: check-app-identity.sh --apk <file.apk>}"
  # The production key that signed the Flutter releases (public certificate digest, not a secret).
  : "${EXPECTED_SIGNING_SHA256:=40b844896859aac22c43c65c5a50787374c7e6c89c29892262b373aad2e878e9}"
  apksigner=$(ls "${ANDROID_HOME:-$HOME/Android/Sdk}"/build-tools/*/apksigner | sort -V | tail -1)
  actual=$("$apksigner" verify --print-certs "$apk" | sed -n 's/.*certificate SHA-256 digest: //p' | head -1 | tr -d ':' | tr 'A-F' 'a-f')
  [ "$actual" = "$(echo "$EXPECTED_SIGNING_SHA256" | tr -d ':' | tr 'A-F' 'a-f')" ] || { echo "signing certificate $actual does not match the expected release key"; exit 1; }
  echo "signing certificate matches: ok"
fi
