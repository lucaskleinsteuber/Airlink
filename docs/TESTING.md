# Testing and verification

AirLink uses background unit, protocol, subprocess, filesystem, and HTTPS integration tests. The test suite does not automate Finder, click application windows, launch a simulator, or take control of the user's desktop. Interactive runtime acceptance is recorded separately from automated test results.

## Run the checks

Install the development dependencies described in [README.md](../README.md), then run from the repository root:

```sh
npm --prefix web ci
bash scripts/test.sh
```

The script runs Go tests with the race detector, .NET Core logic/process tests, web tests, and the production web build. On macOS it also compiles the native target and runs Swift Testing. Actual-helper integration cases are gated until a helper path is supplied. The Windows GUI is compiled by the build script and is never launched by these tests.

To build the complete embedded application and portable binaries:

```sh
bash scripts/build.sh
```

Run the Windows Core background tests against the built helper (use the matching helper path on each OS):

```sh
AIRLINK_TEST_HELPER="$PWD/build/airlink-darwin-arm64" \
  dotnet run --project windows/AirLink.Core.Tests/AirLink.Core.Tests.csproj -c Release
```

The Mac-only HTTPS fixture uses Node with the helper CA, normal hostname verification and TLS 1.3 because the .NET macOS TLS backend does not provide the protocol needed by the service. Windows/Linux retain the .NET HTTP test path. This fixture is test-only; the production Windows app controls its own Go helper through private pipes. No certificate is installed into the system trust store.

On macOS, run the native tests against the built Go helper:

```sh
AIRLINK_TEST_HELPER="$PWD/build/airlink-darwin-arm64" \
  swift test --package-path macos --scratch-path build/swift-tests
```

Choose the matching architecture on an Intel Mac. The integration case starts only the helper process with temporary state and receive directories and discovery disabled. It does not launch the native app.

Set `AIRLINK_GO=/absolute/path/to/go` for the shell scripts if needed. For a direct Go command, invoke that Go binary explicitly.

## 0.9 native installation and signed manual updates — 2026-10-25

| Area | Completed observation | Scope and limits |
| --- | --- | --- |
| Go | **163 top-level race suites / 258 cases passed**; `go vet ./...` passed | Actual 0.3–0.7 helper upgrades and official Sparkle signature compatibility enabled. Historical 0.1/0.2 and large-file stress remain gated. |
| Windows Core | **33 passed, zero skipped/failed**, with the actual 0.9 helper | Signed feed/private-404/tamper/native-channel tests, graceful-only helper exit and literal Windows waiter construction. The harmless waiter execution branch is Windows-only and was not executed on this Mac. |
| macOS Core/web | **59 Swift tests / 63 cases** with both actual-helper cases; **68 web tests** and production build passed | Background logic/protocol checks; no native updater/app GUI launched. |
| Native packaging | Both Windows native CPU publishes and universal macOS assembly passed | App/helper/framework/XPC/tool architecture, relative Sparkle loading, public key/feed policies, exact licenses and nested deep/strict ad hoc signatures checked. Windows PE/embedded resources checked without running the app. |
| Setup | Both x64/ARM64 NSIS Setup files compiled and exact embedded app hashes/native PE machines verified | Fixed per-user Programs path, Start menu/HKCU64 registration and narrowly scoped uninstall are source-reviewed. Actual Windows install/upgrade/uninstall was not run on macOS. |
| Privacy guards | **Six source-guard tests and 17 package-guard tests passed** | Includes compressed NSIS metadata/uninstaller streams, extraction bounds, nested .NET resources, reviewed vendor-license handling and private seed filenames. Final five-package audit: **1,038 payloads, 919 bundle entries, 2,010 managed resources, four NSIS streams, zero findings**. |
| Final signed artifacts | The public-only Go verifier accepts all five actual packages, three exact feeds, manifest and checksums; actual NetSparkle accepts both final architecture feeds/Setup files; official Sparkle verifies the final macOS appcast and universal DMG | Signing is Ed25519 authenticity, separate from OS Developer ID/Authenticode. Altered feeds/packages are rejected in regression fixtures. |
| Installer handoff | Independent source review found and resolved upstream batch launch and later-exception recovery issues | Windows adapter uses absolute cmd.exe, a bounded fixed-PID no-kill waiter, literal paths and recovery of disabled/prepared UI state. This is source/background proof, not physical Windows interaction. |
| Hosted/physical acceptance | Disposable-user silent Setup acceptance and Windows-only waiter execution are provided in CI; neither was executed on this Mac | No GUI automation, new physical throughput or 100 GiB run. Local checks are not hosted CI or physical-runtime success. |
| Online channel | Source/config and signed feeds target the active GitHub repository; its current visibility is private | Token-free live delivery requires public feed/assets. Private/unavailable metadata is an error, not an up-to-date result. Visibility is a separate owner decision. |
| Private release delivery | 0.9.0 published privately; all seven remote file sizes/SHA-256 values match the reviewed local packages, signature manifest and checksums | Actual anonymous delivery remains gated by the owner's visibility decision. No GitHub token is placed in either app. |
| Final publication privacy | Independent fresh GitHub clone at `946267c8a1afe2a1133438640c54d067e31b47b2`: all 796 received objects (565 blobs, 198 trees, 23 commits, ten tags) checked clean; 243 current files/history guard passed | Includes exact original identity/home-account and actual publisher seed raw/base64/hex checks without outputting secret values. All objects reachable, fsck clean, alias/Noreply metadata only. |

Ignored evidence: `build/go-tests-0.9.0.jsonl`, `go-vet-0.9.0.log`, `swift-tests-0.9.0.log`, `web-tests-0.9.0.log`, `web-build-0.9.0.log`, `windows-updater-core-tests-0.9.0.log`, `windows-updater-compilation-0.9.0.log`, `windows-publish-0.9.0.log`, `macos-sparkle-*.log`, `package-guard-tests-0.9.0.log`, `package-0.9.0.log`, `dmg-verify-0.9.0.log`, `release-signature-verify-0.9.0.log`, `update-package-netverify-*-0.9.0.log` and `update-*-sparkle-verify-0.9.0.log`. Private signing material is outside Git and is never a test/release artifact. The installed 0.8.1 Mac app and application data were not changed.

