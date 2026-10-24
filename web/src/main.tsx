import React from 'react';
import { createRoot } from 'react-dom/client';
import { App } from './App';
import { InvalidShare, ShareRecipient } from './ShareRecipient';
import { resolveEntry } from './entry';
import './style.css';
const session = { getItem: (key: string) => window.sessionStorage.getItem(key), setItem: (key: string, value: string) => window.sessionStorage.setItem(key, value) };
// Resolve the route and remove its fragment before React mounts or any API runs.
const content = resolveEntry(window.location, session, url => window.history.replaceState(null, '', url), {
  app: () => <App />, share: (id, secret) => <ShareRecipient id={id} secret={secret} />, invalid: () => <InvalidShare />,
});
createRoot(document.getElementById('root')!).render(<React.StrictMode>{content}</React.StrictMode>);
