import { useCallback, useEffect, useRef, useState } from 'react';
import type { ChangeEvent, DragEvent, ReactNode } from 'react';
import { api, APIError, post, transferPath } from './api';
import { fromDrop, fromFiles } from './files';
import { bytes, progressPercent, SelectionError, terminal } from './protocol';
import { clearPendingPair, loadPendingPair, pollPair, requestPair } from './pairing';
import { manifestsForHost } from './storage';
import { messages, systemLanguage } from './i18n';
import type { Language } from './i18n';
import { UploadManager } from './uploader';
import { SharesPanel } from './SharesPanel';
import { ClipboardPanel } from './ClipboardPanel';
import { featureText } from './features-i18n';
import { RelayManager } from './relay-manager';
import type { RelayLocal, RelayPreparing } from './relay-manager';
import { relayManifests } from './relay-storage';
import { RelaysPanel } from './RelaysPanel';
import { HistoryPanel } from './HistoryPanel';
import { StatisticsPanel } from './StatisticsPanel';
import { transferText } from './transfer-i18n';
import { duplicateAttention, fileHasPayload, transferProgressBytes } from './organization';
import { organizationText } from './organization-i18n';
import type { LocalUpload } from './uploader';
import type { HostInfo, PendingPair, Selection, Status, Transfer } from './types';
function preference(key: string, fallback: string): string { try { return localStorage.getItem(key) || fallback; } catch { return fallback; } }
function Glyph({ name, size = 22 }: { name: 'link' | 'upload' | 'download' | 'device' | 'check' | 'file' | 'shield'; size?: number }) {
  const paths: Record<string, ReactNode> = {
    link: <><path d="M10 13a5 5 0 0 0 7 .2l3-3a5 5 0 0 0-7-7l-2 2" /><path d="M14 11a5 5 0 0 0-7-.2l-3 3a5 5 0 0 0 7 7l2-2" /></>,
    upload: <><path d="M12 16V3m-5 5 5-5 5 5M4 15v5a1 1 0 0 0 1 1h14a1 1 0 0 0 1-1v-5" /></>,
    download: <><path d="M12 3v13m-5-5 5 5 5-5M4 16v4a1 1 0 0 0 1 1h14a1 1 0 0 0 1-1v-4" /></>,
    device: <><rect x="3" y="3" width="18" height="13" rx="2" /><path d="M12 16v5m-5 0h10" /></>,
    check: <path d="m5 12 4 4L19 6" />,
    file: <><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" /><path d="M14 2v6h6" /></>,
    shield: <><path d="m12 2 8 4v6c0 5-8 10-8 10S4 17 4 12V6z" /><path d="m8 11 3 3 5-6" /></>,
  };
  return <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.65" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">{paths[name]}</svg>;
}
export function App() {
  const [language, setLanguage] = useState<Language>(() => preference('airlink-language', 'system') === 'system' ? systemLanguage() : preference('airlink-language', 'de') as Language);
  const [languageSetting, setLanguageSetting] = useState(() => preference('airlink-language', 'system'));
  const [appearance, setAppearance] = useState(() => preference('airlink-appearance', 'system'));
  const t = messages[language] || messages.de;
  const ft = featureText(language);
  const rt = transferText(language);
  const ot = organizationText(language);
  const [section, setSection] = useState<'transfers' | 'shares' | 'clipboard' | 'history' | 'statistics'>('transfers');
  const [host, setHost] = useState<HostInfo>();
  const [status, setStatus] = useState<Status>();
  const statusRef = useRef<Status | undefined>(undefined); statusRef.current = status;
  const [authenticated, setAuthenticated] = useState(false);
  const [online, setOnline] = useState(true);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [pending, setPending] = useState<PendingPair | null>(loadPendingPair);
  const [busy, setBusy] = useState(false);
  const [name, setName] = useState(() => /iPhone|iPad/.test(navigator.userAgent) ? 'iPhone / iPad' : /Android/.test(navigator.userAgent) ? 'Android' : /Windows/.test(navigator.userAgent) ? 'Windows' : /Mac/.test(navigator.userAgent) ? 'Mac' : 'Browser');
  const [dragging, setDragging] = useState(false);
  const [local, setLocal] = useState(new Map<string, LocalUpload>());
  const manager = useRef<UploadManager | undefined>(undefined);
  const relayManager = useRef<RelayManager | undefined>(undefined);
  const [relayLocal, setRelayLocal] = useState(new Map<string, RelayLocal>());
  const [preparingRelay, setPreparingRelay] = useState<RelayPreparing>();
  const [targetId, setTargetId] = useState('host');
  const [relayReselect, setRelayReselect] = useState<string | null>(null);
  const filesInput = useRef<HTMLInputElement>(null);
  const folderInput = useRef<HTMLInputElement>(null);
  const [reselect, setReselect] = useState<string | null>(null);
  const tRef = useRef(t); tRef.current = t;
  const unauthorized = useCallback(() => { setAuthenticated(false); setStatus(undefined); setError(tRef.current.lostSession); }, []);
  const showError = useCallback((failure: unknown) => {
    if (failure instanceof APIError && failure.status === 401) unauthorized();
    else if (failure instanceof SelectionError) setError(tRef.current[failure.reason].replace('{path}', () => failure.path || ''));
    else setError(failure instanceof Error ? failure.message : tRef.current.error);
  }, [unauthorized]);
  const refresh = useCallback(async () => {
    try { const value = await api<Status>('/status'); setStatus(value); setHost(value); setAuthenticated(true); setOnline(true); }
    catch (failure) { if (failure instanceof APIError && failure.status === 401) { setAuthenticated(false); setStatus(undefined); } else { setOnline(false); } }
  }, []);
  useEffect(() => {
    document.documentElement.lang = language;
    document.documentElement.dataset.appearance = appearance;
    try { localStorage.setItem('airlink-language', languageSetting); localStorage.setItem('airlink-appearance', appearance); } catch { /* Preferences are optional. */ }
  }, [language, languageSetting, appearance]);
  useEffect(() => { void api<HostInfo>('/info').then(setHost).catch(() => setOnline(false)); void refresh(); }, [refresh]);
  useEffect(() => {
    if (!authenticated || !host?.id) return;
    const upload = new UploadManager(setLocal, unauthorized); manager.current = upload;
    void manifestsForHost(host.id).then(values => { upload.restore(values); if (statusRef.current) upload.update(statusRef.current); }).catch(() => setError(tRef.current.storageError));
    return () => { upload.stop(); manager.current = undefined; };
  }, [authenticated, host?.id, unauthorized]);
  useEffect(() => {
    if (!authenticated || !host?.id || !status?.currentDeviceId) return;
    const relay = new RelayManager(setRelayLocal, setPreparingRelay, unauthorized); relayManager.current = relay;
    void relayManifests(host.id, status.currentDeviceId).then(values => { relay.restore(values); if (statusRef.current) relay.update(statusRef.current); }).catch(() => setError(tRef.current.storageError));
    return () => { relay.stop(); relayManager.current = undefined; };
  }, [authenticated, host?.id, status?.currentDeviceId, unauthorized]);
  useEffect(() => { if (status) { manager.current?.update(status); relayManager.current?.update(status); } }, [status]);
  useEffect(() => {
    if (!authenticated) return;
    const events = new EventSource('/api/v1/events');
    events.addEventListener('status', event => {
      try { const value = JSON.parse((event as MessageEvent<string>).data) as Status; setStatus(value); setOnline(true); } catch { /* Retry via polling if an event is malformed. */ }
    });
    events.onerror = () => setOnline(false);
    const timer = setInterval(() => { void refresh(); }, 4000);
    const visible = () => { if (document.visibilityState === 'visible') void refresh(); };
    document.addEventListener('visibilitychange', visible);
    return () => { events.close(); clearInterval(timer); document.removeEventListener('visibilitychange', visible); };
  }, [authenticated, refresh]);
  useEffect(() => {
    if (!pending) return;
    let stopped = false; let timer: ReturnType<typeof setTimeout>;
    const poll = async () => {
      try {
        const result = await pollPair(pending);
        if (stopped) return;
        setOnline(true);
        if (result.state === 'approved') { setPending(null); await refresh(); return; }
        if (result.state === 'denied') { setPending(null); setError(tRef.current.denied); return; }
      } catch (failure) {
        if (failure instanceof APIError && failure.status < 500 && failure.status !== 429) { clearPendingPair(); setPending(null); showError(failure); return; }
        setOnline(false);
      }
      if (!stopped) timer = setTimeout(() => { void poll(); }, 1500);
    };
    void poll(); return () => { stopped = true; clearTimeout(timer); };
  }, [pending, refresh, showError]);
  async function connect(event: React.FormEvent) {
    event.preventDefault(); if (!name.trim() || busy) return;
    setBusy(true); setError('');
    try { setPending(await requestPair(name)); }
    catch (failure) { showError(failure); } finally { setBusy(false); }
  }
  async function selected(values: Selection[], existingId: string | null = reselect, existingRelay: string | null = relayReselect) {
    if (!host || !manager.current) return;
    setError('');
    try {
      if (existingRelay) { if (!relayManager.current) throw new Error(rt.permission); await relayManager.current.attach(existingRelay, values); setRelayReselect(null); }
      else if (existingId) { await manager.current.attach(existingId, values); setReselect(null); }
      else if (targetId !== 'host') { if (!relayManager.current) throw new Error(rt.permission); await relayManager.current.create(targetId, values); }
      else await manager.current.create(values, host.id);
      await refresh();
    } catch (failure) { if (!(failure instanceof DOMException && failure.name === 'AbortError')) showError(failure); }
  }
  function inputChange(event: ChangeEvent<HTMLInputElement>) {
    try { if (event.target.files?.length) void selected(fromFiles(event.target.files)); } catch (failure) { showError(failure); }
    event.target.value = '';
  }
  async function drop(event: DragEvent) {
    event.preventDefault(); setDragging(false); setReselect(null);
    if (!authenticated) return;
    try { await selected(await fromDrop(event.dataTransfer), null, null); } catch (failure) { showError(failure); }
  }
  async function action(id: string, name: string) {
    try { await post(`${transferPath(id)}/${name}`); await refresh(); } catch (failure) { showError(failure); }
  }
  async function download(transfer: Transfer, fileId?: string) {
    const suffix = fileId ? `/files/${encodeURIComponent(fileId)}/download` : '/archive';
    const link = document.createElement('a'); link.href = `/api/v1${transferPath(transfer.id)}${suffix}`; link.download = ''; document.body.append(link); link.click(); link.remove();
    setNotice(t.downloadStarted);
    try { await post(`${transferPath(transfer.id)}/downloaded`); } catch (failure) { showError(failure); }
  }
  const transfers = [...(status?.transfers || [])].sort((a, b) => b.createdAt.localeCompare(a.createdAt));
  const secure = window.isSecureContext;
  return <div className="shell">
    <header className="masthead">
      <a className="brand" href="/" aria-label="AirLink"><span className="brand-mark"><Glyph name="link" size={23} /></span><span>AirLink</span></a>
      <div className="preferences">
        <label><span className="sr-only">{t.language}</span><select value={languageSetting} onChange={e => { setLanguageSetting(e.target.value); setLanguage(e.target.value === 'system' ? systemLanguage() : e.target.value as Language); }}><option value="system">{t.system}</option><option value="de">Deutsch</option><option value="en">English</option></select></label>
        <label><span className="sr-only">{t.appearance}</span><select value={appearance} onChange={e => setAppearance(e.target.value)}><option value="system">◐ {t.system}</option><option value="light">☼ {t.light}</option><option value="dark">☾ {t.dark}</option></select></label>
      </div>
    </header>
    <main>
      {(error || notice) && <div className={`banner ${error ? 'error' : ''}`} role={error ? 'alert' : 'status'}><span>{error || notice}</span><button className="icon-button" aria-label={t.dismiss} onClick={() => { setError(''); setNotice(''); }}>×</button></div>}
      {!authenticated ? <section className="welcome">
        <div className="hero-symbol"><Glyph name="link" size={42} /></div>
        <h1>{t.tagline}</h1><p className="intro">{t.subtitle}</p>
        {host && <div className="host-pill"><Glyph name="device" size={18} />{host.name}</div>}
        {!secure && <p className="warning" role="alert">{t.secureOnly}</p>}
        {pending ? <div className="pair-pending" role="status"><span className="spinner" /><h2>{t.pending}</h2><p>{t.pendingHelp}</p><button className="text-button" onClick={() => { clearPendingPair(); setPending(null); }}>{t.leavePending}</button></div>
          : <form className="pair-form" onSubmit={connect}><label htmlFor="device-name">{t.deviceName}</label><input id="device-name" value={name} maxLength={80} autoComplete="off" onChange={e => setName(e.target.value)} /><button className="primary" disabled={!name.trim() || busy || !secure}>{busy ? t.connecting : t.connect}</button></form>}
        {!online && <p className="warning">{t.offline} <button className="text-button" onClick={() => { void refresh(); }}>{t.refresh}</button></p>}
      </section> : <>
        <section className="connection"><div className="connection-icon"><Glyph name="device" size={28} /></div><div><h1>{host?.name}</h1><p><span className={`status-dot ${online ? '' : 'offline'}`} />{online ? t.paired : t.offline}<span className="connection-detail">{online ? t.onlineHelp : t.offlineHelp}</span></p></div><Glyph name="shield" /></section>
        <nav className="feature-tabs" aria-label="AirLink"><button aria-pressed={section === 'transfers'} onClick={() => setSection('transfers')}>{ft.transfers}</button><button aria-pressed={section === 'shares'} onClick={() => setSection('shares')}>{ft.shares}</button><button aria-pressed={section === 'clipboard'} onClick={() => setSection('clipboard')}>{ft.clipboard}</button><button aria-pressed={section === 'history'} onClick={() => setSection('history')}>{rt.history}</button><button aria-pressed={section === 'statistics'} onClick={() => setSection('statistics')}>{rt.statistics}</button></nav>
        <div hidden={section !== 'transfers'}>
        <section className={`dropzone ${dragging ? 'dragging' : ''}`} aria-label={t.send} onDragOver={event => { event.preventDefault(); setDragging(true); }} onDragLeave={event => { if (!event.currentTarget.contains(event.relatedTarget as Node)) setDragging(false); }} onDrop={event => { void drop(event); }}>
          <label className="recipient-picker">{rt.recipient}<select value={targetId} disabled={!!preparingRelay} onChange={event => setTargetId(event.target.value)}><option value="host">{rt.host} · {host?.name}</option>{(status?.relayDevices || []).map(device => <option key={device.id} value={device.id}>{device.name}</option>)}{targetId !== 'host' && !status?.relayDevices?.some(device => device.id === targetId) && <option value={targetId} disabled>{rt.noPeers}</option>}</select></label>
          <div className="drop-symbol"><Glyph name="upload" size={30} /></div><h2>{t.drop}</h2><p>{targetId === 'host' ? t.dropHelp : rt.relayHelp}</p><div className="button-row"><button className="primary" disabled={!!preparingRelay} onClick={() => { setReselect(null); setRelayReselect(null); filesInput.current?.click(); }}>{t.chooseFiles}</button><button className="secondary" disabled={!!preparingRelay} onClick={() => { setReselect(null); setRelayReselect(null); folderInput.current?.click(); }}>{t.chooseFolder}</button></div>
        </section>
        <p className="relay-permission-note">{rt.permission}</p>
        {preparingRelay && <div className="relay-preparation" role="status"><p>{rt.preparing}: {preparingRelay.path}</p><progress max={preparingRelay.total || 1} value={preparingRelay.done} /><div className="transfer-meta"><span>{bytes(preparingRelay.done, language)} / {bytes(preparingRelay.total, language)}</span><button className="text-button" onClick={() => relayManager.current?.cancelPreparation()}>{rt.cancelPreparing}</button></div></div>}
        <RelaysPanel relays={status?.relays || []} currentDeviceId={status?.currentDeviceId || ''} local={relayLocal} language={language} refresh={refresh} showError={showError} reselect={(id, folder) => { setReselect(null); setRelayReselect(id); if (folder) folderInput.current?.click(); else filesInput.current?.click(); }} />
        <section className="transfer-section"><div className="section-heading"><h2>{t.transfers}</h2><span>{transfers.length || ''}</span></div>
          {!transfers.length ? <div className="empty"><Glyph name="file" size={29} /><h3>{t.empty}</h3><p>{t.emptyHelp}</p></div> : <div className="transfer-list">{transfers.map(transfer => {
            const upload = transfer.direction === 'receive'; const stage = local.get(transfer.id); const files = transfer.files.filter(file => !file.directory); const downloadableFiles = files.filter(fileHasPayload);
            const active = !terminal(transfer.state); const needsSource = upload && active && (stage?.phase === 'source' || stage?.phase === 'error');
            const percentage = transfer.state === 'completed' ? 100 : progressPercent(transferProgressBytes(transfer), transfer.totalBytes);
            return <article className="transfer" key={transfer.id}>
              <div className="transfer-top"><span className={`transfer-icon ${!upload ? 'incoming' : ''}`}><Glyph name={upload ? 'upload' : 'download'} /></span><div className="transfer-title"><h3>{files.length === 1 ? files[0].path.split('/').at(-1) : `${files.length} ${t.files}`}</h3><p>{upload ? t.toHost : t.fromHost} {host?.name}</p></div><span className={`state ${transfer.state}`}>{!upload ? t.browserDownload : t[transfer.state] || transfer.state}</span></div>
              {upload && <><progress aria-label={`${percentage.toFixed(0)}%`} max={100} value={percentage} /><div className="transfer-meta"><span>{bytes(transfer.completedBytes, language)} / {bytes(transfer.totalBytes, language)}</span><span>{transfer.bytesPerSecond > 0 && transfer.state === 'running' ? `${bytes(transfer.bytesPerSecond, language)}/s` : `${Math.floor(percentage)}%`}</span></div></>}
              {!!transfer.skippedBytes && <p className="transfer-note">{ot.transferred}: {bytes(transfer.completedBytes, language)} · {ot.skippedBytes}: {bytes(transfer.skippedBytes, language)}</p>}
              {files.filter(file => file.skipped).map(file => <p className="transfer-note skipped-file" key={file.id}>{file.path} · {ot.skipped}</p>)}
              {upload && (duplicateAttention(transfer) || stage?.phase === 'waitingDuplicate') && <div className="source-needed"><p>{ot.attention}</p></div>}
              {stage?.phase === 'hashing' && active && <p className="transfer-note">{t.hashing} · {bytes(stage.done || 0, language)} / {bytes(stage.total || 0, language)}</p>}
              {stage?.phase === 'reconnecting' && active && <p className="transfer-note">{t.reconnecting} · {t.offlineHelp}</p>}
              {(transfer.error || stage?.phase === 'error') && <p className={transfer.state === 'failed' || stage?.phase === 'error' ? 'warning' : 'transfer-note'}>{transfer.error || stage?.error}</p>}
              {needsSource && <div className="source-needed"><p>{t.sourceHelp}</p><div className="button-row"><button className="secondary small" onClick={() => { setRelayReselect(null); setReselect(transfer.id); filesInput.current?.click(); }}>{t.retrySource}</button><button className="text-button" onClick={() => { setRelayReselect(null); setReselect(transfer.id); folderInput.current?.click(); }}>{t.chooseFolder}</button></div></div>}
              {!upload && <><p className="transfer-note">{t.downloadHelp}</p><div className="download-files">{downloadableFiles.map(file => <div key={file.id}><span title={file.path}>{file.path}</span><span>{bytes(file.size, language)}</span><button className="text-button" disabled={['cancelled', 'failed'].includes(transfer.state)} onClick={() => { void download(transfer, file.id); }}>{t.download}</button></div>)}</div>{(downloadableFiles.length > 1 || transfer.files.some(file => file.directory)) && <><button className="secondary small" disabled={['cancelled', 'failed'].includes(transfer.state)} onClick={() => { void download(transfer); }}>{t.downloadAll}</button><p className="transfer-note">{t.archiveHelp}</p></>}</>}
              {upload && active && <div className="transfer-actions">{['running', 'queued', 'reconnecting', 'checkingDuplicate', 'waitingDuplicate'].includes(transfer.state) && <button className="text-button" onClick={() => { void action(transfer.id, 'pause'); }}>{t.pause}</button>}{transfer.state === 'paused' && <button className="text-button" onClick={() => { void action(transfer.id, 'resume'); }}>{t.resume}</button>}<button className="text-button muted" onClick={() => { void action(transfer.id, 'cancel'); }}>{t.cancel}</button></div>}
            </article>;
          })}</div>}
        </section>
        <p className="mobile-note">{t.mobile}</p>
        </div>
        <div hidden={section !== 'history'}><HistoryPanel visible={section === 'history'} currentDeviceId={status?.currentDeviceId || ''} transfers={transfers} relays={status?.relays || []} language={language} refresh={refresh} showError={showError} notice={setNotice} /></div>
        <div hidden={section !== 'statistics'}><StatisticsPanel statistics={status?.statistics} language={language} /></div>
        <div hidden={section !== 'shares'}><SharesPanel shares={status?.shares || []} transfers={transfers} language={language} refresh={refresh} showError={showError} notice={setNotice} /></div>
        <div hidden={section !== 'clipboard'}><ClipboardPanel allowed={status?.clipboardAllowed || false} messages={status?.clipboard || []} hostName={host?.name || 'Host'} language={language} refresh={refresh} showError={showError} notice={setNotice} /></div>
      </>}
      <details className="trust-details"><summary><Glyph name="shield" size={17} />{t.trust}</summary><div className="trust-content"><p>{t.trustIntro}</p><p>{t.trustCompare}</p>{host?.fingerprint && <div className="fingerprint"><span>{t.fingerprint}</span><code>{host.fingerprint}</code></div>}<a className="secondary small inline-button" href="/certificate.cer" download>{t.certificate}</a><a className="secondary small inline-button" href="/certificate.mobileconfig">{t.certificateIOS}</a><p>{t.trustSafety}</p><p>{t.trustDesktop}</p><p>{t.trustIOS}</p><p>{t.trustAndroid}</p><p>{t.awaitingHost}</p></div></details>
      <input ref={filesInput} className="hidden-input" type="file" multiple onChange={inputChange} aria-label={t.chooseFiles} />
      <input ref={folderInput} className="hidden-input" type="file" multiple {...{ webkitdirectory: '' }} onChange={inputChange} aria-label={t.chooseFolder} />
    </main>
    <footer><span>{t.privacy}</span><span>AirLink {host?.version || '0.5.0'}</span></footer>
  </div>;
}
