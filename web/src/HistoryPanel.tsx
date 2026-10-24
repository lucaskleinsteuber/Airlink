import { useEffect, useState } from 'react';
import { api } from './api';
import { historyQuery, historyRows } from './history';
import type { Language } from './i18n';
import { messages } from './i18n';
import { bytes } from './protocol';
import { transferProgressBytes } from './organization';
import { organizationText } from './organization-i18n';
import { transferText } from './transfer-i18n';
import type { History, HistoryFilter, Relay, Transfer } from './types';
export function HistoryPanel({ visible, currentDeviceId, transfers, relays, language, showError, notice, refresh }: { visible: boolean; currentDeviceId: string; transfers: Transfer[]; relays: Relay[]; language: Language; showError: (error: unknown) => void; notice: (text: string) => void; refresh: () => Promise<void> }) {
  const t = transferText(language); const ot = organizationText(language); const common = messages[language];
  const [filter, setFilter] = useState<HistoryFilter>({ query: '', state: '', direction: '', deviceId: '' });
  const [history, setHistory] = useState<History>({ transfers: [], relays: [] });
  const [revision, setRevision] = useState(0);
  const [clearing, setClearing] = useState(false);
  const query = historyQuery(filter);
  useEffect(() => {
    if (!visible) return;
    const controller = new AbortController(); let timer: ReturnType<typeof setTimeout>;
    const load = async () => {
      try { const value = await api<History>(`/history?${query}`, { signal: controller.signal }); if (!controller.signal.aborted) setHistory({ transfers: value.transfers || [], relays: value.relays || [] }); }
      catch (error) { if (!controller.signal.aborted) showError(error); }
      if (!controller.signal.aborted) timer = setTimeout(() => { void load(); }, 5000);
    };
    timer = setTimeout(() => { void load(); }, 250);
    return () => { controller.abort(); clearTimeout(timer); };
  }, [visible, query, revision, showError]);
  async function clear() {
    setClearing(true);
    try { await api('/history', { method: 'DELETE' }); setRevision(value => value + 1); await refresh(); notice(t.cleared); } catch (error) { showError(error); } finally { setClearing(false); }
  }
  const devices = new Map<string, string>();
  for (const transfer of transfers) devices.set(transfer.deviceId, transfer.deviceName);
  for (const relay of relays) { devices.set(relay.senderId, relay.senderName); devices.set(relay.receiverId, relay.receiverName); }
  const change = (key: keyof HistoryFilter, value: string) => setFilter(current => ({ ...current, [key]: value }));
  const rows = historyRows(history, currentDeviceId);
  return <section className="feature-section"><div className="section-heading"><h2>{t.history}</h2><button className="secondary small" disabled={clearing} onClick={() => { void clear(); }}>{t.clearHistory}</button></div><p className="feature-intro">{t.clearHelp}</p><div className="history-filters">
    <input type="search" aria-label={t.search} placeholder={t.search} value={filter.query} onChange={event => change('query', event.target.value)} />
    <select aria-label={t.allStates} value={filter.state} onChange={event => change('state', event.target.value)}><option value="">{t.allStates}</option>{['offered', 'ready', 'queued', 'running', 'paused', 'reconnecting', 'verifying', 'checkingDuplicate', 'waitingDuplicate', 'completed', 'delivered', 'failed', 'cancelled'].map(state => <option key={state} value={state}>{state in t ? t[state as keyof typeof t] : common[state as 'verifying' | 'completed']}</option>)}</select>
    <select aria-label={t.allDirections} value={filter.direction} onChange={event => change('direction', event.target.value)}><option value="">{t.allDirections}</option><option value="send">{t.hostSent}</option><option value="receive">{t.hostReceived}</option><option value="relay">{t.relayDirection}</option></select>
    <select aria-label={t.allDevices} value={filter.deviceId} onChange={event => change('deviceId', event.target.value)}><option value="">{t.allDevices}</option>{[...devices].map(([id, name]) => <option key={id} value={id}>{name}</option>)}</select>
  </div><div className="history-list">{!rows.length && <p className="empty-copy">{t.noHistory}</p>}{rows.map(row => {
    const value = row.value; const relay = row.kind === 'relay' ? row.value : undefined;
    const transfer = row.kind === 'transfer' ? row.value : undefined;
    const label = relay ? `${relay.senderName} → ${relay.receiverName}` : transfer!.deviceName;
    const state = value.state;
    return <article className="history-row" key={`${row.kind}-${value.id}`}><div className="section-heading"><h3>{value.files.find(file => !file.directory)?.path || value.files[0]?.path || label}</h3><span className="share-state">{state in t ? t[state as keyof typeof t] : common[state as 'verifying' | 'completed'] || state}</span></div><p>{label} · {relay ? t.relayDirection : transfer?.direction === 'receive' ? t.hostReceived : t.hostSent}</p><div className="transfer-meta"><time dateTime={value.createdAt}>{new Date(value.createdAt).toLocaleString(language)}</time><span>{bytes(relay?.deliveredBytes ?? (transfer ? transferProgressBytes(transfer) : 0), language)} / {bytes(value.totalBytes, language)}</span></div>{!!transfer?.skippedBytes && <p className="history-explanation">{ot.transferred}: {bytes(transfer.completedBytes, language)} · {ot.skippedBytes}: {bytes(transfer.skippedBytes, language)}</p>}{transfer?.files.filter(file => file.skipped).map(file => <p key={file.id} className="history-explanation">{file.path} · {ot.skipped}</p>)}{relay && <p className="history-explanation">{t.deliveredHelp}</p>}</article>;
  })}</div></section>;
}
