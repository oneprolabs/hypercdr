import { AUTH_EXPIRED_EVENT, readStoredAuthSession } from '../auth/session';

export class ApiRequestError extends Error {
  readonly status: number;
  readonly code: string;
  readonly requestId: string;
  readonly details: Record<string, unknown>;

  constructor(message: string, response: Response, details: Record<string, unknown> = {}) {
    super(message);
    this.name = 'ApiRequestError';
    this.status = response.status;
    this.code = String(details.error || 'request_failed');
    this.requestId = response.headers.get('x-request-id') || String(details.requestId || '');
    this.details = details;
  }
}

async function readApiError(response: Response): Promise<Error> {
  try {
    const body = await response.json() as Record<string, unknown>;
    const message = body?.message || body?.error || `${response.status} ${response.statusText}`;
    return new ApiRequestError(String(message), response, body);
  } catch {
    return new ApiRequestError(`${response.status} ${response.statusText}`, response);
  }
}

export async function ensureApiResponse(response: Response, path: string, requestToken = ''): Promise<Response> {
  if (response.ok) return response;
  const publicAuthRequest = path === '/api/v1/auth/login' || path === '/api/v1/auth/captcha' || path === '/api/v1/auth/forgot-password' || path === '/api/v1/auth/reset-password';
  const currentToken = readStoredAuthSession()?.session.token || '';
  if (response.status === 401 && !publicAuthRequest && requestToken !== '' && requestToken === currentToken) {
    window.dispatchEvent(new Event(AUTH_EXPIRED_EVENT));
  }
  throw await readApiError(response);
}

export function apiHeaders(json = false, token = readStoredAuthSession()?.session.token || ''): Record<string, string> {
  const headers: Record<string, string> = {};
  if (json) headers['Content-Type'] = 'application/json';
  if (token) headers.Authorization = `Bearer ${token}`;
  return headers;
}

export async function apiGet<T>(path: string): Promise<T> {
  const token = readStoredAuthSession()?.session.token || '';
  const response = await ensureApiResponse(await fetch(path, { cache: 'no-store', headers: apiHeaders(false, token) }), path, token);
  return response.json() as Promise<T>;
}

export async function apiPost<T>(path: string, body: unknown): Promise<T> {
  const token = readStoredAuthSession()?.session.token || '';
  const response = await ensureApiResponse(await fetch(path, {
    method: 'POST',
    headers: apiHeaders(true, token),
    body: JSON.stringify(body),
  }), path, token);
  return response.json() as Promise<T>;
}

export async function apiPut<T>(path: string, body: unknown): Promise<T> {
  const token = readStoredAuthSession()?.session.token || '';
  const response = await ensureApiResponse(await fetch(path, { method: 'PUT', headers: apiHeaders(true, token), body: JSON.stringify(body) }), path, token);
  return response.json() as Promise<T>;
}

export async function apiPatch<T>(path: string, body: unknown): Promise<T> {
  const token = readStoredAuthSession()?.session.token || '';
  const response = await ensureApiResponse(await fetch(path, {
    method: 'PATCH',
    headers: apiHeaders(true, token),
    body: JSON.stringify(body),
  }), path, token);
  return response.json() as Promise<T>;
}

export async function apiDelete<T>(path: string): Promise<T> {
  const token = readStoredAuthSession()?.session.token || '';
  const response = await ensureApiResponse(await fetch(path, { method: 'DELETE', headers: apiHeaders(false, token) }), path, token);
  return response.json() as Promise<T>;
}
