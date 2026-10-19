import { useEffect, useState } from 'react';
import { KeyRound, LockKeyhole } from 'lucide-react';
import { apiGet, apiPost } from '../../api/client';
import { clearStoredAuthSession } from '../../auth/session';
import { ModalFrame } from '../../components/modal-frame';
import type { FrontendModuleContext } from '../../app/extensions';
import { SettingsHeader } from './organization-settings';
import './saas-auth.css';
export function AccountAccess(context: FrontendModuleContext) {
  const { toast } = context;
  const [state, setState] = useState<{ linked: boolean; enabled: boolean; passwordConfigured: boolean } | null>(null);
  const [show, setShow] = useState(false);
  const [password, setPassword] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  useEffect(() => { void apiGet<{ linked: boolean; enabled: boolean; passwordConfigured: boolean }>('/api/v1/saas/google/identity').then(setState).catch(e => toast(e.message)); }, [toast]);
  async function link(event: React.FormEvent) {
    event.preventDefault(); if (busy) return; setBusy(true); setError('');
    try { const result = await apiPost<{ url: string }>('/api/v1/saas/google/link', { password }); setPassword(''); window.location.assign(result.url); }
    catch (e) { setError(e instanceof Error ? e.message : 'Could not connect Google'); setBusy(false); }
  }
  return <section className="space-y-5 ee-organization-access"><SettingsHeader title="Sign-in & Security" description="Manage your password and connected sign-in methods." icon={<KeyRound size={18}/>}/><div className="ee-account-body"><PasswordAccess passwordConfigured={state?.passwordConfigured} toast={toast}/>
<div className="ee-connected-card"><h3>Google sign-in</h3><p className="ee-invite-description">{!state ? 'Loading connected account…' : state.linked ? 'Google is connected. You can use it to sign in to this account.' : state.enabled ? 'Connect your Google account as another way to sign in. Your organization and permissions stay the same.' : 'Google sign-in is not configured by the platform administrator.'}</p>{state?.enabled && !state.linked && <button className="hbdr-dr-action-primary mt-4" onClick={() => { setError(''); setPassword(''); setShow(true); }}><KeyRound size={15} />Connect Google</button>}</div></div>
    {show && <ModalFrame title="Connect Google" subtitle="Verify your password before connecting an external account." onClose={() => { if (!busy) setShow(false); }}><form className="ee-invite-form" onSubmit={e => void link(e)}><label>Current password<input type="password" autoComplete="current-password" required value={password} onChange={e => setPassword(e.target.value)} /></label>{error && <p role="alert" className="ee-settings-error">{error}</p>}<div className="ee-invite-actions"><button type="button" className="hbdr-dr-action-secondary" disabled={busy} onClick={() => setShow(false)}>Cancel</button><button type="submit" className="hbdr-dr-action-primary" disabled={busy}>{busy ? 'Connecting…' : 'Continue with Google'}</button></div></form></ModalFrame>}
  </section>;
}


function PasswordAccess({passwordConfigured,toast}:{passwordConfigured?:boolean;toast:FrontendModuleContext['toast']}) {
 const [show,setShow]=useState(false);const [currentPassword,setCurrentPassword]=useState('');const [newPassword,setNewPassword]=useState('');const [confirmation,setConfirmation]=useState('');const [busy,setBusy]=useState(false);const [error,setError]=useState('');
 const passwordAccount=passwordConfigured===true;
 async function change(e:React.FormEvent){e.preventDefault();if(busy)return;if(Array.from(newPassword).length<8||new TextEncoder().encode(newPassword).length>72){setError('Use at least 8 characters, within 72 UTF-8 bytes.');return;}if(newPassword!==confirmation){setError('Passwords do not match.');return;}setBusy(true);setError('');try{await apiPost('/api/v1/auth/change-password',{currentPassword,newPassword});setShow(false);setCurrentPassword('');setNewPassword('');setConfirmation('');clearStoredAuthSession();window.location.replace('/login?password_changed=1');}catch(e){setError(e instanceof Error?e.message:'Could not update password');}finally{setBusy(false);}}
 return <><div className="ee-connected-card"><h3>Password</h3><p className="ee-invite-description">{passwordConfigured===undefined?'Loading password settings…':passwordAccount?'Use a unique password with at least 8 characters.':'Your account uses Google sign-in.'}</p>{passwordAccount&&<button className="hbdr-dr-action-secondary mt-4" onClick={()=>{setCurrentPassword('');setNewPassword('');setConfirmation('');setError('');setShow(true);}}><LockKeyhole size={14}/>Change password</button>}</div>{show&&<ModalFrame title="Change password" subtitle="Verify your current password to set a new one." onClose={()=>{if(!busy)setShow(false);}}><form className="ee-invite-form" onSubmit={e=>void change(e)}><label>Current password<input type="password" autoComplete="current-password" required value={currentPassword} onChange={e=>setCurrentPassword(e.target.value)}/></label><label>New password<input type="password" autoComplete="new-password" required minLength={8} maxLength={72} value={newPassword} onChange={e=>setNewPassword(e.target.value)} placeholder="At least 8 characters"/></label><label>Confirm new password<input type="password" autoComplete="new-password" required minLength={8} maxLength={72} value={confirmation} onChange={e=>setConfirmation(e.target.value)}/></label>{error&&<p className="ee-settings-error" role="alert">{error}</p>}<div className="ee-invite-actions"><button type="button" className="hbdr-dr-action-secondary" disabled={busy} onClick={()=>setShow(false)}>Cancel</button><button className="hbdr-dr-action-primary" disabled={busy}>{busy?'Updating…':'Update password'}</button></div></form></ModalFrame>}</>;
}
