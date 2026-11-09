# AirLink

<img src="docs/images/app-icon.png" width="112" height="112" alt="AirLink icon: two linked arcs">

**Large files, shared locally.**

AirLink transfers files and folders between computers and browsers on the same local network. It combines native macOS and Windows apps with a web interface for desktop and mobile browsers. The Linux service is experimental, available from source and untested at runtime; no Linux binary download is published.

**Current version: 0.11.0. Repository status: private development preview.** Source and release downloads require a GitHub account with access to this private repository. Download development builds manually from [Releases](https://github.com/lucaskleinsteuber/Airlink/releases). The source is MIT licensed. Version 0.8 adds guided device naming and receive-folder setup, a clearer desktop interface and direct receive destinations; 0.9.0 adds a per-user Windows Setup installer. Windows runtime and physical-network acceptance remain open. Automated verification, package creation, physical acceptance, and historical results remain separate delivery states recorded in [TESTING.md](docs/TESTING.md). The [release readiness gates](docs/RELEASE-READINESS.md) define the evidence still needed before 1.0; no release date is assigned.

The active repository containing the sanitized branch/tag history is **private** under the owner's current decision. Independent object/history and known old-object route checks passed for this new repository, and the six published 0.8.1 files matched the reviewed local checksums at the migration check. The earlier repository remains a separate **private, archived** repository. Making the active repository public later requires a new owner decision. The completed historical checks and their limits are recorded in [PRIVACY-REVIEW.md](docs/PRIVACY-REVIEW.md).

## The local transfer foundation

- Send files and folders through drag-and-drop or a file picker. Source files stay in place.
- Upload and download concurrently, with a transfer queue, progress, speed, pause/resume, and reconnection from confirmed checkpoints.
- Two incoming and two outgoing slots keep bidirectional transfers moving without queue deadlocks.
- Connect desktop services through local discovery or an address; connect browsers through QR codes and a local address.
- Pair devices with local approval, remember trusted devices, control send/receive permissions, and revoke access.
- Encrypt transfers with TLS 1.3, verify SHA-256 checksums for chunks and completed incoming files, and persist transfer state locally.
- Use German or English and system, light, or dark appearance.
- On Windows, use a native window, file/folder pickers, drag-and-drop, a tray icon, and direct Explorer actions for completed receives. The bundled runtime needs no separate .NET installation.
- On macOS, use a native window and menu bar, Finder **Services → Send with AirLink**, a configurable shortcut with **⇧⌘L** as the migrated default, optional notifications, and optional launch at login.

**Quick Share** provides links with optional passwords, expiry, download limits, and revocation. **Clipboard** exchanges text only after explicit actions between authorized devices. Version 0.3 adds browser-to-browser relay through one local desktop host, global speed limits, richer history/statistics, and configurable native shortcuts.

The complete web interface is embedded in the desktop service. Normal use needs no account, cloud relay, external website, CDN, or internet connection. Downloading dependencies and building from source requires internet access initially.

Version 0.4 adds **optional duplicate decisions and extension-based sorting for new desktop receives**. Both are off by default, including upgrades. They do not organize browser-managed downloads or relay payloads. See the [organization workflow](#optional-incoming-file-organization) and [roadmap](docs/ROADMAP.md).

## Setup, settings and transfer work in 0.11

Setup now has three steps: choose a recognizable name, choose the receive folder, then review both together with notification preferences. The last step can lead directly to adding the next device. Existing pairings, custom receive locations and completed setup remain saved.

Settings have persistent categories instead of a mixed long page: **General**, **Receiving**, **Transfers** and **About**, plus **Shortcuts** on macOS. **Save** and **Discard** stay visible. Switching categories does not throw away a draft. Updates from setup or another settings surface refresh untouched fields; saving applies only deliberate edits to the newest host snapshot. Windows keeps the draft through main navigation and appearance/language rebuilds, and skips visual work while its main window is hidden.

Transfers use exact-sized bounded block readers rather than repeatedly growing temporary buffers. Identical already-persisted sender progress no longer causes a second SQLite write. TLS, per-block and full SHA-256 checks, source identity checks and durable checkpoints remain enabled. Isolated block benchmarks allocate about 50.8% fewer bytes; the complete same-Mac transfers remain around one second for 300 MiB, with only a small timing difference in three-run samples. These measurements do not explain or establish a fix for physical WLAN/Windows throughput. Details are in [TESTING.md](docs/TESTING.md).

## Everyday use in 0.10

**Add a device** starts with computers found on the same network. Choose the other computer, compare the complete fingerprint with its **This device** display, then approve the request there once. Manual address entry remains available if discovery is blocked. Phones and tablets have a separate browser/QR setup path. A device appears under **Trusted devices** after its pairing is actually confirmed.

The selected trusted device shows **Automatically receive from this device** directly, with access to its device settings. This is an explicit local choice and starts off for a new pairing. Enable it on the receiving computer to accept future files from that sender without another approval; the opposite direction is configured separately.

Finished file transfers have **Remove from list**. **Remove finished entries…** captures the completed, failed and cancelled entries present when selected and asks for confirmation. Active transfers and transfers finishing later remain visible. Removal is persistent and removes history metadata only: originals, received files and independent Quick Share sources stay available. History-derived totals change with removed entries; lifetime payload counters remain. An older status reply cannot put a removed entry back in the interface.

## Setup and desktop workflow in 0.8

On first launch, choose a recognizable device name, such as **My MacBook** or **Windows laptop**, and confirm the receive folder. **Downloads** is suggested for a fresh setup. An old conventional `Downloads/AirLink` default is offered as a change to Downloads during setup; an existing custom folder remains selected. Setup is saved only after the explicit completion action succeeds. You can edit the name and folder later in Settings. Saved pairings and history remain available through the update.

The Windows sidebar is resizable, with two-line device names and separate **Devices**, **Transfers** and **Settings** destinations. The main view concentrates on the selected recipient, file drop and current results. Quick Share and Clipboard are under **More tools**; device permissions are behind the device menu.

After pairing, **drop files or folders directly onto a computer in the device list** to send that batch to it. The central drop area names its selected recipient and also starts that batch directly. On Windows, **Choose files** / **Choose folder** starts the picked batch after selection when a suitable desktop recipient is selected. The macOS picker keeps its reviewable prepared selection and Send action. If no recipient is selected, files stay in a prepared selection: choose a computer, then select **Send**. An explicitly selected offline computer keeps its queued offer until it becomes reachable.

An empty, idle draft may choose the sole online paired desktop with receive permission automatically. Discovery never redirects an existing recipient or a pending selection to a different computer. Each submission pins its recipient and selected entry identities. Files added or reselected during preparation remain for a later send; a failed submission retains its sources for an explicit retry. Dropping another batch while preparation is busy produces a visible message rather than silently creating another offer. If an offer reply is lost, retrying the unchanged selection recovers the same transfer instead of creating a second one. Choosing a different recipient or a new selection remains a new send.

The Windows app owns its bundled transfer service. Its window includes discovery, pairing, direct sending, progress, receive approval, duplicate decisions, Quick Share, clipboard text and settings. The active queue is separated from recent terminal transfers, which retain their result actions. Closing the window keeps transfers running in the system tray; choose **Quit** to stop and checkpoint its work. When enabled, tray notifications draw attention to pairing, incoming offers, duplicate choices, text messages and newly completed receives. Action errors remain visible until dismissed or another user action replaces them; a successful status poll does not erase them.

On Windows, double-click a paired desktop to open its file picker. **Ctrl+O** opens file selection inside AirLink, and **Ctrl+Enter** sends an already prepared selection. These are window shortcuts, not new global shortcuts.

On both desktop apps, pairing remains pending until the other device actually approves it. **Automatically receive from this device** is an explicit choice for the computer where it is enabled. Pairing alone does not enable it on either side. You can choose it during pairing, when accepting an incoming transfer, or later in device permissions; the other computer controls its own separate choice. Existing pairings retain their saved permissions.

Completed incoming transfers provide **Show file** / **Open folder** actions for their actual published destinations, including sorting rules and older transfer locations. New receives are published directly in the chosen folder, after verification. A single `report.pdf` arrives as `Downloads/report.pdf`; a name collision gets a distinct name such as `report (2).pdf`. A sent folder retains its own hierarchy, with a unique top-level name on collision rather than merging into an existing folder. Older received files and saved in-progress destinations retain their historical paths. A skipped duplicate has no newly received file and cannot redirect this action to an existing candidate. Empty internal partial directories are removed after a new successful completion; historical folders are not reorganized.

Windows now provides native Quick Share creation/list/revocation and explicit clipboard Paste/Edit/Send/Copy/Dismiss. Browser-relay administration, detailed statistics and the sorting-rule editor still use the existing web or macOS controls; the native interfaces do not yet have full feature parity. Windows runtime acceptance on a physical laptop remains a separate check in [TESTING.md](docs/TESTING.md).

## Manual updates in 0.9

During private development, download newer builds manually from [Releases](https://github.com/lucaskleinsteuber/Airlink/releases), signed in with a GitHub account that has repository access. Install the macOS DMG or Windows Setup using the instructions below; existing application data and received files stay separate from the app.

The apps also contain explicit **Check for Updates…** actions: on macOS in the AirLink application menu and menu-bar panel, and on Windows in Settings and the tray menu. These actions require publicly reachable signed feeds and packages. They cannot access the current private repository anonymously, and AirLink bundles no private GitHub access token. An unavailable feed is not evidence that the installed app is current. Normal local transfers continue to work offline.

When a signed channel is publicly reachable, the updater verifies the feed and downloaded package against an embedded Ed25519 public key. macOS uses Sparkle through its normal quit/checkpoint path; Windows requires graceful helper exit before the verified Setup can run. Public update delivery or a later public repository requires a new owner decision. Earlier anonymous-access checks in [TESTING.md](docs/TESTING.md) are historical observations, not current availability or proof of physical updater installation/relaunch.

## Recovery in 0.5

- The Mac app can explicitly retry a failed helper using the same application data directory. If the saved receive directory is unavailable, choose a replacement through **Choose receive folder and retry…**. AirLink does not silently fall back to another folder; existing transfers retain their planned destinations. The last status snapshot remains visible, but actions that need the helper wait for a fresh connection.
- Helper cleanup has bounded graceful, termination, and final kill stages. If macOS still reports a running helper after cleanup, AirLink reports that failure and keeps the app open. Session identifiers keep late replies from an earlier helper from changing the current connection.
- Desktop sources retain filesystem identity as well as size and modification time. Historical records with a saved full checksum are validated before that checksum is trusted again. Source originals must still remain unchanged; AirLink does not create a frozen copy or promise to detect deliberate in-place edits that also forge the checked metadata.
- Pause, cancellation, and revoked permissions interrupt active HTTP work, including slow or throttled streams. Browser pairing can continue in the active page when session storage is blocked. A recipient's incorrect clock no longer closes an otherwise valid share; the host enforces expiry and revocation. Unsupported cross-platform filenames are rejected before source hashing or upload, with no silent renaming.

These are implementation boundaries, not evidence that every physical device or macOS interaction has passed acceptance. Current verification belongs in [TESTING.md](docs/TESTING.md); the required physical and distribution checks are listed in [RELEASE-READINESS.md](docs/RELEASE-READINESS.md).

## Choose how to run it

| Device | AirLink role |
| --- | --- |
| macOS 14 or later | Native app with a bundled local transfer service; universal Apple Silicon/Intel build |
| Windows 10/11 | Native app with bundled local transfer service and runtime; x64 and ARM64 Setup installers |
| Linux | Experimental source-only service with a local web interface; runtime untested, no published binary |
| Android / iPhone / iPad | Browser client connecting to a running desktop service |

At least one computer must run AirLink. Two paired desktop services communicate directly. Browsers can send to/download from their connected host, or use an explicitly authorized relay to another browser paired with that same host. The desktop service remains running throughout a relay.

Platform builds are distinct from platform runtime verification. The current evidence and remaining checks are recorded in [TESTING.md](docs/TESTING.md).

For a manual check with a Mac, Windows laptop, iPhone and iPad, follow the [German device test guide](docs/DEVICE-CHECK.md). It starts with small test files and checksum round trips before larger transfers on the physical network.

### macOS

1. Download `AirLink-0.11.0-macos-universal.dmg` manually from the private [Releases](https://github.com/lucaskleinsteuber/Airlink/releases), signed in with a GitHub account that has repository access, when that version's asset is available.
2. Quit the existing AirLink app from its menu-bar menu before updating. Open the disk image and drag **AirLink.app** to **Applications**, replacing the previous AirLink app if present.
3. Open AirLink. Keep only the intended current app in `/Applications`; app updates preserve the separate AirLink data directory and received files.
4. Complete the device-name and receive-folder setup. Choose **Connect device** to pair another computer; use connection information/QR to connect a browser.

The current development build is **ad hoc signed and not notarized**. macOS may require explicit approval in **System Settings → Privacy & Security → Open Anyway** after the first attempted launch. Approve only the app obtained from this repository. A Developer ID signed and notarized distribution is a later release requirement.

Closing the window leaves AirLink running in the menu bar. **Quit** checkpoints and stops the service. Notifications and launch at login are optional; allow the corresponding system permissions when you enable them.

### Windows

1. Download **`AirLink-0.11.0-windows-x64-setup.exe`** for an Intel/AMD laptop, or **`AirLink-0.11.0-windows-arm64-setup.exe`** for Windows on ARM, manually from the private [Releases](https://github.com/lucaskleinsteuber/Airlink/releases), signed in with a GitHub account that has repository access. The development preview is not Authenticode signed; approve only the file obtained from this repository.
2. Run Setup. It installs for your account in **`%LOCALAPPDATA%\Programs\AirLink`**, adds **AirLink** to the Start menu and Windows **Installed apps**, and needs no administrator account, ZIP extraction, separate .NET installation or terminal. If Setup asks you to close AirLink, choose **Quit** in the old app's system-tray menu, then retry; closing its window alone keeps it running. Stop a legacy console service with **Ctrl+C** if needed. Open AirLink from the finish page or Start menu.
3. Complete the three-step setup: a recognizable **device name**, the **receive folder**, and the review/notification choices. Downloads is the usual choice. Licenses for AirLink and the bundled runtime are available under **Settings → About → Licenses**.
4. Select the other computer in the sidebar. If it is new, use **Connect device**, compare the fingerprint shown directly on the other computer, and approve the request there. Existing pairings remain saved.
5. Drop a file on the paired computer or its named drop area. New received files appear directly in the chosen folder; use **Show file** / **Open folder** to reach the actual result.

Enable **Automatically receive from this device** in that device's settings only for a sender you trust. This is the local receiver's choice; the other computer controls its own independent receipt policy. Closing the window keeps AirLink in the system tray. Use its menu to reopen the app, open the receive folder or quit.

Run a later Setup to update the installed app. After confirming the installed app opens from the Start menu, you can manually delete an older downloaded portable AirLink EXE. Remove the installed app through **Windows Settings → Apps → Installed apps → AirLink → Uninstall**. Updates and uninstallation preserve `%APPDATA%\AirLink` settings, identity, pairings and history, `%LOCALAPPDATA%\AirLink` runtime caches, received files and other files placed in the installation directory; a directory containing those other files remains. See the [English/German Windows quick start](windows/QUICKSTART.md).

### Linux

Linux is **experimental and untested at runtime**. There is no published Linux binary package. The source and build scripts retain the amd64/arm64 cross-build targets for development; successful compilation does not establish Linux network, filesystem or desktop behavior. Follow [Build from source](#build-from-source) if you want to work on that experimental service. Linux runtime acceptance remains open before downloadable packages can be offered.

### Connect a browser or phone

1. Connect both devices to a local network that permits communication between them.
2. Open the host's certificate setup address, using the macOS app's **Setup** QR code, the Windows app's connection information, or the setup link in the Linux local console.
3. **Compare the certificate's SHA-256 fingerprint with the value displayed directly on the host before trusting it.** The HTTP setup page serves public certificate material and instructions only. Pairing and file transfers use HTTPS.
4. Install and trust that host certificate on the client device, then open the host's HTTPS address. On iPhone/iPad, install the profile in **Settings → General → VPN & Device Management**, then enable its trust under **General → About → Certificate Trust Settings**. Other operating systems use their certificate or browser trust settings; the setup page provides guidance.
5. Request pairing in the browser and approve the request on the host. Incoming file offers also require approval unless that device has auto-accept enabled.

QR codes carry local connection addresses, not automatic authorization. A discovery result or setup page does not replace fingerprint comparison. Revoke device access in AirLink when needed; remove a previously installed host certificate through the client's certificate settings if you no longer want to trust it.

Some guest Wi-Fi networks isolate devices or block multicast discovery. Use the displayed address when discovery is unavailable; the devices still need a reachable local connection. AirLink does not configure router forwarding or provide internet sharing.

## Quick Share

Create a link from files or folders in either desktop app's **Quick Share** panel, or use **Share link** for a prepared selection or eligible completed transfer. Windows provides native source pickers, optional password, expiry presets/custom date, a download limit, full-link copying and revocation. The Linux host console also provides local path selection and share controls. Local shares reference the original files in place and do not copy an entire source file into a staging area.

The default policy is **24-hour expiry, no password, and unlimited downloads**. Set a different expiry, choose no expiry, add an optional password, or set a download limit. Passwords are protected with Argon2id; plaintext passwords are not stored or returned in status. Keep originals available and unchanged while sharing them. The host checks source identity, size, and modification time and rejects mismatches or unavailable sources; it does not create a frozen content snapshot of a mutable original.

Share the full link, including its fragment after `#`. The recipient opens a read-only share page and does not need device pairing. The random link secret is consumed from the fragment and removed from the address bar; it is not placed in an HTTP URL or referrer. Filenames are hidden until the link and any password are authorized. A share session grants access only to that share's files, not to the host's device administration or arbitrary filesystem paths.

The recipient still needs a reachable local network and trust in the host's HTTPS certificate. Complete the same fingerprint comparison and certificate setup described above. Quick Share does not create an internet link or remove the certificate setup requirement.

Opening or unlocking a share does not consume a download. The first successful download body counts **one session**; additional files, HTTP Range requests, and resumptions within that session do not consume additional allowances. A new browser session can count separately. Revocation or expiry blocks further downloads and stops active streams. Folder ZIP downloads retain the browser limitations below.

A paired browser may create a share only from its own completed upload and may revoke its own shares. It cannot submit arbitrary paths on the host. Local host control can share selected original paths or an eligible existing transfer. Share definitions, ownership, revocations, and counters persist across host restarts; clipboard text does not.

## Explicit clipboard text

On macOS, Windows or in the web interface, open **Clipboard**, choose a paired device, explicitly **Paste** or type editable text, and select **Send**. At the receiving device, inspect the message and choose **Copy** to write it to the operating system clipboard. Receiving a message never automatically reads, monitors, synchronizes, or replaces the system clipboard. A selectable-text/manual-paste fallback is available when a browser's clipboard API is unavailable.

Clipboard permission is **off by default**, including devices paired before 0.2. Enable it in the device's permissions. Desktop-to-desktop clipboard exchange requires permission on both hosts. File send/receive permissions and clipboard permission remain separate.

Each text must contain **1 to 65,536 UTF-8 bytes (64 KiB)**. AirLink keeps up to **100 recent text messages for at most one hour in process memory**, with no database persistence. Restarting the host clears those messages; dismiss removes a message, and revoking a device removes its messages. Copying text remains an explicit user action.

## Optional incoming-file organization

Duplicate detection and sorting are independent host settings, both **off by default** for new and migrated installations. They apply only to files received by a desktop AirLink service. Enabling them never moves existing or active files, scans an arbitrary user library, or changes browser download-manager and relay behavior.

Enable duplicate detection to compare **SHA-256 plus size** with an index of AirLink-managed, verified received files. Existing completed incoming history can populate that index; clearing transfer history does not erase the index. A filename match alone is insufficient. AirLink streams and revalidates an existing candidate before presenting it, and checks the chosen candidate again when the host selects Skip. Missing, modified, replaced, or symlink candidates cannot silently discard a new incoming file.

The sending device waits while the **host** chooses:

- **Keep / Receive a new copy:** receive a separate file in this transfer's planned destination. Existing files are never overwritten.
- **Skip / Use existing:** leave the existing file in place and mark this incoming entry skipped, without copying it or creating a placeholder. Showing or copying the existing candidate path is an explicit private host action; the sender receives no candidate IDs, existing paths, or host destination mappings.

If the same content has another filename, it can still be a candidate; equal filenames with different content are not duplicates. Source originals and existing received files are never automatically deleted, rewritten, or moved. An explicit Skip may remove this transfer's private staging file after validation; it never removes the indexed original. Existing directory structure may remain.

Optional sorting uses editable, ordered extension-to-folder rules; the first match wins. Localized Images/Videos/Audio/Documents/Archives examples are editable drafts and enable nothing until you explicitly configure and save them. Destination folders must be safe relative paths. Only **top-level individual files** are classified: transferred folders and their hierarchy stay together unchanged. Rules are snapshotted when each incoming offer is created, so later settings changes affect only new offers. Invalid, conflicting, case-colliding, or unsafe destinations are rejected before receiving; existing destinations are not overwritten.

Updated senders prepare each hashed file before payload transfer. A browser shows host-attention states and resumes from its saved checksums after the decision. Older senders can still reach the same host choice after full-file verification; `complete` returns HTTP 409 while that choice is pending, rather than reporting success. Keep can publish already-verified staging even if the sender disconnects.

Progress combines actual confirmed bytes with avoided bytes, while statistics keep them separate. A skipped entry does **not** represent a newly received file or extra network traffic. Downloads and share exports exclude skipped payloads; a transfer whose files were all skipped cannot create a file share or serve the existing candidate through that transfer.

## Browser-to-browser relay

Pair both browsers with the same running desktop host and grant **Browser relay** permission to both devices. This permission is off by default for new and existing pairings. The sender also needs file-send permission; the recipient needs file-receive permission. Choose the other authorized browser as the target, then explicitly accept the offer in that recipient browser. Each new relay requires recipient acceptance, even if ordinary host uploads have auto-accept enabled. The host can pause, resume, or cancel a relay without accepting on the recipient's behalf.

Keep **both browser tabs open and reachable**, including on phones. The source hashes the selected files before offering them. AirLink forwards bounded 8 MiB blocks through RAM, with at most two active relay file streams; it does not stage the complete relay payload on the host's disk. Original source files stay in place. The existing two-incoming/two-outgoing desktop transfer pools remain separate.

Relay metadata and confirmed range information survive host restart; RAM blocks do not. A source tab that loses file access must reselect the same unchanged files. The recipient may need to resume its browser download or restart a ZIP download after interruption. AirLink does not silently convert the relay into a stored host upload.

SHA-256 protects each block, and a normal full stream checks its full digest before the final bytes are released. Partial-range retries validate blocks and the unchanged source manifest. **Delivered** means bytes were handed to the recipient's browser transport; it does not prove that the browser saved or verified its local file. Saving and completion reporting remain the browser download manager's responsibility. Folder ZIP downloads are streamed and cannot be resumed.

## Speed limits, history, and native controls

Set independent **send** and **receive** limits on the host. Each limit is shared across normal transfers, Quick Share payloads, and browser relay payloads in that direction, rather than being a separate allowance for each transfer. The default is unlimited (`0`); enabled limits range from **1 KiB/s to 1 TiB/s**. macOS settings offer KiB/s and MiB/s input; Windows settings label their configured byte-rate units. Payload counters exclude control/error response bodies.

**History** filters file and relay records by case-insensitive filename/device search, state, direction, and device. Results are newest first, with up to 500 combined records. Clearing history removes terminal metadata only: completed, failed, cancelled, or relay-delivered entries. It preserves originals, received files, shares, clipboard messages, and active work. A paired browser clears only its own history association; another relay participant's history remains visible to that participant.

Statistics separate confirmed file bytes, skipped files/avoided bytes, and unique relay transport bytes/deliveries. Avoided bytes do not inflate confirmed or lifetime payload traffic. Host-only lifetime payload counters remain after history clearing, while totals derived from the remaining history can decrease. **Daily history groups use the record's last activity date in UTC**; they are not a time series of bytes transferred during each calendar day. A transfer resumed on a later day can move its full accumulated history total to that day. Guests see only their own/participant statistics, not host-wide traffic totals.

On macOS, change or disable the global shortcut in **Settings → Shortcuts**. Recording captures a key only while that Settings control has focus; it does not install a global keystroke monitor. Command or Control is required, with optional Shift/Option. Existing settings migrate to ⇧⌘L unless a shortcut was explicitly disabled. A conflicting replacement preserves the previous registration and displays an error; the UI distinguishes the registered shortcut from a pending draft.

Finder Services accepts modern file URLs and the legacy selected-filename format only when the service is invoked. Login-item status refreshes when Settings opens or the app becomes active; reading that status does not register/unregister the login item. Native shortcut registration, Finder interaction, and login startup still require manual runtime acceptance; compiled code and background model tests do not establish that UI behavior.

## Browser behavior with large files

Browser uploads read and hash bounded chunks. If the connection breaks while the page remains available, AirLink reconnects and resumes at the host's confirmed checkpoint. After a reload, browser restart, or loss of file access, choose the same unchanged source files again. AirLink verifies the saved integrity evidence before continuing.

Resume manifests require IndexedDB storage; AirLink cancels a new upload if it cannot save that evidence before sending bytes. Pairing itself does not require session storage while the page remains open, although a blocked session store prevents restoring a pending request after reload. Unsupported portable names, paths that differ only in letter case, and file/parent-directory conflicts must be fixed on the source device; AirLink does not rename them automatically.

Keep mobile browsers in the foreground during large transfers. iOS and Android can suspend background pages; reconnection starts when the page becomes active again. Folder selection depends on the browser's file picker and drag-and-drop support. Empty directories are preserved by native selection and recursive drag-and-drop; ordinary browser folder pickers do not expose empty directories.

Browser downloads are saved by the browser's download manager. Individual downloads support HTTP Range; the browser decides whether and how to resume. Folder ZIP downloads are streamed and cannot be resumed. AirLink reports download initiation separately and does not claim that a browser finished saving or verified a received file.

## Local data and configuration

| Data | Default location |
| --- | --- |
| macOS settings, pairing identity, transfer database | `~/Library/Application Support/AirLink` |
| Windows settings, pairing identity, transfer database | `%APPDATA%\AirLink` |
| Linux settings, pairing identity, transfer database | `$XDG_CONFIG_HOME/AirLink`, or `~/.config/AirLink` |
| Received files | The chosen receive folder directly; Downloads is the initial default. Existing custom/legacy destinations remain saved until changed in setup/settings. |

The private state directory contains the TLS identity, device credentials, settings, SQLite transfer/relay metadata, the verified managed-file duplicate index, durable share definitions/sessions, and lifetime payload counters. Relay payload blocks and clipboard text are not persisted there. Authorized owners can recover share links after restart, so protect this directory as private application data. Keep application state separate from the bundle. Sending and sharing reference selected source files; they do not move, replace, or create a whole-file staging copy of the originals.

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
- .NET **10 SDK** for building the native Windows packages; published packages include the runtime. The Windows UI uses WPF and targets Windows. Cross-publishing checks do not replace a real Windows run.
- NSIS (`makensis`) to compile the Windows Setup installers, and 7-Zip (`7zz` or `7z`) for their read-only package inspection. These are build dependencies on macOS/Linux as well as Windows; users installing AirLink do not need them.
- On macOS, Xcode with Swift 6 and Icon Composer. The verified local toolchain is **Xcode 27 / Swift 6.4**. The Swift package targets macOS 14; icon packaging needs the bundled Icon Composer tools.

Build from the repository root:

```sh
bash scripts/build.sh
bash scripts/test.sh
bash scripts/package.sh
```

`build.sh` installs web dependencies, builds and embeds the web UI, and compiles amd64/arm64 services for macOS, Windows, and the experimental Linux source target. It also publishes self-contained native Windows apps with the corresponding Go helper embedded and compiles their NSIS Setup installers. On macOS it builds the native universal app. `package.sh` collects Windows `-setup.exe` installers, creates a macOS DMG when run on macOS, and writes SHA-256 checksums under `dist/`. Linux cross-build outputs remain development artifacts; no Linux release archive is published. Packaging does not publish a GitHub release or notarize the app.

Set `AIRLINK_GO=/absolute/path/to/go` when Go is not on your PATH and `AIRLINK_DOTNET=/absolute/path/to/dotnet` for a nonstandard .NET SDK location. Use `AIRLINK_NSIS=/absolute/path/to/makensis` and `AIRLINK_7ZIP=/absolute/path/to/7zz` to select the installer compiler and extractor explicitly. Windows builds disable .NET CLI telemetry. To publish the Windows apps and compile their installers after the Go helpers exist, use `bash scripts/build-windows.sh`; to rebuild only Setup after the native payloads exist, use `bash scripts/build-windows-installer.sh`. To rebuild only the macOS app after both macOS helpers exist, use `bash scripts/build-macos.sh`.

All automated tests run in the background. There are no UI automation tests that take over the desktop. The opt-in Windows installer test performs silent installation, upgrade and uninstallation in a disposable Windows user; macOS package inspection does not execute that test. See [testing instructions and verification limits](docs/TESTING.md) for completed-run evidence, the [release readiness gates](docs/RELEASE-READINESS.md), the [architecture](docs/ARCHITECTURE.md), the [base protocol](docs/API.md), the [0.2 additions](docs/API-0.2.md), the [0.3 additions](docs/API-0.3.md), and the [0.4 organization contract](docs/API-0.4.md).

## Versions and license

AirLink uses **MAJOR.MINOR.PATCH** versions. Pre-1.0 milestones advance the minor version; fixes within a milestone advance the patch version. Release tags use `v0.7.0` form. A compiled binary, local package, GitHub release, and public release are separate delivery states.

Copyright © 2026 lucasfd3. Licensed under the [MIT License](LICENSE).
