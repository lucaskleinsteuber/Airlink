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
for ARCH in arm64 amd64; do
  STAGE="build/package-linux-$ARCH"
  mkdir -p "$STAGE"
  cp "build/airlink-linux-$ARCH" "$STAGE/airlink"
  cp README.md LICENSE "$STAGE/"
  # Archive ownership must not disclose the packager's local account or UID.
  python3 - "$STAGE" "dist/airlink-$VERSION-linux-$ARCH.tar.gz" <<'PY'
import gzip, pathlib, sys, tarfile
stage, destination = map(pathlib.Path, sys.argv[1:])
with destination.open('wb') as raw, gzip.GzipFile(filename='', fileobj=raw, mode='wb', mtime=0) as compressed:
    with tarfile.open(fileobj=compressed, mode='w', format=tarfile.PAX_FORMAT) as archive:
        for name in ('airlink', 'README.md', 'LICENSE'):
            source = stage / name
            info = archive.gettarinfo(str(source), arcname=name)
            info.uid = info.gid = 0
            info.uname = info.gname = ''
            info.mtime = 0
            info.pax_headers = {}
            with source.open('rb') as payload:
                archive.addfile(info, payload)
PY
done
if [[ "$(uname -s)" == Darwin ]]; then
 STAGE="build/dmg-stage"
 mkdir -p "$STAGE"
 ditto build/AirLink.app "$STAGE/AirLink.app"
 if [[ ! -L "$STAGE/Applications" ]]; then ln -s /Applications "$STAGE/Applications"; fi
 hdiutil create -volname AirLink -srcfolder "$STAGE" -ov -format UDZO "dist/AirLink-$VERSION-macos-universal.dmg"
fi
(cd dist && shopt -s nullglob && shasum -a 256 ./*-"$VERSION"-*.dmg ./*-"$VERSION"-*.exe ./*-"$VERSION"-*.tar.gz > SHA256SUMS.txt)
PACKAGES=("dist/AirLink-$VERSION-windows-x64-setup.exe" "dist/AirLink-$VERSION-windows-arm64-setup.exe" "dist/airlink-$VERSION-linux-amd64.tar.gz" "dist/airlink-$VERSION-linux-arm64.tar.gz")
if [[ "$(uname -s)" == Darwin ]]; then PACKAGES+=("dist/AirLink-$VERSION-macos-universal.dmg"); fi
python3 scripts/check-package-privacy.py "${PACKAGES[@]}"
