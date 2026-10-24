import { afterEach, describe, expect, it, vi } from 'vitest';
import { clearPendingPair, loadPendingPair, pollPair, requestPair } from './pairing';
import { recipientMetadata } from './share-recipient-state';
import type { Share } from './types';

const response = (value: unknown, status = 200) => new Response(JSON.stringify(value), { status, headers: { 'Content-Type': 'application/json' } });
const pending = { requestId: 'request', pollToken: 'private-poll-token' };
function session() {
  const data = new Map<string, string>();
  return { data, getItem: (key: string) => data.get(key) || null, setItem: (key: string, value: string) => { data.set(key, value); }, removeItem: (key: string) => { data.delete(key); } };
}
afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); vi.useRealTimers(); });

describe('pairing when browser session storage is unavailable', () => {
  it('requests, polls and finishes approval even when every storage operation throws', async () => {
    const blocked = { getItem: () => { throw new DOMException('Blocked', 'SecurityError'); }, setItem: () => { throw new DOMException('Blocked', 'SecurityError'); }, removeItem: () => { throw new DOMException('Blocked', 'SecurityError'); } };
    const network = vi.fn(async (url: string, options?: RequestInit) => {
      expect(options?.credentials).toBe('same-origin');
      if (url.endsWith('/pair/request')) { expect(JSON.parse(options!.body as string)).toEqual({ name: 'My phone', kind: 'browser' }); return response(pending); }
      expect(url.endsWith('/pair/poll')).toBe(true); expect(JSON.parse(options!.body as string)).toEqual(pending); return response({ state: 'approved' });
    });
    vi.stubGlobal('fetch', network);
    expect(loadPendingPair(() => blocked)).toBeNull();
    const inMemory = await requestPair(' My phone ', () => blocked);
    expect(inMemory).toEqual(pending);
    await expect(pollPair(inMemory, () => blocked)).resolves.toEqual({ state: 'approved' });
    expect(network).toHaveBeenCalledTimes(2);
    expect(() => clearPendingPair(() => blocked)).not.toThrow();
  });
  it('also tolerates a throwing sessionStorage property getter and denied requests', async () => {
    const blockedAccess = () => { throw new DOMException('Storage access denied', 'SecurityError'); };
    vi.stubGlobal('fetch', vi.fn(async (url: string) => response(url.endsWith('/request') ? pending : { state: 'denied' })));
    expect(loadPendingPair(blockedAccess)).toBeNull();
    await expect(pollPair(await requestPair('Phone', blockedAccess), blockedAccess)).resolves.toEqual({ state: 'denied' });
  });
  it('preserves a pending session through retries and clears it only after a host decision', async () => {
    const storage = session(); let approved = false;
    vi.stubGlobal('fetch', vi.fn(async (url: string) => response(url.endsWith('/request') ? pending : { state: approved ? 'approved' : 'pending' })));
    const value = await requestPair('Phone', () => storage);
    expect(loadPendingPair(() => storage)).toEqual(pending);
    await expect(pollPair(value, () => storage)).resolves.toEqual({ state: 'pending' });
    expect(loadPendingPair(() => storage)).toEqual(pending);
    approved = true; await pollPair(value, () => storage);
    expect(loadPendingPair(() => storage)).toBeNull();
  });
  it('rejects malformed saved pairing state without making network calls', () => {
    const storage = session(); const network = vi.fn(); vi.stubGlobal('fetch', network);
    for (const value of ['not JSON', '[]', '{"requestId":1,"pollToken":"secret"}', '{"requestId":"id","pollToken":""}']) {
      storage.setItem('airlink-pair', value); expect(loadPendingPair(() => storage)).toBeNull();
    }
    clearPendingPair(() => storage); expect(network).not.toHaveBeenCalled();
  });
});

describe('server-authoritative anonymous share lifetime', () => {
  const share = { id: 'share', name: 'Files', revoked: false, expiresAt: '2026-10-24T13:00:00Z', maxDownloads: 0, downloads: 0, files: [], totalBytes: 0, createdAt: '', passwordProtected: false } satisfies Share;
  it('keeps a successful share usable and polling when the recipient clock is far ahead', async () => {
    vi.useFakeTimers(); vi.setSystemTime(new Date('2035-01-01T00:00:00Z'));
    vi.stubGlobal('fetch', vi.fn(async () => response(share)));
    await expect(recipientMetadata('/public/shares/share')).resolves.toEqual({ phase: 'ready', share, poll: true });
  });
  it('stops on authoritative expiry even when the recipient clock is behind', async () => {
    vi.useFakeTimers(); vi.setSystemTime(new Date('2000-01-01T00:00:00Z'));
    vi.stubGlobal('fetch', vi.fn(async () => response({ error: 'Share expired' }, 410)));
    await expect(recipientMetadata('/public/shares/share')).resolves.toEqual({ phase: 'gone', missing: false, poll: false });
  });
  it('stops immediately on revocation or removal and retains quota-session downloads', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValueOnce(response({ ...share, revoked: true })).mockResolvedValueOnce(response({ error: 'Not found' }, 404)).mockResolvedValueOnce(response({ ...share, maxDownloads: 1, downloads: 1 })));
    await expect(recipientMetadata('/public/shares/share')).resolves.toEqual({ phase: 'gone', missing: false, poll: false });
    await expect(recipientMetadata('/public/shares/share')).resolves.toEqual({ phase: 'gone', missing: true, poll: false });
    await expect(recipientMetadata('/public/shares/share')).resolves.toMatchObject({ phase: 'ready', poll: true });
  });
  it('waits for explicit unlock after 401 and keeps retrying temporarily unavailable sources', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValueOnce(response({ error: 'Unlock required' }, 401)).mockResolvedValueOnce(response({ error: 'Source unavailable' }, 409)));
    await expect(recipientMetadata('/public/shares/share')).resolves.toEqual({ phase: 'locked', poll: false });
    await expect(recipientMetadata('/public/shares/share')).resolves.toEqual({ phase: 'unavailable', poll: true });
  });
});
