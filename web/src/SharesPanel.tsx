import { useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { post } from './api';
import { featureText } from './features-i18n';
import type { Language } from './i18n';
import { bytes } from './protocol';
import { canShareTransfer, createSharePolicy, defaultShareDraft, shareState } from './sharing';
import { SharePolicies } from './SharePolicies';
import type { Share, Transfer } from './types';
export function SharesPanel({ shares, transfers, language, refresh, showError, notice }: { shares: Share[]; transfers: Transfer[]; language: Language; refresh: () => Promise<void>; showError: (error: unknown) => void; notice: (text: string) => void }) {
  const t = featureText(language);
  const [draft, setDraft] = useState(defaultShareDraft);
  const [transferId, setTransferId] = useState('');
  const [busy, setBusy] = useState(false);
  const links = useRef(new Map<string, HTMLInputElement>());
  const candidates = transfers.filter(canShareTransfer).filter(transfer => transfer.files.some(file => !file.skipped));
  async function create(event: FormEvent) {
    event.preventDefault(); if (!transferId || busy) return;
    setBusy(true);
    try {
      let policy;
      try { policy = createSharePolicy(draft); } catch (error) { throw new Error(t[(error as Error).message as 'downloadLimit' | 'expiryInvalid']); }
      await post<Share>('/shares', { ...policy, transferId });
      setDraft(defaultShareDraft()); notice(t.shareCreated); await refresh();
    } catch (error) { showError(error); } finally { setBusy(false); }
  }
  async function copy(share: Share) {
    if (!share.url) return;
    try { if (!navigator.clipboard?.writeText) throw new Error(); await navigator.clipboard.writeText(share.url); notice(t.copied); }
    catch { links.current.get(share.id)?.select(); notice(t.selectCopy); }
  }
  async function revoke(share: Share) {
    try { await post(`/shares/${encodeURIComponent(share.id)}/revoke`); notice(t.shareRevoked); await refresh(); } catch (error) { showError(error); }
  }
  return <section className="feature-section"><div className="section-heading"><h2>{t.shares}</h2><span>{shares.length || ''}</span></div><p className="feature-intro">{t.sharesIntro}</p>
    <details className="create-share" open={!shares.length}>
      <summary>{t.newShare}</summary>
      {!candidates.length ? <p className="transfer-note">{t.noSource}</p> : <form onSubmit={event => { void create(event); }}>
        <label className="field-label source-select">{t.source}<select value={transferId} required disabled={busy} onChange={event => setTransferId(event.target.value)}><option value="">{t.selectSource}</option>{candidates.map(transfer => <option key={transfer.id} value={transfer.id}>{transfer.files.find(file => !file.directory && !file.skipped)?.path || transfer.files[0]?.path || transfer.id} · {bytes(transfer.totalBytes, language)}</option>)}</select></label>
        <SharePolicies draft={draft} setDraft={setDraft} language={language} disabled={busy} />
        <div className="feature-actions"><button className="primary" disabled={busy || !transferId}>{busy ? t.creating : t.create}</button></div>
      </form>}
    </details>
    <div className="share-list">{[...shares].sort((a, b) => b.createdAt.localeCompare(a.createdAt)).map(share => {
      const state = shareState(share);
      return <article className="share-row" key={share.id}><div className="section-heading"><h3>{share.name}</h3><span className={`share-state ${state}`}>{t[state]}</span></div>
        <p className="transfer-note">{bytes(share.totalBytes, language)} · {share.downloads} / {share.maxDownloads || t.unlimitedShort} {t.downloads}{share.passwordProtected ? ` · ${t.passwordOn}` : ''}</p>
        <p className="transfer-note">{share.expiresAt ? `${t.expires}: ${new Date(share.expiresAt).toLocaleString(language)}` : t.expiresNever}</p>
        {share.url && <label className="sr-only" htmlFor={`share-${share.id}`}>{t.link}</label>}
        {share.url && <input id={`share-${share.id}`} ref={element => { if (element) links.current.set(share.id, element); else links.current.delete(share.id); }} className="share-url" value={share.url} readOnly autoComplete="off" spellCheck={false} />}
        <div className="feature-actions"><button className="secondary small" disabled={!share.url || state === 'revoked'} onClick={() => { void copy(share); }}>{t.copyLink}</button><button className="text-button muted" disabled={share.revoked} onClick={() => { void revoke(share); }}>{t.revoke}</button></div>
      </article>;
    })}</div>
    <p className="mobile-note">{t.shareLocal}</p>
  </section>;
}
