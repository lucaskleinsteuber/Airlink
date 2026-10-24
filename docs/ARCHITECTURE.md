# Architecture

AirLink 0.3 extends the shared local transfer service with browser relay, aggregate payload limits, and richer history/statistics, retaining Quick Share and explicit clipboard text. Native and browser interfaces use the same local host. Runtime resources are bundled; no hosted signaling service, external relay, account, or online asset server is required. Additions are specified in [API-0.2.md](API-0.2.md) and [API-0.3.md](API-0.3.md).

```mermaid
flowchart LR
  Mac[Native macOS app] <-->|Private stdin / stdout| Host[Local Go service]
  Browser[Desktop or mobile browser] <-->|HTTPS + SSE| Host
  Peer[Paired desktop Go service] <-->|Pinned TLS / direct transfer| Host
  Recipient[Share recipient browser] -->|HTTPS / share session| Host
  SenderTab[Paired source browser] -->|Requested RAM blocks| Host
  Host -->|Bounded relay stream| ReceiverTab[Paired recipient browser]
  Host --> State[Private settings + identity + SQLite checkpoints]
  Host --> Destination[Selected receive directory]
  Source[Selected source files] -->|Read in bounded chunks| Host
```

## Transfer engine

`internal/engine` owns transfer state and file I/O. Manifests identify relative file paths, sizes, timestamps, directory entries, and file IDs. Byte counts and offsets use signed 64-bit integers. Data moves in 8 MiB chunks, with a shorter final chunk when necessary.

Each uploaded chunk carries a SHA-256 checksum. Confirmed chunks and transfer metadata are persisted in SQLite with WAL and full synchronization. Retransmitted confirmed chunks are idempotent; unapproved, out-of-order, changed, or inconsistent data is rejected. Startup recovery reconciles file data with confirmed checkpoints rather than trusting a sender's local progress estimate.

Incoming files remain partial until the receiver verifies the complete file. Publication uses an atomic operation that refuses to overwrite an existing destination. Relative path validation and filesystem-root operations constrain received content to its destination. Source manifests reference original files and verify their identity before reuse; sending does not move or replace them.

The queue has independent pools of two incoming and two outgoing transfers. Verification counts against its direction's pool. This avoids a deadlock when both hosts have incoming offers waiting for each other's outgoing queues. Chunk I/O is additionally bounded to two incoming requests. Browser relay has its own two-file-stream bound and separate metadata, so a relay is not duplicated as a stored direct transfer.

## Browser relay

`internal/relay` persists immutable manifests, acceptance/state, confirmed block hashes, and delivered byte-range metadata. Payload blocks stay in bounded RAM only; the host never writes a complete relay source or destination file. At most two active relay file streams request 8 MiB blocks, with a shorter final block. This bound is not a claim about total process RSS: HTTP, hashing, runtime, and metadata allocations remain separate.

Both browsers pair with the same host. A separate `canRelay` permission defaults false for existing and new devices. The sender also needs `canSend`; the receiver needs `canReceive`. Only authorized online browser peers appear in a guest's relay choices, exposing names/IDs rather than addresses or credentials. The recipient explicitly accepts each new offer. The host may pause/resume/cancel but does not auto-accept for a guest; resuming cannot bypass acceptance. Permission changes and revocation interrupt affected activity.

The source hashes each file before the immutable offer and answers unpredictable receiver-driven requests for a specific file/chunk. Unsolicited, stale, wrong-owner, or changed confirmed blocks are rejected. Chunk acknowledgments describe consumption by the receiver transport. A normal full stream verifies the complete digest before releasing its final bytes; partial ranges validate blocks/source identity without asserting a whole saved file.

HEAD and invalid ranges do not request source bytes or advance delivery. Individual downloads support a stable ETag and a single Range; ZIP64 preserves path structure/empty directories with no archive-resume promise. `delivered` means all relevant byte ranges reached the browser transport, not that its download manager saved or verified the filesystem copy.

After host/tab/network interruption, metadata survives but buffered data does not. Both tabs must return, source file access may need reselection, and the receiver may need to resume/restart its download. No fallback silently stages a complete file on the host.

## Payload limits and history

