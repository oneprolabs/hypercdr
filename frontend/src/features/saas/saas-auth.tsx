import { useEffect, useState, type FormEvent } from 'react';
import { Mail, LockKeyhole, Building2, UserRound, ArrowLeft, Eye, EyeOff } from 'lucide-react';
import { HyperCDRMark, writeStoredAuthSession, type ApiLoginResponse } from '../../public';
import './saas-auth.css';
import { organizationRole } from './organization-terms';
let base = '/api/v1/saas/auth';
let editionLabel = 'Community';
export function configureSaaSAuth(apiBase: string, edition: string) { base=apiBase;editionLabel=edition; }
type AuthConfig = { publicUrl?:string; saas: boolean; signupEnabled: boolean; emailConfigured: boolean; googleEnabled: boolean };
export async function publicRequest<T>(path: string, body?: unknown): Promise<T> {
  // Public onboarding intentionally uses fetch because no session exists yet.
  // eslint-disable-next-line no-restricted-globals
  const response = await fetch(`${base}${path}`, { method: body === undefined ? 'GET' : 'POST', credentials: 'same-origin', headers: body === undefined ? {} : { 'Content-Type': 'application/json' }, body: body === undefined ? undefined : JSON.stringify(body) });
  const data = await response.json();
  if (!response.ok) throw new Error(data.message || 'The request could not be completed. Please try again.');
  return data as T;
}
function useAuthConfig() {
  const [config, setConfig] = useState<AuthConfig | null>(null);
  const [error, setError] = useState('');
  useEffect(() => { let active = true; void publicRequest<AuthConfig>('/config').then(v => { if (active) setConfig(v); }).catch(e => { if (active) setError(e.message); }); return () => { active = false; }; }, []);
  return { config, error };
}
function finishLogin(value: ApiLoginResponse) {
  writeStoredAuthSession({ ...value, signedInAt: new Date().toISOString() });
  sessionStorage.removeItem('hypercdr.invitation');
  window.location.replace('/');
}
function GoogleMark() {
  return <svg width="18" height="18" viewBox="0 0 24 24" aria-hidden="true"><path fill="#4285F4" d="M21.6 12.23c0-.71-.06-1.39-.18-2.05H12v3.88h5.38a4.6 4.6 0 0 1-2 3.02v2.51h3.23c1.89-1.74 2.99-4.3 2.99-7.36Z"/><path fill="#34A853" d="M12 22c2.7 0 4.96-.9 6.61-2.41l-3.23-2.51c-.9.6-2.04.97-3.38.97-2.61 0-4.82-1.76-5.61-4.12H3.05v2.59A10 10 0 0 0 12 22Z"/><path fill="#FBBC05" d="M6.39 13.93A6 6 0 0 1 6.08 12c0-.67.11-1.32.31-1.93V7.48H3.05A10 10 0 0 0 2 12c0 1.61.39 3.14 1.05 4.52l3.34-2.59Z"/><path fill="#EA4335" d="M12 5.95c1.47 0 2.79.5 3.82 1.49l2.87-2.87A9.6 9.6 0 0 0 12 2a10 10 0 0 0-8.95 5.48l3.34 2.59C7.18 7.71 9.39 5.95 12 5.95Z"/></svg>;
}
function GoogleButton({ invitation = '', onError }: { invitation?: string; onError: (error: string) => void }) {
  const [busy, setBusy] = useState(false);
  async function start() {
    setBusy(true); onError('');
    try {
      if (invitation) sessionStorage.setItem('hypercdr.invitation', invitation);
      const response = await publicRequest<{ url: string }>('/google/start', { invitation });
      window.location.assign(response.url);
    } catch (e) { onError(e instanceof Error ? e.message : 'Google sign-in could not be started.'); setBusy(false); }
  }
  return <button type="button" className="ee-google-button" disabled={busy} onClick={() => void start()}><GoogleMark />{busy ? 'Connecting to Google…' : 'Continue with Google'}</button>;
}
const oauthErrors: Record<string, string> = {
  account_link_required: 'This email already has an account. Sign in with your password, then connect Google in Sign-in & Security.',
  authorization_cancelled: 'Google sign-in was cancelled. You can try again or sign in with your password.',
  authorization_expired: 'Your Google sign-in request expired. Please try again.',
  invitation_identity_mismatch: 'Use the Google account matching your invitation. An existing account cannot join another organization.',
  signup_disabled: 'Public registration is disabled. Contact your organization administrator for an invitation.',
  identity_already_linked: 'This Google account is already connected. Sign in with the connected account.',
};
export function SaaSLoginSupplement() {
  const { config, error: configError } = useAuthConfig();
  const code = new URLSearchParams(window.location.search).get('oauth_error');
  const [error, setError] = useState(code ? oauthErrors[code] || 'Google sign-in could not be completed. Please try again.' : '');
  return <div className="ee-login-supplement">
    {new URLSearchParams(window.location.search).get('password_changed') === '1' && <p className="ee-auth-notice" role="status">Password updated. Sign in with your new password.</p>}
    {(error || configError) && <p className="hbdr-login-error" role="alert">{error || configError}</p>}
    {config?.googleEnabled && <><div className="ee-auth-divider"><span>or</span></div><GoogleButton onError={setError} /></>}
    {config?.publicUrl && config.publicUrl!==window.location.origin && <p className="ee-auth-description">Use the <a href={config.publicUrl+'/login'}>official platform entry</a> for registration and Google sign-in. Platform administrators can update Settings → Access &amp; Login.</p>}
    {config?.signupEnabled && <div className="hbdr-login-signup">New to HyperCDR? <a href="/signup">Create an account</a></div>}
  </div>;
}
const initialRequests = new Map<string, Promise<unknown>>();
function initialRequest<T>(path: string, body: unknown): Promise<T> {
  const key = path + JSON.stringify(body);
  if (!initialRequests.has(key)) initialRequests.set(key, publicRequest<T>(path, body));
  return initialRequests.get(key) as Promise<T>;
}
export function PublicAuthPage() {
  const [params] = useState(() => new URLSearchParams(window.location.search));
  const joining = window.location.pathname === '/join';
  const exchanging = window.location.pathname === '/auth/complete';
  const google = params.get('provider') === 'google';
  const token = params.get('token') || '';
  const { config, error: configError } = useAuthConfig();
  const [email, setEmail] = useState('');
  const [organization, setOrganization] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [password, setPassword] = useState('');
  const [confirmation, setConfirmation] = useState('');
  const [visible, setVisible] = useState(false);
  const [verified, setVerified] = useState(false);
  const [error, setError] = useState('');
  const [message, setMessage] = useState('');
  const [cooldown, setCooldown] = useState(0);
  useEffect(() => { if (cooldown <= 0) return; const timer = window.setTimeout(() => setCooldown(v => v - 1), 1000); return () => window.clearTimeout(timer); }, [cooldown]);
  const [busy, setBusy] = useState(false);
  const [loading, setLoading] = useState(exchanging || google || Boolean(token));
  const [role, setRole] = useState('');
  // A single request per mounted screen also avoids consuming an OAuth ticket twice in StrictMode.
  const [initial] = useState(() => {
    if (exchanging) return initialRequest<ApiLoginResponse>('/google/session', {});
    if (google) return initialRequest<{ email: string; organizationName: string }>('/google/profile', {});
    if (token) return initialRequest<{ email: string; organizationName?: string; role?: string; roleType?: string }>(joining ? '/invitation' : '/signup/verification', { token });
    return null;
  });
  useEffect(() => {
    document.documentElement.dataset.theme = 'dark';
    const meta = document.createElement('meta'); meta.name = 'referrer'; meta.content = 'no-referrer'; document.head.appendChild(meta);
    return () => meta.remove();
  }, []);
  useEffect(() => {
    let active = true;
    if (joining && token) sessionStorage.setItem('hypercdr.invitation', token);
    if (initial) void initial.then(value => {
      if (!active) return;
      if ('session' in value) { finishLogin(value); return; }
      setEmail(value.email); setOrganization(value.organizationName || ''); if ('role' in value) setRole(organizationRole({ name: value.role || '', roleType: value.roleType }).name); setVerified(true);
    }).catch(e => { if (active) setError(e.message); }).finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, [initial, joining, token]);
  async function submit(event: FormEvent) {
    event.preventDefault(); if (busy) return; setError(''); setMessage('');
    if (verified && !google && (Array.from(password).length < 8 || new TextEncoder().encode(password).length > 72)) { setError('Use at least 8 characters; the password must fit within 72 UTF-8 bytes.'); return; }
    if (verified && !google && password !== confirmation) { setError('Passwords do not match.'); return; }
    setBusy(true);
    try {
      if (!verified) {
        if (joining || token || google) { setError('Open a valid invitation or verification link.'); return; }
        const result = await publicRequest<{ message: string }>('/signup/request', { email }); setMessage(result.message); setCooldown(60);
      } else {
        const value = await publicRequest<ApiLoginResponse>('/complete', { token: google ? '' : token, organizationName: joining ? '' : organization, displayName, password: google ? '' : password, invitation: google && joining ? sessionStorage.getItem('hypercdr.invitation') || '' : '' });
        finishLogin(value);
      }
    } catch (e) { setError(e instanceof Error ? e.message : 'The request could not be completed.'); }
    finally { setBusy(false); }
  }
  const addressMismatch=!!config?.publicUrl && config.publicUrl!==window.location.origin;
  const unavailable = !joining && !exchanging && config && !config.signupEnabled;
  const title = exchanging ? 'Signing you in' : joining ? 'Join your organization' : verified ? 'Create your organization' : 'Create your account';
  return <main className="ee-public-auth"><div className="ee-auth-container">
    <a className="ee-auth-brand" href="/login" aria-label="HyperCDR sign in"><HyperCDRMark className="ee-auth-brand-mark" /><span>Hyper<strong>CDR</strong></span></a>
    <section className="ee-auth-card" aria-labelledby="ee-auth-title">
      <span className="ee-auth-eyebrow">HyperCDR {editionLabel}</span><h1 id="ee-auth-title">{title}</h1>
      <p className="ee-auth-description">{joining ? 'Accept your invitation to collaborate on application protection and recovery.' : exchanging ? 'Completing your secure sign-in…' : verified ? 'Your email is verified. Set up your organization to get started.' : 'Verify your email to start protecting your applications.'}</p>
      {(error || configError) && <div className="hbdr-login-error" role="alert">{error || configError}</div>}
      {message && <div className="ee-auth-notice" role="status"><Mail size={18} /><p>{message}</p></div>}
      {loading ? <p role="status">Checking your secure link…</p> : unavailable ? <p className="ee-auth-notice">Public registration is disabled. Contact your administrator for an invitation.</p> : !exchanging && (!(joining || token || google) || verified) && <form className="ee-auth-form" onSubmit={e => void submit(e)}>
        <label>Email address<div className="ee-auth-input"><Mail size={16} /><input type="email" autoComplete="email" required value={email} readOnly={verified} onChange={e => { setEmail(e.target.value); setMessage(''); }} placeholder="you@company.com" /></div></label>
        {verified && <>
          <label>Organization<div className="ee-auth-input"><Building2 size={16} /><input value={organization} onChange={e => setOrganization(e.target.value)} required={!joining} readOnly={joining} maxLength={100} placeholder="Your organization name" /></div></label>
          {role && <p className="ee-auth-description">Your role: {role}</p>}
          <label>Your name<div className="ee-auth-input"><UserRound size={16} /><input value={displayName} onChange={e => setDisplayName(e.target.value)} autoComplete="name" maxLength={100} placeholder="Full name" /></div></label>
          {!google && <>
            <label>Password<div className="ee-auth-input"><LockKeyhole size={16} /><input type={visible ? 'text' : 'password'} required autoComplete="new-password" value={password} onChange={e => setPassword(e.target.value)} minLength={8} maxLength={72} placeholder="At least 8 characters" /><button type="button" aria-label={visible ? 'Hide password' : 'Show password'} onClick={() => setVisible(v => !v)}>{visible ? <EyeOff size={16} /> : <Eye size={16} />}</button></div></label>
            <label>Confirm password<div className="ee-auth-input"><LockKeyhole size={16} /><input type={visible ? 'text' : 'password'} required autoComplete="new-password" value={confirmation} onChange={e => setConfirmation(e.target.value)} minLength={8} maxLength={72} placeholder="Re-enter your password" /></div></label>
          </>}
        </>}
        <button className="ee-auth-primary" type="submit" disabled={addressMismatch || busy || (!verified && cooldown > 0) || Boolean(!verified && !config?.emailConfigured) || Boolean((joining || token || google) && !verified)}>{busy ? (verified ? 'Please wait…' : 'Sending verification email…') : !verified && cooldown > 0 ? `Resend in ${cooldown}s` : verified ? joining ? 'Accept invitation' : 'Create organization' : 'Send verification email'}</button>
        {addressMismatch && <p className="ee-auth-description" role="status">This address differs from the configured platform address. <a href={config.publicUrl+'/signup'}>Open the official registration page</a>, or ask the platform administrator to update Settings → Access &amp; Login.</p>}
        {!verified && !joining && config && !config.emailConfigured && <p className="ee-auth-description">Email registration is not configured. Contact the platform administrator.</p>}
      </form>}
      {joining && !token && !google && <p className="ee-auth-notice">Open the invitation link from your email to join your organization.</p>}
      {!verified && !exchanging && config?.googleEnabled && !unavailable && (!joining || Boolean(token)) && <><div className="ee-auth-divider"><span>or</span></div><GoogleButton invitation={joining ? token : ''} onError={setError} /></>}
      <a className="ee-auth-back" href="/login"><ArrowLeft size={14} />Back to sign in</a>
    </section>
  </div></main>;
}
