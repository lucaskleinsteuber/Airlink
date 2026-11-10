# Release readiness

AirLink 0.12.0-beta.1 is published as a beta. The active repository is public after the authorized source/history/package checks; the older archive stays private and archived. This document defines the separate 1.0 acceptance gates. Background tests, compilation, packaging, physical device acceptance and publication are distinct evidence. The beta does not assign a 1.0 date.

[TESTING.md](TESTING.md) is the record of actual completed runs, their logs and their limits. Historical results retain their original version and scope. A build or cross-compilation does not prove a platform role works at runtime. The historical aggregate 100 GiB run does not prove one 100 GB file. The separate 0.5 single-file 100 GiB background run is version-specific evidence, and a local two-helper loopback run does not prove physical LAN or browser download behavior. Hosted CI must actually execute successfully before being reported as passed; historical runs without executed jobs remain unverified hosted observations.

All automated checks stay in the background. Physical and native-interaction acceptance below is manual; this checklist does not authorize desktop-controlling UI automation.

The [German device test guide](DEVICE-CHECK.md) walks through the currently available Mac, Windows laptop, iPhone and iPad, starting with small files and round-trip hashes. It does not replace the remaining Linux/Android or broader release gates below.

## Evidence required for each completed gate

Record the exact app/helper version, source revision, package/binary SHA-256, operating system and hardware, host/client roles, network type, transfer direction, fixture names/sizes, settings, and actions. Include timestamps, observed states/errors, received-file hashes where applicable, and the location of the resulting log or manual acceptance record. Do not publish pairing tokens, private console URLs, share secrets/passwords, or unrelated personal filenames in that evidence.

A gate closes only when its expected result has been observed on the final candidate. If a relevant source change follows, repeat the affected checks. A successful targeted regression does not imply the full suite or physical matrix passed. Unsupported browser behavior must have an accurate visible explanation and a workable documented recovery path rather than a simulated completion state.

## Platform roles on physical devices

The following checks remain open until their records are linked from TESTING.md. Use actual separate devices on a reachable local network, with internet disconnected for the offline pass. Test both directions where indicated.

| Required role / connection | Physical acceptance evidence |
| --- | --- |
| macOS native host ↔ Windows native app | Native discovery and manual address pairing, direct fingerprint comparison, honest pending approval, bidirectional files/folders, explicit directional auto-receive, direct Finder/Explorer result actions and restart with preserved trust/history; include supported CPU architectures in the support record. |
| macOS native host ↔ Linux desktop app | The same direct service roles in both directions, including filesystem permissions and case-sensitive source names. |
| Windows native app ↔ Linux desktop app | Native Windows setup, Linux private console setup and bidirectional direct transfer without depending on a Mac. |
| Desktop browser on macOS, Windows, and Linux | Each OS acts as a browser source and recipient: approved host upload, individual Range download, folder ZIP, reload/reselection and correct download-manager guidance. Record the actual browser versions; compiling the web bundle is insufficient. |
| Android browser ↔ desktop host | Certificate installation/trust, pairing, source file/folder capabilities, upload, download, share link, foreground/background transition, and practical recovery with the browser's real permissions. |
| iPhone/iPad browser ↔ desktop host | Profile installation and explicit full trust, pairing, Files picker behavior, upload/download/share, lock/background/return and source reselection as actually supported by that browser. |
| Android browser ↔ iOS browser through one desktop host | Both source/recipient roles, relay permission on both devices, explicit recipient acceptance, Range retry and ZIP fallback; no stored-host-payload fallback or saved-file completion claim. |

- [ ] Record all five operating-system roles above with explicit source and recipient observations.
- [ ] Confirm operation without internet, account, CDN, cloud relay, or external signaling dependency.
- [ ] Observe real multicast discovery through several 15-second browse refreshes, a peer disappearing beyond the 45-second hint lifetime, a changed address and sleep/wake. Library/state unit tests do not prove discovery on a real router.
- [ ] Check an isolated/guest Wi-Fi or unavailable multicast case: discovery failure is explained, a reachable manual address works, and unreachable peers do not produce an endless ready state.
- [ ] Record any supported-platform/browser limits explicitly. A missing platform run stays open rather than being inferred from another device.

## Setup and destination usability

