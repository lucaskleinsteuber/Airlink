# AirLink protocol 3

Protocol 3 replaces the per-block HTTPS API of 0.12 and earlier. It is not compatible with older versions; both devices need AirLink 0.13 or newer.

## Connection

- One TCP port (default 43871) serves TLS 1.3. ALPN `airlink/3` selects a peer session; HTTP/1.1 on the same port only serves `GET /api/v1/info` and the comparison-code pairing (`/api/v1/pair/v2/*`).
- Every device has a self-signed P-256 identity root; its SHA-256 fingerprint is the device identity. A short-lived leaf (server and client authentication) is signed by the root and renewed when local addresses change.
- A session is mutually authenticated: the client presents its leaf chain, the server verifies the chain against its own root and requires the root fingerprint to belong to a paired device. The client pins the paired fingerprint of the server the same way. Host names are not used for trust.
- After the handshake both sides send a `hello` (device ID, name, version, protocol, a per-start boot ID and the listening port). The device ID must match the paired record of the verified fingerprint.
- Each pair of devices keeps one session. Either side may dial; the other side uses the same connection, so a device that cannot accept incoming connections still sends and receives. If both dial at once, the connection dialed by the lower device ID survives on both sides; a hello with a new boot ID replaces a stale session of a restarted device.
- Both sides send a heartbeat every 15 s; a session without received data for 50 s is closed. Dropped sessions are re-established with backoff (1 s doubling to 30 s) and immediately when work is queued or the device is discovered again.

## Framing

`kind(1) reserved(3, zero) stream(4) length(4)` big endian, then the payload.

- kind 1: control message, a JSON object with a `type`, at most 1 MiB. Stream 0 messages are queued ahead of data; stream-bound messages keep their order relative to that stream's data.
- kind 2: file data on a non-zero stream, at most 1 MiB.

Several transfers interleave fairly (round robin per stream). Backpressure is TCP's: a stream holds at most 8 queued data frames.

## Transfers

The receiver is authoritative for confirmed progress.

| Message | Direction | Meaning |
|---|---|---|
| `offer` | sender → receiver | Manifest in parts (≤ 2000 entries, ≤ 512 KiB): relative slash paths, sizes, modification times, folders |
| `query` | sender → receiver | Ask for the transfer's state after (re)connecting |
| `pause` / `resume` / `cancel` | sender → receiver | Shared state changes |
| `status` | receiver → sender | `offered`, `running`, `paused`, `completed`, `failed`, `cancelled` or `unknown` with the durable checkpoint `(next, offset)`; a non-zero `epoch` starts a new stream at that checkpoint |
| `file` | sender → receiver, on the stream | Start of entry `ordinal` at `offset` for `epoch` |
| `segment` | sender → receiver, on the stream | SHA-256 of the previous segment (32 MiB, the last one shorter) |
| `end` | sender → receiver, on the stream | All entries sent |

- Files are sent in manifest order without per-file round trips. The sender reads each file once and hashes while sending; it checks size, modification time and file identity before and after reading.
- The receiver writes sequentially into a private partial file below `.airlink-partials/<transfer>/` of the receive folder and compares every segment's SHA-256. A mismatch fails the transfer; nothing unverified is published.
- Finished files are synced and published in batches (≤ 512 files, ≤ 64 MiB or after 0.7 s), then recorded in one SQLite transaction and acknowledged with a `status`. Large files are checkpointed at segment boundaries at most once per second. On macOS one full-disk-flush barrier covers a batch.
- A resumed or restarted stream begins at the receiver's checkpoint with a fresh epoch. Stream messages of an older epoch, and their data, are ignored.
- Publication never replaces an existing file: names are planned when the transfer is accepted (top-level folders are created immediately) and a name taken meanwhile gets a number (`Report (2).pdf`). Paths are confined to the receive folder; names are adapted to the receiving platform (Windows-reserved characters and names, decomposed Latin letters composed).

## Clipboard

`clipboard` (`id`, `text` ≤ 64 KiB UTF-8) is answered with `clipboard.ack` (`id`, optional `error`). Both devices must allow clipboard text for each other.