## Independent repository migration — 2026-10-25

The owner authorized retaining the earlier repository as a private, archived repository and creating a new independent private `lucasfd3/Airlink` under the active address. This migration changes repository storage/identity, not app behavior or version.

| Area | Completed observation | Scope and limits |
| --- | --- | --- |
| Repository boundary | Active repository ID 1433237767 is private, active and not a fork; earlier ID 1431646266 is private and archived as `Airlink-private-archive` | No public visibility change was made. |
| Sanitized import | Only main and nine version tags imported through a separate fast-export/import object database; all ref IDs preserved | Original local unreachable objects and editor refs were excluded. |
| Independent fresh clone | Import revision `92cd7027606e54e8ff99c63172b43d724d3c651e`: 20 commits, nine annotated tags, 504 blobs, 172 trees; all 705 received objects checked, zero unreachable received objects | Source/history guard and independent exact original identity/home-account byte checks report zero findings. All author/committer/tagger identities use the public handle and Noreply address. |
| Old-object route probes | All 17 original commit IDs explicitly absent through both authenticated commit APIs: 17 no-commit responses (422), 17 object-not-found responses (404), no redirects | Current clean HEAD returns the expected SHA with status 200 through both APIs. Anonymous HTML 404 checks are private-visibility controls only. |
| Earlier tag and copyright objects | Eight original annotated-tag objects and ten original copyright blobs return authenticated object-not-found responses (404) | Across commits/tags/blobs there are 52 negative route probes with no redirects. Positive current HEAD/tag/license controls return status 200 with the exact expected SHA. |
| Inherited metadata | Exactly one intended branch and nine tags; zero PRs, forks or Actions runs at the check | No historical release downloads or hosted runs imported. The earlier repository's retained-object observations below concern its separate private storage. |
| Release copy | Private 0.8.1 published in the new active repository; all six remote file sizes/SHA-256 values exactly match the reviewed local inputs | The new repository has only this replacement release. No rebuild or new platform/runtime acceptance is implied. |

The checks cover publication inputs and the probed repository routes, not provider-wide erasure or copies held elsewhere. The active repository remains private; its visibility is a separate owner decision.

## 0.8.1 privacy patch evidence — 2026-10-25

| Area | Completed observation | Scope and limits |
| --- | --- | --- |
| Go | 154 top-level race suites / 242 cases passed; `go vet ./...` passed | Actual unchanged 0.3–0.7 helper upgrades enabled. Historical 0.1/0.2 and large-file stress remain gated. |
| Native/web checks | Windows Core 28/28 with actual helper; Swift 59 tests / 63 cases with actual-helper cases; web 68/68 and production build passed | Background logic/protocol checks on macOS, no desktop-controlling automation or physical Windows runtime acceptance. |
| Publication regression guards | Five source/history-guard selftests and five package-guard selftests passed | Includes compressed .NET bundle inspection, UTF-8/UTF-16 home paths, neutral fixture handling, archive owner metadata, credential shapes and malformed compressed data. |
| New package privacy | Five final 0.8.1 packages inspected with zero findings: 929 payloads, 913 compressed bundle entries and 2,004 managed resources | Both Windows EXEs, both Linux TARs and read-only universal DMG. No packaged program was executed by the privacy audit. Personal home-path/first-party copyright and archive-owner rules supplement the historical secret scan. |
| Native builds and smoke | Both Windows architectures and universal macOS app/helper built; managed/native package inspection and both final helper smoke checks passed | New first-party MIT license and publisher metadata use the public handle. Windows is unsigned; macOS remains ad hoc signed without a human Developer ID authority. |
| Historical audit | Original 17 commits/eight annotated tags and eight release generations reviewed | Found old author/tagger identity, old first-party copyright, native debug paths and archive ownership. Two Gitleaks results were independently classified as synthetic/prose false positives. No actual credential was identified in the reviewed content/patterns. |
| Earlier-repository cleanup | Approved rewrite applied; 48 old release files and eight hosted runs removed; private 0.8.1 published and all six remote asset digests/sizes verified | Historical observation before the repository migration: fresh clone had 19 commits, nine tags, 502 historical blobs, zero guard findings; exact-value scan of all 700 reachable objects also clean. |
| Earlier retained provider objects | Two directly probed old commit IDs still returned their previous author metadata in the earlier repository | That earlier repository is now the separate private archive. Current active-repository route checks are recorded in the migration evidence above. No provider-wide erasure is claimed. |

Ignored evidence is in `build/go-tests-0.8.1.jsonl`, `go-vet-0.8.1.log`, `dotnet-tests-0.8.1.log`, `swift-tests-0.8.1.log`, `web-tests-0.8.1.log`, `platform-build-0.8.1.log`, `smoke-*-0.8.1.log`, `package-privacy-0.8.1.log`, `package-0.8.1.log` and `dmg-verify-0.8.1.log`. Raw/private audit inputs are not publication artifacts. The installation and application data were not changed. No new throughput or 100 GiB claim is made for this metadata/privacy patch.

## Historical 0.8 evidence — 2026-10-25

