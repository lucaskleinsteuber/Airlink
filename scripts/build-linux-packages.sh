#!/bin/bash
# Debian 12 x64 builder. No desktop session or application is launched.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
VERSION="$(cat VERSION)"
GO_BIN="${AIRLINK_GO:-go}"
export GOMAXPROCS=4
AIRLINK_SOURCE_TIME="$(git -c safe.directory="$ROOT" log -1 --format=%ct)"
[[ "$AIRLINK_SOURCE_TIME" =~ ^[0-9]+$ ]]
export SOURCE_DATE_EPOCH="$AIRLINK_SOURCE_TIME"
OUTPUT="$ROOT/build/linux-packages"
STAGE="$ROOT/build/linux-stage"
APPDIR="$ROOT/build/linux-appdir"
mkdir -p "$OUTPUT" build
CGO_ENABLED=0 "$GO_BIN" build -p 4 -buildvcs=false -trimpath -ldflags='-s -w' -o build/airlink-linux-amd64 ./cmd/airlink
cmake -S linux -B build/linux-release -G Ninja -DCMAKE_BUILD_TYPE=Release -DCMAKE_INSTALL_PREFIX=/usr -DAIRLINK_HELPER="$ROOT/build/airlink-linux-amd64"
cmake --build build/linux-release -j 4
AIRLINK_TEST_HELPER="$ROOT/build/airlink-linux-amd64" ctest --test-dir build/linux-release --output-on-failure
# Only disposable staging trees from this script are replaced.
rm -rf "$STAGE" "$APPDIR"
mkdir -p "$STAGE" "$APPDIR"
DESTDIR="$STAGE" cmake --install build/linux-release --strip
python3 scripts/package-linux.py "$STAGE" "$OUTPUT" "$APPDIR" "$VERSION"
