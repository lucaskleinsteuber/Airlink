#!/bin/bash
# Publishes a native GUI app. It never opens Windows UI or runs the executable.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUTPUT="${AIRLINK_BUILD_DIR:-$ROOT/build}"
DOTNET_BIN="${AIRLINK_DOTNET:-dotnet}"
export DOTNET_CLI_TELEMETRY_OPTOUT=1
export DOTNET_GENERATE_ASPNET_CERTIFICATE=false
export DOTNET_SKIP_FIRST_TIME_EXPERIENCE=1
export DOTNET_NOLOGO=1
export DOTNET_CLI_UI_LANGUAGE=en
if ! command -v "$DOTNET_BIN" >/dev/null 2>&1; then
  echo "Missing .NET SDK 10. Set AIRLINK_DOTNET to the SDK's dotnet executable." >&2
  exit 1
fi
cd "$ROOT"
for ARCH in x64 arm64; do
  HELPER_ARCH="$ARCH"
  if [[ "$ARCH" == x64 ]]; then HELPER_ARCH=amd64; fi
  HELPER="$OUTPUT/airlink-windows-$HELPER_ARCH.exe"
  if [[ ! -f "$HELPER" ]]; then
    echo "Missing $HELPER; build the Go helpers first." >&2
    exit 1
  fi
  DEST="$OUTPUT/windows-$ARCH"
  mkdir -p "$DEST"
  "$DOTNET_BIN" publish windows/AirLink.Windows/AirLink.Windows.csproj \
    --configuration Release --runtime "win-$ARCH" --self-contained true \
    --output "$DEST" -p:AirLinkHelperPath="$HELPER"
  python3 scripts/verify-windows-pe.py "$DEST/AirLink.exe" --arch "$ARCH"
  # Runtime packages carry the exact licensing material for the published version.
  # Locate versions from NuGet's resolved assets rather than guessing the runtime patch.
  python3 - "$ROOT" "$DEST" "$ARCH" <<'PY'
import json, os, pathlib, sys
root, dest, arch = pathlib.Path(sys.argv[1]), pathlib.Path(sys.argv[2]), sys.argv[3]
assets = json.loads((root/'windows/AirLink.Windows/obj/project.assets.json').read_text())
packages = [pathlib.Path(path) for path in assets['packageFolders']]
downloads = [dependency for framework in assets['project']['frameworks'].values() for dependency in framework.get('downloadDependencies', [])]
parts = ['AirLink includes the self-contained .NET and Windows Desktop runtimes.\nThe following notices and licenses accompany those runtime packages.\n']
for stem in ('Microsoft.NETCore.App.Runtime.win-', 'Microsoft.WindowsDesktop.App.Runtime.win-'):
    dependency = next(item for item in downloads if item['name'].lower() == (stem+arch).lower())
    version = dependency['version'].strip('[]').split(',')[0].strip()
    key = dependency['name']+'/'+version
    directory = next((cache/key.lower() for cache in packages if (cache/key.lower()).is_dir()), None)
    if directory is None: raise SystemExit('Missing runtime package: '+key)
    licenses = [directory/'LICENSE.TXT', directory/'LICENSE', directory/'THIRD-PARTY-NOTICES.TXT']
    found = False
    for path in licenses:
        if path.is_file():
            found = True
            parts.extend(['\n\n'+'='*72+'\n'+key+'/'+path.name+'\n'+'='*72+'\n', path.read_text(encoding='utf-8-sig')])
    if not found: raise SystemExit('Missing runtime license: '+key)
(dest/'THIRD-PARTY-NOTICES.txt').write_text(''.join(parts), encoding='utf-8')
PY
done
