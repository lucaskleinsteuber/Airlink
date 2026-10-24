export function hashFile(file: File, chunkSize: number, progress: (done: number) => void, signal: AbortSignal): Promise<{ sha256: string; chunks: string[] }> {
  return new Promise((resolve, reject) => {
    const worker = new Worker(new URL('./hash.worker.ts', import.meta.url), { type: 'module' });
    const cleanup = () => { worker.terminate(); signal.removeEventListener('abort', abort); };
    const abort = () => { cleanup(); reject(new DOMException('Aborted', 'AbortError')); };
    signal.addEventListener('abort', abort, { once: true });
    if (signal.aborted) { abort(); return; }
    worker.onerror = () => { cleanup(); reject(new Error('Checksum worker failed')); };
    worker.onmessage = (e: MessageEvent<{ progress?: number; sha256?: string; chunks?: string[]; error?: string }>) => {
      if (e.data.error) { cleanup(); reject(new Error(e.data.error)); }
      else if (e.data.sha256) { cleanup(); resolve({ sha256: e.data.sha256, chunks: e.data.chunks! }); }
      else if (e.data.progress !== undefined) progress(e.data.progress);
    };
    worker.postMessage({ file, chunkSize });
  });
}
