# AirLink protocol 1 — 0.4.0

The baseline transfer routes introduced in 0.1 remain compatible with the current 0.4 implementation. Additive share/clipboard interfaces are specified in [API-0.2.md](API-0.2.md), relay/rate/history/statistics in [API-0.3.md](API-0.3.md), and optional incoming-file organization in [API-0.4.md](API-0.4.md). Wire protocol version remains 1.

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

Daemon `--stdio` accepts one JSON object per line `{id,method,params}` and replies `{id,result}` or `{id,error}`. Correlate responses by ID: bounded asynchronous `duplicate.resolve` checks may finish out of order, without blocking status/pause/cancel. Candidate hashes have no fixed client timeout. Stdout is reserved for protocol; logs go to stderr. Methods:

- `status` → complete Status (polled by native app once/second).
- `pair.approve` `{id}`; `pair.deny` `{id}`.
- `device.update` Device; `device.revoke` `{id}`.
- `transfer.accept|pause|resume|cancel` `{id}`.
- `offer` `{deviceId,paths:[absolute filesystem paths]}` → Transfer. Native paired engines receive direct TLS transfers; browser peers download offered host files.
- `peer.connect` `{url,fingerprint}` → `{requestId}`; user must compare fingerprint displayed by target, then approve request at target. Polling completes reciprocal credentials automatically.
- `settings.update` full Settings → normalized Settings. Preserve unrelated shortcut/rate fields. Organization defaults to `{detectDuplicates:false,sortEnabled:false,rules:[]}`; explicit rules affect only new incoming offers.
- `duplicate.resolve` `{transferId,fileId,action:"keep"|"skip",candidateId?}` → Transfer. Private host only; Skip streams and revalidates the selected matching candidate.
- `duplicate.path` `{candidateId}` → `{path}`. Private host only, for explicit Show/Copy actions. Neither duplicate command is a guest API.
- `shutdown` → clean exit after checkpointing.

CLI starts same daemon without stdio. A loopback-only administrative setup console uses a per-run unguessable URL token printed to terminal; it exposes these same local methods, never on LAN. Peer transfer tokens use Authorization headers, never URL query strings.

## Web expectations

No UI automation. Use React/TypeScript, CSS, hash-wasm worker for incremental SHA-256, 8MiB body buffers. Direct-transfer states: offered, queued, running, paused, reconnecting, verifying, checkingDuplicate, waitingDuplicate, completed, failed, cancelled. Relay states are defined separately in API-0.3. A browser sends payload only when host status says running and file preparation allows upload. SSE checking/waiting states must not abort an in-flight preparation request or create a busy 409 upload loop. The host alone resolves duplicates. Completed/skipped files require no further payload; cached checksums can be reused after Keep. Retry network failures with bounded backoff and resume at host-confirmed offset. Persist manifests/transfer IDs in IndexedDB; request same files again after reload, verify completed chunk content before resume. Show download-manager limitations honestly. Guest user cannot approve themselves.

## Organization and progress invariants

Only managed, verified received files are indexed; clearing history does not erase the duplicate index. Candidate validation uses size and full SHA-256, never filename equality or an arbitrary user-library scan. Both organization features default off. Sorting classifies only top-level individual files; folders keep their complete hierarchy. Rules and destination mappings snapshot at new-offer creation, and existing/active data never moves when settings change.

`File.path` remains immutable. `destinationPath` and `duplicateCandidates` are private host fields and are omitted from guest/peer responses. Public `skipped` and `duplicateState` describe the result without exposing the candidate. Skip revalidates the chosen file, creates no placeholder and never deletes/overwrites the existing candidate or source originals. Only explicit legacy Skip can remove this transfer's verified private staging data.

`Transfer.completedBytes` records actual confirmed bytes; `skippedBytes` records the avoided remainder. Progress combines them, while skipped-file/avoided-byte statistics remain separate from confirmed bytes and lifetime payload traffic. Downloads/archives/shares exclude skipped payloads. An all-skipped transfer cannot create a file share or serve the existing candidate through a skipped entry. Browser relay and browser-managed destination files are unaffected.
