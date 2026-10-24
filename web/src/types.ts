export const CHUNK_SIZE = 8 * 1024 * 1024;
export type TransferState = 'offered' | 'queued' | 'running' | 'paused' | 'reconnecting' | 'verifying' | 'completed' | 'failed' | 'cancelled';
export interface RemoteFile { id: string; path: string; size: number; modified?: number; directory?: boolean; sha256?: string; received: number; completed: boolean }
export interface Transfer { id: string; deviceId: string; deviceName: string; direction: 'receive' | 'send'; state: TransferState; files: RemoteFile[]; totalBytes: number; completedBytes: number; bytesPerSecond: number; createdAt: string; updatedAt: string; error?: string }
export interface Device { id: string; name: string; kind: string; paired: boolean; online: boolean; canSend: boolean; canReceive: boolean; autoAccept: boolean }
export interface HostInfo { id: string; name: string; version: string; chunkSize: number; fingerprint: string; urls?: string[] }
export interface Status extends HostInfo { transfers: Transfer[]; devices: Device[] }
export interface Selection { path: string; directory: boolean; file?: File }
export interface SavedFile { id: string; path: string; size: number; modified: number; directory: boolean; sha256?: string; chunks?: string[] }
export interface Manifest { id: string; hostId: string; files: SavedFile[]; updatedAt: number }
export interface PendingPair { requestId: string; pollToken: string }
