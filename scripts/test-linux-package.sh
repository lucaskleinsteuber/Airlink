#!/bin/bash
# Runs only inside a disposable container. QCoreApplication/--version only.
set -euo pipefail
FORMAT="$AIRLINK_PACKAGE_FORMAT"
cd /tmp
case "$FORMAT" in
 deb)
  apt-get update
  apt-get install -y --no-install-recommends ca-certificates qt6-wayland libqt6svg6 libqt6test6 libssl3 xz-utils zstd
  PACKAGE="$(find /AirLink/build/linux-artifacts -name '*.deb' -type f -print -quit)"
  apt-get install -y "$PACKAGE"
  ;;
 arch)
  pacman -Syu --noconfirm --needed qt6-base qt6-svg qt6-wayland openssl zstd
  PACKAGE="$(find /AirLink/build/linux-artifacts -name '*.pkg.tar.zst' -type f -print -quit)"
  pacman -U --noconfirm "$PACKAGE"
  ;;
 rpm)
  if ! command -v find >/dev/null; then
    if command -v dnf >/dev/null; then dnf install -y findutils; else zypper --non-interactive install findutils; fi
  fi
  PACKAGE="$(find /AirLink/build/linux-artifacts -name '*.rpm' -type f -print -quit)"
  if command -v dnf >/dev/null; then dnf install -y "$PACKAGE" qt6-qtwayland qt6-qtsvg openssl-libs; else zypper --non-interactive --no-gpg-checks install --allow-unsigned-rpm "$PACKAGE" qt6-wayland libQt6Svg6 libQt6Test6 libopenssl3; fi
  ;;
 *) exit 1;;
esac
VERSION="$(cat /AirLink/VERSION)"
test "$(/usr/bin/airlink --version)" = "$VERSION"
test -x /usr/libexec/airlink/airlink-service
test -f /usr/share/applications/io.github.lucaskleinsteuber.AirLink.desktop
mkdir -p /root/.config/AirLink /root/Downloads
STATE=/root/.config/AirLink
RECEIVED=/root/Downloads
printf 'kept user configuration\n' > "$STATE/fixture"
printf 'kept received content\n' > "$RECEIVED/fixture"
# Install a synthetic successor with the exact same payload. It tests the
# package manager upgrade mechanism; it is never a published AirLink build.
case "$FORMAT" in
 deb)
  dpkg-deb -R "$PACKAGE" /tmp/next-package
  sed -i 's/^Version:.*/Version: 0.12.0~beta.2-1/' /tmp/next-package/DEBIAN/control
  dpkg-deb -Zxz --root-owner-group --build /tmp/next-package /tmp/next.deb
  apt-get install -y /tmp/next.deb
  test "$(dpkg-query -W -f='${Version}' airlink)" = '0.12.0~beta.2-1';;
 arch)
  mkdir -p /tmp/next-package
  tar --zstd -xf "$PACKAGE" -C /tmp/next-package
  sed -i 's/^pkgver = .*/pkgver = 0.12.0beta2-1/' /tmp/next-package/.PKGINFO
  tar --zstd --owner=0 --group=0 --numeric-owner -cf /tmp/next.pkg.tar.zst -C /tmp/next-package .PKGINFO usr
  pacman -U --noconfirm /tmp/next.pkg.tar.zst
  test "$(pacman -Q airlink)" = 'airlink 0.12.0beta2-1';;
 rpm)
  if command -v dnf >/dev/null; then dnf install -y rpm-build; else zypper --non-interactive install rpm-build; fi
  mkdir -p /tmp/rpm-fixture/{BUILD,BUILDROOT,RPMS,SPECS,SOURCES,SRPMS}
  cat > /tmp/rpm-fixture/SPECS/next.spec <<'SPEC'
