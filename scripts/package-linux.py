#!/usr/bin/env python3
"""Create the four x64 packages from one Debian-12-built application payload."""
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import urllib.request

root = Path(__file__).resolve().parents[1]
stage, output, appdir = (Path(v).resolve() for v in sys.argv[1:4])
version = sys.argv[4]
match = re.fullmatch(r'(\d+\.\d+\.\d+)-beta\.([1-9]\d*)', version)
if not match: raise SystemExit('Expected a beta version')
base, beta = match.groups()
prefix = 'AirLink-'+version+'-linux-x64'

def run(*args, **kwargs): subprocess.run(args, check=True, **kwargs)

def write(path, content):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(content)

# Native packages have neutral ownership and contain no user configuration.
write(stage/'DEBIAN/control', f'''Package: airlink
Version: {base}~beta.{beta}-1
Section: net
Priority: optional
Architecture: amd64
Maintainer: Lucas Kleinsteuber <307191020+lucaskleinsteuber@users.noreply.github.com>
Depends: libc6 (>= 2.36), libstdc++6, libqt6core6 (>= 6.4), libqt6gui6, libqt6widgets6, libqt6network6, libqt6dbus6, libqt6svg6, libssl3, qt6-qpa-plugins, qt6-wayland
Homepage: https://github.com/lucaskleinsteuber/Airlink
Description: Local encrypted file transfers
 Native desktop application and private transfer service. No cloud account.
''')
run('dpkg-deb','-Zxz','--root-owner-group','--build',str(stage),str(output/(prefix+'.deb')))
shutil.rmtree(stage/'DEBIAN')

rpmroot=output/'rpm-build'
for directory in ('BUILD','BUILDROOT','RPMS','SOURCES','SPECS','SRPMS'): (rpmroot/directory).mkdir(parents=True, exist_ok=True)
spec=f'''%global debug_package %{{nil}}
%global _build_id_links none
%global __os_install_post %{{nil}}
Name: airlink
Version: {base}~beta.{beta}
Release: 1
Summary: Local encrypted file transfers
License: MIT
URL: https://github.com/lucaskleinsteuber/Airlink
BuildArch: x86_64
AutoReqProv: yes
Recommends: (qt6-qtwayland or qt6-wayland)
%description
Native desktop application and private transfer service. No cloud account.
%install
mkdir -p %{{buildroot}}
cp -a {stage}/usr %{{buildroot}}/
%files
/usr/bin/airlink
/usr/libexec/airlink/airlink-service
/usr/share/applications/io.github.lucaskleinsteuber.AirLink.desktop
/usr/share/icons/hicolor/scalable/apps/io.github.lucaskleinsteuber.AirLink.svg
/usr/share/metainfo/io.github.lucaskleinsteuber.AirLink.metainfo.xml
%license /usr/share/licenses/airlink/LICENSE
'''
write(rpmroot/'SPECS/airlink.spec',spec)
run('rpmbuild','--define','_topdir '+str(rpmroot),'-bb',str(rpmroot/'SPECS/airlink.spec'))
rpms=list((rpmroot/'RPMS/x86_64').glob('*.rpm'))
if len(rpms)!=1: raise SystemExit('Expected exactly one RPM')
shutil.copyfile(rpms[0],output/(prefix+'.rpm'))

write(stage/'.PKGINFO',f'''pkgname = airlink
pkgbase = airlink
pkgver = {base}beta{beta}-1
pkgdesc = Local encrypted file transfers
url = https://github.com/lucaskleinsteuber/Airlink
builddate = {os.environ['SOURCE_DATE_EPOCH']}
packager = Lucas Kleinsteuber <307191020+lucaskleinsteuber@users.noreply.github.com>
size = {sum(p.stat().st_size for p in stage.rglob('*') if p.is_file())}
arch = x86_64
license = MIT
depend = glibc>=2.36
depend = gcc-libs
depend = qt6-base>=6.4
depend = qt6-svg
depend = qt6-wayland
depend = openssl
''')
# libalpm's archive format is tar+zstd. PKGBUILD provides a native rebuild path.
run('tar','--zstd','--sort=name','--owner=0','--group=0','--numeric-owner','--mtime=@'+os.environ['SOURCE_DATE_EPOCH'],'-cf',str(output/(prefix+'.pkg.tar.zst')),'-C',str(stage),'.PKGINFO','usr')
(stage/'.PKGINFO').unlink()

shutil.copytree(stage/'usr',appdir/'usr',dirs_exist_ok=True)
pluginroot=Path('/usr/lib/x86_64-linux-gnu/qt6/plugins')
for part in pluginroot.iterdir():
    if part.name in ('platforms','platformthemes','imageformats','iconengines','platforminputcontexts') or part.name.startswith('wayland') or part.name.startswith('xcb'):
        shutil.copytree(part,appdir/'usr/plugins'/part.name,dirs_exist_ok=True)