| Area | Completed observation | Scope and limits |
| --- | --- | --- |
| Go | **154 top-level race suites / 242 cases passed**; `go vet ./...` passed | Includes unchanged tagged 0.3–0.7 helper upgrades. Historical 0.1/0.2 and large-file stress remain gated. |
| Direct receiving | Ten new direct-publication suites passed | Files publish into the chosen root; collision suffixes, per-transfer stages, identical file IDs, independent receivers, folders/empty directories, external collisions, restart, legacy paths and fresh index reuse were exercised. Existing files are never overwritten by publication. |
| Setup/settings | Three new server suites passed | New defaults, explicit setup flag, legacy/custom settings preservation, validation and failed-save rollback. Incoming configuration changes only with the settings commit. Names refresh through owned mDNS registrations and pinned heartbeat responses; tests use background/fake advertisement seams, not real multicast. |
| Cancellation | Five new race suites passed | Known direct-receive partials are reclaimed only after durable cancellation and file-lock exclusion; published content, unknown files, symlinks and legacy layouts are preserved. Close remains bounded and startup retries pending cleanup. |
| Windows Core | **28/28 tests passed**, no skips, with the actual 0.8 helper | Setup defaults/name bounds and local-version metadata join existing private transport, offer replay, trust, TLS receive, direct result, lifecycle and draft tests. The Core executes on macOS, not Windows UI. |
| macOS Core | **59 Swift tests / 63 cases passed**, no skips, both actual helper cases enabled | Explicit setup defaults/roundtrip/validation plus all prior transport, direct-send, retry and two-helper TLS checks. Both final native CPU architectures compile. No app window was automated. |
| Web | **68 tests across seven files passed**, production build passed | Existing browser functionality retained; no browser automation. |
| Native Windows packaging | x64 and ARM64 GUI executables published; native PE and managed resource inspection passed | Inspected version 0.8.0.0, icon/manifest resources, matching embedded Go helper hash, MIT license and resolved runtime notices without loading WPF or running Windows code. The primary Windows download is now a self-contained EXE. |
| Final helper smoke | Portable arm64 and universal bundled helpers passed | Embedded offline assets, verified TLS 1.3 and private/source/share/relay/clipboard boundaries. This is headless protocol execution on macOS. |
| Physical throughput | Current cross-device WLAN throughput remains a manual acceptance item | No physical Windows phase breakdown or independent current size/hash/timing record is included here. Local benchmark settings describe only their explicit test fixtures. |
| Measured comparison | Three actual 300 MiB transfers per version: released 0.7 median **2.014076 s**, final 0.8 median **0.988569 s** from offer through both services completing | Same Mac/loopback, TLS, non-sparse 314,572,800-byte source, freshly written/cache-warm data and unlimited rates. Independent whole-file hashes matched in every run. This removes about one second of idle sender scheduling, not a measurement of physical cross-device network performance. |
| First confirmed payload and RSS | Median first observed block **1.147704 → 0.193612 s**; final 0.8 sampled RSS maxima source **45,973,504 B**, receiver **48,562,176 B** | Observed polling and sampled RSS, not exact high-water marks or fully occupied duplex capacity. Source preparation, payload and verification observations are in the report. |
| Exact benchmark helper | Final arm64 SHA-256 `9c802884452d3886ca7425a05a25afdf01cdbb4e467d3d5c3a4f5654f0f22d8f` | File SHA-256 `c94f9518a31d14b56d671b997ff6d3c99dc5dd58109f0c409b60f21bd748e6ea`. The bundled universal helper is separately signed and smoke-checked. |
| Hosted CI | Historical hosted run (withdrawn during privacy cleanup): hosted jobs did not execute | Local checks are recorded separately. This is not a passing hosted run or platform-runtime result. |
| Large-file/physical acceptance | No new 100 GiB test or native Windows interaction was run | Historical 0.5 100 GiB evidence remains below. New Windows rendering, high-DPI setup, Explorer actions, actual WLAN/multicast, cancellation on NTFS and physical performance remain open. |

Final ignored evidence is in `build/go-tests-0.8.jsonl`, `go-vet-0.8.log`, `dotnet-tests-0.8.log`, `swift-tests-0.8.log`, `web-tests-0.8.log`, `web-build-0.8.log`, `windows-publish-0.8.log`, `macos-final-build-0.8.log`, `performance-0.7-baseline.json`, `performance-0.8-final.json` / `.log`, and `smoke-*-0.8.log`. Windows EXE equality, Linux archive entries/payloads, universal architectures, bundle version/build 0.8.0/8, ad hoc signature, staged app equality, DMG integrity and all five SHA-256 values passed. The tracked benchmark is `scripts/benchmark-transfer.py`. An existing installed app was left running; installation and interactive acceptance were not performed in this run.

## Historical 0.7 evidence — 2026-10-25

| Area | Completed observation | Scope and limits |
| --- | --- | --- |
| Go | **131 top-level race suites / 219 cases passed**; `go vet ./...` passed | Includes unchanged tagged 0.3, 0.4, 0.5 and 0.6 helper upgrades. Historical 0.1/0.2 and large-file stress remain gated. |
| Idempotent local offers | Lost-reply/restart, 24 concurrent retries, recipient/path conflicts, invalid keys, failed SQLite commit and legacy no-key behavior passed | A request ID and input fingerprint commit with the initial transfer document; replay does not recreate or revive work. Keys last with retained transfer history. New explicit intents remain distinct. |
| Discovery | Five new background suites passed | Bounded browse windows compensate for the dependency's lifetime delivery deduplication; renewed hints stay fresh, stale hints expire after 45 seconds, and expiry cannot erase paired trust. Fake browse/clock evidence does not prove actual multicast/firewall behavior. |
| Windows Core | **27/27 tests passed**, no skips, with the actual 0.7 helper | Direct-batch isolation, selected entry identities, unique-recipient defaults, offline explicit queueing, repeated/stale attempts, stable offer keys, attention notifications and completion ordering. The real private RPC case replays one offer before and after restart and verifies identical transfer/file IDs, in addition to TLS receive/trust/result checks. Core runs on macOS; WPF/Explorer/tray interaction is not executed. |
| macOS Core | **56 declared Swift tests / 60 executed cases passed**, no skips, with both actual 0.7 helper cases enabled | New direct batches pin recipient/files separately from editable selections, unchanged retries retain a wire key with a fresh local attempt identity, and the two-helper TLS case replays the actual direct-send offer without creating a second transfer. Fake-helper cases prove that ordinary command deadlines still fire and that offer/share/duplicate replies can outlive that deadline while independent replies are delivered. Real helper cases separately prove TLS and keyed-offer replay. Go stdio still serializes actual offer/share capture, so large filesystem preparation can delay service status. No GUI was run. |
| Web | **68 tests across seven files passed** and production build passed | Existing browser/share/clipboard/relay/organization capabilities retained. No browser automation. |
| Native Windows build | Self-contained x64 and ARM64 GUI executables published and PE/resource structure checked | New native Quick Share and clipboard forms compile with the direct-send window. This establishes compilation and packaging structure, not runtime rendering, system clipboard integration or tray behavior. |
| Final app/packages | Universal macOS app/helper and both Windows native builds compile; final helper smoke checks passed | Verified embedded offline assets and TLS/private boundaries; both Windows PE machines use GUI subsystem 2 with icon/version/manifest resources. No Windows executable was run. |
| Distribution integrity | Five versioned archives/DMG plus SHA256SUMS created and verified | Exact ZIP/TAR entry sets and executable payload equality passed; universal architectures, bundle version/build 0.7.0/7, deep/strict ad hoc signature, staged app equality and DMG checksum passed. |
| Local Mac update | Final 0.7.0 bundle installed and registered as the sole `/Applications/AirLink.app`; signature verified | The old app was not running, and app-data file sizes/timestamps were unchanged. No old bundle was stored in Applications, and no GUI was launched. This is installation, not interactive acceptance. |
| Hosted CI | Historical hosted run (withdrawn during privacy cleanup): hosted jobs did not execute | Local checks are recorded separately. This is not a passing hosted run or platform-runtime result. |
| Large-file evidence | No new 100 GiB run performed for 0.7 | The 0.5 single-file run below remains historical. New offer metadata and native/discovery work do not establish a new physical or current-package stress result. |
| Physical acceptance | No new native 0.7 Windows or cross-device interaction performed | A preliminary 0.5 Mac → Windows transfer report remains limited evidence; native 0.7 UX, real discovery, interruption, mobile and platform filesystem checks are still open in DEVICE-CHECK / RELEASE-READINESS. |

