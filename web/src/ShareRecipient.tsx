import { useEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { api, APIError, post } from './api';
import { featureText } from './features-i18n';
import { messages, systemLanguage } from './i18n';
import type { Language } from './i18n';
import { bytes } from './protocol';
import { fileHasPayload } from './organization';
import { publicSharePath, shareState } from './sharing';
import type { Share } from './types';
function preference(key: string, fallback: string): string { try { return localStorage.getItem(key) || fallback; } catch { return fallback; } }
export function ShareRecipient({ id, secret }: { id: string; secret: string }) {
  const [languageSetting, setLanguageSetting] = useState(() => preference('airlink-language', 'system'));
  const language: Language = languageSetting === 'en' ? 'en' : languageSetting === 'de' ? 'de' : systemLanguage();
  const [appearance, setAppearance] = useState(() => preference('airlink-appearance', 'system'));
  const t = featureText(language); const common = messages[language];
  const texts = useRef(t); texts.current = t;
  const [share, setShare] = useState<Share>();
  const [phase, setPhase] = useState<'loading' | 'locked' | 'ready' | 'gone' | 'unavailable'>('loading');
  const [problem, setProblem] = useState('');
  const [missing, setMissing] = useState(false);
  const [notice, setNotice] = useState('');
  const [password, setPassword] = useState('');
  const [busy, setBusy] = useState(false);
  const [refreshKey, setRefreshKey] = useState(0);
  const terminal = useRef(false);
  const path = publicSharePath(id);
  useEffect(() => {
    document.documentElement.lang = language;
    document.documentElement.dataset.appearance = appearance;
    try { localStorage.setItem('airlink-language', languageSetting); localStorage.setItem('airlink-appearance', appearance); } catch { /* UI preferences are optional. */ }
  }, [appearance, language, languageSetting]);
  useEffect(() => {
    let stopped = false; let timer: ReturnType<typeof setTimeout> | undefined; let fetching = false;
    async function load() {
      if (fetching || stopped || terminal.current) return;
      fetching = true; let repeat = true;
      try {
        const value = await api<Share>(path);
        if (stopped) return;
        if (['expired', 'revoked'].includes(shareState(value))) { setShare(undefined); setPhase('gone'); terminal.current = true; repeat = false; }
        else { setShare(value); setPhase('ready'); setProblem(''); }
      } catch (error) {
        if (stopped) return;
        if (error instanceof APIError && [401, 403].includes(error.status)) { setShare(undefined); setPhase('locked'); setProblem(''); repeat = false; }
        else if (error instanceof APIError && error.status === 410) { setShare(undefined); setPhase('gone'); setProblem(''); terminal.current = true; repeat = false; }
        else if (error instanceof APIError && error.status === 404) { setShare(undefined); setPhase('gone'); setProblem(''); setMissing(true); terminal.current = true; repeat = false; }
        else if (error instanceof APIError && error.status === 409) { setShare(undefined); setPhase('unavailable'); setProblem(texts.current.unavailable); }
        else setProblem(texts.current.networkError);
      } finally {
        fetching = false;
        if (!stopped && repeat) { if (timer) clearTimeout(timer); timer = setTimeout(() => { void load(); }, 15_000); }
      }
    }
    const visible = () => { if (document.visibilityState === 'visible') { if (timer) clearTimeout(timer); void load(); } };
    document.addEventListener('visibilitychange', visible); void load();
    return () => { stopped = true; if (timer) clearTimeout(timer); document.removeEventListener('visibilitychange', visible); };
  }, [path, refreshKey]);
  async function unlock(event: FormEvent) {
    event.preventDefault(); if (!secret || busy) return;
    setBusy(true); setProblem('');
    try {
      const value = await post<{ share: Share }>(`${path}/unlock`, { secret, ...(password ? { password } : {}) });
      setPassword(''); setShare(value.share); setPhase('ready'); setRefreshKey(current => current + 1);
    } catch (error) {
      if (error instanceof APIError && error.status === 410) { setShare(undefined); setPhase('gone'); terminal.current = true; }
      else if (error instanceof APIError && error.status === 404) { setShare(undefined); setPhase('gone'); setMissing(true); terminal.current = true; }
      else if (error instanceof APIError && [401, 403].includes(error.status)) setProblem(t.unauthorized);
      else if (error instanceof APIError && error.status === 409) setProblem(t.unavailable);
      else setProblem(error instanceof APIError ? t.shareError : t.networkError);
    } finally { setBusy(false); }
  }
  const files = share?.files.filter(fileHasPayload) || [];
  const state = share ? shareState(share) : undefined;
  return <div className="shell share-recipient">
    <header className="masthead"><a className="brand" href="/"><span className="brand-mark"><svg width="23" height="23" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.65" strokeLinecap="round"><path d="M10 13a5 5 0 0 0 7 .2l3-3a5 5 0 0 0-7-7l-2 2M14 11a5 5 0 0 0-7-.2l-3 3a5 5 0 0 0 7 7l2-2" /></svg></span>AirLink</a><div className="preferences">
      <label><span className="sr-only">{common.language}</span><select value={languageSetting} onChange={event => setLanguageSetting(event.target.value)}><option value="system">{common.system}</option><option value="de">Deutsch</option><option value="en">English</option></select></label>
      <label><span className="sr-only">{common.appearance}</span><select value={appearance} onChange={event => setAppearance(event.target.value)}><option value="system">◐ {common.system}</option><option value="light">☼ {common.light}</option><option value="dark">☾ {common.dark}</option></select></label>
    </div></header>
    <main>
      {(problem || notice) && <div className={`banner ${problem ? 'error' : ''}`} role={problem ? 'alert' : 'status'}><span>{problem || notice}</span><button className="icon-button" aria-label={t.close} onClick={() => { setProblem(''); setNotice(''); }}>×</button></div>}
      {!share && phase !== 'gone' && phase !== 'unavailable' && <section className="welcome share-welcome"><div className="hero-symbol"><span aria-hidden="true">↗</span></div><h1>{t.shareOpen}</h1><p className="intro">{t.shareOpenIntro}</p>
        {phase === 'loading' && !problem ? <div className="pair-pending" role="status"><span className="spinner" /><p>{t.unlocking}</p></div> : <form className="pair-form" onSubmit={event => { void unlock(event); }}>
          {!secret ? <p className="transfer-note">{t.missingSecret}</p> : <><label htmlFor="share-password">{t.password} · {t.optional}</label><input id="share-password" type="password" autoComplete="current-password" value={password} disabled={busy} onChange={event => setPassword(event.target.value)} /><p className="field-hint">{t.passwordHint}</p><button className="primary" disabled={busy}>{busy ? t.unlocking : t.unlock}</button></>}
        </form>}
      </section>}
      {phase === 'gone' && <section className="share-unavailable"><h1>{missing ? t.shareError : t.gone}</h1><p>{t.shareLocal}</p></section>}
      {phase === 'unavailable' && <section className="share-unavailable"><h1>{t.unavailable}</h1><button className="secondary" onClick={() => setRefreshKey(current => current + 1)}>{t.retry}</button></section>}
      {problem && phase !== 'gone' && phase !== 'unavailable' && <button className="text-button" onClick={() => setRefreshKey(current => current + 1)}>{t.retry}</button>}
      {share && <section className="recipient-files"><div className="recipient-heading"><span className="eyebrow">Quick Share</span><h1>{share.name}</h1><p>{files.length} {t.files} · {bytes(share.totalBytes, language)}</p></div>
        <div className="share-policy-summary"><span>{share.downloads} / {share.maxDownloads || t.unlimitedShort} {t.downloads}</span><span>{share.expiresAt ? `${t.expires}: ${new Date(share.expiresAt).toLocaleString(language)}` : t.expiresNever}</span>{share.passwordProtected && <span>{t.passwordOn}</span>}</div>
        {state === 'exhausted' && <p className="permission-hint">{t.exhausted}. {t.quotaHelp}</p>}
        <div className="download-files public-downloads">{files.map(file => <div key={file.id}><span title={file.path}>{file.path}</span><span>{bytes(file.size, language)}</span><a className="secondary small" href={`/api/v1${path}/files/${encodeURIComponent(file.id)}`} download onClick={() => setNotice(t.downloadStarted)}>{t.download}</a></div>)}</div>
        {(files.length > 1 || share.files.some(file => file.directory)) && <><a className="primary inline-button" href={`/api/v1${path}/archive`} download onClick={() => setNotice(t.downloadStarted)}>{t.archive}</a><p className="transfer-note">{t.archiveHelp}</p></>}
        <p className="mobile-note">{t.downloadHelp}</p><p className="mobile-note">{t.shareLocal}</p>
      </section>}
      <a className="text-link" href="/setup">{t.secureSetup}</a>
    </main><footer><span>{t.privacy}</span><span>AirLink 0.4.0</span></footer>
  </div>;
}
export function InvalidShare() {
  const t = featureText(systemLanguage());
  return <div className="shell"><main className="share-unavailable"><h1>AirLink</h1><p>{t.shareError}</p><a href="/">AirLink</a></main></div>;
}
