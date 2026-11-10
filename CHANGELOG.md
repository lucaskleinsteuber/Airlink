# Changelog

AirLink uses MAJOR.MINOR.PATCH versions. Entries describe implemented changes; distribution and verification status are recorded separately.

## 0.12.0-beta.3 — 2026-11-10

- Replace a stale reconnect error with the receiver's actual waiting-for-acceptance state as soon as a trusted connection succeeds. Keep the same offer/checkpoints, persist the transition before displaying it, and preserve pause/cancellation if a response arrives late.
- Avoid rewriting unchanged waiting status on every peer poll. Hide unsupported Pause actions while an offer is still awaiting acceptance on macOS and Linux.
- Document the local-network firewall requirement for Linux. A real CachyOS investigation confirmed UFW blocking the service while device discovery and pairing remained visible; allowing the TLS port from the local subnet restored the verified connection.
- Build 16. Complete Go race/vet, desktop core/web tests, native packages and six Linux package lifecycles passed. The physical investigation verified the firewall correction and trusted offer delivery; a complete physical file/hash run remains open because the laptop became unreachable again. See [TESTING.md](docs/TESTING.md) for the exact boundary.

## 0.12.0-beta.2 — 2026-11-10

- Store immutable manifests and individual file checkpoints separately. Block receipts, the file offset and the compact transfer header commit in one SQLite transaction after data fsync. Existing records migrate atomically; older clients fail closed on the new private storage format rather than ignoring checkpoints. Downgrading an upgraded data directory to Beta 1 is unsupported.
- Avoid full-manifest copies in worker status/file reads and source anchoring. Native senders request bounded per-file preparation/completion responses; older peers retain the original response shape.
- Add a manually saved local diagnostic report in macOS, Linux and Windows settings. The service exports a strict aggregate schema with no raw logs, identifiers, names, addresses, paths, credentials, clipboard text or file hashes. Unavailable services permit a minimal client-only report.
- Present German/English recovery advice separately from technical details. Retry recognized recoverable sender failures using the same transfer/source identity and the receiver's durable offsets; source changes, integrity failures, cancellation and completed jobs do not silently restart.
- Build 15. Matched Linux-runner TLS measurements: 10,000 small files 2,525.075 → 79.541 seconds (one matched run each); median 300 MiB 2.330 → 1.895 seconds and 4 GiB 28.973 → 24.833 seconds (three runs each). Independent end hashes, crash recovery and concurrent sending/receiving passed. These are loopback measurements, not WLAN claims. Source/package/update verification is recorded in [TESTING.md](docs/TESTING.md); physical and 100-GB device acceptance remain separate.

## 0.12.0-beta.1 — 2026-11-10

- Add a native Qt 6.4+ Linux desktop with setup, device sidebar, direct sends, cancellable preparation, paged transfers, shares, clipboard, browser setup, notifications, single instance and tray operation. Provide AppImage, DEB, RPM and Arch packaging with a tracked PKGBUILD.
- Reorganize macOS around the selected recipient, active transfers and three recent results. Keep tools separate, drafts intact and file details/history paged. Apply Linux saved language/appearance and avoid rebuilding unchanged transfer actions.
- Add TLS-exporter-bound eight-digit desktop pairing with independently derived displays, mutual confirmation, expiry, request limits, durable handoff and explicitly directional automatic-receive consent. Preserve the verified manual legacy flow.
- Add compact status and lazy tooling, asynchronous cancellable preparation and bounded one-block sender prefetch. Reject stale native snapshots after mutations.
- Add optional daily update notices, separate Mac architecture packages, numeric OS build 14, signed Linux updates and an independently signed release manifest. Keep the existing public signing key.
- Correct Windows cross-volume path checks and platform-sensitive filesystem test fixtures without relaxing source/integrity or publishing-key boundaries.

