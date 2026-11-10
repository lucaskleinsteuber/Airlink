# AirLink 0.12.0 Beta 1 implementation record

Target: `0.12.0-beta.1`. This is a work record, not a release or acceptance claim.

Repository workflow: the owner requested a single branch. Continue directly on
`main`; the temporary beta branch is merged and removed. Tags retain released
versions. Do not create another development branch for this work.

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
- [x] Bounded prefetch and comparable measurements.
- [x] macOS navigation, setup, settings and native model tests.
- [x] Linux desktop and Qt Core tests.
- [x] Windows compatibility, pairing and updater tests.
- [x] Beta versioning, split Mac builds, Linux packages and CI matrix.
- [ ] Signed native updates / AppImage update / package-manager guidance.
- [ ] Source/history/package privacy and license review.
- [ ] Final background test/build/package/signature checks.
- [ ] Git/tag, verified public beta publication and asset checks.

## Evidence and limits

Record actual test/build/benchmark results below as they complete. Physical Mac/Arch WLAN, native window integration and a new 100 GB run require separate evidence. They must not be inferred from builds or same-host tests. The available Mac disk space at planning was insufficient for a two-copy 100 GB fixture; do not fill the user's disk to attempt it.

### Work in progress, 2026-11-10

- Initial complete `go test -race ./...` passed after compact status, preparation, TLS pairing and prefetch changes (server package 53.925 s). The new update package was added later and has its own passing race tests; the final complete suite still must be rerun.
- Targeted pairing tests passed: actual TLS code agreement, one-sided approval never grants credentials, directional opt-in, preserved receipts across helper restart, repeated acknowledgments, failed-persistence rollback, expiry and TLS intermediary mismatch.
- Summary tests use 10,000 history records and 10,000-entry shared manifests, check bounded JSON and complete cursor traversal. Preparation tests check replay keys, changed input conflicts and successful single commit.
- Qt Core tests compiled with Qt 6.12 on macOS and ran with the actual Go helper using temporary state/receive directories. Initial logic/integration suite passed. More regressions are being added.
- The Qt desktop target compiled with warnings treated as errors on macOS. It was not launched. This is a compile check, not Linux runtime or KDE/Wayland evidence.
- Linux native views and signed-feed/download code exist. AppImage handoff now waits for a private readiness reply before quitting. Its Linux-only replacement path still needs Linux execution tests and packaging.
- No app installed, GUI launched, GitHub visibility changed, tag created or release published yet.
- The dedicated Debian 12 / Qt 6.4 hosted job passed at `d313075`, including the desktop compile, Qt Core / actual helper checks and Linux-only update tests. The Mac hosted matrix job also passed for that checkpoint. This is not final-candidate or GUI acceptance.
- The hosted Windows run exposed CRLF-sensitive JavaScript extraction in a test; the Ubuntu run exposed a post-write counter synchronization race in a test. Both checks have been corrected without relaxing their behavioral assertions; final hosted runs remain required.
- macOS now consumes compact status and cancellable preparation, has recipient-focused navigation, paged history/details, native code-comparison pairing and optional daily beta update checks. Compile checks passed; final background tests and release packaging are still pending.

### Stabilization continuation

- Corrected cross-volume publishing-key comparisons, historical receive-file identity comparisons, open-file source replacement checks, and relay lockfile inspection. Local Go release/engine/server race suites pass; the next Windows run passed these cases but exposed the overly short 200 ms share-expiry fixture. Its validity window now spans setup and expires during a deliberately throttled download.
- Windows compiles with no warnings after compact status, cancellable preparation, paged history/details, eight-digit pairing and opt-in daily update checks. The current actual-helper Core suite passes 42 cases with no skips.
- Beta metadata is `0.12.0-beta.1`, numerical OS build 14. New release-tool tests cover the exact eight-package/five-feed set, signed manifest and tampering. Historical three-package fixtures remain supported separately.
- Linux package generation, isolated distro installation/upgrade tests and expanded payload inspection are being integrated. These packages have not passed the hosted matrix or been published yet.

- Expanded the package guard with bounded DEB, RPM/CPIO, Arch/zstd and AppImage/SquashFS inspection; all 19 guard self-tests pass, including intentionally private payloads and malformed archives. Full package audit remains pending.
- The hosted Linux builder now produces all four packages. Actual non-root signed AppImage handoff/relaunch (harmless CLI fixture) and refused-readiness preservation pass. The canonical public-key test needed the test binary to run from its package working directory; this runner fix is being applied.
- Repeated 300-MiB/4-GiB loopback comparisons show lower elapsed time for the prefetched helper and sampled RSS below 60 MiB. These are preliminary helper-hash-specific results. A final helper was rebuilt after the share-capture control fix; the large-manifest run uses that helper, and its single-file comparison must be repeated for final reporting.
- The 10,000-file baseline completed. Its matched candidate is running with the same dense fixtures, Unicode names, empty subfolders and independent destination/source hash checks. It is a control-polling stress workload, not a WLAN/native GUI speed claim.

### Completed transfer measurements

- Final repeated 300-MiB and 4-GiB comparisons and the matched 10,000-file case passed independent file/directory/size/SHA-256 checks. The candidate helper hash, raw per-run phase values and memory observations are retained in `docs/evidence/0.12.0-beta.1-performance.json`. Many-small-file performance remains slow; no significant improvement is claimed for that case.
- A new 4-GiB recovery check passed pause/resume and abrupt receiver/sender restarts without losing confirmed progress, ending in an independent full hash match. A separate simultaneous 300-MiB-per-direction check observed overlap and verified both outputs with a configured 64-MiB/s sending limit. Temporary state/fixtures were removed; no application or user state was installed or changed.
