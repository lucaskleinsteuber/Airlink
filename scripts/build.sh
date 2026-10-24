#!/bin/bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
GO_BIN="${AIRLINK_GO:-go}"
cd "$ROOT"
npm --prefix web ci
npm --prefix web run build
bash scripts/embed-web.sh
mkdir -p build
for PLATFORM in darwin windows linux; do
  for ARCH in arm64 amd64; do
    OUTPUT="build/airlink-$PLATFORM-$ARCH"
    if [[ "$PLATFORM" == windows ]]; then OUTPUT="$OUTPUT.exe"; fi
    CGO_ENABLED=0 GOOS="$PLATFORM" GOARCH="$ARCH" "$GO_BIN" build -trimpath -ldflags='-s -w' -o "$OUTPUT" ./cmd/airlink
  done
done
if [[ "$(uname -s)" == Darwin ]]; then bash scripts/build-macos.sh; fi
