#!/usr/bin/env bash
# Builds an x86_64 test APK that can replace the Flutter debug build on the emulator.
set -euo pipefail
cd "$(dirname "$0")/.."
export RFE_DEBUG_KEYSTORE="$HOME/.android/debug.keystore" RFE_TEST_BUILD=1
npx expo prebuild --platform android --clean --no-install >/dev/null
(cd android && ./gradlew :app:assembleRelease -PreactNativeArchitectures=x86_64 -q)
echo android/app/build/outputs/apk/release/app-release.apk
