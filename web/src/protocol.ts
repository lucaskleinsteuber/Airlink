import { CHUNK_SIZE } from './types';
import type { RemoteFile, SavedFile, Selection, Transfer } from './types';

export function safePath(input: string): string {
  const path = input.replaceAll('\\', '/');
  const parts = path.split('/');
  if (!path || path.startsWith('/') || /^[a-z]:/i.test(path) || parts.some(p => !p || p === '.' || p === '..' || /[\u0000-\u001f]/.test(p))) throw new Error('Unsafe relative path');
  return parts.join('/');
}
export function validateSelection(items: Selection[]): void {
  const paths = new Set<string>();
  for (const item of items) {
    const path = safePath(item.path);
    if (paths.has(path)) throw new Error('Duplicate relative path');
    if (!item.directory && (!item.file || !Number.isSafeInteger(item.file.size))) throw new Error('Invalid source file');
    paths.add(path);
  }
  if (!items.length) throw new Error('No files selected');
}
export function resumeIndex(file: Pick<RemoteFile, 'received' | 'size'>, chunkSize = CHUNK_SIZE): number {
  if (!Number.isSafeInteger(file.size) || file.size < 0 || !Number.isSafeInteger(file.received) || file.received < 0 || file.received > file.size || (file.received !== file.size && file.received % chunkSize !== 0)) throw new Error('Invalid confirmed offset');
  return Math.ceil(file.received / chunkSize);
}
export function verifySource(saved: SavedFile, current: SavedFile, remote: RemoteFile, chunkSize = CHUNK_SIZE): void {
  if (saved.path !== current.path || saved.size !== current.size || saved.modified !== current.modified || saved.directory !== current.directory) throw new Error('Selected source has changed');
  if (saved.sha256 && current.sha256 !== saved.sha256) throw new Error('Selected source has changed');
  const count = resumeIndex(remote, chunkSize);
  if (count && (!saved.chunks || !current.chunks || saved.chunks.length < count || current.chunks.length < count)) throw new Error('Saved checksums are missing. Start a new transfer.');
  for (let i = 0; i < count; i++) if (saved.chunks![i] !== current.chunks![i]) throw new Error('Previously sent source data has changed');
}
export function canUpload(transfer: Transfer): boolean { return transfer.direction === 'receive' && transfer.state === 'running'; }
export function shouldAbortUpload(state: string): boolean { return ['paused', 'cancelled', 'failed', 'reconnecting', 'offered', 'queued'].includes(state); }
export function terminal(state: string): boolean { return ['completed', 'failed', 'cancelled'].includes(state); }
export function retryDelay(attempt: number): number { return Math.min(30_000, 750 * 2 ** Math.min(6, Math.max(0, attempt))); }
export function progressPercent(done: number, total: number): number { return total > 0 ? Math.min(100, Math.max(0, done / total * 100)) : 0; }
export function bytes(value: number, locale: string): string {
  if (!Number.isFinite(value) || value <= 0) return '0 B';
  const unit = Math.min(4, Math.floor(Math.log(value) / Math.log(1000)));
  return `${new Intl.NumberFormat(locale, { maximumFractionDigits: unit ? 1 : 0 }).format(value / 1000 ** unit)} ${['B', 'KB', 'MB', 'GB', 'TB'][unit]}`;
}
