import { createHash } from 'node:crypto';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { digestFile } from './hash-stream';
import { historyQuery, historyRows } from './history';
import { relayCanServe, relayTargetAllowed, requestedChunk, selectRelaySources, validateRelayManifest } from './relay-protocol';
import { CHUNK_SIZE } from './types';
import type { History, Relay, RelayManifest, Status, Transfer } from './types';
const hash = (value: Uint8Array | string) => createHash('sha256').update(value).digest('hex');
function fixture(size = 3, id = 'relay') {
  const data = new Uint8Array(size).fill(7); const file = new File([data], `${id}.bin`, { lastModified: 100 });
  const chunks = []; for (let offset = 0; offset < size; offset += CHUNK_SIZE) chunks.push(hash(data.subarray(offset, offset + CHUNK_SIZE)));
  const saved = { id: 'file', path: file.name, size, modified: 100, directory: false, sha256: hash(data), chunks };
  const relay: Relay = { id, senderId: 'sender', senderName: 'Sender', receiverId: 'receiver', receiverName: 'Receiver', state: 'ready', files: [{ ...saved, received: 0, completed: false }], totalBytes: size, deliveredBytes: 0, bytesPerSecond: 0, createdAt: '2026-10-24T12:00:00Z', updatedAt: '' };
  const manifest: RelayManifest = { id, hostId: 'host', senderId: 'sender', receiverId: 'receiver', files: [saved], updatedAt: 0 };
  return { data, file, relay, manifest, checksums: { sha256: saved.sha256, chunks } };
}
function status(relays: Relay[] = []): Status { return { id: 'host', name: 'Host', version: '0.3.0', fingerprint: '', chunkSize: CHUNK_SIZE, devices: [], transfers: [], currentDeviceId: 'sender', relayDevices: [{ id: 'receiver', name: 'Receiver', kind: 'browser', online: true, paired: true, canSend: true, canReceive: true, canRelay: true, autoAccept: false }], relays }; }
vi.mock('./relay-storage', () => ({ saveRelayManifest: vi.fn(async () => {}) }));
vi.mock('./hash', () => ({ hashFile: vi.fn(async (file: File, chunkSize: number, progress: (done: number) => void) => { const { digestFile } = await import('./hash-stream'); return digestFile(file, chunkSize, progress); }) }));
import { RelayManager } from './relay-manager';
import { hashFile } from './hash';
import { saveRelayManifest } from './relay-storage';
const managers: RelayManager[] = [];
function manager() { const local = vi.fn(); const result = new RelayManager(local, () => {}, () => {}); managers.push(result); return { manager: result, local }; }
const response = (value: unknown, statusCode = 200) => new Response(JSON.stringify(value), { status: statusCode, headers: { 'Content-Type': 'application/json' } });
afterEach(() => { for (const manager of managers.splice(0)) manager.stop(); vi.restoreAllMocks(); vi.clearAllMocks(); vi.unstubAllGlobals(); });
describe('bounded source checksums', () => {
  it('hashes a large file through slices no larger than 8MiB and never loads the full File', async () => {
    const { file, data } = fixture(CHUNK_SIZE * 2 + 17);
    const fullRead = vi.spyOn(file, 'arrayBuffer').mockRejectedValue(new Error('Full file read is forbidden'));
    const slices = vi.spyOn(file, 'slice'); const progress: number[] = [];
    const result = await digestFile(file, CHUNK_SIZE, done => progress.push(done));
    expect(result.sha256).toBe(hash(data)); expect(result.chunks).toHaveLength(3); expect(fullRead).not.toHaveBeenCalled();
    for (const [start, end] of slices.mock.calls) expect((end || 0) - (start || 0)).toBeLessThanOrEqual(CHUNK_SIZE);
    expect(progress.at(-1)).toBe(file.size);
    await expect(digestFile(file, CHUNK_SIZE + 1, () => {})).rejects.toThrow();
  });
});
describe('relay request and identity rules', () => {
  it('only selects an authorized online receiving browser other than oneself', () => {
    const value = status(); expect(relayTargetAllowed(value, 'receiver')).toBe(true); expect(relayTargetAllowed(value, 'sender')).toBe(false);
    for (const changed of [{ online: false }, { canRelay: false }, { canReceive: false }, { kind: 'engine' }]) expect(relayTargetAllowed({ ...value, relayDevices: [{ ...value.relayDevices![0], ...changed }] }, 'receiver')).toBe(false);
  });
  it('serves delivered Range retries but never skips explicit acceptance', () => {
    const { relay } = fixture(); expect(relayCanServe({ ...relay, state: 'offered' }, 'sender')).toBe(false); expect(relayCanServe({ ...relay, state: 'delivered' }, 'sender')).toBe(true); expect(relayCanServe(relay, 'receiver')).toBe(false); expect(relayCanServe({ ...relay, state: 'cancelled' }, 'sender')).toBe(false);
  });
  it('rejects stale identities, directories, absent requests and out-of-range indexes before reading bytes', () => {
    const { relay, manifest } = fixture(CHUNK_SIZE + 3); const request = { requestId: 'nonce', fileId: 'file', index: 1 };
    expect(requestedChunk(relay, manifest, request)).toMatchObject({ offset: CHUNK_SIZE, end: CHUNK_SIZE + 3 });
    for (const change of [{ index: -1 }, { index: 0.5 }, { index: 2 }, { requestId: '' }, { fileId: 'other' }]) expect(() => requestedChunk(relay, manifest, { ...request, ...change })).toThrow();
    expect(() => validateRelayManifest({ ...relay, receiverId: 'other' }, manifest)).toThrow();
    expect(() => validateRelayManifest({ ...relay, files: [{ ...relay.files[0], sha256: 'f'.repeat(64) }] }, manifest)).toThrow();
    expect(() => selectRelaySources(manifest, [{ path: 'other.bin', directory: false, file: new File(['abc'], 'other.bin', { lastModified: 100 }) }])).toThrow();
  });
});
describe('demand-driven relay source', () => {
  it('finishes checksums before publishing an offer and persists no payload or File objects', async () => {
    const value = fixture(); const { manager: source } = manager(); source.update(status());
    let release!: (result: typeof value.checksums) => void;
    vi.mocked(hashFile).mockImplementationOnce(() => new Promise(resolve => { release = resolve; }));
    const network = vi.fn(async (_url: string, options?: RequestInit) => {
      const body = JSON.parse(options!.body as string); expect(body.files[0].sha256).toBe(value.checksums.sha256); expect(body.files[0].chunks).toBeUndefined();
      return response({ ...value.relay, state: 'offered', files: body.files });
    }); vi.stubGlobal('fetch', network);
    const pending = source.create('receiver', [{ path: value.file.name, file: value.file, directory: false }]);
    expect(network).not.toHaveBeenCalled(); release(value.checksums); await pending;
    expect(network).toHaveBeenCalledTimes(1);
    const persisted = vi.mocked(saveRelayManifest).mock.calls.at(-1)![0]; expect(persisted.files[0].sha256).toBe(value.checksums.sha256); expect(JSON.stringify(persisted)).not.toContain('"data"'); expect(Object.values(persisted.files[0]).some(item => item instanceof Blob || item instanceof ArrayBuffer)).toBe(false);
  });
  it('does not push payload when the receiver has not requested a chunk', async () => {
    const value = fixture(); const { manager: source } = manager(); let requests = 0; let payloads = 0;
    vi.stubGlobal('fetch', vi.fn(async (url: string, options?: RequestInit) => { if (options?.method === 'PUT') payloads++; if (url.endsWith('/request')) { requests++; return response({ request: null }); } return response(value.relay); }));
    source.restore([value.manifest]); source.update(status([value.relay])); await source.attach(value.relay.id, [{ path: value.file.name, file: value.file, directory: false }]); await vi.waitFor(() => expect(requests).toBeGreaterThan(0)); expect(payloads).toBe(0);
  });
  it('sends only the requested tail block and keeps delivered sources available', async () => {
    const value = fixture(CHUNK_SIZE + 3); const { manager: source } = manager(); let payloads = 0; let requests = 0;
    vi.stubGlobal('fetch', vi.fn(async (url: string, options?: RequestInit) => {
      if (options?.method === 'PUT') { payloads++; expect(new URL(url, 'https://host').searchParams.get('requestId')).toBe('nonce?&/'); expect((options.body as ArrayBuffer).byteLength).toBe(3); expect((options.headers as Record<string, string>)['X-Chunk-SHA256']).toBe(value.checksums.chunks[1]); value.relay.state = 'delivered'; return response({ delivered: true }); }
      if (url.endsWith('/request')) { requests++; return response({ request: payloads ? null : { requestId: 'nonce?&/', fileId: 'file', index: 1 } }); } return response(value.relay);
    }));
    source.restore([value.manifest]); source.update(status([value.relay])); await source.attach(value.relay.id, [{ path: value.file.name, file: value.file, directory: false }]); await vi.waitFor(() => expect(payloads).toBe(1)); await vi.waitFor(() => expect(requests).toBeGreaterThanOrEqual(2));
  });
  it('refreshes request identity after a consumed block loses its ACK', async () => {
    const value = fixture(); const { manager: source, local } = manager(); const tokens: string[] = []; let requestCount = 0;
    vi.stubGlobal('fetch', vi.fn(async (url: string, options?: RequestInit) => {
      if (options?.method === 'PUT') { tokens.push(new URL(url, 'https://host').searchParams.get('requestId')!); if (tokens.length === 1) throw new TypeError('Connection dropped after consumption'); return response({ delivered: true }); }
      if (url.endsWith('/request')) { requestCount++; return response({ request: tokens.length >= 2 ? null : { requestId: `request-${requestCount}`, fileId: 'file', index: 0 } }); } return response(value.relay);
    }));
    source.restore([value.manifest]); source.update(status([value.relay])); await source.attach(value.relay.id, [{ path: value.file.name, file: value.file, directory: false }]); await vi.waitFor(() => expect(tokens).toEqual(['request-1', 'request-2']), { timeout: 3000 });
    expect(local.mock.calls.some(([state]) => state.get(value.relay.id)?.phase === 'reconnecting')).toBe(true); expect(value.relay.deliveredBytes).toBe(0);
  });
  it('requires matching full content after reload even when size and timestamp match', async () => {
    const value = fixture(); const { manager: source } = manager(); source.update(status([value.relay])); const network = vi.fn(); vi.stubGlobal('fetch', network);
    const changed = new File([new Uint8Array(3).fill(8)], value.file.name, { lastModified: 100 });
    await expect(source.attach(value.relay.id, [{ path: value.file.name, file: changed, directory: false }])).rejects.toThrow('contents have changed'); expect(network).not.toHaveBeenCalled();
  });
  it('refreshes a stale request after 409 instead of repeating its old request token', async () => {
    const value = fixture(); const { manager: source } = manager(); const tokens: string[] = []; let requestCount = 0;
    vi.stubGlobal('fetch', vi.fn(async (url: string, options?: RequestInit) => {
      if (options?.method === 'PUT') { tokens.push(new URL(url, 'https://host').searchParams.get('requestId')!); return tokens.length === 1 ? response({ error: 'Stale request' }, 409) : response({ delivered: true }); }
      if (url.endsWith('/request')) { requestCount++; return response({ request: tokens.length >= 2 ? null : { requestId: `request-${requestCount}`, fileId: 'file', index: 0 } }); } return response(value.relay);
    }));
    source.restore([value.manifest]); source.update(status([value.relay])); await source.attach(value.relay.id, [{ path: value.file.name, file: value.file, directory: false }]); await vi.waitFor(() => expect(tokens).toEqual(['request-1', 'request-2']), { timeout: 3000 });
  });
  it('does not abort local source verification just because an offer is still awaiting acceptance', async () => {
    const value = fixture(); value.relay.state = 'offered'; const { manager: source } = manager(); source.update(status([value.relay]));
    let release!: (result: typeof value.checksums) => void; let hashSignal: AbortSignal | undefined;
    vi.mocked(hashFile).mockImplementationOnce((_file, _size, _progress, signal) => { hashSignal = signal; return new Promise(resolve => { release = resolve; }); });
    const pending = source.attach(value.relay.id, [{ path: value.file.name, file: value.file, directory: false }]); source.update(status([value.relay])); expect(hashSignal?.aborted).toBe(false); release(value.checksums); await pending;
  });
  it('cancels an in-flight block immediately when a participant pauses', async () => {
    const value = fixture(); const { manager: source } = manager(); let putSignal: AbortSignal | undefined;
    vi.stubGlobal('fetch', vi.fn(async (url: string, options?: RequestInit) => {
      if (options?.method === 'PUT') { putSignal = options.signal!; await new Promise((_resolve, reject) => putSignal!.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')), { once: true })); }
      if (url.endsWith('/request')) return response({ request: { requestId: 'nonce', fileId: 'file', index: 0 } }); return response(value.relay);
    }));
    source.restore([value.manifest]); source.update(status([value.relay])); await source.attach(value.relay.id, [{ path: value.file.name, file: value.file, directory: false }]); await vi.waitFor(() => expect(putSignal).toBeDefined()); value.relay.state = 'paused'; source.update(status([value.relay])); expect(putSignal?.aborted).toBe(true);
  });
  it('caps in-flight payload buffers at two even when three receivers request data', async () => {
    const values = [fixture(3, 'one'), fixture(3, 'two'), fixture(3, 'three')]; const { manager: source } = manager(); const inFlight = new Set<string>(); const finished = new Set<string>(); const releases = new Map<string, () => void>(); let maximum = 0;
    vi.stubGlobal('fetch', vi.fn(async (url: string, options?: RequestInit) => {
      const id = url.split('/')[4]; const value = values.find(item => item.relay.id === id)!;
      if (options?.method === 'PUT') {
        inFlight.add(id); maximum = Math.max(maximum, inFlight.size);
        await new Promise<void>((resolve, reject) => { const signal = options.signal!; const abort = () => reject(new DOMException('Aborted', 'AbortError')); signal.addEventListener('abort', abort, { once: true }); releases.set(id, () => { signal.removeEventListener('abort', abort); inFlight.delete(id); finished.add(id); resolve(); }); });
        return response({ delivered: true });
      }
      if (url.endsWith('/request')) return response({ request: finished.has(id) ? null : { requestId: `nonce-${id}`, fileId: 'file', index: 0 } }); return response(value.relay);
    }));
    source.restore(values.map(value => value.manifest)); source.update(status(values.map(value => value.relay)));
    for (const value of values) await source.attach(value.relay.id, [{ path: value.file.name, file: value.file, directory: false }]);
    await vi.waitFor(() => expect(releases.size).toBe(2)); expect(maximum).toBe(2); releases.get('one')!(); await vi.waitFor(() => expect(releases.size).toBe(3)); expect(maximum).toBe(2); for (const release of releases.values()) release();
  });
});
describe('scoped history helpers', () => {
  it('encodes filters as individual query parameters with a bounded page size', () => { const query = new URLSearchParams(historyQuery({ query: 'film & state=other', state: 'delivered', direction: 'relay', deviceId: 'receiver' })); expect(query.get('query')).toBe('film & state=other'); expect(query.get('limit')).toBe('100'); expect(query.get('state')).toBe('delivered'); });
  it('shows only owned transfers or participant relays and sorts both kinds together', () => {
    const value = fixture(); const own = { id: 'transfer', deviceId: 'sender', files: [], createdAt: '2026-10-24T13:00:00Z' } as unknown as Transfer;
    const history: History = { transfers: [own, { ...own, id: 'foreign', deviceId: 'other' }], relays: [value.relay, { ...value.relay, id: 'foreign-relay', senderId: 'other', receiverId: 'someone' }] };
    expect(historyRows(history, 'sender').map(row => row.value.id)).toEqual(['transfer', 'relay']); expect(historyRows(history, 'receiver').map(row => row.value.id)).toEqual(['relay']);
  });
});
