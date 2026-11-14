# AirLink architecture — 0.13 (in development)

AirLink is local-first and desktop-only. One Go service owns transfer state and network policy; native macOS, Linux and Windows apps own their private service process and present the same recipient/setup/settings concepts. Normal transfers require no external resources or account.

**0.13 scope change:** the browser/phone interface (React web app, plain-HTTP certificate setup port, certificate downloads, guest status/events), Quick Share links, browser relay, duplicate detection/extension sorting, legacy pairing v1/manual fingerprint approval and the localhost admin console were removed. Clipboard sharing between paired desktops remains. Transfers use [protocol 3](PROTOCOL.md); 0.12 and older cannot connect.

## Processes and transport

- **Go:** one TLS 1.3 port. ALPN `airlink/3` carries mutually authenticated peer sessions (transfers, clipboard); HTTP/1.1 serves only device info and comparison-code pairing. Connections from outside the local network are dropped before TLS. mDNS discovery, a connection manager that keeps every paired device connected, and the transfer engine (`internal/transfer`) with SQLite metadata. Payloads stream to/from the selected files.
- **macOS:** SwiftUI/AppKit, macOS 14+, menu bar, Finder Services, shortcut and login integration. Sparkle handles signed updates through the graceful termination path.
- **Linux:** C++20 and Qt 6 Widgets with a minimum Qt API of 6.4; KDE/Wayland is primary. QProcess pipes, system widgets, native/portal file dialogs, tray and a private single-instance activation socket. Qt Core tests never construct QApplication.
- **Windows:** self-contained WPF app and matched Go helper, native pickers/drop, tray, per-user NSIS Setup and NetSparkle updates. The installer does not require a separately installed .NET runtime.

Private process-pipe RPC frames contain IDs and correlated results/errors. Replies may complete out of order. The public LAN API never gets the local administration rights or private filesystem mappings of this channel. Existing application identifiers, state locations and device identities stay stable across the GitHub account rename.

## Transfer behavior and persistence

Services send directly to each other. A source is referenced in place, never duplicated as a preparation step. A folder retains its relative hierarchy, including empty directories. File/folder name collisions allocate a distinct result instead of overwriting or merging existing content.

Each pair of devices keeps one session; either side may open it and both use it, so a one-sided firewall does not block either direction. Files follow each other on a stream without per-file round trips; several transfers interleave fairly. The sender reads each source once, hashing while it sends, and checks size, modification time and file identity before and after reading. The receiver writes sequentially into a private partial file, verifies every 32-MiB segment with SHA-256 and publishes nothing unverified.

Finished files are synced and published in batches (at most 512 files or 64 MiB, or after 0.7 s) and recorded in one SQLite transaction before the receiver acknowledges them; large files are checkpointed at segment boundaries at most once per second. The receiver is authoritative for progress: after pause, disconnect or restart a new stream (epoch) starts at its last checkpoint. SQLite runs in WAL mode with `synchronous=NORMAL`; a lost, unacknowledged commit only causes data to be sent again.

The store (`transfers-v3.sqlite`) keeps one row per transfer and per manifest entry. Active transfers are held in memory; finished history keeps only headers and three preview entries in memory and loads manifests on demand. On first start, 0.12 history is imported once; unfinished 0.12 transfers are marked failed and must be sent again. Pairings, settings and received files are unchanged.

Historical receive destinations remain authoritative. Changing settings affects new transfers, and removing history affects metadata rather than originals/received files. Transferred folders stay together.

## Responsive control and native state

Current apps poll `status.summary` rather than fetching all history/manifests. The summary is generated directly from engine records with bounded previews and separate active counts. History and file details load in 50-entry pages, with a hard maximum of 100 per request. Full share/clipboard/statistics tooling loads on demand. Legacy full status remains for compatible clients.

Preparation is an asynchronous, bounded job with a replay identity and explicit cancel/status methods. Source walking does not monopolize the serial pipe loop. EOF/shutdown cancels owned operations; a failed graceful stop prevents application replacement.

Each send binds an immutable recipient/source snapshot before the picker/drop submission. Later navigation and source additions/reselection cannot alter it or be removed by its acknowledgment. Settings use a baseline plus a deliberate draft and apply edited fields to the newest saved snapshot. Request/mutation generations reject stale tool results; transfer timestamps prevent a delayed summary from reactivating a newer completed record. Hidden windows defer transfer-list work and refresh when reopened, while services, notifications and tray state continue.

The [0.12 protocol additions](API-0.12.md) define the private methods and TLS comparison-code flow. Earlier API documents describe 0.12 and older; browser, share, relay and organization endpoints no longer exist.

The native About settings offer an explicit local diagnostic export. Its private-pipe report is constructed from an allowlist of version/platform information, a Go heap sample and aggregate transfer/device counts. It contains no file/device names, addresses, private paths, identifiers, content hashes, credentials, clipboard text or raw logs, and is never uploaded automatically. Heap allocation is not total process RSS. Recoverable sender failures expose fixed localized guidance and an explicit retry action; the action retains the same sources, transfer ID and remote checkpoints. Changed sources, integrity failures, cancellation and terminal receiver failures require a new explicit send.

## Trust

Every service has its own persistent local identity. Desktop pairing v2 independently derives an eight-digit code from the original TLS exporter plus both device identities/nonces; users compare it on both screens. Only mutual confirmation grants credentials. Durable handoff receipts handle loss/restart without duplicate devices, while revocation prevents late resurrection. Automatic acceptance is a separate explicit choice for each local receiving direction. Manual address entry uses the same comparison-code flow.

Clipboard reads/copies are explicit and permission-controlled. No remote peer receives general filesystem access.

## Distribution and updates

New betas use `Major.Minor.Patch-beta.N`, with separate numeric OS version/build fields and increasing builds. Separate Mac ARM64/x64 DMGs, Windows ARM64/x64 Setup EXEs and Linux x64 AppImage/DEB/RPM/Arch packages share one release manifest. Linux ARM and an AUR submission are outside this milestone.

Linux native packages use system Qt/OpenSSL. The Debian-12-built AppImage includes a matching shared Qt runtime, required Wayland/X11 plugins and third-party license/relinking material. It retains the host's glibc requirement (2.36+) rather than promising every Linux distribution.

Updates verify the retained Ed25519 trust anchor. Optional automatic checks require consent and run at most daily; installation is explicit. AppImage updates verify before graceful shutdown/readiness and atomic replacement. Package installations show verified packages and the package-manager command. Mac betas are ad hoc signed, not notarized. Detailed formats and publication order are in [UPDATES.md](UPDATES.md).

Builds/package inspection never launch the native GUI. Tests use temporary state and received-file directories. Physical desktop rendering, actual WLAN behavior and a new 100-GB transfer remain separate evidence in [TESTING.md](TESTING.md) and [RELEASE-READINESS.md](RELEASE-READINESS.md).
