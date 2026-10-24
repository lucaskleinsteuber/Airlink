import type { RelayManifest } from './types';
let database: Promise<IDBDatabase> | undefined;
function open(): Promise<IDBDatabase> {
  database ??= new Promise((resolve, reject) => {
    const request = indexedDB.open('airlink-relay-sources', 1);
    request.onupgradeneeded = () => request.result.createObjectStore('manifests', { keyPath: 'id' });
    request.onsuccess = () => { request.result.onversionchange = () => { request.result.close(); database = undefined; }; resolve(request.result); };
    request.onerror = () => { database = undefined; reject(request.error); };
  });
  return database;
}
// Only names, hashes and checkpoints are persisted. File objects and bytes stay in the current tab.
export async function saveRelayManifest(manifest: RelayManifest): Promise<void> {
  const db = await open();
  return new Promise((resolve, reject) => { const tx = db.transaction('manifests', 'readwrite'); tx.objectStore('manifests').put(manifest); tx.oncomplete = () => resolve(); tx.onerror = () => reject(tx.error); tx.onabort = () => reject(tx.error); });
}
export async function relayManifests(hostId: string, senderId: string): Promise<RelayManifest[]> {
  const db = await open();
  return new Promise((resolve, reject) => { const request = db.transaction('manifests').objectStore('manifests').getAll(); request.onsuccess = () => resolve((request.result as RelayManifest[]).filter(value => value.hostId === hostId && value.senderId === senderId)); request.onerror = () => reject(request.error); });
}