`internal/traffic` applies independent shared token buckets for host send and receive payloads. The persisted setting is zero for unlimited, otherwise 1,024 through 1 TiB/s. Normal file transfers, public share payloads, and relay I/O use the same directional budget. Payload metering excludes control and HTTP error response bodies. Share policy/source checks run after throttling waits and before output so a limit cannot defer delivery past expiry/revocation checks.

Lifetime host payload counters persist separately from history and survive history clearing. Confirmed direct-file bytes, unique relayed transport bytes, and relay-delivery counts remain distinct. Guest statistics are computed only from owned/participant records and omit host-wide traffic counters.

History combines transfer and relay records newest first, with case-insensitive filename/device search and state/direction/device filters; the combined result limit defaults to 100 and is capped at 500. Clear removes terminal metadata only. Host control may clear all terminal records; a guest removes only its own terminal association, preserving the other relay participant's history. Originals, received files, active work, shares, and clipboard messages are not deleted.

Statistics describe the remaining history. Daily groups put each record's accumulated bytes/counts under its latest activity date in UTC (falling back to creation if unavailable); they are not actual per-day byte deltas. Resuming an old record can move its accumulated history total to a later day. Clearing history can reduce these summaries while lifetime payload counters remain unchanged.

## Service and trust

`internal/server` provides the HTTPS API, SSE status events, pairing, discovery, outgoing desktop transfers, and private local control. The wire protocol is documented in [API.md](API.md).

- **LAN transfer endpoint:** TLS 1.3, device authentication, same-origin/Host checks, and authorization for each transfer. Browser responses expose only that device's authorized transfer data.
- **Certificate bootstrap endpoint:** a separate HTTP listener serving public certificate material and setup instructions. It grants no access to transfer or administrator functions. The user compares its fingerprint with the local host display before trusting the certificate.
- **Local administrator console:** loopback only, protected by a per-run secret setup URL. Native macOS control uses JSON-lines stdin/stdout instead of exposing administrative powers on the LAN.

Each host keeps a local CA identity and issues a server certificate covering its local addresses. Desktop pairing pins the CA fingerprint and still verifies the certificate chain, hostname, expiry, and intended use. Discovery advertises presence through `_airlink._tcp`; discovered identity information must be confirmed out of band before pairing.

Browser sessions use Secure, HttpOnly, SameSite cookies. A browser's pairing request grants no access until approved locally. Device send/receive rights and auto-accept are independently managed; revocation invalidates future authenticated access and cancels associated active work.

## Quick Share

`internal/shares` stores share policy, ownership, original source references, and download-session state in a separate SQLite database. Local administration can capture selected original paths or an eligible existing transfer. A paired browser may supply only its own completed received transfer; it cannot choose arbitrary host paths or manage another device's shares. Source identity, size, and modification time are checked before serving and during active reads. Mismatches or unavailable originals fail without staging a whole-file copy. This is not a frozen content snapshot or a promise to detect arbitrary same-file writes that preserve all checked metadata; shared originals must remain unchanged.

Links use `https://HOST/s/ID#SECRET`. The recipient app reads the fragment into session-scoped storage and removes it from the visible address. Unlock sends the secret and optional password over HTTPS; the secret never becomes an HTTP URL or referrer. Public pages disclose no filenames before successful authorization. The anonymous grant is read-only and limited to one share, independent of device pairing and local administrator privileges.

Optional passwords are hashed with Argon2id. Unlock attempts and concurrent password derivation are bounded. A successful unlock establishes a Secure, HttpOnly, SameSite share-scoped cookie; it does not consume download quota. The first successful body write atomically claims one session allowance. Additional files, Range requests, and retries in that session reuse the allowance; HEAD and invalid ranges do not consume it. Session counters and share ownership persist across restart. Private owner status can reconstruct the link, while anonymous status never returns the secret.

UI defaults are a 24-hour expiry, no password, and unlimited downloads. The service supports no expiry and explicit maximum downloads. Revocation is persistent; both revocation and expiry are checked during streaming to stop further output. A new share does not bypass host certificate trust or expose an internet-accessible relay. Individual downloads support Range, while ZIP64 downloads are streamed without resumable archive support.

## Clipboard text

Clipboard is an explicit message exchange, not background synchronization. Each paired device has a separate `canClipboard` permission, defaulting to false when the field is absent from an older saved record. File permissions do not grant clipboard permission. Desktop-to-desktop messages travel through the pinned TLS API and require both hosts to enable the permission; local success is recorded after the remote AirLink service acknowledges acceptance.

