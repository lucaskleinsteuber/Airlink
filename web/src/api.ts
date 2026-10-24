export class APIError extends Error { constructor(public status: number, message: string) { super(message); } }
export async function api<T>(path: string, options: RequestInit = {}): Promise<T> {
  const response = await fetch(`/api/v1${path}`, { credentials: 'same-origin', ...options, headers: { ...(options.body instanceof Blob || options.body instanceof ArrayBuffer ? {} : { 'Content-Type': 'application/json' }), ...options.headers } });
  if (!response.ok) {
    const payload = await response.json().catch(() => ({})) as { error?: string };
    throw new APIError(response.status, payload.error || `${response.status} ${response.statusText}`);
  }
  return response.status === 204 ? undefined as T : response.json() as Promise<T>;
}
export function post<T>(path: string, body?: unknown): Promise<T> { return api<T>(path, { method: 'POST', body: JSON.stringify(body ?? {}) }); }
export const transferPath = (id: string) => `/transfers/${encodeURIComponent(id)}`;
