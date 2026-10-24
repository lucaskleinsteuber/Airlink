export const CHUNK_SIZE = 8 * 1024 * 1024;
export type TransferState = 'offered' | 'queued' | 'running' | 'paused' | 'reconnecting' | 'verifying' | 'completed' | 'failed' | 'cancelled';
export interface RemoteFile { id: string; path: string; size: number; modified?: number; directory?: boolean; sha256?: string; received: number; completed: boolean }
export interface Transfer { id: string; deviceId: string; deviceName: string; direction: 'receive' | 'send'; state: TransferState; files: RemoteFile[]; totalBytes: number; completedBytes: number; bytesPerSecond: number; createdAt: string; updatedAt: string; error?: string }
export interface Device { id: string; name: string; kind: string; paired: boolean; online: boolean; canSend: boolean; canReceive: boolean; canClipboard?: boolean; canRelay?: boolean; autoAccept: boolean }
export interface HostInfo { id: string; name: string; version: string; chunkSize: number; fingerprint: string; urls?: string[] }
export interface Status extends HostInfo { transfers: Transfer[]; devices: Device[]; shares?: Share[]; clipboard?: ClipboardMessage[]; clipboardAllowed?: boolean; currentDeviceId?: string; relayDevices?: Device[]; relays?: Relay[]; statistics?: Statistics }
export interface Selection { path: string; directory: boolean; file?: File }
export interface SavedFile { id: string; path: string; size: number; modified: number; directory: boolean; sha256?: string; chunks?: string[] }
export interface Manifest { id: string; hostId: string; files: SavedFile[]; updatedAt: number }
export interface PendingPair { requestId: string; pollToken: string }
export interface Share { id: string; name: string; url?: string; files: RemoteFile[]; totalBytes: number; createdAt: string; expiresAt?: string; maxDownloads: number; downloads: number; passwordProtected: boolean; revoked: boolean }
export interface ShareCreate { name: string; paths?: string[]; transferId?: string; password?: string; expiresAt?: string; maxDownloads: number }
export interface ClipboardMessage { id: string; deviceId: string; deviceName: string; direction: 'receive' | 'send'; text: string; createdAt: string }
export type RelayState = 'offered' | 'ready' | 'queued' | 'running' | 'paused' | 'reconnecting' | 'delivered' | 'failed' | 'cancelled';
export interface Relay { id: string; senderId: string; senderName: string; receiverId: string; receiverName: string; state: RelayState; files: RemoteFile[]; totalBytes: number; deliveredBytes: number; bytesPerSecond: number; createdAt: string; updatedAt: string; error?: string }
export interface RelayRequest { requestId: string; fileId: string; index: number }
export interface RelayManifest extends Manifest { senderId: string; receiverId: string }
export interface DayStatistics { date: string; sentBytes: number; receivedBytes: number; relayedBytes: number; completedTransfers: number }
export interface Statistics { sentBytes: number; receivedBytes: number; completedTransfers: number; failedTransfers: number; cancelledTransfers: number; activeTransfers: number; relayedBytes: number; relayDeliveries: number; trafficSentBytes?: number; trafficReceivedBytes?: number; days: DayStatistics[] }
export interface History { transfers: Transfer[]; relays: Relay[] }
export interface HistoryFilter { query: string; state: string; direction: string; deviceId: string }
