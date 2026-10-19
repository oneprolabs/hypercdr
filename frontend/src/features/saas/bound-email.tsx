import { useState } from 'react';
import { Mail } from 'lucide-react';
import { apiGet, apiPost } from '../../api/client';
import { ModalFrame } from '../../components/modal-frame';
import type { FrontendModuleContext } from '../../app/extensions';
import { publicRequest } from './saas-auth';

export function BoundEmail({ currentUser, onCurrentUserChange, toast, inline = false }: FrontendModuleContext & { inline?: boolean }) {
 const [email,setEmail]=useState(''),[password,setPassword]=useState(''),[open,setOpen]=useState(false),[busy,setBusy]=useState(false),[error,setError]=useState('');
 const [saved,setSaved]=useState(currentUser.email);
 const [pending,setPending]=useState(false);
 async function refresh(){setBusy(true);setError('');try{const user=await apiGet<typeof currentUser>('/api/v1/auth/me');setSaved(user.email);if(user.email!==saved)setPending(false);onCurrentUserChange?.(user);toast(user.email?'Bound email updated.':'No verified email is bound yet.');}catch(e){setError(e instanceof Error?e.message:'Could not refresh bound email.');}finally{setBusy(false);}}
 async function request(e:React.FormEvent){e.preventDefault();if(busy)return;setBusy(true);setError('');try{const result=await apiPost<{message:string}>('/api/v1/saas/account/email',{email,password});setPassword('');setOpen(false);setPending(true);toast(result.message);}catch(e){setError(e instanceof Error?e.message:'Could not send verification email.');}finally{setBusy(false);}}
 const controls=<><button type="button" className="hbdr-dr-action-secondary" onClick={()=>{setEmail('');setPassword('');setError('');setOpen(true);}}><Mail size={14}/>{saved?'Change':'Bind email'}</button>{pending&&<button type="button" className="hbdr-dr-action-secondary" disabled={busy} onClick={()=>void refresh()}>Check verification</button>}</>;
 return <>{inline?<div className="ee-bound-email-inline"><span className={!saved?'ee-bound-email-unbound':undefined}>{saved||'Not bound'}</span>{controls}{pending&&<p className="ee-invite-description">Check your new email to confirm the change.</p>}{error&&!open&&<p role="alert" className="ee-settings-error">{error}</p>}</div>:<div className="ee-connected-card"><h3>Bound email</h3><p className={`ee-invite-description${!saved?' ee-bound-email-unbound':''}`}>{saved||'Not bound'}</p><p className="ee-invite-description">Your verified email is used for sign-in and password recovery.</p>{controls}</div>}{open&&<ModalFrame title={saved?'Change bound email':'Bind email'} subtitle="Your current email stays unchanged until you confirm the verification link sent to the new address." onClose={()=>{if(!busy)setOpen(false);}}><form className="ee-invite-form" onSubmit={e=>void request(e)}><label>New bound email<input type="email" required autoComplete="email" value={email} onChange={e=>setEmail(e.target.value)}/></label><label>Current password<input type="password" required autoComplete="current-password" value={password} onChange={e=>setPassword(e.target.value)}/></label>{error&&<p role="alert" className="ee-settings-error">{error}</p>}<div className="ee-invite-actions"><button type="button" className="hbdr-dr-action-secondary" disabled={busy} onClick={()=>setOpen(false)}>Cancel</button><button className="hbdr-dr-action-primary" disabled={busy}>{busy?'Sending…':'Send verification email'}</button></div></form></ModalFrame>}</>;
}

export function VerifyBoundEmail(){
 const [busy,setBusy]=useState(false),[message,setMessage]=useState(''),[error,setError]=useState('');
 const token=new URLSearchParams(window.location.search).get('token')||'';
 async function confirm(){if(busy)return;setBusy(true);setError('');try{const result=await publicRequest<{message:string}>('/email/verify',{token});setMessage(result.message);window.history.replaceState(null,'','/verify-email');}catch(e){setError(e instanceof Error?e.message:'Could not verify email.');}finally{setBusy(false);}}
 return <main className="ee-public-auth"><div className="ee-auth-container"><section className="ee-auth-card"><h1>Verify bound email</h1><p className="ee-auth-description">{message||'Confirm this address for your HyperCDR account. Your login account and organization will stay unchanged.'}</p>{error&&<p className="ee-settings-error" role="alert">{error}</p>}{!message&&<button className="ee-auth-primary" disabled={busy||!token} onClick={()=>void confirm()}>{busy?'Verifying…':'Confirm bound email'}</button>}<p className="ee-auth-description"><a href="/login">Back to sign in</a></p></section></div></main>;
}
