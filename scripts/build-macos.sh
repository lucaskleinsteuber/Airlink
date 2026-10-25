#!/bin/bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUTPUT="${AIRLINK_BUILD_DIR:-$ROOT/build}"
APP="$OUTPUT/AirLink.app"
ICON_TOOL="$(xcode-select -p)/../Applications/Icon Composer.app/Contents/Executables/ictool"
for ARCH in arm64 x86_64; do
  HELPER_ARCH="$ARCH"
  if [[ "$ARCH" == x86_64 ]]; then HELPER_ARCH=amd64; fi
  if [[ ! -f "$OUTPUT/airlink-darwin-$HELPER_ARCH" ]]; then
    echo "Missing $OUTPUT/airlink-darwin-$HELPER_ARCH; build Go helpers first." >&2
    exit 1
  fi
  swift build --package-path "$ROOT/macos" --scratch-path "$OUTPUT/swift-$ARCH" --configuration release --triple "$ARCH-apple-macosx14.0" \
    -Xswiftc -gnone -Xswiftc -file-prefix-map -Xswiftc "$ROOT=/AirLink" \
    -Xswiftc -debug-prefix-map -Xswiftc "$ROOT=/AirLink"
done
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Helpers" "$APP/Contents/Resources" "$APP/Contents/Frameworks" "$OUTPUT/icon-previews"
SPARKLE_ARTIFACT="$OUTPUT/swift-arm64/artifacts/sparkle/Sparkle"
SPARKLE_SOURCE="$SPARKLE_ARTIFACT/Sparkle.xcframework/macos-arm64_x86_64/Sparkle.framework"
test -f "$SPARKLE_SOURCE/Versions/Current/Sparkle"
# Copy only the runtime framework, preserving its versioned symlinks. SDK dSYMs,
# headers' sibling debug archives and signing tools remain outside the app.
rm -rf "$APP/Contents/Frameworks/Sparkle.framework"
ditto "$SPARKLE_SOURCE" "$APP/Contents/Frameworks/Sparkle.framework"
cp "$SPARKLE_ARTIFACT/LICENSE" "$APP/Contents/Resources/Sparkle-LICENSE.txt"
cp "$ROOT/LICENSE" "$APP/Contents/Resources/AirLink-LICENSE.txt"
ARM_BIN="$(swift build --package-path "$ROOT/macos" --scratch-path "$OUTPUT/swift-arm64" --configuration release --triple arm64-apple-macosx14.0 --show-bin-path)"
INTEL_BIN="$(swift build --package-path "$ROOT/macos" --scratch-path "$OUTPUT/swift-x86_64" --configuration release --triple x86_64-apple-macosx14.0 --show-bin-path)"
lipo -create "$ARM_BIN/AirLink" "$INTEL_BIN/AirLink" -output "$APP/Contents/MacOS/AirLink"
# Remove linker debug-map paths before signing the distributed executable.
xcrun strip -S "$APP/Contents/MacOS/AirLink"
lipo -create "$OUTPUT/airlink-darwin-arm64" "$OUTPUT/airlink-darwin-amd64" -output "$APP/Contents/Helpers/airlink"
cp "$ROOT/macos/Info.plist" "$APP/Contents/Info.plist"
cp -R "$ROOT/macos/Resources/." "$APP/Contents/Resources/"
/usr/libexec/PlistBuddy -c "Set :CFBundleShortVersionString $(cat "$ROOT/VERSION")" "$APP/Contents/Info.plist"
# Verify the compiled-in trust anchor against the canonical public-only config.
# The publisher's private signing key is never needed by an application build.
python3 - "$ROOT/updates/public-keys.json" "$APP/Contents/Info.plist" <<'PYCONFIG'
import base64, json, pathlib, plistlib, sys
config = json.loads(pathlib.Path(sys.argv[1]).read_text())
info = plistlib.loads(pathlib.Path(sys.argv[2]).read_bytes())
key = config['publicKey']
if len(base64.b64decode(key, validate=True)) != 32 or info.get('SUPublicEDKey') != key:
    raise SystemExit('macOS updater public key is missing or differs from publication config')
if info.get('SUFeedURL') != config['feeds']['macos']:
    raise SystemExit('macOS updater feed differs from publication config')
for name, expected in {'SUEnableAutomaticChecks': False, 'SUAutomaticallyUpdate': False,
                       'SUAllowsAutomaticUpdates': False, 'SUEnableSystemProfiling': False,
                       'SUVerifyUpdateBeforeExtraction': True, 'SURequireSignedFeed': True}.items():
    if info.get(name) is not expected:
        raise SystemExit('Incorrect macOS updater policy: ' + name)
if type(info.get('SUSignedFeedFailureExpirationInterval')) is not int or info['SUSignedFeedFailureExpirationInterval'] != 0:
    raise SystemExit('Signed-feed failures must not expire')
PYCONFIG
for RENDITION in Default Dark TintedLight; do
  "$ICON_TOOL" "$ROOT/macos/AppIcon.icon" --export-image --output-file "$OUTPUT/icon-previews/$RENDITION.png" --platform macOS --rendition "$RENDITION" --width 1024 --height 1024 --scale 1
done
xcrun actool "$ROOT/macos/AppIcon.icon" --compile "$APP/Contents/Resources" --platform macosx --minimum-deployment-target 14.0 --app-icon AppIcon --output-partial-info-plist "$OUTPUT/icon-info.plist" --output-format human-readable-text
/usr/libexec/PlistBuddy -c "Merge $OUTPUT/icon-info.plist" "$APP/Contents/Info.plist"
# actool emits both modern Assets.car and an AppIcon.icns fallback from .icon.
test -f "$APP/Contents/Resources/AppIcon.icns"
# Re-sign nested runtime components from the inside out. Preserve their sandbox
# entitlements; this private preview does not enable host library validation.
SPARKLE="$APP/Contents/Frameworks/Sparkle.framework"
for COMPONENT in "XPCServices/Downloader.xpc" "XPCServices/Installer.xpc" "Autoupdate" "Updater.app"; do
  codesign --force --sign - --preserve-metadata=entitlements "$SPARKLE/Versions/Current/$COMPONENT"
done
codesign --force --sign - "$SPARKLE"
codesign --verify --strict --deep "$SPARKLE"
codesign --force --sign - "$APP/Contents/Helpers/airlink"
codesign --force --sign - "$APP"
codesign --verify --strict --deep "$APP"
echo "Built universal $APP (ad hoc signed; not notarized)."
