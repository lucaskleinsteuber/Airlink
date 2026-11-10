#!/bin/bash
# Compiles and inspects Windows Setup without executing Windows code.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUTPUT="${AIRLINK_BUILD_DIR:-$ROOT/build}"
NSIS_BIN="${AIRLINK_NSIS:-makensis}"
VERSION="$(cat "$ROOT/VERSION")"
native_path() {
  if command -v cygpath >/dev/null 2>&1; then cygpath -aw "$1"; else printf '%s\n' "$1"; fi
}
if ! command -v "$NSIS_BIN" >/dev/null 2>&1; then
  echo "Missing NSIS compiler. Install makensis/NSIS or set AIRLINK_NSIS." >&2
  exit 1
fi
for ARCH in x64 arm64; do
  PAYLOAD="$OUTPUT/windows-$ARCH/AirLink.exe"
  SETUP="$OUTPUT/windows-$ARCH/AirLink-Setup.exe"
  test -f "$PAYLOAD"
  MSYS2_ARG_CONV_EXCL='*' "$NSIS_BIN" -V3 -INPUTCHARSET UTF8 \
    "-DAIRLINK_NUMERIC_VERSION=${VERSION%%-*}.$(cat "$ROOT/BUILD_NUMBER")" "-DAIRLINK_VERSION=$VERSION" "-DAIRLINK_ARCH=$ARCH" \
    "-DAIRLINK_PAYLOAD=$(native_path "$PAYLOAD")" "-DAIRLINK_OUTPUT=$(native_path "$SETUP")" \
    "-DAIRLINK_ICON=$(native_path "$ROOT/windows/AirLink.Windows/Assets/AirLink.ico")" \
    "$(native_path "$ROOT/windows/installer/AirLink.nsi")"
  python3 "$ROOT/scripts/check-package-privacy.py" \
    --expected-installer-payload "$PAYLOAD" --expected-installer-arch "$ARCH" "$SETUP"
done
