# AirLink protocol 1 — 0.7.0

The baseline transfer routes introduced in 0.1 remain compatible with the current 0.7 implementation. Additive share/clipboard interfaces are specified in [API-0.2.md](API-0.2.md), relay/rate/history/statistics in [API-0.3.md](API-0.3.md), and optional incoming-file organization in [API-0.4.md](API-0.4.md). The 0.6 local-only additions below remain available; 0.7 native sending, shares and clipboard controls reuse these existing methods rather than adding recipient approval or filesystem powers to the LAN API. Wire protocol version remains 1.

The Go definitions in `internal/model` define JSON names. All byte counts and offsets are signed 64-bit integers. State and transfer updates are persisted. Empty lists are JSON arrays.

## LAN API (HTTPS, same origin)

- `GET /api/v1/info`: public `{version,id,name,chunkSize,fingerprint,urls}`.
- `POST /api/v1/pair/request` `{name,kind:"browser"}` → `{requestId,pollToken}`. No access granted until local approval.
- `POST /api/v1/pair/poll` `{requestId,pollToken}` → `{state:"pending"|"approved"|"denied",deviceId?,token?}`. Approved browser receives an HttpOnly Secure SameSite=Strict session cookie. Store no bearer in localStorage. Poll secrets stay in sessionStorage only.
- `GET /api/v1/status`: authenticated Status, scoped to owned transfers/shares/text and participant relays, plus authorized browser relay choices. It omits local settings, private filesystem mappings, duplicate candidate IDs/paths and other devices' credentials.
- `GET /api/v1/events`: authenticated SSE `event: status`, JSON Status; reconnect automatically.
- `POST /api/v1/transfers` `{files:[{id,path,size,modified,directory}]}` → Transfer. Direction receive on host. State offered unless device AutoAccept. Source retains original File objects/handles.
- `GET /api/v1/transfers/{id}` → Transfer (device-owned only).
- `POST /api/v1/transfers/{id}/accept|pause|resume|cancel`: owner may pause/resume/cancel; only local control accepts incoming transfers.
- `PUT /api/v1/transfers/{id}/files/{fileId}/chunks/{index}`: raw body, `X-Chunk-SHA256` lowercase hex. In-order 8MiB blocks, final block may be shorter. Returns `{received}`. Exact duplicate idempotent. Reject unconfirmed/out-of-order chunks. Await approval/queue state before sending.
- `POST /api/v1/transfers/{id}/files/{fileId}/prepare` `{sha256}` → `{decision:"upload"|"pending"|"skip",transfer}`. Owning authenticated sender only, after recipient approval. Updated senders call after full hashing and before payload; a 404 from an older host permits the legacy path only after refreshing the owned transfer. Disabled duplicate detection returns upload. Pending requires a host decision and reveals no candidate IDs, existing paths or destination mappings.
- `POST /api/v1/transfers/{id}/files/{fileId}/complete` `{sha256}` → Transfer on completed verification/publication or a confirmed Skip. No final file is visible before verification. Legacy sources may omit prepare; while duplicate checking or a host choice is pending, complete returns **HTTP 409**, not success. Refresh and wait for the host; Keep can publish verified staging even after source disconnection.
- `GET /api/v1/transfers/{id}/files/{fileId}/download`: offered file, Range/ETag supported; auth cookie or Authorization Bearer token. No network path access. Skipped non-existent payloads cannot be opened or redirected to an existing duplicate candidate.
- `GET /api/v1/transfers/{id}/archive`: ZIP64 stream, no resumable Range promise.
- `POST /api/v1/transfers/{id}/downloaded`: browser acknowledges download initiation (not verified completion).
- `GET /certificate.pem`: public CA certificate only, never private keys. Certificate trust must be verified on the host.

JSON errors `{error:"human readable"}` with appropriate 4xx/5xx. 401 means pair again. 409 means refresh transfer and retry only if state allows. Remote APIs may include relative wire filenames but never private host filesystem paths, duplicate candidates or local administrator powers. Origin/Host validation and authentication required.