libdir=appdir/'usr/lib';libdir.mkdir(exist_ok=True)
excluded={'libc.so.6','libm.so.6','libdl.so.2','libpthread.so.0','librt.so.1','libresolv.so.2','ld-linux-x86-64.so.2'}
licenses=set()
for binary in list((appdir/'usr').rglob('*')):
    if not binary.is_file() or binary.read_bytes()[:4]!=b'\x7fELF': continue
    dependencies=subprocess.run(['ldd',str(binary)],text=True,capture_output=True,check=False)
    if dependencies.returncode and 'not a dynamic executable' not in dependencies.stderr+dependencies.stdout: raise SystemExit('Could not inspect ELF dependencies')
    for line in dependencies.stdout.splitlines():
        m=re.match(r'\s*(\S+) => (/\S+)',line)
        if not m: continue
        soname, source=m.groups()
        if soname in excluded: continue
        destination=libdir/soname
        if not destination.exists(): shutil.copyfile(source,destination)
        owner=subprocess.run(['dpkg-query','-S',source],text=True,capture_output=True)
        if owner.returncode: owner=subprocess.run(['dpkg-query','-S',str(Path(source).resolve())],text=True,capture_output=True)
        if owner.returncode==0:
            package=owner.stdout.split(': ',1)[0].split(':',1)[0]
            licenses.add(package)
# Add license notices for bundled plugins and Qt even where merged-/usr aliases
# prevent dpkg-query from resolving an individual shared-library pathname.
for pattern in ('libqt6*','qt6-*'):
    licenses.update(p.name for p in Path('/usr/share/doc').glob(pattern))
for package in sorted(licenses):
    copyright=Path('/usr/share/doc')/package/'copyright'
    if copyright.is_file(): shutil.copyfile(copyright,appdir/'usr/share/licenses/airlink'/('third-party-'+package+'.txt'))
for name in ('LGPL-3','GPL-3'):
    shutil.copyfile('/usr/share/common-licenses/'+name,appdir/'usr/share/licenses/airlink'/name)
write(appdir/'usr/share/licenses/airlink/BUNDLED-LIBRARIES.txt','\n'.join(sorted(p.name for p in libdir.iterdir()))+'\n')
write(appdir/'usr/share/licenses/airlink/RELINKING.txt','''Qt is dynamically linked. Extract this AppImage with --appimage-extract,
replace the corresponding shared libraries under squashfs-root/usr/lib and
plugins under usr/plugins, then run squashfs-root/AppRun. The app can also be
rebuilt from its Git tag using CMake and the tracked PKGBUILD. Qt sources and
Debian patches: https://sources.debian.org/src/qt6-base/ and
https://sources.debian.org/src/qt6-wayland/ and https://sources.debian.org/src/qt6-svg/.
The exact package versions from the build follow in BUILD-PROVENANCE.txt.
''')
provenance=subprocess.check_output(['dpkg-query','-W','-f=${binary:Package} ${Version}\n'],text=True)
write(appdir/'usr/share/licenses/airlink/BUILD-PROVENANCE.txt',provenance)
shutil.copyfile(stage/'usr/share/applications/io.github.lucaskleinsteuber.AirLink.desktop',appdir/'io.github.lucaskleinsteuber.AirLink.desktop')
shutil.copyfile(stage/'usr/share/icons/hicolor/scalable/apps/io.github.lucaskleinsteuber.AirLink.svg',appdir/'io.github.lucaskleinsteuber.AirLink.svg')
write(appdir/'AppRun','''#!/bin/sh
set -eu
APPDIR="${APPDIR:-$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)}"
export LD_LIBRARY_PATH="$APPDIR/usr/lib${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
export QT_PLUGIN_PATH="$APPDIR/usr/plugins"
export QT_QPA_PLATFORM_PLUGIN_PATH="$APPDIR/usr/plugins/platforms"
export QT_QPA_PLATFORMTHEME=xdgdesktopportal
exec "$APPDIR/usr/bin/airlink" "$@"
''');(appdir/'AppRun').chmod(0o755)
write(appdir/'usr/bin/qt.conf','[Paths]\nPrefix=..\nPlugins=plugins\nLibraries=lib\n')
lock=json.loads((root/'linux/packaging/toolchain.json').read_text())['appImageRuntime']
runtime=output/'runtime-x86_64'
if not runtime.exists() or hashlib.sha256(runtime.read_bytes()).hexdigest()!=lock['sha256']:
    request=urllib.request.Request(lock['url'],headers={'User-Agent':'AirLink-release-builder'})
    with urllib.request.urlopen(request,timeout=90) as response: data=response.read(4*1024*1024+1)
    if len(data)>4*1024*1024 or hashlib.sha256(data).hexdigest()!=lock['sha256']: raise SystemExit('AppImage runtime checksum differs from the pinned toolchain')
    runtime.write_bytes(data)
squash=output/'appimage.squashfs'
run('mksquashfs',str(appdir),str(squash),'-noappend','-root-owned','-comp','gzip','-b','1048576','-processors','4','-mkfs-time',os.environ['SOURCE_DATE_EPOCH'],'-all-time',os.environ['SOURCE_DATE_EPOCH'],env={key:value for key,value in os.environ.items() if key!='SOURCE_DATE_EPOCH'})
image=output/(prefix+'.AppImage')
with image.open('wb') as target:
    target.write(runtime.read_bytes())
    with squash.open('rb') as content: shutil.copyfileobj(content,target)
image.chmod(0o755)
for extension in ('.AppImage','.deb','.rpm','.pkg.tar.zst'):
    package=output/(prefix+extension)
    print(hashlib.sha256(package.read_bytes()).hexdigest()+'  '+package.name)
