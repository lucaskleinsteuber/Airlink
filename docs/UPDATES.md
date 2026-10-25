# Native update delivery

AirLink 0.9 has explicit native **Check for Updates…** actions. The macOS action is in the AirLink application menu and menu-bar panel; Windows exposes it in Settings and the tray menu. An app predating the updater needs one manual installation first.

Checks initialize lazily after that action. No scheduled check, automatic download or system profiling is enabled. LAN file transfers remain usable without internet. Token-free GitHub delivery requires the active repository, feeds and download assets to be publicly accessible. A private/missing feed is unavailable; it must not be reported as evidence that the installed version is current. No GitHub PAT is bundled.

## Trust and lifecycle

Both clients pin the Ed25519 identity in `updates/public-keys.json`. macOS uses Sparkle 2.10 with a mandatory nonexpiring signed feed and archive verification before extraction. Windows uses NetSparkle 3.1 with mandatory feed and installer signatures, native architecture/version/exact-URL filtering, bounded unauthenticated metadata reads and no legacy signature fallback. Windows also reverifies the installer before handoff.

Sparkle uses the normal macOS quit event, so AirLink's existing delegate awaits helper checkpoint/shutdown before replying. Windows asks about active transfers and requires graceful helper exit before its fixed-PID waiter starts the verified NSIS Setup. That update path does not force-terminate a hung helper. A failed handoff restores the running window's usable state. Physical install/relaunch behavior still needs the acceptance evidence in [RELEASE-READINESS.md](RELEASE-READINESS.md).

These update signatures are separate from Developer ID, notarization or Windows Authenticode. The development packages retain the documented ad hoc/unsigned OS-signing status.

## Publishing a version

1. Update MAJOR.MINOR.PATCH and the monotonically increasing macOS bundle build. Build/test the final source and create the three native packages: the universal macOS DMG and Windows x64/ARM64 Setup. Keep the packages immutable after this point. Linux is an experimental source-only target until runtime acceptance is recorded.
2. Sign the exact packages and generate the feeds with the local Go tool, for example:

   ```sh
   go run ./cmd/airlink-release sign --version 0.9.0 --build 11 --dist dist --updates updates
   go run ./cmd/airlink-release verify --version 0.9.0 --build 11 --dist dist --updates updates
   ```

3. Verify the actual feeds/packages with the official Sparkle CLI and the Windows Core `--verify-update-release` entry point. Review/stage only the intended source, public configuration and generated XML/signature files.
4. Publish the matching Git tag, the three native packages, `UPDATE-SIGNATURES.json` and `SHA256SUMS.txt`. Verify remote sizes/digests. Feed URLs point to the versioned GitHub assets, so do not overwrite a different binary under an existing signed version.
5. Verify anonymous HTTPS feed/download access. The active repository is public with owner approval; the earlier archive remains private. Publishing source to a private repository would not make its raw feed public.

The generator writes one signed macOS appcast, two Windows architecture appcasts and their detached signatures. Its manifest records SHA-256, length and Ed25519 signatures for the three native packages and exact feeds. Public-only verification needs no private material. The checksum file includes the three packages and signature manifest; the manifest records feed verification data separately.

For the owner-approved removal of untested Linux downloads from an already signed release, `native-only` verifies retained data and removes only the Linux manifest/checksum records. It preserves the publication time, native package bytes/signatures and every feed/signature byte, and needs no private key. It never rebuilds or silently relabels an existing signed package.

## Publishing key

The persistent 32-byte Ed25519 seed is stored as base64 in `~/Library/Application Support/AirLink Publishing/ed25519.seed`, with directory mode `0700` and file mode `0600`. The tool refuses private storage in Git checkouts, including symlink-routed paths, and does not silently rotate a missing/mismatched established key. Source/application builds use only the public configuration.

Keep a secure backup outside Git and distribution artifacts. Public configuration, Git history and package signatures cannot recreate a lost private seed. Do not print the seed, pass its contents in command arguments, upload it as an asset, or substitute a fresh key for clients that already trust the established one. Moving signing to CI requires a separate decision about encrypted secret storage.
