# AirLink roadmap

This roadmap separates the established 0.1/0.2 baseline, implemented 0.3/0.4 preview capabilities, and future capabilities. Milestones are capability boundaries, not release dates. Version numbers use MAJOR.MINOR.PATCH; patch releases fix the current minor release without claiming a later milestone is complete. Source implementation, final automated verification, private release packaging, and physical acceptance are separate states.

## 0.1 — Local transfer foundation

Implemented: native macOS client, Windows/Linux portable services, embedded browser UI, file/folder transfer, concurrent uploads/downloads, queue, progress/speed, pause/resume, connection recovery, local discovery, QR onboarding, approval-based pairing, persistent trust, per-device rights, TLS, checksums, and local transfer state.

German/English and system/light/dark appearance are available. Finder Services, the fixed ⇧⌘L shortcut, and optional launch at login were delivered early. Their native runtime UX has not yet been verified through an interactive device session; no desktop-controlling UI tests are planned.

Before broad release: finish the documented platform/network/device acceptance checks, validate large single-file behavior separately from aggregate throughput, and establish a signed/notarized macOS distribution.

## 0.2 — Quick Share and explicit clipboard sharing (implemented in source)

- LAN-only read-only share links with optional Argon2id passwords, expiry, download limits, and immediate revocation. UI defaults: 24 hours, no password, unlimited downloads. Certificate trust and fingerprint comparison remain necessary.
- Fragment secrets and share-scoped sessions, with no device pairing required for recipients. Paired browsers can share only their own completed uploads; local control can reference original paths without copying them.
- Count a download session once across files, HTTP Range requests, and resumptions. Opening/unlocking does not consume quota; expiry or revocation stops streaming.
- Explicit manual text clipboard sending and copying, with permission disabled by default for existing and new devices. Desktop peers require permission on both sides. Text is limited to 64 KiB UTF-8, retained only in process memory for up to one hour and 100 messages.

These capabilities reached the private 0.2 development package. Completed observations remain in [TESTING.md](TESTING.md). The 0.1 aggregate stress result remains historical evidence; it was not repeated for 0.2.

## 0.3 — Browser relay and transfer controls (implemented in source)

- Browser-to-browser transfers through one running local desktop host, using at most two active relay file streams and bounded 8 MiB blocks without a full-file disk copy. Relay permission defaults off; both paired browsers need the applicable send/receive rights. Recipient acceptance is mandatory for every offer.
- Explicit transport-delivery states, Range retries, and ZIP fallback, with source tabs/file access still required after restart. No claim that the recipient filesystem saved or verified a file.
- A configurable global shortcut with an explicit Settings key recorder, Command/Control validation, migrated ⇧⌘L default, retained disabled settings, and conflict handling that preserves the previous registration.
- Independent aggregate send/receive limits; filtered combined history with terminal-metadata-only clearing; separate confirmed-file, relay-transport, and lifetime payload counters. Daily history totals group by last activity in UTC.
- Modern/legacy Finder Services input and login status refresh without registration side effects. Menu and quit state include active browser relays.

Consolidated local 0.3 verification and private package creation are complete; see [TESTING.md](TESTING.md). Carbon shortcut registration, Finder behavior, login startup, browser interaction, and physical networking still need runtime evidence; no UI automation is added. A new 100 GiB run has not been performed.

## 0.4 — Optional incoming-file organization (implemented preview)

- Separate duplicate-detection and sorting opt-ins, both off for new and migrated settings, with no silently installed rules. Editable localized examples remain drafts until explicitly saved.
- SHA-256-plus-size duplicate decisions against managed verified received files only, with streaming candidate revalidation and a second check at explicit host Skip. The persistent index survives history clearing; arbitrary user libraries are not scanned.
- Private host Keep/Skip and explicit candidate-path actions. No automatic deletion, overwrite, movement or whole-file source copy; Skip creates no fake received payload. Candidate identities and host paths stay out of guest/peer responses.
- Ordered extension rules for top-level individual files only, immutable per-offer destination snapshots and collision/path validation. Transferred folders stay together; settings changes affect new offers only.
- Updated-source preparation, host-attention states, cached checksum reuse and legacy completion compatibility: HTTP 409 remains pending until a host decision; verified staging can be kept without a connected sender.
- Actual transferred bytes, avoided bytes and relay transport remain separate. Skipped files are excluded from downloads/exports; all-skipped transfers cannot become file shares.

Implementation is present in the private 0.4 preview. Final automated verification, packages and delivery evidence are recorded separately in [TESTING.md](TESTING.md). Manual organization decisions, real filesystem/platform behavior and cross-device acceptance remain part of the 1.0 acceptance work; no desktop-controlling UI tests are added.

## 1.0 — Accepted and maintainable (physical acceptance pending)

Still pending: reach the full agreed platform, reliability, integrity, security, usability, and distribution acceptance bar. Stable release claims require completed evidence, including real cross-device transfers, large single files, interrupted transfers, platform-specific file handling, and macOS distribution checks.

Native Windows, Linux, Android, or iOS UI clients beyond the portable desktop service and browser interface remain possible future work; no delivery date is assigned.
