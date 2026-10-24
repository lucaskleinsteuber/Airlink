import { createHash } from 'node:crypto';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { CHUNK_SIZE } from './types';
import type { Manifest, Status, Transfer } from './types';
vi.mock('./storage', () => ({ saveManifest: vi.fn(async () => {}) }));
vi.mock('./hash', () => ({ hashFile: vi.fn(async (file: File, chunkSize: number) => {
  const data = new Uint8Array(await file.arrayBuffer());
  const chunks = [];
  for (let start = 0; start < data.length; start += chunkSize) chunks.push(createHash('sha256').update(data.subarray(start, start + chunkSize)).digest('hex'));
  return { sha256: createHash('sha256').update(data).digest('hex'), chunks };
}) }));
import { UploadManager } from './uploader';
function status(transfer: Transfer): Status { return { id: 'host', name: 'Host', version: '0.1.0', chunkSize: CHUNK_SIZE, fingerprint: '', devices: [], transfers: [structuredClone(transfer)] }; }
function fixture(file: File): { transfer: Transfer; manifest: Manifest } {
  const saved = { id: 'file', path: file.name, size: file.size, modified: file.lastModified, directory: false };
  return {
    transfer: { id: 'transfer', deviceId: 'browser', deviceName: 'Browser', direction: 'receive', state: 'running', totalBytes: file.size, completedBytes: 0, bytesPerSecond: 0, createdAt: '', updatedAt: '', files: [{ ...saved, received: 0, completed: false }] },
    manifest: { id: 'transfer', hostId: 'host', files: [saved], updatedAt: 0 },
  };
}
const response = (value: unknown) => new Response(JSON.stringify(value), { headers: { 'Content-Type': 'application/json' } });
afterEach(() => { vi.restoreAllMocks(); vi.useRealTimers(); vi.unstubAllGlobals(); });
describe('upload protocol with an in-memory host', () => {
  it('completes an empty file with the empty SHA-256 digest and sends no chunk', async () => {
    const file = new File([], 'empty.txt', { lastModified: 100 });
    const { transfer, manifest } = fixture(file);
    let completed = false;
    vi.stubGlobal('fetch', vi.fn(async (url: string, options?: RequestInit) => {
      expect(url).not.toContain('/chunks/');
      if (url.endsWith('/prepare')) return new Response(JSON.stringify({ error: 'Old host endpoint not found' }), { status: 404 });
      if (url.endsWith('/complete')) {
        expect(JSON.parse(options!.body as string)).toEqual({ sha256: 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855' });
        completed = true; transfer.files[0].completed = true; transfer.state = 'completed';
      }
      return response(transfer);
    }));
    const manager = new UploadManager(() => {}, () => {});
    manager.restore([manifest]); manager.update(status(transfer));
    await manager.attach(transfer.id, [{ path: file.name, file, directory: false }]);
    await vi.waitFor(() => expect(completed).toBe(true));
    manager.stop();
  });
  it('uses the host checkpoint after an acknowledged block loses its HTTP response', async () => {
    vi.useFakeTimers();
    const file = new File([new Uint8Array(CHUNK_SIZE + 3).fill(7)], 'large.bin', { lastModified: 100 });
    const { transfer, manifest } = fixture(file);
    const offsets: number[] = []; let complete = false; let loseResponse = true;
    vi.stubGlobal('fetch', vi.fn(async (url: string, options?: RequestInit) => {
      if (url.endsWith('/prepare')) return new Response(JSON.stringify({ error: 'Old host endpoint not found' }), { status: 404 });
      if (url.includes('/chunks/')) {
        const index = Number(url.split('/').at(-1)); offsets.push(index);
        const data = new Uint8Array(options!.body as ArrayBuffer);
        const expectedHash = createHash('sha256').update(data).digest('hex');
        expect((options!.headers as Record<string, string>)['X-Chunk-SHA256']).toBe(expectedHash);
        transfer.files[0].received += data.length;
        if (loseResponse) { loseResponse = false; throw new TypeError('Connection dropped after host checkpoint'); }
        return response({ received: transfer.files[0].received });
      }
      if (url.endsWith('/complete')) { transfer.files[0].completed = true; transfer.state = 'completed'; complete = true; return response(transfer); }
      return response(transfer);
    }));
    const manager = new UploadManager(() => {}, () => { throw new Error('Unexpected auth expiry'); });
    manager.restore([manifest]); manager.update(status(transfer));
    await manager.attach(transfer.id, [{ path: file.name, file, directory: false }]);
    await vi.waitFor(() => expect(offsets).toEqual([0]));
    await vi.advanceTimersByTimeAsync(1000);
    await vi.waitFor(() => expect(complete).toBe(true));
    expect(offsets).toEqual([0, 1]); expect(transfer.files[0].received).toBe(file.size);
    manager.stop();
  });
  it('does not cancel the receiver while SSE reports long-running checksum verification', async () => {
    const file = new File(['hello'], 'small.txt', { lastModified: 100 });
    const { transfer, manifest } = fixture(file);
    let verificationSignal: AbortSignal | undefined;
    let finishVerification: (() => void) | undefined;
    let completedResponse = false;
    vi.stubGlobal('fetch', vi.fn(async (url: string, options?: RequestInit) => {
      if (url.endsWith('/prepare')) return new Response(JSON.stringify({ error: 'Old host endpoint not found' }), { status: 404 });
      if (url.includes('/chunks/')) { transfer.files[0].received = file.size; return response({ received: file.size }); }
      if (url.endsWith('/complete')) {
        verificationSignal = options?.signal as AbortSignal;
        await new Promise<void>(resolve => { finishVerification = resolve; });
        completedResponse = true; transfer.state = 'completed'; return response(transfer);
      }
      return response(transfer);
    }));
    const manager = new UploadManager(() => {}, () => {});
    manager.restore([manifest]); manager.update(status(transfer));
    await manager.attach(transfer.id, [{ path: file.name, file, directory: false }]);
    await vi.waitFor(() => expect(finishVerification).toBeTypeOf('function'));
    transfer.state = 'verifying'; manager.update(status(transfer));
    expect(verificationSignal?.aborted).toBe(false);
    finishVerification!();
    await vi.waitFor(() => expect(completedResponse).toBe(true));
    manager.stop();
  });
});
