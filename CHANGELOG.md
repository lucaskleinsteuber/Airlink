# Changelog

AirLink uses MAJOR.MINOR.PATCH versions. Entries describe implemented changes; distribution and verification status are recorded separately.

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
