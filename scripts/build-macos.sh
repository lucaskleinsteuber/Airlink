#!/bin/bash
# Builds signed app bundles and written icons without launching either app.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUTPUT="${AIRLINK_BUILD_DIR:-$ROOT/build}"
ICON_TOOL="$(xcode-select -p)/../Applications/Icon Composer.app/Contents/Executables/ictool"
VERSION="$(cat "$ROOT/VERSION")"
BUILD="$(cat "$ROOT/BUILD_NUMBER")"
mkdir -p "$OUTPUT/icon-previews" "$OUTPUT/icon-assets"
for RENDITION in Default Dark TintedLight; do
 "$ICON_TOOL" "$ROOT/macos/AppIcon.icon" --export-image --output-file "$OUTPUT/icon-previews/$RENDITION.png" --platform macOS --rendition "$RENDITION" --width 1024 --height 1024 --scale 1
done
xcrun actool "$ROOT/macos/AppIcon.icon" --compile "$OUTPUT/icon-assets" --platform macosx --minimum-deployment-target 14.0 --app-icon AppIcon --output-partial-info-plist "$OUTPUT/icon-info.plist" --output-format human-readable-text
for ARCH in arm64 x86_64; do
 HELPER_ARCH="$ARCH"; LABEL="$ARCH"
 if [[ "$ARCH" == x86_64 ]]; then HELPER_ARCH=amd64; LABEL=x64; fi
 test -f "$OUTPUT/airlink-darwin-$HELPER_ARCH"
 swift build --package-path "$ROOT/macos" --scratch-path "$OUTPUT/swift-$ARCH" --configuration release --triple "$ARCH-apple-macosx14.0" -j 4 \
   -Xswiftc -gnone -Xswiftc -file-prefix-map -Xswiftc "$ROOT=/AirLink" -Xswiftc -debug-prefix-map -Xswiftc "$ROOT=/AirLink"
 BIN="$(swift build --package-path "$ROOT/macos" --scratch-path "$OUTPUT/swift-$ARCH" --configuration release --triple "$ARCH-apple-macosx14.0" --show-bin-path)"
 APP="$OUTPUT/macos-$LABEL/AirLink.app"
 mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Helpers" "$APP/Contents/Resources" "$APP/Contents/Frameworks"
 cp "$BIN/AirLink" "$APP/Contents/MacOS/AirLink"
 xcrun strip -S "$APP/Contents/MacOS/AirLink"
 cp "$OUTPUT/airlink-darwin-$HELPER_ARCH" "$APP/Contents/Helpers/airlink"
 cp "$ROOT/macos/Info.plist" "$APP/Contents/Info.plist"
 cp -R "$ROOT/macos/Resources/." "$APP/Contents/Resources/"
 cp -R "$OUTPUT/icon-assets/." "$APP/Contents/Resources/"
 /usr/libexec/PlistBuddy -c "Set :CFBundleShortVersionString ${VERSION%%-*}" "$APP/Contents/Info.plist"
 /usr/libexec/PlistBuddy -c "Set :CFBundleVersion $BUILD" "$APP/Contents/Info.plist"
 /usr/libexec/PlistBuddy -c "Set :AirLinkDisplayVersion $VERSION" "$APP/Contents/Info.plist"
 /usr/libexec/PlistBuddy -c "Set :SUFeedURL https://raw.githubusercontent.com/lucaskleinsteuber/Airlink/main/updates/macos-$LABEL.xml" "$APP/Contents/Info.plist"
 /usr/libexec/PlistBuddy -c "Merge $OUTPUT/icon-info.plist" "$APP/Contents/Info.plist"
 SPARKLE_ARTIFACT="$OUTPUT/swift-$ARCH/artifacts/sparkle/Sparkle"
 SPARKLE="$APP/Contents/Frameworks/Sparkle.framework"
 rm -rf "$SPARKLE"
 ditto "$SPARKLE_ARTIFACT/Sparkle.xcframework/macos-arm64_x86_64/Sparkle.framework" "$SPARKLE"
 cp "$SPARKLE_ARTIFACT/LICENSE" "$APP/Contents/Resources/Sparkle-LICENSE.txt"
 cp "$ROOT/LICENSE" "$APP/Contents/Resources/AirLink-LICENSE.txt"
 python3 - "$ROOT/updates/public-keys.json" "$APP/Contents/Info.plist" "$LABEL" <<'PYCONFIG'
import base64, json, pathlib, plistlib, sys
config = json.loads(pathlib.Path(sys.argv[1]).read_text())
info = plistlib.loads(pathlib.Path(sys.argv[2]).read_bytes())
if len(base64.b64decode(config['publicKey'], validate=True)) != 32 or info.get('SUPublicEDKey') != config['publicKey']: raise SystemExit('Wrong macOS public update key')
if info.get('SUFeedURL') != config['feeds']['macos-'+sys.argv[3]]: raise SystemExit('Wrong native architecture feed')
for name, expected in {'SUEnableAutomaticChecks':False,'SUAutomaticallyUpdate':False,'SUAllowsAutomaticUpdates':False,'SUEnableSystemProfiling':False,'SUVerifyUpdateBeforeExtraction':True,'SURequireSignedFeed':True}.items():
    if info.get(name) is not expected: raise SystemExit('Incorrect updater policy: '+name)
if info.get('SUSignedFeedFailureExpirationInterval') != 0: raise SystemExit('Signed-feed failures must not expire')
PYCONFIG
 # Sparkle's helpers remain an upstream universal runtime; app and transfer
 # helper are single-architecture and the feed is bound to this architecture.
 for COMPONENT in XPCServices/Downloader.xpc XPCServices/Installer.xpc Autoupdate Updater.app; do
  codesign --force --sign - --preserve-metadata=entitlements "$SPARKLE/Versions/Current/$COMPONENT"
 done
 codesign --force --sign - "$SPARKLE"
 codesign --force --sign - "$APP/Contents/Helpers/airlink"
 codesign --force --sign - "$APP"
 codesign --verify --strict --deep "$APP"
 test "$(lipo -archs "$APP/Contents/MacOS/AirLink")" = "$ARCH"
 test "$(lipo -archs "$APP/Contents/Helpers/airlink")" = "$ARCH"
 echo "Built macOS $LABEL $VERSION (ad hoc signed; not notarized)."
done
