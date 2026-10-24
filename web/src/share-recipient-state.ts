import { api, APIError } from './api';
import type { Share } from './types';

export type RecipientMetadata =
  | { phase: 'ready'; share: Share; poll: true }
  | { phase: 'gone'; missing: boolean; poll: false }
  | { phase: 'locked'; poll: false }
  | { phase: 'unavailable'; poll: true };

// The host enforces expiry using its own clock. A recipient's clock may be
// incorrect while offline, so a successful response must remain downloadable.
export async function recipientMetadata(path: string): Promise<RecipientMetadata> {
  try {
    const share = await api<Share>(path);
    return share.revoked ? { phase: 'gone', missing: false, poll: false } : { phase: 'ready', share, poll: true };
  } catch (error) {
    if (error instanceof APIError) {
      if (error.status === 404 || error.status === 410) return { phase: 'gone', missing: error.status === 404, poll: false };
      if (error.status === 401 || error.status === 403) return { phase: 'locked', poll: false };
      if (error.status === 409) return { phase: 'unavailable', poll: true };
    }
    throw error;
  }
}
