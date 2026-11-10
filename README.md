# AirLink

<img src="docs/images/app-icon.png" width="112" height="112" alt="AirLink icon: two linked arcs">

**Large files, shared locally.**

AirLink sends files and folders directly between devices on the same local network. Native apps for **macOS, Linux and Windows** manage an embedded transfer service; phones and tablets connect through its web interface. No account, cloud relay or internet connection is needed for local transfers.

**0.12.0-beta.1 is being prepared.** Packages are published only after the candidate passes its background checks. This is beta software: physical Mac↔Arch WLAN, desktop rendering and the new 100-GB acceptance run remain separate, open checks. [Verification records](docs/TESTING.md) distinguish tests, builds, packages and physical devices; [1.0 readiness](docs/RELEASE-READINESS.md) remains a separate gate.

## Get AirLink

Use the packages from the matching [GitHub beta release](https://github.com/lucaskleinsteuber/Airlink/releases). All applications in a transfer should use the same beta when testing the new comparison-code flow.

| System | Package | Requirements and limits |
|---|---|---|
| Apple Silicon Mac | `AirLink-0.12.0-beta.1-macos-arm64.dmg` | macOS 14 or later; ad hoc signed, not notarized |
| Intel Mac | `AirLink-0.12.0-beta.1-macos-x64.dmg` | macOS 14 or later; separate x64 executable and helper |
| Linux x64, portable/user installation | `AirLink-0.12.0-beta.1-linux-x64.AppImage` | glibc 2.36+; bundled Qt and Wayland/X11 plugins |
| Debian / Ubuntu x64 | `AirLink-0.12.0-beta.1-linux-x64.deb` | Debian 12 / Ubuntu 24.04 or later; system Qt 6.4+ |
| Fedora / openSUSE x64 | `AirLink-0.12.0-beta.1-linux-x64.rpm` | Compatible supported releases; system Qt 6.4+ and OpenSSL 3 |
| Arch x64 | `AirLink-0.12.0-beta.1-linux-x64.pkg.tar.zst` | Current Arch; Qt 6 and Wayland packages |
| Windows Intel/AMD | `AirLink-0.12.0-beta.1-windows-x64-setup.exe` | Self-contained per-user installer |
| Windows on ARM | `AirLink-0.12.0-beta.1-windows-arm64-setup.exe` | Native ARM64 app and service |

The release includes `SHA256SUMS.txt`, `UPDATE-SIGNATURES.json`, its detached signature and signed update feeds. Verify the downloaded package before installing. Linux ARM and an AUR submission are outside this beta.

### macOS

Open the DMG and drag **AirLink.app** into **Applications**, replacing the existing app. Keep only the current `/Applications/AirLink.app`; do not store old app bundles there. Application data, pairings and received files stay separate and are preserved.

These betas are **ad hoc signed and not notarized**. macOS may require an explicit first-open approval through **System Settings → Privacy & Security → Open Anyway**. Approve only the package you obtained and verified from this repository; do not disable Gatekeeper globally.

### Linux

For the AppImage, make it executable and open it. Its **Add to application menu** action copies it into the user application directory and adds a launcher. After starting the installed copy successfully, the original download can be removed. A desktop session with a private `XDG_RUNTIME_DIR` is required. On systems without usable FUSE support, the AppImage runtime also supports `--appimage-extract-and-run`.

Install the native package using its package manager:

```sh
sudo pacman -U ./AirLink-0.12.0-beta.1-linux-x64.pkg.tar.zst
sudo apt install ./AirLink-0.12.0-beta.1-linux-x64.deb
sudo dnf install ./AirLink-0.12.0-beta.1-linux-x64.rpm
# openSUSE: sudo zypper install ./AirLink-0.12.0-beta.1-linux-x64.rpm
```

Use only the command for your distribution. The [PKGBUILD](linux/packaging/PKGBUILD) also builds the app from the checked-out source. Packages install the desktop launcher, app and private helper; they do not install a system-wide network service. AirLink runs under your user account and keeps existing Linux service data.

### Windows

Run the Setup EXE for your architecture. AirLink installs under `%LOCALAPPDATA%\Programs\AirLink`, with a Start-menu entry and an entry in Installed apps. The app does not need to stay in Downloads, and no separate .NET installation is needed. Betas are not Authenticode signed. Quit AirLink through its tray before replacing an installed app; received files and saved pairings are preserved.

## First transfer

1. Open AirLink on both computers. Setup asks for a recognizable **device name**, your actual **Downloads folder** or another receive folder, and optional notifications/update notices.
2. Choose **Add device**, select the other computer, compare the **eight-digit code directly on both displays**, and confirm on both computers.
3. Select the trusted device in the sidebar and drop files or folders onto the send area, or choose them with the picker. Without a selected recipient, files stay prepared until you choose one and send.
4. Accept the incoming offer. Optionally enable **Automatically accept files from this device** on that receiving computer. The opposite direction has its own independent choice.
5. Follow progress, speed and remaining time. **Show file / Open folder** opens the verified received result.

Files arrive directly in the selected receive folder. A transferred folder retains its hierarchy and empty subfolders. Name collisions receive a distinct name; existing files are never silently overwritten. Source files stay in place. Symbolic links are skipped visibly.

Pause and reconnection resume from confirmed blocks. Source changes, missing write access and unavailable devices produce an actionable error. Removing a finished result from the list removes its history entry and preserves the files. Closing the main window keeps transfers active in the menu bar/tray; **Quit** saves state and stops the owned service.

An older desktop can use the explicit manual address/full-fingerprint flow under other connection methods. AirLink never silently switches the comparison-code flow to an unverified connection.

## Features and platform limits

| Capability | Desktop apps | Browser |
|---|---|---|
| Files, folders, drag-and-drop | Native pickers/drop; direct service-to-service TLS | Upload to / download from the selected desktop host |
| Pause, reconnect and persisted queue | Confirmed blocks survive service restarts | Upload reselection may be required after reload; browser downloads use the download manager |
| Integrity and encryption | TLS 1.3, per-block SHA-256 and full-file verification | HTTPS, upload block hashes, incremental worker hashing |
| Trusted devices and rights | Discovery, code comparison, saved identities and revocation | Separate certificate/QR setup and host approval |
| Quick Share | Local links, password, expiry, session download limit and revocation | Open authorized links while source host/files remain available |
| Clipboard | Explicit paste/send/copy; per-device permission | Explicit text actions; no background clipboard monitoring |
| History and organization | Paged history/details; optional duplicate decisions and extension rules | Host rules do not organize browser-managed downloads |
| Offline use | Transfers, discovery and paired identities work locally | Host-provided web assets require no CDN or account |

Linux follows system widgets and colors, with KDE/Wayland as the primary desktop target. macOS uses SwiftUI/AppKit, menu-bar operation, Finder Services, configurable shortcut and optional launch at login. Windows has native WPF controls and tray operation. The web interface provides the complete host tooling; some advanced Windows tools remain web-based.

**Phones/tablets:** at least one running desktop host is required. Open its browser setup QR/address, compare the host certificate identity, install/trust that host's local certificate as instructed, and approve pairing on the host. iOS/iPadOS also require enabling full certificate trust. Keep the browser open during active mobile uploads. A browser cannot independently discover LAN services. ZIP folder downloads are streamed and require a restart after interruption; individual downloads support HTTP Range where the browser/download manager uses it. Browser relay connects two approved browsers through a desktop host with bounded buffers and no full relay copy on disk.

**Organization:** duplicate detection and sorting are opt-in and off by default. Duplicate decisions never delete existing files. Sorting applies to new individual desktop receives; transferred folders stay together. Existing transfer destinations do not change when receive settings change.

## Settings and updates

Settings are grouped into **General, Receiving, Transfers, Updates and About**, plus **Shortcuts** on macOS. Save/Discard stays visible. Navigation and status refresh preserve intentional edits; untouched fields follow newer saved settings.

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

State contains private device keys, credentials, settings and transfer/share metadata. Do not publish it. AirLink grants access only to offered files and configured receives, not the general filesystem. There is no cloud, account requirement, telemetry or automatic router forwarding. Downloading builds/dependencies or explicitly checking for updates requires internet access; local transfers do not.

## Build and test

The common service uses Go 1.27. The web bundle uses Node 24 and is embedded in the service; end users need neither Go nor Node. macOS requires Xcode/Swift 6 and Icon Composer. Windows packages require .NET 10, NSIS and 7-Zip. Linux requires CMake, Ninja, C++20, Qt 6.4+ Core/Network/Widgets/DBus/Svg and OpenSSL Crypto. The written SVG/Icon Composer assets are part of the source.

```sh
bash scripts/test.sh
bash scripts/build.sh
# In a Debian 12 x64 packaging environment with rpm/zstd/squashfs-tools:
bash scripts/build-linux-packages.sh
# Collect built native packages on the Mac release machine:
bash scripts/package.sh
```

`build.sh` compiles matching helpers and Windows Setup packages, plus separate native Mac apps when run on macOS. Linux packages are built/tested in the hosted Debian container and distro matrix. Package creation does not publish a release or notarize the app. All automated checks are headless; no desktop application or UI suite is launched.

Read the [architecture](docs/ARCHITECTURE.md), [roadmap](docs/ROADMAP.md), [changelog](CHANGELOG.md), [testing record](docs/TESTING.md), [device checks](docs/DEVICE-CHECK.md) and [release gates](docs/RELEASE-READINESS.md). AirLink is MIT licensed, copyright **Lucas Kleinsteuber**. The previous repository remains private and archived; the active repository's publication follows the owner-approved candidate review.
