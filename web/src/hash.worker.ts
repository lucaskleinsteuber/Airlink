import { createSHA256 } from 'hash-wasm';
self.onmessage = async (event: MessageEvent<{ file: File; chunkSize: number }>) => {
  try {
    const { file, chunkSize } = event.data;
    const full = await createSHA256();
    const part = await createSHA256();
    full.init();
    const chunks: string[] = [];
    for (let offset = 0; offset < file.size; offset += chunkSize) {
      const data = new Uint8Array(await file.slice(offset, offset + chunkSize).arrayBuffer());
      full.update(data); part.init(); part.update(data); chunks.push(part.digest('hex'));
      self.postMessage({ progress: Math.min(file.size, offset + chunkSize) });
    }
    self.postMessage({ sha256: full.digest('hex'), chunks });
  } catch (error) { self.postMessage({ error: error instanceof Error ? error.message : 'Checksum failed' }); }
};
