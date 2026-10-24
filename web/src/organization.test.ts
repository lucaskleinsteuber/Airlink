import { createHash } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { runInNewContext } from 'node:vm';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { fileHasPayload, preparationState, transferProgressBytes, validatePrepareResult } from './organization';
import { shouldAbortUpload } from './protocol';
import { CHUNK_SIZE } from './types';
import type { Manifest, Status, Transfer } from './types';
vi.mock('./storage', () => ({ saveManifest: vi.fn(async () => {}) }));
vi.mock('./hash', () => ({ hashFile: vi.fn(async (file: File, chunkSize: number, progress: (done: number) => void) => { const { digestFile } = await import('./hash-stream'); return digestFile(file, chunkSize, progress); }) }));
import { UploadManager } from './uploader';
import { hashFile } from './hash';
import { saveManifest } from './storage';
const managers: UploadManager[] = [];
function source() { const local = vi.fn(); const value = new UploadManager(local, () => {}); managers.push(value); return { manager: value, local }; }
function fixture(text = 'content', fileName = 'file.txt', id = 'file') {
  const file = new File([text], fileName, { lastModified: 100 }); const saved = { id, path: fileName, size: file.size, modified: 100, directory: false };
  const transfer: Transfer = { id: 'transfer', deviceId: 'browser', deviceName: 'Browser', direction: 'receive', state: 'running', files: [{ ...saved, received: 0, completed: false }], totalBytes: file.size, completedBytes: 0, skippedBytes: 0, bytesPerSecond: 0, createdAt: '', updatedAt: '' };
  const manifest: Manifest = { id: 'transfer', hostId: 'host', files: [saved], updatedAt: 0 };
  return { file, transfer, manifest, sha256: createHash('sha256').update(text).digest('hex') };
}
function status(transfer: Transfer): Status { return { id: 'host', name: 'Host', version: '0.4.0', chunkSize: CHUNK_SIZE, fingerprint: '', devices: [], transfers: [structuredClone(transfer)] }; }
const response = (value: unknown, statusCode = 200) => new Response(JSON.stringify(value), { status: statusCode, headers: { 'Content-Type': 'application/json' } });
function attach(manager: UploadManager, value: ReturnType<typeof fixture>) { manager.restore([value.manifest]); manager.update(status(value.transfer)); return manager.attach(value.transfer.id, [{ path: value.file.name, file: value.file, directory: false }]); }
afterEach(() => { for (const manager of managers.splice(0)) manager.stop(); vi.restoreAllMocks(); vi.clearAllMocks(); vi.unstubAllGlobals(); });
describe('duplicate-aware source preparation', () => {
  it('keeps a checking/pending prepare request alive and waits for host Keep before sending bytes', async () => {
    const value = fixture(); const { manager, local } = source(); let prepareSignal: AbortSignal | undefined; let finishPrepare!: () => void; let prepares = 0; let chunks = 0; let complete = false;
    vi.stubGlobal('fetch', vi.fn(async (url: string, options?: RequestInit) => {
      if (url.endsWith('/prepare')) {
        prepares++; expect(JSON.parse(options!.body as string).sha256).toBe(value.sha256); expect(vi.mocked(saveManifest).mock.calls.at(-1)![0].files[0].sha256).toBe(value.sha256);
        if (prepares === 1) { prepareSignal = options!.signal!; await new Promise<void>(resolve => { finishPrepare = resolve; }); return response({ decision: 'pending', transfer: value.transfer }); }
        return response({ decision: 'upload', transfer: value.transfer });
      }
      if (url.includes('/chunks/')) { chunks++; value.transfer.files[0].received = value.file.size; value.transfer.completedBytes = value.file.size; return response({ received: value.file.size }); }
      if (url.endsWith('/complete')) { value.transfer.files[0].completed = true; value.transfer.state = 'completed'; complete = true; return response(value.transfer); }
      return response(value.transfer);
    }));
    await attach(manager, value); await vi.waitFor(() => expect(prepareSignal).toBeDefined());
    value.transfer.state = 'checkingDuplicate'; manager.update(status(value.transfer)); expect(prepareSignal?.aborted).toBe(false);
    value.transfer.state = 'waitingDuplicate'; value.transfer.files[0].duplicateState = 'pending'; manager.update(status(value.transfer)); expect(prepareSignal?.aborted).toBe(false); finishPrepare();
    await vi.waitFor(() => expect(local.mock.calls.at(-1)![0].get('transfer')?.phase).toBe('waitingDuplicate')); expect(chunks).toBe(0); expect(prepares).toBe(1);
    value.transfer.state = 'running'; value.transfer.files[0].duplicateState = 'keep'; manager.update(status(value.transfer));
    await vi.waitFor(() => expect(complete).toBe(true)); expect(chunks).toBe(1); expect(vi.mocked(hashFile)).toHaveBeenCalledTimes(1);
  });
  it('refreshes host state after a lost pending response without uploading or repeatedly preparing', async () => {
    const value = fixture(); const { manager, local } = source(); let prepares = 0; let chunks = 0;
    vi.stubGlobal('fetch', vi.fn(async (url: string, options?: RequestInit) => {
      if (url.endsWith('/prepare')) { prepares++; value.transfer.state = 'waitingDuplicate'; value.transfer.files[0].duplicateState = 'pending'; throw new TypeError('Response lost after host checkpoint'); }
      if (options?.method === 'PUT') chunks++; return response(value.transfer);
    }));
    await attach(manager, value); await vi.waitFor(() => expect(local.mock.calls.at(-1)![0].get('transfer')?.phase).toBe('waitingDuplicate'), { timeout: 3000 });
    for (let i = 0; i < 4; i++) manager.update(status(value.transfer)); expect(prepares).toBe(1); expect(chunks).toBe(0);
  });
  it('retries an idempotent upload decision after its response is lost', async () => {
    const value = fixture(); const { manager } = source(); let prepares = 0; let complete = false;
    vi.stubGlobal('fetch', vi.fn(async (url: string) => {
      if (url.endsWith('/prepare')) { prepares++; value.transfer.files[0].duplicateState = 'checked'; if (prepares === 1) throw new TypeError('Decision response lost'); return response({ decision: 'upload', transfer: value.transfer }); }
      if (url.includes('/chunks/')) { value.transfer.files[0].received = value.file.size; return response({ received: value.file.size }); }
      if (url.endsWith('/complete')) { value.transfer.files[0].completed = true; value.transfer.state = 'completed'; complete = true; } return response(value.transfer);
    }));
    await attach(manager, value); await vi.waitFor(() => expect(complete).toBe(true), { timeout: 3000 }); expect(prepares).toBe(2); expect(vi.mocked(hashFile)).toHaveBeenCalledTimes(1);
  });
  it('stops a pending409 instead of spinning on upload conflicts', async () => {
    const value = fixture(); const { manager, local } = source(); let prepares = 0; let chunks = 0;
    vi.stubGlobal('fetch', vi.fn(async (url: string, options?: RequestInit) => {
      if (url.endsWith('/prepare')) { prepares++; value.transfer.state = 'waitingDuplicate'; value.transfer.files[0].duplicateState = 'pending'; return response({ error: 'Host decision required' }, 409); }
      if (options?.method === 'PUT') chunks++; return response(value.transfer);
    }));
    await attach(manager, value); await vi.waitFor(() => expect(local.mock.calls.at(-1)![0].get('transfer')?.phase).toBe('waitingDuplicate'));
    for (let i = 0; i < 10; i++) manager.update(status(value.transfer)); expect(prepares).toBe(1); expect(chunks).toBe(0);
  });
  it('observes a host Skip after reload without source access or a payload-completion request', async () => {
    const value = fixture(); value.transfer.state = 'waitingDuplicate'; value.transfer.files[0].duplicateState = 'pending';
    const { manager, local } = source(); const network = vi.fn(); vi.stubGlobal('fetch', network); manager.restore([value.manifest]); manager.update(status(value.transfer));
    expect(local.mock.calls.at(-1)![0].get('transfer')?.phase).toBe('waitingDuplicate');
    value.transfer.files[0].skipped = true; value.transfer.files[0].completed = true; value.transfer.files[0].duplicateState = 'skipped'; value.transfer.skippedBytes = value.file.size; value.transfer.state = 'completed'; manager.update(status(value.transfer));
    expect(network).not.toHaveBeenCalled(); expect(transferProgressBytes(value.transfer)).toBe(value.file.size); expect(value.transfer.completedBytes).toBe(0); expect(fileHasPayload(value.transfer.files[0])).toBe(false);
  });
  it('sends only the unskipped member of a mixed transfer and keeps avoided bytes separate', async () => {
    const first = fixture('same', 'duplicate.txt', 'first'); const second = fixture('new content', 'fresh.txt', 'second');
    const transfer = first.transfer; transfer.files.push(second.transfer.files[0]); transfer.totalBytes += second.file.size; first.manifest.files.push(second.manifest.files[0]);
    const { manager } = source(); const sentFiles: string[] = []; let finished = false;
    vi.stubGlobal('fetch', vi.fn(async (url: string, options?: RequestInit) => {
      if (url.endsWith('/first/prepare')) { transfer.files[0].skipped = true; transfer.files[0].completed = true; transfer.files[0].duplicateState = 'skipped'; transfer.skippedBytes = first.file.size; return response({ decision: 'skip', transfer }); }
      if (url.endsWith('/second/prepare')) return response({ decision: 'upload', transfer });
      if (url.includes('/chunks/')) { const id = url.split('/')[6]; sentFiles.push(id); transfer.files[1].received = (options!.body as ArrayBuffer).byteLength; transfer.completedBytes = transfer.files[1].received; return response({ received: transfer.files[1].received }); }
      if (url.endsWith('/second/complete')) { transfer.files[1].completed = true; transfer.state = 'completed'; finished = true; } return response(transfer);
    }));
    manager.restore([first.manifest]); manager.update(status(transfer)); await manager.attach('transfer', [{ path: first.file.name, file: first.file, directory: false }, { path: second.file.name, file: second.file, directory: false }]);
    await vi.waitFor(() => expect(finished).toBe(true)); expect(sentFiles).toEqual(['second']); expect(transfer.completedBytes).toBe(second.file.size); expect(transfer.skippedBytes).toBe(first.file.size); expect(transferProgressBytes(transfer)).toBe(transfer.totalBytes);
  });
  it('falls back only for a404 legacy endpoint, never for a permissions failure', async () => {
    for (const code of [404, 403]) {
      const value = fixture(); const { manager, local } = source(); let chunks = 0; let complete = false;
      vi.stubGlobal('fetch', vi.fn(async (url: string) => { if (url.endsWith('/prepare')) return response({ error: code === 404 ? 'Legacy endpoint' : 'Forbidden' }, code); if (url.includes('/chunks/')) { chunks++; value.transfer.files[0].received = value.file.size; return response({ received: value.file.size }); } if (url.endsWith('/complete')) { value.transfer.files[0].completed = true; value.transfer.state = 'completed'; complete = true; } return response(value.transfer); }));
      await attach(manager, value);
      if (code === 404) { await vi.waitFor(() => expect(complete).toBe(true)); expect(chunks).toBe(1); }
      else { await vi.waitFor(() => expect(local.mock.calls.at(-1)![0].get('transfer')?.phase).toBe('error')); expect(chunks).toBe(0); }
      manager.stop();
    }
  });
});
describe('organization protocol safeguards', () => {
  it('preserves active checking/waiting requests and requires an explicit confirmed skipped file', () => {
    expect(shouldAbortUpload('checkingDuplicate')).toBe(false); expect(shouldAbortUpload('waitingDuplicate')).toBe(false); expect(shouldAbortUpload('paused')).toBe(true);
    const value = fixture(); expect(preparationState(value.transfer, 'file')).toBe('upload'); value.transfer.state = 'waitingDuplicate'; expect(preparationState(value.transfer, 'file')).toBe('pending');
    expect(() => validatePrepareResult({ decision: 'skip', transfer: value.transfer }, 'transfer', 'file')).toThrow();
  });
});
const go = readFileSync(new URL('../../internal/server/control.go', import.meta.url), 'utf8');
const html = go.slice(go.indexOf('const adminHTML = `') + 'const adminHTML = `'.length, go.lastIndexOf('`'));
const script = html.match(/<script nonce="NONCE">([\s\S]*?)<\/script>/)![1];
const helpers = script.slice(script.indexOf('function safeOrganizationFolder'), script.indexOf('function organizationSettings'));
function folder(value: string) { return runInNewContext(`${helpers};safeOrganizationFolder(value)`, { value, TextEncoder, t: (key: string) => key }); }
function rules(value: unknown) { return JSON.parse(runInNewContext(`${helpers};JSON.stringify(normalizedOrganizationRules(value))`, { value, TextEncoder, t: (key: string) => key })); }
describe('private-console rule validation', () => {
  it('preserves safe UTF-8 folder names and rejects traversal, private staging and platform escapes', () => {
    expect(folder('Bilder/Grüße 🏖')).toBe('Bilder/Grüße 🏖');
    for (const value of ['/etc', '../escape', 'a/../b', 'a//b', 'C:\\Temp', 'a\\b', '.airlink-partials', 'a/.AIRLINK-PARTIALS', 'a/NUL.txt', 'a.', 'a/\u0001b']) expect(() => folder(value)).toThrow('invalidRule');
  });
  it('normalizes leading dots/case while preserving explicit rule order and Unicode folders', () => {
    const result = rules([{ id: 'first', extensionsText: ' .JPG, PNG;csv ', folder: 'Bilder/Urlaub' }, { id: 'second', extensionsText: 'jpg', folder: 'Andere' }]);
    expect(result[0]).toEqual({ id: 'first', extensions: ['jpg', 'png', 'csv'], folder: 'Bilder/Urlaub' }); expect(result[1].id).toBe('second');
    expect(() => rules([{ id: 'a', extensionsText: 'jpg', folder: 'Bilder/One' }, { id: 'b', extensionsText: 'png', folder: 'bilder/Two' }])).toThrow('invalidRule');
  });
  it('matches Go rule and UTF-8 limits without silently normalizing folder Unicode', () => {
    expect(folder('é'.repeat(256))).toBe('é'.repeat(256)); expect(() => folder('é'.repeat(257))).toThrow('invalidRule');
    const decomposed = 'Bilder/e\u0301'; expect(folder(decomposed)).toBe(decomposed); expect(folder(' Bilder')).toBe(' Bilder');
    expect(() => folder('Bilder ')).toThrow('invalidRule');
    const rule = { id: 'one', extensionsText: 'jpg', folder: 'Bilder' };
    for (const extension of ['ä', 'c++', 'a'.repeat(33)]) expect(() => rules([{ ...rule, extensionsText: extension }])).toThrow('invalidRule');
    expect(rules([{ ...rule, extensionsText: '.PNG,svg_2,x-1' }])[0].extensions).toEqual(['png', 'svg_2', 'x-1']);
    expect(() => rules([{ ...rule, id: 'é'.repeat(65) }])).toThrow('invalidRule');
    expect(() => rules([{ ...rule, extensionsText: Array.from({ length: 101 }, (_, index) => 'ext' + index).join(',') }])).toThrow('invalidRule');
    expect(() => rules(Array.from({ length: 101 }, (_, index) => ({ ...rule, id: String(index) })))).toThrow('invalidRule');
  });
  it('creates localized preset drafts without turning either opt-in flag on', () => {
    const definition = script.match(/function organizationPresets\(\)\{[^\n]+\}/)![0]; let id = 0;
    const result = runInNewContext(`${definition};organizationPresets()`, { crypto: { randomUUID: () => `rule-${++id}` }, t: (key: string) => key === 'imagesFolder' ? 'Bilder' : key });
    expect(result).toHaveLength(5); expect(result[0].folder).toBe('Bilder'); expect(result.every((rule: Record<string, unknown>) => !('detectDuplicates' in rule) && !('sortEnabled' in rule))).toBe(true);
    const handler = script.match(/el\('organization-presets'\)\.addEventListener\('click',[^\n]+/)![0]; expect(handler).not.toContain('sortEnabled=true'); expect(handler).not.toContain("call('settings.update'");
  });
});
