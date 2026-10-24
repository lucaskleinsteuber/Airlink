# Testing and verification

AirLink uses background unit, protocol, subprocess, filesystem, and HTTPS integration tests. The test suite does not automate Finder, click application windows, launch a simulator, or take control of the user's desktop. Interactive runtime acceptance is recorded separately from automated test results.

## Run the checks

Install the development dependencies described in [README.md](../README.md), then run from the repository root:

```sh
npm --prefix web ci
bash scripts/test.sh
```

The script runs Go tests with the race detector, web tests, and the production web build. On macOS it also compiles the native target and runs Swift Testing. An ordinary native test run skips the actual-helper integration case until a helper path is supplied.

To build the complete embedded application and portable binaries:

```sh
bash scripts/build.sh
```

On macOS, run the native tests against the built Go helper:

```sh
AIRLINK_TEST_HELPER="$PWD/build/airlink-darwin-arm64" \
  swift test --package-path macos --scratch-path build/swift-tests
```

Choose the matching architecture on an Intel Mac. The integration case starts only the helper process with temporary state and receive directories and discovery disabled. It does not launch the native app.

Set `AIRLINK_GO=/absolute/path/to/go` for the shell scripts if needed. For a direct Go command, invoke that Go binary explicitly.

## 0.3 evidence — 2026-10-24

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
| Hosted CI | Hosted jobs did not execute | Local background evidence is recorded separately. |
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
| Hosted CI | Hosted jobs did not execute | Local background evidence is recorded separately. |
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
| Hosted CI | Hosted jobs did not execute | Local background evidence is recorded separately. |
| Native interactive runtime and physical cross-device transfers | Not performed in the current verification session | No claim about physical LAN speed, mobile background execution, Finder runtime, login startup, or notification presentation |

The final background Go run was `go test -race -count=1 -json ./...`: 25 top-level tests passed (38 passing cases including subtests); the opt-in stress case was skipped in this ordinary run and passed separately as above. `go vet ./...` passed. Logs are retained locally outside Git in `build/go-tests.jsonl` and `build/streaming-100gib.log`. The aggregate stress exercised the receive path before the final duplex scheduling and HTTP hardening refinements; the final race suite then passed with those refinements. It is not an identical-final-binary 100 GiB acceptance run.

Final binary smoke commands: `python3 scripts/smoke-binary.py build/airlink-darwin-arm64` and `python3 scripts/smoke-binary.py build/AirLink.app/Contents/Helpers/airlink`. This uses Node's TLS 1.3 client because the Apple-bundled Python SSL library may lack TLS 1.3. No GUI process is launched.

These observations belong to the private 0.1.0 development prerelease. Do not treat them as a 0.2/0.3 rerun, and do not treat a compiled binary as a completed device test. Full cross-device release acceptance remains open below.

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
- History filtering/clearing for both relay participants without deleting the other party's association, preserving originals/received content/shares/active work, and interpreting daily groups as last-activity UTC rather than actual daily traffic.

These checks must be described at their actual scope. They do not authorize adding desktop-controlling UI tests.

## Continuous integration

The workflow runs background Go/race, web, and native checks on hosted runners and compiles portable platform binaries. Native Swift checks include the real helper subprocess contract, without launching the GUI. The macOS test job does not require Icon Composer or attempt a DMG/notarization workflow; complete icon and app packaging is verified through the local macOS build script.

Hosted workflow success must be observed on GitHub before it is reported as green. Artifact publication, GitHub releases, signing, and notarization are separate steps.
