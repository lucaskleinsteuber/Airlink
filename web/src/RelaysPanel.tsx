import { post } from './api';
import type { Language } from './i18n';
import { bytes, progressPercent } from './protocol';
import { relayStopped, relayTerminal } from './relay-protocol';
import type { RelayLocal } from './relay-manager';
import { transferText } from './transfer-i18n';
import type { Relay } from './types';
export function RelaysPanel({ relays, currentDeviceId, local, language, refresh, showError, reselect }: { relays: Relay[]; currentDeviceId: string; local: Map<string, RelayLocal>; language: Language; refresh: () => Promise<void>; showError: (error: unknown) => void; reselect: (id: string, folder: boolean) => void }) {
  const t = transferText(language);
  async function action(relay: Relay, name: string) { try { await post(`/relays/${encodeURIComponent(relay.id)}/${name}`); await refresh(); } catch (error) { showError(error); } }
  if (!relays.length) return null;
  return <section className="transfer-section"><div className="section-heading"><h2>{t.relay}</h2><span>{relays.length}</span></div><p className="feature-intro">{t.relayHelp}</p><div className="transfer-list">
    {[...relays].sort((a, b) => b.createdAt.localeCompare(a.createdAt)).map(relay => {
      const sender = relay.senderId === currentDeviceId;
      const recipient = relay.receiverId === currentDeviceId;
      if (!sender && !recipient) return null;
      const stage = local.get(relay.id); const files = relay.files.filter(file => !file.directory);
      const accepted = relay.state !== 'offered'; const downloadable = accepted && !['failed', 'cancelled', 'paused'].includes(relay.state);
      const needSource = sender && !relayStopped(relay.state) && (stage?.phase === 'source' || stage?.phase === 'error');
      return <article className="transfer" key={relay.id}>
        <div className="transfer-top"><span className="relay-symbol" aria-hidden="true">⇄</span><div className="transfer-title"><h3>{files.length === 1 ? files[0].path.split('/').at(-1) : relay.files[0]?.path || t.relay}</h3><p>{sender ? t.to : t.from} {sender ? relay.receiverName : relay.senderName}</p></div><span className={`state ${relay.state}`}>{t[relay.state]}</span></div>
        <progress max={100} value={relay.state === 'delivered' ? 100 : progressPercent(relay.deliveredBytes, relay.totalBytes)} aria-label={t.bytesDelivered} /><div className="transfer-meta"><span>{t.bytesDelivered}: {bytes(relay.deliveredBytes, language)} / {bytes(relay.totalBytes, language)}</span><span>{relay.bytesPerSecond > 0 ? `${bytes(relay.bytesPerSecond, language)}/s` : ''}</span></div>
        {sender && stage?.phase === 'hashing' && <p className="transfer-note">{t.preparing} · {bytes(stage.done || 0, language)} / {bytes(stage.total || 0, language)}</p>}
        {sender && stage?.phase === 'waiting' && !relayTerminal(relay.state) && <p className="transfer-note">{t.waiting}</p>}
        {(relay.error || stage?.error) && <p className="warning">{relay.error || stage?.error}</p>}
        {needSource && <div className="source-needed"><p>{t.sourceNeeded}</p><div className="feature-actions"><button className="secondary small" onClick={() => reselect(relay.id, false)}>{t.chooseSource}</button><button className="text-button" onClick={() => reselect(relay.id, true)}>{t.chooseFolder}</button></div></div>}
        {recipient && relay.state === 'offered' && <div className="feature-actions"><button className="primary" onClick={() => { void action(relay, 'accept'); }}>{t.accept}</button></div>}
        {recipient && accepted && <><div className="download-files">{files.map(file => <div key={file.id}><span title={file.path}>{file.path}</span><span>{bytes(file.size, language)}</span>{downloadable ? <a className="text-button" href={`/api/v1/relays/${encodeURIComponent(relay.id)}/files/${encodeURIComponent(file.id)}/download`} download>{t.download}</a> : <span className="muted">{t[relay.state]}</span>}</div>)}</div>{downloadable && (files.length > 1 || relay.files.some(file => file.directory)) && <><a className="secondary small inline-button" href={`/api/v1/relays/${encodeURIComponent(relay.id)}/archive`} download>{t.archive}</a><p className="transfer-note">{t.archiveHelp}</p></>}</>}
        <p className="transfer-note">{sender ? t.downloadsManaged : t.deliveredHelp}</p>
        {!relayTerminal(relay.state) && <div className="transfer-actions">{['ready', 'running', 'queued', 'reconnecting'].includes(relay.state) && <button className="text-button" onClick={() => { void action(relay, 'pause'); }}>{t.pause}</button>}{relay.state === 'paused' && <button className="text-button" onClick={() => { void action(relay, 'resume'); }}>{t.resume}</button>}<button className="text-button muted" onClick={() => { void action(relay, 'cancel'); }}>{t.cancel}</button></div>}
      </article>;
    })}
  </div></section>;
}
