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

## Current evidence — 2026-10-24

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

Do not treat a compiled binary as a completed device test. The private 0.1.0 package is a development prerelease; full cross-device release acceptance remains open below.

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
- Native window closure/menu bar access, Finder Services, the fixed shortcut, login-item approval/startup, notification authorization/presentation, and all appearance variants.
- DMG installation/update preserving state, first launch under normal Gatekeeper settings, and a future Developer ID/notarized package.

These checks must be described at their actual scope. They do not authorize adding desktop-controlling UI tests.

## Continuous integration

The workflow runs background Go/race, web, and native checks on hosted runners and compiles portable platform binaries. Native Swift checks include the real helper subprocess contract, without launching the GUI. The macOS test job does not require Icon Composer or attempt a DMG/notarization workflow; complete icon and app packaging is verified through the local macOS build script.

Hosted workflow success must be observed on GitHub before it is reported as green. Artifact publication, GitHub releases, signing, and notarization are separate steps.
