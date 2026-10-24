import type { Dispatch, SetStateAction } from 'react';
import type { Language } from './i18n';
import { featureText } from './features-i18n';
import type { ShareDraft } from './sharing';
export function SharePolicies({ draft, setDraft, language, disabled }: { draft: ShareDraft; setDraft: Dispatch<SetStateAction<ShareDraft>>; language: Language; disabled: boolean }) {
  const t = featureText(language);
  const update = (key: keyof ShareDraft, value: string | boolean) => setDraft(current => ({ ...current, [key]: value }));
  return <div className="feature-form-grid">
    <label className="field-label">{t.name}<input value={draft.name} placeholder={t.optional} disabled={disabled} onChange={event => update('name', event.target.value)} /></label>
    <label className="field-label">{t.password}<input type="password" autoComplete="new-password" value={draft.password} placeholder={t.optional} disabled={disabled} onChange={event => update('password', event.target.value)} /></label>
    <label className="field-label">{t.expires}<input type="datetime-local" value={draft.expiresAt} required={!draft.noExpiry} disabled={disabled || draft.noExpiry} onChange={event => update('expiresAt', event.target.value)} /></label>
    <label className="field-label">{t.maxDownloads}<input type="number" min={0} step={1} required inputMode="numeric" value={draft.maxDownloads} disabled={disabled} onChange={event => update('maxDownloads', event.target.value)} /><span className="field-hint">{t.unlimited}</span></label>
    <label className="check-label"><input type="checkbox" checked={draft.noExpiry} disabled={disabled} onChange={event => update('noExpiry', event.target.checked)} />{t.noExpiry}</label>
  </div>;
}
