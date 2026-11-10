# Desktop protocol additions — 0.12 beta

The HTTPS `/api/v1` transfer protocol remains version 1. These are additive private-pipe methods and explicit desktop pairing-v2 routes. Legacy `status`, `offer` and verified fingerprint pairing remain available. These methods grant no new filesystem access to LAN guests.

## Compact private status and pages

- `status.summary` returns the ordinary host identity/settings/devices plus compact transfer summaries, active counts, preparation jobs and private pairing sessions. It is generated without copying complete manifests. All active work is prioritized, up to three recent terminal entries per peer are retained, and the response has a global transfer cap of 200. Counts represent the whole engine, not only visible rows.
- Transfer summaries add `summary`, `title`, `fileCount` and `hasContent`; `files` is a preview of at most three entries. Candidate lists and private clipboard text are omitted from the summary. `hasContent` is authoritative for whether a completed receive has a new result; a wrapper directory in a preview must not invent one.
- `history.page` accepts an opaque optional `cursor`, `limit` (default 50, maximum 100), `deviceId`, `state`, `direction` and `query`. State may select `active`, `finished` or an individual transfer state. It returns `{transfers,nextCursor?}`. Ordering/cursors use immutable creation time and ID; clients must not interpret or synthesize a cursor.
- `transfer.files` accepts `{id,offset,limit}` and returns `{files,total,nextOffset}`; `nextOffset=-1` ends the list. The default/maximum page bounds are 50/100. Private duplicate candidates are loaded through this detail path.
- `transfer.get`, `statistics.get`, `share.list`, `clipboard.list` and `relay.list` are on-demand private views. Full clipboard text is loaded only for an explicitly visible tool.
- `connection.qr` accepts a URL belonging to the current host and returns the QR image. It cannot encode an arbitrary remote URL.

Pages and summaries can arrive out of order. A newer per-transfer timestamp wins; clients preserve timestamp precision when comparing UTC RFC3339Nano values. Mutation/request generations also invalidate stale tool snapshots and removed history entries. Every history removal still requires the engine's atomic terminal-state check.

## Beta 2 diagnostics, recovery and per-file replies

`diagnostics.export` is a **private host-control method**, without parameters. Native clients use their process pipes; the service's separately authenticated loopback administration channel retains the same private methods. There is no LAN guest/device route. Schema 1 returns `schemaVersion`, `version`, UTC `generatedAt`, `platform`, `architecture`, `pairedDevices`, `goHeapBytes`, `transferSummary` and a fixed `privacy` explanation. The summary contains aggregate `transfers`, `files`, `confirmedBytes`, fixed-state counts and fixed-problem-code counts. The heap sample is not RSS. No names, paths, addresses, IDs, hashes, tokens, clipboard contents or raw errors/logs are exported. Desktop apps save the JSON only after the user chooses a destination; there is no automatic upload. Without a working helper they can save a minimal version/platform/service-unavailable report.

Transfer models optionally add `problem:{code,titleDE,titleEN,recoveryDE,recoveryEN}` and `retryable`. All advice is fixed product text. Original technical errors remain under details. `transfer.resume` also accepts a failed **sender** when `retryable` is true: recoverable space/access/destination/network/device-permission problems can reuse the same source identities and remote checkpoints. The receiver still enforces its state and permissions. Integrity/source-change failures, cancellation, completed jobs and a terminal receiver do not become resumable. Restoring a revoked pairing or an original destination may be required before retry; changing receive settings affects new jobs.

Owning authenticated senders may append `?fileOnly=1` to the existing HTTPS file `prepare` and `complete` routes. Their transfer-shaped result then includes exactly the requested file, with unchanged transfer/file identities and progress fields. It is a partial manifest and must not replace a complete locally stored manifest. Requests without the option retain the full legacy result; older services can ignore the query and return a full result. Hash checking, final publication, ownership and duplicate decisions are unchanged. The option grants no filesystem access.

## Cancellable preparation

`preparation.start` accepts `{deviceId,paths,requestId}`. `paths` contains absolute source selections, and `requestId` is a 32-digit hexadecimal replay identity. The response is a private `Preparation` containing `id`, `requestId`, `deviceId`, `state`, optional `transferId` and `error`.

`preparation.status {id}` reads the job; `preparation.cancel {id}` cancels it. States are `preparing`, `completed`, `failed` and `cancelled`. A completed job points to the one persisted transfer; cancellation/error preserves the frontend's source selection. Replaying the same request and immutable inputs returns the same job, while reusing its key for different inputs is rejected. Jobs are bounded and terminal receipts are retained briefly for retries.

The source tree walk and the commit boundary observe cancellation. Device revocation/permission loss also cancels related preparation. Long legacy offer, Quick Share capture and duplicate validation run in bounded workers so status/control commands remain responsive. Pipe EOF/shutdown cancels and joins owned workers. Native clients correlate replies by ID; they cannot assume request-order replies.

## Desktop pairing v2

`GET /api/v1/info` advertises `capabilities:["pairing-v2"]`. Private methods are `pairing.start {url}`, `pairing.status {id}`, `pairing.confirm {id,automaticReceive}` and `pairing.cancel {id}`. A private session exposes `id`, peer identity/name, state, the locally derived eight-digit `comparisonCode`, expiry and local/remote confirmation flags. The code retains leading zeros.

Public transport routes are `POST /api/v1/pair/v2/start`, `/poll`, `/confirm`, `/ack` and `/cancel`. They exchange session identifiers, nonces, proof secrets and durable handoff receipts; the displayed comparison code is not an authoritative remote field. It is never added to guest status.

Both helpers independently use the original TLS 1.3 session's exporter with label `EXPORTER-AirLink-Pairing-v2` and context `AirLink desktop pairing version 2`. An HMAC-SHA256 derivation binds role-ordered device IDs, root-certificate fingerprints, both nonces and session identity into the decimal eight-digit comparison. It is checked directly on both computers. A TLS intermediary obtains a different exporter/code and cannot acquire trust from a one-sided confirmation.

Attempts expire after five minutes before grant, have unique random secrets, bounded retained sessions and per-origin start limits. States are `starting`, `comparing`, `finishing`, `paired`, `cancelled`, `denied` and `expired`. Trust/usable credentials require both confirmations. The receiver persists a single grant and receipt; the initiator persists credentials before acknowledging. Repeated/lost completion replies recover that same handoff, including after a helper restart. An already-granted handoff can finish beyond the initial comparison expiry. Revocation removes associated receipts and prevents a later grant resurrection.

`automaticReceive` is an explicit local incoming-direction choice, off by default. The remote computer controls its independent choice. An unsupported peer requires the separately displayed, full-fingerprint legacy method; no silent trust downgrade occurs. Browser certificate/QR onboarding and receiver approval remain separate.
