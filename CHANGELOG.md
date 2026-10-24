# Changelog

AirLink uses MAJOR.MINOR.PATCH versions. Entries describe implemented changes; distribution and verification status are recorded separately.

## 0.3.0 — 2026-10-24

- Added browser-to-browser relay through a running local desktop host, bounded to two active file streams using 8 MiB blocks without persisting a full relay payload. Both source and recipient remain required after interruption; metadata alone cannot reproduce the data.
- Added a separate browser-relay permission, disabled for new and migrated pairings. Sender file-send and recipient file-receive rights are also required; every new relay requires explicit recipient acceptance. Host controls do not accept on a recipient's behalf.
- Kept relay transport delivery separate from saved-file completion. Added checksummed blocks/full normal streams, Range retries, and streamed ZIP fallback with no resumable-archive promise.
- Added independent aggregate send/receive payload limits, default unlimited, across direct transfers, shares, and relays; added host-only lifetime payload counters that exclude control/error response bodies.
- Added combined transfer/relay history search and state/direction/device filters, bounded to 500 results. Clearing removes terminal metadata only and respects guest participant scope; files, originals, shares, active work, and lifetime counters remain.
- Added file/relay statistics and UTC history groups based on last activity, rather than claiming actual per-calendar-day traffic accounting.
- Added a configurable native Carbon shortcut with an explicit local Settings recorder, validation, disable support, migrated ⇧⌘L default, and conflict reporting that retains the previous registration. Improved user-invoked Finder legacy path handling and read-only login-item status refresh.
- Added native history/statistics and relay oversight sheets, relay-aware menu/quit state, additive Codable models, 64-bit and migration tests, and 0.3 helper contract checks.
- Hardened share expiry/revocation checks after rate-limit waits and kept HTTP error bodies outside payload metering; final regression and package results are recorded in [TESTING.md](docs/TESTING.md).

The 0.3 development preview has completed local verification and package creation. Distribution remains private, with an ad hoc signed macOS DMG and Windows/Linux archives. No GUI launch, installation, physical device acceptance, notarization, or new 100 GiB run is implied.

## 0.2.0 — 2026-10-24

- Added LAN-only Quick Share creation from local original file/folder references and eligible existing transfers. Paired browsers are restricted to their own completed uploads and own shares.
- Added optional Argon2id password protection, expiry, download limits, persistent ownership/counters, and revocation. UI defaults are 24 hours, no password, and unlimited downloads.
- Added anonymous read-only share pages with fragment secrets, share-scoped cookies, and hidden filenames before authorization. Recipients still need the host's local certificate trust.
- Counted one allowance per download session across files, Range requests, and resumptions. Viewing/unlocking consumes no allowance; expiry or revocation stops further streaming.
- Added explicit clipboard Paste/Edit/Send/Copy/Dismiss controls, separate per-device permission defaulting to false for new and existing devices, a 64 KiB UTF-8 text limit, and an in-memory 100-message / one-hour retention bound. Desktop peers require permission on both hosts.
- Added native Quick Share policy/list/link/QR sheets, clipboard composer/inbox, and dynamic version labels while preserving the existing written Icon Composer assets.
- Added backward-compatible native decoding and regression tests for clipboard permission, optional status fields, UTF-8 limits, share source selection, 64-bit quotas, expiry, and quota state. Final helper, web, service, packaging, and hosted CI results are recorded in [TESTING.md](docs/TESTING.md).

The private 0.2 development package used ad hoc macOS signing. Its completed local checks and packaging observations are retained in [TESTING.md](docs/TESTING.md). No GUI launch, installation, notarization, or renewed 100 GiB acceptance result was claimed.

## 0.1.0 — 2026-10-24

Initial private development baseline.

- Added a portable Go transfer service for macOS, Windows, and Linux, with an embedded React/TypeScript browser interface for desktop and mobile devices.
- Added file/folder transfers, concurrent sending and receiving, a queue, progress and speed, manual pause/resume, checkpoint recovery, and automatic reconnection.
- Added TLS 1.3, per-chunk and full-file SHA-256 verification, durable SQLite transfer state, source-file validation, and publication after successful integrity verification.
- Added local device discovery, browser QR onboarding, certificate setup, approval-based pairing, persistent trust, per-device permissions, and revocation.
- Added a native macOS 14+ SwiftUI app, menu bar access, receive-folder settings, German/English text, light/dark/system appearance, and opt-in notification authorization.
- Delivered Finder Services, a fixed ⇧⌘L send shortcut, and optional launch at login ahead of their originally planned integration milestone. Configurable global shortcuts remain planned.
- Added an original code-authored SVG / Icon Composer app icon and universal arm64/x86_64 app packaging.
- Added background-only Go, web, and Swift tests and build/package scripts. Hosted CI results and physical device behavior must be verified independently.

The private macOS package is ad hoc signed, not Developer ID signed or notarized. Quick Share, clipboard transfer, browser-to-browser relay, speed limits, richer statistics, duplicate handling, and automatic sorting are not part of this version.
