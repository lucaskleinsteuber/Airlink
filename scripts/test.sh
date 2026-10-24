#!/bin/bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
"${AIRLINK_GO:-go}" test -race ./...
npm --prefix web test
npm --prefix web run build
if [[ "$(uname -s)" == Darwin ]]; then
 swift test --package-path macos --scratch-path build/swift-tests
fi
