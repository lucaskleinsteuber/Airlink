import { consumeShareSecret, routeFor } from './sharing';
export function resolveEntry<T>(location: Pick<Location, 'pathname' | 'hash' | 'search'>, session: Pick<Storage, 'getItem' | 'setItem'>, replace: (url: string) => void, create: { app: () => T; share: (id: string, secret: string) => T; invalid: () => T }): T {
  const route = routeFor(location.pathname);
  if (route.kind === 'share') return create.share(route.id, consumeShareSecret(route.id, location, session, replace));
  if (route.kind === 'invalid-share' && location.hash) replace(location.pathname + location.search);
  return route.kind === 'invalid-share' ? create.invalid() : create.app();
}
