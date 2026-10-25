#!/bin/bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
VERSION="$(cat VERSION)"
mkdir -p dist
for ARCH in x64 arm64; do
  # Publish Setup; the self-contained app is installed into the user's Programs folder.
  cp "build/windows-$ARCH/AirLink-Setup.exe" "dist/AirLink-$VERSION-windows-$ARCH-setup.exe"
done
# Linux remains an experimental cross-build; no runtime-untested binary is published.
if [[ "$(uname -s)" == Darwin ]]; then
 STAGE="build/dmg-stage"
 mkdir -p "$STAGE"
 ditto build/AirLink.app "$STAGE/AirLink.app"
 if [[ ! -L "$STAGE/Applications" ]]; then ln -s /Applications "$STAGE/Applications"; fi
 hdiutil create -volname AirLink -srcfolder "$STAGE" -ov -format UDZO "dist/AirLink-$VERSION-macos-universal.dmg"
fi
PACKAGES=("dist/AirLink-$VERSION-windows-x64-setup.exe" "dist/AirLink-$VERSION-windows-arm64-setup.exe")
if [[ "$(uname -s)" == Darwin ]]; then PACKAGES+=("dist/AirLink-$VERSION-macos-universal.dmg"); fi
CHECKSUM_NAMES=()
for package_artifact in "${PACKAGES[@]}"; do CHECKSUM_NAMES+=("${package_artifact#dist/}"); done
(cd dist && shasum -a 256 "${CHECKSUM_NAMES[@]}" > SHA256SUMS.txt)
python3 scripts/check-package-privacy.py "${PACKAGES[@]}"
