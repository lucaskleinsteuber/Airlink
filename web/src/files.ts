import { safePath, validateSelection } from './protocol';
import type { Selection } from './types';
interface Entry { name: string; isFile: boolean; isDirectory: boolean; file(success: (file: File) => void, failure: (error: DOMException) => void): void; createReader(): { readEntries(success: (entries: Entry[]) => void, failure: (error: DOMException) => void): void } }
export function fromFiles(files: FileList | File[]): Selection[] {
  const values = Array.from(files).map(file => ({ path: safePath(file.webkitRelativePath || file.name), file, directory: false }));
  validateSelection(values); return values;
}
async function walk(entry: Entry, prefix = ''): Promise<Selection[]> {
  const path = safePath(prefix + entry.name);
  if (entry.isFile) return [{ path, file: await new Promise<File>((resolve, reject) => entry.file(resolve, reject)), directory: false }];
  if (!entry.isDirectory) return [];
  const reader = entry.createReader(); const result: Selection[] = [{ path, directory: true }];
  while (true) {
    const entries = await new Promise<Entry[]>((resolve, reject) => reader.readEntries(resolve, reject));
    if (!entries.length) break;
    for (const child of entries) result.push(...await walk(child, `${path}/`));
  }
  return result;
}
export async function fromDrop(data: DataTransfer): Promise<Selection[]> {
  const entries = Array.from(data.items).filter(item => item.kind === 'file').map(item => item.webkitGetAsEntry?.() as Entry | null);
  const values = entries.length && entries.every(Boolean) ? (await Promise.all(entries.map(entry => walk(entry!)))).flat() : fromFiles(data.files);
  validateSelection(values); return values;
}
