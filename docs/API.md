# AirLink protocol 1 — 0.1.0

The additive 0.2.0 share and explicit clipboard interfaces are specified in [API-0.2.md](API-0.2.md). The baseline transfer routes below remain compatible.

The Go definitions in `internal/model` define JSON names. All byte counts and offsets are signed 64-bit integers. State and transfer updates are persisted. Empty lists are JSON arrays.

## LAN API (HTTPS, same origin)

- `GET /api/v1/info`: public `{version,id,name,chunkSize,fingerprint}`.
- `POST /api/v1/pair/request` `{name,kind:"browser"}` → `{requestId,pollToken}`. No access granted until local approval.
- `POST /api/v1/pair/poll` `{requestId,pollToken}` → `{state:"pending"|"approved"|"denied",deviceId?,token?}`. Approved browser receives an HttpOnly Secure SameSite=Strict session cookie. Store no bearer in localStorage. Poll secrets stay in sessionStorage only.
- `GET /api/v1/status`: authenticated Status, filtered to this device's transfers (never local settings, paths or other devices).
- `GET /api/v1/events`: authenticated SSE `event: status`, JSON Status; reconnect automatically.
- `POST /api/v1/transfers` `{files:[{id,path,size,modified,directory}]}` → Transfer. Direction receive on host. State offered unless device AutoAccept. Source retains original File objects/handles.
- `GET /api/v1/transfers/{id}` → Transfer (device-owned only).
- `POST /api/v1/transfers/{id}/accept|pause|resume|cancel`: owner may pause/resume/cancel; only local control accepts incoming transfers.
- `PUT /api/v1/transfers/{id}/files/{fileId}/chunks/{index}`: raw body, `X-Chunk-SHA256` lowercase hex. In-order 8MiB blocks, final block may be shorter. Returns `{received}`. Exact duplicate idempotent. Reject unconfirmed/out-of-order chunks. Await approval/queue state before sending.
- `POST /api/v1/transfers/{id}/files/{fileId}/complete` `{sha256}` → Transfer. Receiver verifies complete file. No final file visible before verification.
- `GET /api/v1/transfers/{id}/files/{fileId}/download`: offered file, Range/ETag supported; auth cookie or Authorization Bearer token. No network path access.
- `GET /api/v1/transfers/{id}/archive`: ZIP64 stream, no resumable Range promise.
- `POST /api/v1/transfers/{id}/downloaded`: browser acknowledges download initiation (not verified completion).
- `GET /certificate.pem`: public CA certificate only, never private keys. Certificate trust must be verified on the host.

JSON errors `{error:"human readable"}` with appropriate 4xx/5xx. 401 means pair again. 409 means refresh transfer and retry only if state allows. Remote APIs do not expose paths or local administrator powers. Origin/Host validation and authentication required.

## Local control via stdin/stdout

Daemon `--stdio` accepts one JSON object per line `{id,method,params}` and replies `{id,result}` or `{id,error}`. Stdout is reserved for protocol; logs go to stderr. Methods:

- `status` → complete Status (polled by native app once/second).
- `pair.approve` `{id}`; `pair.deny` `{id}`.
- `device.update` Device; `device.revoke` `{id}`.
- `transfer.accept|pause|resume|cancel` `{id}`.
- `offer` `{deviceId,paths:[absolute filesystem paths]}` → Transfer. Native paired engines receive direct TLS transfers; browser peers download offered host files.
- `peer.connect` `{url,fingerprint}` → `{requestId}`; user must compare fingerprint displayed by target, then approve request at target. Polling completes reciprocal credentials automatically.
- `settings.update` Settings → Settings.
- `shutdown` → clean exit after checkpointing.

CLI starts same daemon without stdio. A loopback-only administrative setup console uses a per-run unguessable URL token printed to terminal; it exposes these same local methods, never on LAN. Peer transfer tokens use Authorization headers, never URL query strings.

## Web expectations

No UI automation. Use React/TypeScript, CSS, hash-wasm worker for incremental SHA-256, 8MiB body buffers. Status states: offered, queued, running, paused, reconnecting, verifying, completed, failed, cancelled. A browser upload only runs when host status says running. Retry network failures with bounded backoff and resume at host-confirmed offset. Persist manifests/transfer IDs in IndexedDB; request same files again after reload, verify completed chunk content before resume. Show download-manager limitations honestly. Guest user cannot approve themselves.
