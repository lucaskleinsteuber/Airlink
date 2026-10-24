import { useRef, useState } from 'react';
import { api, post } from './api';
import type { Language } from './i18n';
import { featureText } from './features-i18n';
import { browserClipboardDirection, clipboardBytes, CLIPBOARD_LIMIT, validClipboardText } from './sharing';
import type { ClipboardMessage } from './types';
export function ClipboardPanel({ allowed, messages, hostName, language, refresh, showError, notice }: { allowed: boolean; messages: ClipboardMessage[]; hostName: string; language: Language; refresh: () => Promise<void>; showError: (error: unknown) => void; notice: (text: string) => void }) {
  const t = featureText(language);
  const [text, setText] = useState('');
  const [busy, setBusy] = useState(false);
  const editor = useRef<HTMLTextAreaElement>(null);
  const messageFields = useRef(new Map<string, HTMLTextAreaElement>());
  const pendingSend = useRef<{ text: string; id: string } | undefined>(undefined);
  const count = clipboardBytes(text);
  async function paste() {
    try { if (!navigator.clipboard?.readText) throw new Error(); setText(await navigator.clipboard.readText()); editor.current?.focus(); }
    catch { editor.current?.focus(); notice(t.manualPaste); }
  }
  async function send() {
    if (!allowed || !validClipboardText(text) || busy) return;
    setBusy(true);
    if (pendingSend.current?.text !== text) pendingSend.current = { text, id: crypto.randomUUID() };
    try { await post('/clipboard', pendingSend.current); pendingSend.current = undefined; setText(''); notice(t.textSent); await refresh(); }
    catch (error) { showError(error); } finally { setBusy(false); }
  }
  async function copy(message: ClipboardMessage) {
    try { if (!navigator.clipboard?.writeText) throw new Error(); await navigator.clipboard.writeText(message.text); notice(t.copied); }
    catch { messageFields.current.get(message.id)?.select(); notice(t.selectCopy); }
  }
  async function dismiss(id: string) {
    try { await api(`/clipboard/${encodeURIComponent(id)}`, { method: 'DELETE' }); await refresh(); } catch (error) { showError(error); }
  }
  return <section className="feature-section"><div className="section-heading"><h2>{t.clipboard}</h2></div><p className="feature-intro">{t.clipboardIntro}</p>
    {!allowed && <p className="permission-hint">{t.clipboardDisabled}</p>}
    <label className="field-label">{t.text}<textarea ref={editor} rows={5} value={text} disabled={!allowed || busy} spellCheck={false} onChange={event => setText(event.target.value)} /></label>
    <div className="text-editor-meta"><span className={count > CLIPBOARD_LIMIT ? 'warning' : ''}>{new Intl.NumberFormat(language).format(count)} / {new Intl.NumberFormat(language).format(CLIPBOARD_LIMIT)} B UTF-8</span><span>{t.textLimit}</span></div>
    <div className="feature-actions"><button className="secondary" disabled={!allowed || busy} onClick={() => { void paste(); }}>{t.paste}</button><button className="primary" disabled={!allowed || busy || !validClipboardText(text)} onClick={() => { void send(); }}>{busy ? t.sending : t.sendText}</button></div>
    <div className="clipboard-list">{!messages.length && <p className="empty-copy">{t.noMessages}</p>}{[...messages].sort((a, b) => b.createdAt.localeCompare(a.createdAt)).map(message => <article className="clipboard-message" key={message.id}>
      <div className="section-heading"><h3>{browserClipboardDirection(message.direction) === 'incoming' ? t.from : t.to} {hostName}</h3><time dateTime={message.createdAt}>{new Date(message.createdAt).toLocaleTimeString(language, { hour: '2-digit', minute: '2-digit' })}</time></div>
      <textarea ref={element => { if (element) messageFields.current.set(message.id, element); else messageFields.current.delete(message.id); }} value={message.text} readOnly rows={Math.min(5, Math.max(2, message.text.split('\n').length))} aria-label={t.text} spellCheck={false} />
      <div className="feature-actions"><button className="secondary small" onClick={() => { void copy(message); }}>{t.copy}</button><button className="text-button muted" onClick={() => { void dismiss(message.id); }}>{t.dismiss}</button></div>
    </article>)}</div><p className="mobile-note">{t.messagesTemporary}</p>
  </section>;
}
