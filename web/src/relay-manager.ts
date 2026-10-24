import { api, APIError, post } from './api';
import { hashFile } from './hash';
import { retryDelay, safePath, validateSelection } from './protocol';
import { abortableDelay, relayCanServe, relayStopped, relayTargetAllowed, requestedChunk, selectRelaySources, validateRelayManifest } from './relay-protocol';
import { saveRelayManifest } from './relay-storage';
import { CHUNK_SIZE } from './types';
import type { Relay, RelayManifest, RelayRequest, SavedFile, Selection, Status } from './types';
export interface RelayLocal { phase: 'source' | 'hashing' | 'ready' | 'waiting' | 'reconnecting' | 'error'; done?: number; total?: number; error?: string }
export interface RelayPreparing { path: string; done: number; total: number }
const relayPath = (id: string) => `/relays/${encodeURIComponent(id)}`;
export class RelayManager {
  private status?: Status;
  private manifests = new Map<string, RelayManifest>();
  private sources = new Map<string, Map<string, File>>();
  private checked = new WeakMap<File, { sha256: string; chunks: string[] }>();
  private jobs = new Map<string, AbortController>();
  private attachments = new Map<string, AbortController>();
  private activeChunks = 0;
  private local = new Map<string, RelayLocal>();
  private preparation?: AbortController;
  private closed = false;
  constructor(private changed: (state: Map<string, RelayLocal>) => void, private preparing: (state?: RelayPreparing) => void, private unauthorized: () => void) {}
  private report(id: string, value: RelayLocal) { this.local.set(id, value); this.changed(new Map(this.local)); }
  restore(values: RelayManifest[]) { for (const value of values) this.manifests.set(value.id, value); this.pump(); }
  update(status: Status) {
    this.status = status;
    for (const [id, job] of this.jobs) { const relay = status.relays?.find(value => value.id === id); if (!relay || !relayCanServe(relay, status.currentDeviceId || '')) job.abort(); }
    for (const [id, job] of this.attachments) { const relay = status.relays?.find(value => value.id === id); if (!relay || relayStopped(relay.state)) job.abort(); }
    this.pump();
  }
  stop() { this.closed = true; this.preparation?.abort(); for (const job of this.jobs.values()) job.abort(); for (const job of this.attachments.values()) job.abort(); }
  cancelPreparation() { this.preparation?.abort(); }
  async create(receiverId: string, selection: Selection[]): Promise<Relay> {
    const status = this.status;
    if (this.closed || !status || !relayTargetAllowed(status, receiverId)) throw new Error('The receiving browser is unavailable or relay permission is missing.');
    if (this.preparation || this.attachments.size) throw new Error('Source files are already being checked.');
    validateSelection(selection);
    const controller = new AbortController(); this.preparation = controller;
    try {
      const files: SavedFile[] = [];
      for (const item of selection) {
        const entry: SavedFile = { id: crypto.randomUUID(), path: safePath(item.path), directory: item.directory, size: item.file?.size || 0, modified: item.file?.lastModified || 0 };
        if (item.file) {
          let checksums = this.checked.get(item.file);
          if (!checksums) { this.preparing({ path: item.path, done: 0, total: item.file.size }); checksums = await hashFile(item.file, CHUNK_SIZE, done => this.preparing({ path: item.path, done, total: item.file!.size }), controller.signal); this.checked.set(item.file, checksums); }
          Object.assign(entry, checksums);
        }
        files.push(entry);
      }
      if (controller.signal.aborted) throw new DOMException('Aborted', 'AbortError');
      if (!this.status || !relayTargetAllowed(this.status, receiverId)) throw new Error('The receiving browser is no longer available.');
      const wire = files.map(({ chunks: _chunks, ...file }) => file);
      const relay = await api<Relay>('/relays', { method: 'POST', body: JSON.stringify({ receiverId, files: wire }), signal: controller.signal });
      const manifest: RelayManifest = { id: relay.id, hostId: status.id, senderId: status.currentDeviceId!, receiverId, files, updatedAt: Date.now() };
      validateRelayManifest(relay, manifest);
      try { await saveRelayManifest(manifest); } catch (error) { await post(`${relayPath(relay.id)}/cancel`).catch(() => {}); throw error; }
      this.manifests.set(relay.id, manifest); this.sources.set(relay.id, new Map(selection.filter(item => item.file).map(item => [item.path, item.file!]))); this.report(relay.id, { phase: 'waiting' });
      if (this.status) this.status = { ...this.status, relays: [relay, ...(this.status.relays || []).filter(value => value.id !== relay.id)] };
      this.pump(); return relay;
    } finally { this.preparation = undefined; this.preparing(undefined); }
  }
  async attach(id: string, selection: Selection[]) {
    validateSelection(selection);
    if (this.preparation || this.attachments.size) throw new Error('Source files are already being checked.');
    const relay = this.status?.relays?.find(value => value.id === id);
    if (!relay || relay.senderId !== this.status?.currentDeviceId || relayStopped(relay.state)) throw new Error('Relay is unavailable.');
    let manifest = this.manifests.get(id);
    if (!manifest) manifest = { id, hostId: this.status!.id, senderId: relay.senderId, receiverId: relay.receiverId, files: relay.files.map(file => ({ id: file.id, path: file.path, size: file.size, modified: file.modified || 0, directory: !!file.directory, sha256: file.sha256 })), updatedAt: Date.now() };
    validateRelayManifest(relay, manifest);
    const sources = selectRelaySources(manifest, selection);
    const controller = new AbortController();
    this.jobs.get(id)?.abort(); this.attachments.set(id, controller);
    try {
      for (const entry of manifest.files) {
        if (entry.directory) continue;
        const file = sources.get(entry.path)!; this.report(id, { phase: 'hashing', done: 0, total: file.size });
        const checksums = await hashFile(file, CHUNK_SIZE, done => this.report(id, { phase: 'hashing', done, total: file.size }), controller.signal);
        if (checksums.sha256 !== entry.sha256) throw new Error('The selected source contents have changed.');
        if (entry.chunks && (entry.chunks.length !== checksums.chunks.length || entry.chunks.some((hash, index) => hash !== checksums.chunks[index]))) throw new Error('The selected source contents have changed.');
        Object.assign(entry, checksums); this.checked.set(file, checksums);
      }
      await saveRelayManifest(manifest); this.manifests.set(id, manifest); this.sources.set(id, sources); this.report(id, { phase: 'waiting' });
    } finally { this.attachments.delete(id); this.pump(); }
  }
  private pump() {
    if (this.closed || !this.status) return;
    for (const relay of this.status.relays || []) {
      if (relay.senderId !== this.status.currentDeviceId || relayStopped(relay.state) || this.attachments.has(relay.id)) continue;
      if (!this.sources.has(relay.id)) { this.report(relay.id, { phase: 'source' }); continue; }
      if (!relayCanServe(relay, this.status.currentDeviceId || '') || this.jobs.has(relay.id) || this.local.get(relay.id)?.phase === 'error') continue;
      const controller = new AbortController(); this.jobs.set(relay.id, controller);
      void this.serve(relay.id, controller.signal).catch(error => {
        if (controller.signal.aborted || error instanceof DOMException && error.name === 'AbortError' || error instanceof APIError && error.status === 404) return;
        if (error instanceof APIError && error.status === 401) { this.unauthorized(); return; }
        this.report(relay.id, { phase: 'error', error: error instanceof Error ? error.message : 'Relay failed' });
      }).finally(() => { this.jobs.delete(relay.id); });
    }
  }
  private async serve(id: string, signal: AbortSignal) {
    let attempt = 0;
    while (!signal.aborted) {
      try {
        const relay = await api<Relay>(relayPath(id), { signal });
        if (!relayCanServe(relay, this.status?.currentDeviceId || '')) return;
        const manifest = this.manifests.get(id); const sources = this.sources.get(id);
        if (!manifest || !sources) { this.report(id, { phase: 'source' }); return; }
        validateRelayManifest(relay, manifest);
        this.report(id, { phase: 'waiting' });
        const { request } = await api<{ request: RelayRequest | null }>(`${relayPath(id)}/request`, { signal });
        if (!request) { await abortableDelay(relay.state === 'delivered' ? 3000 : 600, signal); continue; }
        if (this.activeChunks >= 2) { await abortableDelay(100, signal); continue; }
        const chunk = requestedChunk(relay, manifest, request); const file = sources.get(chunk.file.path);
        if (!file || file.size !== chunk.file.size || file.lastModified !== chunk.file.modified) throw new Error('Source access was lost. Choose the original files again.');
        this.activeChunks++;
        try {
          const body = await file.slice(chunk.offset, chunk.end).arrayBuffer();
          if (body.byteLength > CHUNK_SIZE || body.byteLength !== chunk.end - chunk.offset) throw new Error('Invalid source chunk size');
          const result = await api<{ delivered: boolean }>(`${relayPath(id)}/files/${encodeURIComponent(chunk.file.id)}/chunks/${request.index}?requestId=${encodeURIComponent(request.requestId)}`, { method: 'PUT', headers: { 'Content-Type': 'application/octet-stream', 'X-Chunk-SHA256': chunk.hash }, body, signal });
          if (result.delivered !== true) throw new Error('Relay did not confirm transport consumption');
        } finally { this.activeChunks--; }
        attempt = 0; this.report(id, { phase: 'ready' });
      } catch (error) {
        if (signal.aborted) throw new DOMException('Aborted', 'AbortError');
        if (error instanceof APIError && error.status === 409) { await abortableDelay(200, signal); continue; }
        if (!(error instanceof TypeError) && !(error instanceof APIError && (error.status >= 500 || error.status === 408 || error.status === 429))) throw error;
        this.report(id, { phase: 'reconnecting' }); await abortableDelay(retryDelay(attempt++), signal);
      }
    }
  }
}