Text must be valid UTF-8 with 1–65,536 bytes. The service keeps at most 100 recent messages in process memory, removes messages older than one hour, clears them on shutdown, and removes a device's messages on revocation. Text is never written to the application databases or automatic transfer history. An explicit Copy action writes the selected text to the operating system clipboard; arrival alone never reads or replaces it.

Native and browser UI provide Paste, editable text, Send, preview, Copy, and Dismiss. The native clipboard feature reads `NSPasteboard.general` only from Paste and writes it only from a user-selected Copy action. Browsers without the clipboard API retain selectable text and manual paste. Device-scoped API filtering prevents one browser from reading another pairing's messages.

## Native macOS interface

The Swift package in `macos/` separates Codable protocol models and subprocess transport from SwiftUI/AppKit presentation. The app launches its bundled Go helper with private pipes, polls status once per second, and sends local control commands through correlated JSON-lines requests. Status refresh is serialized, and a single asynchronous reader preserves response byte order.

The app remains available in the menu bar after window closure. The menu and quit warning include active host relays as well as direct transfers. Explicit quit awaits helper checkpoint/process cleanup. File selection, drag-and-drop, and user-invoked Finder Services provide source URLs, with a legacy filename-list fallback; the helper handles the transfer. Login startup uses `SMAppService`. Status is read again when Settings opens or the app becomes active, using a user-action binding so refresh never registers/unregisters a login item. Notification authorization is requested only after a user action.

Compact share/clipboard sheets remain, with new history/statistics and relay oversight sheets. Shares need no paired target; the host relay view has no recipient-accept button. Additive Codable fields tolerate older status/device/settings fixtures, including false relay permission, unlimited rates, and the migrated enabled ⇧⌘L shortcut unless an explicit disabled setting exists. Version labels come from helper status or app bundle.

Shortcut settings persist Carbon key codes/modifier bits (default key code 37, modifiers 768). The Settings recorder receives local key events only while explicitly focused; no global keystroke monitor is installed. Command or Control is required, with optional Shift/Option. The current hotkey is suspended during capture and restored afterward. A replacement is registered before the old registration is removed; failure keeps the working registration, shows the actual registration state, and rolls back the conflicting preference. Quitting unregisters the hotkey and event handler. These paths compile and have pure model tests; actual OS registration/conflicts, Finder interaction, and login startup have not been exercised through a native GUI session.

The app icon consists of written SVG layers and `AppIcon.icon/icon.json`. Icon Composer renders appearance variants headlessly; `actool` produces compiled icon assets and an older-system `.icns` fallback. No image generator is part of this process.

## Browser interface

The React/TypeScript interface in `web/` is compiled into static assets embedded in the Go binary. A worker performs incremental SHA-256 hashing with bounded input blocks. IndexedDB stores resume manifests and integrity evidence; browser memory retains source `File` objects while access remains available.

Reconnection reconciles the host's confirmed offsets. After reload or loss of file access, the user selects the same unchanged sources again. This requirement avoids pretending that every browser grants persistent filesystem access.

Individual downloads use HTTP Range/ETag support; ZIP64 folder downloads stream without a resumable-archive promise. The browser download manager owns saving and completion reporting. A download initiation acknowledgment is never treated as proof of saved or integrity-verified bytes on the client.

Paired browsers use the connected host for direct uploads/downloads or for authorized browser-to-browser relay. Anonymous share recipients use a separate route without device pairing. Both modes retain the certificate onboarding and browser file-access/download-manager limits.

## Persistence and packaging

Host settings, identity, and device records live in a private OS-specific AirLink configuration directory; SQLite stores transfer/relay metadata and durable share/session data, while traffic counters persist separately. Authorized share owners can recover links after restart, so link secrets remain private local application data. Relay payload blocks and clipboard messages are deliberately excluded from persistence. Received files live separately in the selected destination. A data-directory lock prevents two services from operating on the same local state concurrently.

The build pipeline compiles the web interface, embeds it, and cross-compiles portable Go services. On macOS it creates a universal native app with a universal helper and the compiled icon. Packaging creates platform archives, a DMG, and checksums. App replacement must preserve the separate state and receive directories.

The current private macOS build uses ad hoc signing. Developer ID signing, notarization, physical cross-device acceptance, and public distribution remain separate delivery gates.
