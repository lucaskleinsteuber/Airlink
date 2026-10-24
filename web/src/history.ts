import type { History, HistoryFilter, Relay, Transfer } from './types';
export function historyQuery(filter: HistoryFilter): string {
  const query = new URLSearchParams({ limit: '100' });
  for (const [key, value] of Object.entries(filter)) if (value) query.set(key, value);
  return query.toString();
}
export function ownsHistory(entry: Transfer | Relay, deviceId: string): boolean { return 'senderId' in entry ? entry.senderId === deviceId || entry.receiverId === deviceId : entry.deviceId === deviceId; }
export function historyRows(history: History, deviceId: string): Array<{ kind: 'transfer'; value: Transfer } | { kind: 'relay'; value: Relay }> {
  return [
    ...history.transfers.filter(value => ownsHistory(value, deviceId)).map(value => ({ kind: 'transfer' as const, value })),
    ...history.relays.filter(value => ownsHistory(value, deviceId)).map(value => ({ kind: 'relay' as const, value })),
  ].sort((a, b) => b.value.createdAt.localeCompare(a.value.createdAt));
}