Final local logs are ignored under `build/go-tests-0.7.jsonl`, `build/go-vet-0.7.log`, `build/dotnet-tests-0.7.log`, `build/web-tests-0.7.log`, `build/web-build-0.7.log`, `build/windows-publish-0.7.log`, `build/swift-tests-0.7.log`, `build/macos-final-build-0.7.log` and `build/smoke-*-0.7.log`. The hosted run above is blocked; successful local checks do not imply hosted success.

## Historical 0.6 evidence — 2026-10-25

| Area | Completed observation | Scope and limits |
| --- | --- | --- |
| Go | Final race run passed **121 top-level tests, 209 cases including subtests**; `go vet ./...` passed | Includes real released 0.3, 0.4 and 0.5 helper upgrades. Historical 0.1/0.2 and large-transfer stress remain gated. |
| Local receipt and consent | New receipt-resolution, staging-cleanup and directional trust regression suites passed | Actual published/sorted locations, empty folders, skipped payload exclusion, missing/symlink results, private API scope, revoked permissions, explicit pairing/acceptance and persistence failure boundaries. No Explorer/Finder automation. |
| Released 0.5 upgrade | An unchanged tagged 0.5 helper created state, a pairing with explicit auto-receive, a verified receive and a protected share session, then the current service reopened it | Identity, certificate, existing trust, source/received bytes, result action, settings/rules, share session/quota and history survived. Temporary fixture only; user application data was not modified. |
| Windows Core | **19 background tests passed, none skipped**, including the actual final helper | Private request correlation, timeout/retry, bounded diagnostics/shutdown, old sessions, helper extraction/repair/concurrency, certificate identity and trust/display policies, real TLS receive and persistence. This runs the cross-platform Core on macOS and does not execute WPF or Windows system integration. |
| macOS Core and helper | **49 Swift tests / 51 executed cases passed**, both real-helper cases enabled; final arm64/x86_64 production builds passed | Includes two independent live helper readers, malformed/oversized replies, old-session/shutdown regressions and an actual two-service trusted TLS transfer. Per-session nonblocking stdout reads fix the observed shared async-byte-reader starvation. No native GUI was launched. |
| Windows builds | Self-contained x64 and ARM64 native WPF executables published; PE architecture, GUI subsystem 2 and native icon/version/manifest resources checked | Go helper and runtime are bundled. No Windows executable was run. Window/tray, dialogs, firewall behavior and Explorer actions require the physical laptop check. |
| Web | All **68 Node tests** across seven files and production build passed | Existing browser functionality retained; no browser UI automation. |
| Final helper smoke | Portable Apple Silicon helper and universal bundled helper passed `scripts/smoke-binary.py` | Embedded offline resources, verified TLS 1.3, private control boundaries, shares/ranges/quotas, clipboard, relay and organization. No physical LAN or browser UI claim. |
| Distribution | Universal macOS DMG, two native Windows ZIPs and two Linux archives created; all five SHA-256 values, exact ZIP/TAR entries, native PE resources, universal architectures, bundle signature/metadata and DMG integrity checked | Private ad hoc macOS and unsigned Windows previews. Package integrity is distinct from installed interaction or physical runtime. |
| Local Mac update | Installed and registered the final signed bundle as the sole `/Applications/AirLink.app` 0.6.0; signature verified and existing app-data file sizes/timestamps unchanged | The previous app was not running. No old app backup was left in Applications and no native GUI was launched. Installation is not interactive acceptance. |
| Hosted CI | Historical hosted run (withdrawn during privacy cleanup): hosted jobs did not execute | Local checks are recorded separately. This is not a passing hosted run or platform-runtime result. |
| Large-file evidence | The separate 0.5 single-file 100 GiB run below remains historical | It was not repeated for 0.6 and is not a physical-device or current-package 100 GiB claim. |
| Physical devices | A preliminary 0.5 Mac → Windows transfer report has no recorded size, hashes or interruption evidence | File size, end-to-end hashes, interruption behavior and exact Windows runtime are not recorded. The new 0.6 Windows app has not completed physical acceptance. |

Local evidence is ignored under `build/go-tests-0.6.jsonl`, `build/go-vet-0.6.log`, `build/dotnet-tests-0.6.log`, `build/web-tests-0.6.log`, `build/swift-tests-0.6.log`, `build/platform-build-0.6.log`, `build/macos-final-build-0.6.log`, `build/windows-publish-0.6.log` and the two `build/smoke-*-0.6.log` files. Package integrity and hosted CI observations above are from completed inspections; the hosted run is blocked rather than passing.

## Historical 0.5 evidence — 2026-10-24

