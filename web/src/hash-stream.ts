import { createSHA256 } from 'hash-wasm';
import { CHUNK_SIZE } from './types';
export async function digestFile(file: Pick<File, 'size' | 'slice'>, chunkSize: number, progress: (done: number) => void): Promise<{ sha256: string; chunks: string[] }> {
  if (!Number.isSafeInteger(file.size) || file.size < 0 || !Number.isSafeInteger(chunkSize) || chunkSize <= 0 || chunkSize > CHUNK_SIZE) throw new Error('Invalid checksum input size');
  const full = await createSHA256(); const part = await createSHA256(); full.init(); const chunks: string[] = [];
  for (let offset = 0; offset < file.size; offset += chunkSize) {
    const data = new Uint8Array(await file.slice(offset, offset + chunkSize).arrayBuffer());
    full.update(data); part.init(); part.update(data); chunks.push(part.digest('hex')); progress(Math.min(file.size, offset + chunkSize));
  }
  return { sha256: full.digest('hex'), chunks };
}
