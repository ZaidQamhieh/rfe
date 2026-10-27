#!/usr/bin/env bash
# Builds the release APK signed with the production key kept outside the repo in ~/.rfe-keystore.
# The password is read from a file by Gradle at build time; nothing secret is written into android/.
# RFE_ABIS narrows the build, e.g. RFE_ABIS=x86_64 or RFE_ABIS=arm64-v8a,armeabi-v7a,x86_64 (default: all four).
# RFE_VERBOSE=1 shows Gradle progress (CI sets it; the default is quiet).
set -euo pipefail
cd "$(dirname "$0")/.."
KS="${RFE_RELEASE_KEYSTORE:-$HOME/.rfe-keystore/upload-keystore.jks}"
PASS="${RFE_RELEASE_STOREPASS_FILE:-$HOME/.rfe-keystore/.storepass}"
[ -f "$KS" ] && [ -f "$PASS" ] || { echo "missing release keystore ($KS) or password file ($PASS)"; exit 1; }
unset RFE_TEST_BUILD RFE_DEBUG_KEYSTORE
export RFE_RELEASE_KEYSTORE="$KS" RFE_RELEASE_STOREPASS_FILE="$PASS"
[ -z "${RFE_RELEASE_KEYPASS_FILE:-}" ] || export RFE_RELEASE_KEYPASS_FILE
if [ -n "${RFE_VERBOSE:-}" ]; then npx expo prebuild --platform android --clean --no-install; else npx expo prebuild --platform android --clean --no-install >/dev/null; fi
quiet=(-q); [ -z "${RFE_VERBOSE:-}" ] || quiet=(--console=plain)
abis=()
[ -n "${RFE_ABIS:-}" ] && abis=(-PreactNativeArchitectures="$RFE_ABIS")
(cd android && ./gradlew :app:assembleRelease "${abis[@]}" "${quiet[@]}")
apk=android/app/build/outputs/apk/release/app-release.apk
scripts/check-app-identity.sh --apk "$apk"
echo "$apk"
