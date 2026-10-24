import { safePath } from './protocol';
import { CHUNK_SIZE } from './types';
import type { Relay, RelayManifest, RelayRequest, SavedFile, Selection, Status } from './types';
export function relayStopped(state: string): boolean { return state === 'failed' || state === 'cancelled'; }
export function relayTerminal(state: string): boolean { return relayStopped(state) || state === 'delivered'; }
export function relayCanServe(relay: Relay, senderId: string): boolean { return relay.senderId === senderId && ['ready', 'queued', 'running', 'reconnecting', 'delivered'].includes(relay.state); }
export function relayTargetAllowed(status: Status, targetId: string): boolean {
  return !!status.currentDeviceId && targetId !== status.currentDeviceId && !!status.relayDevices?.some(device => device.id === targetId && device.kind === 'browser' && device.online && device.canRelay && device.canReceive);
}
export function validateRelayManifest(relay: Relay, manifest: RelayManifest): void {
  if (relay.senderId !== manifest.senderId || relay.receiverId !== manifest.receiverId || relay.files.length !== manifest.files.length) throw new Error('Relay manifest changed');
  for (const source of manifest.files) {
    const remote = relay.files.find(file => file.id === source.id);
    if (!remote || remote.path !== source.path || remote.size !== source.size || !!remote.directory !== source.directory || (remote.modified || 0) !== source.modified || (!source.directory && (!/^[a-f0-9]{64}$/.test(source.sha256 || '') || source.sha256 !== remote.sha256))) throw new Error('Relay manifest changed');
  }
}
export function requestedChunk(relay: Relay, manifest: RelayManifest, request: RelayRequest): { file: SavedFile; offset: number; end: number; hash: string } {
  validateRelayManifest(relay, manifest);
  if (!request.requestId || request.requestId.length > 256 || !Number.isSafeInteger(request.index) || request.index < 0) throw new Error('Invalid relay request');
  const file = manifest.files.find(file => file.id === request.fileId);
  if (!file || file.directory || !Number.isSafeInteger(file.size) || file.size <= 0) throw new Error('Invalid relay file request');
  const offset = request.index * CHUNK_SIZE;
  if (!Number.isSafeInteger(offset) || offset >= file.size) throw new Error('Invalid relay chunk range');
  const hash = file.chunks?.[request.index];
  if (!hash || !/^[a-f0-9]{64}$/.test(hash)) throw new Error('Source checksums are missing. Choose the original files again.');
  return { file, offset, end: Math.min(file.size, offset + CHUNK_SIZE), hash };
}
export function selectRelaySources(manifest: RelayManifest, selection: Selection[]): Map<string, File> {
  const expected = manifest.files.filter(file => !file.directory);
  const files = new Map(selection.filter(item => !item.directory).map(item => [safePath(item.path), item.file!]));
  if (files.size !== expected.length) throw new Error('Choose the same files and folder structure.');
  for (const source of expected) {
    const file = files.get(source.path);
    if (!file || file.size !== source.size || file.lastModified !== source.modified) throw new Error('Choose the original, unchanged files.');
  }
  return files;
}
export function abortableDelay(ms: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    const abort = () => { clearTimeout(timer); signal.removeEventListener('abort', abort); reject(new DOMException('Aborted', 'AbortError')); };
    const timer = setTimeout(() => { signal.removeEventListener('abort', abort); resolve(); }, ms);
    signal.addEventListener('abort', abort, { once: true }); if (signal.aborted) abort();
  });
}
