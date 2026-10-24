# AirLink roadmap

This roadmap separates the implemented 0.1 baseline from planned capabilities. Milestones are capability boundaries, not release dates. Version numbers use MAJOR.MINOR.PATCH; patch releases fix the current minor release without claiming a later milestone is complete.

## 0.1 — Local transfer foundation

Implemented: native macOS client, Windows/Linux portable services, embedded browser UI, file/folder transfer, concurrent uploads/downloads, queue, progress/speed, pause/resume, connection recovery, local discovery, QR onboarding, approval-based pairing, persistent trust, per-device rights, TLS, checksums, and local transfer state.

German/English and system/light/dark appearance are available. Finder Services, the fixed ⇧⌘L shortcut, and optional launch at login were delivered early. Their native runtime UX has not yet been verified through an interactive device session; no desktop-controlling UI tests are planned.

Before broad release: finish the documented platform/network/device acceptance checks, validate large single-file behavior separately from aggregate throughput, and establish a signed/notarized macOS distribution.

## 0.2 — Quick Share and explicit clipboard sharing

- LAN-only share links with optional passwords, expiry, download limits, and immediate revocation.
- Password protection using Argon2id; share secrets remain separate from local administration credentials.
- Count a download session once across HTTP Range requests and resumptions, so retries do not consume multiple download allowances.
- Manual text clipboard sending with explicit user action. Automatic clipboard monitoring or synchronization is not included.

## 0.3 — Browser relay and transfer controls

- Phone/browser-to-phone/browser transfers through a running local desktop relay, using bounded buffers without staging a full-file disk copy on that relay.
- A configurable global shortcut, extending the fixed shortcut already present in 0.1.
- Transfer speed limits and richer history/statistics.
- Complete and refine the native Finder and login integration already delivered, based on actual runtime acceptance results.

## 0.4 — Optional organization

- Content-based duplicate identification with a visible choice to show or skip matching files. Never automatically delete existing or original files.
- Opt-in sorting of received files, disabled by default. Sending must continue to preserve the source files and their locations.

## 1.0 — Accepted and maintainable

Reach the full agreed platform, reliability, integrity, security, usability, and distribution acceptance bar. Stable release claims require completed evidence, including real cross-device transfers, large single files, interrupted transfers, platform-specific file handling, and macOS distribution checks.

Native Windows, Linux, Android, or iOS UI clients beyond the portable desktop service and browser interface remain possible future work; no delivery date is assigned.