Published as a GitHub prerelease with eight application packages, signed feeds and a signed manifest. Checks and distribution are recorded in [TESTING.md](docs/TESTING.md). Physical desktop rendering, Mac↔Arch WLAN and a new 100-GB run remain open.

## 0.11.0 — 2026-10-26

- Replace growing chunk buffers with exact-sized bounded reads and retain oversize/error/cancellation checks. Reuse no mutable sender buffer across HTTP requests. Skip redundant SQLite saves only for identical already-durable sender offset/hash values; keep fresh in-memory speed samples after resume.
- Organize native settings by category with a persistent draft, visible Save/Discard actions, validation and confirmed-save feedback. Merge edited fields into the newest settings and refresh untouched fields without discarding in-progress input.
- Add a third setup review step for name, receive folder and notification choices, with a direct next-device action. Keep existing setup, custom folders and permissions.
- Avoid Windows visual-tree updates while the main window is hidden while transfers, controller state and tray notifications continue.

Isolated 8-MiB operations allocate about 50.8% fewer bytes. Three-run same-host TLS medians were 0.966 to 0.941 seconds for 300 MiB and 1.989 to 1.923 seconds for 128 small files. These small timing differences are not a demonstrated physical WLAN improvement. Full scope, checks and limitations are in [TESTING.md](docs/TESTING.md). Distribution remains private through manual release downloads.

## 0.10.0 — 2026-10-26

- Guide native device connection through nearby computer selection, complete fingerprint comparison and approval on the other computer. Keep manual entry as a fallback and browser/QR setup separate. Pairing is shown as complete only after the saved certificate identity is confirmed.
- Make trusted devices, device settings and optional automatic receiving easier to find. New pairings keep automatic receiving off until explicitly enabled for that receiving computer.
- Add **Remove from list** for individual finished file transfers and a confirmed **Remove finished entries…** action with a fixed selection. Active transfers and transfers finishing later are not swept into the removal.
- Persist history removal without deleting originals, received files or independent shares. Check ownership and terminal state atomically; database failures retain the entry. Lifetime traffic counters remain separate from history-derived totals.
- Discard older status responses after mutation refreshes so removed entries and changed device preferences do not reappear from an earlier poll.

This remains a private development preview distributed through manual GitHub release downloads. Completed background checks and build evidence are recorded in [TESTING.md](docs/TESTING.md); native visual/device acceptance remains separate. No new 100 GiB or WLAN throughput claim is made.

## 0.9.0 — 2026-10-25

- Replace portable Windows downloads with real x64/ARM64 Setup executables. Setup installs the self-contained app in `%LOCALAPPDATA%\Programs\AirLink`, creates a current-user Start menu shortcut and registers an Installed apps uninstall entry without administrator privileges.
- Stage Windows installation files before replacing the app, refuse changes while the desktop is running, and preserve settings, pairings, history, runtime caches, received files and unknown installation-folder files on uninstall.
- Add manual **Check for Updates…** to the macOS AirLink menu and menu-bar panel, and to Windows Settings and the tray menu. macOS uses Sparkle 2.10; Windows uses NetSparkle 3.1 and the new Setup package.
- Pin an Ed25519 public key and require signed feeds and signed update archives/installers. Checks are user initiated; scheduled checks, automatic downloads and system profiling are disabled. An unavailable private feed is reported as unavailable, not as an up-to-date result.
- Add an offline release-signing/public-verification tool and background installer/privacy/signature checks. The private signing seed stays outside Git; reviewed upstream licenses are preserved.
- Withdraw untested Linux binary downloads from 0.8.1 and 0.9.0 and from future release packaging; Linux remains experimental and available from source. Curate package manifests/checksums without changing native packages or signed update feeds.

The active repository is private during development. The owner chose manual download/installation from GitHub Releases after the earlier approved public interval. Anonymous update delivery was verified during that interval, but the existing updater cannot access the current private feeds/downloads. The earlier archive remains private and archived; a later public release requires a new owner decision. Background, build, delivery and cryptographic verification are recorded in [TESTING.md](docs/TESTING.md); this entry does not claim a physical Windows installer or interactive native updater acceptance run.

