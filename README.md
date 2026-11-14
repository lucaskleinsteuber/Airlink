# AirLink

<img src="docs/images/app-icon.png" width="112" height="112" alt="AirLink icon: two linked arcs">

**Large files, shared locally.**

AirLink sends files and folders directly between computers on the same local network. Native apps for **macOS, Linux and Windows** manage an embedded transfer service. No account, cloud relay or internet connection is needed for local transfers.

> **In development (0.13):** AirLink is being refocused on desktop-to-desktop transfers. The browser/phone interface, Quick Share links, browser relay, duplicate detection and extension sorting have been removed from the source; clipboard sharing remains. A new, much faster transfer protocol follows. The packages below are the previous 0.12 betas, which still contain the removed features.

**[0.12.0 Beta 3 is available](https://github.com/lucaskleinsteuber/Airlink/releases/tag/v0.12.0-beta.3).** The background and package checks passed. This is beta software: physical Mac↔Arch WLAN, desktop rendering and the new 100-GB acceptance run remain separate, open checks. [Verification records](docs/TESTING.md) distinguish tests, builds, packages and physical devices; [1.0 readiness](docs/RELEASE-READINESS.md) remains a separate gate.

Beta 3 (build 16) corrects the waiting-for-acceptance status after a reconnect. It retains Beta 2's transfer improvements, local diagnostics and recovery actions. See the [changelog](CHANGELOG.md) and the measured results in [TESTING.md](docs/TESTING.md). Upgrading migrates transfer metadata; Beta 1 and older cannot reopen the upgraded transfer database. Pairings, settings, confirmed progress and received files are preserved.

## Get AirLink

Use the packages from the matching [GitHub beta release](https://github.com/lucaskleinsteuber/Airlink/releases). All applications in a transfer should use the same beta when testing the new comparison-code flow.

| System | Package | Requirements and limits |
|---|---|---|
| Apple Silicon Mac | `AirLink-0.12.0-beta.3-macos-arm64.dmg` | macOS 14 or later; ad hoc signed, not notarized |
| Intel Mac | `AirLink-0.12.0-beta.3-macos-x64.dmg` | macOS 14 or later; separate x64 executable and helper |
| Linux x64, portable/user installation | `AirLink-0.12.0-beta.3-linux-x64.AppImage` | glibc 2.36+; bundled Qt and Wayland/X11 plugins |
| Debian / Ubuntu x64 | `AirLink-0.12.0-beta.3-linux-x64.deb` | Debian 12 / Ubuntu 24.04 or later; system Qt 6.4+ |
| Fedora / openSUSE x64 | `AirLink-0.12.0-beta.3-linux-x64.rpm` | Compatible supported releases; system Qt 6.4+ and OpenSSL 3 |
| Arch x64 | `AirLink-0.12.0-beta.3-linux-x64.pkg.tar.zst` | Current Arch; Qt 6 and Wayland packages |
| Windows Intel/AMD | `AirLink-0.12.0-beta.3-windows-x64-setup.exe` | Self-contained per-user installer |
| Windows on ARM | `AirLink-0.12.0-beta.3-windows-arm64-setup.exe` | Native ARM64 app and service |

The release includes `SHA256SUMS.txt`, `UPDATE-SIGNATURES.json`, its detached signature and signed update feeds. Verify the downloaded package before installing. Linux ARM and an AUR submission are outside this beta.

### macOS

Open the DMG and drag **AirLink.app** into **Applications**, replacing the existing app. Keep only the current `/Applications/AirLink.app`; do not store old app bundles there. Application data, pairings and received files stay separate and are preserved.

These betas are **ad hoc signed and not notarized**. macOS may require an explicit first-open approval through **System Settings → Privacy & Security → Open Anyway**. Approve only the package you obtained and verified from this repository; do not disable Gatekeeper globally.

### Linux

On Arch, use the native `.pkg.tar.zst` package and `pacman -U` below for normal installation with an application-menu entry.

For the AppImage, mark it executable in Dolphin → Properties → Permissions, or run `chmod +x ./AirLink-0.12.0-beta.3-linux-x64.AppImage` in its folder. Start it with `./AirLink-0.12.0-beta.3-linux-x64.AppImage`. Its **Add to application menu** action copies it into the user application directory and adds a launcher. After starting the installed copy successfully, the original download can be removed. A desktop session with a private `XDG_RUNTIME_DIR` is required. On systems without usable FUSE support, the AppImage runtime also supports `--appimage-extract-and-run`.

Install the native package using its package manager:

```sh
sudo pacman -U ./AirLink-0.12.0-beta.3-linux-x64.pkg.tar.zst
sudo apt install ./AirLink-0.12.0-beta.3-linux-x64.deb
sudo dnf install ./AirLink-0.12.0-beta.3-linux-x64.rpm
# openSUSE: sudo zypper install ./AirLink-0.12.0-beta.3-linux-x64.rpm
```

Use only the command for your distribution. The [PKGBUILD](linux/packaging/PKGBUILD) also builds the app from the checked-out source. Packages install the desktop launcher, app and private helper; they do not install a system-wide network service. AirLink runs under your user account and keeps existing Linux service data.

If a computer appears in AirLink but cannot receive files, check its firewall. Discovery and a successful pairing initiated from that computer do not prove incoming transfer connections are permitted. AirLink normally listens on **TCP 43871**; if that port is occupied, it can use another available port shown in its connection address. Permit the actual TLS port from the intended local subnet, keeping the firewall enabled. For example, on a UFW system whose home network is `192.168.1.0/24`:

```sh
sudo ufw allow proto tcp from 192.168.1.0/24 to any port 43871 comment 'AirLink local network'
```

Replace the example subnet and port with the actual values. This is a local firewall rule, not a router/Internet port forwarding rule. AirLink still requires trusted-device permissions and incoming acceptance unless automatic receiving was explicitly enabled.

### Windows

Run the Setup EXE for your architecture. AirLink installs under `%LOCALAPPDATA%\Programs\AirLink`, with a Start-menu entry and an entry in Installed apps. The app does not need to stay in Downloads, and no separate .NET installation is needed. Betas are not Authenticode signed. Quit AirLink through its tray before replacing an installed app; received files and saved pairings are preserved.

## First transfer

1. Open AirLink. The welcome screen suggests the computer's name and your **Downloads** folder; keep **Open at login** on so your other computers can always reach you, then choose **Get started**.
2. Open AirLink on the other computer too. Each computer appears on the other as a round tile under **Nearby**. Click it: both screens show the same **eight-digit code**. Confirm **Code matches** on both computers. Files from your own paired computers are accepted without asking by default (changeable per device).
3. Drag files or folders onto a computer's tile – or click the tile and choose them. That's it: progress appears as a ring around the tile and under **Recent**.
4. On the receiving computer a notification offers **Show**; the files are in the receive folder.

Files arrive directly in the selected receive folder. A transferred folder retains its hierarchy and empty subfolders. Name collisions receive a distinct name; existing files are never silently overwritten. Source files stay in place. Symbolic links are skipped visibly.

Pause, disconnects and restarts resume from the last verified segment. Closing the main window keeps AirLink running in the menu bar/tray; **Quit** saves state and stops the service.

Sending also works from the file manager: Finder (right-click → Services → **Send with AirLink**, or drop files on the Dock icon), Dolphin (right-click → **Send with AirLink**) and Explorer (right-click → Send to → **AirLink**).

## Features and platform limits

| Capability | Desktop apps |
|---|---|
| Files, folders, drag-and-drop | Native pickers/drop; direct service-to-service TLS |
| Pause, reconnect and persisted queue | Confirmed progress survives service restarts |
| Integrity and encryption | TLS 1.3 with pinned device identities and SHA-256 verification |
| Trusted devices and rights | Discovery, eight-digit code comparison, saved identities and revocation |
| Clipboard | Explicit paste/send/copy; per-device permission |
| History | Paged history and file details |
| Offline use | Transfers, discovery and paired identities work locally |

Linux follows system widgets and colors, with KDE/Wayland as the primary desktop target. macOS uses SwiftUI/AppKit, menu-bar operation, Finder Services, configurable shortcut and optional launch at login. Windows has native WPF controls and tray operation.

Phones and tablets are not supported. If a computer is not discovered automatically, choose **Add → Not found? Connect by IP address** and type the IP address shown under **Help** on the other computer; you still compare the eight-digit code on both screens. If a firewall blocks incoming connections on one computer (common on Linux), start connecting from that computer – afterwards sending works in both directions.

## Settings and updates

Settings have three areas – **General** (name, receive folder, open at login, notifications, language), **Devices** (per paired computer: accept without asking, share clipboard, permissions, remove) and **Advanced** (speed limits, shortcut on macOS, updates, address, diagnostic report). Changes apply immediately.

**Check for Updates** is available in the app/tray menu and Settings → Updates. **Notify me about new betas** is optional and off by default; when enabled, metadata is checked at most once in 24 hours. Downloads and installation require a click. Transfer use remains independent of update availability.

macOS uses Sparkle, Windows NetSparkle, and the Linux AppImage a signed atomic replacement. Native Linux packages remain package-manager controlled; AirLink verifies the offered package and shows its installation command. A failed signature, architecture mismatch or failed graceful service stop prevents installation. No private GitHub token is embedded.

**Upgrading from 0.11:** install the appropriate 0.12 beta manually once to move to the new feeds. Keep your saved state. See [update design and verification](docs/UPDATES.md).

## Data and security

| Data | Location |
|---|---|
| macOS app state | `~/Library/Application Support/AirLink` |
| Windows app state | `%APPDATA%\AirLink` |
| Linux app state | `$XDG_CONFIG_HOME/AirLink`, otherwise `~/.config/AirLink` |
| Received files | Actual system Downloads folder or the folder chosen in setup |

State contains private device keys, credentials, settings and transfer metadata. Do not publish it. AirLink grants access only to offered files and configured receives, not the general filesystem. There is no cloud, account requirement, telemetry or automatic router forwarding. Downloading builds/dependencies or explicitly checking for updates requires internet access; local transfers do not.

## Build and test

The common service uses Go 1.27; end users do not need Go. macOS requires Xcode/Swift 6 and Icon Composer. Windows packages require .NET 10, NSIS and 7-Zip. Linux requires CMake, Ninja, C++20, Qt 6.4+ Core/Network/Widgets/DBus/Svg and OpenSSL Crypto. The written SVG/Icon Composer assets are part of the source.

```sh
bash scripts/test.sh
bash scripts/build.sh
# In a Debian 12 x64 packaging environment with rpm/zstd/squashfs-tools:
bash scripts/build-linux-packages.sh
# Collect built native packages on the Mac release machine:
bash scripts/package.sh
```

`build.sh` compiles matching helpers and Windows Setup packages, plus separate native Mac apps when run on macOS. Linux packages are built/tested in the hosted Debian container and distro matrix. Package creation does not publish a release or notarize the app. All automated checks are headless; no desktop application or UI suite is launched.

Read the [architecture](docs/ARCHITECTURE.md), [roadmap](docs/ROADMAP.md), [changelog](CHANGELOG.md), [testing record](docs/TESTING.md), [device checks](docs/DEVICE-CHECK.md) and [release gates](docs/RELEASE-READINESS.md). AirLink is MIT licensed, copyright **Lucas Kleinsteuber**. The active repository is public; the previous repository remains private and archived.
