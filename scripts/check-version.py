#!/usr/bin/env python3
"""Read-only cross-platform version consistency check."""
from pathlib import Path
import plistlib
import re
import xml.etree.ElementTree as ET
root=Path(__file__).resolve().parents[1]
version=(root/'VERSION').read_text().strip()
match=re.fullmatch(r'(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-beta\.([1-9]\d*))?',version)
if not match: raise SystemExit('VERSION must be Major.Minor.Patch[-beta.N]')
base='.'.join(match.groups()[:3]);build=(root/'BUILD_NUMBER').read_text().strip()
if not build.isdecimal() or not 0<int(build)<=65535 or any(int(x)>65535 for x in match.groups()[:3]): raise SystemExit('OS numeric version is outside its bounds')
if 'const Version = "'+version+'"' not in (root/'internal/model/model.go').read_text(): raise SystemExit('Go service version differs')
info=plistlib.loads((root/'macos/Info.plist').read_bytes())
if (info['CFBundleShortVersionString'],info['CFBundleVersion'],info['AirLinkDisplayVersion'])!=(base,build,version): raise SystemExit('macOS version fields differ')
for name in ('AirLink.Core','AirLink.Windows'):
    document=ET.parse(root/'windows'/name/(name+'.csproj'))
    if document.findtext('./PropertyGroup/Version')!=version: raise SystemExit('Windows project version differs')
    if name=='AirLink.Windows':
        for field in ('AssemblyVersion','FileVersion'):
            if document.findtext('./PropertyGroup/'+field)!=base+'.'+build: raise SystemExit('Windows numeric version differs')
manifest=ET.parse(root/'windows/AirLink.Windows/app.manifest')
identity=manifest.find('{urn:schemas-microsoft-com:asm.v1}assemblyIdentity')
if identity is None or identity.attrib['version']!=base+'.'+build: raise SystemExit('Windows manifest version differs')
if match.group(4) and 'pkgver='+base+'beta'+match.group(4) not in (root/'linux/packaging/PKGBUILD').read_text(): raise SystemExit('Arch package version differs')
print('Version consistent: '+version+'; numeric build '+build)