## 0.8.1 — 2026-10-25

- Use `lucasfd3` for first-party copyright and app metadata, with neutral setup examples and fixtures. Remove owner-specific observations and account details from publication documentation.
- Remove local build paths from native release payloads; disable managed debug paths in the shared Windows assembly, map Swift paths and strip linker debug maps before signing. Go publication builds omit checkout revision metadata.
- Package Linux archives with neutral ownership and timestamps instead of the packager's local account metadata.
- Add background privacy regression checks for tracked publication inputs and historical author/tagger identities. These checks supplement the separate historical binary and GitHub metadata audit.

At original publication, repository visibility remained private. The later public visibility and Linux download withdrawal are recorded in 0.9.0. Historical cleanup and replacement-download publication are recorded separately; this entry does not assert that unreachable GitHub objects or previously downloaded copies have been erased. No updater was enabled in this patch.

## 0.8.0 — 2026-10-25

- Added guided native setup for a recognizable device name and an explicit receive folder, with persisted completion state. Downloads is the normal initial destination; migration from the conventional AirLink subfolder is suggested and requires user confirmation. Existing custom destinations, trust and history remain separate from the app bundle.
- Reworked the Windows interface around a resizable device sidebar, readable two-line names, selected-recipient drop area, focused transfer cards and a dedicated Settings page. Device permissions and advanced tools are separated from the everyday send view.
- Replaced the primary Windows ZIP packages with direct self-contained x64 and ARM64 EXE downloads. The matching service, runtime, icon, MIT license and runtime notices are bundled; license text is accessible in Settings. Builds inspect managed resources without loading WPF or executing Windows code.
- Changed new receive publication to the chosen destination directly. Name collisions receive a distinct file or top-level folder name; sent folders keep their hierarchy. Private partial data remains scoped to its transfer, while older/in-progress receive records retain their saved layout.
- Reclaim known direct-receive partial files after durable cancellation, with writer exclusion and bounded shutdown; retained cancelled records retry cleanup at startup. Published content and legacy layouts are preserved.
- Announce committed device-name changes through owned mDNS registrations and pinned peer heartbeat responses.
- Wake the sender immediately after a new offer or resume and send an explicit length for buffered blocks. Three same-Mac 300 MiB TLS runs per version reduced median offer-to-completion time from 2.014 to 0.989 seconds; this establishes the removed startup delay, not resolution of the reported physical WLAN slowdown.
- Kept device approval and directional automatic receive explicit. Setup does not grant new device permissions, and cancelling or failing setup does not mark it complete.

This remains a private development preview. Completed background, packaging and performance observations belong in [TESTING.md](docs/TESTING.md). A local benchmark does not establish the cause of physical cross-device throughput limits. Native rendering, setup interaction, real Downloads redirection and cross-device throughput still require the manual checks. No desktop-controlling tests are introduced.

## 0.7.0 — 2026-10-25

