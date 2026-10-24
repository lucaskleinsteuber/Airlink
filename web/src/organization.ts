import type { RemoteFile, Transfer } from './types';
export interface PrepareResult { decision: 'upload' | 'pending' | 'skip'; transfer: Transfer }
export function duplicateAttention(transfer: Transfer): boolean { return transfer.state === 'checkingDuplicate' || transfer.state === 'waitingDuplicate'; }
export function fileHasPayload(file: RemoteFile): boolean { return !file.directory && !file.skipped; }
export function transferProgressBytes(transfer: Transfer): number { return Math.min(transfer.totalBytes, Math.max(0, transfer.completedBytes) + Math.max(0, transfer.skippedBytes || 0)); }
export function preparationState(transfer: Transfer, fileId: string): 'skip' | 'pending' | 'upload' | 'wait' {
  const file = transfer.files.find(value => value.id === fileId);
  if (!file) throw new Error('Transfer manifest changed');
  if (file.completed || file.skipped) return 'skip';
  if (duplicateAttention(transfer) || file.duplicateState === 'pending') return 'pending';
  return transfer.state === 'running' ? 'upload' : 'wait';
}
export function validatePrepareResult(value: PrepareResult, transferId: string, fileId: string): PrepareResult {
  if (!value || !['upload', 'pending', 'skip'].includes(value.decision) || value.transfer?.id !== transferId || !Array.isArray(value.transfer.files) || !value.transfer.files.some(file => file.id === fileId)) throw new Error('Invalid file preparation response');
  if (value.decision === 'skip' && !value.transfer.files.some(file => file.id === fileId && file.completed && file.skipped)) throw new Error('Skip was not confirmed by the host');
  return value;
}