| Area | Completed observation | Scope and limits |
| --- | --- | --- |
| Go | Final race run passed **107 top-level tests, 175 cases including subtests**; `go vet ./...` passed | Includes real released 0.3 and 0.4 helper migrations. Historical 0.1/0.2 helper cases and the old aggregate stress were skipped. |
| Source/state reliability | Nine new engine stabilization suites passed | Persisted filesystem identity, nanosecond timestamps, replacement detection, lazy validation of old cached digests, pause/Close cancellation, immutable hashes, late callback isolation, SQLite-write fault injection and restart durability. No startup full-file hashing or source copy. |
| HTTP recovery | Throttled/stalled body interruption, permission withdrawal, changed sources after limiter waits, interrupted ZIP output and permanent protocol/TLS classification passed | Actual HTTPS tests exercise pause/cancel, slot release, resumed uploads, no invalidated payload release, and stopping 400-class rejections at offer/chunk/complete instead of retrying forever. Temporary outages and pending 409 responses remain retryable. |
| Web | All **68 Node tests across seven files** and production build passed | Storage-denied pairing still completes in memory; host-authoritative share expiry tolerates client clock skew; unsupported paths are rejected before hashing/publishing. All prior upload/share/clipboard/relay/organization cases retained. No browser UI automation. |
| Native | All **37 Swift tests** passed against the final 0.5 helper; both production architectures compiled | Fake-helper cases cover failure/retry, old-session callbacks, immediate startup diagnostics, bounded/redacted stderr, hung shutdown with/without ACK, ignored TERM, a blocked input pipe, and in-flight send-selection preservation. Tests launch helper processes only. |
| Released-state migration | Real built 0.3 and 0.4 helpers -> 0.5 passed under race detection | Preserved identity/CA, pairing, receive paths, rates, disabled shortcut, password share/session/quota and received data/history. Explicit 0.4 duplicate-detection opt-in and editable normalized rules survive; older versions retain off/off defaults. Helpers were built from unchanged Git tags in ignored compatibility directories. |
| Single 100 GiB file | **107,374,182,400 bytes** transferred between two real 0.5 daemon processes through TLS 1.3, with pause/resume and SIGKILL/restart of each service; final hashes match | One sparse source fixture with nonzero markers was read in full and sent in 8 MiB protocol blocks into a separate received file. This is actual full-size loopback payload, not an offset-only sparse fixture or 100 separate files. Both services confirmed the full size; independent full SHA-256 reads of source and destination matched the engine's final hash. |
| Large-file recovery timing | Complete transfer/recovery/service verification in **442.62 seconds**; independent end comparison finished at **525.18 seconds** | Includes initial source hashing, pause at 128 MiB, receiver crash recovery around 33.36 GiB and source crash recovery around 66.73 GiB. Same-host storage/loopback evidence, not physical LAN speed or phone/browser behavior. |
| Large-file memory | Observed RSS peaks: source **58,015,744 bytes (55.33 MiB)**, receiver **68,157,440 bytes (65 MiB)** | Sampled each child process every 0.5 seconds; both below the 256 MiB target in this one-active-file run. Sampling is not an exact kernel high-water measurement and does not establish fully occupied duplex/concurrency memory. |
| Large-file integrity and cleanup | SHA-256 `3ac128e0840dbf84dae73fadafabe8fd88f9871075d3832c8134d0ae97b534dd`; temporary source/receiver/state removed after successful exit | Disk reserve and abort thresholds were enforced. Lifetime payload counters checkpoint periodically and lost recent samples at forced crashes; durable confirmed file receipts and final contents remained complete. These counters are not used as a substitute for file-integrity proof. |
| Exact tested binary | Portable macOS arm64 helper SHA-256 `4e330ce3eef7876ac1e4f6ece214d384b499367f791a47b5ec4efcc3f558d466` | The complete platform build reproduced this exact helper digest after the 100 GiB run. The universal bundled helper separately passed the final protocol smoke suite. |
| Packaged service | Portable arm64 service and bundled universal helper passed the expanded final smoke suite | Verified TLS/embedded offline assets, existing protected shares/ranges/quotas, clipboard, relay, organization and explicit duplicate decisions. All six service binaries and both native architectures compile. Package/hash/DMG checks passed. Windows/Linux cross-compilation remains distinct from runtime acceptance. |
| Dependency scan | Zero affected symbols/imported packages reported | Module-only GO-2026-5932 in unused x/crypto/openpgp remains. AirLink imports Argon2. |
| Hosted CI | Historical hosted run (withdrawn during privacy cleanup): hosted jobs did not execute | Local checks are recorded separately. This is not a passing hosted run or platform-runtime result. |
| Preliminary physical test | A Mac → Windows transfer was reported in 0.5 without recorded file size, independent hashes or interruption evidence | This does not establish current native Windows, mobile, Linux or Android acceptance. No GUI automation was used. |

The opt-in runner is `python3 scripts/acceptance-single-file.py build/airlink-darwin-arm64 --gib 100 --expect-version 0.5.0 --report build/acceptance-single-100gib-0.5.json`. A preceding 64 MiB run validated the interruption harness. Final-source evidence is ignored under `build/go-tests-0.5.jsonl`, `build/web-tests-0.5.log`, `build/swift-tests-0.5.log`, `build/platform-build-0.5.log`, `build/acceptance-single-100gib-0.5.log`, `build/acceptance-single-100gib-0.5.json`, the two `build/smoke-*-0.5.log` files, `build/package-0.5.log` and `build/govuln-0.5.log`.

The local single-file test closes that local evidence gap; it does not close physical device, Safari/download-manager, manual native UX or public-distribution gates in [RELEASE-READINESS.md](RELEASE-READINESS.md). The next manual checks cover macOS, Windows, iPhone and iPad, as described in [DEVICE-CHECK.md](DEVICE-CHECK.md).

## Historical 0.4 evidence — 2026-10-24