- Added direct desktop sending: dropping a batch on a paired computer sends to that computer; the named central drop area sends to the selected desktop, and the Windows file/folder picker starts its selected batch directly. The macOS picker retains a prepared selection and Send action. Without a suitable recipient, files remain staged for an explicit Send. Offline paired computers retain their queued offers.
- Added tested Windows send-draft state with immutable recipient/source snapshots, protection against repeated submissions and stale acknowledgments, direct-batch isolation, preserved in-flight additions/reselections and explicit failure retry. A sole online authorized desktop can be selected for an empty idle draft; refresh never replaces a shown recipient or redirects pending files.
- Made private offer creation idempotent with a persisted 32-character request key. An identical retry returns the original transfer and file IDs, including after service restart; conflicting input is rejected. Native draft retries reuse the key only for the same recipient and entry identities, while keeping separate local attempt guards against late callbacks.
- Added native Windows Quick Share creation, expiry/password/download-limit controls, full-link copying, share list and revocation, using the existing local share contract. Added explicit clipboard Paste/Edit/Send/Copy/Dismiss with the existing permission and UTF-8 limits; receiving text does not write the system clipboard.
- Added Windows tray attention for pairing, incoming acceptance, duplicate decisions, clipboard messages and completions that happen between status polls. Historical completions do not replay at startup. Action errors persist across ordinary status refreshes, and the main view separates active work from recent results.
- Added Windows window shortcuts for choosing files and sending a prepared selection. Direct send keeps the selected recipient visible; receipt consent and certificate comparison remain explicit.
- Refreshed mDNS browsing in 15-second windows so the discovery library's lifetime deduplication cache cannot permanently age out a live peer. Nearby hints expire after 45 seconds; saved pairings persist independently. Discovery remains a connection hint, not an authorization or physical-network reliability proof.

- Exempted macOS offer/share preparation and duplicate validation from the ordinary client deadline. Fake-helper tests cover independent replies during a delayed command and continued use after an ordinary timeout. Actual Go stdio offer/share capture remains serial and can delay status during long filesystem enumeration.

This is a private usability preview. Completed final-source tests and artifact checks belong in [TESTING.md](docs/TESTING.md). The new Windows interaction, actual multicast/LAN behavior, notification clicks, dialogs and clipboard integration still require manual device acceptance. Native Windows relay administration, detailed statistics and sorting-rule editing remain further interface work. Historical 0.5 large-file evidence is not a new 0.7 result.

## 0.6.0 — 2026-10-25

- Added a native Windows WPF interface with an embedded Go helper and self-contained .NET runtime for x64 and ARM64. The normal transfer flow uses its own window and tray icon, with discovery, pairing, drag-and-drop, native file/folder selection, progress, pause/resume/cancel, duplicate decisions, receive-folder access, and basic settings. No terminal or browser is required for that workflow.
- Preserved the existing `%APPDATA%\AirLink` identity, pairings, settings and history. A still-running older console service must be stopped once before the app can acquire the same private data directory. The app owns only its helper process and provides explicit startup retry; it does not silently terminate another service or reset user data.
- Made automatic receipt an explicit, directional local choice during pairing or incoming-offer acceptance, as well as in device settings. Ordinary approval retains per-transfer confirmation. Failed or expired pairing does not grant automatic receipt; transfer acceptance and permission persistence report their actual outcomes.
- Kept desktop pairing visibly pending after request acknowledgment until the remote device approves. macOS uses the discovered address/fingerprint as a draft and selects the verified paired device after approval, rather than treating the request itself as a completed connection.
- Added direct received-file/folder actions using a private, validated result lookup. Actions follow actual published destinations, exclude skipped and missing payloads, and reject symlink traversal. They do not reveal source paths or redirect to an indexed duplicate. New successful receives remove only their empty internal partial directory; old receive folders are left in place.
- Replaced the macOS shared asynchronous-byte stdout reader with independent nonblocking readers per helper. Idle connections no longer starve another helper’s replies; malformed, incomplete and oversized messages fail with bounded buffers.
- Added background service, transport, extraction/cache, protocol-model and native state regressions. Windows packages use the existing code-authored linked-arcs icon motif. No desktop-controlling tests are introduced.

This remains a private development preview. Native Windows covers the core transfer workflow; advanced Quick Share, clipboard, relay-management, statistics and sorting-rule controls are not yet at native macOS parity. Completed final-source checks and package evidence belong in [TESTING.md](docs/TESTING.md). Cross-publishing a Windows GUI executable does not establish actual Windows interaction, Explorer/tray/notification behavior, or physical-network acceptance. A preliminary 0.5 Mac-to-Windows report remains incomplete physical evidence; it does not establish final 0.6 acceptance or an independently hashed large-file result.

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
