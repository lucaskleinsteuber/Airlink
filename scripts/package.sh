#!/bin/bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
VERSION="$(cat VERSION)"
mkdir -p dist
for ARCH in x64 arm64; do
  # Windows is a direct self-contained executable; licenses are embedded in-app.
  cp "build/windows-$ARCH/AirLink.exe" "dist/AirLink-$VERSION-windows-$ARCH.exe"
done
for ARCH in arm64 amd64; do
  STAGE="build/package-linux-$ARCH"
  mkdir -p "$STAGE"
  cp "build/airlink-linux-$ARCH" "$STAGE/airlink"
  cp README.md LICENSE "$STAGE/"
  COPYFILE_DISABLE=1 tar -czf "dist/airlink-$VERSION-linux-$ARCH.tar.gz" -C "$STAGE" airlink README.md LICENSE
done
if [[ "$(uname -s)" == Darwin ]]; then
 STAGE="build/dmg-stage"
 mkdir -p "$STAGE"
 ditto build/AirLink.app "$STAGE/AirLink.app"
 if [[ ! -L "$STAGE/Applications" ]]; then ln -s /Applications "$STAGE/Applications"; fi
 hdiutil create -volname AirLink -srcfolder "$STAGE" -ov -format UDZO "dist/AirLink-$VERSION-macos-universal.dmg"
fi
(cd dist && shopt -s nullglob && shasum -a 256 ./*-"$VERSION"-*.dmg ./*-"$VERSION"-*.exe ./*-"$VERSION"-*.tar.gz > SHA256SUMS.txt)
