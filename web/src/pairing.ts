import { post } from './api';
import type { PendingPair } from './types';

type PairStorage = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>;
type StorageAccess = () => PairStorage;
const browserSession: StorageAccess = () => sessionStorage;
const key = 'airlink-pair';

export function loadPendingPair(storage: StorageAccess = browserSession): PendingPair | null {
  try {
    const value: unknown = JSON.parse(storage().getItem(key) || 'null');
    if (!value || typeof value !== 'object') return null;
    const pending = value as Partial<PendingPair>;
    return typeof pending.requestId === 'string' && !!pending.requestId && typeof pending.pollToken === 'string' && !!pending.pollToken ? { requestId: pending.requestId, pollToken: pending.pollToken } : null;
  } catch { return null; }
}

export function clearPendingPair(storage: StorageAccess = browserSession): void {
  try { storage().removeItem(key); } catch { /* Session persistence is optional; the active page keeps its pairing state in memory. */ }
}

export async function requestPair(name: string, storage: StorageAccess = browserSession): Promise<PendingPair> {
  const pending = await post<PendingPair>('/pair/request', { name: name.trim(), kind: 'browser' });
  try { storage().setItem(key, JSON.stringify(pending)); } catch { /* A blocked store only prevents recovery after a page reload. */ }
  return pending;
}

export async function pollPair(pending: PendingPair, storage: StorageAccess = browserSession): Promise<{ state: string }> {
  const result = await post<{ state: string }>('/pair/poll', pending);
  if (result.state === 'approved' || result.state === 'denied') clearPendingPair(storage);
  return result;
}