| Area | Completed observation | Scope and limits |
| --- | --- | --- |
| Go | Final `go test -race -count=1 -json ./...` passed **90 top-level tests, 119 cases including subtests**; `go vet ./...` passed | Includes the opt-in real 0.3 helper migration. The historical 0.1/0.2 helper cases and the 100 GiB stress were skipped. There are 24 engine, 49 server, seven relay, six share and four traffic suites. |
| Organization engine | Eleven new suites plus stale-candidate subcases passed | Default-off migration, rule normalization/order, immutable incoming snapshots, preserved folder hierarchy, destination collision/symlink rejection, equal-content detection, candidate deletion/replacement/same-metadata content changes, second full hash at Skip, independent index after history clear, metadata-only historical import, partial skipped-byte accounting, prepared-source identity and pause/cancel epoch guards. Sparse fixtures establish cancellation/offset behavior, not whole large-file delivery. |
| Protocol and sender | Seven new integration suites and their subcases passed, alongside existing transfer/share/relay regressions | Private candidate/destination redaction, owner boundaries, zero-payload Skip, mixed actual/avoided totals, organized share mapping, exclusion of skipped files, legacy completion HTTP 409 and source-independent Keep publication. Two-service multi-file Keep/Skip cases force revisiting a previously confirmed Skip; no re-open or false source failure occurs. |
| Private control | Bounded asynchronous decisions and out-of-order replies passed Go and Swift tests | Status and transfer cancellation remain responsive while a Skip waits for receive capacity; cancellation does not revive work. The native client has no fixed timeout for full candidate revalidation, while ordinary RPC requests retain their timeout. |
| Migration | Received data, identity, trust, settings, password share/session and quota survived a real released 0.3 helper -> current service upgrade | The final full race run used a headless helper built from the unchanged `v0.3.0` tag in ignored `build/compatibility-0.3/`; an earlier run also exercised the previously built 0.3 distribution helper. Existing received history becomes eligible for lazy duplicate checking, and both new opt-ins remain off. No app bundle was installed or launched. |
| Web | All **57 Node tests across six files** and the production build passed | Pending/Keep/Skip, lost preparation responses, retry/restart, mixed skip/send, cached checksums, legacy 404-only fallback, rule/path/UTF-8 validation, optional presets, and all prior share/clipboard/relay cases. No browser UI automation. |
| Native | All **26 Swift tests** passed against the final 0.4 helper | Additive defaults, organization validation and snapshots, private incoming decisions, paused/checking restrictions, 64-bit actual/avoided progress, separate statistics, real helper settings/share IPC, and deterministic out-of-order replies. Both native production architectures compile. No native GUI launch. |
| Final binary smoke | Portable Apple Silicon service and bundled universal helper both passed `scripts/smoke-binary.py` | Independently verified TLS 1.3 and embedded production resources; existing protected shares/ranges/quotas/revocation, clipboard and relay behavior; new opt-in sorting, private content checks, explicit reveal/Skip, no skipped placeholder, avoided-byte counters and sorted received-share capture. Headless local protocol evidence, not physical mobile/browser interaction. |
| Distribution | All six portable binaries compiled; universal macOS app/helper, DMG and four Windows/Linux archives created | App/helper report 0.4.0; bundle build 4, deep/strict ad hoc signature, Info.plist, both Mach-O architectures, all five SHA-256 package checks and DMG verification passed. Windows/Linux builds are cross-compilation evidence, not runtime execution. The written icon sources were preserved and compiled. |
| Dependency scan | Zero affected code symbols or imported packages reported | The module-only advisory GO-2026-5932 for unused `golang.org/x/crypto/openpgp` remains; AirLink imports Argon2. This is not a claim that all required modules are advisory-free. |
| Hosted CI | Historical hosted run (withdrawn during privacy cleanup): hosted jobs did not execute | Local checks are recorded separately. This is not a passing hosted run or platform-runtime result. |
| Interactive and large-file acceptance | No GUI automation, installation, physical cross-device acceptance or new 100 GiB/RSS run | Manual native organization, Finder/login/shortcut behavior, real network/platform filesystems, large single-file behavior and public signing/notarization remain open. The historical 0.1 stress below is not a 0.4 result. |

Final-source logs are ignored under `build/go-tests-0.4.jsonl`, `build/web-tests-0.4.log`, `build/swift-tests-0.4.log`, `build/platform-build-0.4.log`, `build/smoke-portable-0.4.log`, `build/smoke-bundled-0.4.log`, `build/package-0.4.log` and `build/govuln-0.4.log`. The final race run includes `TestUpgradeFromReleased030PreservesOrganizationMigration`; `build/upgrade-0.3-to-0.4.log` retains the earlier distribution-helper check. This remains a private development prerelease, separate from the physical 1.0 acceptance bar.

## Historical 0.3 evidence — 2026-10-24

| Area | Completed observation | Scope and limits |
| --- | --- | --- |
| Go | `go test -race -count=1 -json ./...` passed 71 top-level tests, 89 cases including subtests; `go vet ./...` passed | Thirteen engine, 41 server, seven relay-store, six share-store and four traffic suites. Optional 0.1/0.2 previous-helper cases and the 100 GiB stress were skipped in this ordinary run. |
| Relay | Seventeen new store/HTTPS suites passed, including three throttled-cancellation subcases | Explicit recipient acceptance, participant ACLs, request-bound PUT/ACK, two global streams, no persisted payload, source hash pinning, stale-operation isolation, pause/Range resume, delivered retries, restart, history visibility, >2 GiB offset, empty files/ZIPs and failed-archive HTTP abort. The large-offset fixture transports one byte, not a whole 3 GiB file. |
| Traffic/history | Global directional cap, live unlimited update, cancellation, lifetime counter restart, private scopes, filtering, source/share preservation and migration tests passed | Confirmed file bytes and unique relay delivery are separate from processed payload counters. Error bodies are excluded. Share expiry/revocation is checked after limiter waits. Daily groups describe last-activity history cohorts in UTC, not real daily wire accounting. |
| Web | Production build and all 45 Node tests passed | Metadata-only browser persistence, bounded source slices, demand-only chunks, lost ACK/retry, delivered Range service, role/acceptance gates, history and existing share/clipboard/upload regressions. No UI automation. |
| Native | All 18 Swift tests passed against the final 0.3 helper | Limits, disabled shortcut, additive relay/statistics/history types, share preservation and private IPC. Both arm64/x86_64 production builds and universal app/helper assembly passed; signature and Info.plist checks passed. |
| Final binary smoke | Portable Apple Silicon binary and bundled universal helper both passed `scripts/smoke-binary.py` | Real verified TLS: embedded assets, protected share/range/quota/revocation, clipboard, explicit-accept relay, source-demand ACK, delivered Range retry, rate settings, payload absence from state files, history clear and lifetime counters. This is headless protocol testing, not a physical phone/browser interaction. |
| Migration | Real built 0.2 helper -> current service compatibility test passed | Preserved identity/fingerprint, original password share, counted recipient session and receive directory; new unlimited rates/default shortcut applied. Log: `build/upgrade-0.2-to-0.3.log`. |
| Distribution | Universal macOS DMG and four Windows/Linux architecture archives created and checked | Ad hoc signature, DMG verification and SHA-256 integrity passed. Cross-compilation does not establish Windows/Linux runtime behavior. |
| Dependency scan | Zero affected symbols/imported packages reported | The module-level advisory for unused `golang.org/x/crypto/openpgp` remains; AirLink imports Argon2, not OpenPGP. |
| Hosted CI | Historical hosted run (withdrawn during privacy cleanup): hosted jobs did not execute | Local checks are recorded separately. This is not a passing hosted run or platform-runtime result. |
| Interactive acceptance | No GUI launch, installation, UI automation, actual shortcut registration, Finder invocation, login startup or physical cross-device acceptance performed | Those runtime checks remain open. Native keyboard recording/conflict handling is implemented and compiled, not physically demonstrated. |
| Large-transfer/RSS | No 100 GiB or new process-RSS benchmark performed for 0.3 | Two 8 MiB relay buffer slots are enforced and tested; that is not a measured total-process RSS claim. Historical 0.1 results below remain historical. |

