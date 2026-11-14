# Signed beta updates

The 0.12 beta has five current channels: `macos-arm64.xml`, `macos-x64.xml`, `windows-x64.xml`, `windows-arm64.xml` and `linux-x64.json`. Their public addresses are pinned to `lucaskleinsteuber/Airlink` in `updates/public-keys.json`. The historical `macos.xml` catalog remains separate for legacy verification. Old installed 0.11 clients require one manual 0.12 installation to use the new channels.

The existing Ed25519 public key remains unchanged. The publishing seed lives outside every Git checkout, is never embedded in applications and is never printed or sent to CI. Applications have no GitHub access token. Transfer-host identities and update-signing identities are separate.

## User policy

Manual Check for Updates works from the app/tray menu and Settings → Updates. Automatic beta notices are off by default, require saved consent, and check metadata at most once every 24 hours. Downloads and installation begin only after a click. Network failures do not interrupt local transfers or imply that the installed version is current.

macOS uses Sparkle 2.10 with signed-feed requirements, verification before extraction, no profiling and no automatic downloads. Each app contains its architecture-specific feed URL. Windows uses NetSparkle 3.1 with a strict signature verifier and a filter binding the numeric product version, displayed beta version, OS, architecture, exact GitHub package URL and bounded length. A graceful helper stop is required before installer handoff. Installer launch failure restores the service/retry path.

The Linux client verifies signed JSON before reading release metadata. Each artifact binds package type, canonical version/URL/name, byte length, SHA-256 and Ed25519 signature. All four Linux packages must appear in a valid catalog. The updater selects only a newer compatible version. The AppImage replacement helper independently checks the pinned public key, complete package, x64 type-2 ELF identity, target ownership and the requesting process identity. It acknowledges readiness before the app exits, waits for that exact process to exit, stages and verifies the replacement in the target directory, then renames atomically and relaunches. An unsuccessful service stop or readiness handshake retains the old app. Only exact owned cache files are removed after a successful handoff.

Native Linux packages remain under package-manager control. AirLink downloads and verifies a selected package and shows a literal installation command. It does not overwrite package-owned application files or start a privileged package manager.

## Versions and release contents

Starting with 0.13.0, `VERSION` is a plain `Major.Minor.Patch` (0.13.0, 0.13.1, 0.14.0 …); `BUILD_NUMBER` keeps increasing across OS builds. Whether a release is a beta is decided only by publishing it as a GitHub **pre-release**; the version number carries no `-beta.N` suffix. All updaters still accept older `-beta.N` versions, which sort before the release with the same number (0.12.0-beta.3 < 0.12.0 < 0.13.0). macOS short version and Windows numeric fields (`Major.Minor.Patch.Build`) are unchanged in form. The read-only `scripts/check-version.py` checks that the source representations agree.

Linux packages use DEB `0.13.0-1`, RPM `0.13.0-1` and Arch `0.13.0-1`. Legacy betas map to `0.12.0~beta.3-1` and `0.12.0beta3-1`, so every package manager upgrades from them. Lifecycle tests install the candidate, then a synthetic next patch version with the same payload. No epoch is introduced.

New catalogs describe exactly eight app packages and five feeds. The schema-2 `UPDATE-SIGNATURES.json` records exact package/feed hashes, lengths and signatures; its detached `.signature` signs the whole manifest. `SHA256SUMS.txt` covers every app package, the manifest and its signature. Mac feed signatures are embedded using Sparkle's signed-feed format; Windows and Linux use detached `.signature` files. The historical three-package schema-1 tests and public-only metadata curation remain separate; old tags/assets are never rewritten by beta publication.

## Candidate publication

1. Build/test packages and audit the source, reachable history and decoded package payloads. Inspect binaries, license material, embedded web resources, native metadata and archive ownership.
2. Sign the sealed packages with the existing local publishing key into a staging output. Public-only verification must accept the exact eight-package set, all feeds, manifest signature and checksum list.
3. Verify Mac feeds/packages using Sparkle's actual public-only command-line verifier, Windows feeds/Setup files with NetSparkle, and Linux feeds/packages with the Qt/Go verifiers. Synthetic signed successors exercise the update path without a real desktop launch.
4. Publish only the approved active repository. Upload a beta as a draft, then mark it as a GitHub pre-release; fetch assets and verify size/hash/signature before making the release available. Stable releases use the same numbering without the pre-release flag.
5. Publish the tracked update feeds only after matching release downloads are reachable. Check anonymous access and exact bytes. Keep the private archived predecessor private and archived.

This pipeline proves cryptographic compatibility, headless handoff/installation logic and actual uploaded bytes. It does not prove a manual native updater interaction or desktop relaunch under KDE/macOS; those remain device checks. macOS beta downloads are ad hoc signed and not notarized. No app is installed into the developer's Applications directory by automated checking.
