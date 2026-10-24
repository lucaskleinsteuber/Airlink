import type { Share, ShareCreate, Transfer } from './types';
export const CLIPBOARD_LIMIT = 64 * 1024;
export interface ShareDraft { name: string; password: string; expiresAt: string; noExpiry: boolean; maxDownloads: string }
export function routeFor(pathname: string): { kind: 'app' } | { kind: 'share'; id: string } | { kind: 'invalid-share' } {
  if (!pathname.startsWith('/s/')) return { kind: 'app' };
  const match = /^\/s\/([A-Za-z0-9_-]{1,128})\/?$/.exec(pathname);
  return match ? { kind: 'share', id: match[1] } : { kind: 'invalid-share' };
}
export function consumeShareSecret(id: string, location: Pick<Location, 'hash' | 'pathname' | 'search'>, storage: Pick<Storage, 'getItem' | 'setItem'>, replace: (url: string) => void): string {
  const key = `airlink-share:${id}:secret`;
  let secret = '';
  if (location.hash.length > 1) {
    try { secret = decodeURIComponent(location.hash.slice(1)); } catch { secret = ''; }
    try { if (secret) storage.setItem(key, secret); } catch { /* A blocked session store limits recovery to this page lifetime. */ }
  } else {
    try { secret = storage.getItem(key) || ''; } catch { /* No durable browser storage is required. */ }
  }
  if (location.hash) replace(location.pathname + location.search);
  return secret;
}
export function defaultExpiry(now = Date.now()): string {
  const date = new Date(now + 24 * 60 * 60 * 1000);
  return new Date(date.getTime() - date.getTimezoneOffset() * 60_000).toISOString().slice(0, 16);
}
export function defaultShareDraft(now = Date.now()): ShareDraft { return { name: '', password: '', expiresAt: defaultExpiry(now), noExpiry: false, maxDownloads: '0' }; }
export function canShareTransfer(transfer: Transfer): boolean { return transfer.direction === 'receive' && transfer.state === 'completed'; }
export function createSharePolicy(draft: ShareDraft, now = Date.now()): Omit<ShareCreate, 'transferId' | 'paths'> {
  const maxDownloads = Number(draft.maxDownloads);
  if (!draft.maxDownloads.trim() || !Number.isSafeInteger(maxDownloads) || maxDownloads < 0) throw new Error('downloadLimit');
  const expires = draft.noExpiry ? undefined : new Date(draft.expiresAt);
  if (expires && (!Number.isFinite(expires.getTime()) || expires.getTime() <= now)) throw new Error('expiryInvalid');
  return { name: draft.name.trim(), ...(draft.password ? { password: draft.password } : {}), ...(expires ? { expiresAt: expires.toISOString() } : {}), maxDownloads };
}
export type ShareState = 'active' | 'expired' | 'revoked' | 'exhausted';
export function shareState(share: Pick<Share, 'revoked' | 'expiresAt' | 'maxDownloads' | 'downloads'>, now = Date.now()): ShareState {
  if (share.revoked) return 'revoked';
  if (share.expiresAt && Date.parse(share.expiresAt) <= now) return 'expired';
  if (share.maxDownloads > 0 && share.downloads >= share.maxDownloads) return 'exhausted';
  return 'active';
}
export function clipboardBytes(text: string): number { return new TextEncoder().encode(text).byteLength; }
export function validClipboardText(text: string): boolean { const count = clipboardBytes(text); return count > 0 && count <= CLIPBOARD_LIMIT; }
export function browserClipboardDirection(direction: string): 'incoming' | 'outgoing' { return direction === 'send' ? 'incoming' : 'outgoing'; }
export const publicSharePath = (id: string) => `/public/shares/${encodeURIComponent(id)}`;
