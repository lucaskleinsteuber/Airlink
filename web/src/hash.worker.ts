import { digestFile } from './hash-stream';
self.onmessage = async (event: MessageEvent<{ file: File; chunkSize: number }>) => {
  try { self.postMessage(await digestFile(event.data.file, event.data.chunkSize, progress => self.postMessage({ progress }))); }
  catch (error) { self.postMessage({ error: error instanceof Error ? error.message : 'Checksum failed' }); }
};
