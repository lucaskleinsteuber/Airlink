# AirLink 0.2 additions (protocol /api/v1 remains additive)

All authenticated status responses add `shares: Share[]`, `clipboard: ClipboardMessage[]`, `clipboardAllowed: boolean`. Device adds `canClipboard` (default false including migrated devices). Existing file transfers are unchanged.

## Share management

Local IPC: `share.create` ShareCreate -> Share, `share.revoke {id}` -> {ok:true}. All host shares appear in local Status.shares. ShareCreate: `{name,paths?:string[],transferId?:string,password?:string,expiresAt?:RFC3339,maxDownloads:number}`; exactly one source selector. Local absolute paths reference original files, never copy them. A transferId selects an entire existing completed received transfer or an outgoing offered transfer. A paired browser may create/revoke only its own shares using an owned completed received transfer; it cannot supply filesystem paths.

Authenticated `POST /api/v1/shares` ShareCreate -> Share; `POST /api/v1/shares/{id}/revoke` -> {ok:true}. Guest status lists only own shares. Name defaults to source basename if blank. Expiry omitted/empty means no expiry. MaxDownloads=0 means unlimited. Defaults in UI: expiry 24h, no password, unlimited. Revocation is immediate, persistent and blocks active streams. Passwords use Argon2id, never plaintext storage/status.

Share JSON is defined in internal/model. URL form `https://HOST/s/SHARE_ID#SECRET`; the random secret is never in an HTTP URL, referrer, analytics or public metadata. Local owner/authorized creator can recover their share link after restart. Anonymous pages cannot see filenames before authorization. Only this share's source files are exposed.

## Anonymous share recipient

`GET /s/{id}` serves the embedded React app, which detects the share route without invoking device pairing. Read the fragment once, retain only in sessionStorage, remove it from the address bar. Offline certificate onboarding still applies.

- `POST /api/v1/public/shares/{id}/unlock` `{secret,password?}` -> `{share:Share}` and a share-scoped HttpOnly Secure SameSite=Strict cookie. Existing valid session is reused. No download quota is consumed by viewing/unlocking.
- `GET /api/v1/public/shares/{id}` -> Share when a valid share session cookie exists. No URL/secret in this response.
- `GET|HEAD /api/v1/public/shares/{id}/files/{fileId}` -> Range/ETag download; cookie required. Each first successful body download starts one atomic session allowance. Files/Range/retries in same session do not count again. Concurrent new sessions cannot exceed maxDownloads. Invalid Range and HEAD do not consume downloads.
- `GET /api/v1/public/shares/{id}/archive` -> streamed ZIP64 folder download; no Range support. Same session quota.

Expiry/revocation returns 410, unavailable original file or exhausted quota 409, missing/bad secret/password/session 401/403. Limit unlock attempts and concurrent Argon2 work. Same-origin/Host security headers also protect anonymous APIs; the public HTTP bootstrap never serves share resources.

Recipient cookies persist for up to 30 days, capped by the share's expiry; the server enforces the same session age. A share's earlier expiry or revocation always invalidates the session, including active streams.

## Explicit clipboard text

Maximum 64KiB UTF-8 per text, no empty text. No automatic monitoring/read/write. Receiver must explicitly choose Copy; a message never replaces its clipboard by arrival. Up to 100 recent messages are held only in process memory for one hour, never persisted. Revoking a device removes its text messages.

Local IPC: `clipboard.send {deviceId,text}` -> ClipboardMessage, `clipboard.dismiss {id}` -> {ok:true}. Browser peer: message appears as direction send in host Status and incoming in browser display. Native peer: send over its pinned HTTPS API and only record local success after acknowledgement. Both sides must explicitly enable canClipboard on their pairing for native-to-native.

Authenticated `POST /api/v1/clipboard` `{text,id?}` -> ClipboardMessage (incoming at host). Requires device.canClipboard. `DELETE /api/v1/clipboard/{id}` dismisses only own device's message. Browser Status.clipboardAllowed reflects the currently granted permission. Browser sees only messages associated with its pairing, and displays direction receive as outgoing-to-host, send as incoming-from-host. Native local status sees the host perspective. UI supports explicit Paste, editable text, Send, Copy and Dismiss; browsers without clipboard API provide selectable text/manual paste instead.
