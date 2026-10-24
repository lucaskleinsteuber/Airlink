import type { Manifest } from './types';
let database: Promise<IDBDatabase> | undefined;
function open(): Promise<IDBDatabase> {
  database ??= new Promise((resolve, reject) => {
    const request = indexedDB.open('airlink-browser', 1);
    request.onupgradeneeded = () => request.result.createObjectStore('manifests', { keyPath: 'id' });
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => { database = undefined; reject(request.error); };
  });
  return database;
}
export async function saveManifest(value: Manifest): Promise<void> {
  const db = await open();
  return new Promise((resolve, reject) => {
    const tx = db.transaction('manifests', 'readwrite');
    tx.objectStore('manifests').put(value);
    tx.oncomplete = () => resolve(); tx.onerror = () => reject(tx.error); tx.onabort = () => reject(tx.error);
  });
}
export async function manifestsForHost(hostId: string): Promise<Manifest[]> {
  const db = await open();
  return new Promise((resolve, reject) => {
    const request = db.transaction('manifests').objectStore('manifests').getAll();
    request.onsuccess = () => resolve((request.result as Manifest[]).filter(x => x.hostId === hostId));
    request.onerror = () => reject(request.error);
  });
}