## Local control via stdin/stdout

Daemon `--stdio` accepts one JSON object per line `{id,method,params}` and replies `{id,result}` or `{id,error}`. Correlate responses by ID: bounded asynchronous `duplicate.resolve` checks may finish out of order, without blocking status/pause/cancel. Candidate hashes have no fixed client timeout. Long source-manifest offers and share creation also require a transport policy that does not impose a short ordinary-command deadline. Stdout is reserved for protocol; logs go to stderr. Methods:

- `status` → complete Status (polled by native app once/second).
- `pair.approve` `{id}`; `pair.deny` `{id}`. Ordinary approval does not enable auto-receive.
- `pair.approveTrusted` `{id}` → success: explicitly approve this request and enable automatic receipt from that device on this host. The device and local receipt flag must be saved before credentials are issued; expired, denied or failed approvals grant neither.
- `device.update` Device; `device.revoke` `{id}`.
- `transfer.accept|pause|resume|cancel` `{id}`.
- `transfer.acceptTrusted` `{id}` → Transfer: accept an offered incoming transfer from a still-paired device with send permission, then save auto-receive for that sender on this host. If acceptance fails, the receipt policy stays unchanged. If acceptance succeeds but saving the policy fails, return an explicit error that the transfer was accepted while automatic receipt was not saved.
- `transfer.received` `{id}` → `{directory,items:[{fileId,path,directory}]}`: resolve actual published local destinations for a completed incoming transfer. Paths use saved destination mappings and the historical receive root, not the current default folder. Validate confined paths and reject symlink traversal; omit missing/skipped entries and child items covered by an explicitly transferred parent directory. An all-skipped regular-file transfer or one with no remaining result returns an error. This lookup performs no cleanup and never returns source paths or duplicate-candidate paths.
- `offer` `{deviceId,paths:[absolute filesystem paths],requestId?}` → Transfer. Native paired engines receive direct TLS transfers; browser peers download offered host files. Since 0.7, an optional request ID of 32 lowercase hexadecimal characters makes creation idempotent: the same ID, device and ordered cleaned paths return the original persisted transfer/file IDs, including after service restart. Reuse with different input returns a conflict. The key is saved in the same transfer document as the initial offer, and a failed save does not consume it. Replaying a key returns the existing state; it does not resume, modify or resend the job. The key lasts with its retained transfer record: explicitly clearing that terminal history also removes its replay record. Omit the ID for legacy behavior.
- `peer.connect` `{url,fingerprint}` → `{requestId}`; user must compare the fingerprint displayed directly by the target, then approve the request at the target. Request acknowledgment is still pending, not a completed pairing. Polling completes reciprocal credentials after approval.
- `peer.connectTrusted` `{url,fingerprint}` → `{requestId}`: the same pairing flow, with an explicit local choice to receive automatically from the target after successful approval. It does not enable the target's independent auto-receive policy. A denied/expired/failed request does not save this receipt grant.
- `settings.update` full Settings → normalized Settings. Preserve unrelated shortcut/rate fields. Organization defaults to `{detectDuplicates:false,sortEnabled:false,rules:[]}`; explicit rules affect only new incoming offers.
- `duplicate.resolve` `{transferId,fileId,action:"keep"|"skip",candidateId?}` → Transfer. Private host only; Skip streams and revalidates the selected matching candidate.
- `duplicate.path` `{candidateId}` → `{path}`. Private host only, for explicit Show/Copy actions. Neither duplicate command is a guest API.
- `shutdown` → clean exit after checkpointing.

The macOS and Windows apps own their bundled daemon through these private pipes. No local paths or trusted-approval methods are added to authenticated guest status or the LAN API. Device trust and permissions are local to each host; reciprocal credentials do not imply reciprocal auto-receive.