Logs stay outside Git under `build/go-tests-0.3.jsonl`, `build/web-tests-0.3.log`, `build/swift-tests-0.3.log`, `build/platform-build-0.3.log` and `build/govuln-0.3.log`. The private 0.3 preview is not a claim that physical 1.0 acceptance is complete.

## Historical 0.2 evidence — 2026-10-24

| Area | Completed observation | Scope and limits |
| --- | --- | --- |
| Go service, share store, clipboard | `go test -race -count=1 -json ./...` passed 44 top-level tests, 57 passing cases including subtests; `go vet ./...` passed | Includes the existing transfer regression suite, six share-store suites, seven HTTPS share suites, clipboard ACL/deduplication/retention and native peer delivery. The optional 100 GiB stress and previous-helper compatibility case were skipped in this ordinary run. |
| Compatibility with released 0.1.0 | An additional opt-in test executed the real built 0.1.0 helper, created a pairing and received file, stopped it, then opened that state with the new 0.2 service | Identity/CA fingerprint, pairing cookie, receive folder, completed history and file bytes survived; new clipboard permission defaulted false; old received content could be shared. Log: `build/upgrade-0.1-to-0.2.log`. |
| Share protocol | Password hash/nonplaintext persistence, 12 concurrent recipient sessions against a limit of one, session reuse/restart/expiry/revocation, ownership and path privacy passed | Sparse >2 GiB fixture validates distant Range offsets, not a complete multi-GiB transfer. HEAD, 304 and invalid ranges do not consume quota; ordinary empty GET does. Active streams stop after revocation. |
| Web | 29 Node tests across four files and the production build passed | Covers isolated share routing, fragment removal/session-only storage, policy defaults/expiry, Unicode text bounds, existing upload recovery, local-console syntax and path handling. No browser UI automation. |
| Native Swift | All eleven tests passed against the final 0.2 helper | Includes real private IPC/share create/revoke, source preservation, backward-compatible decoding, UTF-8 limits, policy counts, and waiting for helper checkpoint/exit after shutdown acknowledgement. |
| Final binary smoke | Both portable Apple Silicon binary and bundled universal helper passed `scripts/smoke-binary.py` | Verified embedded assets over independently verified TLS 1.3, anonymous protected-share unlock/range/quota/revocation, empty-file invalid-Range regression, explicit clipboard grant/delivery/revoke. No GUI process launched. |
| Native builds | arm64 and x86_64 app and helper assembled; deep/strict ad hoc signature and Info.plist checks passed | Universal code compilation/package consistency, not interactive macOS acceptance or notarization. |
| Portable builds and packaging | All six service binaries cross-compiled; universal DMG and four portable archives created | Windows/Linux runtime execution remains open. All five package SHA-256 checks and `hdiutil verify` passed. |
| Dependency scan | `govulncheck` reported zero affected code symbols and zero affected imported packages | One module-level advisory, GO-2026-5932, covers the unused `golang.org/x/crypto/openpgp` package. AirLink imports Argon2, not OpenPGP. This is not a claim that every module is advisory-free. |
| Hosted CI | Historical hosted run (withdrawn during privacy cleanup): hosted jobs did not execute | Local checks are recorded separately. This is not a passing hosted run or platform-runtime result. |
| Native GUI / physical device runtime | No 0.2 GUI launch, installation, UI automation or physical cross-device acceptance performed | Browser interaction, native UI/clipboard presentation, real LAN/mobile behavior and the runtime scenarios below remain open. |
| Large-transfer stress | No 100 GiB test was repeated for 0.2 | The 0.1 aggregate result below is historical evidence only. |

Local logs are outside Git under `build/go-tests-0.2.jsonl`, `build/web-tests-0.2.log`, `build/swift-tests-0.2.log`, `build/platform-build-0.2.log` and `build/govuln-0.2-verbose.log`. The private 0.2 package remains a development prerelease; these observations do not complete the physical cross-device 1.0 acceptance.

## Historical 0.1 evidence — 2026-10-24

