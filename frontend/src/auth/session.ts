import type { AuthSession } from './types';

export const AUTH_SESSION_KEY = 'hypercdr.auth.session';
export const AUTH_EXPIRED_EVENT = 'hypercdr:auth-expired';

export function readStoredAuthSession(): AuthSession | null {
  try {
    const raw = localStorage.getItem(AUTH_SESSION_KEY);
    if (!raw) return null;
    const session = JSON.parse(raw) as AuthSession;
    if (!session?.session?.token || !session?.session?.expiresAt || !(session?.user?.username || session?.user?.email)) {
      localStorage.removeItem(AUTH_SESSION_KEY);
      return null;
    }
    if (Date.parse(session.session.expiresAt) <= Date.now()) {
      localStorage.removeItem(AUTH_SESSION_KEY);
      return null;
    }
    return session;
  } catch {
    try {
      localStorage.removeItem(AUTH_SESSION_KEY);
    } catch {
      // localStorage can be unavailable in restricted browser contexts.
    }
    return null;
  }
}

export function writeStoredAuthSession(session: AuthSession) {
  try {
    localStorage.setItem(AUTH_SESSION_KEY, JSON.stringify(session));
  } catch {
    // The in-memory session still keeps the current tab signed in.
  }
}

export function clearStoredAuthSession() {
  try {
    localStorage.removeItem(AUTH_SESSION_KEY);
  } catch {
    // localStorage can be unavailable in restricted browser contexts.
  }
}

// Bind API calls to the identity rendered by this tab, rather than silently
// borrowing credentials written by another tab.
export const AUTH_CHANGED_EVENT = 'hypercdr:auth-changed';
let renderedToken: string | undefined;
export function bindRequestSession(token: string | undefined) { renderedToken = token; }
export function requestSessionToken(): string {
  const stored = readStoredAuthSession()?.session.token || '';
  if (renderedToken !== undefined && stored !== renderedToken) {
    window.dispatchEvent(new Event(AUTH_CHANGED_EVENT));
    throw new Error('The signed-in account changed. Reloading this page.');
  }
  return stored;
}
export function assertRequestSession(token: string) {
  if (renderedToken !== undefined && (token !== renderedToken || token !== (readStoredAuthSession()?.session.token || ''))) {
    window.dispatchEvent(new Event(AUTH_CHANGED_EVENT));
    throw new Error('Discarded a response from a previous account.');
  }
}
