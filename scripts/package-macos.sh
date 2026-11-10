#!/bin/bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
VERSION="$(cat "$ROOT/VERSION")"
OUTPUT="${AIRLINK_BUILD_DIR:-$ROOT/build}"
DIST="${AIRLINK_DIST_DIR:-$ROOT/dist}"
mkdir -p "$DIST"
PACKAGES=()
for ARCH in arm64 x64; do
 STAGE="$OUTPUT/dmg-stage-$ARCH"
 rm -rf "$STAGE"
 mkdir -p "$STAGE"
 ditto "$OUTPUT/macos-$ARCH/AirLink.app" "$STAGE/AirLink.app"
 ln -s /Applications "$STAGE/Applications"
 ARTIFACT="$DIST/AirLink-$VERSION-macos-$ARCH.dmg"
 hdiutil create -volname AirLink -srcfolder "$STAGE" -ov -format UDZO "$ARTIFACT"
 hdiutil verify "$ARTIFACT"
 PACKAGES+=("$ARTIFACT")
done
python3 "$ROOT/scripts/check-package-privacy.py" "${PACKAGES[@]}"
