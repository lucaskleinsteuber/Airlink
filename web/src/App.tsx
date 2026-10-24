import { useCallback, useEffect, useRef, useState } from 'react';
import type { ChangeEvent, DragEvent, ReactNode } from 'react';
import { api, APIError, post, transferPath } from './api';
import { fromDrop, fromFiles } from './files';
import { bytes, progressPercent, terminal } from './protocol';
import { manifestsForHost } from './storage';
import { messages, systemLanguage } from './i18n';
import type { Language } from './i18n';
import { UploadManager } from './uploader';
import type { LocalUpload } from './uploader';
import type { HostInfo, PendingPair, Selection, Status, Transfer } from './types';
function preference(key: string, fallback: string): string { try { return localStorage.getItem(key) || fallback; } catch { return fallback; } }
function pendingPair(): PendingPair | null { try { const value = JSON.parse(sessionStorage.getItem('airlink-pair') || 'null') as PendingPair | null; return value?.requestId && value.pollToken ? value : null; } catch { return null; } }
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
  const [host, setHost] = useState<HostInfo>();
  const [status, setStatus] = useState<Status>();
  const statusRef = useRef<Status | undefined>(undefined); statusRef.current = status;
  const [authenticated, setAuthenticated] = useState(false);
  const [online, setOnline] = useState(true);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [pending, setPending] = useState<PendingPair | null>(pendingPair);
  const [busy, setBusy] = useState(false);
  const [name, setName] = useState(() => /iPhone|iPad/.test(navigator.userAgent) ? 'iPhone / iPad' : /Android/.test(navigator.userAgent) ? 'Android' : /Windows/.test(navigator.userAgent) ? 'Windows' : /Mac/.test(navigator.userAgent) ? 'Mac' : 'Browser');
  const [dragging, setDragging] = useState(false);
  const [local, setLocal] = useState(new Map<string, LocalUpload>());
  const manager = useRef<UploadManager | undefined>(undefined);
  const filesInput = useRef<HTMLInputElement>(null);
  const folderInput = useRef<HTMLInputElement>(null);
  const [reselect, setReselect] = useState<string | null>(null);
  const tRef = useRef(t); tRef.current = t;
  const unauthorized = useCallback(() => { setAuthenticated(false); setStatus(undefined); setError(tRef.current.lostSession); }, []);
  const showError = useCallback((failure: unknown) => { if (failure instanceof APIError && failure.status === 401) unauthorized(); else setError(failure instanceof Error ? failure.message : tRef.current.error); }, [unauthorized]);
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
  useEffect(() => { if (status) manager.current?.update(status); }, [status]);
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
        const result = await post<{ state: string }>('/pair/poll', pending);
        if (stopped) return;
        setOnline(true);
        if (result.state === 'approved') { sessionStorage.removeItem('airlink-pair'); setPending(null); await refresh(); return; }
        if (result.state === 'denied') { sessionStorage.removeItem('airlink-pair'); setPending(null); setError(tRef.current.denied); return; }
      } catch (failure) {
        if (failure instanceof APIError && failure.status < 500 && failure.status !== 429) { sessionStorage.removeItem('airlink-pair'); setPending(null); showError(failure); return; }
        setOnline(false);
      }
      if (!stopped) timer = setTimeout(() => { void poll(); }, 1500);
    };
    void poll(); return () => { stopped = true; clearTimeout(timer); };
  }, [pending, refresh, showError]);
  async function connect(event: React.FormEvent) {
    event.preventDefault(); if (!name.trim() || busy) return;
    setBusy(true); setError('');
    try { const value = await post<PendingPair>('/pair/request', { name: name.trim(), kind: 'browser' }); sessionStorage.setItem('airlink-pair', JSON.stringify(value)); setPending(value); }
    catch (failure) { showError(failure); } finally { setBusy(false); }
  }
  async function selected(values: Selection[], existingId: string | null = reselect) {
    if (!host || !manager.current) return;
    setError('');
    try {
      if (existingId) { await manager.current.attach(existingId, values); setReselect(null); }
      else await manager.current.create(values, host.id);
      await refresh();
    } catch (failure) { showError(failure); }
  }
  function inputChange(event: ChangeEvent<HTMLInputElement>) {
    try { if (event.target.files?.length) void selected(fromFiles(event.target.files)); } catch (failure) { showError(failure); }
    event.target.value = '';
  }
  async function drop(event: DragEvent) {
    event.preventDefault(); setDragging(false); setReselect(null);
    if (!authenticated) return;
    try { await selected(await fromDrop(event.dataTransfer), null); } catch (failure) { showError(failure); }
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
        {pending ? <div className="pair-pending" role="status"><span className="spinner" /><h2>{t.pending}</h2><p>{t.pendingHelp}</p><button className="text-button" onClick={() => { sessionStorage.removeItem('airlink-pair'); setPending(null); }}>{t.leavePending}</button></div>
          : <form className="pair-form" onSubmit={connect}><label htmlFor="device-name">{t.deviceName}</label><input id="device-name" value={name} maxLength={80} autoComplete="off" onChange={e => setName(e.target.value)} /><button className="primary" disabled={!name.trim() || busy || !secure}>{busy ? t.connecting : t.connect}</button></form>}
        {!online && <p className="warning">{t.offline} <button className="text-button" onClick={() => { void refresh(); }}>{t.refresh}</button></p>}
      </section> : <>
        <section className="connection"><div className="connection-icon"><Glyph name="device" size={28} /></div><div><h1>{host?.name}</h1><p><span className={`status-dot ${online ? '' : 'offline'}`} />{online ? t.paired : t.offline}<span className="connection-detail">{online ? t.onlineHelp : t.offlineHelp}</span></p></div><Glyph name="shield" /></section>
        <section className={`dropzone ${dragging ? 'dragging' : ''}`} aria-label={t.send} onDragOver={event => { event.preventDefault(); setDragging(true); }} onDragLeave={event => { if (!event.currentTarget.contains(event.relatedTarget as Node)) setDragging(false); }} onDrop={event => { void drop(event); }}>
          <div className="drop-symbol"><Glyph name="upload" size={30} /></div><h2>{t.drop}</h2><p>{t.dropHelp}</p><div className="button-row"><button className="primary" onClick={() => { setReselect(null); filesInput.current?.click(); }}>{t.chooseFiles}</button><button className="secondary" onClick={() => { setReselect(null); folderInput.current?.click(); }}>{t.chooseFolder}</button></div>
        </section>
        <section className="transfer-section"><div className="section-heading"><h2>{t.transfers}</h2><span>{transfers.length || ''}</span></div>
          {!transfers.length ? <div className="empty"><Glyph name="file" size={29} /><h3>{t.empty}</h3><p>{t.emptyHelp}</p></div> : <div className="transfer-list">{transfers.map(transfer => {
            const upload = transfer.direction === 'receive'; const stage = local.get(transfer.id); const files = transfer.files.filter(file => !file.directory);
            const active = !terminal(transfer.state); const needsSource = upload && active && (stage?.phase === 'source' || stage?.phase === 'error');
            const percentage = transfer.state === 'completed' ? 100 : progressPercent(transfer.completedBytes, transfer.totalBytes);
            return <article className="transfer" key={transfer.id}>
              <div className="transfer-top"><span className={`transfer-icon ${!upload ? 'incoming' : ''}`}><Glyph name={upload ? 'upload' : 'download'} /></span><div className="transfer-title"><h3>{files.length === 1 ? files[0].path.split('/').at(-1) : `${files.length} ${t.files}`}</h3><p>{upload ? t.toHost : t.fromHost} {host?.name}</p></div><span className={`state ${transfer.state}`}>{!upload ? t.browserDownload : t[transfer.state] || transfer.state}</span></div>
              {upload && <><progress aria-label={`${percentage.toFixed(0)}%`} max={100} value={percentage} /><div className="transfer-meta"><span>{bytes(transfer.completedBytes, language)} / {bytes(transfer.totalBytes, language)}</span><span>{transfer.bytesPerSecond > 0 && transfer.state === 'running' ? `${bytes(transfer.bytesPerSecond, language)}/s` : `${Math.floor(percentage)}%`}</span></div></>}
              {stage?.phase === 'hashing' && active && <p className="transfer-note">{t.hashing} · {bytes(stage.done || 0, language)} / {bytes(stage.total || 0, language)}</p>}
              {stage?.phase === 'reconnecting' && active && <p className="transfer-note">{t.reconnecting} · {t.offlineHelp}</p>}
              {(transfer.error || stage?.phase === 'error') && <p className={transfer.state === 'failed' || stage?.phase === 'error' ? 'warning' : 'transfer-note'}>{transfer.error || stage?.error}</p>}
              {needsSource && <div className="source-needed"><p>{t.sourceHelp}</p><div className="button-row"><button className="secondary small" onClick={() => { setReselect(transfer.id); filesInput.current?.click(); }}>{t.retrySource}</button><button className="text-button" onClick={() => { setReselect(transfer.id); folderInput.current?.click(); }}>{t.chooseFolder}</button></div></div>}
              {!upload && <><p className="transfer-note">{t.downloadHelp}</p><div className="download-files">{files.map(file => <div key={file.id}><span title={file.path}>{file.path}</span><span>{bytes(file.size, language)}</span><button className="text-button" disabled={['cancelled', 'failed'].includes(transfer.state)} onClick={() => { void download(transfer, file.id); }}>{t.download}</button></div>)}</div>{(files.length > 1 || transfer.files.some(file => file.directory)) && <><button className="secondary small" disabled={['cancelled', 'failed'].includes(transfer.state)} onClick={() => { void download(transfer); }}>{t.downloadAll}</button><p className="transfer-note">{t.archiveHelp}</p></>}</>}
              {upload && active && <div className="transfer-actions">{['running', 'queued', 'reconnecting'].includes(transfer.state) && <button className="text-button" onClick={() => { void action(transfer.id, 'pause'); }}>{t.pause}</button>}{transfer.state === 'paused' && <button className="text-button" onClick={() => { void action(transfer.id, 'resume'); }}>{t.resume}</button>}<button className="text-button muted" onClick={() => { void action(transfer.id, 'cancel'); }}>{t.cancel}</button></div>}
            </article>;
          })}</div>}
        </section>
        <p className="mobile-note">{t.mobile}</p>
      </>}
      <details className="trust-details"><summary><Glyph name="shield" size={17} />{t.trust}</summary><div className="trust-content"><p>{t.trustIntro}</p><p>{t.trustCompare}</p>{host?.fingerprint && <div className="fingerprint"><span>{t.fingerprint}</span><code>{host.fingerprint}</code></div>}<a className="secondary small inline-button" href="/certificate.cer" download>{t.certificate}</a><a className="secondary small inline-button" href="/certificate.mobileconfig">{t.certificateIOS}</a><p>{t.trustSafety}</p><p>{t.trustDesktop}</p><p>{t.trustIOS}</p><p>{t.trustAndroid}</p><p>{t.awaitingHost}</p></div></details>
      <input ref={filesInput} className="hidden-input" type="file" multiple onChange={inputChange} aria-label={t.chooseFiles} />
      <input ref={folderInput} className="hidden-input" type="file" multiple {...{ webkitdirectory: '' }} onChange={inputChange} aria-label={t.chooseFolder} />
    </main>
    <footer><span>{t.privacy}</span><span>AirLink {host?.version || '0.1.0'}</span></footer>
  </div>;
}