- [ ] On a fresh native install, choose recognizable names and the normal Downloads target. Cancel before completion, recover from a failed save, restart, and verify the explicit setup-completion state. Confirm updated names appear on the other host.
- [ ] On upgrade, preserve identity, pairings, history and custom receive folders. A conventional old AirLink subfolder may be suggested as a change to Downloads only with confirmation. Existing files and saved transfer roots must stay where they were.
- [ ] Check the direct Windows EXE startup, embedded license pane, sidebar resizing/name readability, normal device selection, separate Settings, first-run wizard and primary result action on real supported Windows hardware.
- [ ] Verify a single receive lands directly in the selected folder; existing names get safe distinct names. Concurrent file and folder collisions must not overwrite or merge user content, and old/in-progress records must keep their original layout.
- [ ] Measure the reported approximately 300 MB physical scenario in both directions, recording source preparation, payload and final verification separately. Same-host throughput does not attribute the real Windows/Wi-Fi bottleneck or establish a physical speed claim.

## Interruption, recovery, and integrity

- [ ] Transfer a **single file of at least 100 GB** (100,000,000,000 bytes or more) between real desktop devices. Record exact bytes, elapsed time, sender/receiver hashes and final publication. A single 100 GiB file also satisfies the size threshold; many smaller files do not.
- [ ] Separately record any local background two-helper large-file run, including whether it uses final binaries, hard restarts, actual TLS payload and hash verification. Report measured process RSS per process and sampling method; a buffer bound or aggregate command-tree peak is not an equivalent measurement.
- [ ] Interrupt a direct transfer by disconnecting the network, then reconnect. Confirm the receiver's durable chunk checkpoint controls resumption, accepted chunks are not silently lost, and only a fully verified file is published.
- [ ] Kill/restart the source helper and receiving helper separately during active work. Confirm persisted identity, pairing, queue, settings and receive destinations survive. Repeat with paused and host-decision-pending work.
- [ ] Sleep/wake a physical source and receiver, change the network/address, and return a suspended phone browser. Confirm recovery or a clear explicit action; do not claim mobile background execution that the OS does not provide.
- [ ] Reload/restart a browser source, reselect the original files, and resume from confirmed offsets. Changed content must fail even when a reselected file has the same name, size and timestamp; missing local integrity evidence must not be guessed.
- [ ] Pause/resume/cancel during upload, source hashing, final verification, duplicate checking, download and relay. Include a deliberately slow body or low payload cap. No cancelled operation may later publish or revive after its stale callback completes.
- [ ] Revoke a device or the relevant send/receive/relay permission during active slow work. Confirm the affected stream stops, unrelated authorized work remains usable, and rejected requests do not disclose another device's metadata or host paths.
- [ ] Retry a browser-managed individual download with Range after interruption. Restart a folder ZIP after interruption and confirm the UI makes its non-resumable nature clear. Neither download initiation nor relay delivery is presented as a verified saved file.
- [ ] Exercise zero-byte files, empty directories where selection exposes them, nested trees, Unicode names, many small files, portable-name rejection, case conflicts and file/parent collisions. Confirm rejected names are never silently changed.

## Storage and source failures

- [ ] Fill the receive filesystem during an active transfer. Confirm a useful error, no incomplete published destination, no overwritten existing file, and a defined retry/recovery after space is available.
- [ ] Make the saved receive folder unavailable or unwritable before startup. On the native desktop apps, recover through the explicit folder-selection-and-retry action using the same application state; no automatic default-directory fallback or relocation of existing transfer destinations.
- [ ] Remove or change a destination during active work and retry after restoring access. Record what happens to existing checkpoints and partial files.
- [ ] Replace a selected source with a different filesystem object while preserving its name/size/time. Test a new source record and a historical record with a cached hash. Confirm rejection before stale cached integrity evidence is trusted.
- [ ] Change/remove a shared source or indexed duplicate candidate. Confirm no whole-file source copy, automatic deletion, placeholder, overwrite, or wrong-file export. Explicit Skip must revalidate its candidate; an all-skipped transfer must not expose the indexed original through a share.
- [ ] Block optional browser session storage. Pair and finish approval in the current page. Separately block IndexedDB and confirm a new upload sends no payload without saved resume evidence and explains how to recover.
- [ ] Set the recipient clock ahead/behind the host while opening a share. Host authorization governs access; host expiry or revocation still stops active work and closes the recipient page.

## macOS manual acceptance