| Area | Observed evidence | What it establishes |
| --- | --- | --- |
| Native Swift tests | Six tests passed, including the actual helper integration and system-language regression case | Models, large byte counts, formatting, language fallback, private subprocess RPC, and actual status/settings integration |
| Native build | arm64 and x86_64 release builds succeeded; universal app and helper assembled | Both architectures compile; minimum native deployment target is macOS 14 |
| Bundle validation | Deep/strict code-sign verification and Info.plist lint passed; bundled helper reported 0.1.0 | Internal ad hoc bundle consistency, not Developer ID distribution or notarization |
| App icon | Default, dark, and tinted variants rendered and visually inspected; `actool` compilation succeeded | Valid written SVG / Icon Composer sources and compiled icon resources |
| Web | Fourteen background tests and the final production build passed | Protocol/source-resume behavior, empty files, lost responses, long verification, and offline bundle validity |
| Transfer engine | Twelve top-level tests passed with the race detector, including duplex scheduling and verification cancellation | Durable resume, corruption, direction-specific queues, unsafe paths, no-overwrite publication, changed sources, crash recovery, empty entries and directories |
| HTTPS service | Thirteen top-level tests passed with the race detector | Browser/native pairing, durable reciprocal credentials, direct service transfers, download ranges, stdin/stdout control, authorization, certificate pins, redirects, redaction, and bootstrap isolation |
| 100 GiB aggregate streaming | Passed: 107,374,182,400 bytes, 100 sequential 1 GiB files, 265.16 seconds for transfer/verification, 386.2 MiB/s local-loopback average | Actual TLS payload and full received-file SHA-256 verification; not one 100 GiB file or physical-network throughput |
| Stress memory | `/usr/bin/time -l` reported 245,366,784 bytes (234 MiB) maximum RSS for the test command | Command-tree measurement includes the test server/client and build runner; no standalone daemon or four-way concurrency memory claim |
| Final binary smoke | Portable macOS binary and bundled universal helper both served embedded production assets through verified TLS 1.3; unauthenticated private status was rejected | Packaged offline web resources, native process control, CA fingerprint and public bootstrap are connected in the final binaries |
| Release packaging | Universal DMG, Windows amd64/arm64 ZIPs, Linux amd64/arm64 archives created; all SHA-256 checks and DMG verification passed | Package integrity and cross-compilation; not Windows/Linux runtime execution |
| Hosted CI | Historical hosted run (withdrawn during privacy cleanup): hosted jobs did not execute | Local checks are recorded separately. This is not a passing hosted run or platform-runtime result. |
| Native interactive runtime and physical cross-device transfers | Not performed in the current verification session | No claim about physical LAN speed, mobile background execution, Finder runtime, login startup, or notification presentation |

The final background Go run was `go test -race -count=1 -json ./...`: 25 top-level tests passed (38 passing cases including subtests); the opt-in stress case was skipped in this ordinary run and passed separately as above. `go vet ./...` passed. Logs are retained locally outside Git in `build/go-tests.jsonl` and `build/streaming-100gib.log`. The aggregate stress exercised the receive path before the final duplex scheduling and HTTP hardening refinements; the final race suite then passed with those refinements. It is not an identical-final-binary 100 GiB acceptance run.

Final binary smoke commands: `python3 scripts/smoke-binary.py build/airlink-darwin-arm64` and `python3 scripts/smoke-binary.py build/AirLink.app/Contents/Helpers/airlink`. This uses Node's TLS 1.3 client because the Apple-bundled Python SSL library may lack TLS 1.3. No GUI process is launched.

These observations belong to the private 0.1.0 development prerelease. Do not treat them as a 0.2/0.3/0.4 rerun, and do not treat a compiled binary as a completed device test. Full cross-device release acceptance remains open below.

## Large-transfer stress case

The opt-in service test writes real payload bytes through HTTPS and verifies each received file:

```sh
AIRLINK_STRESS_GIB=100 go test ./internal/server \
  -run '^TestStreamingAcceptance$' -count=1 -v -timeout=90m
```

The value counts **sequential 1 GiB files**, each transferred in 8 MiB chunks. Completed destination data is removed after each file to limit disk usage. A 100 setting represents **100 GiB aggregate payload**, not a single 100 GiB file. It is a local host test, not a measurement of Wi-Fi, a physical Windows-to-Mac connection, or phone download behavior. Record actual verified bytes and elapsed time from the completed output before claiming success.

## Acceptance scenarios still requiring runtime evidence

- Real Windows ↔ macOS, Linux ↔ macOS, and desktop ↔ Android/iOS transfers, including nested folders and a large single file.
- Local network interruption, device sleep, process restart, changed addresses, revoked access during active work, and deliberate user pause/resume.
- Browser reload with source reselection, mobile foreground/background transitions, browser-managed individual downloads, and streamed folder ZIPs.
- Disk full, unavailable destination, source change, filesystem name/permission differences, and checksum failure without premature publication or overwriting.
- Native window/menu access with active direct and relay work, modern/legacy Finder Services, explicit shortcut recording/disable/conflict recovery, login-item status/approval/startup, notification authorization/presentation, and appearance variants.
- DMG installation/update preserving state, first launch under normal Gatekeeper settings, and a future Developer ID/notarized package.
- Quick Share recipient certificate onboarding, fragment-link handling, optional passwords, expiry/revocation during downloads, session-based quotas across multiple files/Range/resume, and access limited to the chosen share.
- Paired-browser ownership boundaries for creating/revoking shares from completed uploads, source unavailability/change without copying originals, and share persistence after host restart.
- Explicit clipboard Paste/Edit/Send/Copy/Dismiss on native and browser clients, permission disabled for migrated pairings, permission on both desktop peers, UTF-8 size limits, and in-memory retention without automatic system clipboard access.
- Two authorized browser participants through a single desktop host, mandatory recipient acceptance, source re-selection after access loss, simultaneous relay streams, host/tab/network restart, Range retry, and ZIP fallback without stored host payload.
- Aggregate independent send/receive caps across normal files, shares and relays, including live changes and expiry/revocation during throttled output. Keep file-confirmed, relay-transport and lifetime payload totals distinct.
- Optional organization on actual platform filesystems: default-off upgrades, private duplicate Keep/Skip/reveal actions, changed or removed candidates, extension-rule ordering, whole-folder preservation, new-offer-only snapshots, and separate actual/avoided byte displays.
- History filtering/clearing for both relay participants without deleting the other party's association, preserving originals/received content/shares/active work, and interpreting daily groups as last-activity UTC rather than actual daily traffic.

These checks must be described at their actual scope. They do not authorize adding desktop-controlling UI tests.

## Continuous integration

The workflow runs background Go/race, web, and native checks on hosted runners and compiles portable platform binaries. Native Swift checks include the real helper subprocess contract, without launching the GUI. The macOS test job does not require Icon Composer or attempt a DMG/notarization workflow; complete icon and app packaging is verified through the local macOS build script.

Hosted workflow success must be observed on GitHub before it is reported as green. Artifact publication, GitHub releases, signing, and notarization are separate steps.
