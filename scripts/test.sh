#!/bin/bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
python3 scripts/check-privacy.py --self-test
python3 scripts/check-package-privacy.py --self-test
python3 scripts/check-privacy.py
"${AIRLINK_GO:-go}" test -race ./...
DOTNET_CLI_TELEMETRY_OPTOUT=1 DOTNET_GENERATE_ASPNET_CERTIFICATE=false DOTNET_SKIP_FIRST_TIME_EXPERIENCE=1 DOTNET_NOLOGO=1 "${AIRLINK_DOTNET:-dotnet}" run --project windows/AirLink.Core.Tests/AirLink.Core.Tests.csproj --configuration Release
npm --prefix web test
npm --prefix web run build
if [[ "$(uname -s)" == Darwin ]]; then
 swift test --package-path macos --scratch-path build/swift-tests
fi
