import { useEffect, useState, type FormEvent } from 'react';
import { Globe, Save, RefreshCw, Info, Pencil, Copy, Check } from 'lucide-react';
import { apiGet, apiPatch } from '../../api/client';
import type { FrontendModuleContext } from '../../app/extensions';
import { SettingsHeader } from './organization-settings';
import './saas-auth.css';

type Access = {publicUrl:string;googleCallback:string;verificationExample:string;invitationExample:string};
export function AccessLoginPage({currentUser,toast}:FrontendModuleContext){
 const [data,setData]=useState<Access|null>(null);const [url,setURL]=useState('');const [ack,setAck]=useState(false);const [busy,setBusy]=useState(false);const [error,setError]=useState('');
 const [editing,setEditing]=useState(false);
 const [google,setGoogle]=useState<boolean|null>(null);const [copied,setCopied]=useState(false);
 async function load(){setBusy(true);setError('');try{const v=await apiGet<Access>('/api/v1/platform/access');setData(v);setURL(v.publicUrl);setAck(false);setEditing(false);}catch(e){setError(e instanceof Error?e.message:'Could not load platform access settings.');}finally{setBusy(false);}}
 useEffect(()=>{void load();void apiGet<{googleEnabled:boolean}>('/api/v1/saas/auth/config').then(v=>setGoogle(v.googleEnabled)).catch(()=>setGoogle(null));},[]);
 useEffect(()=>{if(!copied)return;const timer=setTimeout(()=>setCopied(false),1500);return()=>clearTimeout(timer);},[copied]);
 if(!currentUser.systemAdmin)return null;
 const normalized=url.trim().replace(/\/+$/,'');const changed=!!data&&normalized!==data.publicUrl;
 let valid=false;try{const p=new URL(url.trim());valid=p.protocol==='https:'&&p.pathname==='/'&&!p.search&&!p.hash&&!p.username&&!p.password;}catch{/* Keep invalid input editable. */}
 async function save(e:FormEvent){e.preventDefault();if(busy||!ack||!changed||!valid)return;setBusy(true);setError('');try{const v=await apiPatch<Access>('/api/v1/platform/access',{publicUrl:normalized});setData(v);setURL(v.publicUrl);setAck(false);setEditing(false);toast('Platform address saved. New links use this address immediately.');}catch(e){setError(e instanceof Error?e.message:'Could not update platform access address.');}finally{setBusy(false);}}
 async function copy(){if(!data)return;try{await navigator.clipboard.writeText(data.googleCallback);setCopied(true);}catch{toast('Could not copy. Select the redirect URI to copy it.');}}
 return <section className="space-y-5 ee-organization-access hc-platform-access">
 <SettingsHeader title="Access & Sign-in" description="Manage the platform access address and sign-in configuration. Platform administrators only." icon={<Globe size={18}/>}/>
 <div className="ee-settings-panel"><header><div><h4>Platform access address</h4><p>Used to generate verification, invitation and password reset links, and the Google sign-in callback.</p></div><button type="button" className="hbdr-dr-action-secondary" disabled={busy} onClick={()=>void load()}><RefreshCw size={14}/>Refresh</button></header>
 {error&&<p className="ee-settings-error" role="alert">{error}</p>}
 {data&&<div className="hc-access-body">
 <div className="hc-access-summary"><dl className="hc-access-current"><dt>Configured URL</dt><dd>{data.publicUrl}</dd>{window.location.origin===data.publicUrl&&<dd className="hc-access-match"><Check size={14}/>Matches your current access address</dd>}</dl>{!editing&&<button type="button" className="hbdr-dr-action-secondary" disabled={busy} onClick={()=>{setURL(data.publicUrl);setAck(false);setEditing(true);}}><Pencil size={14}/>Change address</button>}</div>
 {window.location.origin!==data.publicUrl&&<div className="hc-access-warning" role="status"><Info size={18}/><div><p>You are accessing via {window.location.origin}</p><p>This differs from the configured address. A different access address does not necessarily require a configuration change.</p><button type="button" className="hc-access-use" disabled={busy} onClick={()=>{setURL(window.location.origin);setAck(false);setEditing(true);}}>Use this address</button></div></div>}
 {editing&&<form className="ee-invite-form hc-access-form" onSubmit={e=>void save(e)}>
 <label>New URL {changed&&<span className="hc-access-unsaved">Not saved</span>}<input type="url" required disabled={busy} value={url} onChange={e=>{setURL(e.target.value);setAck(false);}} placeholder="https://app.example.com" autoComplete="off"/></label>
 <p className="ee-invite-description">Use an HTTPS address without a path. Configure DNS, reverse proxy and TLS separately.</p>
 {changed&&!valid&&<p className="ee-settings-error" role="alert">Enter a valid HTTPS address without a path, query or fragment.</p>}
 <div className="hc-access-savebar"><label className="hc-access-confirm"><span><input type="checkbox" checked={ack} disabled={busy||!changed} onChange={e=>setAck(e.target.checked)}/>I confirm users can access the platform at this address.</span></label><div className="hc-access-edit-actions"><button type="button" className="hbdr-dr-action-secondary" disabled={busy} onClick={()=>{setEditing(false);setURL(data.publicUrl);setAck(false);setError('');}}>Cancel</button><button className="hbdr-dr-action-primary" disabled={busy||!changed||!ack||!valid}><Save size={14}/>{busy?'Saving…':'Save changes'}</button></div></div>
 <p className="ee-invite-description">Changes apply immediately to new email links and the Google callback. Existing email links and cluster connections remain unchanged.</p>
 </form>}</div>}</div>
 {data&&<div className="ee-settings-panel"><header><h4>Google sign-in</h4><span className="hc-access-google-status">{google===null?'Configuration unavailable':google?'Enabled':'Not configured'}</span></header><div className="hc-access-body">
 <p className="ee-invite-description">{google?'Google sign-in is enabled. Keep the redirect URI in Google Cloud Console synchronized with the configured address.':'Configure Google Client ID and Secret on the server to enable sign-in.'}</p>
 <dl className="hc-access-current"><dt>Authorized redirect URI · based on configured address</dt><dd className="hc-access-callback"><span>{data.googleCallback}</span><button type="button" className="hbdr-dr-action-secondary" onClick={()=>void copy()}>{copied?<Check size={14}/>:<Copy size={14}/>} {copied?'Copied':'Copy'}</button></dd></dl>
 <p className="ee-invite-description">Add this exact URI in Google Cloud Console. Google sign-in requires a supported domain and matching OAuth configuration; a private IP address is not supported as a web redirect URI.</p>
 </div><details className="hc-access-examples"><summary>View generated email link examples</summary><dl className="ee-settings-details"><div className="ee-settings-detail ee-settings-detail-wide"><dt>Verification email</dt><dd>{data.verificationExample}</dd></div><div className="ee-settings-detail ee-settings-detail-wide"><dt>Organization invitation</dt><dd>{data.invitationExample}</dd></div></dl></details></div>}
 </section>;
}
