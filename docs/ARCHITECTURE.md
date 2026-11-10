# AirLink architecture — 0.12 beta

AirLink is local-first. One Go service owns transfer state and network policy; native macOS, Linux and Windows apps own their private service process and present the same recipient/setup/settings concepts. The React/TypeScript browser interface is compiled into the service. Normal transfers require no external resources or account.

## Processes and transport

- **Go:** HTTPS/TLS 1.3 REST and SSE under `/api/v1`, mDNS discovery, peer trust, bounded transfer slots, persistent queue/checkpoints, checksums, shares, clipboard and relay policy. SQLite stores metadata; payloads stream to/from selected files.
- **macOS:** SwiftUI/AppKit, macOS 14+, menu bar, Finder Services, shortcut and login integration. Sparkle handles signed updates through the graceful termination path.
- **Linux:** C++20 and Qt 6 Widgets with a minimum Qt API of 6.4; KDE/Wayland is primary. QProcess pipes, system widgets, native/portal file dialogs, tray and a private single-instance activation socket. Qt Core tests never construct QApplication.
- **Windows:** self-contained WPF app and matched Go helper, native pickers/drop, tray, per-user NSIS Setup and NetSparkle updates. The installer does not require a separately installed .NET runtime.
- **Browser:** connects to an approved host, uploads/downloads offered content and reconnects with the browser's actual capabilities. It performs no independent LAN discovery. iOS/iPadOS certificate trust and foreground-transfer restrictions are explicit.

Private process-pipe RPC frames contain IDs and correlated results/errors. Replies may complete out of order. The public LAN API never gets the local administration rights or private filesystem mappings of this channel. Existing application identifiers, state locations and device identities stay stable across the GitHub account rename.

## Transfer behavior and persistence

Services send directly to each other; browsers send to/download from their chosen host. A source is referenced in place, never duplicated as a preparation step. A folder retains its relative hierarchy, including empty directories. File/folder name collisions allocate a distinct result instead of overwriting or merging existing content.

There are two outgoing and two incoming file slots by default. Blocks are at most 8 MiB, with a shorter final block. Each active sender can prepare at most one additional block while the current HTTP request is in flight. Mutable request buffers are never pooled or reused by a later request.

Per-block SHA-256, full-source identity/content checks, durable receipts and final full-file verification remain enabled. Acknowledged data is synced before progress is treated as durable. Repeated requests are idempotent; pause, disconnect and restart resume from confirmed blocks. Partial data stays privately marked until safe publication. Missing/changed sources and destination failures cannot publish a corrupt result.

Historical receive destinations remain authoritative. Changing settings affects new transfers, and removing history affects metadata rather than originals/received files. Only verified managed receives enter the duplicate index. Optional duplicate detection and extension sorting start off; Skip never deletes an indexed existing file, and transferred folders stay together.

## Responsive control and native state

Current apps poll `status.summary` rather than fetching all history/manifests. The summary is generated directly from engine records with bounded previews and separate active counts. History and file details load in 50-entry pages, with a hard maximum of 100 per request. Full share/clipboard/statistics tooling loads on demand. Legacy full status remains for compatible clients.

Preparation is an asynchronous, bounded job with a replay identity and explicit cancel/status methods. Source walking, Quick Share capture and duplicate validation do not monopolize the serial pipe loop. EOF/shutdown cancels owned operations; a failed graceful stop prevents application replacement.

Each send binds an immutable recipient/source snapshot before the picker/drop submission. Later navigation and source additions/reselection cannot alter it or be removed by its acknowledgment. Settings use a baseline plus a deliberate draft and apply edited fields to the newest saved snapshot. Request/mutation generations reject stale tool results; transfer timestamps prevent a delayed summary from reactivating a newer completed record. Hidden windows defer transfer-list work and refresh when reopened, while services, notifications and tray state continue.

The [0.12 protocol additions](API-0.12.md) define the private methods and TLS comparison-code flow. Earlier [API](API.md), [shares/clipboard](API-0.2.md), [relay/history/limits](API-0.3.md) and [organization](API-0.4.md) contracts remain available.

## Trust and shared content

Every service has its own persistent local identity. Desktop pairing v2 independently derives an eight-digit code from the original TLS exporter plus both device identities/nonces; users compare it on both screens. Only mutual confirmation grants credentials. Durable handoff receipts handle loss/restart without duplicate devices, while revocation prevents late resurrection. Automatic acceptance is a separate explicit choice for each local receiving direction. Old clients use the explicitly verified full-fingerprint path.

Browsers follow per-host certificate trust and approval. They receive only scoped cookies/permissions and offered content. Quick Share uses fragment secrets, suitable password hashes, expiry/revocation and limits counted per download session rather than per Range request. Clipboard reads/copies are explicit and permission-controlled. Browser relay uses bounded streams and does not make a full extra payload copy on the host. No remote peer receives general filesystem access.

## Distribution and updates

New betas use `Major.Minor.Patch-beta.N`, with separate numeric OS version/build fields and increasing builds. Separate Mac ARM64/x64 DMGs, Windows ARM64/x64 Setup EXEs and Linux x64 AppImage/DEB/RPM/Arch packages share one release manifest. Linux ARM and an AUR submission are outside this milestone.

Linux native packages use system Qt/OpenSSL. The Debian-12-built AppImage includes a matching shared Qt runtime, required Wayland/X11 plugins and third-party license/relinking material. It retains the host's glibc requirement (2.36+) rather than promising every Linux distribution.

Updates verify the retained Ed25519 trust anchor. Optional automatic checks require consent and run at most daily; installation is explicit. AppImage updates verify before graceful shutdown/readiness and atomic replacement. Package installations show verified packages and the package-manager command. Mac betas are ad hoc signed, not notarized. Detailed formats and publication order are in [UPDATES.md](UPDATES.md).

Builds/package inspection never launch the native GUI. Tests use temporary state and received-file directories. Physical desktop rendering, actual WLAN behavior and a new 100-GB transfer remain separate evidence in [TESTING.md](TESTING.md) and [RELEASE-READINESS.md](RELEASE-READINESS.md).
