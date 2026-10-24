# AirLink

<img src="docs/images/app-icon.png" width="112" height="112" alt="AirLink icon: two linked arcs">

**Large files, shared locally.**

AirLink transfers files and folders between computers and browsers on the same local network. It combines a native macOS app with a portable Windows/Linux service and a web interface for macOS, Windows, Linux, Android, and iOS.

**Current version: 0.1.0. Repository status: private development.** Source and release downloads are available only to people with access to this repository. The source is MIT licensed; repository visibility remains private.

## What works in 0.1.0

- Send files and folders through drag-and-drop or a file picker. Source files stay in place.
- Upload and download concurrently, with a transfer queue, progress, speed, pause/resume, and reconnection from confirmed checkpoints.
- Two incoming and two outgoing slots keep bidirectional transfers moving without queue deadlocks.
- Connect desktop services through local discovery or an address; connect browsers through QR codes and a local address.
- Pair devices with local approval, remember trusted devices, control send/receive permissions, and revoke access.
- Encrypt transfers with TLS 1.3, verify SHA-256 checksums for chunks and completed incoming files, and persist transfer state locally.
- Use German or English and system, light, or dark appearance.
- On macOS, use a native window and menu bar, Finder **Services → Send with AirLink**, the fixed **⇧⌘L** shortcut, optional notifications, and optional launch at login.

The complete web interface is embedded in the desktop service. Normal use needs no account, cloud relay, external website, CDN, or internet connection. Downloading dependencies and building from source requires internet access initially.

Quick Share links, clipboard transfer, phone-to-phone relay, configurable shortcuts, speed limits, richer statistics, content-based duplicate handling, and automatic sorting are planned additions. See the [roadmap](docs/ROADMAP.md).

## Choose how to run it

| Device | AirLink role |
| --- | --- |
| macOS 14 or later | Native app with a bundled local transfer service; universal Apple Silicon/Intel build |
| Windows | Portable service with a private local administration page; amd64 and arm64 builds |
| Linux | Portable service with a private local administration page; amd64 and arm64 builds |
| Android / iPhone / iPad | Browser client connecting to a running desktop service |

At least one computer must run AirLink. Two paired desktop services communicate directly. Browser clients send to and download from their connected desktop host. Browser-to-browser relay is not implemented in 0.1.0.

Platform builds are distinct from platform runtime verification. The current evidence and remaining checks are recorded in [TESTING.md](docs/TESTING.md).

### macOS

1. Download `AirLink-0.1.0-macos-universal.dmg` from the repository's [Releases](https://github.com/lucasfd3/Airlink/releases) when an asset is available.
2. Open the disk image and drag **AirLink.app** to **Applications**, replacing the previous AirLink app if present.
3. Open AirLink. Keep only the intended current app in `/Applications`; app updates preserve the separate AirLink data directory and received files.
4. Open the QR button to connect a browser, or **Connect device** to pair another computer.

The current private build is **ad hoc signed and not notarized**. macOS may require explicit approval in **System Settings → Privacy & Security → Open Anyway** after the first attempted launch. Approve only the app obtained from this repository. A Developer ID signed and notarized distribution is a later release requirement.

Closing the window leaves AirLink running in the menu bar. **Quit** checkpoints and stops the service. Notifications and launch at login are optional; allow the corresponding system permissions when you enable them.

### Windows

Extract the ZIP for your computer's architecture and run the service from PowerShell:

```powershell
.\airlink.exe
```

Keep that terminal open. AirLink prints its HTTPS addresses, certificate fingerprint, and a **private local console** URL. Open the complete private console URL on the same computer to manage pairing, incoming transfers, and local settings. Stop the service with **Ctrl+C**.

### Linux

Extract the archive for your computer's architecture and run:

```sh
chmod +x airlink
./airlink
```

Keep the terminal open and use the printed private local console URL on that computer. Stop with **Ctrl+C**. Desktop computers can pair by entering the other host's HTTPS address and comparing its certificate fingerprint directly on both computers.

### Connect a browser or phone

1. Connect both devices to a local network that permits communication between them.
2. Open the host's certificate setup address, using the native app's **Setup** QR code or the setup link in the local console.
3. **Compare the certificate's SHA-256 fingerprint with the value displayed directly on the host before trusting it.** The HTTP setup page serves public certificate material and instructions only. Pairing and file transfers use HTTPS.
4. Install and trust that host certificate on the client device, then open the host's HTTPS address. On iPhone/iPad, install the profile in **Settings → General → VPN & Device Management**, then enable its trust under **General → About → Certificate Trust Settings**. Other operating systems use their certificate or browser trust settings; the setup page provides guidance.
5. Request pairing in the browser and approve the request on the host. Incoming file offers also require approval unless that device has auto-accept enabled.

