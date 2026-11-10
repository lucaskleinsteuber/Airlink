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
 bash scripts/package-macos.sh
 for ARCH in arm64 x64; do PACKAGES+=("dist/AirLink-$VERSION-macos-$ARCH.dmg"); done
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
