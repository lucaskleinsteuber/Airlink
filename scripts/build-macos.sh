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
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Helpers" "$APP/Contents/Resources" "$OUTPUT/icon-previews"
ARM_BIN="$(swift build --package-path "$ROOT/macos" --scratch-path "$OUTPUT/swift-arm64" --configuration release --triple arm64-apple-macosx14.0 --show-bin-path)"
INTEL_BIN="$(swift build --package-path "$ROOT/macos" --scratch-path "$OUTPUT/swift-x86_64" --configuration release --triple x86_64-apple-macosx14.0 --show-bin-path)"
lipo -create "$ARM_BIN/AirLink" "$INTEL_BIN/AirLink" -output "$APP/Contents/MacOS/AirLink"
# Remove linker debug-map paths before signing the distributed executable.
xcrun strip -S "$APP/Contents/MacOS/AirLink"
lipo -create "$OUTPUT/airlink-darwin-arm64" "$OUTPUT/airlink-darwin-amd64" -output "$APP/Contents/Helpers/airlink"
cp "$ROOT/macos/Info.plist" "$APP/Contents/Info.plist"
cp -R "$ROOT/macos/Resources/." "$APP/Contents/Resources/"
/usr/libexec/PlistBuddy -c "Set :CFBundleShortVersionString $(cat "$ROOT/VERSION")" "$APP/Contents/Info.plist"
for RENDITION in Default Dark TintedLight; do
  "$ICON_TOOL" "$ROOT/macos/AppIcon.icon" --export-image --output-file "$OUTPUT/icon-previews/$RENDITION.png" --platform macOS --rendition "$RENDITION" --width 1024 --height 1024 --scale 1
done
xcrun actool "$ROOT/macos/AppIcon.icon" --compile "$APP/Contents/Resources" --platform macosx --minimum-deployment-target 14.0 --app-icon AppIcon --output-partial-info-plist "$OUTPUT/icon-info.plist" --output-format human-readable-text
/usr/libexec/PlistBuddy -c "Merge $OUTPUT/icon-info.plist" "$APP/Contents/Info.plist"
# actool emits both modern Assets.car and an AppIcon.icns fallback from .icon.
test -f "$APP/Contents/Resources/AppIcon.icns"
codesign --force --sign - "$APP/Contents/Helpers/airlink"
codesign --force --sign - "$APP"
codesign --verify --strict --deep "$APP"
echo "Built universal $APP (ad hoc signed; not notarized)."