CLI starts the same daemon without stdio. A loopback-only administrative setup console uses a per-run unguessable URL token printed to terminal; it exposes these same local methods, never on LAN. Peer transfer tokens use Authorization headers, never URL query strings.

## Native interaction rules in 0.7

A drop on a paired desktop is an explicit `offer` intent for that batch and pinned `deviceId`. A named central drop area behaves the same way; the Windows recipient-bound file picker also starts its picked batch directly. The macOS picker retains the prepared-selection workflow. Without a suitable desktop recipient, the UI stages the selection and requires a later Send. Desktop native direct-send choices exclude browser peers; browser download and relay roles retain their existing contracts.

An offer snapshot contains fixed source-entry identities and one recipient. A successful response removes only those entry identities from the draft; later additions and reselected paths survive. A failed offer retains its selection for retry. A second direct batch during preparation is rejected visibly, without appending it silently to the in-flight offer. An unchanged failed native submission reuses its offer key but gets a fresh local attempt identity, so late local callbacks cannot complete a later retry. Changing recipient, source-entry identities or order creates a new key. Drafts themselves are in-memory UI state; backend key durability does not imply persistent unsubmitted selections. An offline paired desktop may be queued explicitly, while automatic recipient defaults consider only online paired desktops with receive permission and an empty, idle draft. Receipt still requires host acceptance or the separately saved local auto-receive policy.

Windows Quick Share calls `share.create` / `share.revoke` through private control and copies the full fragment link only after a user action. Clipboard calls `clipboard.send` / `clipboard.dismiss`; system Paste and Copy remain explicit local UI actions. Their existing permission, expiry, quota, ownership and UTF-8 limits are specified in [API-0.2.md](API-0.2.md). The new UI does not change those protocol semantics.

Discovery browsing refreshes every 15 seconds and nearby entries expire at 45 seconds. These are local presence hints, not trust or delivery receipts. Confirmed paired identities remain persistent and certificate checks remain necessary.

## Web expectations

No UI automation. Use React/TypeScript, CSS, hash-wasm worker for incremental SHA-256, 8MiB body buffers. Direct-transfer states: offered, queued, running, paused, reconnecting, verifying, checkingDuplicate, waitingDuplicate, completed, failed, cancelled. Relay states are defined separately in API-0.3. A browser sends payload only when host status says running and file preparation allows upload. SSE checking/waiting states must not abort an in-flight preparation request or create a busy 409 upload loop. The host alone resolves duplicates. Completed/skipped files require no further payload; cached checksums can be reused after Keep. Retry network failures with bounded backoff and resume at host-confirmed offset. Persist manifests/transfer IDs in IndexedDB; request same files again after reload, verify completed chunk content before resume. Show download-manager limitations honestly. Guest user cannot approve themselves.

## Organization and progress invariants

Only managed, verified received files are indexed; clearing history does not erase the duplicate index. Candidate validation uses size and full SHA-256, never filename equality or an arbitrary user-library scan. Both organization features default off. Sorting classifies only top-level individual files; folders keep their complete hierarchy. Rules and destination mappings snapshot at new-offer creation, and existing/active data never moves when settings change.

`File.path` remains immutable. `destinationPath` and `duplicateCandidates` are private host fields and are omitted from guest/peer responses. Public `skipped` and `duplicateState` describe the result without exposing the candidate. Skip revalidates the chosen file, creates no placeholder and never deletes/overwrites the existing candidate or source originals. Only explicit legacy Skip can remove this transfer's verified private staging data.

`Transfer.completedBytes` records actual confirmed bytes; `skippedBytes` records the avoided remainder. Progress combines them, while skipped-file/avoided-byte statistics remain separate from confirmed bytes and lifetime payload traffic. Downloads/archives/shares exclude skipped payloads. An all-skipped transfer cannot create a file share or serve the existing candidate through a skipped entry. Browser relay and browser-managed destination files are unaffected.
