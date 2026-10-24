import { describe, expect, it } from 'vitest';
import { bytes, canUpload, progressPercent, resumeIndex, retryDelay, safePath, shouldAbortUpload, validateSelection, verifySource } from './protocol';
import { CHUNK_SIZE } from './types';
import type { RemoteFile, SavedFile, Transfer } from './types';
const base: SavedFile = { id: 'file', path: 'Photos/test.jpg', size: CHUNK_SIZE * 2 + 17, modified: 123, directory: false, sha256: 'full', chunks: ['a', 'b', 'c'] };
const remote = (received: number): RemoteFile => ({ ...base, received, completed: false });
describe('relative paths', () => {
  it('preserves nested and Unicode file names', () => { expect(safePath('Fotos/Urlaub/Grüße 🏖.jpg')).toBe('Fotos/Urlaub/Grüße 🏖.jpg'); });
  it('rejects literal backslashes without renaming a Unix file or allowing traversal', () => {
    for (const path of ['Fotos\\Urlaub.jpg', '../secret', '/etc/passwd', 'C:\\Users\\example\\secret', 'one/../../two', 'one//two', 'one/./two', 'a\0b', '\\server\\share', 'folder/']) expect(() => safePath(path)).toThrow();
  });
  it('rejects the same reserved names and unsupported characters as the host', () => {
    for (const path of ['CON', 'nested/nul.txt', 'PrN.zip', 'AUX', 'COM1.log', 'lpt9', 'folder/.AIRLINK-PARTIALS/file', 'folder/name.', 'name ', 'notes?.txt', 'a:b', 'a<b', 'a>b', 'a"b', 'a|b', 'a*b', 'C1\u0085control.txt', 'DEL\u007f.txt']) expect(() => safePath(path), path).toThrow();
    for (const path of ['CONSOLE.txt', 'COM0.txt', 'COM10.txt', '.airlink-partials-other', 'folder/.hidden', 'Datei e\u0301.txt', 'Datei é.txt']) expect(safePath(path)).toBe(path);
  });
  it('rejects duplicate letter case and files that occupy a parent directory before IO', () => {
    const source = (path: string) => ({ path, directory: false, file: new File(['data'], path) });
    for (const paths of [['Readme.md', 'README.md'], ['Folder', 'folder/file.txt'], ['İ.txt', 'i.txt'], ['ΟΣ.txt', 'οσ.txt']]) expect(() => validateSelection(paths.map(source))).toThrow(/Conflicting/);
    expect(() => validateSelection([{ path: 'Folder', directory: true }, source('Folder/file.txt')])).not.toThrow();
    expect(() => validateSelection([source('é.txt'), source('e\u0301.txt')])).not.toThrow();
  });
});
describe('resume safety', () => {
  it('continues only at complete server-confirmed chunk boundaries', () => {
    expect(resumeIndex(remote(0))).toBe(0); expect(resumeIndex(remote(CHUNK_SIZE))).toBe(1); expect(resumeIndex(remote(base.size))).toBe(3);
    for (const received of [-1, 0.5, CHUNK_SIZE + 1, base.size + 1, Number.NaN]) expect(() => resumeIndex(remote(received))).toThrow();
  });
  it('accepts unchanged files after a restart', () => { expect(() => verifySource(base, { ...base, chunks: [...base.chunks!] }, remote(CHUNK_SIZE))).not.toThrow(); });
  it('rejects changed source bytes even if name, timestamp and size match', () => {
    expect(() => verifySource(base, { ...base, sha256: 'different' }, remote(CHUNK_SIZE))).toThrow();
    expect(() => verifySource({ ...base, sha256: undefined }, { ...base, chunks: ['changed', 'b', 'c'] }, remote(CHUNK_SIZE))).toThrow();
  });
  it('never resumes received data without saved integrity evidence', () => { expect(() => verifySource({ ...base, chunks: undefined }, base, remote(CHUNK_SIZE))).toThrow(/missing/); });
  it('rejects changed file metadata or a different relative path', () => {
    for (const change of [{ path: 'other/test.jpg' }, { size: base.size + 1 }, { modified: 124 }]) expect(() => verifySource(base, { ...base, ...change }, remote(0))).toThrow();
  });
  it('handles empty files with no nonexistent chunk', () => { expect(resumeIndex({ received: 0, size: 0 })).toBe(0); });
});
describe('transfer state policy', () => {
  it('only sends bytes for a host-approved running upload', () => {
    for (const state of ['offered', 'queued', 'paused', 'reconnecting', 'verifying', 'completed', 'failed', 'cancelled']) expect(canUpload({ direction: 'receive', state } as Transfer)).toBe(false);
    expect(canUpload({ direction: 'send', state: 'running' } as Transfer)).toBe(false); expect(canUpload({ direction: 'receive', state: 'running' } as Transfer)).toBe(true);
  });
  it('keeps server checksum verification alive while allowing pause and cancellation', () => {
    expect(shouldAbortUpload('verifying')).toBe(false); expect(shouldAbortUpload('completed')).toBe(false);
    for (const state of ['paused', 'cancelled', 'failed', 'reconnecting', 'queued']) expect(shouldAbortUpload(state)).toBe(true);
  });
  it('caps retry backoff and display progress', () => {
    expect(retryDelay(0)).toBe(750); expect(retryDelay(100)).toBe(30_000); expect(progressPercent(110, 100)).toBe(100); expect(progressPercent(0, 0)).toBe(0);
    expect(bytes(1000, 'en')).toBe('1 KB'); expect(bytes(Number.NaN, 'de')).toBe('0 B');
  });
});
