# Changelog

AirLink uses MAJOR.MINOR.PATCH versions. Entries describe implemented changes; distribution and verification status are recorded separately.

## 0.5.0 — 2026-10-24

- Added explicit native helper retry using the same bundled service and application data directory, plus user-selected receive-directory recovery. An unavailable saved directory no longer requires discarding application state or silently choosing a different destination. Existing transfer destinations remain unchanged.
- Kept the last native status snapshot visible after failure while disabling helper-dependent actions until fresh status arrives. Session identifiers isolate late callbacks and replies from earlier helper processes. Quit uses bounded cleanup stages and reports a helper that remains alive instead of waiting indefinitely or claiming successful shutdown.
- Anchored new desktop source references to filesystem identity, size, and nanosecond modification time. Historical records with cached full hashes are lazily rehashed and compared before reuse; older records without a checksum acquire an identity on first use and a full checksum when needed. This does not add startup library hashing, whole-file staging, or a frozen snapshot of mutable originals.
- Preserved explicit pauses against late reconnect/failure callbacks and committed state changes before updating their visible in-memory state. Failed SQLite writes no longer report a cancellation that disappears after restart.
- Stopped permanent peer protocol, authorization, and identity failures with actionable attention rather than retrying indefinitely. Temporary network errors, pending decisions, and server outages remain resumable. Source payload checks run after limiter waits, and failed ZIP streams abort instead of being finalized as successful archives.
- Preserved files added or reselected during an in-flight native send and prevented duplicate offers from repeated Send actions.
- Made active HTTP work cancellable on pause, cancellation, and relevant permission revocation, including slow request bodies and throttled output. Payload limits retain their shared directional budgets.
- Allowed browser pairing request/poll/approval to continue in memory when session storage reads, writes, removal, or property access fail. Reload recovery remains available when that optional session store works; saved upload integrity evidence still requires IndexedDB before payload transmission.
- Made anonymous share access use the host's expiry/revocation decisions. A recipient clock ahead of the host no longer permanently closes a share after successful authorization. Authoritative removal, expiry, and revocation still stop polling; existing download-session allowances retain their semantics.
- Rejected unsupported portable filenames, literal backslashes, case collisions, and file/parent conflicts before source hashing or offer creation. Names are never silently rewritten; German and English errors explain how to correct them on the source device.
- Added a concrete [release readiness checklist](docs/RELEASE-READINESS.md), separating local background checks from physical platform, large-file, native interaction, certificate onboarding, and distribution evidence.

This is a private stabilization preview, not a 1.0 acceptance claim. Final-source test results, package integrity, migration and any large-file/restart measurements belong in [TESTING.md](docs/TESTING.md) only after their runs finish. Historical checks and account-blocked hosted CI are retained at their actual scope. The MIT license, private repository policy and ad hoc macOS signing policy are unchanged.

## 0.4.0 — 2026-10-24

- Added optional content-based duplicate decisions and ordered extension-to-folder sorting for desktop receives. Both default off with no rules for new and migrated settings; editable example rules never silently enable sorting.
- Limited duplicate indexing to AirLink-managed verified received files, including eligible completed incoming history. Candidate identity, size and full SHA-256 are streamed and revalidated before offering Skip and again at the host's explicit decision. History clearing retains the independent index.
- Added private host Keep/Skip choices and explicit existing-path show/copy actions. Source originals and existing received files are never automatically deleted, overwritten, moved or copied; Skip may remove only this transfer's validated private staging data and never creates a fake received-file placeholder.
- Preserved folder transfers unchanged while sorting only top-level individual files. Incoming offers snapshot their rules and relative destination mappings; later settings changes cannot relocate active or completed data. Guest/peer responses omit host destinations and duplicate candidate IDs/paths.
- Added pre-payload preparation and checking/waiting states, cached source-checksum reuse, pause/cancel generation guards and receive-slot release while awaiting a host decision. Legacy completion returns HTTP 409 while a decision is pending; Keep can publish previously verified staging without a connected sender.
- Added bounded asynchronous private duplicate resolution with correlated out-of-order replies, keeping status and pause/cancel responsive during long candidate checks.
- Separated skipped-file/avoided-byte statistics from actual confirmed data and lifetime traffic. Progress includes avoided bytes; exports exclude skipped payloads, and an all-skipped transfer cannot produce a file share or redirect a download to its existing candidate.
- Extended the browser source workflow and portable host console with explicit opt-ins, validated ordered rules, duplicate attention/decisions and honest skipped-file labels. Background regression coverage includes pending/lost responses, mixed skip/send progress, legacy fallback, path validation and migration; final results belong in [TESTING.md](docs/TESTING.md).

This is an implemented private development preview. Final verification and artifact publication are tracked separately in [TESTING.md](docs/TESTING.md). The MIT license, private repository policy and ad hoc macOS signing policy are unchanged. No installation, GUI acceptance, physical-device proof, notarization, or renewed 100 GiB result is implied by this entry.

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