%global debug_package %{nil}
%global _build_id_links none
%global __os_install_post %{nil}
Name: airlink
Version: 0.12.0~beta.2
Release: 1
Summary: Synthetic package upgrade acceptance fixture
License: MIT
BuildArch: x86_64
AutoReqProv: yes
%description
Never published. Contains the previously installed candidate payload.
%install
mkdir -p %{buildroot}/usr/bin %{buildroot}/usr/libexec/airlink %{buildroot}/usr/share/applications %{buildroot}/usr/share/licenses/airlink %{buildroot}/usr/share/icons/hicolor/scalable/apps %{buildroot}/usr/share/metainfo
cp /usr/bin/airlink %{buildroot}/usr/bin/
cp /usr/libexec/airlink/airlink-service %{buildroot}/usr/libexec/airlink/
cp /usr/share/applications/io.github.lucaskleinsteuber.AirLink.desktop %{buildroot}/usr/share/applications/
cp /usr/share/licenses/airlink/LICENSE %{buildroot}/usr/share/licenses/airlink/
cp /usr/share/icons/hicolor/scalable/apps/io.github.lucaskleinsteuber.AirLink.svg %{buildroot}/usr/share/icons/hicolor/scalable/apps/
cp /usr/share/metainfo/io.github.lucaskleinsteuber.AirLink.metainfo.xml %{buildroot}/usr/share/metainfo/
%files
/usr/bin/airlink
/usr/libexec/airlink/airlink-service
/usr/share/applications/io.github.lucaskleinsteuber.AirLink.desktop
/usr/share/licenses/airlink/LICENSE
/usr/share/icons/hicolor/scalable/apps/io.github.lucaskleinsteuber.AirLink.svg
/usr/share/metainfo/io.github.lucaskleinsteuber.AirLink.metainfo.xml
SPEC
  rpmbuild --define '_topdir /tmp/rpm-fixture' -bb /tmp/rpm-fixture/SPECS/next.spec
  NEXT="$(find /tmp/rpm-fixture/RPMS -name '*.rpm' -print -quit)"
  if command -v dnf >/dev/null; then dnf install -y "$NEXT"; else zypper --non-interactive --no-gpg-checks install --allow-unsigned-rpm "$NEXT"; fi
  test "$(rpm -q --qf '%{VERSION}-%{RELEASE}' airlink)" = '0.12.0~beta.2-1';;
esac
TEST_BINARY="$(find /AirLink/build/linux-artifacts -name airlink_core_tests -type f -print -quit)"
cp "$TEST_BINARY" /tmp/airlink-core-tests; chmod +x /tmp/airlink-core-tests
AIRLINK_TEST_HELPER=/usr/libexec/airlink/airlink-service /tmp/airlink-core-tests
IMAGE="$(find /AirLink/build/linux-artifacts -name '*.AppImage' -type f -print -quit)"
cp "$IMAGE" /tmp/AirLink.AppImage; chmod +x /tmp/AirLink.AppImage
test "$(/tmp/AirLink.AppImage --appimage-extract-and-run --version)" = "$VERSION"
case "$FORMAT" in
 deb)
  dpkg --compare-versions '0.12.0~beta.1-1' lt '0.12.0~beta.2-1'
  dpkg --compare-versions '0.12.0~beta.2-1' lt '0.12.0~beta.10-1'
  dpkg --compare-versions '0.12.0~beta.10-1' lt '0.12.0-1'
  apt-get remove -y airlink;;
 arch)
  test "$(vercmp 0.12.0beta1-1 0.12.0beta2-1)" = -1
  test "$(vercmp 0.12.0beta2-1 0.12.0beta10-1)" = -1
  test "$(vercmp 0.12.0beta10-1 0.12.0-1)" = -1
  pacman -R --noconfirm airlink;;
 rpm)
  test "$(rpm --eval '%{lua: print(rpm.vercmp("0.12.0~beta.1", "0.12.0~beta.2"))}')" = -1
  test "$(rpm --eval '%{lua: print(rpm.vercmp("0.12.0~beta.2", "0.12.0~beta.10"))}')" = -1
  test "$(rpm --eval '%{lua: print(rpm.vercmp("0.12.0~beta.10", "0.12.0"))}')" = -1
  if command -v dnf >/dev/null; then dnf remove -y airlink; else zypper --non-interactive remove airlink; fi;;
esac
test ! -e /usr/bin/airlink
test ! -e /usr/libexec/airlink/airlink-service
test "$(cat "$STATE/fixture")" = 'kept user configuration'
test "$(cat "$RECEIVED/fixture")" = 'kept received content'
echo "Passed headless installation, replacement, version ordering, runtime linkage, state preservation and removal: $FORMAT"
