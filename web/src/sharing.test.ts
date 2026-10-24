import { describe, expect, it, vi } from 'vitest';
import { resolveEntry } from './entry';
import { browserClipboardDirection, canShareTransfer, clipboardBytes, CLIPBOARD_LIMIT, consumeShareSecret, createSharePolicy, defaultShareDraft, publicSharePath, routeFor, shareState, validClipboardText } from './sharing';
import type { Transfer } from './types';
function session() { const data = new Map<string, string>(); return { data, getItem: (key: string) => data.get(key) || null, setItem: (key: string, value: string) => { data.set(key, value); } }; }
describe('anonymous share entry isolation', () => {
  it('constructs only the share entry and removes its fragment before rendering', () => {
    const storage = session(); const events: string[] = [];
    const app = vi.fn(() => 'app');
    const result = resolveEntry({ pathname: '/s/abc123', hash: '#private-secret', search: '' }, storage, url => events.push(url), {
      app, invalid: () => 'invalid', share: (id, secret) => { expect(events).toEqual(['/s/abc123']); expect(id).toBe('abc123'); expect(secret).toBe('private-secret'); return 'share'; },
    });
    expect(result).toBe('share'); expect(app).not.toHaveBeenCalled();
    expect([...storage.data.keys()]).toEqual(['airlink-share:abc123:secret']);
  });
  it('does not enter device pairing for malformed share links', () => {
    const app = vi.fn(() => 'app'); const share = vi.fn(() => 'share');
    expect(resolveEntry({ pathname: '/s/../../api', hash: '', search: '' }, session(), () => {}, { app, share, invalid: () => 'invalid' })).toBe('invalid');
    expect(app).not.toHaveBeenCalled(); expect(share).not.toHaveBeenCalled(); expect(routeFor('/')).toEqual({ kind: 'app' });
  });
  it('recovers a share secret only from its own tab-session key', () => {
    const storage = session(); const replace = vi.fn();
    consumeShareSecret('one', { pathname: '/s/one', hash: '#secret', search: '?view=files' }, storage, replace);
    expect(replace).toHaveBeenCalledWith('/s/one?view=files');
    expect(consumeShareSecret('one', { pathname: '/s/one', hash: '', search: '' }, storage, replace)).toBe('secret');
    expect(consumeShareSecret('two', { pathname: '/s/two', hash: '', search: '' }, storage, replace)).toBe('');
  });
  it('removes the secret even when sessionStorage is blocked', () => {
    const replace = vi.fn(); const blocked = { getItem: () => { throw Error('Blocked'); }, setItem: () => { throw Error('Blocked'); } };
    expect(consumeShareSecret('one', { pathname: '/s/one', hash: '#secret', search: '' }, blocked, replace)).toBe('secret'); expect(replace).toHaveBeenCalledWith('/s/one');
  });
  it('does not read/write the system clipboard or call the network during route/session setup', () => {
    const network = vi.fn(); const clipboard = { readText: vi.fn(), writeText: vi.fn() };
    vi.stubGlobal('fetch', network); vi.stubGlobal('navigator', { clipboard });
    resolveEntry({ pathname: '/s/one', hash: '#secret', search: '' }, session(), () => {}, { app: () => null, share: () => null, invalid: () => null });
    expect(network).not.toHaveBeenCalled(); expect(clipboard.readText).not.toHaveBeenCalled(); expect(clipboard.writeText).not.toHaveBeenCalled(); vi.unstubAllGlobals();
  });
  it('encodes share identifiers without moving secrets into HTTP URLs', () => { expect(publicSharePath('a/b?secret')).toBe('/public/shares/a%2Fb%3Fsecret'); });
});
describe('share policies and lifetime', () => {
  const now = Date.UTC(2026, 9, 24, 12);
  it('defaults to 24 hours, unlimited downloads and no password', () => {
    const value = createSharePolicy(defaultShareDraft(now), now);
    expect(Date.parse(value.expiresAt!) - now).toBe(24 * 60 * 60 * 1000); expect(value.maxDownloads).toBe(0); expect(value.password).toBeUndefined();
  });
  it('supports no-expiry and rejects invalid limits or expired dates', () => {
    expect(createSharePolicy({ ...defaultShareDraft(now), noExpiry: true }, now).expiresAt).toBeUndefined();
    for (const maxDownloads of ['', '-1', '1.5', 'Infinity', '9007199254740992']) expect(() => createSharePolicy({ ...defaultShareDraft(now), maxDownloads }, now)).toThrow('downloadLimit');
    expect(() => createSharePolicy({ ...defaultShareDraft(now), expiresAt: '2000-01-01T12:00' }, now)).toThrow('expiryInvalid');
  });
  it('treats expiry as inclusive and revocation as immediate', () => {
    const share = { revoked: false, expiresAt: new Date(now).toISOString(), maxDownloads: 3, downloads: 3 };
    expect(shareState(share, now - 1)).toBe('exhausted'); expect(shareState(share, now)).toBe('expired'); expect(shareState({ ...share, revoked: true }, now)).toBe('revoked'); expect(shareState({ revoked: false, maxDownloads: 0, downloads: 1000 }, now)).toBe('active');
  });
  it('offers remote creation only from completed received transfers', () => {
    expect(canShareTransfer({ direction: 'receive', state: 'completed' } as Transfer)).toBe(true); expect(canShareTransfer({ direction: 'receive', state: 'running' } as Transfer)).toBe(false); expect(canShareTransfer({ direction: 'send', state: 'completed' } as Transfer)).toBe(false);
  });
});
describe('explicit clipboard text', () => {
  it('enforces UTF-8 bytes and exact 64KiB boundaries', () => {
    expect(clipboardBytes('😀')).toBe(4); expect(validClipboardText('')).toBe(false); expect(validClipboardText('a'.repeat(CLIPBOARD_LIMIT))).toBe(true); expect(validClipboardText('a'.repeat(CLIPBOARD_LIMIT + 1))).toBe(false); expect(validClipboardText('😀'.repeat(CLIPBOARD_LIMIT / 4))).toBe(true); expect(validClipboardText('😀'.repeat(CLIPBOARD_LIMIT / 4 + 1))).toBe(false);
  });
  it('labels host directions correctly in the browser', () => { expect(browserClipboardDirection('send')).toBe('incoming'); expect(browserClipboardDirection('receive')).toBe('outgoing'); });
});