- [ ] Close the main window during a direct transfer and relay. Confirm the menu bar still shows accurate activity and can reopen the intended window without interrupting work.
- [ ] Stop or fail the bundled helper. Confirm visible failed state, disabled helper-dependent actions, explicit Retry with preserved data, and no stale ready state or late response from the old helper session.
- [ ] Quit while idle, sending, receiving, hashing, waiting on a duplicate decision and relaying. Verify checkpoint/exit behavior and bounded cleanup. A helper still reported alive must leave an actionable failure rather than false successful termination.
- [ ] Invoke Finder **Services → Send with AirLink** with real selected files/folders; cover modern file URLs and the supported legacy input. Confirm original paths and files remain unchanged.
- [ ] Record, use, disable and restore the global shortcut; test an actual OS registration conflict and confirm the prior working shortcut survives. The recorder must capture only while explicitly focused.
- [ ] Enable/disable launch at login, observe OS approval/status changes, log out/in and confirm the intended startup behavior. Reading Settings/status must not register the item by itself.
- [ ] Allow/deny notifications and observe actual delivery for the intended completed/failed events. No repeated permission requests during ordinary status refresh.
- [ ] Review German/English, system/light/dark, long filenames, pending/error states and keyboard access in the real app. Model tests and a successful build do not establish native interaction quality.

## Windows manual acceptance

Native Windows is required for the agreed 1.0 product. A successful cross-publish or a test of the old console service does not close these native workflow gates.

- [ ] Install the x64/ARM64 Setup on its real OS architecture as a standard user. Verify Programs location, Start menu target and Installed apps metadata, then remove the downloaded Setup and confirm normal launching. Exercise update while running, locked-file failure, repair/reinstall and uninstall with preserved settings/cache/received/unknown files. The opt-in disposable-user silent acceptance script is a separate background check; parser success is not execution evidence.

- [ ] Start the correct x64/ARM64 package on a real supported Windows computer without a separate .NET install, terminal or browser. Record launch/security prompts and any Windows Firewall permission needed for reachable local transfers.
- [ ] Upgrade from an existing console installation with saved identity, pairings, history and receipt settings. A still-running console reports the data-directory conflict clearly; stop it once and retry without deleting state or pairing again.
- [ ] Discover/pair the Mac, compare the fingerprint directly, and keep the connection pending until actual approval. Exercise denial, expiry, retry and already-paired devices without a false connected state.
- [ ] Choose auto-receive explicitly on one computer and confirm it affects that receipt direction only. Check saved settings after restart; ordinary pairing/acceptance must not opt in silently.
- [ ] Send files/folders in both directions by dropping directly on a paired computer, dropping onto the central area labelled with the recipient, and choosing files from that recipient's picker. Each action must create only its explicit batch; an unaddressed selection must stay staged for a deliberate Send.
- [ ] Confirm the sole-recipient default only chooses an online authorized desktop for an empty, idle draft. An explicit target, a pending selection, a missing peer or a newly discovered second peer must not silently redirect selected files. Exercise an explicit offline queued offer and its eventual recovery.
- [ ] Add/remove/reselect paths and change the visible recipient while offer preparation is active. Acknowledgment may remove only the submitted entry identities; failure must retain retryable sources. A repeated Send or busy second direct drop must not silently create, merge or redirect another offer. Verify pause/resume/cancel, speed/progress and the real received file.
- [ ] Interrupt the local offer acknowledgment after a durable commit, retry the unchanged selection and confirm only the original transfer/file IDs exist. After service restart the same key must still return that job; changed selection/recipient keys must not redirect an old one. Check that this is distinct from unsubmitted draft persistence.
- [ ] Use Show file/Open folder for a completed receive, sorted files, transferred folders, an empty folder and an older destination. Missing files and all-skipped duplicates must not open an unrelated file. Review the visible saved-location guidance.
- [ ] Keep active work separate from recent results and verify a finishing old/resumed transfer remains discoverable with its result action. Trigger a visible user-action error and confirm an ordinary status refresh does not erase it.
- [ ] Close/reopen the window during work and confirm tray continuity, pairing/offer/duplicate/text/completion attention when enabled (including transfers finishing between polls), explicit Quit checkpointing and no orphaned owned helper. Test a helper failure, retry and unavailable receive-directory recovery without resetting data.
- [ ] Review German/English, system/light/dark, long names, keyboard access, file/folder dialogs, tray menus and notification clicks manually. Record x64 and ARM64 runtime support separately where tested.

## Shares, clipboard, and organization

