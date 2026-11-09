# AirLink 0.12.0 Beta 1 implementation record

Target: `0.12.0-beta.1`. This is a work record, not a release or acceptance claim.

## Authorized delivery

- macOS SwiftUI: simpler recipient-focused navigation, three recent results, direct picker sends, grouped settings, optional daily update notices.
- Linux x64: C++20 / Qt 6.4+ Widgets desktop, private Go helper pipes, setup, settings, device/pairing/transfer management, history, shares, clipboard, tray and single instance.
- Linux packages: AppImage, DEB, RPM, Arch package and tracked PKGBUILD. KDE Plasma / Wayland first. No AUR submission or Linux ARM downloads.
- Shared core: compact status and paged history/details, cancellable preparation, TLS-bound eight-digit pairing with mutual confirmation, bounded one-block prefetch.
- macOS ARM64 and x64 DMGs; Windows ARM64 and x64 Setup; signed feeds and manifests with the existing publishing key.
- Public active `lucaskleinsteuber/Airlink` only after privacy/package checks. Archived predecessor stays private/archived. Existing releases remain prereleases; update titles to include Beta without changing tags or assets.
- MIT copyright Lucas Kleinsteuber; GitHub noreply commit email. Canonical links use the current account, existing app/data identities remain stable.
- No GUI tests, browser/Finder automation, application launch or local installation as part of automated checking. Preserve all user state and received files.

## Design

Use system fonts and palette; macOS reference light colors: background #F5F5F7, surface #FFFFFF, separator #E5E5EA, primary #1D1D1F, secondary #6E6E73, accent #007AFF. Dark equivalents come from semantic system colors. Linux uses QPalette, system icons and native widgets rather than hardcoded light colors.

Layout: readable resizable devices sidebar; recipient and one send surface; compact transfer rows; separate transfers/history destination; secondary tools in their own views. Left aligned content, no decorative card dashboard. Native controls, clear focus, reduced motion. Keep device trust decisions and update installation explicit.

## Implementation checkpoints

- [x] Core compact status / paged history and file details; desktop consumers still need final integration.
- [x] Core cancellable preparation and responsive control pipe.
- [x] Core TLS-bound mutual pairing and compatibility; native presentation integration still in progress.
- [ ] Bounded prefetch and comparable measurements.
- [ ] macOS navigation, setup, settings and native model tests.
- [ ] Linux desktop and Qt Core tests.
- [ ] Windows compatibility, pairing and updater tests.
- [ ] Beta versioning, split Mac builds, Linux packages and CI matrix.
- [ ] Signed native updates / AppImage update / package-manager guidance.
- [ ] Source/history/package privacy and license review.
- [ ] Final background test/build/package/signature checks.
- [ ] Git/tag, verified public beta publication and asset checks.

## Evidence and limits

Record actual test/build/benchmark results below as they complete. No new checks have run yet. Physical Mac/Arch WLAN, native window integration and a new 100 GB run require separate evidence. They must not be inferred from builds or same-host tests. The available Mac disk space at planning was insufficient for a two-copy 100 GB fixture; do not fill the user's disk to attempt it.

### Work in progress, 2026-11-10

- Initial complete `go test -race ./...` passed after compact status, preparation, TLS pairing and prefetch changes (server package 53.925 s). The new update package was added later and has its own passing race tests; the final complete suite still must be rerun.
- Targeted pairing tests passed: actual TLS code agreement, one-sided approval never grants credentials, directional opt-in, preserved receipts across helper restart, repeated acknowledgments, failed-persistence rollback, expiry and TLS intermediary mismatch.
- Summary tests use 10,000 history records and 10,000-entry shared manifests, check bounded JSON and complete cursor traversal. Preparation tests check replay keys, changed input conflicts and successful single commit.
- Qt Core tests compiled with Qt 6.12 on macOS and ran with the actual Go helper using temporary state/receive directories. Initial logic/integration suite passed. More regressions are being added.
- The Qt desktop target compiled with warnings treated as errors on macOS. It was not launched. This is a compile check, not Linux runtime or KDE/Wayland evidence.
- Linux native views and signed-feed/download code exist. AppImage handoff now waits for a private readiness reply before quitting. Its Linux-only replacement path still needs Linux execution tests and packaging.
- No app installed, GUI launched, GitHub visibility changed, tag created or release published yet.