QR codes carry local connection addresses, not automatic authorization. A discovery result or setup page does not replace fingerprint comparison. Revoke device access in AirLink when needed; remove a previously installed host certificate through the client's certificate settings if you no longer want to trust it.

Some guest Wi-Fi networks isolate devices or block multicast discovery. Use the displayed address when discovery is unavailable; the devices still need a reachable local connection. AirLink does not configure router forwarding or provide internet sharing.

## Browser behavior with large files

Browser uploads read and hash bounded chunks. If the connection breaks while the page remains available, AirLink reconnects and resumes at the host's confirmed checkpoint. After a reload, browser restart, or loss of file access, choose the same unchanged source files again. AirLink verifies the saved integrity evidence before continuing.

Keep mobile browsers in the foreground during large transfers. iOS and Android can suspend background pages; reconnection starts when the page becomes active again. Folder selection depends on the browser's file picker and drag-and-drop support. Empty directories are preserved by native selection and recursive drag-and-drop; ordinary browser folder pickers do not expose empty directories.

Browser downloads are saved by the browser's download manager. Individual downloads support HTTP Range; the browser decides whether and how to resume. Folder ZIP downloads are streamed and cannot be resumed. AirLink reports download initiation separately and does not claim that a browser finished saving or verified a received file.

## Local data and configuration

| Data | Default location |
| --- | --- |
| macOS settings, pairing identity, transfer database | `~/Library/Application Support/AirLink` |
| Windows settings, pairing identity, transfer database | `%APPDATA%\AirLink` |
| Linux settings, pairing identity, transfer database | `$XDG_CONFIG_HOME/AirLink`, or `~/.config/AirLink` |
| Received files | `~/Downloads/AirLink`, configurable locally |

The private state directory contains the TLS identity, device credentials, settings, and SQLite transfer checkpoints. Keep it separate from the application bundle. Sending references selected source files; it does not move, replace, or create a whole-file staging copy of the originals.

Portable service options:

```sh
./airlink --data-dir /absolute/private/state --receive-dir /absolute/received
./airlink --port 43871 --no-discovery
./airlink --version
```

`--receive-dir` explicitly overrides the saved destination; omit it to preserve the stored setting. The default HTTPS port is `43871`, with an available-port fallback if it is occupied. Use the addresses AirLink actually displays. The private administration console is bound to loopback and uses a per-run secret URL; keep that URL on the host.

## Build from source

Development dependencies:

- Go **1.27** or later; local verification used **1.27.3**.
- Node.js **24** and npm; dependencies are locked in `web/package-lock.json`.
- Python 3 for embedding the built web assets.
- On macOS, Xcode with Swift 6 and Icon Composer. The verified local toolchain is **Xcode 27 / Swift 6.4**. The Swift package targets macOS 14; icon packaging needs the bundled Icon Composer tools.

Build from the repository root:

```sh
bash scripts/build.sh
bash scripts/test.sh
bash scripts/package.sh
```

`build.sh` installs web dependencies, builds and embeds the web UI, and compiles amd64/arm64 services for macOS, Windows, and Linux. On macOS it also builds the native universal app. `package.sh` creates Windows ZIPs, Linux archives, a macOS DMG when run on macOS, and SHA-256 checksums under `dist/`. Packaging does not publish a GitHub release or notarize the app.

Set `AIRLINK_GO=/absolute/path/to/go` when Go is not on your PATH. To rebuild only the macOS app after both macOS helpers exist, use `bash scripts/build-macos.sh`.

All automated tests run in the background. There are no UI automation tests that take over the desktop. See [testing instructions and verification limits](docs/TESTING.md), the [architecture](docs/ARCHITECTURE.md), and the [protocol reference](docs/API.md).

## Versions and license

AirLink uses **MAJOR.MINOR.PATCH** versions. New pre-1.0 capabilities advance the minor version; fixes advance the patch version. Release tags use `v0.1.0` form. A compiled binary, local package, GitHub release, and public release are separate delivery states.

Copyright © 2026 lucasfd3. Licensed under the [MIT License](LICENSE).