- [ ] Complete phone certificate onboarding from the unencrypted public-material setup page, compare the fingerprint on the host, and open the trusted HTTPS page. Test removing trust and an untrusted/mismatched host without bypassing identity comparison.
- [ ] Open a full fragment share link with and without a password. Confirm filenames remain hidden before authorization, secrets leave the address bar, and access is limited to that share without device pairing.
- [ ] Verify one session allowance across multiple files, Range/resume, host restart and a new recipient session. Expiry/revocation during low-rate output must stop further payload. A client clock cannot override the host's policy.
- [ ] Create/list/copy/revoke shares in the native Windows dialog, including a custom local expiry time, password and download limit; verify its full fragment link on a second device and host-authoritative expiry/revocation.
- [ ] Use explicit clipboard Paste/Edit/Send/Copy/Dismiss in macOS, Windows and browser roles, including permission off, both desktop peers' permission checks, manual-paste fallback and the 64 KiB UTF-8 boundary. Receiving text must never replace the system clipboard automatically.
- [ ] Verify organization remains off/off with no rules on new and migrated settings. Exercise explicit Keep/Skip/path reveal, changed candidates, ordered rules, top-level individual-file sorting, unchanged folder hierarchy and new-offer-only snapshots.
- [ ] Review actual/avoided/relay/lifetime byte displays and last-activity UTC history cohorts. Clear terminal history and confirm files, active work, shares, other relay participants' associations and lifetime traffic remain as specified.

## Final candidate and distribution

- [ ] Exercise each native **Check for Updates…** action with a public signed channel, no newer version, an unavailable/private channel, invalid feed signature and corrupted payload. Verify graceful transfer checkpointing, cancellation and correct relaunch/version after a real update, with only the intended Mac app retained in Applications. Keep the publishing seed outside Git and securely backed up; public-key configuration alone cannot recreate it.

- [ ] Run the required background Go/race, web, Swift/helper and .NET core/helper checks on the final source and record their actual scope/results in TESTING.md. Regressions for discovered failures must remain in the background suites.
- [ ] Validate migration from the last private release with preserved identity, pairing, receive folder, transfer history, share policies/counters, explicit user opt-ins, and the documented defaults for any newly introduced setting.
- [ ] Build intended platform/architecture artifacts; verify embedded offline assets, helper/app version consistency, archive/DMG integrity, signatures and published checksums. Cross-compiled binaries still require their platform runtime evidence above.
- [ ] For a broad macOS release, produce a Developer ID signed and notarized app/DMG, validate stapling and first launch under ordinary Gatekeeper settings. Establish and validate the Windows signing/distribution policy before broader Windows delivery. The ad hoc development preview does not satisfy this gate.
- [ ] Manually install/update from the intended DMG. Keep only the current intended app bundle in `/Applications`; preserve separate data, libraries and received files. Recovery uses Git tags/releases rather than old app bundles stored there.
- [ ] Verify the hosted workflow's real result. If runners do not start, record that no hosted checks executed and keep local evidence distinct from hosted success; do not represent the run as green. A stable release needs completed platform verification.
- [ ] Verify release notes, README, version labels, artifact names/checksums, repository access and license match the actual candidate. Publish only the reviewed active repository under the current authorization; keep the archived predecessor private/archived. Record Linux package/runtime checks separately from actual KDE/Wayland device acceptance. Historical anonymous-access checks do not establish current availability.
- [ ] Resolve known data-loss, corruption, authorization and unrecoverable-state failures in the acceptance cases. Record any remaining supported-browser/platform limitation clearly before deciding whether the 1.0 bar has been met.

Completion requires reviewing the linked evidence, not a calculated percentage. There is no fixed 1.0 date and no substitution of a tag, green build, or local benchmark for the remaining physical and distribution gates.

## Native status scaling

The current native transports bound each helper reply to 16 MiB (Windows counts decoded characters). The legacy private status response retains its full shape. Current desktop clients use status.summary and paged history/file details; page/frame bounds and folder-entry counts remain finite and independent of payload file size. Since 0.7, native offer/share preparation and duplicate validation are exempt from the ordinary command deadline; large-folder preparation, cancellation and recovery still need physical acceptance.

Actual offer/share source enumeration still runs serially through the Go stdio dispatcher. The native transport’s out-of-order tests use fake helpers; actual long enumeration can delay status commands. Background preparation with bounded concurrency and cancellation must be validated before claiming responsive status for arbitrary large or unavailable source trees.

## Direct-root filesystem acceptance

Publication uses a confined selected root, identity checks on accepted source-folder roots and no-replace file publication. A simultaneous local process replacing a destination parent between validation and publication can still redirect a new file within that chosen root; parent descriptors are not pinned through the entire syscall. Remote manifests cannot create such links, and existing files are not overwritten. Strict local filesystem-race resistance remains a hardening item. A crash before an accepted source-folder reservation is committed may leave an empty original-named folder; recovery conservatively chooses a suffix and never adopts or deletes unproven user content.
