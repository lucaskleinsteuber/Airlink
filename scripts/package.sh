#!/bin/bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
VERSION="$(cat VERSION)"
mkdir -p dist
PACKAGES=()
for ARCH in x64 arm64; do
 ARTIFACT="dist/AirLink-$VERSION-windows-$ARCH-setup.exe"
 cp "build/windows-$ARCH/AirLink-Setup.exe" "$ARTIFACT"; PACKAGES+=("$ARTIFACT")
done
if [[ "$(uname -s)" == Darwin ]]; then
 for ARCH in arm64 x64; do
  STAGE="build/dmg-stage-$ARCH"
  rm -rf "$STAGE"
  mkdir -p "$STAGE"
  ditto "build/macos-$ARCH/AirLink.app" "$STAGE/AirLink.app"
  ln -s /Applications "$STAGE/Applications"
  ARTIFACT="dist/AirLink-$VERSION-macos-$ARCH.dmg"
  hdiutil create -volname AirLink -srcfolder "$STAGE" -ov -format UDZO "$ARTIFACT"
  hdiutil verify "$ARTIFACT"
  PACKAGES+=("$ARTIFACT")
 done
fi
for EXT in AppImage deb rpm pkg.tar.zst; do
 ARTIFACT="AirLink-$VERSION-linux-x64.$EXT"
 cp "build/linux-packages/$ARTIFACT" "dist/$ARTIFACT"
 PACKAGES+=("dist/$ARTIFACT")
done
CHECKSUM_NAMES=()
for ARTIFACT in "${PACKAGES[@]}"; do CHECKSUM_NAMES+=("${ARTIFACT#dist/}"); done
(cd dist && shasum -a 256 "${CHECKSUM_NAMES[@]}" > SHA256SUMS.txt)
python3 scripts/check-package-privacy.py "${PACKAGES[@]}"
