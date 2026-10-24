import { api, APIError, post, transferPath } from './api';
import { hashFile } from './hash';
import { canUpload, retryDelay, safePath, shouldAbortUpload, validateSelection, verifySource, resumeIndex, terminal } from './protocol';
import { saveManifest } from './storage';
import { duplicateAttention, preparationState, validatePrepareResult } from './organization';
import type { PrepareResult } from './organization';
import type { Manifest, Selection, Status, Transfer } from './types';
export interface LocalUpload { phase: 'source' | 'hashing' | 'ready' | 'reconnecting' | 'error' | 'waitingDuplicate'; done?: number; total?: number; error?: string }
function wait(ms: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    const abort = () => { clearTimeout(timer); reject(new DOMException('Aborted', 'AbortError')); };
    const timer = setTimeout(() => { signal.removeEventListener('abort', abort); resolve(); }, ms);
    signal.addEventListener('abort', abort, { once: true });
    if (signal.aborted) abort();
  });
}
export class UploadManager {
  private manifests = new Map<string, Manifest>();
  private sources = new Map<string, Map<string, File>>();
  private jobs = new Map<string, AbortController>();
  private prepares = new Set<string>();
  private checkedSources = new WeakMap<File, { sha256: string; chunks: string[] }>();
  private local = new Map<string, LocalUpload>();
  private status?: Status;
  private closed = false;
  constructor(private changed: (state: Map<string, LocalUpload>) => void, private unauthorized: () => void) {}
  private report(id: string, value: LocalUpload) { this.local.set(id, value); this.changed(new Map(this.local)); }
  restore(manifests: Manifest[]) { for (const value of manifests) this.manifests.set(value.id, value); this.pump(); }
  update(status: Status) {
    this.status = status;
    for (const [id, controller] of this.jobs) {
      const transfer = status.transfers.find(value => value.id === id);
      if (!transfer || shouldAbortUpload(transfer.state) && !(this.prepares.has(id) && transfer.state === 'queued')) controller.abort();
    }
    this.pump();
  }
  stop() { this.closed = true; for (const controller of this.jobs.values()) controller.abort(); }
  async create(selection: Selection[], hostId: string): Promise<Transfer> {
    validateSelection(selection);
    const files = selection.map(item => ({ id: crypto.randomUUID(), path: safePath(item.path), size: item.file?.size ?? 0, modified: item.file?.lastModified ?? 0, directory: item.directory }));
    const transfer = await post<Transfer>('/transfers', { files });
    const manifest: Manifest = { id: transfer.id, hostId, files, updatedAt: Date.now() };
    try {
      await saveManifest(manifest); // Persistence must succeed before uploading any bytes.
    } catch (error) {
      await post(`${transferPath(transfer.id)}/cancel`).catch(() => {});
      throw error;
    }
    this.manifests.set(transfer.id, manifest);
    this.sources.set(transfer.id, new Map(selection.filter(item => item.file).map(item => [item.path, item.file!])));
    this.report(transfer.id, { phase: 'ready' });
    if (this.status) this.status = { ...this.status, transfers: [transfer, ...this.status.transfers.filter(item => item.id !== transfer.id)] };
    this.pump(); return transfer;
  }
  async attach(id: string, selection: Selection[]) {
    validateSelection(selection);
    const manifest = this.manifests.get(id);
    if (!manifest) throw new Error('Saved checksums are missing. Start a new transfer.');
    const expected = manifest.files.filter(file => !file.directory);
    const chosen = selection.filter(item => !item.directory);
    if (expected.length !== chosen.length) throw new Error('Choose the same files and folder structure.');
    const files = new Map(chosen.map(item => [item.path, item.file!]));
    for (const saved of expected) {
      const file = files.get(saved.path);
      if (!file || file.size !== saved.size || file.lastModified !== saved.modified) throw new Error('Choose the original, unchanged files.');
    }
    this.sources.set(id, files); this.report(id, { phase: 'ready' }); this.pump();
  }
  private pump() {
    if (this.closed || !this.status) return;
    for (const transfer of this.status.transfers) {
      if (transfer.direction !== 'receive' || terminal(transfer.state)) continue;
      if (duplicateAttention(transfer) && !this.jobs.has(transfer.id)) { this.report(transfer.id, { phase: 'waitingDuplicate' }); continue; }
      if (!this.sources.has(transfer.id)) { this.report(transfer.id, { phase: 'source' }); continue; }
      if (!canUpload(transfer) || this.jobs.has(transfer.id) || this.jobs.size >= 2 || this.local.get(transfer.id)?.phase === 'error') continue;
      const controller = new AbortController(); this.jobs.set(transfer.id, controller);
      void this.upload(transfer, controller.signal).catch(error => {
        if (error instanceof DOMException && error.name === 'AbortError') return;
        if (error instanceof APIError && error.status === 401) { this.unauthorized(); return; }
        this.report(transfer.id, { phase: 'error', error: error instanceof Error ? error.message : 'Transfer failed' });
      }).finally(() => { this.jobs.delete(transfer.id); });
    }
  }
  private async upload(initial: Transfer, signal: AbortSignal) {
    const manifest = this.manifests.get(initial.id);
    const sources = this.sources.get(initial.id);
    if (!manifest || !sources || !this.status) return;
    const chunkSize = this.status.chunkSize;
    if (chunkSize !== 8 * 1024 * 1024) throw new Error('Unsupported protocol chunk size');
    for (const saved of manifest.files) {
      if (saved.directory) continue;
      let transfer = await this.refresh(initial.id, signal);
      if (!canUpload(transfer)) return;
      let remote = transfer.files.find(file => file.id === saved.id);
      if (!remote) throw new Error('Transfer manifest changed');
      if (remote.completed || remote.skipped) continue;
      const source = sources.get(saved.path);
      if (!source) { this.report(initial.id, { phase: 'source' }); return; }
      let checksums = this.checkedSources.get(source);
      if (!checksums) {
        this.report(initial.id, { phase: 'hashing', done: 0, total: source.size });
        checksums = await hashFile(source, chunkSize, done => this.report(initial.id, { phase: 'hashing', done, total: source.size }), signal);
        this.checkedSources.set(source, checksums);
      }
      const current = { ...saved, size: source.size, modified: source.lastModified, ...checksums };
      verifySource(saved, current, remote, chunkSize);
      Object.assign(saved, checksums); manifest.updatedAt = Date.now(); await saveManifest(manifest);
      const prepared = await this.prepareFile(initial.id, saved.id, checksums.sha256, signal);
      transfer = prepared.transfer;
      remote = transfer.files.find(file => file.id === saved.id);
      if (!remote) throw new Error('Transfer manifest changed');
      if (remote.completed || remote.skipped) continue;
      if (prepared.decision !== 'upload' || !canUpload(transfer)) { this.report(initial.id, { phase: duplicateAttention(transfer) || prepared.decision === 'pending' ? 'waitingDuplicate' : 'ready' }); return; }
      this.report(initial.id, { phase: 'ready' });
      let attempt = 0;
      while (!signal.aborted) {
        try {
          transfer = await this.refresh(initial.id, signal);
          if (!canUpload(transfer)) { if (duplicateAttention(transfer)) this.report(initial.id, { phase: 'waitingDuplicate' }); return; }
          remote = transfer.files.find(file => file.id === saved.id);
          if (!remote) throw new Error('Transfer manifest changed');
          if (remote.completed || remote.skipped) break;
          const index = resumeIndex(remote, chunkSize);
          if (remote.received === remote.size) {
            await api<Transfer>(`${transferPath(initial.id)}/files/${encodeURIComponent(saved.id)}/complete`, { method: 'POST', body: JSON.stringify({ sha256: checksums.sha256 }), signal });
            break;
          }
          const body = await source.slice(remote.received, Math.min(source.size, remote.received + chunkSize)).arrayBuffer();
          const confirmed = await api<{ received: number }>(`${transferPath(initial.id)}/files/${encodeURIComponent(saved.id)}/chunks/${index}`, { method: 'PUT', headers: { 'Content-Type': 'application/octet-stream', 'X-Chunk-SHA256': checksums.chunks[index] }, body, signal });
          resumeIndex({ received: confirmed.received, size: remote.size }, chunkSize);
          if (confirmed.received < Math.min(remote.size, remote.received + chunkSize)) throw new Error('Invalid confirmed offset');
          attempt = 0; this.report(initial.id, { phase: 'ready' });
        } catch (error) {
          if (signal.aborted) throw new DOMException('Aborted', 'AbortError');
          if (error instanceof APIError && error.status === 401) throw error;
          if (error instanceof APIError && error.status === 409) { await wait(500, signal); continue; }
          if (!(error instanceof TypeError) && !(error instanceof APIError && (error.status >= 500 || error.status === 408 || error.status === 429))) throw error;
          this.report(initial.id, { phase: 'reconnecting' });
          await wait(retryDelay(attempt++), signal);
        }
      }
      if (signal.aborted) throw new DOMException('Aborted', 'AbortError');
    }
  }
  private async prepareFile(id: string, fileId: string, sha256: string, signal: AbortSignal): Promise<{ decision: 'upload' | 'pending' | 'skip' | 'wait'; transfer: Transfer }> {
    let attempt = 0;
    while (!signal.aborted) {
      const current = await this.refresh(id, signal);
      const state = preparationState(current, fileId);
      if (state !== 'upload') return { decision: state, transfer: current };
      this.prepares.add(id);
      try {
        return validatePrepareResult(await api<PrepareResult>(`${transferPath(id)}/files/${encodeURIComponent(fileId)}/prepare`, { method: 'POST', body: JSON.stringify({ sha256 }), signal }), id, fileId);
      } catch (error) {
        if (signal.aborted) throw new DOMException('Aborted', 'AbortError');
        if (error instanceof APIError && error.status === 404) {
          const legacy = await this.refresh(id, signal);
          return { decision: preparationState(legacy, fileId), transfer: legacy };
        }
        if (error instanceof APIError && error.status === 409) {
          const changed = await this.refresh(id, signal);
          const decision = preparationState(changed, fileId);
          return { decision: decision === 'upload' ? 'wait' : decision, transfer: changed };
        }
        if (!(error instanceof TypeError) && !(error instanceof APIError && (error.status >= 500 || error.status === 408 || error.status === 429))) throw error;
        this.report(id, { phase: 'reconnecting' }); await wait(retryDelay(attempt++), signal);
      } finally { this.prepares.delete(id); }
    }
    throw new DOMException('Aborted', 'AbortError');
  }
  private async refresh(id: string, signal: AbortSignal): Promise<Transfer> {
    let attempt = 0;
    while (!signal.aborted) {
      try {
        const value = await api<Transfer>(transferPath(id), { signal });
        if (this.local.get(id)?.phase === 'reconnecting') this.report(id, { phase: 'ready' });
        return value;
      } catch (error) {
        if (signal.aborted) break;
        if (!(error instanceof TypeError) && !(error instanceof APIError && (error.status >= 500 || error.status === 408 || error.status === 429))) throw error;
        this.report(id, { phase: 'reconnecting' });
        await wait(retryDelay(attempt++), signal);
      }
    }
    throw new DOMException('Aborted', 'AbortError');
  }
}
